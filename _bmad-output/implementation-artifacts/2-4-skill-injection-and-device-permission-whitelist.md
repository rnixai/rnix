# Story 2.4: Skill 注入与设备权限白名单

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want Spawn 时指定 Skill，系统自动注入 instructions 并限制设备访问范围,
So that 智能体获得专业指令同时只能访问 Skill 声明的设备。

## Acceptance Criteria

1. **Skill Instructions 注入** — Given 用户执行 `crux "分析代码" --skill=code-analyst`，When Spawn 创建进程，Then 加载 code-analyst Skill 的 instructions.md 内容，And 注入到 LLM 调用的 system prompt 中
2. **设备权限白名单拒绝** — Given Skill manifest 声明 `tools: ["/dev/fs", "/dev/shell"]`，When 智能体尝试访问不在白名单中的设备（如 `/dev/llm/claude`），Then 将权限拒绝错误追加到智能体上下文中，通知 LLM 该设备不可用，并继续推理循环（NFR16 优雅降级）
3. **Skill 模型选择** — Given Skill manifest 声明 `models.provider: claude`、`models.preferred: sonnet`，When Spawn 创建进程，Then LLM 调用自动使用 `/dev/llm/claude` 驱动和 `sonnet` 模型
4. **无 Skill 通用模式** — Given 用户未指定 `--skill`，When Spawn 创建进程，Then 使用通用模式（无 Skill instructions 注入），所有设备可访问（NFR17 最小安全边界）
5. **工具结果上下文追加** — Given 工具执行产生结果，When reasonStep 处理 tool_call 返回值，Then 结果追加到智能体上下文中（CtxWrite）供后续推理使用（FR12）

## Tasks / Subtasks

- [x] Task 0: 修复循环依赖 — skills 包移除 kernel 导入 (AC: 前置条件)
  - [x] 0.1 在 `skills/loader.go` 中移除 `kernel` 导入，将 `kernel.NewSyscallError` 替换为 `fmt.Errorf` 或自定义错误
  - [x] 0.2 运行 `go build ./...` 确认编译通过
  - [x] 0.3 运行 `go test -race ./skills/...` 确认测试通过

- [x] Task 1: Process 添加 AllowedDevices 字段 (AC: #2, #4)
  - [x] 1.1 在 `kernel/process.go` 的 `Process` 结构体中添加 `AllowedDevices []string` 字段
  - [x] 1.2 `AllowedDevices` 为 nil 或空时表示"所有设备可访问"（通用模式，AC #4）
  - [x] 1.3 `AllowedDevices` 非空时仅允许白名单中的设备路径

- [x] Task 2: KernelImpl 添加 SkillLoader 依赖 (AC: #1, #3)
  - [x] 2.1 `kernel/kernel.go` 添加 `import "github.com/usecrux/crux/skills"`
  - [x] 2.2 `KernelImpl` 结构体添加 `skillLoader *skills.SkillLoader` 字段
  - [x] 2.3 修改 `NewKernel` 签名：添加 `skillLoader *skills.SkillLoader` 参数（放在 `ctxMgr` 和 `cb` 之间）
  - [x] 2.4 在 `NewKernel` 中存储 `skillLoader` 到结构体

- [x] Task 3: Spawn 中加载 Skill 并注入 (AC: #1, #3, #4)
  - [x] 3.1 在 `Spawn()` 中，创建 Process 之后、分配上下文之前，检查 `len(skills) > 0`
  - [x] 3.2 如果有 Skill，调用 `k.skillLoader.Load(skills[0])` 加载第一个 Skill
  - [x] 3.3 加载失败时返回 `*SyscallError{Syscall: "Spawn", Code: ErrNotFound}`
  - [x] 3.4 加载成功后，将 `skillInfo.Manifest.Tools` 赋值给 `proc.AllowedDevices`
  - [x] 3.5 注入 instructions 到 system prompt：如果 `opts.SystemPrompt` 为空，直接设为 `skillInfo.Instructions`；如果非空，追加到现有 SystemPrompt 后面（`\n\n` 分隔）
  - [x] 3.6 模型选择优先级：CLI `--model` > Skill `manifest.models.preferred` > 驱动默认值。如果 `opts.Model` 为空且 Skill 有 `Models.Preferred`，设置 `opts.Model = skillInfo.Manifest.Models.Preferred`
  - [x] 3.7 将 Skill 名称记录到 Spawn 的 emitEvent args 中

- [x] Task 4: reasonStep 添加设备权限检查 (AC: #2, #4)
  - [x] 4.1 在 `reasonStep()` 的 `case ActionToolCall:` 分支中，`k.vfs.Open()` 调用之前添加权限检查
  - [x] 4.2 权限检查逻辑：如果 `proc.AllowedDevices` 非空，检查 `action.ToolPath` 是否在白名单中
  - [x] 4.3 匹配规则：精确匹配（`/dev/fs` == `/dev/fs`）或前缀匹配（`/dev/fs/path/to/file` 以 `/dev/fs` 为前缀）
  - [x] 4.4 不匹配时：不直接终止进程，而是将权限错误作为工具结果追加到上下文，让 LLM 知道该工具不可用（更优雅的处理方式，参考 AC 描述的语义）
  - [x] 4.5 发送 emitEvent 记录权限拒绝事件
  - [x] 4.6 AllowedDevices 为空时跳过检查（通用模式，AC #4）

- [x] Task 5: CLI 添加 --skill 标志 (AC: #1)
  - [x] 5.1 在 `cmd/crux/main.go` 中添加 `flagSkill string` 变量
  - [x] 5.2 在 `init()` 中注册：`rootCmd.Flags().StringVar(&flagSkill, "skill", "", "Skill to load (e.g., code-analyst)")`
  - [x] 5.3 在 `runRoot()` 中初始化 SkillLoader：`skillLoader := skills.NewSkillLoader("lib/skills")`
  - [x] 5.4 修改 `kernel.NewKernel()` 调用：传入 `skillLoader`
  - [x] 5.5 构建 skills 列表并传入 Spawn：`var skillsList []string; if flagSkill != "" { skillsList = []string{flagSkill} }`
  - [x] 5.6 添加 `import "github.com/usecrux/crux/skills"`

- [x] Task 6: 单元测试 (AC: #1-5)
  - [x] 6.1 `kernel/kernel_test.go` — 新增测试：
    - `TestSpawn_WithSkill_InjectsInstructions` — 验证 Skill instructions 注入到 context 的 system prompt
    - `TestSpawn_WithSkill_SetsAllowedDevices` — 验证 AllowedDevices 从 manifest.Tools 设置
    - `TestSpawn_WithSkill_ModelSelection` — 验证模型优先级（CLI > Skill > 默认）
    - `TestSpawn_WithSkill_NotFound` — 验证 Skill 不存在时返回 ErrNotFound
    - `TestSpawn_WithoutSkill_AllDevicesAllowed` — 验证无 Skill 时 AllowedDevices 为空
  - [x] 6.2 `kernel/kernel_test.go` — 权限检查测试：
    - `TestReasonStep_PermissionDenied_WhenDeviceNotInWhitelist` — 验证白名单外设备被拒绝
    - `TestReasonStep_PermissionAllowed_WhenDeviceInWhitelist` — 验证白名单内设备可访问
    - `TestReasonStep_PrefixMatch_AllowsSubpath` — 验证 `/dev/fs/path/to/file` 匹配 `/dev/fs` 白名单
    - `TestReasonStep_NoWhitelist_AllowsAll` — 验证无白名单时所有设备可访问
  - [x] 6.3 使用 mock SkillLoader 或 testdata fixture，不依赖真实 Skill 文件

- [x] Task 7: 全量回归测试 (AC: #1-5)
  - [x] 7.1 `go test -race ./skills/...` 通过（确认循环依赖修复后 skills 包无回归）
  - [x] 7.2 `go test -race ./kernel/...` 通过（新增测试 + 现有测试无回归）
  - [x] 7.3 `go test -race ./...` 全量通过
  - [x] 7.4 `go vet ./...` 无警告

## Dev Notes

### 核心实现分析

**Story 2.4 是 Epic 2 的安全与集成核心** — 将 Skill 系统（Story 2.1）与内核推理循环（Story 1.6）连接起来，实现 instructions 注入和设备权限白名单，是 Story 2.5（code-analyst 参考 Skill）的直接前置条件。

**⚠️ 关键前置问题：循环依赖**

当前 `skills/loader.go:9` 导入了 `kernel` 包（用于 `kernel.NewSyscallError`）。Story 2.4 需要 `kernel` 导入 `skills`（使用 `SkillLoader`），这将产生**循环依赖**。

**修复方案（Task 0）**：`skills/loader.go` 中将 `kernel.NewSyscallError(...)` 替换为 `fmt.Errorf(...)` 或自定义 `SkillError` 类型。类似 Story 2.2 中 `drivers/fs/` 移除 kernel 依赖的做法。

具体改动：
```go
// 当前（skills/loader.go:47）
return nil, kernel.NewSyscallError("Load", 0, dir, err, types.ErrNotFound)

// 修改为
return nil, fmt.Errorf("skill %q not found: %w", skillName, err)
```

### 架构约束（必须遵循）

**依赖方向（修复后）：**
```
cmd/crux/main.go → kernel/ → skills/（使用 SkillLoader）
                  → vfs/
                  → context/
skills/ → internal/types/（零 kernel 依赖）
skills/ 不导入 → kernel/、vfs/、drivers/、cmd/
```

**权限检查位置：在 kernel/reasonStep 中，不在 VFS.Open() 中**
- VFS 无法访问 Process 信息（避免 vfs/ → kernel/ 反向依赖）
- reasonStep 已持有 Process 引用，可直接检查 AllowedDevices
- 提供更清晰的错误链路和 SyscallEvent 记录

**Process 结构体（kernel/process.go:32-52）当前字段：**
```go
type Process struct {
    PID        types.PID
    PPID       types.PID
    State      types.ProcessState
    Intent     string              // 不可变
    Skills     []string            // ✅ 已存在，保存 Skill 名称列表
    Children   []types.PID
    FDTable    map[types.FD]vfs.VFSFile
    DebugChan  chan types.SyscallEvent
    Done       chan ExitStatus
    CreatedAt  time.Time
    Exit       *ExitStatus
    CtxID      types.CtxID
    Result     string
    TokensUsed int
    // ⬇️ 新增
    // AllowedDevices []string    // nil = 全部可访问; 非空 = 仅白名单设备
}
```

**KernelImpl 结构体（kernel/kernel.go:73-78）当前字段：**
```go
type KernelImpl struct {
    procTable *xsync.SyncMap[types.PID, *Process]
    vfs       *vfs.VFS
    ctxMgr    *cruxctx.Manager
    callbacks KernelCallbacks
    // ⬇️ 新增
    // skillLoader *skills.SkillLoader
}
```

**NewKernel 签名（kernel/kernel.go:82）当前：**
```go
func NewKernel(v *vfs.VFS, ctxMgr *cruxctx.Manager, cb KernelCallbacks) *KernelImpl
// ⬇️ 修改为
func NewKernel(v *vfs.VFS, ctxMgr *cruxctx.Manager, skillLoader *skills.SkillLoader, cb KernelCallbacks) *KernelImpl
```

**Spawn 签名（kernel/kernel.go:92）不变：**
```go
func (k *KernelImpl) Spawn(intent string, skills []string, opts SpawnOpts) (types.PID, error)
```
`skills` 参数已存在并传递给 `NewProcess()`，目前未使用 SkillLoader 加载。

### Skill 注入插入点

在 `kernel/kernel.go` 的 `Spawn()` 中，**创建 Process 之后、分配上下文之前**（约第 96-100 行之间）插入 Skill 加载逻辑：

```go
proc := NewProcess(0, intent, skills)

// ← 在此处插入 Skill 加载逻辑
if len(skills) > 0 && k.skillLoader != nil {
    skillInfo, err := k.skillLoader.Load(skills[0])
    if err != nil {
        return 0, NewSyscallError("Spawn", proc.PID, "skill:"+skills[0], err, types.ErrNotFound)
    }
    proc.AllowedDevices = skillInfo.Manifest.Tools
    // 注入 instructions
    if opts.SystemPrompt == "" {
        opts.SystemPrompt = skillInfo.Instructions
    } else {
        opts.SystemPrompt = opts.SystemPrompt + "\n\n" + skillInfo.Instructions
    }
    // 模型选择（CLI > Skill > 默认）
    if opts.Model == "" && skillInfo.Manifest.Models.Preferred != "" {
        opts.Model = skillInfo.Manifest.Models.Preferred
    }
}

cid, err := k.ctxMgr.CtxAlloc(DefaultCtxSize)
```

### 权限检查插入点

在 `kernel/kernel.go` 的 `reasonStep()` 中，`case ActionToolCall:` 分支（约第 307 行），`k.vfs.Open()` 调用之前：

```go
case ActionToolCall:
    // 保留已有的 assistant message 追加（第 309-312 行）

    // ← 在此处插入权限检查
    if len(proc.AllowedDevices) > 0 {
        allowed := false
        for _, dev := range proc.AllowedDevices {
            if action.ToolPath == dev || strings.HasPrefix(action.ToolPath, dev+"/") {
                allowed = true
                break
            }
        }
        if !allowed {
            // 将权限错误作为工具结果追加到上下文，让 LLM 知道
            permErr := fmt.Sprintf("permission denied: device %s not in allowed list %v", action.ToolPath, proc.AllowedDevices)
            _ = k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, permErr)
            k.emitEvent(proc, "ReasonStep", map[string]any{
                "step":   step,
                "action": "permission_denied",
                "tool":   action.ToolPath,
            }, nil, fmt.Errorf(permErr), time.Since(stepStart))
            continue // 继续下一轮 reasonStep，不终止进程
        }
    }

    // 打开工具设备（已有代码第 315 行）
    toolFD, err := k.vfs.Open(proc.PID, action.ToolPath, vfs.O_RDWR)
```

**设计决策：权限被拒时不终止进程**
- 将错误信息追加到 context，让 LLM 知道该工具不可用
- LLM 可以选择使用其他可用工具或直接给出文本回复
- 这比立即终止进程更优雅，更符合智能体的交互模式

### CLI 修改（cmd/crux/main.go）

**当前的 Spawn 调用（第 194 行）：**
```go
pid, err := kern.Spawn(intent, nil, kernel.SpawnOpts{Model: flagModel, MaxTurns: flagMaxSteps})
```

**修改后：**
```go
// 初始化 SkillLoader
skillLoader := skills.NewSkillLoader("lib/skills")

// 修改 NewKernel 调用
kern := kernel.NewKernel(vfsInst, ctxMgr, skillLoader, cb)

// 构建 skills 列表
var skillsList []string
if flagSkill != "" {
    skillsList = []string{flagSkill}
}
pid, err := kern.Spawn(intent, skillsList, kernel.SpawnOpts{Model: flagModel, MaxTurns: flagMaxSteps})
```

### 模型选择优先级

```
1. CLI --model 标志          （最高优先级）
2. Skill manifest.models.preferred
3. 驱动默认模型 "sonnet"      （最低优先级）
```

### SkillLoader nil 安全

`NewKernel` 的 `skillLoader` 参数允许 `nil`（向后兼容测试场景）。Spawn 中检查 `k.skillLoader != nil` 后才尝试加载。

### 现有 SkillManifest 类型（skills/types.go）

```go
type SkillManifest struct {
    Name          string      `yaml:"name"`
    Description   string      `yaml:"description"`
    Tools         []string    `yaml:"tools"`           // ← 设备白名单来源
    Models        SkillModels `yaml:"models"`
    ContextBudget int         `yaml:"context_budget"`
}

type SkillModels struct {
    Provider  string `yaml:"provider"`   // "claude"
    Preferred string `yaml:"preferred"` // "sonnet"
    Fallback  string `yaml:"fallback"`
}

type SkillInfo struct {
    Manifest     SkillManifest
    Instructions string  // ← 注入到 system prompt
}
```

### 参考实现模式

**Context SetSystemPrompt（context/context.go）：**
- `SetSystemPrompt(cid, prompt)` 设置 system prompt
- `BuildPrompt(cid)` 返回 `PromptResult{SystemPrompt, Messages}`
- Spawn 中已有调用模式（kernel/kernel.go:105-110）

**LLM Request 中 system prompt 传递（kernel/kernel.go:243-244）：**
```go
req := llmRequest{
    SystemPrompt: promptResult.SystemPrompt,  // ← 来自 BuildPrompt
    ...
}
```

**VFS Open 路由（vfs/vfs.go:175-187）：**
- 通过 DeviceRegistry 路由到设备驱动
- 支持精确匹配和前缀匹配（`/dev/fs/path/to/file` 匹配注册的 `/dev/fs`）

### 前序 Story 经验（必须吸收）

**Story 2.3 Code Review 关键发现：**

| 问题 | Shell 驱动启示 → Story 2.4 启示 |
|------|------------------------------|
| H1: drivers/ 导入 kernel 违反架构 | **skills/ 同样不能导入 kernel** — Task 0 必须修复 |
| H2: 错误类型不一致 | **Spawn 中 Skill 加载错误统一用 SyscallError 包装** |
| M2: VFS 层 DriverError 码提取 | **权限拒绝在 kernel 层处理，不走 VFS** |

**Story 2.1 实现模式（SkillLoader）：**
- `SkillLoader.Load(skillName)` 返回 `*SkillInfo`
- 包含 `Manifest`（解析后的 YAML）和 `Instructions`（原始 markdown 文本）
- 测试用 `testdata/mock-skill/` fixture

**Story 1.6 reasonStep 模式（kernel/kernel.go:197-355）：**
- ActionToolCall 分支：Open → Write → Read → Close → AppendToolResult
- 所有错误导致 finishProcess（进程终止）
- 权限检查应在 Open 之前，拒绝时 continue 而非 finishProcess

### Git 智能分析

**最近 10 次提交：**
```
45453a0 Update Story 2.3 status to 'done' and finalize Shell Driver implementation
621e064 Enhance Shell Driver Implementation with Additional Tests and Error Handling
9c0bc04 Update Story 2.3 status to review and finalize Shell Driver implementation
823a77a Add Story 2.3: Shell Driver Implementation
d401528 Enhance Host Filesystem Driver with Error Handling and Unit Tests
f4a42dd Finalize Story 2.2: Host Filesystem Driver Implementation
3d0ef3e Implement Host Filesystem Driver for /dev/fs
```

**相关模式：**
- 每个 Story 作为独立单元实现和提交
- `go test -race ./...` 作为质量门禁
- 循环依赖在 Story 2.2 中通过移除 kernel 导入解决（drivers/fs 案例）
- 本 Story 需要对 skills/ 做相同处理

### NFR 合规要求

| NFR | 要求 | 实现策略 |
|-----|------|---------|
| NFR16 | Skill tools 白名单作为设备权限边界 | `Process.AllowedDevices` + reasonStep 前置检查 |
| NFR17 | MVP 无完整 Capability，Skill tools 为最小安全边界 | 无 Skill 时 AllowedDevices 为空 = 全部可访问 |

### AC #5 工具结果上下文追加

AC #5（FR12）**已在 Story 1.6 中实现**。当前 `reasonStep()` 的 `ActionToolCall` 分支已调用 `k.ctxMgr.AppendToolResult()`（kernel/kernel.go:339）。本 Story 无需额外修改，仅需验证权限拒绝场景下也正确追加了错误信息到上下文。

### Project Structure Notes

**本 Story 修改的文件：**

```
skills/loader.go              (修改 — 移除 kernel 导入，~2 行改动)
kernel/process.go             (修改 — 添加 AllowedDevices 字段，1 行)
kernel/kernel.go              (修改 — 添加 skillLoader + Spawn 注入 + 权限检查，~40 行)
cmd/crux/main.go              (修改 — 添加 --skill 标志 + SkillLoader 初始化，~10 行)
kernel/kernel_test.go         (修改 — 新增 ~9 个测试用例)
```

**不需要修改的文件：**
- `skills/types.go` — SkillManifest、SkillInfo 类型已完整
- `vfs/` 下任何文件 — 权限检查在 kernel 层
- `drivers/` 下任何文件 — 无需变更
- `context/` 下任何文件 — SetSystemPrompt/BuildPrompt 已存在
- `internal/types/types.go` — ErrPermission 已定义
- `go.mod` / `go.sum` — 无新外部依赖

**不需要创建的文件：**
- `lib/skills/code-analyst/manifest.yaml` 和 `instructions.md` — 留给 Story 2.5

### 与架构文档的一致性

| 架构要求 | 本 Story 实现 |
|---------|-------------|
| Skill manifest tools 声明映射为设备权限白名单（NFR16） | ✅ Task 1 + Task 4 |
| spawn 时指定加载 Skill（FR25） | ✅ Task 3 |
| instructions.md 注入 system prompt（FR24） | ✅ Task 3.5 |
| Skill tools 白名单（FR26） | ✅ Task 4 |
| 依赖方向严格单向 | ✅ Task 0 修复循环依赖 |
| MVP 无完整 Capability，Skill tools 为最小安全边界（NFR17） | ✅ Task 4.6 |

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.4] — AC 和 User Story 定义
- [Source: _bmad-output/planning-artifacts/architecture.md#Skill 管理] — FR23-FR27 架构支撑
- [Source: _bmad-output/planning-artifacts/architecture.md#依赖方向] — 依赖约束
- [Source: _bmad-output/planning-artifacts/architecture.md#安全] — NFR15-17 安全规则
- [Source: _bmad-output/project-context.md#安全规则] — Skill tools 白名单规范
- [Source: kernel/kernel.go:73-89] — KernelImpl 结构体和 NewKernel
- [Source: kernel/kernel.go:92-156] — Spawn 函数（注入切入点）
- [Source: kernel/kernel.go:197-355] — reasonStep 函数（权限检查切入点）
- [Source: kernel/kernel.go:307-349] — ActionToolCall 分支
- [Source: kernel/process.go:32-52] — Process 结构体（添加 AllowedDevices）
- [Source: skills/loader.go:40-78] — SkillLoader.Load 方法
- [Source: skills/loader.go:9] — **循环依赖来源**：`import kernel`
- [Source: skills/types.go:11-17] — SkillManifest（Tools 字段 = 白名单）
- [Source: vfs/vfs.go:175-187] — VFS.Open（设备路由，不修改）
- [Source: cmd/crux/main.go:145-151] — CLI flags 注册点
- [Source: cmd/crux/main.go:167-242] — runRoot 函数（CLI 修改点）
- [Source: cmd/crux/main.go:182-190] — 依赖注入链（添加 SkillLoader）
- [Source: cmd/crux/main.go:194] — Spawn 调用点
- [Source: context/context.go] — SetSystemPrompt + BuildPrompt
- [Source: internal/types/types.go:24] — ErrPermission 常量已定义
- [Source: 2-3-shell-driver-dev-shell.md] — 前序 Story 经验

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- ✅ Task 0: 移除 `skills/loader.go` 对 `kernel` 包的导入，`kernel.NewSyscallError` 替换为 `fmt.Errorf`，同步更新 `skills/loader_test.go` 中的错误断言
- ✅ Task 1: `Process` 结构体添加 `AllowedDevices []string` 字段，nil/空=全部可访问，非空=白名单
- ✅ Task 2: `KernelImpl` 添加 `skillLoader *skills.SkillLoader` 字段，`NewKernel` 签名新增 `skillLoader` 参数，同步更新所有调用点（kernel_test.go、main.go、integration_test.go）
- ✅ Task 3: `Spawn()` 中实现 Skill 加载逻辑 — instructions 注入到 SystemPrompt、AllowedDevices 从 manifest.Tools 设置、模型优先级 CLI > Skill > 默认、emitEvent 记录 loaded_skill
- ✅ Task 4: `reasonStep()` ActionToolCall 分支添加设备权限检查 — 精确匹配 + 前缀匹配（dev+"/")），拒绝时将错误追加到上下文（不终止进程），emitEvent 记录 permission_denied
- ✅ Task 5: CLI 添加 `--skill` 标志，初始化 `SkillLoader("lib/skills")`，构建 skillsList 传入 Spawn
- ✅ Task 6: 新增 9 个单元测试 — 5 个 Spawn/Skill 测试 + 4 个权限检查测试，使用 `skills/testdata/mock-skill` fixture 和 `capturingLLMFile` mock
- ✅ Task 7: 全量回归通过 — `go test -race ./...` 全部 PASS，`go vet ./...` 无警告

### Code Review Fixes Applied

- **[H1] AC #2 文本修正** — 更新 AC #2 文本，移除"返回 *SyscallError/ErrPermission"描述，改为匹配实际实现的优雅降级语义（追加错误到上下文，继续推理循环）
- **[M1] AppendToolResult 错误处理** — `kernel/kernel.go` 权限拒绝路径中，`AppendToolResult` 错误不再被 `_` 忽略，改为终止进程（与正常工具结果路径一致）
- **[M2] 路径遍历防护** — `kernel/kernel.go` 权限检查前添加 `path.Clean()` 规范化 `action.ToolPath`，防止 `/dev/fs/../shell` 绕过白名单
- **[M3] 集成测试覆盖** — `cmd/crux/integration_test.go` 新增 `TestE2E_WithSkill_InjectsInstructions`，验证 CLI→SkillLoader→Spawn→AllowedDevices 端到端路径
- **[L1] 错误构造简化** — `fmt.Errorf("%s", permErr)` 替换为 `errors.New(permErr)`
- **新增测试** — `kernel/kernel_test.go` 新增 `TestReasonStep_PathTraversal_Blocked`，验证路径遍历攻击被正确拦截

### File List

- `skills/loader.go` — 修改：移除 kernel/types 导入，NewSyscallError 替换为 fmt.Errorf
- `skills/loader_test.go` — 修改：移除 kernel/types 导入，更新 DirNotFound 测试断言
- `kernel/process.go` — 修改：Process 结构体添加 AllowedDevices 字段
- `kernel/kernel.go` — 修改：添加 skills/fmt/strings 导入，KernelImpl 添加 skillLoader，NewKernel 签名变更，Spawn 添加 Skill 加载注入，reasonStep 添加权限检查
- `kernel/kernel_test.go` — 修改：添加 errors/strings/skills 导入，更新 NewKernel 调用，新增 9 个测试和 2 个辅助类型
- `cmd/crux/main.go` — 修改：添加 skills 导入，新增 flagSkill 变量和 --skill 标志，初始化 SkillLoader，Spawn 传入 skillsList
- `cmd/crux/integration_test.go` — 修改：更新 NewKernel 调用签名

## Change Log

- 2026-02-24: 实现 Story 2.4 — Skill 注入与设备权限白名单。修复 skills 包循环依赖，实现 Spawn 中 Skill 加载/instructions 注入/模型选择，实现 reasonStep 中设备权限白名单检查，添加 CLI --skill 标志，新增 9 个单元测试。
