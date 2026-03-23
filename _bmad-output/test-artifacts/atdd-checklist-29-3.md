---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-04c-aggregate
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-23'
storyId: '29-3'
storyTitle: '默认视图 Focus Card'
detectedStack: backend
generationMode: ai-generation
executionMode: sequential
inputDocuments:
  - _bmad-output/implementation-artifacts/29-3-default-view-focus-card.md
  - _bmad/tea/config.yaml
  - cmd/rnix/dashboard_types.go
  - cmd/rnix/dashboard.go
  - cmd/rnix/atdd_29_2_view_mode_nav_overhaul_test.go
---

# ATDD Checklist: Story 29.3 — 默认视图 Focus Card

## TDD Red Phase（当前）

🔴 **所有 16 个测试均处于 FAILING 状态**（TDD Red Phase）

### 测试文件

| 文件 | 测试数 | 状态 |
|------|--------|------|
| `cmd/rnix/atdd_29_3_default_view_focus_card_test.go` | 16 | 🔴 RED（14 failing, 2 passing） |

### 通过的测试（验证现有代码不变性）

| 测试 ID | 名称 | AC | 说明 |
|---------|------|-----|------|
| 29.3-UNIT-007 | TestFocusCard_ExpandedLayoutUnchanged | AC-4 | 展开视图不受影响 |
| 29.3-UNIT-016 | TestFocusCard_DefaultLayoutProportions | AC-4 | 布局比例已符合规范 |

### 失败的测试（需实现后通过）

| 测试 ID | 优先级 | 名称 | AC | 失败原因 |
|---------|--------|------|-----|---------|
| 29.3-UNIT-001 | P0 | TestFocusCard_FileExistsWithExpectedFunctions | AC-1,5 | dashboard_focus.go 不存在 |
| 29.3-UNIT-002 | P0 | TestFocusCard_TypesExistInDashboardTypes | AC-1 | focusCardState/intentMiniTask 未定义 |
| 29.3-UNIT-003 | P0 | TestFocusCard_DashboardModelHasFocusCardField | AC-1 | dashboardModel 缺少 focusCardData 字段 |
| 29.3-UNIT-004 | P0 | TestFocusCard_FocusCardStateFields | AC-1 | focusCardState 不存在 |
| 29.3-UNIT-005 | P0 | TestFocusCard_IntentMiniTaskFields | AC-1 | intentMiniTask 不存在 |
| 29.3-UNIT-006 | P0 | TestFocusCard_DefaultLayoutNoActivePaneSwitch | AC-4 | renderDefaultLayout 仍含 activePane switch |
| 29.3-UNIT-008 | P1 | TestFocusCard_TickTriggersAggregate | AC-2 | dashboard.go 未调用 aggregateFocusCard |
| 29.3-UNIT-009 | P0 | TestFocusCard_UsesLipglossBorders | AC-5 | dashboard_focus.go 不存在 |
| 29.3-UNIT-010 | P0 | TestFocusCard_RenderFuncSignature | AC-1 | dashboard_focus.go 不存在 |
| 29.3-UNIT-011 | P0 | TestFocusCard_SixCardRenderMethodsExist | AC-1 | dashboard_focus.go 不存在 |
| 29.3-UNIT-012 | P0 | TestFocusCard_GridLayoutStructure | AC-1 | dashboard_focus.go 不存在 |
| 29.3-UNIT-013 | P1 | TestFocusCard_DeadProcessDifferentialRendering | AC-3 | dashboard_focus.go 不存在 |
| 29.3-UNIT-014 | P1 | TestFocusCard_ResultCardReplacesAlertsForDead | AC-3 | dashboard_focus.go 不存在 |
| 29.3-UNIT-015 | P0 | TestFocusCard_AggregateUsesExistingFields | AC-2 | dashboard_focus.go 不存在 |

## 验收准则覆盖

| AC | 描述 | 测试覆盖 |
|----|------|---------|
| AC-1 | Focus Card 2×3 网格渲染 | UNIT-001, 002, 003, 004, 005, 010, 011, 012 |
| AC-2 | Running 进程实时数据 | UNIT-008, 015 |
| AC-3 | Dead 进程快照 | UNIT-013, 014 |
| AC-4 | 默认视图布局变更 | UNIT-006, 007, 016 |
| AC-5 | lipgloss 卡片渲染 | UNIT-001, 009 |

## 测试策略

- **测试级别**：Unit（AST 结构验证 + 源码文本分析）
- **技术选择**：Go `go/ast` + `go/parser` + `os.ReadFile` + `strings.Contains`
- **原因**：Go 不支持 `test.skip()` 式的 TDD red phase，使用 AST 解析验证结构存在性，确保测试编译通过但逻辑失败
- **零 IPC 依赖**：所有测试均为纯文件/AST 分析，无需 daemon 运行

## 下一步（TDD Green Phase）

实现 Story 29.3 后：

1. 创建 `cmd/rnix/dashboard_focus.go`（包含 aggregateFocusCard + renderFocusCard + 6 个卡片方法）
2. 在 `cmd/rnix/dashboard_types.go` 新增 `focusCardState` 和 `intentMiniTask` 类型
3. 在 `dashboardModel` 新增 `focusCardData *focusCardState` 字段
4. 修改 `renderDefaultLayout` 调用 `renderFocusCard` 替代 activePane switch
5. 在 `dashboardTick` 中调用 `aggregateFocusCard`
6. 运行 `go test -race -run TestFocusCard ./cmd/rnix/...` 验证全部通过
7. 运行 `make all` 确保无回归

## 风险和假设

- **假设**：`vfs.ProcInfo` 没有 `ExitCode` 字段，Dead 进程的退出状态需通过其他方式获取（如 `Result` 字段或 `procDetail` 响应）
- **风险**：Focus Card 渲染可能在小终端尺寸下溢出，需要 `max()` 保护
- **假设**：`heatmapProfile` 的 `Classification` 结构用于 Context 卡片渲染（而非独立的 pct 字段）
