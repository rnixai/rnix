# Epic 28: PID 标识体系重构（Process Identity System）

系统引入 UUID v7 作为进程的持久化唯一标识，PID 保留为 daemon 内递增的用户友好短标识。所有持久化数据路径（StepRecord、process-meta.json 等）使用 UUID 而非 PID，消除 daemon 重启后 PID 复用导致的数据覆盖和混淆。IPC 方法支持按 PID 或 UUID 查询，Dashboard 通过 UUID 验证进程同一性。

> **设计基础**
>
> | 文档 | 说明 |
> |------|------|
> | [PRD FR177-FR180](../prd/functional-requirements.md#process-identity-system进程标识体系phase-2) | 进程标识体系功能需求 |
> | [PRD NFR65-obs](../prd/non-functional-requirements.md#unified-observation-system-quality统一观察系统质量--dashboard-增强phase-2) | UUID 性能需求 |
> | [Architecture Decision 27](../architecture/core-architectural-decisions.md#decision-27-process-uuid-v7-标识体系--pid-短标识--uuid-持久化唯一标识) | UUID v7 标识体系架构决策 |
> | [Sprint Change Proposal](../sprint-change-proposal-2026-03-22.md) | PID 问题发现与决策 |
>
> **触发事件：** Dashboard 刷新问题调查中发现 PID 跨 daemon 复用导致数据混淆——`pidCounter` 为内存全局变量，daemon 重启归零；Step 路径仅用 PID，新进程数据覆盖旧进程。

**架构决策：** Decision 27（Process UUID v7 标识体系）
**FRs covered:** FR177, FR178, FR179, FR180
**NFRs:** NFR65-obs（UUID v7 生成 ≤ 1ms）
**Dependencies:** 无（可与 Epic 27 并行）

---

## Story 28.1: Process UUID v7 引入

As a 平台构建者,
I want 系统为每个进程在 Spawn 时生成 UUID v7 唯一标识符,
So that 每个进程在跨 daemon 重启后仍保持全局唯一身份。

**FRs:** FR177
**NFRs:** NFR65-obs（UUID 生成 ≤ 1ms，存储开销 ≤ 36 bytes/进程）

**Acceptance Criteria:**

**Given** Process 结构体（`kernel/process.go`）
**When** 添加 UUID 字段
**Then** 新增 `UUID string` 字段（标准 UUID v7 字符串格式，36 字符）
**And** UUID 在进程创建后不可变

**Given** Kernel.Spawn 方法
**When** 创建新进程
**Then** 生成 UUID v7 并赋值给 Process.UUID
**And** UUID v7 包含时间戳（毫秒精度），保证时间有序
**And** 生成延迟 ≤ 1ms（NFR65-obs）

**Given** 两个 daemon 生命周期
**When** daemon 重启后创建新进程
**Then** 新进程的 UUID 与旧 daemon 中所有进程的 UUID 不同
**And** PID 可能从 1 重新开始（PID 行为不变）

**Given** `rnix ps` 输出
**When** 显示进程列表
**Then** 默认显示 PID（用户友好短标识）
**And** `--uuid` flag 额外显示 UUID 列

**Given** spawn 完成输出
**When** 显示进程信息
**Then** 输出中包含 PID 和 UUID（如 `PID 3 (uuid: 019...)`）

**Technical Notes:**
- 修改文件：`kernel/process.go`（新增 UUID 字段）、`kernel/kernel.go`（Spawn 中生成 UUID）
- 新增依赖：UUID v7 生成库（如 `github.com/google/uuid` 的 v7 支持）
- `ipc/protocol.go`：ProcInfo 新增 UUID 字段
- `cmd/rnix/ps.go`：新增 --uuid flag

---

## Story 28.2: StepRecord 路径迁移到 UUID

As a 平台构建者,
I want 所有持久化数据路径使用 UUID 而非 PID,
So that daemon 重启后新进程不会覆盖旧进程的 StepRecord 数据。

**FRs:** FR178
**NFRs:** NFR60-obs（StepRecord 写入性能不受影响）

**Acceptance Criteria:**

**Given** StepWriter 创建步骤目录
**When** 进程首次进入 reasonStep 循环
**Then** 目录路径从 `.rnix/data/steps/<pid>/` 变更为 `.rnix/data/steps/<uuid>/`
**And** 文件名仍为 `steps.jsonl`

**Given** process-meta.json 写入
**When** reaper 清理进程时
**Then** 路径从 `.rnix/data/steps/<pid>/process-meta.json` 变更为 `.rnix/data/steps/<uuid>/process-meta.json`
**And** process-meta.json 中包含 PID 字段（用于反向查找）

**Given** 旧格式的 `.rnix/data/steps/<pid>/` 目录
**When** 系统遇到纯数字目录名
**Then** 保持向后兼容——可读取但新写入使用 UUID 路径

**Given** `rnix record list` 命令
**When** 列出已记录的进程
**Then** 扫描 `.rnix/data/steps/` 下所有子目录
**And** UUID 格式目录显示 UUID + 从 meta 中读取的 PID
**And** 旧 PID 格式目录标记为 "(legacy)"

**Given** `rnix replay <id>` 命令
**When** 指定 UUID 前缀
**Then** 匹配 `.rnix/data/steps/<uuid>/` 目录
**And** 加载 steps.jsonl 进行回放

**Technical Notes:**
- 修改文件：`kernel/step_writer.go`（路径使用 UUID）、`kernel/kernel.go`（reaper 中 meta 路径）
- 修改文件：`cmd/rnix/record.go`、`cmd/rnix/replay.go`（UUID 路径支持）
- 7 天自动清理策略不变，基于目录修改时间

---

## Story 28.3: IPC PID→UUID 映射

As a 平台构建者,
I want IPC 方法（GetStepDetail 等）支持按 PID 或 UUID 查询,
So that 我可以用 PID 快速查询当前 daemon 中的进程，也可以用 UUID 查询历史进程。

**FRs:** FR179

**Acceptance Criteria:**

**Given** GetStepDetail IPC 请求
**When** 请求中包含 PID
**Then** daemon 内部通过进程表将 PID 映射到 UUID
**And** 使用 UUID 路径读取 steps.jsonl

**Given** GetStepDetail IPC 请求
**When** 请求中包含 UUID（新增字段）
**Then** 直接使用 UUID 路径读取 steps.jsonl
**And** 跳过 PID→UUID 映射步骤

**Given** GetStepDetailRequest 结构体
**When** 扩展
**Then** 新增 `UUID string` 可选字段
**And** PID 和 UUID 至少提供一个，UUID 优先

**Given** 进程已被 reaper 清理（不在内存中）
**When** 用 PID 查询
**Then** 返回 not_found 错误（PID 仅在当前 daemon 生命周期内有效）

**Given** 进程已被 reaper 清理
**When** 用 UUID 查询
**Then** 从 `.rnix/data/steps/<uuid>/` 读取数据
**And** 正常返回（UUID 全局唯一，不受 daemon 生命周期限制）

**Given** 其他 IPC 方法（如 attach_debug、get_log 等）
**When** 涉及进程查询
**Then** 统一支持 PID 或 UUID 参数

**Technical Notes:**
- 修改文件：`ipc/protocol.go`（GetStepDetailRequest 新增 UUID）、`ipc/server.go`（dispatch 中 UUID 优先查找）
- 新增辅助方法：`resolveProcessByPIDOrUUID(pid, uuid)` 统一查找逻辑

---

## Story 28.4: Dashboard PID 有效性检查

As a 平台构建者,
I want Dashboard 通过 UUID 验证进程同一性,
So that 进程死亡并被 Reaper 清理后，新进程复用 PID 不会导致 Dashboard 误显示。

**FRs:** FR180

**Acceptance Criteria:**

**Given** Dashboard 中选中了某个进程（selectedPID + selectedUUID）
**When** 该进程死亡并被 Reaper 清理
**Then** Dashboard 在下一次刷新时检测到 PID 对应的进程已不存在
**And** 正确清除选中状态
**And** 时间线窗格切换到空状态或进程列表

**Given** 旧进程（PID=3, UUID=A）死亡后，新进程复用 PID=3（UUID=B）
**When** Dashboard 持有 selectedPID=3, selectedUUID=A
**Then** 刷新时发现 PID=3 的 UUID 为 B ≠ A
**And** 判定为不同进程，清除旧选中状态
**And** 不会显示新进程的数据覆盖旧进程的视图

**Given** Dashboard Model 结构体
**When** 添加 UUID 跟踪
**Then** 新增 `selectedUUID string` 字段
**And** 选中进程时同时记录 PID 和 UUID
**And** 每次刷新数据时验证 PID→UUID 映射一致性

**Given** Dashboard 通过 IPC 获取进程列表
**When** ProcInfo 中包含 UUID 字段
**Then** 使用 UUID 进行进程同一性验证
**And** 不依赖 PID 作为唯一标识

**Technical Notes:**
- 修改文件：`cmd/rnix/dashboard.go`（新增 selectedUUID + 有效性检查逻辑）
- 依赖：Story 28.1（Process UUID 字段）
- 刷新周期中的 UUID 验证不增加额外 IPC 调用（ProcInfo 已包含 UUID）
