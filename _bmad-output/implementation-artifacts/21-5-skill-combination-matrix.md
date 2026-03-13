# Story 21.5: Skill 组合矩阵

Status: done

## Story

As a 平台构建者,
I want 系统维护 Skill 组合矩阵记录历史表现，可通过命令查看有效组合,
So that 我可以了解哪些 Skill 组合效果最好。

## Acceptance Criteria

1. **AC1: Synergy 组合执行记录**
   - Given 智能体加载了多个 Skill 并完成执行
   - When SLA 评估完成后
   - Then 系统将该次执行的 Skill 组合及结果记录到组合矩阵存储中
   - And 记录包含：技能组合标识、SLA 结果（是否通过、token 消耗、时长）、时间戳

2. **AC2: `rnix synergy list` 命令**
   - Given 系统已积累 Skill 组合的历史执行数据
   - When 用户执行 `rnix synergy list`
   - Then 展示已知的有效 Skill 组合，包含：成功率、平均 token 消耗、使用频次
   - And 输出按推荐度排序（成功率高的在前）

3. **AC3: 组合 vs 单独表现对比**
   - Given 组合矩阵数据中同时包含组合执行和单 Skill 执行的记录
   - When 用户查看组合列表
   - Then 每条组合显示对比数据：组合成功率 vs 各 Skill 单独平均成功率
   - And 组合 token 效率提升百分比

4. **AC4: 推荐组合标记**
   - Given 组合矩阵数据中存在显著优于单 Skill 的组合
   - When 组合成功率比各 Skill 单独平均成功率高出 10% 以上
   - Then 标记为推荐组合（recommended）

5. **AC5: JSON 输出支持**
   - Given 用户执行 `rnix synergy list --json`
   - When 结果返回
   - Then 输出符合 `JSONResponse[T]` 格式
   - And `data` 字段包含完整的组合矩阵信息

6. **AC6: 空数据优雅处理**
   - Given 无历史组合数据
   - When 用户执行 `rnix synergy list`
   - Then 显示 "No synergy combination data available."
   - And 不报错、不 panic

7. **AC7: 向后兼容**
   - Given 已有的 `rnix reputation` 命令和 ReputationStore
   - When 新增 Synergy 组合矩阵功能
   - Then 不影响现有声誉系统的功能和数据格式

## Tasks / Subtasks

### Task 1: SynergyRecord 数据类型（AC: #1, #7）

- [x] 1.1 在 `kernel/synergy_matrix.go` 中定义数据结构：

  ```go
  // SynergyComboKey 表示一组 Skill 组合的标识（排序后逗号拼接）
  type SynergyComboKey string

  // NewComboKey 根据 Skill 名称列表创建确定性组合键。
  // 名称排序后用逗号拼接，保证 {A,B} 和 {B,A} 生成相同 key。
  func NewComboKey(skills []string) SynergyComboKey

  // SynergyRecord 记录一次 Skill 组合执行的结果。
  type SynergyRecord struct {
      ComboKey   SynergyComboKey `json:"combo_key"`    // 组合标识
      Skills     []string        `json:"skills"`       // 参与的 Skill 名称列表
      Passed     bool            `json:"passed"`       // SLA 是否通过
      TokensUsed int             `json:"tokens_used"`  // token 消耗
      DurationMs int64           `json:"duration_ms"`  // 执行时长（毫秒）
      Timestamp  time.Time       `json:"timestamp"`    // 记录时间
  }
  ```

- [x] 1.2 单元测试：
  - `TestNewComboKey_SortedDeterministic` -- {B,A} 和 {A,B} 返回相同 key
  - `TestNewComboKey_SingleSkill` -- 单 Skill 也能正常生成 key
  - `TestNewComboKey_Empty` -- 空列表返回空字符串

### Task 2: SynergyMatrix 存储引擎（AC: #1, #6, #7）

- [x] 2.1 在 `kernel/synergy_matrix.go` 中实现 SynergyMatrix：

  ```go
  // SynergyMatrix 管理 Skill 组合的历史表现数据。
  // 数据持久化为 JSON Lines 文件，路径 $PROJECT/.rnix/reputation/synergy-matrix.json。
  type SynergyMatrix struct {
      mu       sync.Mutex
      filePath string // $PROJECT/.rnix/reputation/synergy-matrix.json
  }

  func NewSynergyMatrix(reputationDir string) *SynergyMatrix

  // RecordCombo 追加一条组合执行记录。
  func (m *SynergyMatrix) RecordCombo(record SynergyRecord) error

  // GetAllRecords 读取全部历史记录。
  func (m *SynergyMatrix) GetAllRecords() ([]SynergyRecord, error)
  ```

  **存储策略：**
  - 复用 ReputationStore 的 JSON Lines 模式：一行一条 JSON 记录，追加写入
  - 文件路径放在 `$PROJECT/.rnix/reputation/` 目录下（与声誉数据同级），因为组合矩阵是声誉数据的延伸
  - 文件名 `synergy-matrix.json`（所有组合记录集中在一个文件中，按组合 key 区分）

- [x] 2.2 单元测试：
  - `TestSynergyMatrix_RecordAndRead` -- 写入 3 条记录，读回全部
  - `TestSynergyMatrix_EmptyFile` -- 文件不存在时返回空切片
  - `TestSynergyMatrix_ConcurrentWrites` -- 多 goroutine 并发写入不丢数据

### Task 3: ComboSummary 统计计算（AC: #2, #3, #4）

- [x] 3.1 在 `kernel/synergy_matrix.go` 中实现统计聚合：

  ```go
  // ComboSummary 组合矩阵中一个 Skill 组合的统计摘要。
  type ComboSummary struct {
      ComboKey        SynergyComboKey `json:"combo_key"`
      Skills          []string        `json:"skills"`
      SuccessRate     float64         `json:"success_rate"`      // 组合成功率
      AvgTokens       int             `json:"avg_tokens"`        // 平均 token 消耗
      TotalExecutions int             `json:"total_executions"`  // 使用频次
      AvgSoloRate     float64         `json:"avg_solo_rate"`     // 各 Skill 单独平均成功率
      TokenImprovement float64        `json:"token_improvement"` // token 效率提升百分比
      Recommended     bool            `json:"recommended"`       // 推荐组合标记
  }

  // GetComboSummaries 计算所有组合的统计摘要。
  // 按推荐度排序：recommended 在前，然后按成功率降序。
  func (m *SynergyMatrix) GetComboSummaries() ([]ComboSummary, error)
  ```

  **计算逻辑：**
  1. 按 ComboKey 分组聚合所有记录
  2. 计算每个组合的成功率、平均 token、执行次数
  3. 对于"单 Skill"组合（key 只含一个 Skill），记录其成功率作为 solo baseline
  4. 对于多 Skill 组合，计算 `AvgSoloRate` = 各参与 Skill 的 solo 成功率的平均值
  5. `TokenImprovement` = (solo_avg_tokens - combo_avg_tokens) / solo_avg_tokens * 100
  6. `Recommended` = 组合成功率 > AvgSoloRate + 0.10（10%以上提升）

- [x] 3.2 单元测试：
  - `TestGetComboSummaries_BasicStats` -- 验证成功率、平均 token 计算
  - `TestGetComboSummaries_Recommended` -- 组合优于单 Skill 10% 以上标记推荐
  - `TestGetComboSummaries_NotRecommended` -- 差距不足 10% 不标记
  - `TestGetComboSummaries_NoSoloData` -- 无单 Skill 数据时，AvgSoloRate=0，Recommended=false
  - `TestGetComboSummaries_SortOrder` -- 推荐在前、成功率降序
  - `TestGetComboSummaries_Empty` -- 空数据返回空切片

### Task 4: Compose 引擎集成（AC: #1）

- [x] 4.1 修改 `compose/engine.go`：
  - 在 `Engine` 结构体新增 `synergyMatrix *kernel.SynergyMatrix` 字段
  - 新增 `SetSynergyMatrix(m *kernel.SynergyMatrix)` setter 方法
  - 在 `executeNode` 完成后，如果智能体加载了多个 Skill，且有 SLA 评估结果，则调用 `synergyMatrix.RecordCombo` 记录

  **记录时机：** 在现有 `reputationStore.RecordResult` 调用之后，追加 synergy 记录。获取 Skill 名称列表的方式：从 `agentInfo.Skills` 中提取。

- [x] 4.2 单元测试（不依赖真实 Compose 执行）：
  - 验证 `SetSynergyMatrix` 后字段正确设置
  - 验证 `synergyMatrix` 为 nil 时不 panic（nil 保护）

### Task 5: IPC 协议扩展（AC: #2, #5）

- [x] 5.1 在 `ipc/protocol.go` 新增：

  ```go
  MethodSynergyList Method = "synergy_list"

  type SynergyListRequest struct{}

  type SynergyListResponse struct {
      Combos []kernel.ComboSummary `json:"combos"`
  }
  ```

- [x] 5.2 在 `ipc/server.go` 新增 handler：
  - `handleSynergyList` -- 调用 `synergyMatrix.GetComboSummaries()` 返回结果
  - 在 `dispatch` map 中注册 `MethodSynergyList`
  - 在 `Server` 结构体增加 `synergyMatrix *kernel.SynergyMatrix` 字段 + setter

- [x] 5.3 在 `ipc/client.go` 新增客户端方法：

  ```go
  func (c *Client) SynergyList() (*SynergyListResponse, error)
  ```

- [x] 5.4 单元测试（protocol 层）：
  - `TestMethodSynergyList_Constant` -- 验证常量值 "synergy_list"

### Task 6: CLI 命令 `rnix synergy list`（AC: #2, #5, #6）

- [x] 6.1 在 `cmd/rnix/synergy.go` 新建 `synergy` 命令组和 `synergy list` 子命令：

  ```go
  var synergyCmd = &cobra.Command{
      Use:   "synergy",
      Short: "Skill synergy combination management",
  }

  var synergyListCmd = &cobra.Command{
      Use:   "list",
      Short: "Show skill synergy combination matrix",
      RunE:  runSynergyList,
  }
  ```

  **终端输出格式：**
  ```
  SKILLS               SUCCESS  AVG TOKENS  EXECUTIONS  VS SOLO  TOKEN GAIN  STATUS
  code-analysis,review   85.0%       1,200          20   +15.0%      +12.3%  recommended
  analysis,security      72.0%       1,500          10    +8.0%       +5.1%  -
  ```

- [x] 6.2 单元测试：
  - `TestRunSynergyList_NoData` -- 无数据时输出 "No synergy combination data available."
  - `TestRunSynergyList_JSON` -- JSON 模式输出正确格式
  - `TestRunSynergyList_Table` -- 表格模式输出包含正确列

### Task 7: Daemon 初始化集成（AC: #1, #7）

- [x] 7.1 在 `cmd/rnix/main.go` 的 `runDaemon` 中：
  - 创建 `SynergyMatrix` 实例（复用 reputation 目录）
  - 调用 `engine.SetSynergyMatrix(matrix)` 和 `server.SetSynergyMatrix(matrix)`

- [x] 7.2 确认现有 daemon 启动流程不受影响（回归验证）

## Dev Notes

### 核心设计决策

**组合矩阵数据存储在 reputation 目录下。** 设计选择：
1. 文件路径 `$PROJECT/.rnix/reputation/synergy-matrix.json`
2. 与 ReputationStore 的 agent 声誉文件同目录——组合矩阵是声誉数据的自然延伸
3. 使用相同的 JSON Lines 格式（一行一条记录），与 `ReputationStore.RecordResult` 模式一致
4. 不复用 ReputationStore 的代码——SynergyMatrix 是独立结构体，避免 ReputationStore 职责膨胀

**ComboKey 设计：排序后逗号拼接。** 设计选择：
1. `NewComboKey(["review", "analysis"])` → `"analysis,review"`（字母序）
2. 保证 {A,B} 和 {B,A} 生成相同 key——数据聚合依赖此确定性
3. 单 Skill 执行也记录——作为 "solo baseline" 用于对比计算

**推荐算法简单直接。** 设计选择：
1. `Recommended` = 组合成功率 > 各 Skill 单独平均成功率 + 10%
2. 10% 阈值来自 Epic 21 AC（"显著优于单 Skill"）
3. 无 solo baseline 数据时不标记推荐——避免误导

**记录时机在 Compose executeNode 完成后。** 设计选择：
1. 在 `reputationStore.RecordResult` 之后追加——确保声誉记录和组合记录同步
2. 只记录有多个 Skill 的执行——单 Skill 执行由 ReputationStore 单独覆盖
3. 同时也记录单 Skill 的执行到 SynergyMatrix——用作 solo baseline 对比数据

### 架构合规

- **依赖方向**：`kernel/synergy_matrix.go` 仅使用标准库（sync, encoding/json, os, path/filepath, sort, bufio, time）
- **包边界**：`compose/ → kernel/` 依赖已存在（通过 ReputationStore），新增 SynergyMatrix 无新依赖方向
- **IPC 扩展标准步骤**：protocol.go → server.go → client.go → cmd/rnix/synergy.go（4 步骤严格遵循）
- **JSON 输出**：字段名全部 `snake_case`，包装在 `JSONResponse` 中
- **持久化路径**：遵循 `$PROJECT/.rnix/reputation/` 目录规范
- **向后兼容**：新文件 `synergy-matrix.json` 不影响现有 `{agent}.json` 文件

### 关键文件变更

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `kernel/synergy_matrix.go` | **新建** | SynergyComboKey、SynergyRecord、SynergyMatrix、ComboSummary 类型和存储引擎 |
| `kernel/synergy_matrix_test.go` | **新建** | 组合矩阵单元测试 |
| `compose/engine.go` | 修改 | 新增 synergyMatrix 字段、SetSynergyMatrix setter、executeNode 中追加记录 |
| `ipc/protocol.go` | 修改 | 新增 MethodSynergyList 常量和请求/响应类型 |
| `ipc/server.go` | 修改 | 新增 synergyMatrix 字段、setter、handleSynergyList handler |
| `ipc/client.go` | 修改 | 新增 SynergyList() 客户端方法 |
| `cmd/rnix/synergy.go` | **新建** | synergy 命令组 + synergy list 子命令 |
| `cmd/rnix/synergy_test.go` | **新建** | CLI 单元测试 |
| `cmd/rnix/main.go` | 修改 | runDaemon 中初始化 SynergyMatrix 并注入 |

### 复用模式

- **JSON Lines 持久化模式**：复用 `ReputationStore`（`kernel/reputation.go`）的 JSON Lines 文件格式和读写模式（`os.OpenFile` 追加写 + `bufio.Scanner` 逐行读）
- **IPC 扩展 4 步标准**：复用 `MethodReputationStatus`（Story 21.3）的 IPC 扩展模式
- **CLI 命令模式**：复用 `cmd/rnix/reputation.go` 的 Cobra 命令结构（flagJSON 检测、daemon 连接、表格输出）
- **Compose Engine setter 注入**：复用 `SetReputationStore` 的 setter 注入模式
- **SLAResult 数据流**：复用 compose `executeNode` 中已有的 SLAResult 生成链路

### 从 Story 21.4 继承的经验

- **向后兼容是关键**：21.4 的 `Synergies` 字段为空时完全不改变行为——本 Story 的 SynergyMatrix 为 nil 时完全不记录，不 panic
- **nil 保护**：21.4 的 `DetectSynergies` 接受 nil slice——本 Story 的 `RecordCombo` 检查 `synergyMatrix != nil`
- **纯函数偏好**：21.4 的 `DetectSynergies` 是纯函数——本 Story 的 `GetComboSummaries` 也尽量无副作用（只读数据后计算）
- **测试覆盖充分**：21.4 有 24 个测试——本 Story 也需覆盖正向、反向、边界、并发场景
- **不引入新外部依赖**：21.4 全部用标准库——本 Story 同样无新依赖

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| SynergyMatrix.RecordCombo | compose.executeNode | 集成：SLA 评估后追加 synergy 记录 | 是 |
| SynergyMatrix.RecordCombo | ReputationStore.RecordResult | 顺序依赖：reputation 记录之后追加 synergy 记录 | 是 |
| SynergyMatrix | ReputationStore | 共存：独立存储文件，同目录 | 是 |
| rnix synergy list | IPC Server | 标准 IPC 调用链 | 是 |
| synergy list --json | JSONResponse | 复用 JSON 输出模式 | 是 |
| SynergyMatrix | DetectSynergies | 独立：检测发生在 prompt 组装时，记录发生在执行完成后 | 否 |
| SynergyMatrix | BudgetPool | 独立：组合矩阵不影响 token 预算分配 | 否 |
| synergy 命令组 | reputation 命令 | 独立：不同的 CLI 命令，不共享 handler | 否 |
| SynergyMatrix 文件 | daemon 重启 | 持久化：daemon 重启后重新加载文件，数据不丢失 | 是 |

### Project Structure Notes

- `kernel/synergy_matrix.go` 新建在 kernel 包——与 `kernel/reputation.go` 同级，职责相关
- `cmd/rnix/synergy.go` 新建在 cmd/rnix 目录——与 `cmd/rnix/reputation.go` 同级，CLI 命令对称
- 测试文件与源文件同目录——遵循 Go 惯例

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-21-token经济声誉与skill协同-token-economy-reputation-skill-synergy.md#Story 21.5]
- [Source: _bmad-output/implementation-artifacts/21-4-skill-synergy-declaration-and-auto-detection.md] -- 前序 Story 实现
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#文件持久化路径模式]
- [Source: _bmad-output/project-context.md#IPC 扩展标准步骤]
- [Source: kernel/reputation.go] -- ReputationStore JSON Lines 模式参考
- [Source: kernel/sla.go] -- SLAResult 类型定义
- [Source: compose/engine.go] -- executeNode SLA 评估链路
- [Source: ipc/protocol.go#MethodReputationStatus] -- IPC 协议扩展参考
- [Source: ipc/server.go#handleReputationStatus] -- IPC handler 模式参考
- [Source: ipc/client.go#ReputationStatus] -- IPC 客户端方法参考
- [Source: cmd/rnix/reputation.go] -- CLI 命令模式参考
- [Source: skills/synergy.go] -- DetectSynergies 检测函数（Story 21.4）
- [Source: agents/types.go#SystemPrompt] -- synergy 检测调用点

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- 实现 SynergyComboKey、SynergyRecord、SynergyMatrix、ComboSummary 全部类型和方法（kernel/synergy_matrix.go）
- SynergyMatrix 使用 JSON Lines 持久化模式，文件路径 $PROJECT/.rnix/reputation/synergy-matrix.json
- NewComboKey 排序后逗号拼接，保证 {A,B} 和 {B,A} 确定性相同
- GetComboSummaries 实现完整统计计算：成功率、平均 token、solo baseline 对比、token 效率提升、推荐标记
- Compose Engine 集成：在 executeNode SLA 评估后追加 synergy 记录
- IPC 扩展完整 4 步：protocol.go（MethodSynergyList 常量 + 请求/响应类型）→ server.go（handleSynergyList handler + SetSynergyMatrix setter）→ client.go（SynergyList 方法）→ cmd/rnix/synergy.go（CLI 命令）
- Daemon 初始化：runDaemon 中创建 ReputationStore 和 SynergyMatrix 实例并注入 server
- 全部 28 个 ATDD 测试通过，20 个包零回归，lint + vet + build 全部通过

#### Code Review 修复（CR Pass）

- **TokenImprovement 浮点精度修复**：`kernel/synergy_matrix.go` 中 `GetComboSummaries` 的 token 效率计算从整数除法改为浮点除法，避免精度丢失（`soloTokenSum / soloCount` → `float64(soloTokenSum) / float64(soloCount)`）

### Change Log

- 2026-03-14: Story 21.5 全部 7 个 Task 实现完成，28 个 ATDD 测试绿色通过
- 2026-03-14: Code Review 通过，修复 TokenImprovement 浮点精度问题，状态 → done

### File List

- kernel/synergy_matrix.go（新建）
- kernel/atdd_21_5_synergy_matrix_test.go（新建，17 个测试）
- compose/engine.go（修改：新增 synergyMatrix 字段 + SetSynergyMatrix setter + executeNode 中追加 synergy 记录）
- compose/atdd_21_5_synergy_engine_test.go（新建，2 个测试）
- ipc/protocol.go（修改：新增 MethodSynergyList + SynergyListRequest/Response）
- ipc/server.go（修改：新增 synergyMatrix 字段 + SetSynergyMatrix setter + handleSynergyList handler + dispatch 注册）
- ipc/client.go（修改：新增 SynergyList 方法）
- ipc/atdd_21_5_synergy_ipc_test.go（修改：修复 lint 问题，5 个测试）
- cmd/rnix/synergy.go（新建）
- cmd/rnix/atdd_21_5_synergy_cmd_test.go（新建，4 个测试）
- cmd/rnix/main.go（修改：runDaemon 中初始化 ReputationStore + SynergyMatrix 并注入 server）
