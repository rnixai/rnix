# Epic 21: Token 经济、声誉与 Skill 协同

系统智能管理 token 预算，通过合约 SLA 约束质量，基于声誉择优选择，自动检测 Skill 协同效应。

## Story 21.1: Token 预算池与分配调度

As a 应用开发者,
I want 为 Compose 编排分配总 token 预算池，系统按优先级智能分配配额,
So that 关键任务获得更多资源，低优先级任务不会浪费预算。

**Acceptance Criteria:**

**Given** rnix-compose.yaml 定义了总 token 预算 `token_budget: 50000` 和各智能体优先级 `priority: high|normal|low`
**When** `compose up` 执行
**Then** 系统创建 `BudgetPool`（`kernel/budget_pool.go`）并按优先级分配初始配额
**And** 高优先级（`PriorityHigh=10`）获得更大比例配额，低优先级（`PriorityLow=1`）获得较小比例

**Given** 多个智能体竞争有限 token 预算
**When** 高优先级智能体需要更多配额
**Then** 系统通过价格信号机制调度——高优先级获得更多配额，低优先级被降级或排队
**And** 预算分配决策延迟 <= 100ms（NFR46）

**Given** Compose 编排正在运行且预算池已创建
**When** 用户通过 IPC 查询预算池状态（`GetStatus()`）
**Then** 返回 `BudgetPoolStatus`，包含总预算、已分配、已消耗、各智能体配额和消耗情况
**And** 返回 JSON 结构可被 `--json` 标志序列化

**Given** 预算池已耗尽（总消耗 >= 总预算，`IsExhausted()` 返回 true）
**When** 智能体通过 `RecordUsage(pid, tokens)` 请求更多 token
**Then** 所有智能体收到预算耗尽通知，按现有 `budget_exceeded` 机制终止
**And** Compose 编排标记为 `budget_exhausted` 状态

**Given** rnix-compose.yaml 未定义 `token_budget` 字段
**When** `compose up` 执行
**Then** 行为与现有完全一致——每个智能体使用自己的 `context_budget`（Story 10.3 已实现）
**And** 不创建 `BudgetPool`，`compose/engine.go` 中相关逻辑被跳过

**Given** 100 个智能体同时请求配额分配
**When** 并发调用 `AllocateQuota` 和 `RecordUsage`
**Then** 通过 `sync.RWMutex` 保证并发安全，无数据竞争
**And** `go test -race ./kernel/...` 通过
**And** 100 次分配总耗时 < 100ms（NFR46）

**Given** 智能体 PID 不存在于预算池中
**When** 调用 `RecordUsage(invalidPID, tokens)`
**Then** 返回错误，不影响预算池中其他智能体的数据

## Story 21.2: 合约 SLA 与评估

As a 应用开发者,
I want 通过合约 SLA 约束智能体协作的输入格式、输出质量、token 消耗和超时,
So that 智能体之间的协作有明确的质量保证。

**Acceptance Criteria:**

**Given** agent.yaml 或 compose.yaml 中定义了合约 SLA（`max_tokens`、`max_duration_ms`、`output_format` 等约束）
**When** 系统加载配置时
**Then** `SLASpec`（`kernel/sla.go`）被正确解析为结构化类型
**And** 缺省字段使用合理默认值（`max_tokens=0` 表示不限制，`output_format=""` 表示接受任何格式）

**Given** 智能体完成执行
**When** 系统自动调用 SLA 评估（`EvaluateSLA`）
**Then** 对每项约束（token 消耗、响应时间、输出格式）逐一检查通过/失败
**And** 生成 `SLAResult`，包含各 `SLACheckResult` 条目（`Name`、`Passed`、`Actual`、`Limit`）

**Given** SLA 评估完成
**When** 评估结果生成
**Then** 结果通过 `ReputationStore` 记录到该 Agent 模板的声誉分数文件（`$PROJECT/.rnix/reputation/{agent_name}.json`）
**And** 记录包含各项通过/失败状态、时间戳、执行元数据

**Given** Compose 编排完成每个智能体的执行
**When** 智能体退出后
**Then** `compose/engine.go` 中 Engine 自动触发 SLA 评估并将结果附加到 `ScheduleResult`
**And** SLA 评估结果可通过 IPC 方法查询

**Given** agent.yaml 或 compose.yaml 未定义 SLA（`SLASpec` 为零值）
**When** 智能体执行完成
**Then** 行为与现有完全一致——不进行 SLA 评估，无声誉记录
**And** `compose/engine.go` 中 SLA 相关逻辑被跳过

**Given** 智能体实际 token 消耗为 8000，SLA 定义 `max_tokens: 5000`
**When** SLA 评估执行
**Then** `max_tokens` 检查结果为 `Passed=false`，`Actual="8000"`，`Limit="5000"`
**And** 整体 SLA 结果标记为 `Passed=false`

**Given** 多个 SLA 约束中部分通过部分失败
**When** 评估完成后查看 `SLAResult`
**Then** 各项检查结果独立记录，`SLAResult.Passed` 为所有检查项的逻辑与
**And** 失败项可被单独识别用于声誉系统的细粒度分析

## Story 21.3: 声誉系统与自动选择

As a 应用开发者,
I want 系统跟踪 Agent 模板的历史表现并在自动分配时择优选择,
So that 表现好的 Agent 模板被更多使用，系统质量持续优化。

**Acceptance Criteria:**

**Given** Agent 模板有历史执行记录（`ReputationStore`（`kernel/reputation.go`）中的 JSON Lines 数据）
**When** 系统调用 `GetSummary(agentName)` 计算声誉分数
**Then** 基于历史 SLA 评估结果生成 `ReputationSummary`，包含 `Score`（0.0~1.0）、`SuccessRate`、`AvgTokens`、`AvgDurationMs`、`TotalRecords`
**And** 近期记录权重高于历史记录（时间衰减），`RecentTrend` 显示 "improving"/"declining"/"stable"

**Given** Agent 模板有历史执行记录
**When** 用户执行 `rnix reputation [agent]`（`cmd/rnix/reputation.go`）
**Then** 显示声誉分数、成功率、平均 token 效率、SLA 达标率和近期趋势
**And** 输出格式包含表格化展示，使用 `internal/ui/` 样式组件

**Given** 用户未指定 agent 名称
**When** 执行 `rnix reputation`（无参数）
**Then** 列出所有已知 Agent 的声誉摘要，按声誉分数降序排列
**And** 使用 `--json` 标志时输出符合 `JSONResponse[T]` 格式

**Given** Reconciler 或系统需要自动选择 Agent 模板
**When** 多个候选模板可用（通过 agent.yaml 的 `alternatives` 或 Compose 的 `candidates` 字段）
**Then** 系统优先选择 `ReputationSummary.Score` 最高的模板
**And** 选择逻辑在 `compose/engine.go` 中实现

**Given** Agent 模板无历史执行记录（新创建的模板）
**When** 查询声誉或进行自动选择
**Then** 返回默认中性分数（`Score=0.5`），不被惩罚也不被优先
**And** 行为与当前完全一致——单一 Agent 指定时直接使用，无候选列表时跳过选择

**Given** 通过 IPC 查询 `reputation_status`
**When** 指定 Agent 名称
**Then** 返回该 Agent 的 `ReputationSummary` JSON
**And** 未指定名称时返回全部 Agent 的声誉列表

**Given** 声誉数据文件（`$PROJECT/.rnix/reputation/{agent_name}.json`）损坏或格式错误
**When** 系统尝试加载声誉数据
**Then** 返回错误日志但不 panic，该 Agent 使用默认中性分数
**And** 不影响其他 Agent 的声誉数据加载

**Given** 两个候选 Agent 模板声誉分数相同
**When** 系统进行自动选择
**Then** 选择结果确定性（如按名称字母序），避免随机性导致的不可预测行为

## Story 21.4: Skill Synergy 声明与自动检测

As a 平台构建者,
I want SKILL.md 可以声明 synergy 字段，系统在加载多个 Skill 时自动检测并激活协同效应,
So that Skill 组合能产生 1+1>2 的涌现能力。

**Acceptance Criteria:**

**Given** SKILL.md 中声明了 `synergy` 字段（YAML frontmatter list），包含 `with`（目标 Skill 名称）和 `instruction`（涌现指令）
**When** `SkillLoader`（`skills/loader.go`）解析该 SKILL.md
**Then** 正确解析 synergy 声明列表到 `SkillManifest.Synergies`（`[]SynergyDecl`，`skills/types.go`）
**And** 未声明 synergy 字段时默认为空切片（向后兼容）

**Given** 智能体同时加载两个 Skill（如 A 和 B），A 的 synergy 声明中有 `with: "B"` 的条目
**When** 系统调用 `DetectSynergies`（`skills/synergy.go`）组装 system prompt
**Then** 自动检测到 synergy 命中，将涌现指令追加到 system prompt
**And** 涌现指令来自 `SynergyDecl.Instruction` 字段

**Given** 智能体加载 N 个有交叉 synergy 的 Skill
**When** 系统执行 `DetectSynergies(skills []*SkillInfo)` 检测 synergy 组合
**Then** 所有命中的 synergy 指令都被追加（不遗漏）
**And** 同一条 synergy 指令不重复追加（去重处理）

**Given** 任意数量的 Skill synergy 声明
**When** 执行组合检测
**Then** 检测开销 <= 100ms（NFR49）
**And** 算法复杂度 O(N*M)（N=Skill 数，M=平均 synergy 声明数），对合理规模远低于阈值

**Given** 现有 SKILL.md（如 `lib/skills/code-analysis/SKILL.md`）无 synergy 字段
**When** 被 `SkillLoader` 加载
**Then** 行为与当前完全一致——`Synergies` 为 nil，解析无错误，SystemPrompt 输出不变
**And** go-yaml 自动忽略未知字段 + 新增字段有零值默认 = 向后兼容

**Given** synergy 声明中 `with` 字段引用了一个未加载的 Skill 名称
**When** 系统执行 synergy 检测
**Then** 该条 synergy 声明不命中，不追加指令
**And** 不产生错误或警告（静默跳过）

**Given** 双向 synergy 声明（A 声明 with B，B 也声明 with A）
**When** 智能体同时加载 A 和 B
**Then** 两条 synergy 指令都被追加（如果内容不同）
**And** 如果两条指令内容完全相同，去重后只保留一条

## Story 21.5: Skill 组合矩阵

As a 平台构建者,
I want 系统维护 Skill 组合矩阵记录历史表现，可通过命令查看有效组合,
So that 我可以了解哪些 Skill 组合效果最好。

**Acceptance Criteria:**

**Given** 智能体加载了多个 Skill 并完成执行
**When** SLA 评估完成后
**Then** 系统将该次执行的 Skill 组合及结果记录到 `SynergyMatrix`（`kernel/synergy_matrix.go`）
**And** 记录包含：`SynergyComboKey`（排序后逗号拼接的技能组合标识）、SLA 结果（是否通过、token 消耗、时长）、时间戳

**Given** 系统已积累 Skill 组合的历史执行数据
**When** 用户执行 `rnix synergy list`（`cmd/rnix/synergy.go`）
**Then** 展示已知的有效 Skill 组合，包含成功率、平均 token 消耗、使用频次
**And** 输出按推荐度排序（成功率高的在前）

**Given** 组合矩阵数据中同时包含组合执行和单 Skill 执行的记录
**When** 用户查看组合列表
**Then** 每条组合显示对比数据：组合成功率 vs 各 Skill 单独平均成功率
**And** 组合 token 效率提升百分比

**Given** 组合矩阵数据中存在显著优于单 Skill 的组合
**When** 组合成功率比各 Skill 单独平均成功率高出 10% 以上
**Then** 标记为推荐组合（`recommended`）
**And** 在 CLI 输出中以特殊标记（如星号或颜色）突出显示

**Given** 用户执行 `rnix synergy list --json`
**When** 结果返回
**Then** 输出符合 `JSONResponse[T]` 格式
**And** `data` 字段包含完整的组合矩阵信息（组合标识、成功率、token 消耗、推荐状态）

**Given** 无历史组合数据
**When** 用户执行 `rnix synergy list`
**Then** 显示 "No synergy combination data available."
**And** 不报错、不 panic，退出码为 0

**Given** 已有的 `rnix reputation` 命令和 `ReputationStore`
**When** 新增 Synergy 组合矩阵功能
**Then** 不影响现有声誉系统的功能和数据格式
**And** `compose/engine.go` 中 SLA 评估完成后同时记录到声誉存储和组合矩阵存储

**Given** 组合矩阵中记录了大量历史数据（> 1000 条）
**When** 用户执行 `rnix synergy list`
**Then** 输出按推荐度排序并聚合，响应时间合理
**And** 不一次性加载全部原始记录到内存（支持流式聚合或分页）
