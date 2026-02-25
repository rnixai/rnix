# Story 2.6: Agent 抽象层与 Skill 标准化

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want Agent 和 Skill 是清晰分离的两个概念，且 Skill 遵循行业标准格式,
So that Agent 定义"我是谁"（身份+策略+模型），Skill 定义"如何做 X"（程序性知识+工具权限），且 Skill 可与 30+ AI 工具生态兼容。

## Acceptance Criteria

1. **agents/types.go 类型正确** — Given agents/types.go 已创建，When 查看类型定义，Then AgentManifest 包含 Name/Description/Models(AgentModels)/ContextBudget/Skills([]string) 字段，AgentModels 包含 Provider/Preferred/Fallback，AgentInfo 包含 Manifest/Instructions/Skills([]*SkillInfo) 字段
2. **agents/loader.go 完整加载** — Given agents/loader.go 已创建，When 调用 AgentLoader.Load("code-analyst")，Then 正确加载 agent.yaml + instructions.md + 解析 Skills 引用列表 + 对每个 Skill 调用 SkillLoader.LoadFull() + 聚合所有 Skill 的 AllowedTools 为统一权限白名单
3. **skills/types.go 精简** — Given skills/types.go 已更新，When 查看类型定义，Then SkillManifest 仅包含 Name/Description/AllowedTools([]string)/Metadata(map[string]string) 字段（不再有 Models 和 ContextBudget）
4. **skills/loader.go 标准格式** — Given skills/loader.go 已更新，When 调用 SkillLoader.LoadFull("code-analysis")，Then 正确解析 SKILL.md 文件（YAML frontmatter + Markdown body），返回包含 frontmatter 字段和 body 的 SkillInfo
5. **参考 Agent 定义** — Given lib/agents/code-analyst/ 已创建，When 查看 agent.yaml，Then 包含 name: code-analyst、models(provider/preferred/fallback)、context_budget: 8192、skills: [code-analysis]；When 查看 instructions.md，Then 包含 Agent 角色定义和行为策略
6. **参考 Skill 标准格式** — Given lib/skills/code-analysis/SKILL.md 已创建，When 查看内容，Then frontmatter 包含 name: code-analysis、description、allowed-tools: /dev/fs /dev/shell；body 包含代码分析的程序性知识和工作流
7. **Spawn 签名更新** — Given kernel/kernel.go 已更新，When 查看 Spawn 函数签名，Then 接受 AgentInfo 而非 skills []string 参数
8. **CLI --agent flag** — Given cmd/crux/main.go 已更新，When 执行 crux "分析代码" --agent=code-analyst，Then AgentLoader 加载 Agent 定义，Spawn 使用 AgentInfo
9. **权限白名单聚合** — Given Agent 引用多个 Skill，When AgentLoader 聚合权限，Then 所有 Skill 的 AllowedTools 合并去重后作为进程的 AllowedDevices
10. **端到端验证** — Given 所有修改完成，When 执行 crux "分析代码" --agent=code-analyst，Then 智能体正确启动、使用 agent models 偏好、注入 agent + skill instructions、设备权限限制正确
11. **所有现有测试通过** — Given 测试已更新，When 执行 go test -race ./...，Then 所有测试通过，无回归

## Tasks / Subtasks

- [x] Task 1: 创建 agents/ Go 包 — 类型定义与加载器 (AC: #1, #2)
  - [x] 1.1 创建 `agents/types.go`：定义 AgentManifest、AgentModels、AgentInfo 类型
  - [x] 1.2 创建 `agents/loader.go`：AgentLoader 结构体，Load() 方法加载 agent.yaml + instructions.md + Skill 引用解析 + tools 聚合
  - [x] 1.3 创建 `agents/loader_test.go`：单元测试验证 Agent 加载器

- [x] Task 2: 重构 skills/ 包 — SKILL.md 标准格式 (AC: #3, #4)
  - [x] 2.1 更新 `skills/types.go`：SkillManifest 简化为 Name/Description/AllowedTools/Metadata，移除 Models 和 ContextBudget
  - [x] 2.2 更新 `skills/loader.go`：解析 SKILL.md 格式（YAML frontmatter + Markdown body），支持渐进式加载
  - [x] 2.3 更新 `skills/testdata/mock-skill/`：将 manifest.yaml + instructions.md 转换为 SKILL.md 格式
  - [x] 2.4 更新 `skills/loader_test.go`：适配 SKILL.md 格式的测试用例

- [x] Task 3: 创建参考 Agent 和 Skill 内容 (AC: #5, #6)
  - [x] 3.1 创建 `lib/agents/code-analyst/agent.yaml`：Agent 配置（name、models、context_budget、skills 引用）
  - [x] 3.2 创建 `lib/agents/code-analyst/instructions.md`：从现有 `lib/skills/code-analyst/instructions.md` 迁移角色定义部分
  - [x] 3.3 创建 `lib/skills/code-analysis/SKILL.md`：从现有内容提取程序性知识，转换为 Agent Skills 标准格式
  - [x] 3.4 删除旧文件：`lib/skills/code-analyst/manifest.yaml` 和 `lib/skills/code-analyst/instructions.md`

- [x] Task 4: 更新 kernel 层 — Spawn 签名调整 (AC: #7, #9)
  - [x] 4.1 更新 `kernel/kernel.go`：Spawn 函数签名从 `(intent, skills []string, opts)` 改为 `(intent, agent *agents.AgentInfo, opts)`
  - [x] 4.2 更新 Spawn 内部逻辑：从 AgentInfo 获取 Models、Instructions、AllowedTools
  - [x] 4.3 更新 `kernel/kernel_test.go`：适配新 Spawn 签名

- [x] Task 5: 更新 CLI 层 — --agent flag (AC: #8, #10)
  - [x] 5.1 更新 `cmd/crux/main.go`：--skill flag 改为 --agent flag，初始化 AgentLoader
  - [x] 5.2 更新 AgentLoader 和 SkillLoader 的依赖注入：AgentLoader 持有 SkillLoader 引用
  - [x] 5.3 更新 `cmd/crux/integration_test.go`：适配 Agent 加载的集成测试

- [x] Task 6: 全量回归测试与验收 (AC: #11)
  - [x] 6.1 `go test -race ./skills/...` 通过
  - [x] 6.2 `go test -race ./agents/...` 通过
  - [x] 6.3 `go test -race ./kernel/...` 通过
  - [x] 6.4 `go test -race ./cmd/crux/...` 通过
  - [x] 6.5 `go test -race ./...` 全量通过
  - [x] 6.6 `go vet ./...` 无警告

## Dev Notes

### 变更起源

本 Story 源自 **Sprint Change Proposal 2026-02-25**（已批准），不在原始 epics.md 中。变更提案完整定义了 Agent/Skill 分层架构的设计决策和实施路径。

**核心问题：**
1. Skill 概念职责混淆 — SkillManifest 包含 Models/ContextBudget（智能体级配置），不是共享库该有的
2. 缺少 Agent 抽象层 — 只有 Process（运行时）和 Skill（全能定义），缺少"Agent"（可执行程序）
3. Skill 格式与 Agent Skills 行业标准不兼容 — 当前 manifest.yaml + instructions.md 双文件格式无法与生态互操作

### 目标架构层次

```
Process（运行时实例 — 已实现）
  ← Agent（智能体定义：身份 + 模型 + 策略 + Skill 引用 — 本 Story 新增）
      ← Skill(s)（能力模块：遵循 Agent Skills 行业标准 — 本 Story 重构）
```

OS 隐喻对应：Process = 进程，Agent = 可执行程序，Skill = 共享库

### 当前代码状态深度分析

#### skills/types.go（当前状态 — 需简化）

```go
// 当前：混合了 Agent 和 Skill 两层职责
type SkillModels struct {
    Provider  string `yaml:"provider"`
    Preferred string `yaml:"preferred"`
    Fallback  string `yaml:"fallback"`
}

type SkillManifest struct {
    Name          string      `yaml:"name"`
    Description   string      `yaml:"description"`
    Tools         []string    `yaml:"tools"`
    Models        SkillModels `yaml:"models"`        // ⚠️ 应属于 Agent
    ContextBudget int         `yaml:"context_budget"` // ⚠️ 应属于 Agent
}

type SkillInfo struct {
    Manifest     SkillManifest
    Instructions string
}
```

**目标：**

```go
// 新：仅保留 Skill 标准字段
type SkillManifest struct {
    Name         string            `yaml:"name"`
    Description  string            `yaml:"description"`
    AllowedTools []string          `yaml:"allowed-tools"` // 注意：连字符分隔（Agent Skills 标准）
    Metadata     map[string]string `yaml:"metadata"`
}

type SkillInfo struct {
    Manifest SkillManifest
    Body     string // SKILL.md 的 Markdown body（程序性知识）
}
```

**注意：** `Tools` 重命名为 `AllowedTools`，YAML tag 从 `tools` 改为 `allowed-tools`（遵循 Agent Skills 标准）。

#### skills/loader.go（当前状态 — 需重写）

```go
// 当前：加载 manifest.yaml + instructions.md 双文件
type SkillLoader struct {
    basePath string
}

func NewSkillLoader(basePath string) *SkillLoader

func (l *SkillLoader) Load(skillName string) (*SkillInfo, error)
    // 1. 检查目录存在
    // 2. 读取 manifest.yaml → yaml.Unmarshal → SkillManifest
    // 3. 读取 instructions.md → 原始文本
    // 4. 返回 &SkillInfo{Manifest, Instructions}
```

**目标：**

```go
// 新：解析 SKILL.md 单文件（YAML frontmatter + Markdown body）
type SkillLoader struct {
    basePath string
}

func NewSkillLoader(basePath string) *SkillLoader

// 渐进式加载：仅 frontmatter
func (l *SkillLoader) LoadMetadata(skillName string) (*SkillInfo, error)

// 完整加载：frontmatter + body
func (l *SkillLoader) LoadFull(skillName string) (*SkillInfo, error)
```

**SKILL.md 解析逻辑：**

1. 读取整个 `SKILL.md` 文件
2. 检测 `---` 分隔符，提取 YAML frontmatter
3. `yaml.Unmarshal` frontmatter → SkillManifest
4. 提取 `---` 后的 Markdown body
5. **特殊处理 `allowed-tools`**：SKILL.md 标准中 `allowed-tools` 是空格分隔的字符串（`/dev/fs /dev/shell`），需解析为 `[]string`。可使用 YAML 自定义 unmarshaler 或在加载后手动 `strings.Fields()` 分割

**⚠️ SKILL.md allowed-tools 解析注意：**

Agent Skills 标准中 `allowed-tools` 的格式是**空格分隔的单行字符串**：
```yaml
allowed-tools: /dev/fs /dev/shell
```

而非 YAML 列表：
```yaml
allowed-tools:
  - /dev/fs
  - /dev/shell
```

实现建议：在 SkillManifest 中使用自定义类型或在 Unmarshal 后处理：

```go
type SkillManifest struct {
    Name            string            `yaml:"name"`
    Description     string            `yaml:"description"`
    AllowedToolsRaw string            `yaml:"allowed-tools"` // 原始空格分隔字符串
    Metadata        map[string]string `yaml:"metadata"`
}

// 解析后调用
func (m *SkillManifest) AllowedTools() []string {
    if m.AllowedToolsRaw == "" {
        return nil
    }
    return strings.Fields(m.AllowedToolsRaw)
}
```

或者直接使用两种格式兼容的方案（推荐，更健壮）：

```go
type SpaceSeparatedList []string

func (s *SpaceSeparatedList) UnmarshalYAML(unmarshal func(interface{}) error) error {
    var str string
    if err := unmarshal(&str); err == nil {
        *s = strings.Fields(str)
        return nil
    }
    var list []string
    if err := unmarshal(&list); err == nil {
        *s = list
        return nil
    }
    return fmt.Errorf("allowed-tools must be space-separated string or string list")
}
```

#### kernel/kernel.go Spawn 函数（当前状态 — 需更新签名）

**当前签名和逻辑（kernel/kernel.go:100-187）：**

```go
func (k *KernelImpl) Spawn(intent string, skills []string, opts SpawnOpts) (types.PID, error) {
    // ...
    var systemPrompt string
    var allowedDevices []string
    var model string

    if len(skills) > 0 {
        skillInfo, err := k.skillLoader.Load(skills[0])
        // ...
        systemPrompt = skillInfo.Instructions
        allowedDevices = skillInfo.Manifest.Tools
        model = skillInfo.Manifest.Models.Preferred
        if opts.Model != "" {
            model = opts.Model
        }
    }
    // ...
}
```

**目标签名：**

```go
func (k *KernelImpl) Spawn(intent string, agent *agents.AgentInfo, opts SpawnOpts) (types.PID, error) {
    var systemPrompt string
    var allowedDevices []string
    var model string

    if agent != nil {
        // Agent instructions + 所有 Skill body 拼接为 system prompt
        systemPrompt = agent.Instructions
        for _, skill := range agent.Skills {
            systemPrompt += "\n\n" + skill.Body
        }

        // 聚合所有 Skill 的 AllowedTools
        toolSet := make(map[string]bool)
        for _, skill := range agent.Skills {
            for _, tool := range skill.Manifest.AllowedTools() {
                toolSet[tool] = true
            }
        }
        for tool := range toolSet {
            allowedDevices = append(allowedDevices, tool)
        }

        // 模型偏好来自 Agent
        model = agent.Manifest.Models.Preferred
        if opts.Model != "" {
            model = opts.Model
        }
    }
    // ... 其余逻辑不变
}
```

**⚠️ 关键变更点：**
1. 参数从 `skills []string` 改为 `agent *agents.AgentInfo`（可为 nil，无 agent 模式）
2. KernelImpl 不再持有 `skillLoader` — Skill 加载在 AgentLoader 中完成，KernelImpl 只接收已解析好的 AgentInfo
3. system prompt 组装逻辑更新：Agent instructions + Skill body（原来只有 Skill instructions）
4. AllowedDevices 从单个 Skill 改为多 Skill 聚合

#### cmd/crux/main.go（当前状态 — 需更新）

**当前关键代码片段：**

```go
// 第 34 行
var flagSkill string

// 第 153 行
rootCmd.Flags().StringVar(&flagSkill, "skill", "", "Skill to load for the agent (e.g., code-analyst)")

// 第 193 行
skillLoader := skills.NewSkillLoader("lib/skills")

// 第 198-202 行
skillsList := []string{}
if flagSkill != "" {
    skillsList = append(skillsList, flagSkill)
}
pid, err := kern.Spawn(intent, skillsList, kernel.SpawnOpts{...})
```

**目标：**

```go
// flag 改为 --agent
var flagAgent string

rootCmd.Flags().StringVar(&flagAgent, "agent", "", "Agent definition to use (e.g., code-analyst)")

// 初始化 AgentLoader + SkillLoader
skillLoader := skills.NewSkillLoader("lib/skills")
agentLoader := agents.NewAgentLoader("lib/agents", skillLoader)

// Spawn 调用
var agentInfo *agents.AgentInfo
if flagAgent != "" {
    agentInfo, err = agentLoader.Load(flagAgent)
    if err != nil {
        // 错误处理
    }
}
pid, err := kern.Spawn(intent, agentInfo, kernel.SpawnOpts{...})
```

**依赖注入变化：**
- 新增 `agents.AgentLoader` 实例化
- AgentLoader 持有 SkillLoader 引用（`agents/ → skills/` 单向依赖）
- KernelImpl 不再需要 SkillLoader（从构造函数中移除）

#### KernelImpl 构造函数变化

**当前（kernel/kernel.go）：**

```go
type KernelImpl struct {
    vfs         *vfs.VFS
    ctxMgr      *context.Manager
    skillLoader *skills.SkillLoader  // ⚠️ 需移除
    procTable   *xsync.SyncMap[types.PID, *Process]
    pidCounter  uint64
}

func New(v *vfs.VFS, cm *context.Manager, sl *skills.SkillLoader) *KernelImpl
```

**目标：**

```go
type KernelImpl struct {
    vfs        *vfs.VFS
    ctxMgr     *context.Manager
    procTable  *xsync.SyncMap[types.PID, *Process]
    pidCounter uint64
    // skillLoader 已移除 — Skill 加载在 AgentLoader 中完成
}

func New(v *vfs.VFS, cm *context.Manager) *KernelImpl
```

### agents/ 包完整设计

#### agents/types.go

```go
package agents

import "github.com/gonewx/crux/skills"

// AgentModels 定义 Agent 的模型偏好
type AgentModels struct {
    Provider  string `yaml:"provider"`
    Preferred string `yaml:"preferred"`
    Fallback  string `yaml:"fallback"`
}

// AgentManifest 定义 Agent 的 YAML 配置
type AgentManifest struct {
    Name          string      `yaml:"name"`
    Description   string      `yaml:"description"`
    Models        AgentModels `yaml:"models"`
    ContextBudget int         `yaml:"context_budget"`
    Skills        []string    `yaml:"skills"` // Skill 名称引用列表
}

// AgentInfo 包含 Agent 的完整加载信息
type AgentInfo struct {
    Manifest     AgentManifest
    Instructions string              // instructions.md 内容
    Skills       []*skills.SkillInfo // 已加载的 Skill 列表
}

// AllowedTools 聚合所有引用 Skill 的 AllowedTools，去重后返回
func (a *AgentInfo) AllowedTools() []string {
    toolSet := make(map[string]struct{})
    for _, skill := range a.Skills {
        for _, tool := range skill.Manifest.AllowedTools() {
            toolSet[tool] = struct{}{}
        }
    }
    tools := make([]string, 0, len(toolSet))
    for tool := range toolSet {
        tools = append(tools, tool)
    }
    return tools
}

// SystemPrompt 组装完整的 system prompt = Agent instructions + Skill bodies
func (a *AgentInfo) SystemPrompt() string {
    prompt := a.Instructions
    for _, skill := range a.Skills {
        if skill.Body != "" {
            prompt += "\n\n" + skill.Body
        }
    }
    return prompt
}
```

#### agents/loader.go

```go
package agents

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/gonewx/crux/skills"
    "gopkg.in/yaml.v3"
)

// AgentLoader 加载 Agent 定义
type AgentLoader struct {
    basePath    string
    skillLoader *skills.SkillLoader
}

func NewAgentLoader(basePath string, sl *skills.SkillLoader) *AgentLoader {
    return &AgentLoader{basePath: basePath, skillLoader: sl}
}

// Load 加载指定 Agent 的完整信息
// 1. 加载 agent.yaml → AgentManifest
// 2. 读取 instructions.md → 原始文本
// 3. 对 manifest.Skills 中每个名称，调用 skillLoader.LoadFull() 加载 Skill
// 4. 返回 AgentInfo（包含聚合的权限信息）
func (l *AgentLoader) Load(agentName string) (*AgentInfo, error) {
    agentDir := filepath.Join(l.basePath, agentName)

    // 1. 检查目录存在
    if _, err := os.Stat(agentDir); os.IsNotExist(err) {
        return nil, fmt.Errorf("agent directory not found: %s", agentDir)
    }

    // 2. 加载 agent.yaml
    manifestPath := filepath.Join(agentDir, "agent.yaml")
    manifestData, err := os.ReadFile(manifestPath)
    if err != nil {
        return nil, fmt.Errorf("failed to read agent manifest: %w", err)
    }
    var manifest AgentManifest
    if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
        return nil, fmt.Errorf("failed to parse agent manifest: %w", err)
    }
    if manifest.Name == "" {
        return nil, fmt.Errorf("agent manifest missing required field: name")
    }

    // 3. 读取 instructions.md
    instructionsPath := filepath.Join(agentDir, "instructions.md")
    instructionsData, err := os.ReadFile(instructionsPath)
    if err != nil {
        return nil, fmt.Errorf("failed to read agent instructions: %w", err)
    }

    // 4. 加载引用的 Skills
    var loadedSkills []*skills.SkillInfo
    for _, skillName := range manifest.Skills {
        skillInfo, err := l.skillLoader.LoadFull(skillName)
        if err != nil {
            return nil, fmt.Errorf("failed to load skill %q referenced by agent %q: %w",
                skillName, agentName, err)
        }
        loadedSkills = append(loadedSkills, skillInfo)
    }

    return &AgentInfo{
        Manifest:     manifest,
        Instructions: string(instructionsData),
        Skills:       loadedSkills,
    }, nil
}
```

### 参考 Agent 内容设计

#### lib/agents/code-analyst/agent.yaml

```yaml
name: code-analyst
description: "分析代码质量、识别潜在问题并提供改进建议的智能体"
models:
  provider: claude
  preferred: sonnet
  fallback: haiku
context_budget: 8192
skills:
  - code-analysis
```

**字段说明：**
- `name` — 与目录名一致，`--agent=code-analyst` 触发
- `models` — 从旧 SkillManifest 迁移过来的模型偏好
- `context_budget` — 从旧 SkillManifest 迁移过来（当前未被内核使用，预留字段）
- `skills: [code-analysis]` — 引用 `lib/skills/code-analysis/SKILL.md`

#### lib/agents/code-analyst/instructions.md

从现有 `lib/skills/code-analyst/instructions.md` 中**提取角色定义和行为策略部分**，移除程序性知识（那些留给 SKILL.md）。

**当前 instructions.md 结构（78 行）包含：**
- 角色定义（"你是一名高级代码审查工程师"）→ **保留在 Agent instructions**
- 分析维度（5 维度列表）→ **保留在 Agent instructions**（策略级）
- 工具使用指南（如何用 /dev/fs、/dev/shell）→ **迁移到 Skill SKILL.md body**
- 工作流程（读取→分析→报告步骤）→ **迁移到 Skill SKILL.md body**
- 输出格式模板 → **保留在 Agent instructions**（输出策略属于 Agent 角色）
- 严重等级定义 → **迁移到 Skill SKILL.md body**（通用知识）

### 参考 Skill SKILL.md 设计

#### lib/skills/code-analysis/SKILL.md

```markdown
---
name: code-analysis
description: >
  Analyze code quality, identify bugs, performance issues and security
  vulnerabilities. Use when the user wants to review code files or
  find problems in source code.
allowed-tools: /dev/fs /dev/shell
metadata:
  author: crux
  version: "1.0"
compatibility: Designed for Crux
---

# Code Analysis

## When to use this skill
Use this skill when the user asks to analyze, review, or audit source code...

## How to analyze code
1. Read the target file(s) via /dev/fs
2. Examine code structure, naming conventions, error handling...
3. Run static analysis tools via /dev/shell if available...

## Severity levels
- **Critical**: Security vulnerabilities, data loss risks...
- **Warning**: Potential bugs, performance issues...
- **Info**: Style issues, minor improvements...

## Common patterns to check
- Unchecked error returns
- Resource leaks
- Race conditions
- Security vulnerabilities
- Performance anti-patterns
```

**⚠️ 注意目录名变化：** `code-analyst`（旧 Skill 名）→ `code-analysis`（新 Skill 名，Agent Skills 标准推荐使用描述性名称而非角色名）。

### SKILL.md 解析实现细节

**frontmatter 分离算法：**

```go
func parseSKILLMD(content string) (frontmatter string, body string, err error) {
    // SKILL.md 格式：
    // ---
    // YAML frontmatter
    // ---
    // Markdown body

    const sep = "---"
    lines := strings.SplitN(content, "\n", -1)

    // 找到第一个 "---"
    if len(lines) == 0 || strings.TrimSpace(lines[0]) != sep {
        return "", "", fmt.Errorf("SKILL.md must start with ---")
    }

    // 找到第二个 "---"
    endIdx := -1
    for i := 1; i < len(lines); i++ {
        if strings.TrimSpace(lines[i]) == sep {
            endIdx = i
            break
        }
    }
    if endIdx == -1 {
        return "", "", fmt.Errorf("SKILL.md missing closing ---")
    }

    frontmatter = strings.Join(lines[1:endIdx], "\n")
    body = strings.TrimSpace(strings.Join(lines[endIdx+1:], "\n"))
    return frontmatter, body, nil
}
```

### testdata 更新

#### skills/testdata/mock-skill/ 需转换

**当前结构：**
```
skills/testdata/mock-skill/
├── manifest.yaml     # name: code-analyst, tools, models, context_budget
└── instructions.md   # mock instructions text
```

**目标结构：**
```
skills/testdata/mock-skill/
└── SKILL.md          # frontmatter(name, description, allowed-tools) + body
```

**mock-skill/SKILL.md 内容：**
```markdown
---
name: mock-skill
description: "A mock skill for testing"
allowed-tools: /dev/fs /dev/shell
---

# Mock Skill

This is a mock skill for testing purposes.
```

### 现有测试影响分析

#### skills/loader_test.go

| 测试函数 | 当前行为 | 需要变更 |
|---------|---------|---------|
| `TestLoadYAML_Success` | 测试 LoadYAML 泛型加载 yaml | 如果仅用于 manifest.yaml，可能需更新或移除；如果是通用 YAML 工具，保留 |
| `TestLoadYAML_FileNotFound` | 测试文件不存在 | 保留（通用 YAML 工具） |
| `TestLoadYAML_InvalidYAML` | 测试无效 YAML | 保留 |
| `TestSkillLoader_Load_Success` | 加载 testdata/mock-skill (manifest.yaml + instructions.md) | **重写**：改为加载 SKILL.md |
| `TestSkillLoader_Load_DirNotFound` | 目录不存在 | 保留逻辑，更新错误消息 |
| `TestSkillLoader_Load_ManifestNotFound` | manifest.yaml 不存在 | **重写**：改为 SKILL.md 不存在 |
| `TestSkillLoader_Load_InvalidManifest` | 无效 manifest | **重写**：改为无效 SKILL.md |
| `TestSkillLoader_Load_EmptyName` | manifest name 为空 | **重写**：SKILL.md name 为空 |
| `TestSkillLoader_Load_RealCodeAnalyst` | 加载真实 code-analyst | **重写**：改为加载 code-analysis/SKILL.md |

#### kernel/kernel_test.go

所有使用 `Spawn(intent, skills, opts)` 的测试都需要更新签名为 `Spawn(intent, agentInfo, opts)`。

**关键测试函数（需更新）：**
- `TestSpawn_Success` — 更新签名
- `TestSpawn_WithSkill_*` — 更新为使用 AgentInfo
- `TestReasonStep_*` — 如果涉及 Spawn 调用则更新
- Mock 对象需要适配新签名

#### cmd/crux/integration_test.go

| 测试函数 | 需要变更 |
|---------|---------|
| `TestE2E_SimpleIntent` | 更新 Spawn 调用签名 |
| `TestE2E_WithSkill_InjectsInstructions` | **重写**：改为 Agent 注入 |
| `TestE2E_CodeAnalystSkill` | **重写**：改为 Agent 端到端 |

### 依赖方向验证

**新增依赖：**
```
cmd/ → agents/ → skills/   (新增 agents 包)
cmd/ → kernel/              (kernel 依赖 agents，不再直接依赖 skills)
```

**完整依赖图：**
```
internal/types/  ← 所有包均可导入（零外部依赖）
internal/xsync/  ← 所有包均可导入（仅依赖 internal/types/）
internal/ui/     ← 仅 cmd/ 导入

cmd/ → kernel/ → vfs/     → drivers/{llm,shell,fs}
                → context/
                → agents/ → skills/
cmd/ → agents/ → skills/
cmd/ → debug/（仅依赖 internal/types/）
```

**关键变化：** `kernel/` 不再直接依赖 `skills/`，改为依赖 `agents/`（通过 Spawn 函数签名中的 `*agents.AgentInfo` 类型）。Skill 加载在 `agents/` 包中完成，kernel 只接收已解析好的 AgentInfo。这简化了 kernel 的职责——kernel 仅知道 Agent 抽象，不需要了解 Skill 细节。

### 实施顺序建议

**推荐自底向上实施：**

1. **Step 1: skills/ 包重构** — 先完成 SKILL.md 格式支持（types + loader + tests）
2. **Step 2: 参考 Skill 创建** — 创建 lib/skills/code-analysis/SKILL.md
3. **Step 3: agents/ 包创建** — 创建 types + loader + tests（依赖已重构的 skills/）
4. **Step 4: 参考 Agent 创建** — 创建 lib/agents/code-analyst/（agent.yaml + instructions.md）
5. **Step 5: kernel/ 更新** — Spawn 签名修改（依赖已创建的 agents/）
6. **Step 6: cmd/ 更新** — --agent flag + 依赖注入
7. **Step 7: 清理** — 删除旧文件、全量测试

这个顺序确保每一步的依赖都已就绪，可以独立测试。

### 旧文件清理清单

| 操作 | 文件 | 说明 |
|------|------|------|
| 删除 | `lib/skills/code-analyst/manifest.yaml` | 被 Agent agent.yaml 和 Skill SKILL.md 替代 |
| 删除 | `lib/skills/code-analyst/instructions.md` | 角色部分迁入 Agent，程序性知识迁入 SKILL.md |
| 删除 | `skills/testdata/mock-skill/manifest.yaml` | 被 SKILL.md 替代 |
| 删除 | `skills/testdata/mock-skill/instructions.md` | 被 SKILL.md 替代 |

**可能需要删除整个 `lib/skills/code-analyst/` 目录**（如果该目录下没有其他文件）。

### 前序 Story 经验（必须吸收）

**Story 2.5 Code Review 发现：**

| 问题 | 经验 → 2.6 影响 |
|------|-----------------|
| H1: AC 文本与实现不一致 | 确保 AC 描述与实际行为匹配 |
| M1: time.Since(time.Now()) 虚假耗时 | 集成测试中避免此模式 |
| L1: Models.Fallback 断言缺失 | Agent tests 中覆盖 Fallback 字段 |
| L4: 并发保护 | AgentLoader 如需并发使用，加锁保护 |

**Story 2.4 经验：**

| 问题 | 经验 |
|------|------|
| 路径遍历防护 | skills/loader 中路径 Clean 在操作前执行 |
| 权限检查 | AllowedDevices 检查在 reasonStep 中执行（kernel/kernel.go:345-368） |
| AppendToolResult 错误处理 | 已修复，本 Story 不需要修改此处 |

### Git 智能分析

**最近提交模式：**
```
807c56b Refactor architecture documentation to enhance clarity on Agent and Skill management
1c9b1e2 Add documentation for Agent Skills and Model Context Protocol
3293ae7 Complete Epic 2: Skill Capabilities and File Access Implementation
4ed70d9 Finalize Story 2.5: Code-Analyst Reference Skill Implementation
```

**观察：**
- 架构文档已经更新反映了 Agent/Skill 分层设计（807c56b）
- 文档更新先于代码实现，这是正确的流程
- 提交消息风格：英文，动词短语开头

### NFR 合规

| NFR | 要求 | 本 Story 关系 |
|-----|------|--------------|
| NFR16 | Skill tools 白名单 | 从单 Skill.Tools → 多 Skill 聚合 AllowedTools |
| NFR17 | 最小安全边界 | Agent 无 --agent 时所有设备可访问，有 --agent 时受白名单限制 |
| NFR20 | LLM 驱动封装 | 不受影响 |
| NFR18 | Go 标准项目布局 | 新增 agents/ 遵循项目约定 |

### Project Structure Notes

**本 Story 新建的文件：**

```
agents/types.go              — AgentManifest + AgentModels + AgentInfo 类型
agents/loader.go             — AgentLoader 加载器
agents/loader_test.go        — Agent 加载器单元测试
lib/agents/code-analyst/
├── agent.yaml               — 参考 Agent 配置
└── instructions.md          — Agent 角色定义
lib/skills/code-analysis/
└── SKILL.md                 — 标准格式参考 Skill
skills/testdata/mock-skill/
└── SKILL.md                 — 更新后的测试 fixture
```

**本 Story 修改的文件：**

```
skills/types.go              — 简化 SkillManifest
skills/loader.go             — SKILL.md 解析 + 渐进式加载
skills/loader_test.go        — 适配新格式
kernel/kernel.go             — Spawn 签名 + KernelImpl 构造函数
kernel/kernel_test.go        — 适配新 Spawn 签名
cmd/crux/main.go             — --agent flag + AgentLoader 注入
cmd/crux/integration_test.go — 适配 Agent 加载
```

**本 Story 删除的文件：**

```
lib/skills/code-analyst/manifest.yaml       — 被 Agent/Skill 拆分替代
lib/skills/code-analyst/instructions.md     — 被 Agent/Skill 拆分替代
skills/testdata/mock-skill/manifest.yaml    — 被 SKILL.md 替代
skills/testdata/mock-skill/instructions.md  — 被 SKILL.md 替代
```

### References

- [Source: _bmad-output/planning-artifacts/sprint-change-proposal-2026-02-25.md] — 完整变更提案定义
- [Source: _bmad-output/planning-artifacts/architecture.md#agents/] — Agent 包架构设计
- [Source: _bmad-output/planning-artifacts/architecture.md#Kernel ↔ Agents 边界] — 架构边界定义
- [Source: _bmad-output/planning-artifacts/architecture.md#Agents ↔ Skills 边界] — 单向依赖
- [Source: _bmad-output/planning-artifacts/architecture.md#修正后的完整项目结构] — 最终目录结构
- [Source: _bmad-output/planning-artifacts/epics.md#Epic 2] — Epic 2 上下文
- [Source: _bmad-output/project-context.md] — 完整项目规范和约束
- [Source: _bmad-output/implementation-artifacts/2-5-code-analyst-reference-skill.md] — 前序 Story 经验
- [Source: skills/types.go:4-23] — 当前 SkillManifest 类型定义
- [Source: skills/loader.go:38-76] — 当前 SkillLoader.Load() 方法
- [Source: kernel/kernel.go:100-187] — 当前 Spawn 函数实现
- [Source: kernel/kernel.go:345-368] — 设备权限白名单检查逻辑
- [Source: cmd/crux/main.go:34,153,193,198-202] — 当前 --skill flag 和 SkillLoader 初始化

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

N/A

### Completion Notes List

- 由于 agents/ 和 skills/ 存在交叉类型依赖，采用协调式实现而非严格按 Task 顺序
- AllowedToolsRaw 字段 + AllowedTools() 方法方案避免了 goccy/go-yaml 自定义 unmarshaler 兼容问题
- AllowedTools() 返回排序结果确保测试断言的确定性
- 旧 code-analyst Skill 内容按职责拆分：角色/策略 → Agent instructions.md，程序性知识/工具指南 → Skill SKILL.md body
- KernelImpl 不再直接依赖 skills/ 包（通过 agents/ 间接依赖），简化了 kernel 职责边界
- skills/testdata/no-instructions/ 需保留空目录（含 .gitkeep）用于测试 SKILL.md 缺失场景
- [Code Review Fix] M1: AgentLoader/SkillLoader 添加路径逃逸防护（absPath containment check）
- [Code Review Fix] M2/M3: 修正 project-context.md 和 Story 中的依赖图，补充 kernel → agents 边
- [Code Review Fix] M4: parseSKILLMD 增加 extractBody 参数，LoadMetadata 不再构建 body 字符串
- [Code Review Fix] L1: Spawn debug 事件增加 allowed_devices 字段

### Change Log

| 文件 | 操作 | 说明 |
|------|------|------|
| `agents/types.go` | 新增 | AgentManifest、AgentModels、AgentInfo 类型定义 + AllowedTools()/SystemPrompt() 方法 |
| `agents/loader.go` | 新增 | AgentLoader 加载 agent.yaml + instructions.md + Skill 引用解析 + 路径逃逸防护 |
| `agents/loader_test.go` | 新增 | 13 个测试用例覆盖 Agent 加载、辅助方法和路径遍历 |
| `agents/testdata/mock-agent/` | 新增 | agent.yaml + instructions.md 测试 fixture |
| `agents/testdata/invalid-agent/` | 新增 | 无效 YAML 测试 fixture |
| `agents/testdata/missing-instructions/` | 新增 | 缺少 instructions.md 测试 fixture |
| `agents/testdata/missing-name/` | 新增 | 缺少 name 字段测试 fixture |
| `agents/testdata/bad-skill-ref/` | 新增 | 引用不存在 Skill 的测试 fixture |
| `skills/types.go` | 重写 | 简化为 Name/Description/AllowedToolsRaw/Metadata，移除 SkillModels |
| `skills/loader.go` | 重写 | parseSKILLMD(extractBody) + 路径逃逸防护 + LoadMetadata(false) + LoadFull(true) |
| `skills/loader_test.go` | 重写 | 适配 SKILL.md 格式的 16 个测试用例（含 MetadataOnly 和 PathTraversal） |
| `skills/testdata/mock-skill/SKILL.md` | 新增 | 替代旧 manifest.yaml + instructions.md |
| `skills/testdata/invalid-manifest/SKILL.md` | 新增 | 无效 YAML frontmatter 测试 |
| `skills/testdata/missing-fields/SKILL.md` | 新增 | 缺少 name 字段测试 |
| `skills/testdata/no-instructions/.gitkeep` | 新增 | 目录存在但无 SKILL.md 测试 |
| `lib/agents/code-analyst/agent.yaml` | 新增 | 参考 Agent 配置 |
| `lib/agents/code-analyst/instructions.md` | 新增 | 参考 Agent 角色定义 |
| `lib/skills/code-analysis/SKILL.md` | 新增 | 参考 Skill 标准格式 |
| `kernel/kernel.go` | 修改 | NewKernel 3 参数，Spawn 接受 *agents.AgentInfo，debug 事件含 allowed_devices |
| `kernel/kernel_test.go` | 重写 | 所有 Spawn 调用适配 AgentInfo，新增 testAgentInfo() 辅助函数 |
| `cmd/crux/main.go` | 修改 | --skill → --agent flag，AgentLoader 注入 |
| `cmd/crux/integration_test.go` | 修改 | NewKernel 调用适配，两个 Skill 测试重写为 Agent 测试 |
| `_bmad-output/project-context.md` | 修改 | 依赖图更新：kernel → agents → skills |
| `skills/testdata/mock-skill/manifest.yaml` | 删除 | 被 SKILL.md 替代 |
| `skills/testdata/mock-skill/instructions.md` | 删除 | 被 SKILL.md 替代 |
| `skills/testdata/invalid-manifest/manifest.yaml` | 删除 | 被 SKILL.md 替代 |
| `skills/testdata/missing-fields/manifest.yaml` | 删除 | 被 SKILL.md 替代 |
| `skills/testdata/no-instructions/manifest.yaml` | 删除 | 被空目录 + .gitkeep 替代 |
| `lib/skills/code-analyst/manifest.yaml` | 删除 | 被 Agent/Skill 拆分替代 |
| `lib/skills/code-analyst/instructions.md` | 删除 | 被 Agent/Skill 拆分替代 |

### File List

**新增文件：**
- `agents/types.go`
- `agents/loader.go`
- `agents/loader_test.go`
- `agents/testdata/mock-agent/agent.yaml`
- `agents/testdata/mock-agent/instructions.md`
- `agents/testdata/invalid-agent/agent.yaml`
- `agents/testdata/missing-instructions/agent.yaml`
- `agents/testdata/missing-name/agent.yaml`
- `agents/testdata/missing-name/instructions.md`
- `agents/testdata/bad-skill-ref/agent.yaml`
- `agents/testdata/bad-skill-ref/instructions.md`
- `skills/testdata/mock-skill/SKILL.md`
- `skills/testdata/invalid-manifest/SKILL.md`
- `skills/testdata/missing-fields/SKILL.md`
- `skills/testdata/no-instructions/.gitkeep`
- `lib/agents/code-analyst/agent.yaml`
- `lib/agents/code-analyst/instructions.md`
- `lib/skills/code-analysis/SKILL.md`

**修改文件：**
- `skills/types.go`
- `skills/loader.go`
- `skills/loader_test.go`
- `kernel/kernel.go`
- `kernel/kernel_test.go`
- `cmd/crux/main.go`
- `cmd/crux/integration_test.go`

**删除文件：**
- `skills/testdata/mock-skill/manifest.yaml`
- `skills/testdata/mock-skill/instructions.md`
- `skills/testdata/invalid-manifest/manifest.yaml`
- `skills/testdata/missing-fields/manifest.yaml`
- `skills/testdata/no-instructions/manifest.yaml`
- `lib/skills/code-analyst/manifest.yaml`
- `lib/skills/code-analyst/instructions.md`
