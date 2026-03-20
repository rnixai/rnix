---
title: '内核原生 ToolCalls 支持与 VFS 设备自描述架构'
slug: 'native-toolcalls-vfs-tooldescriptor'
created: '2026-03-20'
status: 'implementation-complete'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go 1.26', 'JSON Schema', 'OpenAI Function Calling', 'MCP tools/list']
files_to_modify:
  - 'vfs/vfs.go'
  - 'vfs/dev.go'
  - 'vfs/dev_test.go'
  - 'context/context.go'
  - 'context/context_test.go'
  - 'drivers/llm/driver.go'
  - 'drivers/llm/vfsfile.go'
  - 'drivers/llm/vfsfile_test.go'
  - 'drivers/shell/shell.go'
  - 'drivers/shell/shell_test.go'
  - 'drivers/fs/hostfs.go'
  - 'drivers/fs/hostfs_test.go'
  - 'cmd/rnix/main.go'
  - 'kernel/kernel.go'
  - 'kernel/process.go'
  - 'kernel/toolgen.go (new)'
  - 'kernel/toolgen_test.go (new)'
  - 'kernel/kernel_test.go'
code_patterns:
  - 'Go optional interface (io.WriterTo, http.Flusher)'
  - 'compile-time interface check: var _ Interface = (*Type)(nil)'
  - 'type assertion: if td, ok := driver.(ToolDescriptor); ok'
test_patterns:
  - 'mock ToolCallingDriver for e2e kernel tests'
  - 'table-driven ToolDef generation tests'
adversarial_review:
  date: '2026-03-20'
  findings_total: 29
  critical_fixed: ['F7-fallback-degradation', 'F3-writestream-toolcalls']
  high_fixed: ['F2-llmresponse-fields', 'F4-registry-consistency', 'F5-getfile-exists', 'F8-devfs-read', 'F10-buildtooldefs-traversal', 'F13-planprotocol-injection', 'F17-permission-check', 'F23-task-dependencies', 'F27-task14-split']
  medium_fixed: ['F9-toolcall-compat', 'F11-meta-schema', 'F14-native-fallback-parse', 'F15-mcp-mixed-mode', 'F20-hostfs-compat', 'F22-stream-tests', 'F26-input-type', 'F28-driver-message-conversion']
---

# Tech-Spec: 内核原生 ToolCalls 支持与 VFS 设备自描述架构

**Created:** 2026-03-20
**Adversarial Review:** 2026-03-20 (29 findings, all addressed)

## Overview

### Problem Statement

当前内核通过 `toolProtocol` 硬编码文本告诉 LLM 如何调用 VFS 设备。这有两个严重问题：

1. **不可靠**：不同 LLM provider 对文本协议的遵循度不同，弱模型输出错误 JSON 格式（如 `sh: 1: {command:: not found`）
2. **不可扩展**：添加新设备需要修改 `kernel/kernel.go` 的 `toolProtocol` 常量（6 处变更），设备与内核强耦合

OpenAI-compatible 驱动已实现 `ToolCallingDriver` 接口（原生函数调用），但内核完全没有使用 — `llmResponse` 甚至没有 `ToolCalls` 字段。

### Solution

引入 `vfs.ToolDescriptor` 可选接口，让 VFS 设备自描述工具能力（JSON Schema 格式的 ToolDef）。内核在 spawn 时收集 ToolDefs，对支持 `ToolCallingDriver` 的 LLM 驱动使用原生函数调用。CLI 驱动继续走文本协议，且 toolProtocol 从 ToolDefs 自动生成。

### Scope

**In Scope:**
- `vfs.ToolDef` / `vfs.ToolDescriptor` / `vfs.ToolCapable` 类型定义
- DeviceRegistry 扩展存储 driver 引用
- Context Message 添加 ToolCalls 字段
- LLMRequest.Tools 字段 + VFS bridge 路由到 CallWithTools
- Shell/FS 驱动实现 ToolDescriptor（FS 拆分为 read_file/write_file/list_dir）
- kernel/toolgen.go — ToolDef 收集 + meta 动作定义 + toolMap + toolProtocol 自动生成
- reasonStep 双路径（native tools vs text protocol）
- executeNativeToolCalls() — 拆分为 VFS 工具执行 + Meta 动作执行两个子函数
- Fallback 降级策略：native tools 进程 fallback 时注入 toolProtocol 文本
- Mixed mode：MCP 工具通过文本协议补充描述注入系统提示

**Out of Scope:**
- 并行工具调用执行（本次顺序执行，预留架构）
- AnthropicDriver / GeminiDriver 新 LLM 驱动（接口已兼容，实现留后续）
- MCP tools/list 动态发现转为 native ToolDef（架构预留，本次 MCP 走 mixed mode 文本描述）
- Anthropic Tool Search / defer_loading 优化

## Context for Development

### Codebase Patterns

- **可选接口模式**：项目已有 `HealthChecker`（`drivers/llm/tools.go:66-69`）、`StreamObserver`（`vfs/vfs.go:39-45`）作为可选接口，`ToolDescriptor` 遵循相同模式
- **包隔离**：`context.Message` 和 `llm.Message` 是独立类型、JSON 兼容，内核做转换。`vfs.ToolDef` 和 `llm.ToolDef` 同理
- **DeviceRegistry**：`vfs/dev.go` — 使用 `xsync.Registry[VFSFileFactory]`（非 SyncMap），扩展需使用平行 SyncMap 或改底层类型
- **VFS.GetFile**：`vfs/vfs.go:222-233` 已存在，内核在 Spawn 中（kernel.go:638）已用于设置 StreamObserver
- **reasonStep 循环**：核心推理循环，需要添加 native tools 分支
- **OpenAI driver message 转换**：`openai_compat.go:204-260` 的 `buildMessages` 和 `openai_official.go:143-159` 的 `convertMessageToSDK` 已正确处理 `llm.Message.ToolCalls`，无需修改

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `vfs/vfs.go:31-50` | VFSFile 接口、VFSFileFactory、StreamObserver |
| `vfs/vfs.go:222-233` | VFS.GetFile（已存在，Spawn 中已使用） |
| `vfs/dev.go:15-30` | DeviceRegistry — 使用 xsync.Registry |
| `kernel/kernel.go:58-107` | toolProtocol + planProtocol 常量 |
| `kernel/kernel.go:108-128` | ActionType、ReasonAction 结构 |
| `kernel/kernel.go:130-146` | llmRequest、llmResponse 结构 |
| `kernel/kernel.go:637-644` | Spawn 中 StreamObserver 设置（ToolCapable 检测插入点） |
| `kernel/kernel.go:883-886` | attemptFallback 调用位置 |
| `kernel/kernel.go:1093-1096` | toolProtocol/planProtocol 注入位置 |
| `kernel/kernel.go:1315-1517` | ActionToolCall 执行分支（native VFS 工具执行参考） |
| `kernel/kernel.go:1339-1372` | AllowedDevices 权限检查（VFS 路径前缀匹配） |
| `kernel/kernel.go:1978-2052` | parseAction / tryParseStructuredAction |
| `kernel/process.go:33-89` | Process 结构体 |
| `drivers/llm/tools.go:8-19` | llm.ToolDef{Name,Description,Parameters}、llm.ToolCall{ID,Name,Input map[string]any} |
| `drivers/llm/driver.go:18-37` | LLMRequest、LLMResponse（已有 ToolCalls、Reasoning） |
| `drivers/llm/vfsfile.go:48-54` | writeCall（CallWithTools 路由点） |
| `drivers/llm/vfsfile.go:57-113` | writeStream（done 事件 ToolCalls 丢失修复点） |
| `drivers/llm/openai_compat.go:204-260` | buildMessages（已处理 Message.ToolCalls） |
| `drivers/shell/shell.go` | ShellDriver + extractCommand |
| `drivers/fs/hostfs.go:229-271` | FileFactory 无状态闭包、O_RDONLY 模式直接打开文件 |
| `context/context.go:25-30` | Message{Role,Content,ToolCallID} |
| `context/context.go:258-283` | AppendToolResult（第二参数当前是 VFS 路径，非 tool_call_id） |
| `cmd/rnix/main.go:1168-1176` | daemon 启动设备注册 |

### Technical Decisions

1. **ToolDef 放 vfs 包**：避免 kernel→drivers 反向依赖。`vfs.ToolDef` 字段精确匹配 `llm.ToolDef`：`Name string json:"name"`, `Description string json:"description,omitempty"`, `Parameters map[string]any json:"parameters,omitempty"`
2. **ToolDescriptor 在 Driver 对象上实现**：ShellDriver 已有 Driver 结构体；FS 需新建 HostFSDriver
3. **DeviceRegistry 平行 SyncMap 存储 driver**：保留 `xsync.Registry[VFSFileFactory]` 不变，新增 `driverMap *xsync.SyncMap[string, any]`。`RegisterWithDriver` 先写 registry 再写 driverMap；`Unregister` 同时清理两者
4. **FS 拆分为 3 工具**：read_file/write_file/list_dir，参照 Claude Code 风格
5. **toolProtocol 从 ToolDefs 自动生成**：`generateToolProtocol(toolDefs, metaDefs, planningEnabled)` 分别生成工具和 plan 部分，保留 planProtocol 条件注入语义
6. **顺序执行多 ToolCalls**：简单可靠，架构预留并行
7. **Fallback 降级策略（F7 修复）**：attemptFallback 时检测 fallback driver 是否支持 ToolCallingDriver；若不支持，注入自动生成的 toolProtocol 文本到系统提示，req.Tools 清空
8. **MCP Mixed Mode（F15 修复）**：native tools 模式下，MCP 工具通过文本描述补充注入系统提示（"MCP 工具请通过文本协议调用"），而非 native ToolDef
9. **read_file 不需要 Write（F8 修复）**：`executeNativeVFSTool` 对 read_file 使用 O_RDONLY 直接 Read，不调用 Write
10. **context.ToolCall.Input 类型为 `map[string]any`（F9/F26 修复）**：与 llm.ToolCall.Input 完全一致
11. **Process.toolMap 是 Spawn 后不可变的（F21）**：注释标注 `// immutable after Spawn`，与 Intent 字段同级
12. **保留包级 `fs.FileFactory()` 兼容函数（F20 修复）**：新增 `NewDriver()` + 方法 `FileFactory()`，保留包级 `FileFactory()` 调用 `NewDriver().FileFactory()` 作为兼容 wrapper

### Key Technical Constraints

1. **FS 驱动无 Driver 对象** — `drivers/fs/hostfs.go:229` FileFactory 是无状态闭包，需新建 HostFSDriver
2. **LLMFile.writeStream 丢失 ToolCalls** — `drivers/llm/vfsfile.go:83-91` done 事件的 `evt.ToolCalls` 未传递到 LLMResponse。注意：done 事件中的 ToolCalls 来自 OpenAI stream 的 `flushToolCalls`（openai_compat.go:586-614），是在 done 事件而非 tool_call 中间事件中发送
3. **DeviceRegistry 使用 xsync.Registry 而非 SyncMap** — `vfs/dev.go:16` 底层是 `xsync.Registry[VFSFileFactory]`，不能直接存储额外字段
4. **Process 结构体需扩展** — `kernel/process.go:33` 添加 UseNativeTools + toolMap（immutable after Spawn）
5. **Context Message 缺少 ToolCalls** — `context/context.go:25-30` 缺少 ToolCalls 列表
6. **内核 llmResponse 还缺少 Reasoning 字段** — `kernel/kernel.go:143-146` 只有 Content+TokensUsed，Reasoning 也被丢弃（但本 spec 只修复 ToolCalls，Reasoning 留后续）
7. **AllowedDevices 中的 MCP 路径不在 DeviceRegistry 中** — MCP 通过 MountManager 注册到 DeviceRegistry，GetDriver 需要处理
8. **AppendToolResult 的 toolCallID 参数语义不同** — 文本协议路径传 VFS 路径，native tools 路径传 tool_call_id。两者不冲突但不能混用

## Implementation Plan

### Task Dependencies

```
Task 1 ──→ Task 6, Task 7, Task 9
Task 2 ──→ Task 8, Task 9
Task 3 ──→ Task 14a
Task 4 ──→ Task 5
Task 5 ──→ Task 12
Task 6, 7 ──→ Task 8
Task 8, 9 ──→ Task 11
Task 10 ──→ Task 11, Task 12, Task 13
Task 11 ──→ Task 12
Task 12 ──→ Task 13
Task 13 ──→ Task 14a, 14b, 14c
Task 14a, 14b, 14c ──→ Task 14d, Task 15
```

### Tasks

**Phase 1: VFS 基础设施**

- [x] Task 1: 定义 vfs.ToolDef 和 vfs.ToolDescriptor 接口
  - File: `vfs/vfs.go`
  - Action: 在 StreamObserver 接口后新增：
    - `ToolDef` 结构体：`Name string json:"name"`, `Description string json:"description,omitempty"`, `Parameters map[string]any json:"parameters,omitempty"`（字段和 JSON tag 与 `llm.ToolDef` 完全一致）
    - `ToolDescriptor` 可选接口：`ToolDefs() []ToolDef`
    - `ToolCapable` 可选接口：`SupportsToolCalling() bool`
  - Notes: 参照 StreamObserver 放置位置和注释风格。新增 `TestToolDef_JSONRoundTrip`（F39 修复）：验证 `vfs.ToolDef` 序列化为 JSON 后可被 `llm.ToolDef` 正确反序列化，反之亦然

- [x] Task 2: 扩展 DeviceRegistry 存储 driver 引用 + VFS 访问器
  - File: `vfs/dev.go`
  - Action: 新增 `driverMap *xsync.SyncMap[string, any]`（平行于现有 `registry`）。新增 `RegisterWithDriver(path string, factory VFSFileFactory, driver any) error`（先调 `d.registry.Register`，再 `d.driverMap.Store`）。新增 `GetDriver(path string) (any, bool)`。新增 `RangeDrivers(fn func(path string, driver any) bool)` 方法（遍历 driverMap，供 buildToolDefs 在空 allowedDevices 时使用，F38 修复）。修改 `Unregister(path)` 同时调 `d.driverMap.Delete(path)`。保留原 `Register(path, factory)` 不变
  - File: `vfs/vfs.go`
  - Action: 新增 `(v *VFS) DeviceRegistry() *DeviceRegistry` 访问器方法，返回内部 `v.devReg`（F30 修复，供 kernel 在 Spawn 中调用 buildToolDefs）
  - File: `vfs/dev_test.go`
  - Action: 新增 `TestDeviceRegistry_RegisterWithDriver`、`TestDeviceRegistry_GetDriver`、`TestDeviceRegistry_RangeDrivers`、`TestDeviceRegistry_UnregisterCleansDriverMap`

- [x] Task 3: Context Message 添加 ToolCalls 支持
  - File: `context/context.go`
  - Action: 新增 `ToolCall struct { ID string json:"id"; Name string json:"name"; Input map[string]any json:"input,omitempty" }`（字段和 JSON tag 与 `llm.ToolCall` 完全一致）。Message 添加 `ToolCalls []ToolCall json:"tool_calls,omitempty"`。新增 `AppendAssistantWithToolCalls(cid types.CtxID, content string, toolCalls []ToolCall) error` 方法
  - File: `context/context_test.go`
  - Action: 新增 `TestAppendAssistantWithToolCalls`、`TestMessage_JSONCompat_ToolCalls`（验证 context.Message 序列化后可被 llm.Message 正确反序列化，包括 ToolCalls 字段）

- [x] Task 4: LLMRequest 添加 Tools 字段（依赖 Task 1）
  - File: `drivers/llm/driver.go`
  - Action: `LLMRequest` 结构体添加 `Tools []ToolDef json:"tools,omitempty"`（使用已有的 `llm.ToolDef` 类型）
  - Notes: 纯新增，omitempty 保证向后兼容

- [x] Task 5: LLMFile VFS bridge 路由到 CallWithTools（依赖 Task 4）
  - File: `drivers/llm/vfsfile.go`
  - Action:
    - (a) `writeCall`：检查 `len(req.Tools) > 0` → 类型断言 `tcd, ok := f.driver.(ToolCallingDriver)` → 调用 `tcd.CallWithTools(ctx, req, req.Tools)`，否则回退 `f.driver.Call(ctx, req)`。注意：writeCall 返回的 `*LLMResponse` 已包含 ToolCalls（driver 层正确填充），`bufferResponse` 正确序列化
    - (b) `writeStream`：同理检查 Tools → `tcd.StreamWithTools(ctx, req, req.Tools)`
    - (c) `writeStream` 的 `done` 分支：添加 `toolCalls = evt.ToolCalls` 收集，然后在构造 `resp` 时设置 `ToolCalls: toolCalls`（注意：ToolCalls 在 done 事件中发送，不在 tool_call 中间事件中）
    - (d) 新增 `SupportsToolCalling() bool` 方法：`_, ok := f.driver.(ToolCallingDriver); return ok`（实现 `vfs.ToolCapable`）
  - File: `drivers/llm/vfsfile_test.go`
  - Action: 新增 `TestLLMFile_WriteCall_WithTools`（mock ToolCallingDriver 验证 CallWithTools 被调用）、`TestLLMFile_WriteCall_NonToolDriver_Fallback`（普通 driver 回退到 Call）、`TestLLMFile_WriteStream_ToolCallsCollected`（验证 stream done 事件中 ToolCalls 传递到 response JSON）、`TestLLMFile_SupportsToolCalling`

**Phase 2: 设备自描述**

- [x] Task 6: ShellDriver 实现 ToolDescriptor（依赖 Task 1）
  - File: `drivers/shell/shell.go`
  - Action: 添加 `var _ vfs.ToolDescriptor = (*ShellDriver)(nil)` 编译检查。实现 `ToolDefs() []vfs.ToolDef` 返回单个工具 `{Name:"shell", Description:"Execute a shell command and return stdout+stderr. Commands run via sh -c in the process working directory. Non-zero exit codes are reported in output, not as errors.", Parameters: {type:object, properties:{command:{type:string, description:"Shell command to execute"}}, required:["command"]}}`
  - File: `drivers/shell/shell_test.go`
  - Action: 新增 `TestShellDriver_ToolDefs` 验证返回的 ToolDef 结构

- [x] Task 7: 新建 HostFSDriver 并实现 ToolDescriptor（依赖 Task 1）
  - File: `drivers/fs/hostfs.go`
  - Action:
    - (a) 新建 `HostFSDriver struct{}`
    - (b) `NewDriver() *HostFSDriver` 返回新实例
    - (c) `(d *HostFSDriver) FileFactory() vfs.VFSFileFactory` 方法，内部逻辑与当前包级 `FileFactory()` 相同
    - (d) **保留包级 `FileFactory()` 作为兼容 wrapper**：`func FileFactory() vfs.VFSFileFactory { return NewDriver().FileFactory() }`（F20 修复）
    - (e) 实现 `ToolDefs() []vfs.ToolDef` 返回 3 个工具：`read_file`（path required）、`write_file`（path+content required）、`list_dir`（path required）
    - (f) 编译检查 `var _ vfs.ToolDescriptor = (*HostFSDriver)(nil)`
  - File: `drivers/fs/hostfs_test.go`
  - Action: 新增 `TestHostFSDriver_ToolDefs` 验证 3 个工具定义、`TestFileFactory_Compat`（验证包级 FileFactory 仍可用）

- [x] Task 8: 更新 daemon 启动注册（依赖 Task 2, 6, 7）
  - File: `cmd/rnix/main.go`
  - Action: FS 从 `devReg.Register("/dev/fs", fs.FileFactory())` 改为 `fsDriver := fs.NewDriver(); devReg.RegisterWithDriver("/dev/fs", fsDriver.FileFactory(), fsDriver)`。Shell 从 `devReg.Register("/dev/shell", ...)` 改为 `devReg.RegisterWithDriver("/dev/shell", drivershell.FileFactory(shellDriver, "/dev/shell"), shellDriver)`
  - Notes: `/dev/llm/*` 注册不变（LLMFile 通过 ToolCapable 接口暴露能力）

- [x] Task 9: 实现 ToolDef 收集器和 toolMap（依赖 Task 1, 2）
  - File: `kernel/toolgen.go`（新建）
  - Action:
    - (a) 定义 `toolMapping struct { Type string; VFSPath string; Action ActionType; FSOperation string }`。FSOperation 用于区分 read_file/write_file/list_dir
    - (b) `buildToolDefs(devReg *vfs.DeviceRegistry, allowedDevices []string, planningEnabled bool) ([]vfs.ToolDef, map[string]toolMapping)`：
      - 若 allowedDevices 非空：遍历 allowedDevices，跳过 `/dev/llm/` 前缀（LLM 设备不是工具），对每个路径调 `devReg.GetDriver(path)` → 类型断言 ToolDescriptor → 收集 ToolDefs。GetDriver 返回空时静默跳过（处理 MCP 路径等未在 DeviceRegistry 直接注册的设备）
      - 若 allowedDevices 为空/nil：调用 `devReg.RangeDrivers()` 遍历所有已注册 driver（非 registry 的 VFSFileFactory），跳过 `/dev/llm/` 前缀
      - 构建 toolMap：工具名→{Type:"vfs", VFSPath, FSOperation}
    - (c) `metaToolDefs(planningEnabled bool) ([]vfs.ToolDef, map[string]toolMapping)`：返回 complete/spawn/replan/specialize 工具定义（plan 仅在 planningEnabled 时包含）。每个工具的完整 JSON Schema：
      - `complete`：`{result: string}` required:[result]
      - `spawn`：`{intent: string, agent?: string, model?: string}` required:[intent]
      - `replan`：`{reason: string}` required:[reason]
      - `specialize`：`{skill_name: string}` required:[skill_name]
      - `plan`：`{steps: array of string, reason: string}` required:[steps, reason]
    - (d) `generateToolProtocol(toolDefs []vfs.ToolDef, toolMap map[string]toolMapping, metaDefs []vfs.ToolDef, metaMap map[string]toolMapping, planningEnabled bool) string`：从 ToolDefs 自动生成文本协议。对每个 VFS 工具生成 `tool="<VFSPath>", data={...}` 格式。对 meta 工具生成 `{"action":"<name>", ...}` 格式。planProtocol 部分仅在 planningEnabled 时生成
    - (e) `mcpToolProtocolSnippet(mcpDevices []string) string`：为 MCP 设备生成文本描述片段（mixed mode，F15 修复），格式：`MCP tool: tool="/dev/mcp/<server>/<tool>", data={...}`
  - File: `kernel/toolgen_test.go`（新建）
  - Action: `TestBuildToolDefs_ShellAndFS`、`TestBuildToolDefs_AllowedDevicesFilter`、`TestBuildToolDefs_NilAllowedDevices_SkipsLLMDevices`、`TestBuildToolDefs_UnknownPath_SkipsSilently`、`TestMetaToolDefs`、`TestMetaToolDefs_PlanningDisabled`、`TestMetaToolDefs_JSONSchemaComplete`（验证每个 meta 工具的 parameters 是有效 JSON Schema）、`TestGenerateToolProtocol`、`TestGenerateToolProtocol_PlanningConditional`

**Phase 3: 内核集成**

- [x] Task 10: 扩展内核数据结构
  - File: `kernel/kernel.go`
  - Action:
    - (a) `llmRequest` 添加 `Tools []vfs.ToolDef json:"tools,omitempty"`（注意：序列化为 JSON 后，LLMFile 反序列化为 `llm.LLMRequest.Tools []llm.ToolDef`，两者 JSON 兼容因为 vfs.ToolDef 和 llm.ToolDef 字段完全一致）
    - (b) `llmResponse` 添加 `ToolCalls []llmToolCall json:"tool_calls,omitempty"`，其中 `llmToolCall struct { ID string json:"id"; Name string json:"name"; Input map[string]any json:"input,omitempty" }`（与 llm.ToolCall JSON 完全一致）
    - (c) **保留 `toolProtocol` 和 `planProtocol` 常量不删除**（F13 修复）。不使用 kernel 级缓存。文本协议路径中，每个进程在 reasonStep 中按需调用 `generateToolProtocol()` 生成（参数来自该进程的 AllowedDevices 对应的 ToolDefs）。生成结果可存储在 `Process.generatedProtocol string`（immutable after first reasonStep）以避免重复生成。老常量保留作为 fallback 参考和测试基线
  - File: `kernel/process.go`
  - Action: Process 结构体添加：
    - `UseNativeTools bool // immutable after Spawn; true when LLM driver implements ToolCallingDriver`
    - `toolMap map[string]toolMapping // immutable after Spawn; tool name → VFS path mapping`
    - `nativeToolDefs []vfs.ToolDef // immutable after Spawn; collected ToolDefs for req.Tools`
    - `generatedProtocol string // immutable after first reasonStep; auto-generated toolProtocol text for CLI drivers (F44 修复: per-process, 不用 kernel 级缓存)`
    - `mcpDevicePaths []string // immutable after Spawn; MCP device paths for mixed mode text injection (F34 修复)`

- [x] Task 11: Spawn 阶段检测和 ToolDef 收集（依赖 Task 2, 9, 10）
  - File: `kernel/kernel.go`（Spawn 函数内，紧跟 StreamObserver 设置 kernel.go:637-644 之后）
  - Action:
    - (a) 通过 `k.vfs.GetFile(proc.PID, llmFD)` → 类型断言 `vfs.ToolCapable` → `tc.SupportsToolCalling()` → 设置 `proc.UseNativeTools`（注意：VFS.GetFile 已存在且已在使用，F5 修复）
    - (b) 如果 UseNativeTools：调用 `buildToolDefs(k.vfs.DeviceRegistry(), proc.AllowedDevices, proc.PlanningEnabled)` + `metaToolDefs(proc.PlanningEnabled)` → 合并到 `proc.nativeToolDefs` 和 `proc.toolMap`
    - (c) 从 AllowedDevices 中提取 MCP 路径（`/dev/mcp/` 或 `/mnt/mcp/` 前缀），存储到 `proc.mcpDevicePaths`（用于 mixed mode 文本注入）

- [x] Task 12: reasonStep 系统提示分支（依赖 Task 5, 10, 11）
  - File: `kernel/kernel.go`（reasonStep 中 kernel.go:1093-1096 替换）
  - Action:
    - `if proc.UseNativeTools`：不注入 toolProtocol/planProtocol 文本。设置 `req.Tools = proc.nativeToolDefs`。若进程有 MCP 设备路径（mixed mode），注入 `mcpToolProtocolSnippet(proc.mcpDevicePaths)` 到系统提示（F15 修复）
    - `else`（CLI driver 路径）：注入 `generateToolProtocol(...)` 生成的文本（替代硬编码常量）。planProtocol 部分仅在 `proc.PlanningEnabled` 时注入（F13 修复：保留条件注入语义）

- [x] Task 13: reasonStep 响应处理分支（依赖 Task 12）
  - File: `kernel/kernel.go`（reasonStep 中 parseAction 调用之前）
  - Action: 在 `json.Unmarshal(respData, &resp)` 之后：
    - `if proc.UseNativeTools && len(resp.ToolCalls) > 0` → 调用 `executeNativeToolCalls()`
    - `elif proc.UseNativeTools && len(resp.ToolCalls) == 0 && resp.Content != ""` → 先尝试 `parseAction(&resp)`（F14 修复：处理 LLM 错误回退到文本协议的情况），若 parseAction 返回 ActionText 则视为最终文本回答
    - `else` → 走现有 `parseAction()` 路径

- [x] Task 14a: 实现 executeNativeToolCalls 主函数（依赖 Task 3, 13）
  - File: `kernel/kernel.go`
  - Action: 新函数 `executeNativeToolCalls(proc *Process, resp llmResponse, step int) (shouldContinue bool)`：
    - (a) `ctxMgr.AppendAssistantWithToolCalls(cid, resp.Content, convertToolCalls(resp.ToolCalls))` 存储 assistant 消息。`convertToolCalls` 是 kernel 包内的辅助函数（放在 `kernel/toolgen.go` 中），签名：`func convertToolCalls(calls []llmToolCall) []rnixctx.ToolCall`，逐字段复制 ID/Name/Input（两个类型字段完全一致）
    - (b) 遍历每个 ToolCall：查 `proc.toolMap[tc.Name]`，根据 `mapping.Type` 分发到 `executeNativeVFSTool` 或 `executeNativeMetaAction`
    - (c) 未知工具名：AppendToolResult(cid, tc.ID, "error: unknown tool "+tc.Name)
    - (d) Circuit breaker：复用连续错误计数
    - (e) emitEvent 记录每个 native tool call
  - Notes: 拆分为 3 个子函数（F27 修复）

- [x] Task 14b: 实现 executeNativeVFSTool（依赖 Task 14a）
  - File: `kernel/kernel.go`
  - Action: 新函数 `executeNativeVFSTool(proc, tc llmToolCall, mapping toolMapping) (string, error)`：
    - 权限检查：从 toolMap 获取 `mapping.VFSPath` → 用 VFS 路径做 AllowedDevices 前缀匹配（F17 修复）
    - 根据 `mapping.FSOperation` 处理 /dev/fs 特殊逻辑（F8 修复）：
      - `read_file`：`vfsPath = "/dev/fs/" + tc.Input["path"]`，Open O_RDONLY → **直接 Read（不 Write）** → Close
      - `write_file`：`vfsPath = "/dev/fs/" + tc.Input["path"]`，Open O_WRONLY → Write `{"content": contentStr}` 其中 `contentStr, _ := tc.Input["content"].(string)`（类型断言 any→string，F45 修复） → Read → Close
      - `list_dir`：`vfsPath = "/dev/fs/" + tc.Input["path"]`，Open O_WRONLY → Write `{"op":"list"}` → Read → Close
    - 其他 VFS 工具（shell 等）：`vfsPath = mapping.VFSPath`，Open O_RDWR → Write `json.Marshal(tc.Input)` → Read → Close
    - AppendToolResult(cid, tc.ID, result)（使用 tool_call_id 而非 VFS 路径，F18 说明）

- [x] Task 14c: 实现 executeNativeMetaAction（依赖 Task 14a）
  - File: `kernel/kernel.go`
  - Action: 新函数 `executeNativeMetaAction(proc, tc llmToolCall, mapping toolMapping) (shouldContinue bool)`：
    - `ActionComplete`：从 `tc.Input["result"]` 设置 proc.Result，返回 false
    - `ActionSpawn`：从 `tc.Input["intent"]`/`tc.Input["agent"]`/`tc.Input["model"]` 构造 SpawnOpts，执行子进程等待结果，AppendToolResult
    - `ActionReplan`：从 `tc.Input["reason"]` 追加 replan 消息到 context
    - `ActionSpecialize`：从 `tc.Input["skill_name"]` 加载 skill，追加结果
    - `ActionPlan`：从 `tc.Input["steps"]`/`tc.Input["reason"]` 追加 plan 到 context

- [ ] Task 14d: Fallback 降级处理（依赖 Task 14a，F7 修复）
  - File: `kernel/kernel.go`（reasonStep 中 attemptFallback 调用之后，F40 修复：降级检测在 reasonStep 中 attemptFallback 返回成功之后执行，不在 attemptFallback 内部）
  - Action: 在 `attemptFallback` 成功切换到 fallback device 后，检测 fallback LLMFile 是否实现 ToolCapable：
    - 若 fallback 支持 ToolCallingDriver：继续 native tools 模式（UseNativeTools 不变）
    - 若 fallback 不支持：设置 `proc.UseNativeTools = false`。在 reasonStep 下一轮中，系统提示注入自动生成的 toolProtocol 文本，req.Tools 清空。**注意**：context 中已有的 tool_calls 格式的 assistant/tool 消息对 CLI driver 无法理解，但这是 fallback 的固有代价——OpenAI driver 的对话历史不兼容 text protocol driver。追加一条 user 消息说明工具调用协议已切换

- [ ] Task 15: 内核 e2e 测试（依赖所有前置 Task）
  - File: `kernel/kernel_test.go`
  - Action:
    - (a) `TestReasonStep_NativeToolCall_Shell` — mock ToolCallingDriver 返回 ToolCall{name:"shell",input:{command:"echo hi"}}，验证 shell 执行和结果追加到 context（tool_call_id 匹配）
    - (b) `TestReasonStep_NativeToolCall_ReadFile` — ToolCall{name:"read_file",input:{path:"test.txt"}}，验证 /dev/fs/test.txt O_RDONLY 直接 Read
    - (c) `TestReasonStep_NativeToolCall_Complete` — ToolCall{name:"complete",input:{result:"done"}}，验证进程完成
    - (d) `TestReasonStep_TextProtocol_Unchanged` — 使用非 ToolCallingDriver（ClaudeCliDriver mock），验证走 parseAction 旧路径且系统提示包含自动生成的 toolProtocol
    - (e) `TestReasonStep_NativeToolCall_PermissionDenied` — ToolCall 引用未授权设备，验证 error result
    - (f) `TestReasonStep_NativeToolCall_Fallback_Degradation` — primary 用 native tools，fallback 降级到 text protocol
    - (g) `TestReasonStep_NativeToolCall_Stream_ToolCalls` — 验证流式模式下 ToolCalls 正确收集

### Acceptance Criteria

**Phase 1: VFS 基础设施**

- [ ] AC1: Given vfs.ToolDescriptor 接口定义, when ShellDriver 实现该接口, then `var _ vfs.ToolDescriptor = (*ShellDriver)(nil)` 编译通过
- [ ] AC2: Given DeviceRegistry.RegisterWithDriver 被调用, when GetDriver(path) 被查询, then 返回注册时传入的 driver 对象
- [ ] AC2b: Given DeviceRegistry.Unregister 被调用, when GetDriver(path) 被查询, then 返回 false（driverMap 已清理）
- [ ] AC3: Given context.Message 有 ToolCalls 字段, when AppendAssistantWithToolCalls 被调用, then Message 正确存储 ToolCalls 列表
- [ ] AC3b: Given context.Message 序列化为 JSON, when 反序列化为 llm.Message, then ToolCalls 字段正确映射（ID/Name/Input 类型一致）
- [ ] AC4: Given LLMRequest 含 Tools 字段, when LLMFile.writeCall 执行, then 调用 ToolCallingDriver.CallWithTools 而非普通 Call
- [ ] AC5: Given LLM driver 不实现 ToolCallingDriver, when LLMRequest 含 Tools, then 回退到普通 Call（忽略 Tools）
- [ ] AC6: Given LLMFile.writeStream 收到 done 事件含 ToolCalls, when response 被序列化, then ToolCalls 出现在 JSON 中
- [ ] AC6b: Given LLMFile.writeCall 使用 CallWithTools, when driver 返回 ToolCalls, then bufferResponse 正确序列化 ToolCalls

**Phase 2: 设备自描述**

- [ ] AC7: Given ShellDriver.ToolDefs() 被调用, then 返回 `[{name:"shell", parameters:{type:"object", properties:{command:{type:"string"}}, required:["command"]}}]`
- [ ] AC8: Given HostFSDriver.ToolDefs() 被调用, then 返回 3 个工具 `[read_file, write_file, list_dir]`，每个有正确的 JSON Schema parameters
- [ ] AC8b: Given 包级 fs.FileFactory() 被调用, then 仍然返回有效的 VFSFileFactory（兼容 wrapper 工作）
- [ ] AC9: Given AllowedDevices=["/dev/shell"], when buildToolDefs 执行, then 只返回 shell 相关 ToolDef，不返回 fs 工具
- [ ] AC10: Given AllowedDevices 为空/nil, when buildToolDefs 执行, then 返回所有注册设备（/dev/shell、/dev/fs）的 ToolDefs，不返回 /dev/llm/* 设备
- [ ] AC10b: Given AllowedDevices 包含未知路径, when buildToolDefs 执行, then 静默跳过（不报错）
- [ ] AC11: Given ToolDefs 已收集, when generateToolProtocol 被调用 planningEnabled=true, then 生成的文本包含 plan 动作
- [ ] AC11b: Given planningEnabled=false, when generateToolProtocol 被调用, then 生成的文本不包含 plan 动作
- [ ] AC11c: Given metaToolDefs 被调用, then 每个 meta 工具的 Parameters 是有效的 JSON Schema（有 type、properties、required）

**Phase 3: 内核集成**

- [ ] AC12: Given LLM driver 实现 ToolCallingDriver, when 进程 spawn, then proc.UseNativeTools=true 且 proc.toolMap 非空且 proc.nativeToolDefs 非空
- [ ] AC13: Given proc.UseNativeTools=true, when reasonStep 构建请求, then req.Tools 包含收集的 ToolDefs 且系统提示不含 toolProtocol 文本
- [ ] AC13b: Given proc.UseNativeTools=true 且进程有 MCP 设备, when reasonStep 构建请求, then 系统提示包含 MCP 工具的文本描述片段（mixed mode）
- [ ] AC14: Given proc.UseNativeTools=false（CLI driver）, when reasonStep 构建请求, then 系统提示包含自动生成的 toolProtocol 文本（非硬编码常量），req.Tools 为空
- [ ] AC15: Given LLM 返回 ToolCalls=[{name:"shell",input:{command:"echo hi"}}], when executeNativeToolCalls 执行, then /dev/shell 收到 `{"command":"echo hi"}` 并执行，结果追加到 context（tool_call_id 匹配）
- [ ] AC16: Given LLM 返回 ToolCalls=[{name:"read_file",input:{path:"src/main.go"}}], when executeNativeToolCalls 执行, then 打开 /dev/fs/src/main.go (O_RDONLY)，**直接 Read 不 Write**，内容追加到 context
- [ ] AC17: Given LLM 返回 ToolCalls=[{name:"complete",input:{result:"done"}}], when executeNativeToolCalls 执行, then 进程以 result="done" 完成
- [ ] AC18: Given LLM 返回空 ToolCalls 和非空 Content（无 JSON action）, when proc.UseNativeTools=true, then 视为最终文本回答（ActionText）
- [ ] AC18b: Given LLM 返回空 ToolCalls 但 Content 包含 JSON action, when proc.UseNativeTools=true, then fallback 到 parseAction 处理
- [ ] AC19: Given primary driver 用 native tools 但 fallback driver 不支持, when fallback 触发, then proc.UseNativeTools 切换为 false，下一轮注入 toolProtocol 文本
- [ ] AC20: Given 所有改动完成, when `make all` 执行, then lint + vet + 全部测试通过 + 构建成功

## Additional Context

### Dependencies

- 技术调研报告：`_bmad-output/planning-artifacts/research/technical-vfs-device-tooldef-architecture-research-2026-03-20.md`
- shell 驱动 extractCommand 修复已合入（`drivers/shell/shell.go`）
- 无外部依赖新增
- Task 依赖关系见上方 "Task Dependencies" 图

### Testing Strategy

**单元测试：**
- `vfs/dev_test.go` — RegisterWithDriver / GetDriver / Unregister 清理
- `context/context_test.go` — AppendAssistantWithToolCalls / ToolCalls JSON 兼容性验证
- `drivers/llm/vfsfile_test.go` — CallWithTools 路由 / SupportsToolCalling / stream ToolCalls 收集 / writeCall ToolCalls 传递
- `drivers/shell/shell_test.go` — ToolDefs 返回值验证
- `drivers/fs/hostfs_test.go` — ToolDefs 返回值验证（3 工具）/ FileFactory 兼容 wrapper
- `kernel/toolgen_test.go` — buildToolDefs / metaToolDefs（含 JSON Schema 验证）/ generateToolProtocol / AllowedDevices 过滤 / LLM 设备跳过 / 未知路径跳过

**集成测试：**
- `kernel/kernel_test.go` — native ToolCall 完整 e2e（shell / read_file / complete）
- `kernel/kernel_test.go` — 文本协议路径不受影响（自动生成 toolProtocol）
- `kernel/kernel_test.go` — 权限拒绝
- `kernel/kernel_test.go` — Fallback 降级
- `kernel/kernel_test.go` — 流式模式 ToolCalls

**回归验证：**
- `make all` 全部包通过

### Notes

- **高风险项**：reasonStep 是核心循环，Phase 3 的 Task 12-14 修改最敏感。建议小步提交，每步 `make test` 验证
- **CLI 驱动零影响**：Claude CLI / Cursor CLI 不实现 ToolCallingDriver，完全走现有文本协议路径（使用自动生成的 toolProtocol 替代硬编码常量）
- **MCP Mixed Mode**：native tools 模式下，MCP 工具通过文本描述补充注入系统提示，LLM 用文本协议调用 MCP 工具。native tools 路径只处理静态设备（shell/fs）+ meta 动作
- **Fallback 降级代价**：native→text 切换时，context 中已有的 tool_calls 格式消息对 CLI driver 不可理解，这是 fallback 的固有限制
- **JSON 序列化链验证**：vfs.ToolDef ↔ llm.ToolDef、context.ToolCall ↔ llm.ToolCall 字段名和 JSON tag 完全一致，确保序列化/反序列化兼容
- **未来扩展**：新增设备只需实现 ToolDescriptor + 注册到 DeviceRegistry，无需修改内核代码

### Adversarial Review Findings Disposition

| ID | Severity | Fix |
|----|----------|-----|
| F3 | Critical | Task 5(c) 明确 ToolCalls 在 done 事件收集，非 tool_call 中间事件 |
| F7 | Critical | 新增 Task 14d Fallback 降级处理 |
| F2 | High | Task 10(b) llmToolCall.Input 明确为 `map[string]any`；Reasoning 字段不在本 spec 范围 |
| F4 | High | Task 2 使用平行 SyncMap，Unregister 同时清理两者 |
| F5 | High | Task 11 Note 修正：VFS.GetFile 已存在，引用 kernel.go:638 |
| F8 | High | Task 14b read_file 使用 O_RDONLY 直接 Read 不 Write |
| F10 | High | Task 9(b) 明确跳过 /dev/llm/ 前缀，未知路径静默跳过 |
| F13 | High | Task 10(c) 保留常量不删除，generateToolProtocol 接受 planningEnabled 参数 |
| F17 | High | Task 14b 从 toolMap.VFSPath 做 AllowedDevices 前缀匹配 |
| F23 | High | Task Dependencies 图明确声明；Task 5 新增 AC6b 验证 writeCall 路径 |
| F27 | High | Task 14 拆分为 14a(主函数)/14b(VFS)/14c(Meta)/14d(Fallback) |
| F9 | Medium | context.ToolCall.Input 明确为 `map[string]any` |
| F11 | Medium | Task 9(c) 列出每个 meta 工具的完整 JSON Schema |
| F14 | Medium | Task 13 native mode 空 ToolCalls 时 fallback 到 parseAction |
| F15 | Medium | MCP Mixed Mode：Task 9(e) + Task 12 文本注入 |
| F20 | Medium | Task 7(d) 保留包级 FileFactory 兼容 wrapper |
| F22 | Medium | Task 15(g) 新增流式模式 ToolCalls 测试 |
| F26 | Medium | context.ToolCall 字段类型明确 |
| F28 | Medium | 确认 OpenAI driver 已处理 Message.ToolCalls（Context 参考表） |
| F1,F12,F21,F24,F25,F29 | Low | 行号修正 / immutable 注释 / 小偏差修正 |

**第二轮审查发现 (7 项, 已修复):**

| ID | Severity | Fix |
|----|----------|-----|
| F30 | High | Task 2 新增 `VFS.DeviceRegistry()` 访问器方法 |
| F38 | High | Task 2 新增 `RangeDrivers(fn)` 遍历方法，Task 9(b) 使用 RangeDrivers 而非 registry.Range |
| F33 | Medium | 同 F38 |
| F34 | Medium | Task 10 Process 结构体补充 `mcpDevicePaths []string` 字段 |
| F39 | Medium | Task 1 Notes 新增 `TestToolDef_JSONRoundTrip` 测试 |
| F37 | Low | AppendToolResult 语义差异已在 Key Technical Constraints #8 说明 |
| F40 | Low | Task 14d 明确降级检测位置：reasonStep 中 attemptFallback 返回之后 |

**第三轮审查发现 (3 项, 已修复):**

| ID | Severity | Fix |
|----|----------|-----|
| F43 | High | Task 14a 明确 `convertToolCalls` 函数位置（kernel/toolgen.go）和签名 |
| F44 | High | 移除 kernel 级缓存，改为 per-process `generatedProtocol` 字段（Process 结构体） |
| F45 | Medium | Task 14b write_file 明确 `tc.Input["content"].(string)` 类型断言 |
