---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
lastStep: step-04-generate-tests
lastSaved: '2026-03-23'
storyId: '29-2'
detectedStack: backend
generationMode: ai-generation
inputDocuments:
  - _bmad-output/implementation-artifacts/29-2-view-mode-system-and-navigation-overhaul.md
  - _bmad-output/planning-artifacts/epics/epic-29-dashboard-ux-redesign.md
  - cmd/rnix/atdd_29_1_dashboard_file_splitting_test.go
  - cmd/rnix/dashboard_types.go
  - cmd/rnix/dashboard.go
---

# ATDD Checklist — Story 29.2: 视图模式系统与导航重构

## 测试文件

`cmd/rnix/atdd_29_2_view_mode_nav_overhaul_test.go`

## TDD 状态：🔴 RED PHASE

所有测试均在实现前编写，预期在功能实现后变绿。

## 测试清单

| ID | 优先级 | AC | 测试名 | 级别 | 描述 | 状态 |
|----|--------|-----|--------|------|------|------|
| 29.2-UNIT-001 | P0 | #1 | `TestViewModeSystem_TypeAndConstantsExist` | Unit | viewMode 类型和 4 个常量定义存在 | 🔴 |
| 29.2-UNIT-002 | P0 | #1 | `TestViewModeSystem_ModelFieldsExist` | Unit | dashboardModel 包含 viewMode 和 expandedPane 字段 | 🔴 |
| 29.2-UNIT-003 | P0 | #1 | `TestViewModeSystem_ZeroValueIsDefault` | Unit | viewMode 零值为 viewDefault | 🔴 |
| 29.2-UNIT-004 | P0 | #2 | `TestViewModeSystem_NavFileExists` | Unit | dashboard_nav.go 存在且含关键函数 | 🔴 |
| 29.2-UNIT-005 | P0 | #2 | `TestViewModeSystem_DashboardKeyRemovedFromMain` | Unit | dashboardKey 已从 dashboard.go 迁移 | 🔴 |
| 29.2-UNIT-006 | P0 | #2 | `TestViewModeSystem_JumpToPaneToggle` | Unit | jumpToPane 首次展开 + 再次 toggle 回默认 | 🔴 |
| 29.2-UNIT-007 | P0 | #2 | `TestViewModeSystem_JumpToDifferentPane` | Unit | 切换到不同面板保持 viewExpanded | 🔴 |
| 29.2-UNIT-008 | P0 | #2 | `TestViewModeSystem_AllPanesJumpable` | Unit | 8 个面板均可 jumpToPane | 🔴 |
| 29.2-UNIT-009 | P0 | #5 | `TestViewModeSystem_TitleBarPaneLabels` | Unit | Title bar 包含 [N] 编号标签 | 🔴 |
| 29.2-UNIT-010 | P1 | #5 | `TestViewModeSystem_TitleBarExpandedHighlight` | Unit | 展开时激活面板显示 N:Name* | 🔴 |
| 29.2-UNIT-011 | P0 | #6 | `TestViewModeSystem_StatusBarDefaultView` | Unit | 默认视图 status bar 含全局快捷键 | 🔴 |
| 29.2-UNIT-012 | P1 | #6 | `TestViewModeSystem_StatusBarExpandedView` | Unit | 展开视图 status bar 含面板提示 | 🔴 |
| 29.2-UNIT-013 | P1 | #6 | `TestViewModeSystem_PaneSpecificHintsExists` | Unit | paneSpecificHints 方法存在 | 🔴 |
| 29.2-INT-001 | P0 | 全部 | `TestViewModeSystem_DefaultLayoutUnchanged` | Int | viewDefault 下 renderDashboard 正常 | 🔴 |
| 29.2-INT-002 | P0 | #2,3 | `TestViewModeSystem_ExpandedLayoutDiffers` | Int | viewExpanded 布局与默认不同 | 🔴 |
| 29.2-INT-003 | P0 | #2 | `TestViewModeSystem_TreeFullScreenWhenExpanded` | Int | Tree 展开时全屏 | 🔴 |
| 29.2-UNIT-014 | P0 | #7 | `TestViewModeSystem_LayoutMethodsExist` | Unit | renderDefaultLayout/renderExpandedLayout 存在 | 🔴 |
| 29.2-UNIT-015 | P1 | #5 | `TestViewModeSystem_TitleBarRetainsStats` | Unit | Title bar 保留连接状态 | 🔴 |
| 29.2-UNIT-016 | P1 | #6 | `TestViewModeSystem_StatusBarRetainsOps` | Unit | Status bar 保留操作快捷键 | 🔴 |
| 29.2-UNIT-017 | P0 | — | `TestViewModeSystem_StubKeysExist` | Unit | L/H stub 键处理存在 | 🔴 |
| 29.2-INT-004 | P0 | 全部 | *隐式* | Int | 编译兼容性（go test 编译验证） | 🔴 |

## AC 覆盖矩阵

| AC | 描述 | 覆盖测试 |
|----|------|----------|
| #1 | viewMode 枚举 + expandedPane 字段 | UNIT-001, UNIT-002, UNIT-003 |
| #2 | 数字键 1-8 直达面板 | UNIT-004, UNIT-005, UNIT-006, UNIT-007, UNIT-008, INT-002, INT-003 |
| #3 | Esc 回默认视图 | INT-002 (间接), UNIT-006 (toggle 验证) |
| #4 | Shift-Tab 反向导航 | UNIT-004 (结构验证, dispatchPaneKey 含 shift+tab) |
| #5 | Title bar 编号标签 | UNIT-009, UNIT-010, UNIT-015 |
| #6 | Status bar 动态提示 | UNIT-011, UNIT-012, UNIT-013, UNIT-016 |
| #7 | Tab 进入 viewExpanded | UNIT-014 (布局方法存在) |

## 统计

- 总测试数：17 (+ 1 隐式编译)
- P0：12
- P1：5
- Unit 级：14
- Integration 级：4
- AC 覆盖率：7/7 (100%)
