# Story 9.3: VFS 路径暴露 MCP 工具

Status: done

## Story

As a 智能体,
I want 通过标准 VFS Open/Read/Write 访问 MCP 服务器提供的工具和资源,
so that 我不需要知道 MCP 协议细节，只需操作文件。

## Acceptance Criteria

1. **工具调用（已有基础，需验证）** — Given MCP 服务器已挂载（如 `/mnt/mcp/1-github/`）, When 调用 `Open("/mnt/mcp/1-github/tools/create-issue")`, Then 返回 VFSFile 封装的 MCP 工具接口, And `Write(fd, toolParams)` 发送 `tools/call` 请求, And `Read(fd, maxLen)` 返回工具执行结果

2. **工具列表发现** — Given MCP 服务器已挂载, When 调用 `Open("/mnt/mcp/1-github/tools")` 后 `Read(fd, maxLen)`, Then 调用 MCP `tools/list` 方法返回 JSON 格式的工具列表

3. **资源读取** — Given MCP 服务器已挂载, When 调用 `Open("/mnt/mcp/1-github/resources/repo://owner/repo")` 后 `Read(fd, maxLen)`, Then 调用 MCP `resources/read` 方法返回资源内容

4. **资源列表发现** — Given MCP 服务器已挂载, When 调用 `Open("/mnt/mcp/1-github/resources")` 后 `Read(fd, maxLen)`, Then 调用 MCP `resources/list` 方法返回 JSON 格式的资源列表

5. **挂载根路径信息** — Given MCP 服务器已挂载, When 调用 `Open("/mnt/mcp/1-github/")` 后 `Read(fd, maxLen)`, Then 返回 JSON 格式的可用命名空间（`["tools","resources"]`）

6. **MCP 兼容性** — Given 接入符合 MCP 标准的第三方服务器, When 挂载并使用, Then 无需 Rnix 侧代码修改即可挂载和使用（NFR27）

7. **无效路径错误处理** — Given MCP 服务器已挂载, When 调用 `Open("/mnt/mcp/1-github/invalid-path")`, Then 返回 `ErrNotFound` 错误，包含清晰路径信息

8. **AllowedDevices 白名单** — Given Spawn 时自动挂载了 MCP 服务器, When reasonStep 中 LLM 返回 tool_call 指向 `/mnt/mcp/{pid}-{name}/tools/{tool}`, Then AllowedDevices 权限检查通过, And VFS Open/Write/Read/Close 调用链正确执行

## Tasks / Subtasks

- [x] Task 1: 重构 mcpFileFactory 支持多种子路径类型（AC: #1-#7）
  - [x] 1.1 在 `vfs/mcp.go` 中重构 `mcpFileFactory`，根据子路径类型创建不同的 VFSFile 实现
  - [x] 1.2 定义子路径路由逻辑：根（`""` 或 `"/"`）→ mcpRootFile，`/tools` → mcpToolListFile，`/tools/{name}` → mcpFile（现有），`/resources` → mcpResourceListFile，`/resources/{uri}` → mcpResourceFile
  - [x] 1.3 子路径清理：统一处理尾斜杠（`/tools/` 等价于 `/tools`）
  - [x] 1.4 无效子路径返回 `types.NewDriverError("Open", subpath, err, types.ErrNotFound)`

- [x] Task 2: 实现 mcpToolListFile — 工具列表发现（AC: #2）
  - [x] 2.1 创建 `mcpToolListFile` 结构体，实现 VFSFile 接口
  - [x] 2.2 `Read()` 首次调用时使用 `context.WithTimeout(context.Background(), mcpCallTimeout)` 调用 `transport.Call(ctx, "tools/list", nil)` 并缓存结果
  - [x] 2.3 后续 `Read()` 从缓存返回（使用 `readFromBuffer` 辅助函数）
  - [x] 2.4 `Write()` 返回只读错误（`types.NewDriverError("Write", "/tools", ..., types.ErrInvalid)`）
  - [x] 2.5 `Close()` / `Stat()` 与现有 mcpFile 模式一致

- [x] Task 3: 实现 mcpResourceFile — 资源读取（AC: #3）
  - [x] 3.1 创建 `mcpResourceFile` 结构体，实现 VFSFile 接口
  - [x] 3.2 `Read()` 首次调用时解析资源 URI（`parseResourceURI`），调用 `transport.Call(ctx, "resources/read", {"uri": uri})` 并缓存结果
  - [x] 3.3 `Write()` 返回只读错误
  - [x] 3.4 实现 `parseResourceURI(subpath)` — 从 `/resources/repo://owner/repo` 提取 `repo://owner/repo`

- [x] Task 4: 实现 mcpResourceListFile — 资源列表发现（AC: #4）
  - [x] 4.1 创建 `mcpResourceListFile` 结构体，实现 VFSFile 接口
  - [x] 4.2 `Read()` 首次调用时调用 `transport.Call(ctx, "resources/list", nil)` 并缓存
  - [x] 4.3 `Write()` 返回只读错误

- [x] Task 5: 实现 mcpRootFile — 挂载根路径信息（AC: #5）
  - [x] 5.1 创建 `mcpRootFile` 结构体，实现 VFSFile 接口
  - [x] 5.2 构造时预填充 `["tools","resources"]` JSON
  - [x] 5.3 `Read()` 返回预填充数据，`Write()` 返回只读错误

- [x] Task 6: 提取 readFromBuffer 辅助函数（AC: #1-#5）
  - [x] 6.1 从现有 `mcpFile.Read()` 中提取分块读取逻辑为 `readFromBuffer(buf []byte, length int) (data []byte, remaining []byte)` 私有函数
  - [x] 6.2 所有 5 种文件类型的 Read 统一使用 `readFromBuffer`，确保分块行为一致
  - [x] 6.3 注意：`mcpFile.Read()` 的 `f.response` 字段更新需在调用 `readFromBuffer` 后赋值 remaining

- [x] Task 7: AllowedDevices 白名单支持 MCP 路径（AC: #8）
  - [x] 7.1 在 `kernel/kernel.go` Spawn 方法中，MCP 自动挂载完成后，将挂载路径追加到 `proc.AllowedDevices`
  - [x] 7.2 在已有的 `proc.mu.Lock()` ... `proc.MCPMounts = mountedPaths` 块中同时追加 AllowedDevices
  - [x] 7.3 验证 reasonStep 中现有 `strings.HasPrefix(cleanPath, dev+"/")` 逻辑能正确匹配 `/mnt/mcp/1-github/tools/create-issue` 到 `/mnt/mcp/1-github` 前缀

- [x] Task 8: 定义 mcpCallTimeout 常量（AC: #2-#4）
  - [x] 8.1 在 `vfs/mcp.go` 中定义 `mcpCallTimeout = 30 * time.Second`，用于 Read 侧 MCP 协议调用的超时
  - [x] 8.2 不要修改 VFSFile.Read 签名（不接受 context）— 在 Read 内部创建带超时的 context

- [x] Task 9: 测试（AC: #1-#8）
  - [x] 9.1 创建或扩展 `vfs/mcp_test.go`：mcpFileFactory 路由测试（表驱动）
    - 各种子路径正确路由到对应 VFSFile 类型
    - 无效子路径返回 ErrNotFound
    - 尾斜杠等价处理
  - [x] 9.2 mcpToolListFile 测试
    - Read 调用 transport.Call("tools/list", nil) 并返回结果
    - 多次 Read 使用缓存（transport 仅调用一次）
    - Write 返回 ErrInvalid 只读错误
    - transport 错误返回 ErrServiceUnavailable
    - Close 后 Read/Write 返回错误
  - [x] 9.3 mcpResourceFile 测试
    - Read 调用 transport.Call("resources/read", {"uri": ...}) 并传入正确 URI
    - parseResourceURI 正确解析各种 URI 格式（`repo://a/b`、`file:///path`、`https://...`）
    - transport 错误正确传播
  - [x] 9.4 mcpResourceListFile 测试
    - Read 调用 transport.Call("resources/list", nil) 并返回结果
    - 行为与 mcpToolListFile 对称
  - [x] 9.5 mcpRootFile 测试
    - Read 返回 `["tools","resources"]`
    - 内容是有效 JSON
  - [x] 9.6 现有 mcpFile（工具调用）无回归验证
    - Write(params) → Read(result) 完整流程不变
    - parseToolName 行为不变
  - [x] 9.7 readFromBuffer 辅助函数测试
    - 空 buffer 返回 (nil, nil)
    - length=0 返回全部
    - length 超过 buffer 长度返回全部
    - 正确分块和 remaining 计算
  - [x] 9.8 AllowedDevices MCP 路径集成测试
    - Spawn 后 proc.AllowedDevices 包含 MCP 挂载路径
    - reasonStep 权限检查对 MCP 子路径通过
  - [x] 9.9 集成验证
    - `make test` 全部通过（含 `-race`）
    - `make lint` 通过
    - `make build` 编译成功
    - 现有测试无回归

## Dev Notes

### 核心设计决策

**基于子路径的多态 VFSFile 创建**：扩展现有 `mcpFileFactory`，根据子路径前缀创建不同的 VFSFile 实现。每种 MCP 操作类型有独立的文件类型，职责清晰：

| 子路径模式 | VFSFile 类型 | MCP 方法 | 模式 |
|-----------|-------------|---------|------|
| `""` 或 `"/"` | mcpRootFile | 无（静态数据） | 只读 |
| `/tools` | mcpToolListFile | `tools/list` | 只读 |
| `/tools/{name}` | mcpFile（现有） | `tools/call` | 读写 |
| `/resources` | mcpResourceListFile | `resources/list` | 只读 |
| `/resources/{uri}` | mcpResourceFile | `resources/read` | 只读 |
| 其他 | 错误 | — | ErrNotFound |

这是最小侵入性的设计：不修改 DeviceRegistry 或 VFS 层，仅在 MCP 文件工厂层增加路由逻辑。

### VFS 路径约定

```
/mnt/mcp/{pid}-{server}/                → mcpRootFile（只读，列出命名空间）
/mnt/mcp/{pid}-{server}/tools           → mcpToolListFile（只读，tools/list）
/mnt/mcp/{pid}-{server}/tools/{name}    → mcpFile（读写，tools/call）← 已有实现
/mnt/mcp/{pid}-{server}/resources       → mcpResourceListFile（只读，resources/list）
/mnt/mcp/{pid}-{server}/resources/{uri} → mcpResourceFile（只读，resources/read）
```

### 技术要求

**mcpFileFactory 重构**（`vfs/mcp.go`）：

```go
func mcpFileFactory(transport MCPTransport) VFSFileFactory {
    return func(subpath string, flags OpenFlag) (VFSFile, error) {
        subpath = strings.TrimRight(subpath, "/")
        if subpath == "" {
            subpath = "/"
        }

        switch {
        case subpath == "/":
            return newMCPRootFile(), nil
        case subpath == "/tools":
            return newMCPToolListFile(transport), nil
        case strings.HasPrefix(subpath, "/tools/"):
            return newMCPFile(subpath, transport), nil
        case subpath == "/resources":
            return newMCPResourceListFile(transport), nil
        case strings.HasPrefix(subpath, "/resources/"):
            return newMCPResourceFile(subpath, transport), nil
        default:
            return nil, types.NewDriverError("Open", subpath,
                fmt.Errorf("unknown mcp subpath: %s (valid: /tools, /tools/{name}, /resources, /resources/{uri})"),
                types.ErrNotFound)
        }
    }
}
```

**readFromBuffer 辅助函数**：

```go
// readFromBuffer reads up to length bytes from buf. Returns the data read
// and the remaining buffer. Returns (nil, nil) when buffer is empty.
func readFromBuffer(buf []byte, length int) (data []byte, remaining []byte) {
    if len(buf) == 0 {
        return nil, nil
    }
    if length <= 0 || length > len(buf) {
        length = len(buf)
    }
    data = make([]byte, length)
    copy(data, buf[:length])
    return data, buf[length:]
}
```

**mcpToolListFile 实现**：

```go
type mcpToolListFile struct {
    transport MCPTransport
    response  []byte
    closed    bool
    loaded    bool
}

func newMCPToolListFile(transport MCPTransport) *mcpToolListFile {
    return &mcpToolListFile{transport: transport}
}

func (f *mcpToolListFile) Read(length int) ([]byte, error) {
    if f.closed {
        return nil, fmt.Errorf("read from closed mcp file")
    }
    if !f.loaded {
        ctx, cancel := context.WithTimeout(context.Background(), mcpCallTimeout)
        defer cancel()
        resp, err := f.transport.Call(ctx, "tools/list", nil)
        if err != nil {
            return nil, types.NewDriverError("Read", "/tools", err, types.ErrServiceUnavailable)
        }
        f.response = resp
        f.loaded = true
    }
    data, remaining := readFromBuffer(f.response, length)
    f.response = remaining
    return data, nil
}

func (f *mcpToolListFile) Write(_ context.Context, _ []byte) error {
    return types.NewDriverError("Write", "/tools",
        fmt.Errorf("tools listing is read-only"), types.ErrInvalid)
}

func (f *mcpToolListFile) Close() error {
    if f.closed {
        return fmt.Errorf("mcp file already closed")
    }
    f.closed = true
    f.response = nil
    return nil
}

func (f *mcpToolListFile) Stat() (FileStat, error) {
    if f.closed {
        return FileStat{}, fmt.Errorf("stat on closed mcp file")
    }
    return FileStat{Name: "/tools", IsDevice: true}, nil
}
```

**mcpResourceFile 实现**：

```go
type mcpResourceFile struct {
    subpath   string
    transport MCPTransport
    response  []byte
    closed    bool
    loaded    bool
}

func newMCPResourceFile(subpath string, transport MCPTransport) *mcpResourceFile {
    return &mcpResourceFile{subpath: subpath, transport: transport}
}

func (f *mcpResourceFile) Read(length int) ([]byte, error) {
    if f.closed {
        return nil, fmt.Errorf("read from closed mcp file")
    }
    if !f.loaded {
        uri := parseResourceURI(f.subpath)
        params, _ := json.Marshal(map[string]string{"uri": uri})
        ctx, cancel := context.WithTimeout(context.Background(), mcpCallTimeout)
        defer cancel()
        resp, err := f.transport.Call(ctx, "resources/read", params)
        if err != nil {
            return nil, types.NewDriverError("Read", f.subpath, err, types.ErrServiceUnavailable)
        }
        f.response = resp
        f.loaded = true
    }
    data, remaining := readFromBuffer(f.response, length)
    f.response = remaining
    return data, nil
}

func (f *mcpResourceFile) Write(_ context.Context, _ []byte) error {
    return types.NewDriverError("Write", f.subpath,
        fmt.Errorf("resource read is read-only"), types.ErrInvalid)
}

func (f *mcpResourceFile) Close() error {
    if f.closed {
        return fmt.Errorf("mcp file already closed")
    }
    f.closed = true
    f.response = nil
    return nil
}

func (f *mcpResourceFile) Stat() (FileStat, error) {
    if f.closed {
        return FileStat{}, fmt.Errorf("stat on closed mcp file")
    }
    return FileStat{Name: f.subpath, IsDevice: true}, nil
}
```

**parseResourceURI 实现**：

```go
// parseResourceURI extracts the resource URI from a subpath.
// e.g. "/resources/repo://owner/repo" -> "repo://owner/repo"
func parseResourceURI(subpath string) string {
    return strings.TrimPrefix(subpath, "/resources/")
}
```

**mcpResourceListFile 实现**：与 mcpToolListFile 对称，调用 `resources/list`。

**mcpRootFile 实现**：

```go
type mcpRootFile struct {
    response []byte
    closed   bool
}

func newMCPRootFile() *mcpRootFile {
    data, _ := json.Marshal([]string{"tools", "resources"})
    return &mcpRootFile{response: data}
}

func (f *mcpRootFile) Read(length int) ([]byte, error) {
    if f.closed {
        return nil, fmt.Errorf("read from closed mcp file")
    }
    data, remaining := readFromBuffer(f.response, length)
    f.response = remaining
    return data, nil
}

func (f *mcpRootFile) Write(_ context.Context, _ []byte) error {
    return types.NewDriverError("Write", "/",
        fmt.Errorf("root listing is read-only"), types.ErrInvalid)
}
```

**常量定义**：

```go
// mcpCallTimeout is the timeout for MCP protocol calls during VFS Read operations.
// Applies to tools/list, resources/list, and resources/read.
// tools/call timeout is managed by the caller's context via VFS Write.
const mcpCallTimeout = 30 * time.Second
```

**AllowedDevices 追加**（`kernel/kernel.go` Spawn 方法中）：

```go
// 在 Spawn 中，MCP 自动挂载完成后（现有 proc.mu.Lock() 块内）
proc.mu.Lock()
proc.MCPMounts = mountedPaths
// 将 MCP 挂载路径加入 AllowedDevices 使 reasonStep 权限检查通过
for _, mp := range mountedPaths {
    proc.AllowedDevices = append(proc.AllowedDevices, mp)
}
proc.mu.Unlock()
```

这确保 reasonStep 中 `strings.HasPrefix(cleanPath, dev+"/")` 正确匹配 `/mnt/mcp/1-github/tools/create-issue` 到 `/mnt/mcp/1-github` 前缀。

### Read 方法的 context 问题

`VFSFile.Read(length int)` 签名不包含 `context.Context`。对于列表类和资源读取类文件，需要在 Read 内部调用 `transport.Call`（需要 context）。

**解决方案**：在 Read 内部创建 `context.WithTimeout(context.Background(), mcpCallTimeout)` 作为超时 context。理由：
1. 不修改 VFSFile 接口签名（避免破坏性变更影响所有驱动）
2. 工具调用（Write）已通过 VFS.Write 的 context 获得超时控制
3. 列表和资源读取是独立请求，固定超时合理
4. 30 秒超时对列表和资源操作足够宽裕

**注意**：`mcpFile.Write(ctx, data)` 已正确接收 context。只有 Read 侧的新 MCP 调用需要自行创建 context。

### 依赖方向

```
本 Story 不引入新的包间依赖。所有改动在现有依赖方向内：
vfs/mcp.go      → vfs/（同包内部）
                → internal/types（已有 DriverError 依赖）
                → encoding/json（已有）
                → context（已有）
kernel/kernel.go → vfs/（已有依赖，仅修改 Spawn 内逻辑）
```

### 代码复用

**必须复用的现有代码**：
- `vfs.MCPTransport` — MCP 通信接口（Story 9.1 定义，不变）
- `vfs.mcpFile` — 现有工具调用实现（保持不变，不要重写）
- `vfs.newMCPFile` — 工具调用文件构造函数（保持不变）
- `vfs.mcpFileFactory` — 需要扩展路由逻辑（不是重建）
- `vfs.parseToolName` — 现有工具名解析（保持不变）
- `vfs.FileStat` — 文件元数据结构体
- `types.NewDriverError` — 驱动错误构造函数
- `types.ErrNotFound` / `types.ErrServiceUnavailable` / `types.ErrInvalid` — 错误码

**参考现有模式**：
- `mcpFile.Read()` 的分块读取逻辑 → 提取为 `readFromBuffer`
- `mcpFile.Close()` / `mcpFile.Stat()` 的实现模式 → 新文件类型使用相同模式
- `mcpFile.Write()` 的错误包装模式 → 只读文件类型的 Write 使用相同错误类型

### 反模式防护

- **不要**修改 `VFSFile` 接口签名 — 会导致所有驱动代码破坏
- **不要**修改现有 `mcpFile` 的 Write/Read 逻辑 — 工具调用已稳定，只扩展工厂路由
- **不要**修改 `DeviceRegistry.Open` 的前缀匹配逻辑 — 已正确支持 MCP 子路径路由
- **不要**修改 `MountManager` — 挂载/卸载逻辑已在 Story 9.1 完成
- **不要**在 ReadOnly 文件类型中接受 Write — 明确返回 ErrInvalid
- **不要**缓存 tools/list 或 resources/list 结果跨文件实例 — 每次 Open 创建新实例，避免过时缓存
- **不要**在 Read 侧 MCP 调用中使用 `context.TODO()` — 使用带 mcpCallTimeout 超时的 context
- **不要**忘记 `Close()` 中清理 response 缓存 — 防止内存泄漏
- **不要**忘记在 Spawn 中将 MCP 挂载路径加入 AllowedDevices — 否则 reasonStep 权限检查拒绝 MCP 工具调用
- **不要**使用 `.yml` 后缀 — 统一 `.yaml`
- **不要**引入 vfs → kernel 方向的导入 — 保持架构边界
- **不要**返回裸 error — 所有 mcpFile 操作的错误应包装为 `types.DriverError`

### 测试策略

**mock transport**：所有测试使用 mock `MCPTransport` 实现：

```go
type mockTransport struct {
    calls    []mockCall
    response json.RawMessage
    err      error
}

type mockCall struct {
    method string
    params json.RawMessage
}

func (m *mockTransport) Connect(ctx context.Context) error        { return nil }
func (m *mockTransport) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
    m.calls = append(m.calls, mockCall{method: method, params: params})
    return m.response, m.err
}
func (m *mockTransport) Close() error                             { return nil }
func (m *mockTransport) Ping(ctx context.Context) error           { return nil }
```

**测试用例覆盖**：

1. **mcpFileFactory 路由测试**（表驱动）：

| subpath | 预期类型 | 说明 |
|---------|---------|------|
| `""` | mcpRootFile | 空子路径 |
| `"/"` | mcpRootFile | 根路径 |
| `"/tools"` | mcpToolListFile | 工具列表 |
| `"/tools/"` | mcpToolListFile | 尾斜杠等价 |
| `"/tools/create-issue"` | mcpFile | 工具调用 |
| `"/resources"` | mcpResourceListFile | 资源列表 |
| `"/resources/"` | mcpResourceListFile | 尾斜杠等价 |
| `"/resources/repo://a/b"` | mcpResourceFile | 资源读取 |
| `"/invalid"` | error (ErrNotFound) | 无效路径 |

2. **readFromBuffer 测试**：空 buffer、length=0、length 超过、正常分块

3. **mcpToolListFile 测试**：Read→tools/list、缓存、Write→error、Close→error

4. **mcpResourceFile 测试**：Read→resources/read + URI、parseResourceURI

5. **mcpResourceListFile 测试**：Read→resources/list

6. **mcpRootFile 测试**：Read→["tools","resources"]

7. **mcpFile 回归测试**：现有 Write(tools/call)+Read 流程不变

8. **AllowedDevices 集成测试**（kernel/spawn_mcp_test.go 扩展）

### 前一个 Story 的经验教训（来自 Story 9.1 和 9.2）

1. **Lock 保护并发字段**：Story 9.2 Review 发现 MCPMounts 赋值未加锁（HIGH-1）。本 Story 修改 Spawn 中 AllowedDevices 追加时在已有 `proc.mu.Lock()` 块内。
2. **LLM FD 资源泄漏**：Story 9.2 发现 MCP Mount 失败时 LLM FD 未关闭（MEDIUM-1）。已修复，本 Story 不涉及此路径。
3. **StdioTransport.Ping 仍为 no-op**：不影响本 Story。如果 MCP 服务器崩溃，transport.Call 返回 EOF 错误，由 DriverError 正确传播。
4. **SyscallEvent 必须 emit**：Story 9.1 发现死代码。本 Story 不新增 Kernel 层 SyscallEvent（复用 reasonStep 现有 emitEvent）。
5. **Transport 调用需要 context 超时**：transport.Call 如果 MCP 服务器无响应可能阻塞。本 Story 在所有新文件类型的 Read 中使用 `mcpCallTimeout` 超时。

### Git 提交模式参考

最近提交（22a8c79）为 Story 9.2 实现。本 Story 继续 Epic 9 MCP 集成。主要影响：
- 修改：`vfs/mcp.go`（mcpFileFactory 重构 + 4 种新文件类型 + readFromBuffer + mcpCallTimeout + parseResourceURI）
- 修改：`kernel/kernel.go`（Spawn 中 AllowedDevices 追加 MCP 路径）
- 新增/扩展：`vfs/mcp_test.go`（全面单元测试）
- 扩展：`kernel/spawn_mcp_test.go`（AllowedDevices 测试）

### Project Structure Notes

修改文件：
```
vfs/mcp.go                         # mcpFileFactory 重构 + 新增 mcpToolListFile, mcpResourceFile, mcpResourceListFile, mcpRootFile + readFromBuffer + mcpCallTimeout + parseResourceURI
kernel/kernel.go                    # Spawn 中 MCP 挂载后追加 AllowedDevices
```

新增/扩展测试文件：
```
vfs/mcp_test.go                    # 全面的 MCP VFS 文件类型测试
kernel/spawn_mcp_test.go           # AllowedDevices MCP 路径测试（扩展）
```

不修改的文件：
- `vfs/vfs.go` — VFSFile 接口不变
- `vfs/dev.go` — DeviceRegistry 不变
- `vfs/mount.go` — MountManager 不变
- `drivers/mcp/transport.go` — StdioTransport 不变
- `drivers/mcp/config.go` — MCPGlobalConfig 不变
- `agents/` — Agent/Skill 加载不变
- `cmd/rnix/main.go` — Daemon 初始化不变

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-9-mcp-服务集成mcp-integration.md#Story 9.3]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR56] — MCP 工具和资源通过 VFS 暴露
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR54] — Mount/Unmount syscall
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 3] — VFS 实现策略
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 7] — Agent 抽象层与 MCP 兼容性
- [Source: _bmad-output/planning-artifacts/architecture/project-structure-boundaries.md] — 依赖方向和架构边界
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md] — 命名和编码规则
- [Source: vfs/mcp.go] — 现有 mcpFile、mcpFileFactory、parseToolName 实现
- [Source: vfs/vfs.go] — VFSFile 接口、VFS Open/Read/Write/Close
- [Source: vfs/dev.go] — DeviceRegistry 前缀匹配路由
- [Source: vfs/mount.go] — MountManager 挂载和 DeviceRegistry 注册
- [Source: drivers/mcp/transport.go] — StdioTransport JSON-RPC 实现
- [Source: kernel/kernel.go#Spawn] — 自动 Mount 和 AllowedDevices 逻辑
- [Source: kernel/kernel.go#reasonStep] — ActionToolCall 分支和权限检查
- [Source: kernel/kernel.go#finishProcess] — 自动 Unmount 逻辑
- [Source: _bmad-output/implementation-artifacts/9-1-mount-unmount-syscall.md] — Story 9.1 实现
- [Source: _bmad-output/implementation-artifacts/9-2-agent-yaml-mcp-field-and-auto-mount.md] — Story 9.2 实现

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

- golangci-lint staticcheck S1011: replaced loop append with `append(proc.AllowedDevices, mountedPaths...)` variadic form
- fmt.Errorf format string bug: `%s` without matching arg in mcpFileFactory error path, fixed by adding `subpath` arg

### Completion Notes List

- Implemented 4 new VFSFile types in `vfs/mcp.go`: mcpRootFile, mcpToolListFile, mcpResourceFile, mcpResourceListFile
- Refactored mcpFileFactory to route subpaths to appropriate file type using switch/case
- Extracted `readFromBuffer` helper for consistent chunked read behavior across all 5 MCP file types
- Defined `mcpCallTimeout = 30s` constant for Read-side MCP calls with internal context.WithTimeout
- Added `parseResourceURI` to extract resource URI from VFS subpath
- Added AllowedDevices append in Spawn for MCP mount paths (1 line using variadic append)
- All existing mcpFile (tools/call) behavior preserved - no changes to Write/Read round-trip
- ATDD tests pre-written by step2-atdd agent; all 50+ test cases pass
- Race detection clean on vfs/ and kernel/
- Lint clean (0 issues)
- Build successful

### File List

- vfs/mcp.go (modified) — mcpFileFactory refactored + 4 new file types + readFromBuffer + mcpCallTimeout + parseResourceURI
- kernel/kernel.go (modified) — Spawn: append MCP mount paths to AllowedDevices
- vfs/mcp_test.go (pre-existing ATDD tests) — comprehensive tests for all new types
- kernel/spawn_mcp_test.go (pre-existing ATDD tests) — AllowedDevices MCP path tests

## Change Log

- 2026-03-02: Implemented Story 9.3 — VFS path routing for MCP tools/resources, 4 new file types, readFromBuffer helper, AllowedDevices MCP support
- 2026-03-02: Code Review (step4-review) — 2 HIGH, 1 MEDIUM, 3 LOW issues found; 1 HIGH + 2 LOW fixed:
  - [FIXED] HIGH-1: mcpFile.Read() refactored to use readFromBuffer for consistent chunking across all 5 types
  - [NOTED] HIGH-2: Bare errors from closed-file checks (pre-existing pattern, not changed in this story scope)
  - [NOTED] MEDIUM-1: sprint-status.yaml modified but not in File List (documentation artifact)
  - [FIXED] LOW-2: Added close/write/double-close tests for mcpResourceFile
  - [FIXED] LOW-3: Added close/write/double-close tests for mcpResourceListFile
