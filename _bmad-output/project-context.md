---
project_name: 'Rnix'
user_name: 'Decker'
date: '2026-03-14'
sections_completed: ['technology_stack', 'language_rules', 'framework_rules', 'testing_rules', 'code_quality', 'workflow_rules', 'critical_rules']
status: 'complete'
rule_count: 142
optimized_for_llm: true
---

# Project Context for AI Agents

_This file contains critical rules and patterns that AI agents must follow when implementing code in this project. Focus on unobvious details that agents might otherwise miss._

---

## Technology Stack & Versions

- **Go 1.26** — 模块路径 `github.com/rnixai/rnix`，单二进制 `cmd/rnix/main.go` 入口
- **Go 1.26 新特性要求**：优先使用 `new(expr)` 初始化结构体、自引用泛型约束、利用 Goroutine Leak Profiler 验证资源释放
- **CLI 框架**：Cobra v1.10.2（`github.com/spf13/cobra`）
- **终端样式**：Charm 生态（lipgloss v1.1.0 + bubbletea v2.0.0），MVP 仅用 cobra + lipgloss
- **YAML 解析**：goccy/go-yaml v1.19.2（不用 gopkg.in/yaml.v3）
- **终端检测**：mattn/go-isatty v0.0.20 + mattn/go-runewidth v0.0.19
- **测试**：Go 标准 `testing`，默认 `-race` 竞态检测
- **Lint**：golangci-lint v2（errcheck, govet, staticcheck, unused, modernize）
- **构建**：`make all` = lint → vet → modernize-check → test → build
- **LLM 集成**：三种驱动类型 — `claude-cli`、`cursor-cli`、`openai-compat`
- **YAML 后缀**：统一使用 `.yaml`（不用 `.yml`）
- **包总数**：20 个 Go 包

## Critical Implementation Rules

### Go 语言特定规则

- **泛型必用场景**：Registry、SyncMap、Future、Result、JSONResponse、LoadYAML 必须用泛型实现，减少 `interface{}` 和类型断言
- **泛型禁用场景**：Kernel 接口（方法签名固定）、Process 结构体（单一具体类型）、SyscallEvent（需运行时灵活性）
- **泛型命名**：领域类型用语义参数名（`Registry[Item]`、`SyncMap[K, V]`），通用工具允许 `T`
- **错误处理**：所有 syscall 必须返回 `*kernel.SyscallError`（含 Syscall/PID/Device/Err/Code），禁止返回裸 `error`
- **Driver 层错误**：drivers/ 包使用 `types.DriverError`（含 Op/Device/Err/Code），避免 drivers/ → kernel/ 循环依赖
- **错误码**：`ErrTimeout` / `ErrNotFound` / `ErrPermission` / `ErrInternal` / `ErrDriver` / `ErrInvalid` / `ErrBrokenPipe` / `ErrServiceUnavailable` / `ErrAlreadyMounted`，类型为 `ErrCode string`
- **`errors.Unwrap` 支持**：SyscallError 和 DriverError 必须实现 `Unwrap() error`
- **PID 分配**：`atomic.AddUint64` 全局递增，不回收
- **并发保护**：进程表用 `SyncMap[PID, *Process]`，设备注册表用 `Registry[T]`
- **goroutine 生命周期**：每进程一个 `context.Context` + `sync.WaitGroup`，退出时严格按顺序释放资源
- **context.Context 传播**：Kernel 方法不接受 ctx 参数（用 Process.cancel 控制），Driver 方法必须接受 ctx 参数支持取消
- **外部命令调用**：必须使用 `exec.CommandContext`，ctx 必须有 deadline
- **模块路径**：`github.com/rnixai/rnix`

#### 导入别名约定（强制）

以下导入别名是项目约定，所有文件必须一致使用：

- `rnixctx "github.com/rnixai/rnix/context"` — 避免与 `context` 标准库冲突
- `drivershell "github.com/rnixai/rnix/drivers/shell"` — 避免与 shell 包冲突
- `agentshell "github.com/rnixai/rnix/shell"` — 避免与 drivers/shell 冲突
- `gocontext "context"` — 仅在 kernel/ 包中使用，因为 kernel 已导入 `rnixctx`

#### 自定义类型（internal/types）

所有标识符必须使用自定义类型，禁止裸整数或字符串：

| 类型 | 底层类型 | 用途 |
|------|----------|------|
| `types.PID` | `uint64` | 进程标识符 |
| `types.FD` | `int` | 文件描述符 |
| `types.CtxID` | `uint64` | 上下文标识符 |
| `types.MsgSeq` | `uint64` | 消息序列号 |
| `types.PGID` | `uint64` | 进程组标识符 |
| `types.TID` | `uint64` | 线程标识符（进程内） |
| `types.CoID` | `uint64` | 协程标识符（进程内） |
| `types.TraceID` | `string` | 分布式追踪 ID |
| `types.SpanID` | `string` | 追踪 Span ID |
| `types.ErrCode` | `string` | 错误码 |
| `types.Signal` | `int` | 进程信号（SIGTERM=1...SIGRESUME=5） |
| `types.ProcessState` | `int` | 进程状态（Created=0...Dead=3） |

### 架构框架规则

#### Kernel 组合接口设计

KernelImpl 通过组合式子接口实现，不是单一巨型接口：

- `ProcessManager` — Spawn/Kill/Wait
- `MountManager` — Mount/Unmount/UnmountAll（MCP）
- `KernelCallbacks` — OnSpawn/OnStep/OnComplete/OnError（CLI 进度回调）
- `ServiceInitializer` — Name/Init（init 引导服务）

KernelImpl 内部持有的关键子系统（通过字段组合，非接口）：

| 字段 | 类型 | 职责 |
|------|------|------|
| `procTable` | `SyncMap[PID, *Process]` | 进程表 |
| `msgQueues` | `SyncMap[PID, *MessageQueue]` | IPC 消息队列 |
| `procGroups` | `SyncMap[PGID, *ProcGroup]` | 进程组 |
| `budgetPools` | `SyncMap[PGID, *BudgetPool]` | Token 预算池 |
| `slaResults` | `SyncMap[PGID, []*SLAResult]` | SLA 评估结果 |
| `immuneDaemon` | `*ImmuneDaemon` | 免疫系统守护进程 |
| `stemMatcher` | `*StemMatcher` | Stem 技能匹配 |
| `diffMemory` | `*DiffMemory` | 分化记忆 |
| `spanRecorder` | `*debug.SpanRecorder` | 分布式追踪 |
| `recordMgr` | `*debug.RecordManager` | 执行录制 |

#### VFS 设备模型

- **设备注册在 `cmd/rnix/main.go`**：依赖注入点，所有驱动在此创建和注册
- **VFS 路径约定**：`/proc/{pid}/` 动态进程信息、`/dev/llm/{provider}` LLM 驱动、`/dev/shell` Shell 驱动、`/dev/fs` 宿主文件系统、`/dev/mcp/*` MCP 服务
- **VFSFile 接口**：所有设备驱动必须实现 `Read/Write(ctx)/Close/Stat` 四个方法，Write 接受 `context.Context` 支持取消传播
- **FD 管理**：每进程独立 `FDTable map[FD]VFSFile`，FD 为进程内递增整数

#### 进程状态机

- **合法状态转移**：Created→Running（reasonStep 开始）、Running→Zombie（完成/错误/超时/kill）、Zombie→Dead（wait 回收）
- **非法转移绝对禁止**：Running→Created、Zombie→Running、Dead→任何状态
- **资源释放顺序**：cancel() → wg.Wait() → 关闭 FD → 关闭 DebugChan → CtxFree → 状态转 Dead → 移除进程表

#### 推理循环模式

两种推理模式通过 `SpawnOpts.ReasoningMode` 选择：

- **`""` (线性模式，默认)**：经典 reasonStep 循环，LLM 输出 → 解析 action → 执行 tool → 写入 context → 下一步
- **`"ooda"` (OODA 模式)**：4 阶段循环 Observe → Orient → Decide → Act
  - Observe: 收集当前状态信息
  - Orient: LLM 评估偏差与约束
  - Decide: LLM 输出结构化 JSON `OODADecision`（action: tool_call/spawn/complete/replan/specialize）
  - Act: 执行决策（tool 调用、子进程 spawn、完成、重新规划、技能热加载）
  - `OODADecision.Action = "specialize"` 触发 StemMatcher 动态加载技能

#### LLM 多 Provider 架构

```
rnix-providers.yaml → LoadProvidersConfig → CreateDriver → DriverRegistry
                                                              ↓
                                                    VFS: /dev/llm/{name}
```

- **三种驱动类型**：`claude-cli`（Claude Code CLI）、`cursor-cli`（Cursor CLI）、`openai-compat`（HTTP OpenAI API）
- **驱动接口**：`LLMDriver` = `Call(ctx, req)` + `Stream(ctx, req)` + `Info()`
- **可选接口**：`HealthChecker` = `HealthCheck(ctx)` — 驱动可选实现健康检查
- **DriverRegistry**：线程安全注册表，持有所有 LLM 驱动实例 + 健康状态
- **Provider 解析优先级**：CLI `--provider` > agent manifest `models.provider` > `default_provider` > `"claude"`
- **配置文件搜索路径**：CWD `rnix-providers.yaml` → `$XDG_CONFIG_HOME/rnix/rnix-providers.yaml`
- **Provider 名称约束**：必须匹配 `^[a-zA-Z0-9_-]+$`
- **VFS 路径映射**：provider name `foo` → `/dev/llm/foo`
- **Factory 模式**：`CreateDriver(ProviderConfig)` 按 driver 类型分发构造

#### OpenAI 兼容 HTTP 网关

- **入口**：`ipc.OpenAIServer`，绑定 `127.0.0.1:8080`
- **端点**：`POST /v1/chat/completions`、`GET /v1/models`、`GET /health`
- **路由**：请求中的 model 字段映射到 DriverRegistry 中的 provider

#### Intent 系统（声明式意图分解）

- **状态机**：`pending → decomposing → await_confirm → executing → completed/failed`，额外 `retrying` 状态
- **核心类型**：`IntentTree`（含 Nodes map + DesiredNodes + Drifts）、`IntentNode`（单个子任务）
- **DAG 执行**：`BuildIntentDAG` 验证依赖图无环，`RunnableNodes()` 返回依赖已满足的节点
- **Reconciler**：持续循环，检测漂移（DriftItem）、自动重试（MaxRetries）、超时处理
- **漂移类型**：`node_failed` / `node_timeout` / `new_requirement` / `node_modified`
- **失败级联**：`MarkFailed` 自动通过 `cascadeFailure` 递归标记所有下游依赖节点为 failed
- **增量意图**：`InjectNodes` 在运行时向执行中的 IntentTree 注入新节点
- **IPC 方法**：`apply_intent` / `intent_status` / `intent_confirm` / `apply_incremental_intent` / `intent_list`

#### Compose 多智能体编排

- **配置文件**：`rnix-compose.yaml`，顶层 `ComposeSpec`（version/intent/agents map）
- **DAG 调度**：`DAGNode` 含 `DependsOn` + `DependedBy`，调度器按拓扑序并行执行
- **KernelSpawner 接口**：Compose 通过此接口与 Kernel 交互（Spawn/Wait/GetProcessResult/GetSpanID/GetTokensUsed）
- **优先级**：`PriorityHigh=10` / `PriorityNormal=5` / `PriorityLow=1`，影响 BudgetPool 配额分配
- **SLA 集成**：每个 AgentSpec 可附加 `SLASpec`（max_tokens/max_duration_ms/output_format）
- **候选人自动选择**：AgentSpec.Candidates 列表 + ReputationStore 评分自动选择最优智能体

#### Token 经济系统

**BudgetPool（预算池）**：
- 按进程组（PGID）管理总 Token 预算
- `AllocateQuota` 按优先级权重比例分配配额：`quota = totalBudget * agentWeight / totalWeight`
- 每次新注册代理时重新计算所有配额（保持比例公平）
- `ConsumeTokens` 原子扣减，超出配额返回错误
- 线程安全：`sync.RWMutex`

**SLA 约束**：
- `SLASpec` 定义合约约束（max_tokens / max_duration_ms / output_format）
- `Evaluate` 方法逐项检查，返回 `SLAResult`（含所有 CheckResult + 总体 Passed）
- 非零约束才检查，零值跳过

**ReputationStore（声誉系统）**：
- 基于文件的 JSON Lines 持久化，路径 `$PROJECT/.rnix/reputation/{agent-name}.json`
- 每条记录为 `ReputationRecord`（SLAResult + Timestamp）
- `ReputationSummary` 聚合：TotalRuns/PassedRuns/SuccessRate/AvgTokens/AvgDuration/Score
- Score 计算：`SuccessRate * 100 - tokenPenalty - durationPenalty`
- 支持按 score 排序的 `RankedSummaries`

**SynergyMatrix（技能协同矩阵）**：
- 记录技能组合的历史表现数据
- `SynergyComboKey`：技能名称排序后逗号连接（确保 {A,B} = {B,A}）
- 持久化：`$PROJECT/.rnix/reputation/synergy-matrix.json`（JSON Lines）
- `ComboSummary` 聚合：TotalRuns/Passed/AvgTokens/AvgDuration/SuccessRate

#### 免疫系统（Adaptive Security）

**ImmuneDaemon**：
- 后台守护进程，持续监控智能体行为
- `BehaviorSample` 记录每次执行的 syscall 计数、设备访问、token 消耗、执行时长
- `NormalProfile` 从历史样本计算统计基线（均值 + 标准差），最少需要 `MinSamplesForProfile=5` 个样本
- 异常检测：偏差超过阈值（默认 3 倍标准差）触发 `AnomalyAlert`
- 威胁响应：异常进程自动 `SIGPAUSE`（暂停），可通过 `immune resume` 恢复
- 安全状态：`green` / `yellow` / `red`
- 持久化：行为样本 JSON Lines 文件

**CapabilitySimilarity（能力相似度）**：
- 基于技能集合的 Jaccard 相似度计算
- Supervisor 重启耗尽时，通过相似度查找替代智能体进行能力迁移

**CooperationTopology（协作拓扑）**：
- `TopologyNode`（智能体节点）+ `CooperationEdge`（协作关系边）
- 边包含成功/失败计数 + 权重，识别强化路径（`reinforced_paths`）

#### Stem Cell 分化系统

**StemMatcher（技能匹配）**：
- 基于关键词的意图→技能匹配：tokenize intent → overlap score with skill keywords
- 返回按相关性降序排列的技能名称列表

**DiffMemory（分化记忆）**：
- 缓存 intent→skills 映射，加速重复意图的技能匹配
- LRU 驱逐策略：`maxSize` 限制，按 HitCount 最低 + Timestamp 最旧驱逐
- `normalizeIntent`：小写 + 去标点 + 排序单词 → 生成签名（确保语义等价的意图匹配）
- `Lookup` 递增 HitCount，`Record` 不递增（区分记录 vs 实际重用）

**Lineage（谱系追踪）**：
- 记录进程的完整分化历史：`LineageEvent`（phase/skills/trigger/fromMemory）
- `phase` 值：`"initial"` 或 `"progressive"`
- `Events()` 返回深拷贝（防止调用者修改）
- IPC 方法：`lineage`

#### Supervisor 树

- **重启策略**：`one_for_one`（仅重启失败子进程）/ `one_for_all`（全部重启）/ `rest_for_one`（失败及其后的全部重启）
- **子进程重启策略**：`permanent`（总是重启）/ `transient`（非正常退出才重启）/ `temporary`（不重启）
- **速率限制**：`MaxRestarts` 在 `MaxWindow` 时间窗口内，超出则 Supervisor 本身失败
- **Init 引导**：`rnix-init.yaml` 定义 services（skill_registry/mcp_manager/log_aggregator）+ supervisors 树

#### AgentTest 框架（agtest）

- **声明式测试**：YAML 定义测试用例（intent/agent/assert）
- **三种断言**：`OutputAssert`（contains/not_contains）、`SyscallAssert`（includes/excludes）、`QualityAssert`（LLM judge criteria）
- **测试套件**：`TestSuiteSpec` 支持单文件和多文件组织
- **LLM Judge**：轻量级 LLM 评估输出质量

#### Skill 包管理（skillpkg）

- **注册表文件**：`lib/skills/.registry.yaml` — 本地已安装技能的元数据
- **社区注册表**：`https://registry.rnix.ai`（远程 skill 索引）
- **操作**：Install（下载+解压+注册）、Search（远程搜索）、Update（版本比较+升级）、List（合并本地+内置）
- **SkillPackage**：name/version/checksum(`sha256:<hex>`)/data(tar.gz bytes)
- **来源标记**：`"builtin"` 或 `"community"`

#### 依赖方向（严格单向）

```
cmd/rnix           → ipc, kernel, drivers/*, compose, intent, shell, agents, skills, skillpkg, agtest, debug
├── ipc            → kernel, drivers/llm, vfs, internal/*
├── kernel         → agents, skills, context, debug, vfs, internal/*
│   ├── vfs        → internal/types
│   ├── context    → internal/types
│   └── debug      → internal/types
├── compose        → agents, kernel, internal/types
├── intent         → internal/types
├── shell          → (独立，无外部依赖)
├── agents         → skills, internal/types
├── skills         → internal/types
├── skillpkg       → skills
├── agtest         → (独立)
└── drivers/*      → vfs, internal/types (绝不导入 kernel/)
```

**绝对禁止的导入方向**：
- kernel/ 不导入 cmd/
- vfs/ 不导入 kernel/
- drivers/ 不导入 kernel/（使用 `types.DriverError` 替代 `kernel.SyscallError`）
- agents/ 不导入 kernel/
- 任何包不导入 cmd/rnix/

### 测试规则

- **测试文件位置**：与源文件同目录（Go 惯例），如 `kernel/errors_test.go` 对应 `kernel/errors.go`
- **测试函数命名**：`Test<Type>_<Method>`（如 `TestRegistry_RegisterAndGet`、`TestSyscallError_Unwrap`）
- **ATDD 测试文件**：`atdd_{story_id}_{description}_test.go`（如 `atdd_21_1_budget_pool_test.go`）— 验收驱动开发测试
- **集成测试文件**：`{feature}_integration_test.go`（如 `stem_integration_test.go`）
- **竞态检测**：`make test` 默认启用 `-race`，所有并发数据结构必须通过竞态测试
- **并发测试模式**：使用 `sync.WaitGroup` + 多 goroutine 并发访问，验证线程安全
- **Mock 策略**：通过接口抽象实现可测试性——`LLMDriver`、`VFSFile`、`KernelSpawner`、`DeviceRegisterer` 等接口允许注入 mock 实现
- **工厂函数可测试性**：如 `NewStemMatcherFromFunc` 接受自定义 discovery 函数，用于测试注入
- **Driver 测试**：`exec.Command` 通过注入可替换的 command builder 来 mock，不依赖真实 CLI
- **测试 fixtures**：放在 `testdata/` 子目录（如 `skills/testdata/mock-skill/`）
- **覆盖率**：核心路径（状态机转移、错误传播、syscall 入口/出口）必须有测试覆盖
- **JSON Lines 测试**：持久化文件（reputation、synergy-matrix、immune samples）使用 JSON Lines 格式，测试时验证逐行 json.Unmarshal

### 代码质量与风格规则

#### 命名约定

- **包名**：全小写单词，不用下划线（`kernel`、`vfs`、`context`）
- **导出类型**：PascalCase（`Process`、`SyscallEvent`、`LLMDriver`）
- **非导出类型**：camelCase（`pidCounter`、`fdTable`）
- **接口**：名词或 `-er` 后缀（`FileSystem`、`Debugger`、`LLMDriver`、`HealthChecker`），禁止 `I` 前缀
- **常量**：PascalCase 导出 / camelCase 非导出，错误变量 `Err` 前缀（`ErrNotFound`），禁止全大写下划线（不用 `MAX_TOKENS`）
- **Syscall 命名**：PascalCase 动词（`Spawn`、`Kill`）、`Ctx` 前缀（`CtxAlloc`）、Unix 风格（`Open`、`Read`）
- **VFS 路径**：全小写 Unix 风格，连字符分隔（`/dev/llm/claude`、`/lib/skills/code-analyst/`）
- **Go 源文件**：全小写下划线分隔（`claude_cli.go`、`kernel_test.go`、`budget_pool.go`、`synergy_matrix.go`）
- **方法接收器**：简短单字母（`r *Registry`、`m *SyncMap`、`k *KernelImpl`、`bp *BudgetPool`）
- **Option 模式**：`WithXxx` 函数名（`WithModel`、`WithAPIKey`、`WithCompatModel`），类型为 `XxxOption func(*xxx)`
- **Wire 类型**：IPC 传输类型以 `Wire` 后缀命名（`ProcInfoWire`、`IntentTreeWire`、`AlertWire`）

#### 文件组织

- **每文件单一职责**：`kernel.go` = Kernel + Spawn + reasonStep，`process.go` = Process + 状态机
- **子系统独立文件**：`budget_pool.go`、`sla.go`、`reputation.go`、`synergy_matrix.go`、`immune.go`、`stem.go`、`diffmemory.go`、`lineage.go`、`ooda.go`、`supervisor.go`、`init.go`
- **接口定义在使用方**：`LLMDriver` 定义在 `drivers/llm/driver.go`，`KernelSpawner` 定义在 `compose/types.go`
- **共享类型独立文件**：PID/FD/ErrCode 等放在 `internal/types/types.go`
- **内部包隔离**：UI 组件放 `internal/ui/`，泛型工具放 `internal/xsync/`

#### 输出格式

- **JSON 字段命名**：全部 `snake_case`（Go 用 PascalCase + json tag）
- **YAML 字段命名**：全部 `snake_case`（Go 用 PascalCase + yaml tag）
- **双 tag 模式**：需要同时支持 JSON 和 YAML 的类型加双 tag（如 IntentNode 的 `json:"id" yaml:"id"`）
- **时间格式**：JSON 用毫秒整数（`elapsed_ms: 6200`），终端用人类可读（`6.2s`），日志用 RFC3339
- **JSON Lines 持久化**：reputation、synergy-matrix、immune samples 使用 JSON Lines（每行一个 JSON 对象 + `\n`），用 `bufio.Scanner` + `json.Unmarshal` 逐行读取

#### 持久化数据目录

- **声誉数据**：`$PROJECT/.rnix/reputation/{agent-name}.json`（JSON Lines）
- **协同矩阵**：`$PROJECT/.rnix/reputation/synergy-matrix.json`（JSON Lines）
- **免疫样本**：`$PROJECT/.rnix/immune/{agent-template}.json`（JSON Lines）
- **本地注册表**：`lib/skills/.registry.yaml`

### Dev Notes 规范

#### 功能组合检查清单（Combination Matrix）

- **每个涉及跨模块交互的 Story 的 Dev Notes 必须包含"组合矩阵"段落**
- 列出本 Story 实现的功能与现有功能的交互点（正交性、共存行为、副作用）
- 对于"需验证"标记为"是"的交互，必须有对应的测试用例

### 上下文传播编码规范

#### Context 操作规则

- **运行时 Context 修改必须通过 `ctxMgr` 方法**：`AppendMessage`、`SetSystemPrompt`、`GetContextInfo` 等
- **gdb 运行时修改的生效时机**：所有通过 `set` 命令的修改在下一次 `reasonStep` 迭代时生效
- **Context 修改不可撤销**：`set context append` 永久改变上下文历史
- **Model override 的作用域**：`proc.gdbModelOverride` 影响 reasonStep 中的 `llmRequest.Model`，不修改 `opts.Model`
- **Skills 热加载方式**：OODA specialize action 或 gdb 技能通过 SkillLoader 加载 body 后追加到上下文

#### Context 传播方向

- **Kernel → Context Manager**：通过 `k.ctxMgr` 单向调用
- **IPC Server → Context Manager**：通过 `s.ctxMgr` 调用
- **禁止 Context Manager 反向调用 Kernel 或 IPC**

### IPC 扩展标准步骤

新增 IPC 方法时，必须按以下 4 步顺序实现：

1. **protocol.go** — 定义 Method 常量 + Request/Response 类型
2. **server.go** — 注册 handler（`dispatch` map）+ 实现 `handleXxx` 方法
3. **client.go** — 封装客户端调用方法
4. **cmd/rnix/xxx.go** — 编写 CLI 入口（Cobra command）

每步的产出文件独立可编译。跳步或合并步骤会增加错误定位难度。

当前已注册的 IPC 方法（34 个）：
`ping` / `spawn` / `list_procs` / `kill` / `attach_debug` / `attach_log` / `shutdown` / `spawn_pipeline` / `exec_script` / `attach_gdb` / `detach_gdb` / `gdb_command` / `record_start` / `record_stop` / `record_list` / `replay_load` / `fork_continue` / `ctx_profile` / `ctx_growth` / `apply_intent` / `intent_status` / `intent_confirm` / `apply_incremental_intent` / `intent_list` / `lineage` / `provider_status` / `budget_status` / `sla_status` / `reputation_status` / `synergy_list` / `immune_status` / `immune_resume` / `similarity_query` / `topology_query`

### CJK 字符处理检查

- **Code Review 必检项**：所有涉及字符串截断、对齐、终端表格渲染的代码，必须验证 CJK 字符处理正确性
- **关键规则**：截断按 rune 计数（不按 byte），终端对齐考虑 CJK 字符占 2 列宽

### 开发工作流规则

#### 构建与验证

- **质量门禁**：`make all` = lint → vet → modernize-check → test → build
- **编译目标**：`go build -o rnix ./cmd/rnix/`，单二进制输出
- **安装方式**：`go install ./cmd/rnix/`

#### Channel 使用规则

- **DebugChan 缓冲 256**：防止 syscall 阻塞在写入
- **Done 缓冲 1**：确保写入不阻塞
- **nil channel 检查**：写入前 `if ch != nil`，零开销跳过
- **关闭责任在生产者**：DebugChan 由进程退出时关闭

#### 配置文件体系

| 文件 | 用途 | 加载方式 |
|------|------|----------|
| `rnix-providers.yaml` | LLM 提供商定义 | `llm.LoadProvidersConfig` |
| `rnix-init.yaml` | 引导服务与 Supervisor 树 | `kernel.LoadInitConfig` |
| `rnix-compose.yaml` | 多智能体编排 DAG | `compose.LoadSpec` |
| `lib/agents/*/agent.yaml` | 智能体清单 | `agents.LoadAgent` |
| `lib/skills/*/SKILL.md` | 技能定义（YAML frontmatter + markdown body） | `skills.LoadSkill` |

### 关键防错规则

#### 反模式（绝对禁止）

- **禁止返回裸 error**：syscall 实现中必须包装为 `*SyscallError`；drivers/ 层使用 `*types.DriverError`
- **禁止反向依赖**：`vfs/` 导入 `kernel/`、`drivers/` 导入 `kernel/` 会产生循环依赖，通过接口解耦
- **禁止非法状态转移**：状态机实现必须有守卫检查
- **禁止跳过资源释放步骤**：不能只 cancel() 不 wg.Wait()
- **禁止直接用 `sync.Mutex + map`**：已有 `SyncMap[K, V]` 和 `Registry[T]`
- **禁止 `.yml` 后缀**：统一 `.yaml`
- **禁止 `I` 前缀接口命名**
- **禁止全大写常量**：不用 `MAX_TOKENS`，用 `MaxTokens`
- **禁止 `sync.Map`**：类型不安全，使用泛型 `xsync.SyncMap`
- **禁止 `gopkg.in/yaml.v3`**：使用 `github.com/goccy/go-yaml`

#### 线程安全模式

- **SyncMap 的 Load+Modify+Store 原子性**：当需要读取→修改→写回时，外部加 `sync.Mutex`（参见 `slaResults` + `slaResultsMu`）
- **BudgetPool 用 `sync.RWMutex`**：读操作（Status）用 RLock，写操作（Allocate/Consume）用 Lock
- **DiffMemory/Lineage 用 `sync.RWMutex`**：读写分离
- **ReputationStore/SynergyMatrix 用 `sync.Mutex`**：文件 I/O 操作序列化
- **ImmuneDaemon 用 `sync.RWMutex`**：profiles/alerts 读写分离

#### 深拷贝防护

- **切片返回必须深拷贝**：`Lineage.Events()` 拷贝 events 切片及内部 Skills 切片，防止调用者修改
- **nil slice → empty slice**：JSON 序列化时 `Skills: nil` 应转为 `Skills: []string{}`，避免 JSON `null`

#### 边界情况

- **DebugChan 为 nil 时**：跳过事件记录，零开销
- **进程退出后 FD 访问**：Dead 状态进程的 FD 已关闭，返回 `ErrNotFound`
- **孤儿进程**：父进程退出后子进程 reparent 到 init（PID=1）
- **Zombie 进程超时**：转 Zombie 而非直接 Dead，必须等 Wait 回收
- **NormalProfile 最少样本数**：少于 5 个样本不生成行为基线，返回 nil
- **DiffMemory 驱逐**：容量满时按 HitCount 最低 + Timestamp 最旧驱逐
- **Intent 失败级联**：上游节点失败自动递归标记所有下游依赖为 failed
- **BudgetPool 配额重算**：每次新代理注册触发所有配额重新计算

#### 安全规则

- **Skill tools 白名单**：manifest.yaml 中 `tools` 列表定义允许的 `/dev/` 路径，非白名单返回 `ErrPermission`
- **免疫系统自动暂停**：异常行为检测后自动 SIGPAUSE，需人工 `immune resume` 确认后恢复
- **API Key 管理**：通过环境变量引用（`api_key_env` 字段），不在配置文件中存储明文密钥
- **Provider 名称验证**：正则 `^[a-zA-Z0-9_-]+$` 防止路径注入

---

## Usage Guidelines

**For AI Agents:**

- Read this file before implementing any code
- Follow ALL rules exactly as documented
- When in doubt, prefer the more restrictive option
- Update this file if new patterns emerge

**For Humans:**

- Keep this file lean and focused on agent needs
- Update when technology stack changes
- Review periodically for outdated rules
- Remove rules that become obvious over time

Last Updated: 2026-03-14
