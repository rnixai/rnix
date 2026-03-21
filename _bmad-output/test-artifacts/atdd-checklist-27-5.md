---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-21'
inputDocuments:
  - _bmad-output/implementation-artifacts/27-5-top-watch-bidirectional-navigation.md
  - cmd/rnix/top.go
  - cmd/rnix/watch.go
  - cmd/rnix/top_test.go
  - cmd/rnix/atdd_27_4_watch_tui_test.go
---

# ATDD Checklist: Story 27.5 — top↔watch 双向导航

## 基本信息

| 项目 | 值 |
|---|---|
| Story ID | 27-5 |
| 技术栈 | backend (Go 1.26) |
| 生成模式 | AI 生成 |
| TDD 阶段 | RED（桩代码使行为测试失败） |
| 测试框架 | Go testing + race detector |

## TDD Red Phase 状态

共 30 个测试：21 FAIL / 8 PASS / 1 SKIP

- **FAIL (21)** — 行为测试，appModel.Update() 桩返回 `m, nil` 导致所有功能断言失败
- **PASS (8)** — 结构检查、回归测试、guard clause（桩的零值行为恰好正确）
- **SKIP (1)** — 前置条件依赖失败 (Enter 未实现)

### AC-1: top Enter 键进入 watch 视图

| # | 测试名 | 优先级 | 状态 |
|---|---|---|---|
| 1 | `TestATDD_27_5_AC1_AppModel_EnterKey_SwitchesToWatch` | P0 | FAIL |
| 2 | `TestATDD_27_5_AC1_AppModel_EnterKey_CreatesWatchModel` | P0 | FAIL |
| 3 | `TestATDD_27_5_AC1_AppModel_EnterKey_WatchTargetsPID` | P0 | FAIL |
| 4 | `TestATDD_27_5_AC1_AppModel_EnterKey_ReturnsInitCmd` | P0 | FAIL |
| 5 | `TestATDD_27_5_AC1_AppModel_EnterKey_EmptyRows_NoOp` | P1 | PASS |

### AC-2: watch q 键返回 top 视图

| # | 测试名 | 优先级 | 状态 |
|---|---|---|---|
| 6 | `TestATDD_27_5_AC2_AppModel_QKey_WatchNormal_ReturnsToTop` | P0 | FAIL |
| 7 | `TestATDD_27_5_AC2_AppModel_QKey_WatchExpanded_ReturnsToTop` | P0 | FAIL |
| 8 | `TestATDD_27_5_AC2_AppModel_BackToTop_ClearsWatch` | P0 | FAIL |
| 9 | `TestATDD_27_5_AC2_AppModel_BackToTop_ReturnsTick` | P1 | FAIL |
| 10 | `TestATDD_27_5_AC2_AppModel_CursorPreserved_AfterRoundTrip` | P1 | SKIP |

### AC-3: watch Ctrl+C 直接退出程序

| # | 测试名 | 优先级 | 状态 |
|---|---|---|---|
| 11 | `TestATDD_27_5_AC3_AppModel_CtrlC_TopMode_Quits` | P0 | FAIL |
| 12 | `TestATDD_27_5_AC3_AppModel_CtrlC_WatchMode_Quits` | P0 | FAIL |

### AC-4: watch Pager 中 q 返回 watch（非 top）

| # | 测试名 | 优先级 | 状态 |
|---|---|---|---|
| 13 | `TestATDD_27_5_AC4_AppModel_QKey_Pager_StaysInWatch` | P0 | FAIL |
| 14 | `TestATDD_27_5_AC4_AppModel_DoubleQ_PagerThenTop` | P1 | FAIL |

### AC-5: 统一 BubbleTea Program

| # | 测试名 | 优先级 | 状态 |
|---|---|---|---|
| 15 | `TestATDD_27_5_AC5_AppModel_ImplementsTeaModel` | P0 | PASS |
| 16 | `TestATDD_27_5_AC5_AppModel_DefaultModeIsTop` | P0 | PASS |
| 17 | `TestATDD_27_5_AC5_AppModel_Init_ReturnsCmd` | P0 | FAIL |
| 18 | `TestATDD_27_5_AC5_AppModel_View_TopMode_HasContent` | P0 | FAIL |
| 19 | `TestATDD_27_5_AC5_AppModel_View_WatchMode_HasContent` | P0 | FAIL |
| 20 | `TestATDD_27_5_AC5_AppModel_WindowSizeMsg_Propagates` | P1 | FAIL |

### AC-6: watch 视图目标进程已结束

| # | 测试名 | 优先级 | 状态 |
|---|---|---|---|
| 21 | `TestATDD_27_5_AC6_AppModel_WatchCompleted_QReturnsToTop` | P1 | FAIL |

### AC-7: IPC 连接生命周期管理

| # | 测试名 | 优先级 | 状态 |
|---|---|---|---|
| 22 | `TestATDD_27_5_AC7_AppModel_TopClient_Field` | P1 | PASS |

### AC-8: 独立 watch 命令不受影响 [回归]

| # | 测试名 | 优先级 | 状态 |
|---|---|---|---|
| 23 | `TestATDD_27_5_AC8_StandaloneWatch_QKey_Quits` | P0 | PASS |
| 24 | `TestATDD_27_5_AC8_StandaloneWatch_CtrlC_Quits` | P0 | PASS |

### AC-9: top 原有 detail 面板替换

| # | 测试名 | 优先级 | 状态 |
|---|---|---|---|
| 25 | `TestATDD_27_5_AC9_AppModel_EnterKey_NoDetailPID` | P1 | PASS |

### AC-10: top 视图帮助栏更新

| # | 测试名 | 优先级 | 状态 |
|---|---|---|---|
| 26 | `TestATDD_27_5_AC10_AppModel_TopHelpBar_ShowsWatch` | P0 | FAIL |

### Integration: 消息路由与导航

| # | 测试名 | 优先级 | 状态 |
|---|---|---|---|
| 27 | `TestATDD_27_5_TickMsg_InWatchMode_Ignored` | P1 | PASS |
| 28 | `TestATDD_27_5_TopMode_QKey_Quits` | P0 | FAIL |
| 29 | `TestATDD_27_5_TopMode_JK_Navigation` | P1 | FAIL |
| 30 | `TestATDD_27_5_TopMode_KKey_Kill` | P1 | PASS |

## 覆盖矩阵

| AC | 测试数 | P0 | P1 | 覆盖率 |
|---|---|---|---|---|
| AC-1 (Enter→watch) | 5 | 4 | 1 | 完整 |
| AC-2 (q→top) | 5 | 3 | 2 | 完整 |
| AC-3 (Ctrl+C) | 2 | 2 | 0 | 完整 |
| AC-4 (Pager隔离) | 2 | 1 | 1 | 完整 |
| AC-5 (appModel) | 6 | 4 | 2 | 完整 |
| AC-6 (进程结束) | 1 | 0 | 1 | 完整 |
| AC-7 (IPC生命周期) | 1 | 0 | 1 | 基础 |
| AC-8 (独立watch) | 2 | 2 | 0 | 完整（回归） |
| AC-9 (detail替换) | 1 | 0 | 1 | 完整 |
| AC-10 (帮助栏) | 1 | 1 | 0 | 完整 |

## 文件清单

| 文件 | 类型 | 说明 |
|---|---|---|
| `cmd/rnix/atdd_27_5_top_watch_nav_test.go` | 测试 | 30 个 ATDD 测试 |
| `cmd/rnix/top_app.go` | 桩代码 | appModel 最小桩，实现时替换 |

## Green Phase 指南

实现 Story 27.5 后，所有 30 个测试应变为 PASS：

1. 将 `top_app.go` 中的桩替换为完整 `appModel` 实现
2. 在 `appModel.Init()` 中委托到 `topModel.Init()`
3. 在 `appModel.Update()` 中实现消息路由（按键拦截 + 模式委托）
4. 在 `appModel.View()` 中按模式委托到子 model
5. 修改 `topModel.View()` 帮助栏文案：`Details` → `Watch`
6. `runTop` 改用 `newAppModel` + `tea.NewProgram(appModel)`
7. 运行 `go test -race -run TestATDD_27_5 ./cmd/rnix/` 确认全绿
