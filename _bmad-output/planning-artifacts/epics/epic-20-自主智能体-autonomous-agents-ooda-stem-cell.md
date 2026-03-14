# Epic 20: 自主智能体（OODA + 干细胞分化）

智能体通过 OODA 循环自主执行任务，通用基底智能体根据意图自动匹配 Skill 完成分化。

## Story 20.1: OODA 循环核心实现

As a 平台构建者,
I want 智能体可以在推理循环中执行 OODA 四阶段（感知-判断-决策-行动）,
So that 智能体能够自主感知环境变化并做出适应性决策。

**FRs:** FR112, FR113, FR114, FR115

**Acceptance Criteria:**

**Given** 一个 OODA 模式的智能体
**When** 进入 Observe 阶段
**Then** 智能体通过 VFS 读取环境信息（/proc/ 状态、其他进程输出、文件变更）

**Given** Observe 阶段完成
**When** 进入 Orient 阶段
**Then** 智能体评估感知数据与目标的偏差

**Given** Orient 阶段完成
**When** 进入 Decide 阶段
**Then** 智能体自主选择下一步行动（调用工具、spawn 子进程、请求协作或调整计划）

**Given** Decide 阶段完成
**When** 进入 Act 阶段
**Then** 执行决策并将结果反馈到下一轮 Observe 形成闭环
**And** 单轮 OODA 循环框架开销 <= 200ms（NFR44）

**Given** `kernel/ooda.go` 中 `OODAPhase` 和 `OODAState` 已实现
**When** 创建 OODA 模式的进程
**Then** `OODAPhase` 包含四个常量：`PhaseObserve`、`PhaseOrient`、`PhaseDecide`、`PhaseAct`
**And** `OODAState` 包含 `Phase`、`Cycle`、`Observations`、`Orientation`、`Decision` 字段
**And** `OODADecision` 包含 `Action`（`tool_call|spawn|complete|replan`）、`Target`、`Data`、`Reason` 字段
**And** `Process` 结构体（`kernel/process.go`）新增 `oodaEnabled bool` 和 `oodaState *OODAState` 字段，通过 `proc.mu` 保护
**And** 提供线程安全的 `IsOODA()`、`GetOODAState()`、`SetOODAPhase()` 方法

**Given** `kernel/ooda.go` 中 `oodaReasonStep` 已实现
**When** OODA 模式进程启动推理循环
**Then** `oodaReasonStep` 作为独立 goroutine 运行，与默认 `reasonStep` 并行存在，不修改现有 `reasonStep` 代码
**And** Observe 阶段是纯框架代码（不调用 LLM），通过 `k.ListProcesses()` 和 `k.ctxMgr.GetContextInfo()` 收集环境数据
**And** Orient 阶段通过 LLM 调用评估观测数据与目标偏差
**And** Decide 阶段通过 LLM 调用输出结构化 JSON 决策：`{"action":"...", "target":"...", "data":{}, "reason":"..."}`
**And** Act 阶段根据 `OODAActionType` 分支执行：`OODAToolCall` 复用 VFS 工具调用、`OODASpawn` 调用 `k.Spawn()`、`OODAComplete` 设置结果并退出、`OODAReplan` 写入上下文供下一轮评估

**Given** `SpawnOpts` 中 `ReasoningMode` 设置为 `"ooda"`
**When** Spawn 方法启动推理循环
**Then** `proc.oodaEnabled = true`，初始化 `proc.oodaState`
**And** 启动 `go k.oodaReasonStep(proc, llmFD, opts)` 而非 `go k.reasonStep(proc, llmFD, opts)`
**And** 不设置 `ReasoningMode` 时走默认线性推理路径，确保零回归

**Given** OODA 循环到达 `OODAComplete` 决策
**When** Act 阶段执行
**Then** 进程以 `ExitStatus{Code: 0, Reason: "ooda_completed"}` 正常完成

**Given** OODA 循环超过 `proc.MaxSteps`（复用为最大循环数）
**When** 达到最大循环限制
**Then** 进程以 `ExitStatus{Code: 1, Reason: "max ooda cycles exceeded"}` 终止

**Given** OODA 循环中 context 被取消（如外部 kill 信号）
**When** 任意阶段间检测到 `ctx.Done()`
**Then** 循环立即退出，进程正确转入 Zombie 状态
**And** 各阶段之间均有 inter-phase context cancellation 检查

**Given** OODA 各阶段执行时
**When** 阶段完成
**Then** 产生对应的 SyscallEvent（`OODAObserve`/`OODAOrient`/`OODADecide`/`OODAAct`）和 `OODACycle` 汇总事件
**And** 产生 `LogOODA` 类别的日志（`internal/types/types.go` 中定义），格式为 `[ooda:observe]`、`[ooda:orient]` 等
**And** `OODACycle` 事件包含 `cycle`（轮数）和 `framework_overhead`（毫秒）字段

**Given** 使用 mock LLM 即时返回的性能测试环境
**When** 运行单轮 OODA 循环框架开销基准测试
**Then** 纯框架代码开销（不含 LLM 调用时间）≤ 200ms（NFR44）
**And** 通过 `go test -race` 无数据竞争

## Story 20.2: OODA 配置与任务式指挥

As a 平台构建者,
I want 通过 agent.yaml 启用 OODA 模式，并让 OODA 智能体自主 spawn 子智能体,
So that 我可以构建自主决策的智能体层级。

**FRs:** FR116, FR117

**Acceptance Criteria:**

**Given** agent.yaml 中声明 `reasoning: ooda`
**When** spawn 该 Agent
**Then** 智能体使用 OODA 循环替代默认线性推理模式

**Given** OODA 模式的智能体在 Decide 阶段
**When** 智能体决定需要子任务
**Then** 智能体自主 spawn 子智能体，只下达意图不规定执行细节（任务式指挥）

**Given** OODA 智能体 spawn 的子智能体
**When** 子智能体的 agent.yaml 也声明 `reasoning: ooda`
**Then** 子智能体内部同样以 OODA 循环自主执行

**Given** `agents/types.go` 中 `AgentManifest` 结构体
**When** 新增 `Reasoning string` 字段（`yaml:"reasoning,omitempty"`）
**Then** agent.yaml 中可声明 `reasoning: ooda` 或 `reasoning: linear`
**And** `agents/loader.go` 的 `Load` 方法验证 Reasoning 字段值：空字符串或 `"linear"` 为默认线性模式，`"ooda"` 为 OODA 循环模式
**And** 传入无效值（如 `reasoning: foo`）时返回错误：`invalid reasoning mode "foo": must be empty, "linear", or "ooda"`

**Given** `kernel/kernel.go` 的 Spawn 方法中 agent info 处理块
**When** 检测到 `agent.Manifest.Reasoning == "ooda"`
**Then** 自动设置 `opts.ReasoningMode = "ooda"`
**And** 优先级为 agent.yaml 的 `reasoning` 字段 > `SpawnOpts.ReasoningMode`（agent.yaml 声明式配置覆盖程序化参数）
**And** 无需修改 IPC 协议（`SpawnRequest` 不新增字段，reasoning mode 通过 agent info 在 Spawn 内部传播）

**Given** OODA 模式的智能体在 Decide 阶段输出 spawn 决策
**When** `OODADecision.Data` 包含 `{"agent": "子智能体名"}` JSON 字段
**Then** `oodaActSpawn`（`kernel/ooda.go`）解析 `oodaSpawnData` 结构体（含 `Agent`、`Model` 可选字段）
**And** 通过 `KernelImpl.agentLoader`（函数类型 `func(string) (*agents.AgentInfo, error)`，Spawn 后注入）加载指定 agent 并传入 `k.Spawn()`
**And** 父智能体只下达意图（`decision.Target`），不规定执行细节，体现任务式指挥原则

**Given** `OODADecision.Data` 中指定了不存在的 agent 名称
**When** `oodaActSpawn` 尝试加载该 agent
**Then** 返回描述性错误信息，不导致父进程崩溃
**And** 错误信息写入上下文供下一轮 OODA 循环评估

**Given** `OODADecision.Data` 中 JSON 格式不合法
**When** `oodaActSpawn` 解析 `oodaSpawnData`
**Then** 返回解析错误信息，不静默吞掉错误（Story 20.2 代码审查 HIGH #1 修复点）

**Given** 父 OODA 智能体 spawn 的子智能体 agent.yaml 声明 `reasoning: ooda`
**When** 子进程通过 Spawn 创建
**Then** Spawn 读取子 agent 的 `Manifest.Reasoning` 并自动设置 `opts.ReasoningMode = "ooda"`
**And** 子进程 `proc.IsOODA() == true`，内部以 OODA 循环自主执行（AC#3 由 AC#1 + AC#2 联合满足）

**Given** 父 OODA 智能体 spawn 的子智能体 agent.yaml 不声明 reasoning
**When** 子进程通过 Spawn 创建
**Then** 子进程 `proc.IsOODA() == false`，使用默认线性推理模式

## Story 20.3: Stem Agent 与自动分化

As a 平台构建者,
I want 系统提供通用基底智能体，根据接收到的意图自动匹配和加载最相关的 Skill 组合,
So that 我不需要预先指定 Agent 模板，系统能自动选择最佳能力组合。

**FRs:** FR118, FR119

**Acceptance Criteria:**

**Given** 用户执行 `rnix "分析代码" --agent=stem`
**When** Stem Agent 接收意图
**Then** 系统分析意图，自动匹配最相关的 Skill 组合（如 code-analysis + git-tools），加载后完成分化

**Given** Stem Agent 分化过程
**When** 匹配到候选 Skill
**Then** 实时输出分化过程：`[agent/N] differentiating: loading skills [...]`
**And** Skill 匹配和加载 <= 3s（NFR45）

**Given** `skills/discovery.go` 中 `SkillDiscovery` 已实现
**When** 调用 `DiscoverAll()`
**Then** 扫描 `basePath`（`lib/skills/`）下所有子目录，对每个子目录调用 `loader.LoadMetadata(name)` 读取 SKILL.md frontmatter
**And** 跳过无效目录（无 SKILL.md 或解析失败），仅吞掉 `os.IsNotExist` 错误，其他错误正确传播
**And** 返回所有有效 skill 的 `SkillInfo` 列表（仅 manifest，不含 body）

**Given** `kernel/stem.go` 中 `StemMatcher` 已实现
**When** 调用 `Match(intent string)`
**Then** 通过关键词重叠度算法匹配意图与 skill：将 intent 按空格/标点分词，将 skill 的 `Name`（按 `-` 分词）和 `Description`（按空格分词）合并为关键词集合
**And** 匹配分数 = 交集大小 / intent 关键词数，分数 > 0 的 skill 入选
**And** 返回匹配度降序排列的 skill 名称列表
**And** 不依赖 LLM 调用（关键词匹配确保满足 NFR45 ≤ 3s 要求）

**Given** Stem Agent（`lib/agents/stem/agent.yaml`）的定义
**When** 查看 agent manifest
**Then** `name: stem`、`skills: []`（空，分化时动态加载）、`reasoning: ooda`（使用 OODA 推理模式）
**And** `lib/agents/stem/instructions.md` 包含 Stem Agent 的 system prompt

**Given** `kernel/kernel.go` 的 Spawn 方法检测到 `agent.Manifest.Name == "stem"` 且 `len(agent.Manifest.Skills) == 0`
**When** `k.stemMatcher != nil`
**Then** 调用 `k.stemMatcher.Match(intent)` 进行 skill 匹配
**And** 对匹配到的 skill 名称列表逐个调用 `k.skillLoader(skillName)` 加载完整 skill 信息
**And** 将加载的 skill 追加到 `agent.Skills` 切片，`SystemPrompt()` 和 `AllowedTools()` 自动聚合更新
**And** 更新 `proc.Skills` 确保 `rnix ps` 反映最新 skill 列表

**Given** StemMatcher 匹配返回空列表
**When** 无任何 skill 与意图匹配
**Then** Stem Agent 以裸进程运行（无 skill 加载），不报错

**Given** 纯中文意图（如 "分析代码"）
**When** StemMatcher 尝试匹配英文 skill 元数据
**Then** 关键词算法无法有效匹配（已知 CJK 局限性，Story 20.4 记录为待改进项）
**And** 返回空列表，Stem Agent 以裸进程运行

**Given** 分化过程完成
**When** 产生 `StemDifferentiate` 事件
**Then** 事件包含 `matched_skills`（匹配到的 skill 列表）和 `duration_ms`（分化耗时）字段
**And** 通过 `emitLog` 输出格式为 `[ooda:stem] differentiating: matching skills for intent "..."` 和 `[ooda:stem] differentiating: loading skills [...]`

**Given** 使用 mock SkillDiscovery（返回 50 个 skill 元数据）的性能测试
**When** 运行 NFR45 性能基准测试
**Then** Match + LoadFull 总耗时 ≤ 3s（NFR45）
**And** 通过 `go test -race` 无数据竞争

## Story 20.4: 渐进式特化与分化记忆

As a 平台构建者,
I want 分化后的智能体可以在执行过程中动态加载额外 Skill，并记忆分化路径,
So that 智能体能力可以按需扩展，且相似任务可快速复用上次分化结果。

**FRs:** FR120, FR121

**Acceptance Criteria:**

**Given** 一个已分化的智能体在执行任务
**When** 智能体检测到能力缺口（需要当前 Skill 未覆盖的工具）
**Then** 动态加载额外 Skill 进一步特化，不中断执行

**Given** 智能体完成分化和执行
**When** 分化路径被记录
**Then** 系统保存"表观遗传"记忆：哪些 Skill 被加载、加载顺序、触发意图

**Given** 下次接收到相似意图
**When** Stem Agent 开始分化
**Then** 系统优先复用上次记录的分化路径，加速分化过程

**Given** `kernel/diffmemory.go` 中 `DiffMemory` 结构体已实现
**When** 调用 `Record(intent string, skills []string)`
**Then** 将 intent 通过 `normalizeIntent()` 规范化为签名（复用 `kernel/stem.go` 中的 `tokenize()` 函数，token 排序后拼接）
**And** 创建或更新 `DiffMemoryEntry`（包含 `Intent`、`Skills`、`Timestamp`、`HitCount` 字段）
**And** 如果条目已存在且 skill 列表相同，仅更新 Timestamp（`HitCount` 仅在 `Lookup` 时递增）
**And** 超过 `maxSize`（默认 256）时淘汰 HitCount 最低且 Timestamp 最旧的条目
**And** 使用 `sync.RWMutex` 保护并发访问，Record 加写锁

**Given** `DiffMemory` 中已记录分化路径
**When** 调用 `Lookup(intent string)` 查找相似意图
**Then** 规范化 intent 后精确匹配签名（确保 "analyze code" 和 "code analyze" 映射到同一签名）
**And** 命中时更新 `HitCount`，返回 skill 列表
**And** 未命中时返回 `nil, false`

**Given** `kernel/kernel.go` 的 Spawn 方法中 stem agent 分化逻辑
**When** Stem Agent 开始分化
**Then** 优先调用 `k.diffMemory.Lookup(intent)` 查询分化记忆
**And** 命中时直接使用记忆中的 skill 列表，跳过关键词匹配，输出日志 `differentiating: reusing remembered path for intent "..."`
**And** 未命中时降级为 `k.stemMatcher.Match(intent)` 关键词匹配
**And** 分化成功后调用 `k.diffMemory.Record(intent, loadedNames)` 记录路径
**And** `StemDifferentiate` 事件包含 `from_memory` 布尔字段标记是否来自记忆复用

**Given** `kernel/ooda.go` 中新增 `OODASpecialize` 动作类型
**When** OODA Decide 阶段 LLM 输出 `{"action": "specialize", "target": "skill-name"}`
**Then** `oodaAct` 分支到 `oodaActSpecialize`，不中断 OODA 循环

**Given** `oodaActSpecialize` 执行动态 Skill 加载
**When** 指定 skill 未被当前进程加载
**Then** 通过 `k.skillLoader(skillName)` 加载 skill 完整信息
**And** 追加 skill 名到 `proc.Skills`（通过 `proc.mu` 保护，含 TOCTOU 防护——re-check under lock before append）
**And** 追加 skill 工具权限到 `proc.AllowedDevices`
**And** 通过 `k.ctxMgr.AppendMessage()` 将 skill body 注入上下文（格式：`[Dynamic Skill Loaded: name]\n{body}`），LLM 在下一轮 OODA 循环可使用新能力
**And** 产生 `StemSpecialize` 事件，包含 `skill` 和 `total_skills` 字段
**And** 更新 `DiffMemory` 中当前进程意图对应的 skill 列表

**Given** `oodaActSpecialize` 尝试加载已存在的 skill
**When** `proc.Skills` 中已包含目标 skill
**Then** 返回提示信息 `skill "name" already loaded`，不重复加载

**Given** `oodaActSpecialize` 尝试加载不存在的 skill
**When** `k.skillLoader` 返回错误
**Then** 返回错误信息 `specialize error: skill "name" load failed: ...`，不导致进程崩溃
**And** 错误信息写入上下文供下一轮 OODA 循环 Orient 阶段评估

**Given** `oodaActSpecialize` 中 `ctxMgr.AppendMessage()` 调用失败
**When** 上下文已被释放或其他异常
**Then** 输出警告日志（不静默吞掉错误，Story 20.4 代码审查 HIGH #3 修复点）

**Given** 多个 goroutine 并发对 `DiffMemory` 读写
**When** 同时执行 Record 和 Lookup
**Then** 通过 `sync.RWMutex` 保证线程安全
**And** 通过 `go test -race`（100 goroutine 并发）无数据竞争

## Story 20.5: 分化谱系图

As a 平台构建者,
I want 通过 `rnix lineage <pid>` 查看从基底到特化体的完整分化路径,
So that 我可以理解智能体是如何获得当前能力的。

**FR:** FR122

**Acceptance Criteria:**

**Given** 一个经过分化的智能体
**When** 用户执行 `rnix lineage <pid>`
**Then** 展示从 Stem Agent 到当前特化体的完整路径，包含每次分化加载的 Skill 和触发的意图

**Given** 谱系图中包含多次渐进式特化
**When** 展示谱系
**Then** 每次 Skill 加载标注时间点和触发原因

**Given** `kernel/lineage.go` 中 `Lineage` 和 `LineageEvent` 已实现
**When** 创建和使用谱系记录
**Then** `LineageEvent` 包含 `Timestamp`、`Phase`（`"initial"` 或 `"progressive"`）、`Skills`（加载的 skill 列表）、`Trigger`（触发意图或原因）、`FromMemory`（是否来自 DiffMemory 复用）字段
**And** `Lineage` 使用 `sync.RWMutex` 保护（Record 加写锁，Events 加读锁），与 `proc.mu` 独立避免嵌套锁
**And** `Events()` 返回深拷贝（包含 Skills 切片的独立副本），防止调用方修改原始数据

**Given** `kernel/process.go` 中 `Process` 结构体
**When** 新增 `lineage *Lineage` 字段
**Then** 提供 `GetLineage()` 和 `SetLineage()` 方法
**And** lineage 在 Spawn stem 分化成功后延迟初始化（非 stem agent 不创建 lineage）

**Given** `kernel/kernel.go` 的 Spawn 方法中 stem agent 分化成功
**When** skill 加载完成后
**Then** 初始化 `proc.lineage = NewLineage()` 并记录 `LineageEvent{Phase: "initial", Skills: loadedNames, Trigger: intent, FromMemory: fromMemory}`
**And** 非 stem agent 的 Spawn 不创建 lineage 记录

**Given** `kernel/ooda.go` 的 `oodaActSpecialize` 成功加载 skill
**When** 动态 skill 加载完成后
**Then** 如果 `proc.lineage != nil`，追加 `LineageEvent{Phase: "progressive", Skills: []string{skillName}, Trigger: decision.Reason}`

**Given** `ipc/protocol.go` 中定义 IPC lineage 查询协议
**When** 客户端发送 `MethodLineage` 请求
**Then** `LineageRequest` 包含 `PID` 字段
**And** `LineageResponse` 包含 `Events []LineageEvent`（IPC 边界类型，`TimestampMs` 使用 `int64` 毫秒格式，遵循 IPC wire format 约定）
**And** `ipc/server.go` 中 `handleLineage` 调用 `s.kern.GetLineage(req.PID)` 并将 `kernel.LineageEvent` 转换为 `ipc.LineageEvent`
**And** `ipc/client.go` 中 `Lineage(pid types.PID)` 方法封装 IPC 调用

**Given** 用户执行 `rnix lineage <pid>` 且进程存在且有 lineage 记录
**When** CLI 渲染谱系信息（`cmd/rnix/lineage.go`）
**Then** 文本模式下：步骤编号粗体，`"initial"` 阶段绿色显示，`"progressive"` 阶段蓝色显示，skill 名称高亮
**And** `FromMemory=true` 时 Source 显示 `"memory-reuse"`，否则显示 `"keyword-match"`（initial 阶段）或 `"ooda-specialize"`（progressive 阶段）
**And** 支持 `--json` 标志输出 JSON 格式

**Given** 用户执行 `rnix lineage <pid>` 且 PID 不存在
**When** IPC 查询返回 NOT_FOUND 错误
**Then** 输出错误提示，CLI 不崩溃

**Given** 用户执行 `rnix lineage <pid>` 且进程存在但无 lineage 记录（非 stem agent）
**When** IPC 查询返回空事件列表
**Then** 输出 `"Process <pid> has no differentiation lineage"`

**Given** 用户执行 `rnix lineage` 未提供 PID 参数
**When** 解析命令行参数
**Then** 输出缺少参数的错误提示

**Given** 多个 goroutine 并发调用 `Lineage.Record()` 和 `Lineage.Events()`
**When** 同时写入和读取谱系
**Then** 通过 `sync.RWMutex` 保证线程安全
**And** 通过 `go test -race` 无数据竞争
