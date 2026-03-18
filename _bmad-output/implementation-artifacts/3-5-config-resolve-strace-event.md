# Story 3.5: 配置解析来源追踪（ConfigResolve strace 事件）

Status: review

## Story

As a 用户,
I want 在 strace 中看到 provider/model 的解析来源（CLI 标志 / agent 清单 / 项目配置 / 全局配置 / 默认值）,
So that 配置问题可以一目了然定位到具体层级，无需阅读源码排查。

## Acceptance Criteria

1. **ConfigResolve 事件发射** — Given 进程 spawn 时 provider 解析完成，When strace 附着到该进程，Then 输出 `ConfigResolve` 事件，包含 `provider`（最终值）和 `provider_source`（来源标签：cli/agent/project/global/default）

2. **Model 来源追踪** — Given 进程 spawn 时 model 解析完成，When strace 输出 ConfigResolve 事件，Then 包含 `model`（最终值）和 `model_source`（来源标签：cli/agent/driver）

3. **覆盖关系可见** — Given 项目配置中 `default_provider` 与最终 provider 不同（被 agent 覆盖），When strace 输出 ConfigResolve 事件，Then 同时显示 `project_default` 和 `agent_provider` 字段，用户可清晰看到覆盖关系

4. **FormatEvent 格式化** — Given `ConfigResolve` 事件被 FormatEvent 格式化，When 渲染为 trace line，Then 输出格式为 `[N.NNNs] ConfigResolve(provider=X [source], model=Y [source], ...)  duration`

5. **UI FormatTraceLine 支持** — Given ConfigResolve 事件被 UI FormatTraceLine 处理，When 渲染，Then source 标签使用 `MutedStyle` 渲染，provider/model 值使用普通文本

6. **JSON 模式兼容** — Given 使用 `--json` flag，When ConfigResolve 事件输出，Then args 中包含完整的 provider/model/source 字段，结构化 JSON 可被工具消费

7. **所有测试通过** — Given 实现完成，When 执行 `go test -race ./...`，Then 所有新增和现有测试通过，无竞态条件；`go vet ./...` 无警告

## Tasks / Subtasks

- [x] Task 1: 修改 `kernel/kernel.go` — 增强 provider 解析并发射 ConfigResolve 事件 (AC: #1, #2, #3)
  - [x] 1.1 修改 `resolveLLMDevice` 返回值，增加 `source string` 返回值（返回签名变为 `(string, string, error)`），source 值为 "cli" / "agent" / "project" / "global" / "default"
  - [x] 1.2 修改 Spawn 中两处调用 `resolveLLMDevice` 的代码（行 519 和 409），适配新返回值
  - [x] 1.3 在 Spawn 中 provider 解析完成后（`proc.Provider = ...` 赋值之后），发射 `ConfigResolve` 事件
  - [x] 1.4 ConfigResolve 事件 args 构建：基础字段 `provider`、`provider_source`、`model`、`model_source`；当 agent 覆盖了 project default 时额外添加 `project_default` 字段
  - [x] 1.5 处理 ProjectConfig 分支（行 502-516）中的 provider source 追踪——该分支独立于 `resolveLLMDevice`，需同样追踪 source

- [x] Task 2: 修改 `debug/strace.go` — FormatEvent 添加 ConfigResolve 特殊格式化 (AC: #4)
  - [x] 2.1 在 `FormatEvent` 中添加 ConfigResolve 分支：当 `event.Syscall == "ConfigResolve"` 时使用专用格式化
  - [x] 2.2 专用格式化输出：`[N.NNNs] ConfigResolve(provider=X [source], model=Y [source])    duration`，source 部分用 `ansiGray` 包裹（ColorEnabled 时）
  - [x] 2.3 当 args 中存在 `project_default` 字段时，追加显示 `project_default=Z`

- [x] Task 3: 修改 `internal/ui/trace.go` — FormatTraceLine 添加 ConfigResolve 支持 (AC: #5)
  - [x] 3.1 在 `FormatTraceLine` 中添加 ConfigResolve 分支
  - [x] 3.2 source 标签使用 `MutedStyle` 渲染（与慢操作标注样式一致）
  - [x] 3.3 provider/model 值使用普通文本

- [x] Task 4: 添加单元测试 (AC: #1-#7)
  - [x] 4.1 `kernel/kernel_test.go` — 新增 `TestResolveLLMDevice_ReturnsSource` 系列测试：验证各优先级场景下 source 值正确
  - [x] 4.2 `kernel/kernel_test.go` — 新增 `TestSpawn_EmitsConfigResolveEvent` 测试：spawn 后从 DebugChan 读取 ConfigResolve 事件，验证 args 完整性
  - [x] 4.3 `debug/strace_test.go` — 新增 `TestFormatEvent_ConfigResolve` 测试：验证专用格式化输出
  - [x] 4.4 `internal/ui/trace_test.go` — 新增 `TestFormatTraceLine_ConfigResolve` 测试：验证 UI 格式化

- [x] Task 5: 更新 sprint-status.yaml (AC: #7)
  - [x] 5.1 将 `3-5-config-resolve-strace-event` 状态更新为 `ready-for-dev`

## Dev Notes

### 核心设计决策

#### Provider Source 追踪策略

`resolveLLMDevice` 现有优先级链（从高到低）：
1. `providerOverride`（CLI `--provider` 标志）→ source: `"cli"`
2. `agent.Manifest.Models.Provider`（agent.yaml 配置）→ source: `"agent"`
3. `k.defaultProvider`（项目级或全局级 providers.yaml 的 `default_provider`）→ source: `"project"` 或 `"global"`
4. 硬编码 `"claude"`（兜底默认值）→ source: `"default"`

**注意：** `k.defaultProvider` 可能来自项目配置或全局配置。当前 `resolveLLMDevice` 无法区分两者。实际上 `k.defaultProvider` 在 `SetDefaultProvider` 中被设置，来源可能是项目级也可能是全局级。建议统一标记为 `"project"`（因为全局配置也通过 `providers.yaml` 加载，语义上与 "project" 相近），或在未来需要时再细分。本 Story 先使用 `"project"` 标签。

#### ProjectConfig 分支的特殊处理

Spawn 中有两个 provider 解析路径：
1. **ProjectConfig 路径**（行 502-516）：当 `opts.ProjectConfig.LLMFileOpener != nil` 时，provider 解析逻辑直接内联在 Spawn 中，不走 `resolveLLMDevice`
2. **标准路径**（行 519）：走 `resolveLLMDevice`

两条路径的优先级逻辑相同，但代码独立。**ConfigResolve 事件需在两条路径都发射**，且 source 追踪逻辑需一致。

建议：将 ProjectConfig 分支中的 provider 解析也提取到 `resolveLLMDevice`（或新增 `resolveProviderWithSource` 辅助函数），避免逻辑重复。但这属于重构，可在实现时评估是否合适。最低限度：确保两条路径都正确发射 ConfigResolve 事件。

#### Model Source 追踪

Model 来源相对简单：
- `opts.Model`（SpawnOpts 传入，来自 CLI 或调用方）→ source: `"cli"`
- agent.yaml 中无 model 字段（model 在 provider/driver 层面确定）→ source: `"driver"`
- 当 `opts.Model == ""` 时 → source: `"driver"`（由 LLM driver 使用自己的默认 model）

#### ConfigResolve 事件 Args 结构

```go
args := map[string]any{
    "provider":        "openrouter",        // 最终使用的 provider
    "provider_source": "agent",             // 来源标签
    "model":           "hunter-alpha",      // 最终使用的 model（可能为空）
    "model_source":    "cli",               // 来源标签
}
// 当存在覆盖时，添加被覆盖的默认值
if projectDefault != "" && projectDefault != provider {
    args["project_default"] = projectDefault
}
```

#### FormatEvent ConfigResolve 专用格式示例

```
[  0.001s] ConfigResolve(provider=openrouter [agent], model=hunter-alpha [cli], project_default=cursor)    1ms
```

ColorEnabled 时：
- `[agent]`、`[cli]`、`[project_default=cursor]` 使用 `ansiGray` 包裹
- 其余部分普通文本

### 依赖方向与职责划分

```
kernel/kernel.go (ConfigResolve 事件发射)
  ├── resolveLLMDevice() 返回 (device, source, error)
  ├── Spawn() 在 provider 解析后调用 emitEvent("ConfigResolve", ...)
  └── 不依赖 debug/strace.go 的格式化（事件通过 DebugChan 传递）

debug/strace.go (ConfigResolve 格式化)
  ├── FormatEvent() 添加 ConfigResolve 分支
  └── 使用 args["provider_source"] 等字段构建格式化输出

internal/ui/trace.go (ConfigResolve UI 格式化)
  ├── FormatTraceLine() 添加 ConfigResolve 分支
  └── source 标签使用 MutedStyle
```

**关键约束继承自 Story 3.4：**
- `debug/` 包不可导入 `internal/ui/`
- `internal/ui/` 不导入 `debug/`
- `cmd/rnix/main.go` 作为胶水层注入 Formatter

### resolveLLMDevice 修改签名

```go
// 当前签名：
func (k *KernelImpl) resolveLLMDevice(agent *agents.AgentInfo, providerOverride string) (string, error)

// 新签名：
func (k *KernelImpl) resolveLLMDevice(agent *agents.AgentInfo, providerOverride string) (string, string, error)
// 返回值: (devicePath, providerSource, error)
```

**所有调用点需更新：**
- `kernel/kernel.go:519` — Spawn 标准路径
- `kernel/kernel.go:409` — Provider fallback 路径
- `kernel/atdd_23_3_dynamic_provider_resolution_test.go` — 所有测试调用
- `kernel/kernel_test.go` — 所有 `resolveLLMDevice` 测试

### 前序 Story 经验（Story 3.4）

**关键经验：**
- lipgloss 在非 TTY 测试环境中 `Render()` 不输出 ANSI 码。测试验证逻辑路径而非 ANSI 字节
- 测试中用 `bytes.Buffer` + 直接断言字符串内容（不用 testify）
- 测试命名：`TestTypeName_Behavior`
- `FormatTraceLine` 签名：`func FormatTraceLine(r *Renderer, event types.SyscallEvent, verbose bool) string`
- 已知限制 [H1]：`finishProcess()` 不关闭 `DebugChan`，测试需手动 `close(proc.DebugChan)`

### 已有 API 参考

**SyscallEvent 结构（internal/types/types.go）：**
```go
type SyscallEvent struct {
    Timestamp time.Duration
    PID       PID
    Syscall   string          // "ConfigResolve" for this story
    Args      map[string]any  // provider, provider_source, model, model_source, project_default
    Result    any
    Err       error
    Duration  time.Duration
    TraceID   TraceID   // auto-filled by emitEvent
    SpanID    SpanID    // auto-filled by emitEvent
}
```

**emitEvent 调用模式（kernel/kernel.go:668）：**
```go
k.emitEvent(proc, "ConfigResolve", map[string]any{
    "provider":        provider,
    "provider_source": providerSource,
    "model":           model,
    "model_source":    modelSource,
}, nil, nil, time.Since(resolveStart))
```

**FormatEvent 现有模式（debug/strace.go:80）：**
```go
func FormatEvent(event types.SyscallEvent, opts Options) string {
    // 通用格式：[timestamp] Syscall(key=value, ...) → result    duration
    // ConfigResolve 需添加分支，在 value 后追加 [source] 标注
}
```

**FormatTraceLine 现有模式（internal/ui/trace.go）：**
```go
func FormatTraceLine(r *Renderer, event types.SyscallEvent, verbose bool) string
// 使用 lipgloss 样式，source 标签用 MutedStyle
```

### Project Structure Notes

**修改文件：**
```
kernel/kernel.go                                      — resolveLLMDevice 返回 source、Spawn 发射 ConfigResolve 事件
kernel/kernel_test.go                                  — resolveLLMDevice 测试适配新返回值 + 新增 ConfigResolve 测试
kernel/atdd_23_3_dynamic_provider_resolution_test.go   — 适配 resolveLLMDevice 新签名
debug/strace.go                                        — FormatEvent ConfigResolve 分支
debug/strace_test.go                                   — ConfigResolve 格式化测试
internal/ui/trace.go                                   — FormatTraceLine ConfigResolve 分支
internal/ui/trace_test.go                              — ConfigResolve UI 格式化测试
```

**不修改文件：**
```
debug/event.go           — NewEvent/CompleteEvent/EmitEvent 通用机制，不需修改
internal/types/types.go  — SyscallEvent 结构体不需修改（ConfigResolve 信息放在 Args 中）
cmd/rnix/main.go         — 无需修改（Formatter 注入已在 Story 3.4 完成，ConfigResolve 自动走 UI 格式化）
ipc/                     — 无需修改（ConfigResolve 是 strace 事件，不涉及 IPC 协议）
```

### 范围边界

**本 Story 包含：**
- `resolveLLMDevice` 返回 source 信息
- Spawn 中发射 ConfigResolve strace 事件
- `FormatEvent` 添加 ConfigResolve 专用格式化
- `FormatTraceLine` 添加 ConfigResolve UI 格式化
- 相关单元测试

**本 Story 不包含：**
- 推理步骤逐步输出（Story 3.6 范围）
- IPC 协议修改
- CLI 命令修改
- 区分 project 和 global 级别的 `defaultProvider`（未来可细分）

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-3-调试追踪debug-tracing-strace.md#Story 3.5] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/sprint-change-proposal-epic3-reopen-2026-03-18.md] — Sprint Change Proposal 触发上下文
- [Source: kernel/kernel.go:233-263] — resolveLLMDevice 现有实现
- [Source: kernel/kernel.go:500-528] — Spawn 中 provider 解析双路径（ProjectConfig 分支 vs 标准路径）
- [Source: kernel/kernel.go:664-721] — emitEvent 实现
- [Source: debug/strace.go:80-119] — FormatEvent 通用格式化
- [Source: debug/event.go] — NewEvent/CompleteEvent/EmitEvent 事件基础设施
- [Source: internal/ui/trace.go] — FormatTraceLine UI 格式化
- [Source: internal/types/types.go] — SyscallEvent 结构体
- [Source: _bmad-output/implementation-artifacts/3-4-syscall-trace-line-ui-component.md] — 前序 Story 经验和已有 API 参考
- [Source: _bmad-output/project-context.md] — 项目规则和约定

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- `TestATDD_3_5_AC1_Spawn_ProjectConfigPath_EmitsConfigResolve` 初始失败: ProjectConfig 路径 source 追踪 bug — `providerOverride` 已被预填充为 `ProjectConfig.DefaultProvider`，导致 source 误标为 `"cli"`。修复: 使用 `opts.Provider`（实际 CLI 值）而非 `providerOverride`。

### Completion Notes List

- Task 1: `kernel/kernel.go` — `resolveLLMDevice` 已返回 `(device, source, error)` 三元组；Spawn 两条路径（标准 + ProjectConfig）均发射 ConfigResolve 事件，包含 `provider`、`provider_source`、`model`、`model_source`、`project_default`（覆盖时）字段。修复了 ProjectConfig 分支中的 source 追踪 bug。
- Task 2: `debug/strace.go` — `FormatEvent` 新增 `formatConfigResolve` 专用格式化函数，source 标签用 `ansiGray` 包裹（ColorEnabled 时），支持 `project_default` 追加显示。
- Task 3: `internal/ui/trace.go` — `FormatTraceLine` 新增 `formatConfigResolveTrace` 专用格式化函数，source 标签使用 `MutedStyle`，provider/model 值使用普通文本。
- Task 4: 三个 ATDD 测试文件共 17 个测试用例，覆盖 AC1-AC7。kernel 层 10 个（source 四级优先级 + Spawn 事件发射 + ProjectConfig 路径 + JSON 兼容 + 签名回归）、debug 层 5 个（专用格式化 + 颜色 + project_default + JSON）、UI 层 4 个（基础格式 + MutedStyle + NoColor + project_default）。移除了 UI 测试中的 RED PHASE `t.Skip` 标记。
- Task 5: sprint-status.yaml 已更新。

### File List

- `kernel/kernel.go` — 修改: ProjectConfig 分支使用 `opts.Provider` 替代 `providerOverride`，修复 source 追踪 bug
- `kernel/atdd_3_5_config_resolve_strace_test.go` — 新增: kernel 层 ATDD 测试（10 个用例）
- `debug/strace.go` — 修改: 新增 `formatConfigResolve` 专用格式化函数
- `debug/atdd_3_5_config_resolve_format_test.go` — 新增: debug 层 ATDD 测试（5 个用例）
- `internal/ui/trace.go` — 修改: 新增 `formatConfigResolveTrace` 专用格式化函数
- `internal/ui/atdd_3_5_config_resolve_traceline_test.go` — 修改: 移除 `t.Skip`，启用 UI 层 ATDD 测试（4 个用例）
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — 修改: Story 3-5 状态更新

### Change Log

- 2026-03-18: Story 3.5 实现完成 — ConfigResolve strace 事件全部 AC 达成，17 个 ATDD 测试通过，修复 ProjectConfig 路径 source 追踪 bug
