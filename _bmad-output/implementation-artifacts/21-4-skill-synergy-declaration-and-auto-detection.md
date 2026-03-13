# Story 21.4: Skill Synergy 声明与自动检测

Status: done

## Story

As a 平台构建者,
I want SKILL.md 可以声明 synergy 字段，系统在加载多个 Skill 时自动检测并激活协同效应,
So that Skill 组合能产生 1+1>2 的涌现能力。

## Acceptance Criteria

1. **AC1: Synergy 字段声明**
   - Given SKILL.md 中声明了 `synergy` 字段（YAML frontmatter list）
   - When SkillLoader 解析该 SKILL.md
   - Then 正确解析 synergy 声明列表到 SkillManifest.Synergies 字段
   - And 未声明 synergy 字段时默认为空切片（向后兼容）

2. **AC2: 自动检测 Synergy 组合**
   - Given 智能体同时加载两个 Skill（如 A 和 B），A 的 synergy 声明中有匹配 B 的条目
   - When 系统组装 system prompt 时
   - Then 自动检测到 synergy 命中，将涌现指令追加到 system prompt
   - And 涌现指令来自 synergy 声明中的 `instruction` 字段

3. **AC3: 多重 Synergy 同时命中**
   - Given 智能体加载 N 个有交叉 synergy 的 Skill
   - When 系统检测 synergy 组合
   - Then 所有命中的 synergy 指令都被追加（不遗漏）
   - And 同一条 synergy 指令不重复追加（去重）

4. **AC4: 性能要求**
   - Given 任意数量的 Skill synergy 声明
   - When 执行组合检测
   - Then 检测开销 <= 100ms（NFR46）

5. **AC5: 向后兼容**
   - Given 现有 SKILL.md 无 synergy 字段
   - When 被系统加载
   - Then 行为与当前完全一致——解析无错误，SystemPrompt 输出不变

## Tasks / Subtasks

### Task 1: 扩展 SkillManifest 类型（AC: #1, #5）

- [x] 1.1 在 `skills/types.go` 中新增 `SynergyDecl` 类型：

  ```go
  // SynergyDecl 声明与另一个 Skill 的协同效应。
  type SynergyDecl struct {
      With        string `yaml:"with"`        // 目标 Skill 名称
      Instruction string `yaml:"instruction"` // 涌现指令（追加到 system prompt）
  }
  ```

- [x] 1.2 在 `SkillManifest` 添加 `Synergies []SynergyDecl \`yaml:"synergy,omitempty"\`` 字段
  - YAML 字段名为 `synergy`（与 `_bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md` 中 Skill 元数据扩展模式一致）
  - 默认零值为 nil（空切片），向后兼容

- [x] 1.3 单元测试 `skills/types_test.go`：
  - `TestSkillManifest_Synergies_Empty` -- 无 synergy 字段，Synergies 为 nil
  - `TestSkillManifest_Synergies_Parsed` -- 有 synergy 字段，正确解析

### Task 2: SkillLoader 解析 synergy 字段（AC: #1, #5）

- [x] 2.1 验证 `skills/loader.go` 的现有 `loadAndParse` 方法无需改动
  - 现有 `yaml.Unmarshal` 到 `SkillManifest` 会自动解析新增的 `synergy` 字段
  - go-yaml 忽略未知字段 + 新增字段有零值默认 = 向后兼容

- [x] 2.2 单元测试 `skills/loader_test.go`（补充）：
  - `TestSkillLoader_LoadFull_WithSynergy` -- 加载含 synergy 的 SKILL.md，验证 Synergies 正确填充
  - `TestSkillLoader_LoadFull_WithoutSynergy` -- 加载无 synergy 的 SKILL.md（如现有 code-analysis），验证 Synergies 为 nil

### Task 3: Synergy 检测引擎（AC: #2, #3, #4）

- [x] 3.1 在 `skills/synergy.go` 新建 synergy 检测模块：

  ```go
  // DetectSynergies 检测已加载 Skill 列表中命中的 synergy 组合。
  // 返回去重后的涌现指令列表。
  // skills: 当前加载的全部 Skill 列表。
  func DetectSynergies(skills []*SkillInfo) []string
  ```

  **算法：**
  1. 构建 skill name set（O(N)）：`loaded := map[string]struct{}{}`
  2. 遍历每个 skill 的 Synergies 列表，检查 `decl.With` 是否在 loaded 中
  3. 命中则将 `decl.Instruction` 加入结果（用 map 去重）
  4. 返回排序后的去重指令列表（排序保证确定性）

  **复杂度：** O(N * M)，N = Skill 数量，M = 每个 Skill 的平均 synergy 声明数。对于合理规模（N < 100, M < 10），远低于 100ms 阈值。

- [x] 3.2 单元测试 `skills/synergy_test.go`：
  - `TestDetectSynergies_NoSynergies` -- 无 synergy 声明，返回空
  - `TestDetectSynergies_SingleMatch` -- A 声明 with: B，两者都加载，命中 1 条
  - `TestDetectSynergies_BidirectionalMatch` -- A→B 和 B→A 同时声明，两条都命中
  - `TestDetectSynergies_PartialLoad` -- A 声明 with: B，但 B 未加载，不命中
  - `TestDetectSynergies_MultipleMatches` -- 多个交叉 synergy，全部命中
  - `TestDetectSynergies_Dedup` -- 相同 instruction 只追加一次
  - `TestDetectSynergies_DeterministicOrder` -- 结果按字母序排列
  - `TestDetectSynergies_Performance` -- 100 个 Skill 各含 10 条 synergy，检测耗时 < 100ms

### Task 4: SystemPrompt 集成 synergy 指令（AC: #2, #3）

- [x] 4.1 修改 `agents/types.go` 的 `AgentInfo.SystemPrompt()` 方法：
  - 在现有逻辑（agent instructions + skill bodies）之后
  - 调用 `skills.DetectSynergies(a.Skills)` 获取涌现指令
  - 如果有命中的 synergy 指令，追加到 system prompt 末尾
  - 格式：`\n\n[Skill Synergy]\n\n` + 各条指令换行拼接

  ```go
  func (a *AgentInfo) SystemPrompt() string {
      var prompt strings.Builder
      prompt.WriteString(a.Instructions)
      for _, skill := range a.Skills {
          if skill.Body != "" {
              prompt.WriteString("\n\n" + skill.Body)
          }
      }
      // Synergy detection: append emergent instructions
      synergyInstructions := skills.DetectSynergies(a.Skills)
      if len(synergyInstructions) > 0 {
          prompt.WriteString("\n\n[Skill Synergy]\n\n")
          prompt.WriteString(strings.Join(synergyInstructions, "\n"))
      }
      return prompt.String()
  }
  ```

- [x] 4.2 单元测试 `agents/types_test.go`（补充）：
  - `TestAgentInfo_SystemPrompt_WithSynergy` -- Skill 有 synergy 命中时，prompt 包含 `[Skill Synergy]` 段落
  - `TestAgentInfo_SystemPrompt_NoSynergy` -- 无 synergy 时，prompt 与原逻辑完全一致（回归验证）
  - `TestAgentInfo_SystemPrompt_SynergyDedup` -- 重复 instruction 只出现一次

### Task 5: SKILL.md synergy 声明示例（文档验证）

- [x] 5.1 创建测试 fixture `skills/testdata/with-synergy/SKILL.md`：

  ```
  ---
  name: test-skill-a
  description: Test skill A with synergy
  allowed-tools: /dev/fs
  synergy:
    - with: test-skill-b
      instruction: "When both code-analysis and code-review skills are active, cross-reference findings from analysis in your review comments for comprehensive coverage."
  ---

  # Test Skill A

  Body content for test skill A.
  ```

- [x] 5.2 创建测试 fixture `skills/testdata/with-synergy-b/SKILL.md`：

  ```
  ---
  name: test-skill-b
  description: Test skill B with synergy
  allowed-tools: /dev/shell
  synergy:
    - with: test-skill-a
      instruction: "When both skills are loaded, prioritize reviewing files that were flagged by analysis."
  ---

  # Test Skill B

  Body content for test skill B.
  ```

## Dev Notes

### 核心设计决策

**Synergy 声明在 SKILL.md frontmatter。** 这遵循架构文档中 Skill 元数据扩展模式（`implementation-patterns-consistency-rules.md` 第 306-325 行）：
1. 新字段 `synergy` 追加到 frontmatter，不修改已有字段语义
2. 零值默认（未声明 = nil/空）= 不启用
3. go-yaml 解析器忽略未知字段（前向兼容）

**检测逻辑在 `skills/` 包。** 设计选择：
1. `DetectSynergies` 是纯函数，输入 `[]*SkillInfo`，输出 `[]string`
2. 不依赖 kernel、agents、compose 等包——遵循依赖方向（`agents/ → skills/`，不能反向）
3. 调用点在 `AgentInfo.SystemPrompt()`（agents 包）——agents 已依赖 skills，无新依赖引入

**涌现指令追加到 system prompt 末尾。** 设计选择：
1. 格式 `[Skill Synergy]` 段落标记，与现有 `[GDB Environment Variables]` 段落风格一致
2. 追加在 skill bodies 之后——涌现指令是对已有能力的增强
3. 指令文本完全由 SKILL.md 作者控制——系统只做检测和注入

**性能保证。** 设计选择：
1. 算法复杂度 O(N*M)，N=Skill 数, M=平均 synergy 声明数
2. 实际场景 N < 20, M < 5，检测耗时 << 1ms
3. 不引入缓存——每次 SystemPrompt() 调用时实时检测（调用频率低）

### 架构合规

- **依赖方向**：`skills/synergy.go` 仅使用 skills 包内类型 + 标准库，无新外部依赖
- **包边界**：agents → skills 依赖已存在，`SystemPrompt()` 调用 `skills.DetectSynergies()` 合规
- **SKILL.md frontmatter 扩展模式**：遵循 `implementation-patterns-consistency-rules.md` 的 Skill 元数据扩展规则
- **向后兼容**：无 synergy 字段的 SKILL.md 解析和行为完全不变
- **命名规范**：`SynergyDecl`（导出类型 PascalCase），`synergy`（YAML 字段全小写）

### 关键文件变更

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `skills/types.go` | 修改 | 新增 SynergyDecl 类型，SkillManifest 新增 Synergies 字段 |
| `skills/synergy.go` | **新建** | DetectSynergies 检测函数 |
| `skills/synergy_test.go` | **新建** | synergy 检测单元测试 |
| `agents/types.go` | 修改 | SystemPrompt() 集成 synergy 指令注入 |
| `agents/types_test.go` | **新建/修改** | SystemPrompt synergy 集成测试 |
| `skills/testdata/with-synergy/SKILL.md` | **新建** | 测试 fixture A |
| `skills/testdata/with-synergy-b/SKILL.md` | **新建** | 测试 fixture B |

### 复用模式

- **SKILL.md frontmatter 扩展模式**：复用 `implementation-patterns-consistency-rules.md` 中定义的 Skill 元数据扩展规则——只追加字段，不修改已有语义
- **go-yaml Unmarshal 自动解析**：现有 `loadAndParse` 无需改动，`yaml.Unmarshal` 自动处理新字段
- **SystemPrompt 段落追加模式**：复用 `[GDB Environment Variables]` 段落注入风格（在 `kernel/kernel.go` reasonStep 中）
- **纯函数检测模式**：`DetectSynergies` 无状态、无副作用，易于测试和复用

### 从 Story 21.3 继承的经验

- **向后兼容是关键**：21.3 的 `Alternatives`/`Candidates` 字段为空时完全不改变行为——本 Story 的 `Synergies` 同理
- **nil 保护**：21.3 中 `reputationStore` 为 nil 时优雅降级——本 Story 的 `DetectSynergies` 接受 nil slice，返回空
- **测试覆盖充分**：21.3 有 31 个测试——本 Story 也需覆盖所有正向、反向、边界场景
- **不引入新外部依赖**：21.3 全部用标准库——本 Story 同样无新依赖

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| DetectSynergies | SkillLoader.LoadFull | 依赖：synergy 字段在加载时解析 | 是 |
| SystemPrompt + synergy | AgentInfo.SystemPrompt | 扩展：synergy 指令追加到末尾 | 是 |
| synergy 与 AllowedTools | AgentInfo.AllowedTools | 独立：synergy 不影响工具白名单 | 是 |
| synergy 与 Compose Engine | compose.executeNode | 间接：通过 agentLoader → AgentInfo → SystemPrompt 传递 | 是 |
| synergy 与 reputation auto-select | 21.3 SelectBest | 独立：synergy 在选择之后、prompt 组装时触发 | 否 |
| synergy 与 SLA 评估 | 21.2 SLASpec | 独立：synergy 影响 prompt 内容，不影响 SLA 评估 | 否 |
| synergy 与 BudgetPool | 21.1 TokenBudget | 独立：synergy 指令追加可能增加 token 消耗，但由 BudgetPool 统一管控 | 否 |
| synergy 与 gdb set skills | gdb 热加载 | 需注意：gdb 热加载 skill 后，下次 SystemPrompt 调用会重新检测 synergy | 是 |
| synergy 与 MCP | agent MCP 配置 | 独立：synergy 不涉及 MCP 配置 | 否 |

### Project Structure Notes

- `skills/synergy.go` 新建在 skills 包——与 `skills/types.go`、`skills/loader.go` 同级，保持内聚
- 测试 fixture 放在 `skills/testdata/`——遵循 project-context.md 中的测试 fixtures 规则
- `agents/types.go` 修改 `SystemPrompt()` 方法——这是 synergy 注入的唯一调用点

### SKILL.md synergy 声明示例

```yaml
---
name: code-analysis
description: Analyze code quality and find bugs
allowed-tools: /dev/fs /dev/shell
synergy:
  - with: code-review
    instruction: "When both code-analysis and code-review skills are active, cross-reference analysis findings in review comments for comprehensive coverage."
  - with: security-audit
    instruction: "When both code-analysis and security-audit are active, prioritize security-relevant findings in analysis output."
---
```

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-21-token经济声誉与skill协同-token-economy-reputation-skill-synergy.md#Story 21.4]
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#Skill 元数据扩展模式]
- [Source: _bmad-output/project-context.md#SKILL.md 格式]
- [Source: skills/types.go] -- 现有 SkillManifest 和 SkillInfo 类型
- [Source: skills/loader.go] -- 现有 SkillLoader、parseSKILLMD、loadAndParse
- [Source: agents/types.go#SystemPrompt] -- 现有 system prompt 组装逻辑（synergy 注入点）
- [Source: agents/types.go#AllowedTools] -- 工具白名单聚合（synergy 不影响）
- [Source: agents/loader.go#Load] -- Agent 加载流程（skill 解析发生在这里）
- [Source: compose/engine.go#executeNode] -- Compose 引擎中 agentLoader 调用链
- [Source: _bmad-output/implementation-artifacts/21-3-reputation-system-and-auto-selection.md] -- 21.3 实现经验
- [Source: lib/skills/code-analysis/SKILL.md] -- 现有 SKILL.md 格式参考

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

### Completion Notes List

- Task 1: 在 `skills/types.go` 新增 `SynergyDecl` 结构体和 `SkillManifest.Synergies` 字段。YAML tag `synergy,omitempty` 确保向后兼容。ATDD 测试 21.4-UNIT-001/002/003 通过。
- Task 2: 验证 `skills/loader.go` 无需改动——`yaml.Unmarshal` 自动解析新增字段。ATDD 测试 21.4-UNIT-004/005/006 通过（含真实 code-analysis skill 回归）。
- Task 3: 新建 `skills/synergy.go`，实现 `DetectSynergies` 纯函数。算法 O(N*M) 构建 name set + 遍历检测 + map 去重 + 排序。ATDD 测试 21.4-UNIT-007~015 及 21.4-PERF-001 全部通过（100 skills x 10 synergies < 100ms）。
- Task 4: 修改 `agents/types.go` 的 `SystemPrompt()` 方法，在 skill bodies 之后调用 `skills.DetectSynergies()` 并追加 `[Skill Synergy]` 段落。ATDD 测试 21.4-INT-001~007 全部通过。
- Task 5: 测试 fixture 已由 ATDD 步骤预创建（`skills/testdata/with-synergy/` 和 `skills/testdata/with-synergy-b/`），验证通过。
- 全量回归：`make all`（lint + vet + 20 packages test + build）全部通过，零回归。
- 修复了 ATDD 测试文件中的 lint 问题（`interface{}` → `any`）。

### File List

- `skills/types.go` -- 修改：新增 SynergyDecl 类型，SkillManifest 新增 Synergies 字段
- `skills/synergy.go` -- **新建**：DetectSynergies 检测函数（review 修复：跳过空 instruction）
- `agents/types.go` -- 修改：SystemPrompt() 集成 synergy 指令注入
- `skills/atdd_21_4_synergy_decl_test.go` -- 修改：修复 lint（interface{} → any）
- `skills/atdd_21_4_synergy_detect_test.go` -- ATDD 测试（预创建，未修改）
- `agents/atdd_21_4_synergy_prompt_test.go` -- ATDD 测试（预创建，未修改）
- `skills/testdata/with-synergy/SKILL.md` -- 测试 fixture（预创建，未修改）
- `skills/testdata/with-synergy-b/SKILL.md` -- 测试 fixture（预创建，未修改）

## Change Log

- 2026-03-13: Story 21.4 实现完成——Skill Synergy 声明解析、自动检测引擎、SystemPrompt 集成。24 个 ATDD 测试全部通过，零回归。
- 2026-03-13: Code Review 完成——1 个 MEDIUM 问题已修复（空 instruction 跳过），1 个 LOW 记录（自引用 synergy 边界情况）。`make all` 全量通过。

## Senior Developer Review (AI)

**Reviewer:** Claude Opus 4.6 (adversarial code review)
**Date:** 2026-03-13

### Git vs Story 差异分析

| 类别 | 说明 | 严重度 |
|------|------|--------|
| `sprint-status.yaml` git 中有修改但未列入 File List | 流程文件，可接受 | 无 |

**差异数量：** 0 个实质差异（1 个流程文件排除）

### AC 验证结果

| AC | 状态 | 证据 |
|----|------|------|
| AC1: Synergy 字段声明 | IMPLEMENTED | `SynergyDecl` 类型 `skills/types.go:6-9`，`Synergies` 字段 `skills/types.go:17`，YAML tag `synergy,omitempty`。6 个单元测试通过（UNIT-001~006）。 |
| AC2: 自动检测 Synergy 组合 | IMPLEMENTED | `DetectSynergies` `skills/synergy.go:7-34`，`SystemPrompt()` `agents/types.go:65-80` 调用并追加 `[Skill Synergy]` 段落。INT-001/006/007 通过。 |
| AC3: 多重 Synergy 同时命中 | IMPLEMENTED | 测试 `TestDetectSynergies_MultipleMatches`（3 条交叉匹配）、`TestDetectSynergies_Dedup`（去重）、`TestDetectSynergies_DeterministicOrder`（排序确定性）全部通过。 |
| AC4: 性能 <= 100ms | IMPLEMENTED | `TestDetectSynergies_Performance`：100 skills x 10 synergies < 100ms 通过。 |
| AC5: 向后兼容 | IMPLEMENTED | 无 synergy 字段的 SKILL.md 解析无错误（UNIT-002/005/006），SystemPrompt 输出不变（INT-002/004）。 |

### Task 完成审计

| Task | 标记 | 实际状态 | 证据 |
|------|------|---------|------|
| 1.1 SynergyDecl 类型 | [x] | DONE | `skills/types.go:6-9` |
| 1.2 SkillManifest.Synergies | [x] | DONE | `skills/types.go:17` |
| 1.3 types 单元测试 | [x] | DONE | `atdd_21_4_synergy_decl_test.go` 3 个测试 |
| 2.1 loader 无需改动 | [x] | DONE | `skills/loader.go` 未修改，`yaml.Unmarshal` 自动解析 |
| 2.2 loader 单元测试 | [x] | DONE | `atdd_21_4_synergy_decl_test.go` UNIT-004/005/006 |
| 3.1 synergy.go 检测模块 | [x] | DONE | `skills/synergy.go` 34 行，O(N*M) 算法 |
| 3.2 synergy 单元测试 | [x] | DONE | `atdd_21_4_synergy_detect_test.go` 9 个测试 |
| 4.1 SystemPrompt 集成 | [x] | DONE | `agents/types.go:64-80` |
| 4.2 SystemPrompt 测试 | [x] | DONE | `atdd_21_4_synergy_prompt_test.go` 7 个测试 |
| 5.1 fixture with-synergy | [x] | DONE | `skills/testdata/with-synergy/SKILL.md` |
| 5.2 fixture with-synergy-b | [x] | DONE | `skills/testdata/with-synergy-b/SKILL.md` |

### 代码质量深审

**安全性：** 无注入风险。synergy instruction 来自本地 SKILL.md 文件（已有路径遍历防护），不涉及外部输入。

**性能：** O(N*M) 算法对合理规模 (N<100, M<10) 远低于阈值。无缓存需求。

**错误处理：** nil/空 slice 输入优雅处理（返回 nil），无 panic 路径。

**代码质量：** 纯函数设计，无状态、无副作用。依赖方向合规（agents → skills）。代码简洁，命名清晰。

**测试质量：** 24 个 ATDD 测试覆盖正向、反向、边界、性能场景。测试使用真实断言（非占位符）。

### 发现的问题

| # | 严重度 | 描述 | 处理 |
|---|--------|------|------|
| 1 | MEDIUM | `DetectSynergies` 未跳过空 `Instruction` 字符串。空 instruction 的 synergy 匹配会导致 SystemPrompt 追加无意义的 `[Skill Synergy]` 段落。 | **已修复** — 在 `skills/synergy.go` 添加 `if decl.Instruction == "" { continue }` |
| 2 | LOW | 自引用 synergy（`with` 等于自身 name）会触发。AC 未要求阻止，属 SKILL.md 作者错误而非系统 bug。 | 记录，不修复 |

### 结论

**Approve** — 所有 5 个 AC 已实现，所有 11 个 Task 已完成，24 个 ATDD 测试通过。1 个 MEDIUM 问题已修复，1 个 LOW 问题记录。`make all` 全量通过（20 packages, 0 lint issues）。
