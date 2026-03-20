---
stepsCompleted: [1, 2, 3, 4, 5]
inputDocuments: []
workflowType: 'research'
lastStep: 1
research_type: 'technical'
research_topic: 'vfs-device-tooldef-architecture'
research_goals: '评估 rnix VFS 设备注册机制的可扩展性，设计原生 ToolCalls 的工具发现与自描述架构'
user_name: 'Decker'
date: '2026-03-20'
web_research_enabled: true
source_verification: true
---

# Research Report: technical

**Date:** 2026-03-20
**Author:** Decker
**Research Type:** technical

---

## Research Overview

[Research overview and methodology will be appended here]

---

<!-- Content will be appended sequentially through research workflow steps -->

## Technical Research Scope Confirmation

**Research Topic:** vfs-device-tooldef-architecture
**Research Goals:** 评估 rnix VFS 设备注册机制的可扩展性，设计原生 ToolCalls 的工具发现与自描述架构

**Technical Research Scope:**

- 架构分析 — rnix VFS 设备注册机制、toolProtocol 硬编码、AllowedDevices 权限模型
- 实现模式分析 — Claude Code / OpenAI Function Calling / MCP tools/list 对比
- 微内核能力声明 — seL4 Capability / Plan9 9P 自描述 / Fuchsia FIDL 服务发现
- 集成模式 — 设备自注册协议、动态 vs 静态工具发现
- 可扩展性评估 — 新设备摩擦点、ToolDef 自动生成可行性、未来扩展路径

**Research Methodology:**

- rnix 源码深度分析（关键路径全量阅读）
- 业界方案 web 调研（当前公开资料，严格来源验证）
- 微内核文献对比（学术 + 工程实现）
- 多源交叉验证关键技术断言

**Scope Confirmed:** 2026-03-20

## 技术栈分析

### 一、Rnix 现有 VFS 设备注册架构

#### 1.1 设备注册机制

设备注册分为三层：

**DeviceRegistry（`vfs/dev.go:15-30`）**：核心注册表，存储 `path → VFSFileFactory` 映射。
- `Register(path, factory)` — 将一个路径前缀绑定到一个 FileFactory
- `Open(path, flags, workDir)` — 精确匹配 → 最长前缀匹配 → 找到 factory 后调用 `factory(subpath, flags, workDir)`
- 注册表**只存储 factory 函数**，不存储任何设备元数据（名称、描述、参数 schema）

**VFSFileFactory（`vfs/vfs.go:47-50`）**：
```go
type VFSFileFactory func(subpath string, flags OpenFlag, workDir string) (VFSFile, error)
```
这是一个纯函数类型 — 没有 Name()、Description()、Schema() 等自描述方法。

**VFSFile 接口（`vfs/vfs.go:31-37`）**：
```go
type VFSFile interface {
    Read(length int) ([]byte, error)
    Write(ctx context.Context, data []byte) error
    Close() error
    Stat() (FileStat, error)
}
```
Stat() 返回 `FileStat{Name, Size, IsDevice, DevicePath}` — 只有基本元数据，无能力声明。

#### 1.2 设备注册时机与位置

**静态设备（daemon 启动时）** — `cmd/rnix/main.go:1168-1176`：
```go
devReg := vfs.NewDeviceRegistry()
llm.RegisterProviders(providersCfg, driverReg, devReg)  // /dev/llm/*
_ = devReg.Register("/dev/fs", fs.FileFactory())         // /dev/fs
_ = devReg.Register("/dev/shell", drivershell.FileFactory(shellDriver, "/dev/shell"))  // /dev/shell
```

**动态设备（进程 spawn 时）** — MCP 服务器通过 `MountManager.Mount()` 动态注册：
- `kernel/kernel.go:664` — 在 Spawn 中调用 `k.mountMgr.Mount(mountPath, mcpCfg)`
- `vfs/mount.go:36-77` — Mount 创建 transport，注册到 DeviceRegistry

**项目级 LLM 提供者** — `ipc/server.go:1609-1617`：
```go
factories[pc.Name] = llm.FileFactory(driver, "/dev/llm/"+pc.Name, pc.Mode)
```

#### 1.3 toolProtocol 硬编码问题

`kernel/kernel.go:58-98` 的 `toolProtocol` 常量**是 LLM 学习可用工具的唯一来源**。它硬编码了：
- `/dev/fs` 的三种操作（read/write/list）及其 data 格式
- `/dev/shell` 的 `{"command": "..."}` 格式
- `/dev/llm/<provider>` 和 `/dev/mcp/<server>/<tool>` 的存在

**没有动态工具发现**。添加新设备后，必须手动修改 `toolProtocol` 字符串，否则 LLM 不知道新设备存在。

#### 1.4 AllowedDevices 权限模型

流程：`agent.yaml` → `agent.AllowedTools()` → `proc.AllowedDevices`（字符串切片）→ `kernel/kernel.go:1339-1349` 前缀匹配检查。

AllowedDevices 只是 VFS 路径前缀列表（如 `["/dev/shell", "/dev/fs"]`），不携带任何工具描述信息。

#### 1.5 添加新设备的摩擦点

假设添加 `/dev/db`（数据库访问），需要修改的文件：

| 文件 | 修改内容 |
|------|---------|
| `drivers/db/db.go` (新建) | 驱动实现 VFSFile 接口 |
| `drivers/db/db_test.go` (新建) | 驱动测试 |
| `cmd/rnix/main.go` | 添加 `devReg.Register("/dev/db", ...)` |
| `kernel/kernel.go` | 修改 `toolProtocol` 常量，添加 `/dev/db` 描述 |
| `agents/*/agent.yaml` | 更新 allowed_tools 列表 |
| `skills/*/SKILL.md` | 更新 allowed_tools |

**共 6 处修改**，其中 `toolProtocol` 硬编码是最大的耦合点 — 驱动开发者需要修改内核代码。

#### 1.6 架构缺口总结

```
当前：设备 → Register(path, factory) → DeviceRegistry
                                           ↓
                        Open(path) → factory(subpath) → VFSFile
                                                          ↓
                                              Read/Write/Close/Stat

缺失：设备无法声明自己的 {name, description, parameters schema}
      toolProtocol 硬编码 = 设备与内核的强耦合
      无法动态生成 ToolDef
```

### 二、业界方案对比

#### 2.1 OpenAI Function Calling

OpenAI 使用 JSON Schema 定义工具，随每次 API 请求传递：

```json
{
  "name": "get_weather",
  "description": "Get current weather for a location",
  "input_schema": {
    "type": "object",
    "properties": {
      "location": {"type": "string", "description": "City and state"},
      "unit": {"type": "string", "enum": ["celsius", "fahrenheit"]}
    },
    "required": ["location"]
  }
}
```

**关键设计**：
- `strict: true` 模式保证 LLM 输出严格遵循 schema
- 工具定义在请求级别传递，不嵌入系统提示
- 支持并行工具调用（`disable_parallel_tool_use` 可控制）
- 所有字段必须标记为 `required`（可选字段通过 `type: ["string", "null"]` 实现）

_Source: [OpenAI Function Calling Guide](https://platform.openai.com/docs/guides/function-calling)_

#### 2.2 MCP (Model Context Protocol) 动态工具发现

MCP 定义了完整的工具发现协议：

- **`tools/list`** — 客户端查询服务器可用工具列表
- **`tools/call`** — 客户端调用特定工具
- **`notifications/tools/list_changed`** — 服务器主动通知工具列表变更
- **Tool Annotations** — 描述工具行为特征（`readOnlyHint`、`destructiveHint`、`idempotentHint`、`openWorldHint`）

```json
// tools/list 响应示例
{
  "tools": [{
    "name": "read_file",
    "description": "Read file contents",
    "inputSchema": {
      "type": "object",
      "properties": {
        "path": {"type": "string"}
      },
      "required": ["path"]
    },
    "annotations": {
      "readOnlyHint": true
    }
  }]
}
```

**关键设计**：
- 工具自注册 — 服务器声明自己的工具，客户端动态发现
- 运行时变更 — 工具可在运行时添加/删除
- Schema 标准化 — 使用 JSON Schema 描述参数
- MCP Registry — 2025年9月推出的公共注册表，支持服务器发现

_Source: [MCP Specification](https://modelcontextprotocol.io/specification/2025-11-25), [MCP Dynamic Tool Discovery](https://www.speakeasy.com/mcp/tool-design/dynamic-tool-discovery)_

#### 2.3 Claude Code 工具模型

Claude Code 对每个内置工具定义详尽的 `tool-description-*.md`，包含：
- 功能描述 + 使用规范 + 注意事项
- JSON Schema 参数定义（type、enum、required、description）
- 工具间引用（"use X instead of Y"）

与 rnix 的关键差异：工具定义是**结构化的**（JSON Schema），而非**文本示例**（toolProtocol）。

### 三、微内核能力声明

#### 3.1 seL4 Capability-Based Security

seL4 使用**能力（capability）**作为所有内核服务的访问控制机制。每个能力是一个带有权限位的内核对象引用。

**核心概念**：
- 能力不仅是权限令牌，还携带了**对象类型和允许的操作**
- `Untyped Memory` 能力用于显式创建新内核对象
- 能力可以被复制、传递、限制（mint）— 实现精细的权限委托

**与 rnix 的映射**：rnix 的 `AllowedDevices` 类似能力列表，但缺少能力携带的**操作声明**（seL4 的能力知道自己允许哪些方法调用）。

_Source: [seL4 Whitepaper 2025](https://sel4.systems/About/seL4-whitepaper.pdf), [seL4 Reference Manual](https://sel4.systems/Info/Docs/seL4-manual-latest.pdf)_

#### 3.2 Plan9 / 9P 设备自描述

Plan9 的核心理念：**设备通过文件系统接口自描述**。

**`ctl` 文件模式**：
- 每个设备目录包含 `ctl`（控制）、`data`（数据）、`status`（状态）等文件
- 读取 `ctl` 文件 → 获取设备当前状态和能力
- 写入 `ctl` 文件 → 控制设备行为
- 所有通信使用 ASCII 文本，避免字节序问题

**连接服务器（cs）**：应用不需要知道每个网络的寻址方式，而是向 cs 写入符号地址，读取具体连接信息。这是一种**服务发现**机制。

**与 rnix 的映射**：rnix 的 VFSFile 有 `Stat()` 方法，但只返回基本元数据。可以扩展为 Plan9 风格的能力文件 — 设备暴露 `schema` 子路径供内核查询。

_Source: [9P Notes](https://blog.gnoack.org/post/9pnotes), [Plan 9 Networking](https://9p.io/sys/doc/net/net.html)_

#### 3.3 Fuchsia 能力路由

Fuchsia 的组件框架使用**声明式能力路由**：

```json
// component.cml
{
  "capabilities": [
    {"protocol": "fuchsia.example.Echo"}
  ],
  "expose": [
    {"protocol": "fuchsia.example.Echo", "from": "self"}
  ],
  "use": [
    {"protocol": "fuchsia.example.Echo"}
  ]
}
```

**核心设计**：
- **`capabilities`** — 声明组件提供的能力
- **`expose`** — 将能力暴露给父组件
- **`use`** — 声明组件需要的能力
- 框架在运行时验证能力路由的完整性

**与 rnix 的映射**：类似于 agent.yaml 的 `allowed_tools`（use）和设备注册（capabilities），但 Fuchsia 的声明是**双向的** — 提供者和消费者都声明自己的能力。rnix 目前只有消费者侧声明（AllowedDevices），缺少提供者侧的能力声明。

_Source: [Fuchsia Capabilities](https://fuchsia.dev/fuchsia-src/concepts/components/v2/capabilities), [Fuchsia Component Introduction](https://fuchsia.dev/fuchsia-src/concepts/components/v2/introduction)_

### 四、技术趋势

#### 4.1 共同模式：工具自描述 + 动态发现

三个来源（LLM 工具调用、MCP、微内核）汇聚于同一设计模式：

| 维度 | OpenAI/Anthropic | MCP | seL4/Plan9/Fuchsia | Rnix 现有 |
|------|-----------------|-----|-------|-----------|
| 能力声明 | JSON Schema | inputSchema | Capability/ctl/cml | ❌ 无 |
| 声明位置 | API 请求参数 | tools/list 响应 | 内核对象/文件/manifest | toolProtocol 硬编码 |
| 动态发现 | 每次请求传递 | tools/list + 变更通知 | 能力传递/ctl 读取 | ❌ 无 |
| 权限控制 | 请求级 tools 列表 | 无（客户端决定） | 能力令牌 | AllowedDevices 前缀匹配 |

#### 4.2 Rnix 的差距与机会

**关键缺口**：设备（提供者）没有自描述能力。toolProtocol 是内核对设备的**第三方描述**，而非设备自己的声明。

**设计机会**：借鉴 Plan9 的 ctl 文件 + MCP 的 tools/list + Fuchsia 的能力声明，让 VFS 设备自描述其 ToolDef。

## 集成模式分析

### 一、设备自描述协议设计

基于技术栈分析的缺口，核心问题是：**如何让 VFS 设备声明自己的 ToolDef，而不依赖 toolProtocol 硬编码？**

三种候选模式：

#### 模式 A：接口扩展 — ToolDescriptor 接口

在 VFSFile 或 VFSFileFactory 层面添加可选接口：

```go
// vfs/vfs.go — 新增可选接口
type ToolDescriptor interface {
    ToolDefs() []ToolDef  // 设备声明自己支持的工具定义
}
```

**集成方式**：设备的 FileFactory 或驱动对象实现此接口。内核在 spawn 时通过类型断言查询：

```go
if td, ok := driver.(vfs.ToolDescriptor); ok {
    tools = append(tools, td.ToolDefs()...)
}
```

**优势**：Go 惯用模式（参照 `HealthChecker`、`StreamObserver` 可选接口）、零破坏性、编译时安全
**劣势**：需要每个驱动实现接口、不支持运行时动态变更

#### 模式 B：Plan9 风格 — 设备文件自描述

设备暴露 `schema` 子路径，读取返回 JSON Schema：

```
Open("/dev/shell/schema")  → Read() → [{"name":"shell","description":"...","input_schema":{...}}]
Open("/dev/fs/schema")     → Read() → [{"name":"read_file",...}, {"name":"write_file",...}]
```

**集成方式**：内核在 spawn 时 Open 每个设备的 `/schema` 子路径，读取 ToolDef 列表。

**优势**：完美贴合 "everything is a file" 哲学、MCP 设备天然支持（tools/list 已有类似机制）
**劣势**：为每个设备增加 schema 文件处理逻辑、序列化开销

#### 模式 C：注册表扩展 — DeviceRegistry 携带元数据

扩展 Register 签名，注册时携带 ToolDef：

```go
type DeviceRegistration struct {
    Factory  VFSFileFactory
    ToolDefs []ToolDef  // 工具定义
}

func (d *DeviceRegistry) Register(path string, reg DeviceRegistration) error
```

**集成方式**：注册表直接存储元数据，内核查询注册表获取 ToolDef。

**优势**：集中式、查询高效
**劣势**：改变 Register API（破坏性变更）、ToolDef 在注册时静态确定

#### 推荐：模式 A + 模式 B 混合

| 设备类型 | 自描述方式 | 理由 |
|---------|----------|------|
| 静态设备 (/dev/shell, /dev/fs) | **模式 A** — 驱动实现 ToolDescriptor | ToolDef 固定，编译时确定 |
| MCP 设备 (/dev/mcp/*) | **模式 B** — 读 tools/list | ToolDef 动态，由 MCP 服务器定义 |
| Meta 动作 (spawn/complete/...) | **内核内置** | 不是 VFS 设备，由内核定义 |

混合模式保留了两种发现机制的优势：静态设备高效直接，动态设备灵活自治。

### 二、各 LLM Provider 的工具协议对比

Anthropic API 的工具定义与 OpenAI 高度同构，但有关键差异：

| 特性 | OpenAI | Anthropic | 两者共通 |
|------|--------|-----------|---------|
| 工具定义字段 | `parameters` | `input_schema` | JSON Schema 格式 |
| 工具调用返回 | `tool_calls` 数组 | `tool_use` content block | id + name + input |
| 结果返回 | `tool` role 消息 | `tool_result` content block | tool_use_id + content |
| 并行调用 | 默认启用 | 默认启用 | 可禁用 |
| 高级特性 | strict mode | tool search (defer_loading) | — |

**rnix 的 ToolCallingDriver 接口已经统一了这两种格式**：
- `ToolDef{Name, Description, Parameters}` — 对应两者的 JSON Schema
- `ToolCall{ID, Name, Input}` — 对应两者的调用返回
- `ToolResult{ToolCallID, Content}` — 对应两者的结果

**Anthropic 的 Tool Search** 特别值得关注：允许标记 `defer_loading: true` 的工具在需要时才加载定义。这与 rnix 的 `specialize` 动作（动态加载 skill）设计思路一致。

_Source: [Anthropic Tool Use](https://platform.claude.com/docs/en/agents-and-tools/tool-use/implement-tool-use), [Advanced Tool Use](https://www.anthropic.com/engineering/advanced-tool-use)_

### 三、ToolDef 生成的集成流程

综合推荐的混合模式，完整的工具发现流程：

```
                  ┌──────────────────────────────────────────────┐
                  │              Spawn 阶段                       │
                  ├──────────────────────────────────────────────┤
                  │                                              │
                  │  1. 检测 LLM Driver 是否实现 ToolCallingDriver│
                  │     └─ 否 → 走 toolProtocol 文本路径          │
                  │     └─ 是 → 继续                             │
                  │                                              │
                  │  2. 收集 ToolDefs                             │
                  │     ├─ 静态设备: 类型断言 ToolDescriptor       │
                  │     │   ├─ shellDriver.ToolDefs()             │
                  │     │   └─ fsDriver.ToolDefs()                │
                  │     ├─ MCP 设备: VFS 读取 tools/list           │
                  │     │   └─ Open/Read /dev/mcp/*/tools         │
                  │     └─ Meta 动作: 内核内置定义                  │
                  │         └─ spawn, complete, replan, ...        │
                  │                                              │
                  │  3. AllowedDevices 过滤                       │
                  │     └─ 只保留进程有权限的工具                  │
                  │                                              │
                  │  4. 构建 toolMap (name → VFS path)             │
                  │     └─ 存储在 Process 上供执行时反查           │
                  │                                              │
                  └──────────────────────────────────────────────┘
                                      ↓
                  ┌──────────────────────────────────────────────┐
                  │            reasonStep 循环                    │
                  ├──────────────────────────────────────────────┤
                  │                                              │
                  │  LLMRequest.Tools = 收集的 ToolDefs           │
                  │  LLMResponse.ToolCalls → 查 toolMap 执行      │
                  │                                              │
                  └──────────────────────────────────────────────┘
```

### 四、安全模型：能力声明 vs 权限控制

借鉴 seL4 的能力模型和 Fuchsia 的双向声明：

**当前 rnix（单向）**：
- 消费者（进程）声明需要哪些设备 → AllowedDevices
- 提供者（设备）不声明任何能力 → toolProtocol 硬编码代替

**目标模型（双向）**：
- 提供者（设备）声明自己的能力 → ToolDescriptor / tools/list
- 消费者（进程）声明需要哪些能力 → AllowedDevices
- 内核做**交集**：只向 LLM 暴露双方都允许的工具

这实现了 **最小权限原则**：即使一个设备注册了 10 个工具，进程只能使用它被授权的那些。

### 五、新设备扩展路径（改进后）

引入 ToolDescriptor 后，添加 `/dev/db` 只需要：

| 文件 | 修改内容 |
|------|---------|
| `drivers/db/db.go` (新建) | 实现 VFSFile + **ToolDescriptor** |
| `drivers/db/db_test.go` (新建) | 驱动测试 |
| `cmd/rnix/main.go` | 添加 `devReg.Register("/dev/db", ...)` |
| `agents/*/agent.yaml` | 更新 allowed_tools |

**不再需要修改 kernel/kernel.go 的 toolProtocol** — 从 6 处变更减少到 4 处，且消除了与内核代码的耦合。设备开发者完全在自己的包内定义工具能力。

_Source: [capDL Capability Description Language](https://trustworthy.systems/publications/nicta_full_text/3679.pdf), [Fuchsia Capabilities](https://fuchsia.dev/fuchsia-src/concepts/components/v2/capabilities)_

## 架构模式分析

### 一、推荐架构：ToolDescriptor 可选接口 + 混合发现

#### 1.1 核心接口设计

采用 Go 标准库惯用的**可选接口模式**（如 `io.WriterTo`、`http.Flusher`）：

```go
// vfs/vfs.go — 新增

// ToolDef describes a tool that an LLM can invoke via native function calling.
// JSON tags are compatible with drivers/llm.ToolDef for VFS bridge serialization.
type ToolDef struct {
    Name        string         `json:"name"`
    Description string         `json:"description,omitempty"`
    Parameters  map[string]any `json:"parameters,omitempty"` // JSON Schema
}

// ToolDescriptor is an optional interface for VFS drivers that can describe
// their capabilities as structured tool definitions. The kernel checks for
// this interface during spawn to build ToolDef lists for native tool calling.
//
// Design rationale: Follows Go's optional capability pattern (io.WriterTo,
// http.Flusher). Drivers that don't implement this interface continue to
// work via the text-based toolProtocol.
type ToolDescriptor interface {
    ToolDefs() []ToolDef
}
```

**为什么放在 `vfs` 包**：ToolDef 是 VFS 层概念（设备向内核声明能力），而非驱动层概念。这避免了 `kernel` → `drivers` 的反向依赖。

**为什么不用 `llm.ToolDef`**：保持包隔离。`vfs.ToolDef` 和 `llm.ToolDef` 结构相同、JSON 兼容，内核做转换。这与 `context.Message` 和 `llm.Message` 的设计一致。

_Source: [Go Optional Interface Pattern](https://blog.merovius.de/posts/2017-07-30-the-trouble-with-optional-interfaces/), [Go Interface Compliance](https://www.stanza.dev/courses/go-architecture/interface-design/go-architecture-interface-compliance)_

#### 1.2 静态设备实现 ToolDescriptor

**Shell 驱动**（`drivers/shell/shell.go`）：

```go
// Compile-time check
var _ vfs.ToolDescriptor = (*ShellDriver)(nil)

func (d *ShellDriver) ToolDefs() []vfs.ToolDef {
    return []vfs.ToolDef{{
        Name:        "shell",
        Description: "Execute a shell command and return stdout+stderr. " +
            "Commands run via sh -c in the process working directory. " +
            "Non-zero exit codes are reported in output, not as errors.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "command": map[string]any{
                    "type":        "string",
                    "description": "Shell command to execute",
                },
            },
            "required": []string{"command"},
        },
    }}
}
```

**FS 驱动**（`drivers/fs/hostfs.go`）— 拆分为 3 个工具：

```go
func (d *HostFSDriver) ToolDefs() []vfs.ToolDef {
    return []vfs.ToolDef{
        {
            Name:        "read_file",
            Description: "Read file contents. Path is relative to project root.",
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "path": map[string]any{"type": "string", "description": "File path relative to project root"},
                },
                "required": []string{"path"},
            },
        },
        {
            Name:        "write_file",
            Description: "Write content to a file. Creates or overwrites.",
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "path":    map[string]any{"type": "string", "description": "File path relative to project root"},
                    "content": map[string]any{"type": "string", "description": "File content to write"},
                },
                "required": []string{"path", "content"},
            },
        },
        {
            Name:        "list_dir",
            Description: "List directory contents.",
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "path": map[string]any{"type": "string", "description": "Directory path relative to project root"},
                },
                "required": []string{"path"},
            },
        },
    }
}
```

#### 1.3 动态设备发现（MCP）

MCP 设备已有 `tools/list` 机制。在 Spawn 时通过 VFS 读取：

```go
// kernel 在 spawn 阶段收集 MCP 工具
func (k *KernelImpl) collectMCPToolDefs(proc *Process, mountPath string) []vfs.ToolDef {
    fd, _ := k.vfs.Open(proc.ctx, proc.PID, mountPath+"/tools", vfs.O_RDONLY, "")
    data, _ := k.vfs.Read(proc.PID, fd, 1<<20)
    k.vfs.Close(proc.PID, fd)

    var mcpTools []struct {
        Name        string         `json:"name"`
        Description string         `json:"description"`
        InputSchema map[string]any `json:"inputSchema"`
    }
    json.Unmarshal(data, &mcpTools)

    var defs []vfs.ToolDef
    for _, t := range mcpTools {
        defs = append(defs, vfs.ToolDef{
            Name:        "mcp__" + serverName + "__" + t.Name,
            Description: t.Description,
            Parameters:  t.InputSchema,
        })
    }
    return defs
}
```

#### 1.4 Meta 动作工具（内核内置）

Spawn/Complete/Replan/Specialize/Plan 不是 VFS 设备，由内核定义：

```go
// kernel/toolgen.go
func metaToolDefs(planningEnabled bool) []vfs.ToolDef {
    defs := []vfs.ToolDef{
        {Name: "complete", Description: "Finish with a final result", Parameters: ...},
        {Name: "spawn", Description: "Spawn a child agent process", Parameters: ...},
        {Name: "replan", Description: "Revise your approach", Parameters: ...},
        {Name: "specialize", Description: "Load a skill by name", Parameters: ...},
    }
    if planningEnabled {
        defs = append(defs, vfs.ToolDef{Name: "plan", ...})
    }
    return defs
}
```

### 二、工具发现总流程

```
┌─────────────── DeviceRegistry ────────────────┐
│                                                │
│  "/dev/shell" → ShellDriver (ToolDescriptor✓)  │
│  "/dev/fs"    → HostFSDriver (ToolDescriptor✓) │
│  "/dev/llm/X" → LLMFile (ToolCapable✓)        │
│  "/dev/mcp/Y" → mcpFile (动态 tools/list)       │
│                                                │
└────────────────────┬───────────────────────────┘
                     │
          ┌──────────▼──────────┐
          │   Spawn 阶段收集     │
          │                     │
          │  1. 遍历 AllowedDevices       │
          │  2. 对每个设备:              │
          │     - 获取 driver/factory    │
          │     - 类型断言 ToolDescriptor │
          │     - 或 VFS 读 tools/list   │
          │  3. 收集 Meta 动作 ToolDefs   │
          │  4. AllowedDevices 过滤      │
          │  5. 构建 toolMap             │
          │                     │
          └──────────┬──────────┘
                     │
          ┌──────────▼──────────┐
          │   reasonStep 使用    │
          │                     │
          │  UseNativeTools=true │
          │  → req.Tools = defs │
          │  → resp.ToolCalls   │
          │  → toolMap 反查执行  │
          │                     │
          │  UseNativeTools=false│
          │  → toolProtocol 文本 │
          │  （零改动）          │
          │                     │
          └─────────────────────┘
```

### 三、关键权衡分析

#### 3.1 ToolDescriptor 在哪个对象上实现？

| 选项 | 实现在 | 优势 | 劣势 |
|------|-------|------|------|
| **A. Driver 对象** | `ShellDriver` | 编译时检查，一处定义 | 需要从 factory 获取 driver 引用 |
| B. FileFactory 闭包 | factory 函数 | 已在注册表中 | 闭包不能实现接口 |
| C. 独立注册 | 额外的 ToolDef 注册 | 灵活 | 分散，易不同步 |

**推荐 A**：Driver 对象实现 ToolDescriptor。需要在注册表中同时存储 driver 引用，或通过 DeviceRegistry 扩展提供查询接口。

**实际做法**：DeviceRegistry 增加 `RegisterWithDriver(path, factory, driver any)` 或 `DriverRegistry` 侧通道。内核在 spawn 时遍历注册表，对每个 driver 做类型断言。

或者更简洁：**让 FileFactory 本身返回 driver 引用**：

```go
// 新增到 DeviceRegistry
type DeviceEntry struct {
    Factory VFSFileFactory
    Driver  any // 可选，供 ToolDescriptor 断言
}
```

#### 3.2 toolProtocol 是否保留？

| 选项 | 行为 |
|------|------|
| **保留（推荐）** | CLI 驱动（Claude/Cursor）走 toolProtocol，API 驱动走 native tools |
| 移除 | 所有驱动走 native tools — 但 CLI 驱动不支持 ToolCallingDriver |

**必须保留**。toolProtocol 是 CLI 驱动的唯一工具描述方式。两条路径共存是正确的设计。

#### 3.3 ToolDef 定义的一致性

当 toolProtocol（文本）和 ToolDescriptor（结构化）同时存在时，两者的工具描述必须语义一致。违反一致性会导致不同 provider 看到不同的工具集。

**建议**：长期用 ToolDescriptor 作为单一事实来源，从 ToolDefs 自动生成 toolProtocol 文本。这消除了手动同步的风险。

```go
// 未来: 从 ToolDefs 自动生成 toolProtocol 文本
func generateToolProtocol(defs []vfs.ToolDef, metaDefs []vfs.ToolDef) string {
    var sb strings.Builder
    sb.WriteString("\n[Action Protocol]\n...")
    for _, d := range defs {
        // 生成 tool="/dev/xxx", data={...} 格式的文本描述
    }
    return sb.String()
}
```

### 四、与现有架构的兼容性评估

| 现有组件 | 影响 | 兼容性 |
|---------|------|--------|
| DeviceRegistry | 扩展存储 driver 引用 | 向后兼容（新增字段） |
| VFSFile 接口 | 不变 | 100% 兼容 |
| VFSFileFactory 类型 | 不变 | 100% 兼容 |
| toolProtocol 常量 | 保留，CLI 路径不变 | 100% 兼容 |
| AllowedDevices | 不变，复用前缀匹配 | 100% 兼容 |
| MountManager | 不变，MCP 走 VFS 读取 | 100% 兼容 |
| 现有测试 | 不变，不实现 ToolDescriptor 即走旧路径 | 100% 兼容 |

**结论：完全向后兼容。所有改动都是增量添加，现有功能零影响。**

## 实现建议

### 分阶段实施路线

本调研建议分 3 个阶段实施，每阶段独立可交付、可测试：

#### Phase 1: 基础设施层（ToolDescriptor + Context 扩展）

**目标**：建立设备自描述能力，不改变运行时行为

| 文件 | 变更 | 风险 |
|------|------|------|
| `vfs/vfs.go` | 新增 `ToolDef`、`ToolDescriptor`、`ToolCapable` 类型 | 无（纯新增） |
| `vfs/dev.go` | DeviceRegistry 扩展 `RegisterWithDriver`，存储 driver 引用 | 低（向后兼容） |
| `context/context.go` | Message 添加 `ToolCalls` 字段，新增 `AppendAssistantWithToolCalls` | 低（omitempty） |
| `drivers/llm/driver.go` | LLMRequest 添加 `Tools` 字段 | 无（omitempty） |
| `drivers/llm/vfsfile.go` | `SupportsToolCalling()` 方法，`writeCall` 路由到 `CallWithTools` | 低 |

**验证**：`make all` 全通过，现有行为不变

#### Phase 2: 设备自描述（各驱动实现 ToolDescriptor）

**目标**：消除 toolProtocol 硬编码耦合

| 文件 | 变更 |
|------|------|
| `drivers/shell/shell.go` | ShellDriver 实现 `ToolDescriptor` |
| `drivers/fs/hostfs.go` | HostFSDriver 实现 `ToolDescriptor`（拆分 read/write/list 三工具） |
| `cmd/rnix/main.go` | 注册时传递 driver 引用 |
| `kernel/toolgen.go`（新建） | `buildToolDefs()` 收集函数 + meta 工具定义 + `toolMap` |
| `kernel/toolgen_test.go`（新建） | ToolDef 生成、权限过滤测试 |

**验证**：ToolDef 生成正确，AllowedDevices 过滤正确

#### Phase 3: 内核集成（reasonStep 双路径）

**目标**：ToolCallingDriver 使用原生工具调用

| 文件 | 变更 |
|------|------|
| `kernel/kernel.go` | llmRequest/llmResponse 扩展，Spawn 检测，reasonStep 分支，`executeNativeToolCalls` |
| `kernel/kernel_test.go` | native tool call e2e 测试 |

**验证**：OpenAI-compat 驱动走 native tools，Claude CLI 走 toolProtocol，astrace 正确记录

### 风险与缓解

| 风险 | 概率 | 影响 | 缓解 |
|------|------|------|------|
| ToolDescriptor 返回的 schema 与 toolProtocol 不一致 | 中 | 不同 provider 行为不同 | 长期从 ToolDefs 生成 toolProtocol |
| 弱模型不遵循 strict JSON Schema | 中 | 工具调用失败 | 保留 extractCommand 防御性解析 |
| MCP tools/list 返回大量工具 | 低 | token 膨胀 | 参考 Anthropic Tool Search 的 defer_loading |
| Fallback driver 不支持 ToolCallingDriver | 中 | 会话历史格式不兼容 | Fallback 时清除 tool_calls 消息 |

### 度量标准

- **可扩展性**：添加新设备从 6 处修改 → 4 处修改（减少 33%）
- **可靠性**：native tool calling 消除 LLM 文本格式解析错误
- **兼容性**：现有 20+ 个测试包全部不变通过

---

## 研究综合

### 执行摘要

本调研评估了 rnix VFS 设备注册机制的可扩展性，并设计了原生 ToolCalls 的工具发现与自描述架构。

**核心发现**：rnix 当前的 `toolProtocol` 硬编码是设备与内核的强耦合点，添加新设备需要 6 处修改。通过引入 `ToolDescriptor` 可选接口，设备可以自描述其工具能力，实现与内核的解耦。

**设计灵感**：
- **Plan9 ctl 文件** → 设备通过文件系统接口自描述（"everything is a file"）
- **MCP tools/list** → 动态工具发现 + 运行时变更通知
- **seL4 Capability** → 能力令牌携带操作声明
- **Fuchsia FIDL/CML** → 双向能力声明（provide + use）
- **Go 标准库** → 可选接口模式（io.WriterTo、http.Flusher）

**推荐架构**：

```
                    ToolDescriptor 接口
                    （可选，设备自描述）
                           │
    ┌──────────────────────┼──────────────────────┐
    │                      │                      │
  静态设备              MCP 设备              Meta 动作
  ShellDriver          VFS tools/list        内核内置
  HostFSDriver         动态发现              spawn/complete/...
    │                      │                      │
    └──────────┬───────────┴──────────────────────┘
               │
         buildToolDefs()
               │
    ┌──────────▼──────────┐
    │   AllowedDevices    │ ← 最小权限过滤
    │      过滤           │
    └──────────┬──────────┘
               │
    ┌──────────▼──────────┐     ┌──────────────────┐
    │  ToolCallingDriver  │     │  文本协议路径      │
    │  → native tools     │     │  → toolProtocol   │
    │  (OpenAI/Anthropic) │     │  (Claude/Cursor)  │
    └─────────────────────┘     └──────────────────┘
```

**关键结论**：
1. 现有架构**可以**支持 ToolDef 自描述 — 通过 Go 可选接口模式增量添加
2. **不需要**重新设计注册协议 — DeviceRegistry 微扩展即可
3. toolProtocol **必须保留** — CLI 驱动的唯一路径，长期可自动生成
4. 分 3 阶段实施，每阶段独立可交付，**完全向后兼容**

### 参考来源

- [OpenAI Function Calling Guide](https://platform.openai.com/docs/guides/function-calling)
- [MCP Specification (2025-11-25)](https://modelcontextprotocol.io/specification/2025-11-25)
- [MCP Dynamic Tool Discovery](https://www.speakeasy.com/mcp/tool-design/dynamic-tool-discovery)
- [Anthropic Tool Use](https://platform.claude.com/docs/en/agents-and-tools/tool-use/implement-tool-use)
- [Anthropic Advanced Tool Use](https://www.anthropic.com/engineering/advanced-tool-use)
- [seL4 Whitepaper 2025](https://sel4.systems/About/seL4-whitepaper.pdf)
- [seL4 Reference Manual](https://sel4.systems/Info/Docs/seL4-manual-latest.pdf)
- [capDL Capability Description Language](https://trustworthy.systems/publications/nicta_full_text/3679.pdf)
- [Plan9 9P Protocol Notes](https://blog.gnoack.org/post/9pnotes)
- [Plan9 Network Organization](https://9p.io/sys/doc/net/net.html)
- [Fuchsia Capabilities](https://fuchsia.dev/fuchsia-src/concepts/components/v2/capabilities)
- [Fuchsia Component Introduction](https://fuchsia.dev/fuchsia-src/concepts/components/v2/introduction)
- [Go Optional Interface Pattern](https://blog.merovius.de/posts/2017-07-30-the-trouble-with-optional-interfaces/)
- [Go Interface Compliance](https://www.stanza.dev/courses/go-architecture/interface-design/go-architecture-interface-compliance)
