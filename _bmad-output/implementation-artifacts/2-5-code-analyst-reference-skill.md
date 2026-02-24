# Story 2.5: code-analyst 参考 Skill

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want 一个预装的 code-analyst Skill 作为参考实现,
So that 我可以立即使用 Crux 分析代码并作为编写自定义 Skill 的模板。

## Acceptance Criteria

1. **manifest.yaml 完整** — Given `lib/skills/code-analyst/manifest.yaml` 已创建，When 查看 manifest 内容，Then 包含 `name: code-analyst`、`tools: ["/dev/fs", "/dev/shell"]`、`models.provider: claude`、`models.preferred: sonnet`、`context_budget` 字段
2. **instructions.md 专业** — Given `lib/skills/code-analyst/instructions.md` 已创建，When 查看 instructions 内容，Then 包含代码分析的系统指令（角色定义、分析策略、输出格式要求），指令足够具体使 LLM 能输出结构化分析结果
3. **端到端分析** — Given code-analyst Skill 已加载，When 执行 `crux "分析 ./kernel/kernel.go" --skill=code-analyst`，Then 智能体读取目标文件，进行分析，输出结构化的分析结果，And 能够识别至少 1 个可验证的真实代码问题（FR27）
4. **Skill 加载验证** — Given `skills/testdata/mock-skill/` 已存在，When 运行 Skill 加载器测试，Then 使用 mock-skill 作为测试 fixture 验证加载流程（已有测试，本 Story 新增针对真实 code-analyst Skill 的加载测试）
5. **SkillLoader 路径兼容** — Given CLI 中 `SkillLoader("lib/skills")` 已初始化（cmd/crux/main.go:193），When 调用 `Load("code-analyst")`，Then 正确找到并加载 `lib/skills/code-analyst/` 下的 manifest.yaml 和 instructions.md

## Tasks / Subtasks

- [x] Task 1: 创建 code-analyst manifest.yaml (AC: #1, #5)
  - [x] 1.1 在 `lib/skills/code-analyst/manifest.yaml` 中定义完整的 Skill 元信息
  - [x] 1.2 删除 `lib/skills/code-analyst/.gitkeep`（manifest.yaml 替代占位）
  - [x] 1.3 验证 manifest 格式符合 `SkillManifest` 类型定义（skills/types.go:10-17）

- [x] Task 2: 创建 code-analyst instructions.md (AC: #2)
  - [x] 2.1 在 `lib/skills/code-analyst/instructions.md` 中编写专业的代码分析 system prompt
  - [x] 2.2 instructions 必须包含：角色定义、分析维度、输出格式模板、工具使用指南
  - [x] 2.3 instructions 中明确指导 LLM 使用 `/dev/fs` 读取文件和 `/dev/shell` 执行命令
  - [x] 2.4 输出格式要求结构化（问题分类、严重等级、代码位置、修复建议）

- [x] Task 3: Skill 加载单元测试 (AC: #4, #5)
  - [x] 3.1 `skills/loader_test.go` 新增 `TestSkillLoader_Load_RealCodeAnalyst` — 验证真实 code-analyst Skill 的完整加载流程
  - [x] 3.2 验证 manifest 字段值正确（name="code-analyst"、tools=["/dev/fs", "/dev/shell"]）
  - [x] 3.3 验证 instructions 非空且包含关键指令关键词
  - [x] 3.4 使用相对路径 `../lib/skills` 作为 basePath 加载

- [x] Task 4: 集成测试 (AC: #3)
  - [x] 4.1 `cmd/crux/integration_test.go` 新增 `TestE2E_CodeAnalystSkill` — 验证端到端流程
  - [x] 4.2 使用 mock LLM 驱动模拟 code-analyst 行为（不依赖真实 Claude Code CLI）
  - [x] 4.3 验证 Skill instructions 被正确注入到 LLM 请求的 system prompt
  - [x] 4.4 验证 AllowedDevices 限制为 ["/dev/fs", "/dev/shell"]
  - [x] 4.5 验证模型自动选择为 "sonnet"（来自 manifest.models.preferred）

- [x] Task 5: 全量回归测试 (AC: #1-5)
  - [x] 5.1 `go test -race ./skills/...` 通过
  - [x] 5.2 `go test -race ./kernel/...` 通过
  - [x] 5.3 `go test -race ./cmd/crux/...` 通过
  - [x] 5.4 `go test -race ./...` 全量通过
  - [x] 5.5 `go vet ./...` 无警告

## Dev Notes

### 核心实现分析

**Story 2.5 是 Epic 2 的收官 Story** — 在 Skill 系统（2.1 加载器 + 2.4 注入/权限白名单）完全就位的基础上，交付一个真实可用的参考 Skill。核心工作是**内容创作**（manifest.yaml + instructions.md），而非系统代码修改。

**关键 FR**: FR27 — 系统交付一个完整的参考 Skill（code-analyst），能够分析代码并识别至少 1 个可验证的真实代码问题。

**⚠️ 本 Story 不涉及任何内核代码修改** — Spawn 中的 Skill 加载、instructions 注入、设备权限白名单检查全部在 Story 2.4 中已实现并通过测试。本 Story 只需：
1. 创建 2 个内容文件（manifest.yaml + instructions.md）
2. 新增 2 组测试（Skill 加载 + 集成）

### 架构约束（必须遵循）

**文件位置（架构文档明确指定）：**
```
lib/skills/code-analyst/
├── manifest.yaml       # Skill 元信息
└── instructions.md     # 代码分析 system prompt
```

**manifest.yaml 格式要求（project-context.md + architecture.md）：**
- 字段名全 `snake_case`
- 缩进 2 空格
- 列表用序列语法 `- item`
- 后缀统一 `.yaml`（不用 `.yml`）

**SkillManifest 类型定义（skills/types.go:10-17）：**
```go
type SkillManifest struct {
    Name          string      `yaml:"name"`
    Description   string      `yaml:"description"`
    Tools         []string    `yaml:"tools"`
    Models        SkillModels `yaml:"models"`
    ContextBudget int         `yaml:"context_budget"`
}
```

**SkillModels 类型定义（skills/types.go:4-8）：**
```go
type SkillModels struct {
    Provider  string `yaml:"provider"`
    Preferred string `yaml:"preferred"`
    Fallback  string `yaml:"fallback"`
}
```

**依赖方向不变：** 本 Story 不引入新的包间依赖，仅在 `lib/skills/` 目录创建内容文件。

### manifest.yaml 具体内容

根据 Epics AC 定义（epics.md Story 2.5 AC #1）：

```yaml
name: code-analyst
description: "分析代码质量、识别潜在问题并提供改进建议"
tools:
  - "/dev/fs"
  - "/dev/shell"
models:
  provider: claude
  preferred: sonnet
  fallback: haiku
context_budget: 8192
```

**字段说明：**
- `name: code-analyst` — 与目录名一致，`--skill=code-analyst` 触发
- `tools` — 只需文件读取和 shell 执行，**不包含 `/dev/llm/claude`**（LLM 访问由内核自动提供，不在白名单中声明）
- `models.preferred: sonnet` — Claude Sonnet 模型，适合代码分析任务
- `context_budget: 8192` — 代码分析需要较大上下文（目前字段被定义但未被内核使用，为未来预留）

**⚠️ 重要：tools 白名单中不含 `/dev/llm/claude`**
这是设计正确的。LLM 设备 (`/dev/llm/claude`) 由 `kernel/kernel.go:227-411` 的 `reasonStep()` 内部直接使用，不通过工具调用路径。`proc.AllowedDevices` 仅限制 `ActionToolCall` 分支中的设备访问（kernel/kernel.go:345-368）。

### instructions.md 设计指南

**instructions.md 是注入到 LLM system prompt 的核心指令**，通过 `kernel/kernel.go:112-117` 的注入逻辑生效。指令质量直接决定 code-analyst 的分析能力。

**必须包含的内容维度：**

1. **角色定义** — 明确 LLM 是代码分析专家
2. **分析维度** — 指定分析的具体方面（Bug、安全、性能、可维护性、代码风格）
3. **工具使用指南** — 如何使用 `/dev/fs` 读取文件和 `/dev/shell` 执行辅助命令
4. **输出格式** — 结构化的分析报告格式，确保输出可验证
5. **工作流程** — 明确的分析步骤（读取文件 → 分析 → 输出报告）
6. **严重等级定义** — Critical / Warning / Info 三级分类

**工具使用上下文（reasonStep 中的工具调用流程）：**

当 LLM 返回 `ActionToolCall` 时（kernel/kernel.go:340-405）：
1. 权限检查：验证 `action.ToolPath` 在 `proc.AllowedDevices` 中
2. `vfs.Open(pid, action.ToolPath, O_RDWR)` — 打开设备
3. `vfs.Write(ctx, pid, fd, action.ToolData)` — 写入请求数据
4. `vfs.Read(pid, fd, 1<<20)` — 读取结果
5. `ctxMgr.AppendToolResult(ctxID, toolPath, result)` — 追加到上下文

因此 instructions.md 中指导 LLM 使用的工具调用格式必须与 `LLMResponse.Actions` 解析兼容。当前 Claude Code CLI 驱动（drivers/llm/claude_cli.go）解析 JSON 输出，工具调用通过 LLM 的 native tool_use 机制完成，instructions 只需描述工具的语义用途，不需要指定 JSON 格式。

### 与 testdata/mock-skill 的关系

**`skills/testdata/mock-skill/`** 是测试 fixture，已在 Story 2.1 和 2.4 中使用。它的 manifest 恰好也使用 `name: code-analyst`，但这是测试用途。

**真实 code-analyst 在 `lib/skills/code-analyst/`**，通过 CLI 的 `SkillLoader("lib/skills")` 加载。

**测试策略区分：**
- 已有测试使用 `skills/testdata/mock-skill` → 不修改
- 新增 Skill 加载测试使用 `../lib/skills/code-analyst` → 验证真实 Skill
- 集成测试使用 `../../lib/skills` basePath → 验证 CLI 路径

### 前序 Story 经验（必须吸收）

**Story 2.4 Code Review 关键发现：**

| 问题 | 2.4 经验 → 2.5 影响 |
|------|---------------------|
| H1: AC 文本与实现不一致 | **确保 AC 描述与实际行为匹配** |
| M1: AppendToolResult 错误处理 | **已修复，权限拒绝路径正确处理** |
| M2: 路径遍历防护 | **已修复，path.Clean() 在权限检查前执行** |
| M3: 集成测试覆盖 | **本 Story 需要新增集成测试验证真实 Skill** |

**Story 2.1 SkillLoader 实现模式：**
- `SkillLoader.Load(skillName)` 自动查找 `{basePath}/{skillName}/manifest.yaml` 和 `{basePath}/{skillName}/instructions.md`
- 验证 `manifest.Name` 非空
- 返回 `*SkillInfo{Manifest, Instructions}`

### Git 智能分析

**最近提交模式：**
```
ec380a8 Finalize Story 2.4: Skill Injection and Device Permission Whitelist Implementation
6fab22d Update Story 2.4 status to 'review'
28c73ae Add Story 2.4: Skill Injection and Device Permission Whitelist Implementation
45453a0 Update Story 2.3 status to 'done'
```

**模式总结：**
- 每个 Story 独立提交
- `go test -race ./...` 作为质量门禁
- Story 完成后更新 `sprint-status.yaml`

### 测试策略

**新增 Skill 加载测试（skills/loader_test.go）：**

```go
func TestSkillLoader_Load_RealCodeAnalyst(t *testing.T) {
    loader := NewSkillLoader("../lib/skills")
    info, err := loader.Load("code-analyst")
    // 验证加载成功
    // 验证 manifest 字段：name, tools, models
    // 验证 instructions 非空且包含关键词
}
```

**新增集成测试（cmd/crux/integration_test.go）：**

```go
func TestE2E_CodeAnalystSkill(t *testing.T) {
    // 使用 mock LLM 驱动
    // SkillLoader 指向 ../../lib/skills
    // Spawn with skills=["code-analyst"]
    // 验证 system prompt 包含 instructions 内容
    // 验证 AllowedDevices = ["/dev/fs", "/dev/shell"]
    // 验证 model = "sonnet"
}
```

**已有测试不修改：**
- `TestSkillLoader_Load_Success` — 使用 testdata/mock-skill，继续验证加载器基础功能
- `TestSpawn_WithSkill_*` — 使用 testdata/mock-skill，继续验证内核 Skill 注入
- `TestReasonStep_Permission*` — 继续验证权限白名单
- `TestE2E_WithSkill_InjectsInstructions` — 使用 testdata/mock-skill，继续验证集成路径

### NFR 合规

| NFR | 要求 | 本 Story 关系 |
|-----|------|--------------|
| NFR16 | Skill tools 白名单 | manifest.yaml tools 声明决定设备权限范围 |
| NFR17 | Skill tools 为最小安全边界 | code-analyst 仅声明 /dev/fs + /dev/shell |

### Project Structure Notes

**本 Story 修改/创建的文件：**

```
lib/skills/code-analyst/manifest.yaml     (创建 — Skill 元信息)
lib/skills/code-analyst/instructions.md   (创建 — 代码分析 system prompt)
skills/loader_test.go                     (修改 — 新增 1 个测试)
cmd/crux/integration_test.go             (修改 — 新增 1 个集成测试)
```

**不需要修改的文件：**
- `kernel/kernel.go` — Skill 加载、注入、权限检查已在 2.4 完成
- `kernel/process.go` — AllowedDevices 字段已存在
- `skills/loader.go` — SkillLoader 实现已完整
- `skills/types.go` — 类型定义已完整
- `cmd/crux/main.go` — --skill 标志和 SkillLoader 初始化已完成
- `vfs/` — 无需修改
- `drivers/` — 无需修改
- `context/` — 无需修改
- `go.mod` / `go.sum` — 无新依赖

**需要删除的文件：**
- `lib/skills/code-analyst/.gitkeep` — 被实际内容文件替代

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.5] — AC 和 User Story 定义
- [Source: _bmad-output/planning-artifacts/epics.md#FR27] — 交付 code-analyst 参考 Skill，识别真实代码问题
- [Source: _bmad-output/planning-artifacts/architecture.md#lib/skills/code-analyst/] — 文件位置定义
- [Source: _bmad-output/planning-artifacts/architecture.md#Skill 管理] — FR23-FR27 架构支撑
- [Source: _bmad-output/planning-artifacts/architecture.md#manifest.yaml 格式] — snake_case、缩进 2 空格
- [Source: _bmad-output/project-context.md#安全规则] — Skill tools 白名单规范
- [Source: _bmad-output/project-context.md#YAML 后缀] — 统一 .yaml
- [Source: skills/types.go:4-8] — SkillModels 类型
- [Source: skills/types.go:10-17] — SkillManifest 类型
- [Source: skills/types.go:19-23] — SkillInfo 类型
- [Source: skills/loader.go:38-76] — SkillLoader.Load() 方法
- [Source: kernel/kernel.go:100-122] — Spawn 中 Skill 加载与注入逻辑
- [Source: kernel/kernel.go:112-117] — instructions 注入到 SystemPrompt
- [Source: kernel/kernel.go:345-368] — reasonStep 中设备权限白名单检查
- [Source: cmd/crux/main.go:193] — SkillLoader("lib/skills") 初始化
- [Source: cmd/crux/main.go:202] — Spawn 调用传入 skillsList
- [Source: skills/testdata/mock-skill/manifest.yaml] — 现有测试 fixture 参考
- [Source: skills/testdata/mock-skill/instructions.md] — 现有测试 fixture 参考
- [Source: 2-4-skill-injection-and-device-permission-whitelist.md] — 前序 Story 完整经验

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

无异常。集成测试首次运行因缺少 `outputSuccess` 调用导致输出验证失败，添加后一次通过。

### Completion Notes List

- Task 1: 创建 `lib/skills/code-analyst/manifest.yaml`，包含 name、description、tools(["/dev/fs", "/dev/shell"])、models(provider: claude, preferred: sonnet, fallback: haiku)、context_budget: 8192。删除 .gitkeep 占位文件。
- Task 2: 创建 `lib/skills/code-analyst/instructions.md`，包含角色定义（高级代码审查工程师）、5 个分析维度（Bug/安全/性能/可维护性/风格）、工具使用指南（/dev/fs + /dev/shell）、工作流程、严重等级定义（Critical/Warning/Info）、结构化输出格式模板。
- Task 3: `skills/loader_test.go` 新增 `TestSkillLoader_Load_RealCodeAnalyst`，使用 `../lib/skills` 路径加载真实 code-analyst Skill，验证所有 manifest 字段和 instructions 关键词。9/9 tests PASS。
- Task 4: `cmd/crux/integration_test.go` 新增 `capturingMockLLMDriver`（捕获请求以验证注入内容）和 `TestE2E_CodeAnalystSkill`，验证 system prompt 注入、AllowedDevices=["/dev/fs", "/dev/shell"]、model="sonnet"。
- Task 5: `go test -race ./...` 全量通过，`go vet ./...` 无警告，零回归。

### File List

- `lib/skills/code-analyst/manifest.yaml` — 新建：Skill 元信息定义
- `lib/skills/code-analyst/instructions.md` — 新建：代码分析 system prompt
- `lib/skills/code-analyst/.gitkeep` — 删除：被实际内容文件替代
- `skills/loader_test.go` — 修改：新增 TestSkillLoader_Load_RealCodeAnalyst
- `cmd/crux/integration_test.go` — 修改：新增 capturingMockLLMDriver + TestE2E_CodeAnalystSkill
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — 修改：2-5 状态更新
- `_bmad-output/implementation-artifacts/2-5-code-analyst-reference-skill.md` — 修改：任务标记完成

### Change Log

- 2026-02-24: Story 2.5 实现完成 — 创建 code-analyst 参考 Skill（manifest.yaml + instructions.md），新增 2 组测试验证加载和端到端流程
