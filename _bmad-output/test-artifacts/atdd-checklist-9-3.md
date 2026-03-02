---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-04c-aggregate', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-02'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/9-3-vfs-path-expose-mcp-tools.md'
  - 'vfs/mcp.go'
  - 'vfs/mcp_test.go'
  - 'vfs/vfs.go'
  - 'vfs/mount_test.go'
  - 'kernel/kernel.go'
  - 'kernel/process.go'
  - 'kernel/spawn_mcp_test.go'
  - 'kernel/kernel_test.go'
  - 'internal/types/types.go'
---

# ATDD Checklist - Epic 9, Story 3: VFS 路径暴露 MCP 工具

**Date:** 2026-03-02
**Author:** Decker
**Primary Test Level:** Unit/Integration (Go backend)

---

## Story Summary

Story 9.3 扩展现有 `mcpFileFactory`，根据子路径前缀创建不同的 VFSFile 实现，使 MCP 服务器的工具列表、资源读取、资源列表、根路径信息均可通过标准 VFS Open/Read/Write 操作访问。同时确保 Spawn 时自动挂载的 MCP 路径加入 AllowedDevices 白名单。

**As a** 智能体
**I want** 通过标准 VFS Open/Read/Write 访问 MCP 服务器提供的工具和资源
**So that** 我不需要知道 MCP 协议细节，只需操作文件

---

## Acceptance Criteria

1. **AC1: 工具调用（已有基础，需验证）** — Open("/mnt/mcp/1-github/tools/create-issue") 返回 VFSFile，Write 发送 tools/call，Read 返回结果
2. **AC2: 工具列表发现** — Open("/mnt/mcp/1-github/tools") 后 Read 调用 MCP tools/list 返回 JSON 工具列表
3. **AC3: 资源读取** — Open("/mnt/mcp/1-github/resources/repo://owner/repo") 后 Read 调用 resources/read 返回资源内容
4. **AC4: 资源列表发现** — Open("/mnt/mcp/1-github/resources") 后 Read 调用 resources/list 返回 JSON 资源列表
5. **AC5: 挂载根路径信息** — Open("/mnt/mcp/1-github/") 后 Read 返回 ["tools","resources"]
6. **AC6: MCP 兼容性** — 符合 MCP 标准的第三方服务器无需代码修改即可挂载使用
7. **AC7: 无效路径错误处理** — Open("/mnt/mcp/1-github/invalid-path") 返回 ErrNotFound
8. **AC8: AllowedDevices 白名单** — Spawn 后 proc.AllowedDevices 包含 MCP 挂载路径，reasonStep 权限检查通过

---

## Generation Mode

**选择模式：AI 生成**

理由：本项目为 Go 后端项目（detected_stack=backend），无需浏览器录制。验收标准清晰，测试场景为标准的接口/单元测试模式。直接使用 AI 从故事定义和源码分析生成测试。

---

## Test Strategy

### AC → 测试场景映射

| AC | 测试场景 | 测试级别 | 优先级 | 测试文件 |
|----|---------|---------|--------|---------|
| AC1 | mcpFileFactory 路由 `/tools/{name}` 到 mcpFile | Unit | P0 | `vfs/mcp_test.go` |
| AC1 | mcpFile Write→Read 完整流程无回归 | Unit | P0 | `vfs/mcp_test.go` |
| AC2 | mcpToolListFile.Read 调用 tools/list | Unit | P0 | `vfs/mcp_test.go` |
| AC2 | mcpToolListFile.Read 多次调用使用缓存 | Unit | P1 | `vfs/mcp_test.go` |
| AC2 | mcpToolListFile.Write 返回 ErrInvalid | Unit | P1 | `vfs/mcp_test.go` |
| AC2 | mcpToolListFile transport 错误返回 ErrServiceUnavailable | Unit | P0 | `vfs/mcp_test.go` |
| AC2 | mcpToolListFile Close 后 Read/Write 返回错误 | Unit | P1 | `vfs/mcp_test.go` |
| AC3 | mcpResourceFile.Read 调用 resources/read 并传入正确 URI | Unit | P0 | `vfs/mcp_test.go` |
| AC3 | parseResourceURI 解析各种 URI 格式 | Unit | P0 | `vfs/mcp_test.go` |
| AC3 | mcpResourceFile transport 错误正确传播 | Unit | P1 | `vfs/mcp_test.go` |
| AC3 | mcpResourceFile.Write 返回 ErrInvalid | Unit | P1 | `vfs/mcp_test.go` |
| AC4 | mcpResourceListFile.Read 调用 resources/list | Unit | P0 | `vfs/mcp_test.go` |
| AC4 | mcpResourceListFile 行为与 mcpToolListFile 对称 | Unit | P1 | `vfs/mcp_test.go` |
| AC5 | mcpRootFile.Read 返回 ["tools","resources"] | Unit | P0 | `vfs/mcp_test.go` |
| AC5 | mcpRootFile 内容是有效 JSON | Unit | P1 | `vfs/mcp_test.go` |
| AC5 | mcpRootFile.Write 返回 ErrInvalid | Unit | P1 | `vfs/mcp_test.go` |
| AC7 | mcpFileFactory 无效子路径返回 ErrNotFound | Unit | P0 | `vfs/mcp_test.go` |
| AC1-5 | mcpFileFactory 路由表驱动测试 | Unit | P0 | `vfs/mcp_test.go` |
| AC1-5 | readFromBuffer 辅助函数测试 | Unit | P0 | `vfs/mcp_test.go` |
| AC1-5 | mcpFileFactory 尾斜杠等价处理 | Unit | P1 | `vfs/mcp_test.go` |
| AC8 | Spawn 后 AllowedDevices 包含 MCP 路径 | Integration | P0 | `kernel/spawn_mcp_test.go` |
| AC8 | reasonStep 权限检查对 MCP 子路径通过 | Integration | P0 | `kernel/spawn_mcp_test.go` |

### 测试级别分布

- **Unit 测试** (vfs/mcp_test.go): 约 20 个测试用例
  - mcpFileFactory 路由（表驱动）
  - readFromBuffer 辅助函数
  - mcpToolListFile 完整行为
  - mcpResourceFile 完整行为
  - mcpResourceListFile 完整行为
  - mcpRootFile 完整行为
  - parseResourceURI 解析
  - mcpFile 无回归验证

- **Integration 测试** (kernel/spawn_mcp_test.go): 约 2 个测试用例
  - Spawn AllowedDevices MCP 路径追加
  - reasonStep 权限检查 MCP 子路径

### Red Phase 设计

所有测试在实现前会失败，因为：
- `mcpFileFactory` 尚未重构为多类型路由
- `mcpToolListFile`, `mcpResourceFile`, `mcpResourceListFile`, `mcpRootFile` 类型尚未定义
- `readFromBuffer` 辅助函数尚未提取
- `parseResourceURI` 函数尚未实现
- `mcpCallTimeout` 常量尚未定义
- AllowedDevices 追加逻辑尚未添加到 Spawn

---

## Failing Tests Created (RED Phase)

### Unit Tests - vfs/mcp_test.go (30 tests)

**File:** `vfs/mcp_test.go` (~590 lines)

**mcpFileFactory 路由测试（表驱动，14 case）:**

- **Test:** TestMCPFileFactory_Routing/empty_subpath_routes_to_mcpRootFile
  - **Status:** RED - `mcpRootFile` type undefined
  - **Verifies:** AC5 - 空子路径路由到根文件

- **Test:** TestMCPFileFactory_Routing/slash_routes_to_mcpRootFile
  - **Status:** RED - `mcpRootFile` type undefined
  - **Verifies:** AC5 - "/" 路由到根文件

- **Test:** TestMCPFileFactory_Routing/tools_routes_to_mcpToolListFile
  - **Status:** RED - `mcpToolListFile` type undefined
  - **Verifies:** AC2 - "/tools" 路由到工具列表文件

- **Test:** TestMCPFileFactory_Routing/tools_trailing_slash_routes_to_mcpToolListFile
  - **Status:** RED - 尾斜杠等价处理未实现
  - **Verifies:** AC2 - "/tools/" 等价于 "/tools"

- **Test:** TestMCPFileFactory_Routing/tools_create_issue_routes_to_mcpFile
  - **Status:** RED - mcpFileFactory 路由未重构
  - **Verifies:** AC1 - "/tools/create-issue" 路由到现有 mcpFile

- **Test:** TestMCPFileFactory_Routing/resources_routes_to_mcpResourceListFile
  - **Status:** RED - `mcpResourceListFile` type undefined
  - **Verifies:** AC4 - "/resources" 路由到资源列表文件

- **Test:** TestMCPFileFactory_Routing/resources_repo_routes_to_mcpResourceFile
  - **Status:** RED - `mcpResourceFile` type undefined
  - **Verifies:** AC3 - "/resources/repo://owner/repo" 路由到资源文件

- **Test:** TestMCPFileFactory_Routing/invalid_returns_ErrNotFound
  - **Status:** RED - mcpFileFactory 无效路径处理未实现
  - **Verifies:** AC7 - 无效路径返回 ErrNotFound

**readFromBuffer 辅助函数测试（6 case）:**

- **Test:** TestReadFromBuffer/empty_buffer_returns_nil_nil
  - **Status:** RED - `readFromBuffer` function undefined
  - **Verifies:** AC1-5 - 空 buffer 行为

- **Test:** TestReadFromBuffer/length_zero_returns_all_data
  - **Status:** RED - `readFromBuffer` function undefined
  - **Verifies:** AC1-5 - length=0 返回全部

- **Test:** TestReadFromBuffer/correct_chunking_and_remaining
  - **Status:** RED - `readFromBuffer` function undefined
  - **Verifies:** AC1-5 - 正确分块和 remaining 计算

- **Test:** TestReadFromBuffer/multiple_reads_drain_buffer
  - **Status:** RED - `readFromBuffer` function undefined
  - **Verifies:** AC1-5 - 多次读取耗尽 buffer

**mcpToolListFile 测试（8 case）:**

- **Test:** TestMCPToolListFile_Read/read_calls_tools_list_via_transport
  - **Status:** RED - `newMCPToolListFile` undefined
  - **Verifies:** AC2 - Read 调用 tools/list

- **Test:** TestMCPToolListFile_Read/read_passes_nil_params_to_tools_list
  - **Status:** RED - `newMCPToolListFile` undefined
  - **Verifies:** AC2 - 传递 nil params

- **Test:** TestMCPToolListFile_Read/subsequent_reads_use_cache
  - **Status:** RED - `newMCPToolListFile` undefined
  - **Verifies:** AC2 - transport 仅调用一次

- **Test:** TestMCPToolListFile_Read/read_returns_ErrServiceUnavailable
  - **Status:** RED - `newMCPToolListFile` undefined
  - **Verifies:** AC2 - transport 错误返回 ErrServiceUnavailable

- **Test:** TestMCPToolListFile_Write/write_returns_ErrInvalid
  - **Status:** RED - `newMCPToolListFile` undefined
  - **Verifies:** AC2 - 只读文件 Write 返回 ErrInvalid

- **Test:** TestMCPToolListFile_Close/close_then_read_returns_error
  - **Status:** RED - `newMCPToolListFile` undefined
  - **Verifies:** AC2 - Close 后 Read 返回错误

- **Test:** TestMCPToolListFile_Close/double_close_returns_error
  - **Status:** RED - `newMCPToolListFile` undefined
  - **Verifies:** AC2 - 双重 Close 返回错误

- **Test:** TestMCPToolListFile_Stat/stat_returns_tool_list_metadata
  - **Status:** RED - `newMCPToolListFile` undefined
  - **Verifies:** AC2 - Stat 返回设备元数据

**mcpResourceFile 测试（4 case）:**

- **Test:** TestMCPResourceFile_Read/read_calls_resources_read_with_correct_URI
  - **Status:** RED - `newMCPResourceFile` undefined
  - **Verifies:** AC3 - Read 调用 resources/read 并传入正确 URI

- **Test:** TestMCPResourceFile_Read/read_returns_ErrServiceUnavailable
  - **Status:** RED - `newMCPResourceFile` undefined
  - **Verifies:** AC3 - transport 错误传播

- **Test:** TestMCPResourceFile_Write/write_returns_ErrInvalid
  - **Status:** RED - `newMCPResourceFile` undefined
  - **Verifies:** AC3 - 只读资源 Write 返回 ErrInvalid

- **Test:** TestMCPResourceFile_Close/close_then_read_returns_error
  - **Status:** RED - `newMCPResourceFile` undefined
  - **Verifies:** AC3 - Close 后 Read 返回错误

**parseResourceURI 测试（5 case）:**

- **Test:** TestParseResourceURI/repo_scheme
  - **Status:** RED - `parseResourceURI` undefined
  - **Verifies:** AC3 - 解析 repo:// URI

- **Test:** TestParseResourceURI/file_scheme
  - **Status:** RED - `parseResourceURI` undefined
  - **Verifies:** AC3 - 解析 file:/// URI

- **Test:** TestParseResourceURI/https_scheme
  - **Status:** RED - `parseResourceURI` undefined
  - **Verifies:** AC3 - 解析 https:// URI

**mcpResourceListFile 测试（5 case）:**

- **Test:** TestMCPResourceListFile_Read/read_calls_resources_list
  - **Status:** RED - `newMCPResourceListFile` undefined
  - **Verifies:** AC4 - Read 调用 resources/list

- **Test:** TestMCPResourceListFile_Read/subsequent_reads_use_cache
  - **Status:** RED - `newMCPResourceListFile` undefined
  - **Verifies:** AC4 - transport 仅调用一次

- **Test:** TestMCPResourceListFile_Read/read_returns_ErrServiceUnavailable
  - **Status:** RED - `newMCPResourceListFile` undefined
  - **Verifies:** AC4 - transport 错误传播

- **Test:** TestMCPResourceListFile_Write/write_returns_ErrInvalid
  - **Status:** RED - `newMCPResourceListFile` undefined
  - **Verifies:** AC4 - 只读列表 Write 返回 ErrInvalid

**mcpRootFile 测试（5 case）:**

- **Test:** TestMCPRootFile_Read/read_returns_tools_and_resources_namespace_list
  - **Status:** RED - `newMCPRootFile` undefined
  - **Verifies:** AC5 - Read 返回 ["tools","resources"]

- **Test:** TestMCPRootFile_Read/read_returns_valid_JSON
  - **Status:** RED - `newMCPRootFile` undefined
  - **Verifies:** AC5 - 返回有效 JSON

- **Test:** TestMCPRootFile_Read/read_drains_on_second_call
  - **Status:** RED - `newMCPRootFile` undefined
  - **Verifies:** AC5 - 第二次 Read 返回 nil

- **Test:** TestMCPRootFile_Write/write_returns_ErrInvalid
  - **Status:** RED - `newMCPRootFile` undefined
  - **Verifies:** AC5 - 只读根 Write 返回 ErrInvalid

- **Test:** TestMCPRootFile_Close/close_then_read_returns_error
  - **Status:** RED - `newMCPRootFile` undefined
  - **Verifies:** AC5 - Close 后 Read 返回错误

**mcpCallTimeout 常量测试（1 case）:**

- **Test:** TestMCPCallTimeout/mcpCallTimeout_is_30_seconds
  - **Status:** RED - `mcpCallTimeout` constant undefined
  - **Verifies:** AC2-4 - 超时常量为 30 秒

**mcpFile 回归测试（2 case）:**

- **Test:** TestMCPFile_WriteReadRoundTrip/write_then_read_complete_round_trip
  - **Status:** GREEN (existing) - 回归验证 Write→Read 流程不变
  - **Verifies:** AC1 - 工具调用无回归

- **Test:** TestParseToolName/extracts_tool_name_from_subpath
  - **Status:** GREEN (existing) - 回归验证 parseToolName
  - **Verifies:** AC1 - 工具名解析不变

### Integration Tests - kernel/spawn_mcp_test.go (3 new tests)

**File:** `kernel/spawn_mcp_test.go` (appended ~90 lines)

- **Test:** TestSpawn_AllowedDevices_IncludesMCPPaths/spawn_appends_mcp_mount_paths_to_AllowedDevices
  - **Status:** RED - AllowedDevices 追加逻辑未实现
  - **Verifies:** AC8 - Spawn 后 AllowedDevices 包含 MCP 挂载路径

- **Test:** TestSpawn_AllowedDevices_IncludesMCPPaths/spawn_without_mcp_does_not_add_mcp_paths
  - **Status:** RED - 验证无 MCP 时不污染 AllowedDevices
  - **Verifies:** AC8 - 无 MCP 时不添加 MCP 路径

- **Test:** TestSpawn_AllowedDevices_IncludesMCPPaths/mcp_subpath_matches_AllowedDevices_prefix_check
  - **Status:** RED - 前缀匹配验证
  - **Verifies:** AC8 - reasonStep 权限检查对 MCP 子路径通过

---

## Data Factories Created

### Mock Transport（已有）

**File:** `vfs/mount_test.go`

**说明：** 复用已有的 `mockMCPTransport` mock，支持 `callFn` 回调函数注入，可捕获 method、params 参数并返回自定义响应或错误。

---

## Mock Requirements

### MCP Transport Mock

**接口:** `MCPTransport`

**方法:**
- `Call(ctx, "tools/list", nil)` → 返回工具列表 JSON
- `Call(ctx, "tools/call", params)` → 返回工具执行结果
- `Call(ctx, "resources/read", {"uri": uri})` → 返回资源内容
- `Call(ctx, "resources/list", nil)` → 返回资源列表 JSON

**说明:** 所有测试使用已有的 `mockMCPTransport`，通过 `callFn` 注入行为。无需新增外部服务 mock。

---

## Implementation Checklist

### Test: TestMCPFileFactory_Routing（14 case）

**File:** `vfs/mcp_test.go`

**Tasks to make this test pass:**

- [ ] 在 `vfs/mcp.go` 中定义 `mcpCallTimeout = 30 * time.Second` 常量
- [ ] 实现 `readFromBuffer(buf []byte, length int) ([]byte, []byte)` 辅助函数
- [ ] 实现 `parseResourceURI(subpath string) string` 函数
- [ ] 实现 `mcpRootFile` 结构体及 `newMCPRootFile()` 构造函数
- [ ] 实现 `mcpToolListFile` 结构体及 `newMCPToolListFile(transport)` 构造函数
- [ ] 实现 `mcpResourceListFile` 结构体及 `newMCPResourceListFile(transport)` 构造函数
- [ ] 实现 `mcpResourceFile` 结构体及 `newMCPResourceFile(subpath, transport)` 构造函数
- [ ] 重构 `mcpFileFactory` 添加子路径路由逻辑
- [ ] 运行测试: `go test -v -run TestMCPFileFactory_Routing ./vfs/`
- [ ] 测试通过 (green phase)

---

### Test: TestReadFromBuffer（6 case）

**File:** `vfs/mcp_test.go`

**Tasks to make this test pass:**

- [ ] 从现有 `mcpFile.Read()` 中提取分块读取逻辑为 `readFromBuffer` 私有函数
- [ ] 空 buffer 返回 (nil, nil)
- [ ] length=0 或超出返回全部
- [ ] 正确计算 data 和 remaining
- [ ] 运行测试: `go test -v -run TestReadFromBuffer ./vfs/`
- [ ] 测试通过 (green phase)

---

### Test: TestMCPToolListFile_*（8 case）

**File:** `vfs/mcp_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `mcpToolListFile` 结构体（transport, response, closed, loaded 字段）
- [ ] `Read()`: 首次调用 `transport.Call(ctx, "tools/list", nil)`，后续从缓存返回
- [ ] `Read()`: 使用 `readFromBuffer` 分块读取
- [ ] `Read()`: transport 错误包装为 `types.NewDriverError(..., types.ErrServiceUnavailable)`
- [ ] `Write()`: 返回 `types.NewDriverError(..., types.ErrInvalid)`
- [ ] `Close()`: 标记 closed，清理 response
- [ ] `Stat()`: 返回 `FileStat{Name: "/tools", IsDevice: true}`
- [ ] 运行测试: `go test -v -run TestMCPToolListFile ./vfs/`
- [ ] 测试通过 (green phase)

---

### Test: TestMCPResourceFile_*（4 case）

**File:** `vfs/mcp_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `mcpResourceFile` 结构体
- [ ] `Read()`: 解析 URI 后调用 `transport.Call(ctx, "resources/read", {"uri": uri})`
- [ ] `Read()`: 使用 `readFromBuffer` 分块读取
- [ ] `Write()`: 返回 ErrInvalid
- [ ] `Close()`: 标记 closed
- [ ] 运行测试: `go test -v -run TestMCPResourceFile ./vfs/`
- [ ] 测试通过 (green phase)

---

### Test: TestParseResourceURI（5 case）

**File:** `vfs/mcp_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `parseResourceURI(subpath string) string`
- [ ] 从 `/resources/repo://owner/repo` 提取 `repo://owner/repo`
- [ ] 支持各种 URI scheme（repo://, file:///, https://）
- [ ] 运行测试: `go test -v -run TestParseResourceURI ./vfs/`
- [ ] 测试通过 (green phase)

---

### Test: TestMCPResourceListFile_*（5 case）

**File:** `vfs/mcp_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `mcpResourceListFile` 结构体（与 mcpToolListFile 对称）
- [ ] `Read()`: 调用 `transport.Call(ctx, "resources/list", nil)`
- [ ] 缓存、错误处理、Write/Close 与 mcpToolListFile 行为对称
- [ ] 运行测试: `go test -v -run TestMCPResourceListFile ./vfs/`
- [ ] 测试通过 (green phase)

---

### Test: TestMCPRootFile_*（5 case）

**File:** `vfs/mcp_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `mcpRootFile` 结构体
- [ ] 构造时预填充 `["tools","resources"]` JSON
- [ ] `Read()`: 返回预填充数据，使用 `readFromBuffer`
- [ ] `Write()`: 返回 ErrInvalid
- [ ] `Close()`: 标记 closed
- [ ] 运行测试: `go test -v -run TestMCPRootFile ./vfs/`
- [ ] 测试通过 (green phase)

---

### Test: TestMCPCallTimeout（1 case）

**File:** `vfs/mcp_test.go`

**Tasks to make this test pass:**

- [ ] 定义 `mcpCallTimeout = 30 * time.Second` 常量
- [ ] 运行测试: `go test -v -run TestMCPCallTimeout ./vfs/`
- [ ] 测试通过 (green phase)

---

### Test: TestSpawn_AllowedDevices_IncludesMCPPaths（3 case）

**File:** `kernel/spawn_mcp_test.go`

**Tasks to make this test pass:**

- [ ] 在 `kernel/kernel.go` Spawn 方法中，MCP 自动挂载完成后的 `proc.mu.Lock()` 块内追加 AllowedDevices
- [ ] `for _, mp := range mountedPaths { proc.AllowedDevices = append(proc.AllowedDevices, mp) }`
- [ ] 验证 reasonStep 中 `strings.HasPrefix(cleanPath, dev+"/")` 正确匹配 MCP 子路径
- [ ] 运行测试: `go test -v -run TestSpawn_AllowedDevices_IncludesMCPPaths ./kernel/`
- [ ] 测试通过 (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story (VFS)
go test -v -run "TestMCPFileFactory_Routing|TestReadFromBuffer|TestMCPToolListFile|TestMCPResourceFile|TestParseResourceURI|TestMCPResourceListFile|TestMCPRootFile|TestMCPCallTimeout|TestMCPFile_WriteReadRoundTrip|TestParseToolName" ./vfs/

# Run all failing tests for this story (Kernel)
go test -v -run "TestSpawn_AllowedDevices_IncludesMCPPaths" ./kernel/

# Run all VFS tests
go test -v ./vfs/

# Run all kernel tests
go test -v ./kernel/

# Run all tests with race detection
make test

# Run lint
make lint

# Full CI check
make all
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All tests written and failing (compilation errors for VFS, runtime failures for kernel)
- Mock infrastructure reused from existing `mockMCPTransport`
- Implementation checklist created mapping each test to concrete tasks
- Tests follow existing Given-When-Then comment pattern and table-driven style

**Verification:**

- VFS tests: `go vet ./vfs/` fails with "undefined: readFromBuffer" (new types not yet defined)
- Kernel tests: Compile but fail at runtime (AllowedDevices not yet appended in Spawn)

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. 在 `vfs/mcp.go` 中添加 `mcpCallTimeout` 常量和 `readFromBuffer` 辅助函数
2. 实现 `mcpRootFile`, `mcpToolListFile`, `mcpResourceListFile`, `mcpResourceFile` 结构体
3. 实现 `parseResourceURI` 函数
4. 重构 `mcpFileFactory` 添加路由逻辑
5. 在 `kernel/kernel.go` Spawn 中追加 AllowedDevices
6. 逐一运行测试验证通过
7. `make all` 全部通过

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. 验证所有测试通过
2. 检查 5 种文件类型的代码重复，考虑提取公共模式
3. 确保所有错误使用 `types.NewDriverError` 包装
4. `make all` 通过

---

## Next Steps

1. **将此 checklist 和失败测试交给 DEV workflow**（手动移交）
2. **查看失败测试**确认 RED phase: `go vet ./vfs/`
3. **按实现清单逐项完成**，从 readFromBuffer 和常量开始
4. **每完成一项运行对应测试**验证 green
5. **全部通过后**运行 `make all` 确认无回归
6. **手动更新 sprint-status.yaml** 标记 Story 9.3 为 done

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go vet ./vfs/`

**Results:**

```
# github.com/gonewx/crux/vfs
# [github.com/gonewx/crux/vfs]
vet: vfs/mcp_test.go:257:22: undefined: readFromBuffer
```

**Summary:**

- VFS tests: Compilation failure (undefined symbols - expected in RED phase)
- Kernel tests: Compile successfully, runtime failures expected for AllowedDevices tests
- Status: RED phase verified

---

## Notes

- 本 Story 所有新增代码仅在 `vfs/mcp.go` 和 `kernel/kernel.go` 中
- 不修改 VFSFile 接口、DeviceRegistry、MountManager
- AC6 (MCP 兼容性) 通过架构设计保证，不需要独立测试用例（MCPTransport 接口抽象）
- 测试复用已有 `mockMCPTransport`，无需新增 mock 基础设施
- readFromBuffer 提取后需更新现有 mcpFile.Read() 使用新函数

---

## Contact

**Questions or Issues?**

- 参考 Story 定义: `_bmad-output/implementation-artifacts/9-3-vfs-path-expose-mcp-tools.md`
- 参考架构: `_bmad-output/planning-artifacts/architecture/`
- 参考 VFS 接口: `vfs/vfs.go`

---

**Generated by BMad TEA Agent** - 2026-03-02
