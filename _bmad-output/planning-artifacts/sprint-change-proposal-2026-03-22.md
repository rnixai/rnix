---
date: '2026-03-22'
author: Decker
trigger: 'Party Mode 讨论发现 watch 与 dashboard 功能重叠 + PID 管理系统级缺陷'
scope: major
status: approved
---

# Sprint Change Proposal: Dashboard 增强 + PID 标识体系重构

## 1. 问题摘要

### 触发事件

2026-03-22 Party Mode 讨论中，在评估统一观察系统方案时发现两个关键问题：

**问题 A：watch 与 dashboard 功能重叠**

Epic 27 设计了独立的 `rnix watch` 命令（Story 27-3/27-4/27-5），但 Epic 17 已实现的 `rnix dashboard`（1785 行代码）包含智能体树 + syscall 时间线 + 上下文热力图 + 窗格联动 + 离线回放，与 watch 目标高度重叠。两个独立命令做类似的事会让用户困惑。

**决策：** 方案 A——增强 dashboard，不新建独立 watch 命令。将 watch 的独特能力（三级详细度、prompt 查看、top 下钻）合并到 dashboard 中。

**问题 B：PID 标识体系缺陷**

调查 dashboard 刷新问题时发现：PID 在 daemon 内递增不回收，但 daemon 重启后从 1 开始。跨 daemon 的 PID 复用导致：
- Step Records 路径 `.rnix/data/steps/<pid>/` 仅用 PID，新进程数据覆盖旧进程
- GetStepDetail 可能返回混合数据
- AttachDebug/Log 可能误 attach 新进程
- Dashboard selectedPID 无有效性检查

### 分类

- 问题 A：**失败的方案需要不同解决路径**
- 问题 B：**实现过程中发现的技术缺陷**

### 证据

- `cmd/rnix/dashboard.go`：1785 行，已实现树+时间线+热力图+联动+回放
- `cmd/rnix/watch.go`：715 行，功能与 dashboard 时间线窗格重叠
- `kernel/process.go:16-23`：`pidCounter` 为内存全局变量，daemon 重启归零
- `kernel/step_writer.go:22-29`：Step 路径仅用 PID，无 epoch/UUID 区分

---

## 2. 影响分析

### Epic 影响

| Epic | 影响 | 说明 |
|------|------|------|
| **Epic 27（统一观察系统）** | **重大变更** | 27-3/27-4/27-5 已回滚，需重新定义为 dashboard 增强 Story |
| **Epic 17（Dashboard）** | 间接受益 | dashboard 将从"调试面板"升级为"Rnix 完整能力透视窗" |
| 其他 Epic | 不受影响 | — |

### 产物冲突

| 产物 | 影响 | 状态 |
|------|------|------|
| PRD functional-requirements.md | FR62/FR165-FR171 已改为 dashboard 增强 | **已更新** |
| PRD non-functional-requirements.md | NFR57-NFR62-obs 已改为 dashboard 增强 | **已更新** |
| PRD user-journeys.md | 旅程 7 已改为 dashboard 路径 | **已更新** |
| Architecture core-architectural-decisions.md | Decision 23-26 已改为 dashboard 增强 | **已更新** |
| Architecture implementation-patterns.md | watch 引用已改为 dashboard | **已更新** |
| Architecture project-structure-boundaries.md | watch.go 引用已改为 dashboard.go | **已更新** |
| Product Brief | watch 改为 dashboard 增强 | **已更新** |
| sprint-status.yaml | 27-3/27-4/27-5 标记为 rolled-back | **已更新** |
| PRD/Architecture PID 相关 | **需要新增** UUID v7 相关 FR/NFR/Decision | **已更新（PRD）** |

### 技术影响

**已执行的代码变更：**
- `git revert f39e7d9`（27-5 top↔watch）
- `git revert 5e77a62`（27-4 watch 三级详细度）
- `git revert 399ebc9`（27-3 watch 命令）
- watch.go 已删除，top.go 已恢复，main.go 已恢复

**保留的基础设施（27-1 + 27-2）：**
- StepRecord 类型和 StepWriter（磁盘全量步骤记录）
- GetStepDetail IPC 方法（按需查询完整 prompt）

---

## 3. 推荐方案

### 选择：混合方案（直接调整 + MVP 审视）

**变更 1：重新定义 Epic 27 剩余 Story（直接调整）**

将 27-3/27-4/27-5 替换为 dashboard 增强 Story：

| 新 Story | 内容 | 优先级 |
|---------|------|--------|
| **27-3（重定义）** | Dashboard 时间线三级详细度（v/V 键切换 Level 1/2/3） | P0 |
| **27-4（重定义）** | Dashboard prompt 查看（p 键 + GetStepDetail 集成） | P0 |
| **27-5（重定义）** | top→dashboard 导航（top 中选进程按回车跳转 dashboard） | P0 |
| **27-6（新增）** | Dashboard 进程详情面板（当前仅 top 有详情视图） | P1 |
| **27-7（新增）** | Dashboard 意图树集成（Intent DAG 可视化） | P1 |
| **27-8（新增）** | Dashboard 安全异常面板（Immune 告警集成） | P2 |
| **27-9（新增）** | Dashboard 分布式追踪集成（Trace span 树） | P2 |
| **27-10（新增）** | Dashboard 多智能体评价视图（声誉+拓扑+协同） | P2 |

**变更 2：新增 Epic 28 — PID 标识体系重构**

| Story | 内容 | 优先级 |
|-------|------|--------|
| **28-1** | Process UUID v7 引入：Process 结构体新增 UUID 字段，Spawn 时生成，所有持久化路径改用 UUID | P0 |
| **28-2** | StepRecord 路径迁移：`.rnix/data/steps/<pid>/` → `.rnix/data/steps/<uuid>/` | P0 |
| **28-3** | IPC PID→UUID 映射：GetStepDetail 等方法支持按 UUID 查询，保持 PID 作为用户友好短标识 | P1 |
| **28-4** | Dashboard PID 有效性：selectedPID 增加有效性检查，进程死亡后正确清除 | P1 |

### 理由

- **直接调整**：dashboard 增强复用现有 1785 行代码基础，避免新建独立命令的重复实现
- **MVP 审视**：PID 问题是系统级缺陷，影响数据完整性，必须在数据量增长前修复
- **风险低**：所有底层 IPC 方法已就绪，只需 UI 集成；UUID v7 是标准化方案
- **向后兼容**：PID 保留为用户友好标识，UUID 用于内部持久化

### 工作量评估

- Epic 27 剩余（P0 部分）：中等
- Epic 28（P0 部分）：中等
- 总体：中等偏大

---

## 4. 详细变更提案

### 4.1 Epic 27 Story 重定义

**Story 27-3（重定义）：Dashboard 时间线三级详细度**

```
OLD: Story 27-3 — watch 命令 Level 1 实时流
     创建独立的 rnix watch <pid> 命令

NEW: Story 27-3 — Dashboard 时间线三级详细度
     在 dashboard 时间线窗格中增加三级详细度切换：
     - Level 1（默认）：每步一行摘要
     - Level 2（v 键）：展开参数、返回值、token 消耗
     - Level 3（V 键）：调试级，含 prompt 摘要
     数据通过 GetStepDetail IPC 获取（27-2 已实现）

Rationale: dashboard 已有时间线窗格和事件流，增加详细度切换比新建独立命令更合理
```

**Story 27-4（重定义）：Dashboard prompt 查看**

```
OLD: Story 27-4 — watch 三级详细度 + prompt 查看
     在 watch TUI 中按 p 键查看完整 prompt

NEW: Story 27-4 — Dashboard prompt 查看
     在 dashboard 时间线窗格中按 p 键进入 prompt 翻页模式：
     - 显示 SystemPrompt + Messages + Tools 定义
     - 类似 less 的翻页查看，按 q 返回时间线
     数据通过 GetStepDetail IPC 获取

Rationale: 复用 dashboard 已有的键盘事件处理和窗格管理
```

**Story 27-5（重定义）：top→dashboard 导航**

```
OLD: Story 27-5 — top↔watch 双向导航
     top 中按回车进入 watch，watch 中按 q 返回 top

NEW: Story 27-5 — top→dashboard 导航
     top 中选中进程按回车跳转到 dashboard，自动聚焦该进程
     dashboard 中按 q 退出（不返回 top，因为 dashboard 是独立全屏 TUI）

Rationale: dashboard 是独立的全屏应用，不适合嵌入 top，改为进程跳转
```

### 4.2 Epic 28 新增

**Epic 28：PID 标识体系重构**

```
触发：Dashboard 刷新问题调查中发现 PID 跨 daemon 复用导致数据混淆
目标：引入 UUID v7 作为进程的持久化唯一标识，PID 保留为用户友好短标识
依赖：无（可与 Epic 27 并行）
```

### 4.3 PRD 变更

**需要新增的 FR（PID 体系）：**
- FR173：系统为每个进程在 Spawn 时生成 UUID v7 唯一标识符，UUID 在跨 daemon 重启后保持唯一
- FR174：所有持久化数据路径使用 UUID 而非 PID（Step Records、process-meta.json 等）
- FR175：IPC 方法支持按 PID 或 UUID 查询，PID 在当前 daemon 内唯一，UUID 全局唯一
- FR176：Dashboard 通过 UUID 验证进程同一性，进程死亡并被清理后正确更新 UI

**需要新增的 NFR：**
- NFR65-obs：UUID v7 生成延迟 ≤ 1ms（不影响 Spawn 性能）

### 4.4 架构变更

**需要新增的 Decision：**
- Decision 27：Process UUID v7 标识体系——PID 为 daemon 内短标识（用户可见），UUID v7 为持久化唯一标识（内部使用）

---

## 5. 实施交接

### 变更范围分类：**Moderate**（中等）

需要重新规划 backlog，但不影响项目核心架构方向。

### 交接计划

| 角色 | 职责 |
|------|------|
| 📋 John（PM） | 更新 PRD（新增 FR173-FR176 + NFR65-obs），更新 Epic 列表 |
| 🏗️ Winston（架构师） | 新增 Decision 27（UUID v7 体系），更新架构验证 |
| 🏃 Bob（SM） | 重新定义 Epic 27 Story 27-3/27-4/27-5，新建 Epic 28 Story，更新 sprint-status |
| 💻 Amelia（Dev） | 实施 Story |

### 成功标准

1. Dashboard 时间线支持三级详细度 + prompt 查看
2. top→dashboard 跳转可用
3. Process UUID v7 生成并用于所有持久化路径
4. daemon 重启后不再有 PID 数据混淆
5. 现有测试全部通过
