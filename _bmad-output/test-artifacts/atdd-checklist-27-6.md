---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04c-aggregate
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-22'
storyId: '27-6'
storyTitle: 'Dashboard 进程详情面板'
detectedStack: backend
generationMode: ai-generation
inputDocuments:
  - _bmad-output/implementation-artifacts/27-6-dashboard-process-detail-panel.md
  - _bmad/tea/config.yaml
  - _bmad/tea/testarch/tea-index.csv
  - _bmad/tea/testarch/knowledge/data-factories.md
  - _bmad/tea/testarch/knowledge/test-quality.md
  - _bmad/tea/testarch/knowledge/test-healing-patterns.md
  - _bmad/tea/testarch/knowledge/test-levels-framework.md
  - _bmad/tea/testarch/knowledge/test-priorities-matrix.md
---

# ATDD 检查清单: Story 27.6 — Dashboard 进程详情面板

## Step 1: 预检与上下文

- **检测栈类型**: `backend`（Go 项目，go.mod 存在）
- **测试框架**: Go 标准 `testing` 包 + race 检测
- **Story 状态**: ready-for-dev，验收标准清晰
- **配置标志**: `tea_use_playwright_utils`/`tea_use_pactjs_utils` 不适用（纯后端 Go 项目）

## Step 2: 生成模式

- **选择模式**: AI 生成（后端项目，无浏览器录制需求）
- **理由**: 验收标准清晰，场景标准（IPC 方法 + TUI 渲染），纯 Go 后端

## Step 3: 测试策略

### AC → 测试场景映射

| AC | 场景 | 测试级别 | 优先级 | 测试文件 |
|----|------|----------|--------|----------|
| AC-1 | MethodGetProcDetail 常量值 | Unit | P0 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-1 | GetProcDetailRequest 序列化 | Unit | P0 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-1 | GetProcDetailResponse 序列化 | Unit | P0 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-1 | SkillInfoWire 序列化 | Unit | P0 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-1 | FDEntryWire 序列化 | Unit | P0 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-1 | ContextStatsWire 序列化 | Unit | P0 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-1 | Server handler — Running 进程返回详情 | Integration | P0 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-1 | 环境变量敏感 key 脱敏 | Integration | P0 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-1 | FD 表返回 | Integration | P1 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-1 | 上下文统计返回 | Integration | P1 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-1 | Client 方法 roundtrip | Integration | P0 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-2 | Tab 键循环 4 窗格 | Unit | P0 | cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go |
| AC-2 | Tab 取模为 4 | Unit | P0 | cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go |
| AC-2 | Detail 窗格激活时高亮 | Unit | P1 | cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go |
| AC-2 | 帮助行提及 Detail | Unit | P1 | cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go |
| AC-3 | 基础信息区渲染 | Unit | P0 | cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go |
| AC-3 | Skill 列表区渲染 | Unit | P0 | cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go |
| AC-3 | FD 表区渲染 | Unit | P0 | cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go |
| AC-3 | 上下文统计区渲染 | Unit | P0 | cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go |
| AC-4 | IPC roundtrip ≤1s | Integration | P2 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-5 | PID 不存在返回 not_found | Integration | P0 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-5 | 切换进程触发刷新 | Unit | P1 | cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go |
| AC-5 | 缓存前一进程数据 | Unit | P1 | cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go |
| AC-6 | Dead 进程返回历史数据 | Integration | P1 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-6 | Dead 进程 DeadAtMs 非零 | Integration | P1 | ipc/atdd_27_6_getprocdetail_test.go |
| AC-6 | Dead 进程 FD 表为空 | Unit | P1 | cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go |
| AC-6 | Dead 进程显示历史 Skill | Unit | P1 | cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go |

### 优先级分布

| 优先级 | 数量 | 说明 |
|--------|------|------|
| P0 | 14 | 核心 IPC 类型 + 服务端 handler + Tab 切换 + 四分区渲染 |
| P1 | 12 | 环境脱敏、FD 表、缓存、Dead 进程、帮助行 |
| P2 | 1 | 性能 NFR |

## Step 4: TDD 红色阶段

### 生成的测试文件

| 文件 | 测试数 | 状态 |
|------|--------|------|
| `ipc/atdd_27_6_getprocdetail_test.go` | 15 | RED (t.Skip + 编译失败 — 类型未定义) |
| `cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go` | 12 | RED (t.Skip + 编译失败 — 字段未定义) |

**总计**: 27 个测试，全部处于红色阶段

### TDD 红色阶段说明

Go 语言的 TDD 红色阶段表现为**编译失败** — 测试引用了尚未定义的类型和方法：
- `MethodGetProcDetail`、`GetProcDetailRequest`、`GetProcDetailResponse` — ipc/protocol.go 中未定义
- `SkillInfoWire`、`FDEntryWire`、`ContextStatsWire` — 新 wire 类型未定义
- `Client.GetProcDetail()` — 客户端方法未实现
- `paneDetail` — dashboard.go 中的新窗格常量未定义
- `procDetail`、`procDetailPID` — dashboardModel 字段未添加

每个测试函数开头都有 `t.Skip("TDD RED PHASE: ...")` 作为二级保护。

## Step 5: 验证

- [x] 前置条件满足（Story approved，Go 测试框架就绪）
- [x] 测试文件已创建
- [x] 检查清单覆盖所有 6 个验收标准
- [x] 测试设计为实现前失败（编译错误 + t.Skip）
- [x] 无 CLI 会话残留
- [x] 产物存储在正确位置

## 绿色阶段指南

实现完成后：
1. 在 `ipc/protocol.go` 中定义所有 wire 类型 → 测试应能编译
2. 在 `kernel/kernel.go` 中实现 `GetProcDetail` 内核方法
3. 在 `ipc/server.go` 中实现 handler
4. 在 `ipc/client.go` 中实现客户端方法
5. 在 `cmd/rnix/dashboard.go` 中添加 Detail 窗格
6. 移除所有 `t.Skip()` 调用
7. 运行 `go test -race ./ipc/... ./cmd/rnix/...` → 验证全部通过
