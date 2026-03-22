---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-04c-aggregate
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-22'
inputDocuments:
  - _bmad-output/implementation-artifacts/27-7-dashboard-intent-tree-integration.md
  - _bmad/tea/config.yaml
  - cmd/rnix/dashboard.go
  - ipc/protocol.go (IntentTreeWire, IntentNodeWire, IntentStatusResponse)
---

# ATDD Checklist: Story 27.7 — Dashboard 意图树集成

## 项目信息

- **Story**: 27.7 Dashboard Intent Tree Integration
- **Stack**: backend (Go)
- **测试框架**: Go standard testing
- **生成模式**: AI Generation
- **执行模式**: sequential

## TDD Red Phase (当前)

✅ Failing 测试已生成

- **测试文件**: `cmd/rnix/atdd_27_7_dashboard_intent_tree_test.go`
- **测试总数**: 22 个测试函数
- **RED 原因**: 引用未实现的类型/函数/字段导致编译失败

### 未实现的符号（编译器错误源）

| 符号 | 类型 | 说明 |
|------|------|------|
| `paneIntent` | const | 新窗格类型 = 4 |
| `intentFlatNode` | struct | 展平后的意图节点 |
| `flattenIntentTrees()` | func | DAG 展平算法 |
| `intentStateColor()` | func | 状态→颜色映射 |
| `renderIntentPane()` | method | 意图窗格渲染 |
| `intentTreesMsg` | type | IPC 消息类型 |
| `fetchIntentTreesCmd()` | func | IPC 异步获取 |
| `dashboardModel.intentTrees` | field | 意图树数据 |
| `dashboardModel.intentFlatNodes` | field | 展平节点列表 |
| `dashboardModel.intentCursor` | field | 导航光标 |
| `dashboardModel.intentTreeErr` | field | 错误状态 |

## 验收条件覆盖

| AC | 优先级 | 测试函数 | 覆盖内容 |
|----|--------|----------|----------|
| AC-1 | P0 | TestATDD_27_7_AC1_PaneIntentConstant | paneIntent=4 常量 |
| AC-1 | P0 | TestATDD_27_7_AC1_TabCycles5Panes | Tab 切换 5 窗格循环 |
| AC-1 | P0 | TestATDD_27_7_AC1_IntentPaneBorderHighlight | 边框高亮 |
| AC-1 | P1 | TestATDD_27_7_AC1_StatusBarIntentHelp | 帮助文本更新 |
| AC-2 | P0 | TestATDD_27_7_AC2_ModelHasIntentFields | 模型字段存在性 |
| AC-2 | P0 | TestATDD_27_7_AC2_IntentTreesMsgUpdatesModel | IPC 消息更新模型 |
| AC-2 | P0 | TestATDD_27_7_AC2_IntentTreesMsgError | 错误处理 |
| AC-2 | P0 | TestATDD_27_7_AC2_CursorClampedAfterRefresh | 刷新后光标越界修正 |
| AC-3 | P0 | TestATDD_27_7_AC3_FlattenIntentTrees_Topology | DAG 拓扑展开正确性 |
| AC-3 | P0 | TestATDD_27_7_AC3_FlattenIntentTrees_SortOrder | 同层节点排序 |
| AC-3 | P0 | TestATDD_27_7_AC3_IntentStateColor | 7 种状态颜色映射 |
| AC-3 | P0 | TestATDD_27_7_AC3_RenderIntentPane_NodeFormat | 节点渲染格式 |
| AC-3 | P0 | TestATDD_27_7_AC3_FlattenIntentTrees_MissingDeps | 缺失依赖容错 |
| AC-3 | P1 | TestATDD_27_7_AC3_EmptyNodesMap | 空 Nodes map |
| AC-3/6 | P1 | TestATDD_27_7_AC3_6_RenderMultipleTrees | 多树渲染 |
| AC-4 | P0 | TestATDD_27_7_AC4_JK_MovesIntentCursor | j/k 导航 |
| AC-4 | P0 | TestATDD_27_7_AC4_Enter_LinksToProcess | Enter PID 联动 |
| AC-4 | P0 | TestATDD_27_7_AC4_Enter_NoPID_ShowsMessage | PID=0 提示 |
| AC-4 | P0 | TestATDD_27_7_AC4_CursorBounds | 光标越界保护 |
| AC-5 | P1 | TestATDD_27_7_AC5_EmptyState_ShowsHint | 空状态提示 |
| AC-5 | P1 | TestATDD_27_7_AC5_EmptyState_NavigationSafe | 空状态导航安全 |
| AC-6 | P1 | TestATDD_27_7_AC6_MultipleTrees_Headers | 多树标题 |
| AC-6 | P1 | TestATDD_27_7_AC6_CrossTreeNavigation | 跨树导航 |
| AC-6 | P1 | TestATDD_27_7_AC6_FlattenPreservesTreeIndex | 树索引保持 |

### 优先级分布

- **P0**: 15 个测试（AC-1,2,3,4 核心功能）
- **P1**: 7 个测试（AC-5,6 边缘场景）
- **P2**: 0 个（AC-7 性能为 NFR，隐式覆盖）

## Next Steps (TDD Green Phase)

实现功能后：

1. 在 `cmd/rnix/dashboard.go` 中添加所有未实现的符号
2. 运行测试：`go test -run TestATDD_27_7 ./cmd/rnix/`
3. 验证所有 22 个测试通过（GREEN phase）
4. 如有测试失败：
   - 实现缺陷 → 修复实现
   - 测试缺陷 → 修复测试
5. 提交通过的测试

## 实现指引

### 需要添加的类型/函数

```go
// dashboard.go 新增
const paneIntent paneType = 4  // iota 序列扩展

type intentFlatNode struct {
    treeIndex    int
    nodeID       string
    indent       int
    node         *ipc.IntentNodeWire
    isTreeHeader bool
    treeWire     *ipc.IntentTreeWire
}

type intentTreesMsg struct {
    trees *ipc.IntentStatusResponse
    err   error
}

// dashboardModel 新增字段
intentTrees     []*ipc.IntentTreeWire
intentTreeErr   error
intentFlatNodes []intentFlatNode
intentCursor    int

// 新增函数
func flattenIntentTrees(trees []*ipc.IntentTreeWire) []intentFlatNode
func intentStateColor(state string) lipgloss.Color
func (m dashboardModel) renderIntentPane(width, height int) string
func fetchIntentTreesCmd() tea.Cmd
```

### 修改点

- Tab 切换: `% 4` → `% 5`
- View: paneIntent 分支渲染
- Update: intentTreesMsg 消息处理
- dashboardKey: paneIntent 下 j/k/Enter
- renderDashboardStatus: paneIntent help text

## 风险与假设

- **假设**: IntentList IPC 方法已稳定，返回格式不变
- **风险**: DAG 展平算法中循环依赖检测（当前测试未覆盖，依赖 intent 包自身保证）
- **假设**: RNIX_ASCII 降级模式已有通用机制，无需单独测试
