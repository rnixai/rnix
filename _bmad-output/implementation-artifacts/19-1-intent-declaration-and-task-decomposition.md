# Story 19.1: 意图声明与任务分解

Status: ready-for-dev

## Story

As a 应用开发者,
I want 通过 `rnix apply "高层意图"` 声明期望状态，系统自动分解为子意图树,
So that 我可以用自然语言描述目标而不需要手动编排。

## Acceptance Criteria

1. **Given** 用户执行 `rnix apply "我要一个完整的博客系统"`
   **When** 系统接收意图
   **Then** 系统将高层意图递归分解为子意图树（Intent Tree），每个子意图对应一个或多个智能体进程

2. **Given** 意图分解完成
   **When** 系统展示分解结果
   **Then** 显示子任务列表、依赖关系和执行计划，等待用户确认后开始执行

3. **Given** 用户确认执行计划
   **When** 系统开始执行
   **Then** 按 DAG 拓扑顺序调度子意图，无依赖的子意图并行执行，有依赖的等待上游完成

4. **Given** 用户执行 `rnix intent status`
   **When** 意图正在执行或已完成
   **Then** 显示意图树的当前状态：整体进度、各子意图完成度、执行中的智能体列表

5. **Given** `rnix apply` 使用 `--yes` 标志
   **When** 分解完成
   **Then** 跳过确认步骤，直接开始执行

6. **Given** 子意图执行失败
   **When** 系统检测到失败
   **Then** 停止依赖该子意图的下游任务，独立分支继续执行，最终汇报失败详情

## Tasks / Subtasks

### Task 1: Intent 数据模型（AC: #1, #4）

- [ ] 1.1 新建 `intent/` 包，创建 `intent/types.go`：

  ```go
  type IntentID string

  type IntentState string
  const (
      IntentPending     IntentState = "pending"
      IntentDecomposing IntentState = "decomposing"
      IntentAwaitConfirm IntentState = "await_confirm"
      IntentExecuting   IntentState = "executing"
      IntentCompleted   IntentState = "completed"
      IntentFailed      IntentState = "failed"
  )

  type IntentNode struct {
      ID          string      `json:"id" yaml:"id"`
      Intent      string      `json:"intent" yaml:"intent"`
      Agent       string      `json:"agent,omitempty" yaml:"agent,omitempty"`
      Model       string      `json:"model,omitempty" yaml:"model,omitempty"`
      DependsOn   []string    `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
      State       IntentState `json:"state" yaml:"state"`
      PID         types.PID   `json:"pid,omitempty" yaml:"pid,omitempty"`
      Result      string      `json:"result,omitempty" yaml:"result,omitempty"`
      Error       string      `json:"error,omitempty" yaml:"error,omitempty"`
      Children    []string    `json:"children,omitempty" yaml:"children,omitempty"`
  }

  type IntentTree struct {
      ID          IntentID               `json:"id" yaml:"id"`
      RootIntent  string                 `json:"root_intent" yaml:"root_intent"`
      State       IntentState            `json:"state" yaml:"state"`
      Nodes       map[string]*IntentNode `json:"nodes" yaml:"nodes"`
      CreatedAt   time.Time              `json:"created_at" yaml:"created_at"`
      CompletedAt *time.Time             `json:"completed_at,omitempty" yaml:"completed_at,omitempty"`
  }
  ```

- [ ] 1.2 实现 `IntentTree` 辅助方法：
  - `Progress() (completed, total int)` — 计算完成进度
  - `RunnableNodes() []*IntentNode` — 返回所有依赖已满足且状态为 pending 的节点
  - `MarkCompleted(nodeID, result string)` — 标记节点完成并检查下游
  - `MarkFailed(nodeID, errMsg string)` — 标记失败并级联标记依赖下游为 failed
  - `IsTerminal() bool` — 所有节点都为 completed 或 failed

### Task 2: Intent DAG 构建与拓扑排序（AC: #1, #3）

- [ ] 2.1 新建 `intent/dag.go`，复用 `compose/dag.go` 的设计模式：
  - `BuildIntentDAG(tree *IntentTree) (*DAG, error)` — 从 IntentTree 构建 DAG
  - `DetectCycle(dag *DAG) error` — 循环依赖检测
  - `TopologicalSort(dag *DAG) [][]string` — 分层拓扑排序（每层可并行）
  - DAG 类型可直接复用 `compose/dag.go` 中的 `DAG` 和 `DAGNode` 结构模式，但为 intent 包独立定义（避免跨包耦合）

### Task 3: LLM 意图分解器（AC: #1）

- [ ] 3.1 新建 `intent/decomposer.go`：

  ```go
  type Decomposer struct {
      llmDriver LLMCaller
  }

  type LLMCaller interface {
      Call(ctx context.Context, prompt string, model string) (string, error)
  }

  func (d *Decomposer) Decompose(ctx context.Context, intent string, model string) (*IntentTree, error)
  ```

- [ ] 3.2 实现分解逻辑：
  - 构造分解 system prompt：要求 LLM 输出 JSON 格式的子任务列表，包含 `id`、`intent`、`agent`（可选）、`depends_on`
  - 调用 LLM（通过 `LLMCaller` 接口），解析返回的 JSON 为 `[]IntentNode`
  - 构建 `IntentTree`，运行 `DetectCycle` 验证无循环依赖
  - 分解 prompt 需要清晰指示 LLM 产出格式——参考 compose YAML 的 agent 依赖结构

- [ ] 3.3 定义分解 prompt 模板（嵌入为 Go 常量）：
  ```
  你是一个任务规划系统。请将以下高层意图分解为具体子任务。

  意图: {{intent}}

  要求:
  1. 每个子任务用 JSON 对象表示，包含 id（短标识符）、intent（具体任务描述）、depends_on（依赖的子任务 id 列表，无依赖则为空数组）
  2. 子任务粒度适中——每个子任务应能由单个智能体独立完成
  3. 正确声明依赖关系——有数据流依赖的任务必须声明 depends_on
  4. 返回纯 JSON 数组，不要包含其他文本

  示例输出:
  [
    {"id": "design", "intent": "设计数据模型和 API 接口", "depends_on": []},
    {"id": "backend", "intent": "实现后端 API 服务", "depends_on": ["design"]},
    {"id": "frontend", "intent": "实现前端界面", "depends_on": ["design"]},
    {"id": "test", "intent": "编写集成测试", "depends_on": ["backend", "frontend"]}
  ]
  ```

### Task 4: Intent 执行引擎（AC: #3, #6）

- [ ] 4.1 新建 `intent/engine.go`：

  ```go
  type Engine struct {
      tree      *IntentTree
      spawner   KernelSpawner
      mu        sync.Mutex
      callbacks EngineCallbacks
  }

  type KernelSpawner interface {
      SpawnIntent(ctx context.Context, node *IntentNode) (types.PID, error)
      Wait(pid types.PID) (kernel.ExitStatus, error)
  }

  type EngineCallbacks struct {
      OnNodeStart    func(nodeID string, pid types.PID)
      OnNodeComplete func(nodeID string, result string)
      OnNodeFailed   func(nodeID string, err string)
      OnProgress     func(completed, total int)
  }
  ```

- [ ] 4.2 实现 `Engine.Execute(ctx context.Context) error`：
  - 分层执行：获取 `TopologicalSort` 的每层，并行 Spawn 当前层所有 runnable 节点
  - 每个节点在 goroutine 中：Spawn → Wait → 根据退出状态 MarkCompleted/MarkFailed
  - 节点失败时：MarkFailed 级联标记下游，但不终止已启动的独立分支
  - 使用 `sync.WaitGroup` 等待每层完成后推进下一层
  - 所有节点终止后返回（成功或部分失败）

- [ ] 4.3 实现事件驱动的执行循环（非简单分层，而是更灵活的调度）：
  - 维护一个 runnable 队列——当节点完成时，立即检查并启动新的 runnable 节点
  - 使用 channel 收集完成/失败事件，主循环消费事件并推进状态
  - 这比严格分层更高效（不需要等整层完成才推进下一层）

### Task 5: IPC 协议扩展（AC: #1, #4）

- [ ] 5.1 在 `ipc/protocol.go` 新增：

  ```go
  MethodApplyIntent  Method = "apply_intent"
  MethodIntentStatus Method = "intent_status"

  type ApplyIntentRequest struct {
      Intent    string `json:"intent"`
      Model     string `json:"model,omitempty"`
      AutoStart bool   `json:"auto_start,omitempty"` // --yes flag
  }

  type ApplyIntentResponse struct {
      IntentID string      `json:"intent_id"`
      Tree     *IntentTree `json:"tree"`
  }

  type IntentConfirmRequest struct {
      IntentID string `json:"intent_id"`
      Confirm  bool   `json:"confirm"`
  }

  type IntentStatusRequest struct {
      IntentID string `json:"intent_id,omitempty"` // 为空则返回所有
  }

  type IntentStatusResponse struct {
      Intents []*IntentTree `json:"intents"`
  }
  ```

  注意：`IntentTree` 定义在 `intent/types.go` 中，IPC 层通过 JSON 序列化传递。如需避免 `ipc/` 导入 `intent/`，可在 protocol.go 中定义 wire types（纯 JSON 结构体），server.go 中做转换。

- [ ] 5.2 流式协议设计：
  - `apply_intent` 是流式方法（类似 `spawn`）——先返回分解结果，等待确认，然后流式报告执行进度
  - StreamEvent 类型扩展：
    ```go
    StreamEventIntentDecomposed  = "intent_decomposed"
    StreamEventIntentConfirmReq  = "intent_confirm_required"
    StreamEventIntentNodeStart   = "intent_node_start"
    StreamEventIntentNodeDone    = "intent_node_done"
    StreamEventIntentNodeFailed  = "intent_node_failed"
    StreamEventIntentProgress    = "intent_progress"
    StreamEventIntentComplete    = "intent_complete"
    ```

### Task 6: IPC Server 处理（AC: #1, #3, #4）

- [ ] 6.1 在 `ipc/server.go` 的 `handleConn` dispatch 中新增：
  ```go
  case MethodApplyIntent:
      s.handleApplyIntent(conn, req.Payload)
      return
  case MethodIntentStatus:
      s.handleIntentStatus(conn, req.Payload)
  ```

- [ ] 6.2 实现 `handleApplyIntent(conn net.Conn, payload json.RawMessage)`：
  - 解析 `ApplyIntentRequest`
  - 调用 `Decomposer.Decompose()` 分解意图
  - 发送 `StreamEventIntentDecomposed` 事件（包含完整 IntentTree）
  - 如果 `AutoStart` 为 false：发送 `StreamEventIntentConfirmReq`，等待客户端确认
  - 如果确认或 AutoStart：调用 `Engine.Execute()`，通过回调发送进度事件
  - 最后发送 `StreamEventIntentComplete`

- [ ] 6.3 实现 `handleIntentStatus(conn net.Conn, payload json.RawMessage)`：
  - 解析 `IntentStatusRequest`
  - 从 IntentManager 获取当前活跃的 IntentTree 列表
  - 返回 `IntentStatusResponse`

- [ ] 6.4 Server 需持有 `IntentManager` 引用：
  ```go
  type Server struct {
      // ... 现有字段 ...
      intentMgr *intent.Manager
  }
  ```

### Task 7: IntentManager — 意图生命周期管理（AC: #1, #4, #6）

- [ ] 7.1 新建 `intent/manager.go`：

  ```go
  type Manager struct {
      mu          sync.RWMutex
      intents     map[IntentID]*IntentTree
      decomposer  *Decomposer
      spawner     KernelSpawner
      nextID      atomic.Uint64
  }

  func NewManager(decomposer *Decomposer, spawner KernelSpawner) *Manager
  func (m *Manager) Apply(ctx context.Context, req ApplyRequest) (*IntentTree, error)
  func (m *Manager) Confirm(intentID IntentID) error
  func (m *Manager) Execute(ctx context.Context, intentID IntentID, callbacks EngineCallbacks) error
  func (m *Manager) Status(intentID IntentID) (*IntentTree, error)
  func (m *Manager) ListActive() []*IntentTree
  ```

- [ ] 7.2 IntentID 生成：使用 `fmt.Sprintf("intent-%d", m.nextID.Add(1))` 格式，递增唯一

### Task 8: IPC Client 扩展（AC: #1, #4）

- [ ] 8.1 在 `ipc/client.go` 新增：
  ```go
  func (c *Client) ApplyIntentAndWatch(req ApplyIntentRequest, onEvent func(StreamEvent)) (*ApplyIntentResponse, error)
  func (c *Client) ConfirmIntent(intentID string, confirm bool) error
  func (c *Client) IntentStatus(intentID string) (*IntentStatusResponse, error)
  ```

- [ ] 8.2 `ApplyIntentAndWatch` 实现模式同 `SpawnAndWatch`：
  - `sendRequest(MethodApplyIntent, req)`
  - 读取初始响应
  - 循环读取 StreamEvent 并回调 onEvent
  - 当收到 `intent_confirm_required` 事件时，等待用户输入后调用 `ConfirmIntent`

### Task 9: CLI 命令实现（AC: #1, #2, #4, #5）

- [ ] 9.1 新建 `cmd/rnix/apply.go`：
  ```go
  var applyCmd = &cobra.Command{
      Use:   "apply <intent>",
      Short: "Declare intent and auto-decompose into sub-tasks",
      Args:  cobra.ExactArgs(1),
      RunE:  runApply,
  }
  ```
  - `--yes` / `-y` flag：跳过确认
  - `--model` / `-m` flag：指定分解使用的 LLM 模型（继承 root）
  - `--json` flag：JSON 输出模式（继承 root）

- [ ] 9.2 实现 `runApply`：
  - 连接 daemon（复用现有 `ensureDaemon` + `ipc.NewClient`）
  - 调用 `client.ApplyIntentAndWatch(req, onEvent)`
  - 收到 decomposed 事件：渲染子任务列表和依赖关系
  - 收到 confirm_required 事件：提示用户确认（非 `--yes` 模式）
  - 用户确认后发送 `ConfirmIntent`
  - 流式渲染执行进度（复用进度条/spinner 模式）
  - 最终汇总显示

- [ ] 9.3 新建 `cmd/rnix/intent.go`：
  ```go
  var intentCmd = &cobra.Command{
      Use:   "intent",
      Short: "Manage declarative intents",
  }
  var intentStatusCmd = &cobra.Command{
      Use:   "status [intent-id]",
      Short: "Show intent tree status",
      RunE:  runIntentStatus,
  }
  ```

- [ ] 9.4 实现 `runIntentStatus`：
  - 连接 daemon
  - 调用 `client.IntentStatus(intentID)`
  - 渲染意图树状态（树形视图 + 进度）

- [ ] 9.5 注册命令：
  ```go
  func init() {
      applyCmd.Flags().BoolVarP(&flagAutoStart, "yes", "y", false, "Skip confirmation")
      rootCmd.AddCommand(applyCmd)
      intentCmd.AddCommand(intentStatusCmd)
      rootCmd.AddCommand(intentCmd)
  }
  ```

### Task 10: UI 渲染组件（AC: #2, #4）

- [ ] 10.1 新建 `internal/ui/intent.go`：
  - `RenderIntentTree(tree *IntentTreeWire, mode OutputMode)` — 渲染意图树（树形缩进 + 状态颜色）
  - `RenderIntentProgress(completed, total int, mode OutputMode)` — 进度条
  - `RenderIntentNodeEvent(event StreamEvent, mode OutputMode)` — 节点事件实时显示
  - 样式：pending=灰色, executing=蓝色/spinner, completed=绿色✓, failed=红色✗

### Task 11: LLMCaller 适配（AC: #1）

- [ ] 11.1 在 `intent/decomposer.go` 中定义 `LLMCaller` 接口
- [ ] 11.2 在 `ipc/server.go` 中实现适配——通过 kernel 的 VFS 访问 `/dev/llm/claude`：
  - 打开 LLM 设备文件
  - 写入分解 prompt
  - 读取响应
  - 关闭文件

  或者更直接地：使用 `drivers/llm` 包中的 `ClaudeCliDriver` 创建独立的 LLM 调用（不走 VFS）。推荐前者（通过 VFS），保持架构一致性。

  但注意：Decomposer 的 LLM 调用不属于任何进程上下文，不需要走完整的 reasonStep 循环。它只需要一次性 prompt→response。因此：
  - 方案 A（推荐）：在 Server 层直接调用 `exec.CommandContext("claude", "-p", prompt, "--output-format", "json", "--model", model)` — 最简单，与其他 LLM 调用一致
  - 方案 B：创建一个临时 Process 来做分解 — 过度设计

  选择方案 A：`LLMCaller` 实现为直接调用 Claude Code CLI。

- [ ] 11.3 实现 `CLICaller`：
  ```go
  type CLICaller struct{}

  func (c *CLICaller) Call(ctx context.Context, prompt string, model string) (string, error) {
      args := []string{"-p", prompt, "--output-format", "json"}
      if model != "" {
          args = append(args, "--model", model)
      }
      cmd := exec.CommandContext(ctx, "claude", args...)
      out, err := cmd.Output()
      // 解析 JSON 响应，提取 result 文本
      return extractResult(out), err
  }
  ```

### Task 12: Server/Daemon 初始化集成（AC: #1）

- [ ] 12.1 在 `ipc/server.go` 的 `NewServer` 或 `Server` 构造中添加 `IntentManager` 注入
- [ ] 12.2 在 `cmd/rnix/main.go` 的 daemon 启动路径中创建 `IntentManager` 并注入 Server：
  ```go
  decomposer := intent.NewDecomposer(&intent.CLICaller{})
  intentMgr := intent.NewManager(decomposer, kernelSpawner)
  server.SetIntentManager(intentMgr)
  ```

### Task 13: 测试（AC: #1-#6）

- [ ] 13.1 `intent/types_test.go`：
  - `TestIntentTree_Progress` — 进度计算正确
  - `TestIntentTree_RunnableNodes` — 正确返回无依赖且 pending 的节点
  - `TestIntentTree_MarkCompleted` — 标记完成后状态正确
  - `TestIntentTree_MarkFailed` — 失败级联到下游
  - `TestIntentTree_IsTerminal` — 所有节点终止时返回 true

- [ ] 13.2 `intent/dag_test.go`：
  - `TestBuildIntentDAG_Basic` — 基本 DAG 构建
  - `TestBuildIntentDAG_CycleDetection` — 循环依赖报错
  - `TestTopologicalSort_Layers` — 分层排序正确
  - `TestTopologicalSort_ParallelNodes` — 无依赖节点在同一层

- [ ] 13.3 `intent/decomposer_test.go`：
  - `TestDecomposer_Decompose_Success` — mock LLM 返回有效 JSON，成功构建 IntentTree
  - `TestDecomposer_Decompose_InvalidJSON` — LLM 返回无效 JSON 时报错
  - `TestDecomposer_Decompose_CyclicDeps` — LLM 返回的依赖有循环时报错
  - `TestDecomposer_Decompose_EmptyResult` — LLM 返回空结果时报错
  - `TestDecomposer_Decompose_Timeout` — LLM 调用超时处理

- [ ] 13.4 `intent/engine_test.go`：
  - `TestEngine_Execute_Sequential` — 有序依赖的串行执行
  - `TestEngine_Execute_Parallel` — 无依赖节点并行执行
  - `TestEngine_Execute_PartialFailure` — 节点失败不终止独立分支
  - `TestEngine_Execute_CascadeFailure` — 失败级联到下游
  - `TestEngine_Execute_AllSuccess` — 全部成功完成
  - `TestEngine_Execute_ContextCancel` — ctx 取消时正确停止

- [ ] 13.5 `intent/manager_test.go`：
  - `TestManager_Apply` — 创建意图并分解
  - `TestManager_Confirm` — 确认后进入执行状态
  - `TestManager_Status` — 查询意图状态
  - `TestManager_ListActive` — 列出所有活跃意图

- [ ] 13.6 `cmd/rnix/apply_test.go`：
  - `TestApplyCmd_Registered` — `apply` 子命令已注册
  - `TestApplyCmd_NoArgs` — 无参数报错
  - `TestApplyCmd_YesFlag` — `--yes` flag 正确解析
  - `TestApplyCmd_UsageAndDescription` — Use 和 Short 正确

- [ ] 13.7 `cmd/rnix/intent_test.go`：
  - `TestIntentCmd_Registered` — `intent` 子命令已注册
  - `TestIntentStatusCmd_Registered` — `intent status` 子命令已注册
  - `TestIntentStatusCmd_UsageAndDescription` — Use 和 Short 正确

- [ ] 13.8 `internal/ui/intent_test.go`：
  - `TestRenderIntentTree_TTY` — TTY 模式渲染正确（含颜色和树形缩进）
  - `TestRenderIntentTree_JSON` — JSON 模式输出正确
  - `TestRenderIntentProgress` — 进度显示正确

- [ ] 13.9 竞态测试：`go test -race ./intent/... ./ipc/... ./cmd/rnix/...`

## Dev Notes

### 关键架构约束

- **FR106**：用户通过 `rnix apply "高层意图"` 声明期望状态，系统自动分解为子任务并分配给智能体执行
- **FR110**：系统将高层意图递归分解为子意图树（Intent Tree），每个子意图对应一个或多个智能体进程，父意图完成取决于所有子意图达成
- **FR111**：`rnix intent status` 查看意图树当前状态
- **NFR40**：Reconciler 从检测到 drift 到启动调和行动延迟 ≤ 5s（本 Story 不实现 Reconciler，但数据模型需为 19-2 预留）
- **架构决策延迟项**："声明式意图 Reconciler 的具体事件驱动框架选型"——本 Story 不需选型，但 IntentTree 数据模型需支持 Desired/Current/Drift 三态（为 Story 19-2 预留字段即可，本 Story 不实现）

### Intent 包设计原则

**新建 `intent/` 包**，不放入现有 `kernel/` 或 `compose/` 中：
- 意图分解是独立领域——有自己的数据模型（IntentTree）、执行引擎（Engine）、LLM 交互（Decomposer）
- 与 `compose/` 的关系：复用 DAG 调度的设计模式，但不直接依赖 compose 包。两者都使用类似的 DAG 拓扑排序，但 intent 是动态 LLM 生成的，compose 是静态 YAML 声明的
- 依赖方向：`intent/` → `internal/types/`（PID 等共享类型），`ipc/` → `intent/`（server 调用 manager），`cmd/` → `ipc/`（CLI 通过 IPC 通信）
- **禁止**：`intent/` 不导入 `kernel/`、`cmd/`、`ipc/`

### Decomposer 的 LLM 调用策略

分解不属于任何进程上下文——这是一次独立的 LLM 调用（prompt → structured response），不走 reasonStep 循环。

```
rnix apply "我要一个完整的博客系统"
  ↓
CLI → IPC(apply_intent) → Server.handleApplyIntent
  ↓
Decomposer.Decompose(ctx, intent, model)
  ↓
exec.CommandContext("claude", "-p", decompose_prompt, "--output-format", "json", "--model", model)
  ↓
解析 JSON → []IntentNode → BuildIntentDAG → DetectCycle → IntentTree
  ↓
Stream: intent_decomposed → intent_confirm_required
  ↓
用户确认 → Engine.Execute()
  ↓
TopologicalSort → 按层并行 Spawn → Stream: node_start/done/failed
```

**Claude Code CLI 调用参数**：
- 不传 `--max-turns`（让 CLI 使用默认值，分解是单轮 prompt）
- `--output-format json`（获取结构化 JSON 响应）
- `-p` 传递分解 prompt（包含原始意图 + 格式指示）
- `--model` 可选（用户通过 `--model` flag 指定或使用默认）

**JSON 响应解析**：Claude Code CLI `--output-format json` 返回形如：
```json
{"type": "result", "result": "...", "cost_usd": 0.01, ...}
```
需要解析 `.result` 字段，再从中提取子任务 JSON 数组。

### Intent 执行引擎 vs Compose Engine

| 维度 | Compose Engine | Intent Engine |
|------|---------------|---------------|
| 任务来源 | 静态 YAML | LLM 动态分解 |
| Agent 指定 | YAML 显式声明 | LLM 建议或默认 |
| 确认流程 | 无（直接执行）| 需用户确认 |
| 失败策略 | fail-all / fail-fast | 独立分支继续（类 fail-fast）|
| DAG 构建 | 编译时 | 运行时（分解后）|
| Reconciler | 无 | 19-2 实现 |

两者共享设计模式：
- DAG 拓扑排序 + 分层并行执行
- `KernelSpawner` 接口（Spawn + Wait）
- StreamEvent 进度报告

### IPC 流式协议设计

`apply_intent` 的通信时序：

```
Client                          Server
  │ ──── Request(apply_intent) ──→ │
  │                                 │ Decomposer.Decompose()
  │ ←── StreamEvent(decomposed) ── │
  │ ←── StreamEvent(confirm_req) ─ │
  │                                 │ [等待确认]
  │ ──── Request(confirm, true) ──→ │
  │                                 │ Engine.Execute()
  │ ←── StreamEvent(node_start) ── │
  │ ←── StreamEvent(node_done) ─── │
  │ ←── StreamEvent(progress) ──── │
  │ ...                             │
  │ ←── StreamEvent(complete) ──── │
```

**确认机制**：在流式连接中，Server 发送 `confirm_required` 后进入等待状态。Client 在同一连接上发送确认请求（或 auto_start 跳过此步）。实现方式：Server 的 `handleApplyIntent` 在发送 `confirm_required` 后，从 conn 读取下一个 Request（即确认请求）。

### KernelSpawner 适配

Intent Engine 的 `KernelSpawner` 接口需要将 IntentNode 转换为 kernel.Spawn 调用：
- `node.Intent` → spawn intent 参数
- `node.Agent` → AgentInfo（通过 AgentLoader 加载，或使用默认 Agent）
- `node.Model` → SpawnOpts.Model（可选覆盖）
- 上游节点的 result → 注入到当前节点的上下文（类似 compose 的 `buildUpstreamPrompt`）

实现位于 `ipc/server.go`（与 compose 类似，server 层做适配，因为 server 持有 kernel 引用）。

### 为 Story 19-2 预留的扩展点

IntentTree 数据模型预留但本 Story 不实现：
- `Desired`/`Current`/`Drift` 三态字段 → 不添加，19-2 时扩展
- Reconciler 调和循环 → 不实现
- 事件驱动重试 → 不实现

IntentNode 预留但本 Story 不实现：
- 重试计数和策略 → 不添加
- 超时配置 → 不添加

### 为 Story 19-3 预留的扩展点

- 增量更新（`rnix apply "加上评论功能"`）→ 本 Story 每次 `apply` 创建新的 IntentTree，不支持增量
- `rnix intent status` 在本 Story 实现基本功能

### 现有代码模式（必须遵循）

**IPC Method 注册模式** — 参考 `protocol.go` 中 `MethodSpawn`、`MethodCompose*`：
- 常量定义：`MethodApplyIntent Method = "apply_intent"`
- Request/Response 类型：`ApplyIntentRequest`、`ApplyIntentResponse`
- snake_case method 名（IPC 命名规范）

**流式 IPC Handler 模式** — 参考 `server.go` 的 `handleSpawn`：
- handler 方法签名：`func (s *Server) handleApplyIntent(conn net.Conn, payload json.RawMessage)`
- handler 返回后连接由调用方关闭（`handleConn` 中 `return`）
- 进度通过 `writeStreamEvent(conn, StreamEvent{...})` 发送

**CLI 命令模式** — 参考 `cmd/rnix/compose.go`：
- 独立文件定义 command
- `init()` 中 `rootCmd.AddCommand()`
- 连接 daemon、调用 IPC client、处理 stream events
- `--json` flag 通过 `flagJSON` 全局变量切换输出模式

**测试模式** — 参考 `compose/engine_test.go`、`compose/dag_test.go`：
- mock `KernelSpawner` 实现
- 测试 DAG 构建、循环检测、拓扑排序
- 测试执行引擎的并行和串行场景

**错误处理** — 遵循项目规范：
- 函数返回 `error` 而非 `*SyscallError`（Intent 不是 syscall，是上层编排）
- 错误消息包含上下文信息（intent ID、node ID）
- `fmt.Errorf("intent %s: node %s: %w", intentID, nodeID, err)` 格式

### 与 Epic 18 的关系

- Epic 18 完成了 AgentShell 完整脚本语言（for/while/fn/parallel/source/run）
- Intent Engine 不依赖 AgentShell——它是独立的声明式编排层
- 但未来可能的扩展：intent 分解可以生成 AgentShell 脚本而非直接 Spawn（本 Story 不实现）

### Git Intelligence

最近提交（Epic 18 Story 18.5 完成）：
- `64ca3fb` feat: add manual verification guide for Epic 17
- `0957382` feat: add retrospective for Epic 18
- `8cb4811` feat: update traceability matrix for Story 18.5
- `feb42a8` feat: cr Story 18.5
- `678d6f0` feat: ds 18-5

Epic 18 Code Review 修复模式：
- 补充缺失测试（如 `TestRunCmd_ShebangStripped`）
- 修正过时注释
- 新增边界测试

本 Story 应确保：
- 所有 JSON 字段使用 `snake_case`
- 新增包 `intent/` 遵循依赖方向规则（不导入 `kernel/`、`cmd/`、`ipc/`）
- `LLMCaller` 接口抽象确保 Decomposer 可测试
- IPC method 名称使用 snake_case（`apply_intent`、`intent_status`）
- CLI 命令遵循 Cobra 注册模式
- 所有并发结构通过 `-race` 测试
- DAG 相关代码参考 `compose/dag.go` 的模式但独立实现
- IntentTree 上的并发访问通过 `sync.Mutex` 保护
- 错误消息包含足够上下文（intent ID、node ID、失败原因）

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| rnix apply | IPC daemon | 流式连接，发送 apply_intent + 接收事件 | 是 |
| intent decompose | Claude Code CLI | exec.CommandContext 一次性调用 | 是 |
| intent execute | kernel Spawn | 通过 KernelSpawner 接口调度子进程 | 是 |
| intent execute | kernel Wait | 等待子进程完成获取退出状态 | 是 |
| intent DAG | compose DAG | 设计模式复用（独立实现），不直接依赖 | 否（独立包） |
| intent status | IPC protocol | 非流式请求-响应 | 是 |
| intent progress | StreamEvent | 复用现有 StreamEvent 机制 | 是 |
| intent confirm | IPC 流式连接 | 在同一流式连接中嵌套请求-响应 | 是 |
| intent UI | internal/ui | 新增 intent.go 渲染组件 | 是 |
| rnix apply --yes | auto_start 字段 | 跳过确认步骤 | 是 |
| rnix intent status | IntentManager | 查询活跃意图状态 | 是 |
| intent node failure | downstream cascade | MarkFailed 级联标记 | 是 |
| intent engine | context.Context | ctx 取消时正确停止所有 goroutine | 是 |

### 不支持的特性（本 Story 范围外）

- **Reconciler**：不实现事件驱动调和循环（Story 19-2）
- **增量更新**：不支持 `rnix apply "加上评论功能"` 修改现有意图（Story 19-3）
- **Desired/Current/Drift**：数据模型不包含三态字段（Story 19-2 扩展）
- **递归分解**：本 Story 仅支持一级分解（高层意图 → 子任务），不支持子任务再分解
- **自动 Agent 选择**：LLM 可以建议 agent 名称，但如果不匹配已安装 Agent 则使用默认
- **意图持久化**：IntentTree 仅存在于内存，进程退出后丢失（未来可扩展持久化到 `$PROJECT/.rnix/intents/`）

### Project Structure Notes

新增文件：
- `intent/types.go` — IntentTree、IntentNode、IntentState 类型
- `intent/dag.go` — Intent DAG 构建和拓扑排序
- `intent/decomposer.go` — LLM 意图分解器 + LLMCaller 接口 + CLICaller
- `intent/engine.go` — 意图执行引擎
- `intent/manager.go` — 意图生命周期管理
- `intent/types_test.go` — 类型测试
- `intent/dag_test.go` — DAG 测试
- `intent/decomposer_test.go` — 分解器测试
- `intent/engine_test.go` — 引擎测试
- `intent/manager_test.go` — Manager 测试
- `cmd/rnix/apply.go` — `rnix apply` CLI 命令
- `cmd/rnix/apply_test.go` — CLI 测试
- `cmd/rnix/intent.go` — `rnix intent status` CLI 命令
- `cmd/rnix/intent_test.go` — CLI 测试
- `internal/ui/intent.go` — 意图渲染组件
- `internal/ui/intent_test.go` — UI 测试

修改文件：
- `ipc/protocol.go` — 新增 MethodApplyIntent、MethodIntentStatus、Request/Response 类型、StreamEvent 类型
- `ipc/server.go` — 新增 handleApplyIntent、handleIntentStatus、intentMgr 字段
- `ipc/client.go` — 新增 ApplyIntentAndWatch、ConfirmIntent、IntentStatus
- `cmd/rnix/main.go` — daemon 路径新增 IntentManager 初始化和注入

不涉及 `kernel/`（内核不感知 intent 概念）、`vfs/`、`drivers/`、`agents/`（除通过 server 层间接调用 AgentLoader）、`skills/`、`shell/`、`compose/`。

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-19-声明式意图与自动规划-declarative-intent-auto-planning.md#Story 19.1]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR106, FR110, FR111]
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR40]
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#延迟决策 - 声明式意图 Reconciler]
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#IPC Method 命名规范]
- [Source: _bmad-output/planning-artifacts/architecture/project-structure-boundaries.md]
- [Source: _bmad-output/project-context.md]
- [Source: _bmad-output/implementation-artifacts/18-5-modularization-and-script-execution.md]
- [Source: compose/engine.go — Engine.Execute/executeNode/buildUpstreamPrompt]
- [Source: compose/dag.go — BuildDAG/DetectCycle/TopologicalSort]
- [Source: compose/types.go — ComposeSpec/AgentSpec/DAG/DAGNode/KernelSpawner]
- [Source: ipc/protocol.go — Method 常量/Request-Response 类型/StreamEvent]
- [Source: ipc/server.go — handleConn dispatch/handleSpawn 流式模式/handleCompose*]
- [Source: ipc/client.go — SpawnAndWatch/call/sendRequest 模式]
- [Source: cmd/rnix/compose.go — Cobra 子命令注册模式]
- [Source: cmd/rnix/main.go — daemon 初始化/ensureDaemon/runRoot]
- [Source: agents/loader.go — AgentLoader.Load]
- [Source: agents/types.go — AgentInfo/AgentManifest]
- [Source: internal/ui/compose.go — Compose 状态渲染模式]

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
