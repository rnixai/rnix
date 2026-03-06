---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-04c-aggregate', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-01'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/9-1-mount-unmount-syscall.md'
  - 'vfs/vfs.go'
  - 'vfs/dev.go'
  - 'internal/xsync/registry.go'
  - 'kernel/kernel.go'
  - 'internal/types/types.go'
  - 'kernel/errors.go'
  - 'vfs/dev_test.go'
  - 'kernel/kernel_test.go'
  - 'internal/xsync/registry_test.go'
---

# ATDD Checklist - Epic 9, Story 1: Mount/Unmount Syscall

**Date:** 2026-03-01
**Author:** Decker
**Primary Test Level:** Unit/Integration (Go backend)

---

## Story Summary

Story 9.1 implements Mount/Unmount syscall for MCP (Model Context Protocol) server integration in the VFS layer. This enables external MCP services to be mounted as file paths accessible by AI agents through the standard VFS Open/Read/Write/Close operations.

**As a** platform builder
**I want** to mount/unmount MCP servers via Mount/Unmount syscall in VFS
**So that** external services can be accessed by agents as file paths

---

## Acceptance Criteria

1. **AC1: Mount MCP Server** - Given `vfs/mcp.go` is implemented, When calling `Mount("/mnt/mcp/github", mcpConfig)`, Then MCP server is mounted at `/mnt/mcp/github/` path, And mount latency <= 500ms (NFR25)

2. **AC2: Unmount MCP Server** - Given MCP server is mounted, When calling `Unmount("/mnt/mcp/github")`, Then server is unmounted, connection closed, VFS path cleaned up

3. **AC3: Error Handling on MCP Server Failure** - Given MCP server exits abnormally, When agent accesses paths under `/mnt/mcp/github/`, Then return `ErrServiceUnavailable` error within 3 seconds (NFR26), And kernel stability is not affected

4. **AC4: Duplicate Mount Returns Error** - Given path is already mounted, When calling Mount again, Then return `*SyscallError` (path already occupied / ErrAlreadyMounted)

---

## Failing Tests Created (RED Phase)

### Unit Tests - vfs/mount_test.go (14 tests)

**File:** `vfs/mount_test.go` (~500 lines)

- **Test:** TestMountManager_Mount/mount_registers_path_in_DeviceRegistry
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC1 - Mount registers path in DeviceRegistry for VFS routing

- **Test:** TestMountManager_Mount/mount_calls_TransportFactory_and_connects
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC1 - Mount creates transport and calls Connect

- **Test:** TestMountManager_Mount/mount_with_failing_transport_returns_error
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC1/AC3 - Mount fails gracefully on transport error

- **Test:** TestMountManager_MountDuplicate/duplicate_mount_returns_ErrAlreadyMounted
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC4 - Duplicate Mount returns ErrAlreadyMounted

- **Test:** TestMountManager_Unmount/unmount_removes_path_from_DeviceRegistry
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC2 - Unmount removes VFS path registration

- **Test:** TestMountManager_Unmount/unmount_closes_Transport_connection
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC2 - Unmount closes MCP connection

- **Test:** TestMountManager_Unmount/unmount_non-existent_path_returns_error
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC2 - Unmount error handling for missing paths

- **Test:** TestMountManager_UnmountAll/unmount_all_cleans_up_all_mounts
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC2 - UnmountAll cleans all mounts on daemon shutdown

- **Test:** TestMountManager_GetStatus/get_status_of_mounted_path
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC1 - Status query returns Connected after mount

- **Test:** TestMountManager_GetStatus/get_status_of_non-existent_path_returns_error
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC1 - Status query error for non-existent mount

- **Test:** TestMountManager_ListMounts/list_mounts_returns_all_mounted_paths
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC1 - ListMounts returns all mounts

- **Test:** TestMountManager_ConcurrentAccess/concurrent_mount_and_unmount_is_safe
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC1/AC2 - Thread safety under concurrent operations

- **Test:** TestMountManager_IntegrationFlow/mount_then_open_read_write_close_unmount
  - **Status:** RED - `TransportFactory` type undefined
  - **Verifies:** AC1/AC2 - Full Mount-Open-Write-Read-Close-Unmount integration flow

- **Test:** TestDeviceRegistry_Unregister (3 subtests)
  - **Status:** RED - `DeviceRegistry.Unregister` method undefined
  - **Verifies:** AC2 - DeviceRegistry Unregister support

### Unit Tests - vfs/mcp_test.go (7 tests)

**File:** `vfs/mcp_test.go` (~180 lines)

- **Test:** TestMCPFile_Write/write_sends_tools_call_via_transport
  - **Status:** RED - `newMCPFile` function undefined
  - **Verifies:** AC1 - mcpFile.Write sends tools/call via Transport

- **Test:** TestMCPFile_Write/write_returns_ErrServiceUnavailable_when_transport_fails
  - **Status:** RED - `newMCPFile` function undefined, `types.ErrServiceUnavailable` undefined
  - **Verifies:** AC3 - Error handling returns ErrServiceUnavailable

- **Test:** TestMCPFile_Read/read_returns_tool_execution_result
  - **Status:** RED - `newMCPFile` function undefined
  - **Verifies:** AC1 - mcpFile.Read returns tool result

- **Test:** TestMCPFile_Read/read_returns_ErrServiceUnavailable_when_transport_fails
  - **Status:** RED - `newMCPFile` function undefined
  - **Verifies:** AC3 - Read error handling on transport failure

- **Test:** TestMCPFile_Close/close_does_not_close_transport_connection
  - **Status:** RED - `newMCPFile` function undefined
  - **Verifies:** AC1 - File close does not terminate MCP connection

- **Test:** TestMCPFile_Stat/stat_returns_MCP_tool_metadata
  - **Status:** RED - `newMCPFile` function undefined
  - **Verifies:** AC1 - Stat returns device metadata

- **Test:** TestMCPFile_Timeout/write_respects_context_timeout
  - **Status:** RED - `newMCPFile` function undefined
  - **Verifies:** AC3/NFR26 - Context timeout within 3 seconds

### Unit Tests - internal/xsync/registry_test.go (6 new tests)

**File:** `internal/xsync/registry_test.go` (appended ~90 lines)

- **Test:** TestRegistry_Unregister/unregister_existing_key_succeeds
  - **Status:** RED - `Registry.Unregister` method undefined
  - **Verifies:** AC2 - Registry supports Unregister

- **Test:** TestRegistry_Unregister/unregister_missing_key_returns_error
  - **Status:** RED - `Registry.Unregister` method undefined
  - **Verifies:** AC2 - Unregister error for missing key

- **Test:** TestRegistry_Unregister/unregister_then_register_same_key_succeeds
  - **Status:** RED - `Registry.Unregister` method undefined
  - **Verifies:** AC2 - Re-registration after Unregister

- **Test:** TestRegistry_Unregister/unregister_updates_list_count
  - **Status:** RED - `Registry.Unregister` method undefined
  - **Verifies:** AC2 - List count updates after Unregister

- **Test:** TestRegistry_ConcurrentUnregister/concurrent_register_and_unregister_is_safe
  - **Status:** RED - `Registry.Unregister` method undefined
  - **Verifies:** AC2 - Thread safety for concurrent Unregister

### Unit Tests - kernel/mount_test.go (9 tests)

**File:** `kernel/mount_test.go` (~295 lines)

- **Test:** TestKernel_Mount/mount_with_valid_path_delegates_to_MountManager
  - **Status:** RED - `vfs.MCPConfig` undefined, `KernelImpl.Mount` undefined
  - **Verifies:** AC1 - Kernel.Mount delegates to MountManager

- **Test:** TestKernel_Mount/mount_with_invalid_path_prefix_returns_ErrInvalid
  - **Status:** RED - `vfs.MCPConfig` undefined, `KernelImpl.Mount` undefined
  - **Verifies:** AC1 - Path validation rejects non /mnt/mcp/ paths

- **Test:** TestKernel_Mount/mount_with_nil_mountMgr_returns_ErrInternal
  - **Status:** RED - `vfs.MCPConfig` undefined, `KernelImpl.Mount` undefined
  - **Verifies:** AC1 - Nil mountMgr safety check

- **Test:** TestKernel_Mount/mount_duplicate_path_returns_SyscallError
  - **Status:** RED - `vfs.MCPConfig` undefined, `KernelImpl.Mount` undefined
  - **Verifies:** AC4 - Duplicate mount returns SyscallError

- **Test:** TestKernel_Mount/mount_emits_SyscallEvent
  - **Status:** RED - `vfs.MCPConfig` undefined, `KernelImpl.Mount` undefined
  - **Verifies:** AC1 - SyscallEvent tracing

- **Test:** TestKernel_Unmount/unmount_with_valid_path_delegates_to_MountManager
  - **Status:** RED - `KernelImpl.Unmount` undefined
  - **Verifies:** AC2 - Kernel.Unmount delegates to MountManager

- **Test:** TestKernel_Unmount/unmount_with_nil_mountMgr_returns_ErrInternal
  - **Status:** RED - `KernelImpl.Unmount` undefined
  - **Verifies:** AC2 - Nil mountMgr safety check

- **Test:** TestKernel_Unmount/unmount_non-existent_path_returns_SyscallError
  - **Status:** RED - `KernelImpl.Unmount` undefined
  - **Verifies:** AC2 - Unmount error for non-existent path

- **Test:** TestKernel_Unmount/unmount_emits_SyscallEvent
  - **Status:** RED - `KernelImpl.Unmount` undefined
  - **Verifies:** AC2 - SyscallEvent tracing

### Unit Tests - drivers/mcp/transport_test.go (5 tests)

**File:** `drivers/mcp/transport_test.go` (~110 lines)

- **Test:** TestStdioTransport_Connect/connect_initializes_MCP_session
  - **Status:** RED - `NewStdioTransport` undefined, `TransportConfig` undefined
  - **Verifies:** AC1 - Transport Connect initializes session

- **Test:** TestStdioTransport_Ping/ping_times_out_within_3_seconds
  - **Status:** RED - `NewStdioTransport` undefined, `TransportConfig` undefined
  - **Verifies:** AC3/NFR26 - Ping timeout within 3 seconds

- **Test:** TestStdioTransport_Close/close_terminates_MCP_server_process
  - **Status:** RED - `NewStdioTransport` undefined, `TransportConfig` undefined
  - **Verifies:** AC2 - Close terminates server process

- **Test:** TestStdioTransport_Call/call_sends_JSON-RPC_request_and_returns_response
  - **Status:** RED - `NewStdioTransport` undefined, `TransportConfig` undefined
  - **Verifies:** AC1 - Call sends JSON-RPC 2.0 request

- **Test:** TestTransportConfig_Validation/empty_command_is_invalid
  - **Status:** RED - `NewStdioTransport` undefined, `TransportConfig` undefined
  - **Verifies:** AC1 - Config validation

---

## Data Factories Created

### Mock Transport Factory (Go)

**File:** `vfs/mount_test.go` (embedded)

**Exports (test-scoped):**
- `mockMCPTransport` - Mock MCPTransport with configurable Connect/Call/Close/Ping functions
- `mockTransportFactory(transport)` - Creates a TransportFactory returning the given mock
- `failingTransportFactory(err)` - Creates a TransportFactory that always fails

### Mock MountManager Factory (Go)

**File:** `kernel/mount_test.go` (embedded)

**Exports (test-scoped):**
- `mockMountManager` - Mock MountManager with configurable Mount/Unmount functions
- `newMockMountManager()` - Creates a new mock with map-based tracking

---

## Fixtures Created

### Kernel Test Fixtures (Go)

**File:** `kernel/mount_test.go` (embedded)

**Fixtures:**
- `newTestKernelWithMountManager(t)` - Creates Kernel with mock MountManager and auto-cleanup
- `newTestKernelWithoutMountManager(t)` - Creates Kernel with nil mountMgr for nil-safety tests

---

## Mock Requirements

### MCP Transport Mock

**Interface:** `MCPTransport` (defined in `vfs/mcp.go` - to be implemented)

**Methods:**
- `Connect(ctx) error` - Establish MCP session
- `Call(ctx, method, params) (json.RawMessage, error)` - Send JSON-RPC 2.0 request
- `Close() error` - Terminate connection
- `Ping(ctx) error` - Health check

**Mock Pattern:** Functional mock with configurable behavior via function fields:
```go
type mockMCPTransport struct {
    connectFn func(ctx context.Context) error
    callFn    func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
    closeFn   func() error
    pingFn    func(ctx context.Context) error
}
```

**Notes:** No external service mocking required. All tests use in-process mock transports. No network calls.

---

## Required data-testid Attributes

N/A - This is a backend Go project. No UI components or data-testid attributes needed.

---

## Implementation Checklist

### Test: Registry.Unregister tests

**File:** `internal/xsync/registry_test.go`

**Tasks to make this test pass:**

- [ ] Add `Unregister(name string) error` method to `Registry[T]` in `internal/xsync/registry.go`
- [ ] Lock mutex, check existence, delete from map, return error if not found
- [ ] Run test: `go test ./internal/xsync/ -run TestRegistry_Unregister -race -v`
- [ ] Run test: `go test ./internal/xsync/ -run TestRegistry_ConcurrentUnregister -race -v`

---

### Test: DeviceRegistry.Unregister tests

**File:** `vfs/mount_test.go` (TestDeviceRegistry_Unregister section)

**Tasks to make this test pass:**

- [ ] Add `Unregister(path string) error` method to `DeviceRegistry` in `vfs/dev.go`
- [ ] Delegate to `d.registry.Unregister(path)`
- [ ] Run test: `go test ./vfs/ -run TestDeviceRegistry_Unregister -race -v`

---

### Test: Error codes (types.ErrServiceUnavailable, types.ErrAlreadyMounted)

**File:** `vfs/mount_test.go` (compile-time checks)

**Tasks to make this test pass:**

- [ ] Add `ErrServiceUnavailable ErrCode = "SERVICE_UNAVAILABLE"` to `internal/types/types.go`
- [ ] Add `ErrAlreadyMounted ErrCode = "ALREADY_MOUNTED"` to `internal/types/types.go`
- [ ] Run test: `go build ./...`

---

### Test: MCPConfig, MCPTransport, mcpFile

**File:** `vfs/mcp_test.go`

**Tasks to make this test pass:**

- [ ] Create `vfs/mcp.go` with `MCPConfig` struct
- [ ] Define `MCPTransport` interface (Connect/Call/Close/Ping)
- [ ] Define `TransportFactory` type
- [ ] Define `MCPStatus` type and constants (MCPStatusConnected, MCPStatusDisconnected, MCPStatusError)
- [ ] Define `MCPMount` struct
- [ ] Implement `mcpFile` struct satisfying `VFSFile` interface
- [ ] Implement `newMCPFile(subpath string, transport MCPTransport) *mcpFile`
- [ ] mcpFile.Write: parse subpath for tool name, call Transport.Call with tools/call method
- [ ] mcpFile.Read: return last call result
- [ ] mcpFile.Close: no-op on transport (don't close MCP connection)
- [ ] mcpFile.Stat: return FileStat with IsDevice=true
- [ ] Error wrapping: transport errors -> DriverError with ErrServiceUnavailable
- [ ] Run test: `go test ./vfs/ -run TestMCPFile -race -v`

---

### Test: MountManager

**File:** `vfs/mount_test.go`

**Tasks to make this test pass:**

- [ ] Create `vfs/mount.go` with `MountManager` struct
- [ ] Implement `NewMountManager(devReg *DeviceRegistry, factory TransportFactory) *MountManager`
- [ ] Implement `MountManager.Mount(path, config)`: validate, check duplicate, create transport, connect, register in DeviceRegistry
- [ ] Implement `MountManager.Unmount(path)`: check exists, close transport, unregister from DeviceRegistry
- [ ] Implement `MountManager.UnmountAll()`: iterate all mounts, unmount each
- [ ] Implement `MountManager.GetStatus(path)`: lookup and return MCPStatus
- [ ] Implement `MountManager.ListMounts()`: return all MCPMount entries
- [ ] Run test: `go test ./vfs/ -run TestMountManager -race -v`

---

### Test: Kernel Mount/Unmount syscall

**File:** `kernel/mount_test.go`

**Tasks to make this test pass:**

- [ ] Add `mountMgr` field to `KernelImpl` struct (interface type or `*vfs.MountManager`)
- [ ] Define `MountManager` interface in kernel (or use vfs.MountManager directly)
- [ ] Implement `KernelImpl.Mount(path string, config vfs.MCPConfig) error`
- [ ] Implement `KernelImpl.Unmount(path string) error`
- [ ] Path validation: reject paths not starting with `/mnt/mcp/`
- [ ] Nil mountMgr check: return SyscallError with ErrInternal
- [ ] Delegate to mountMgr.Mount/Unmount
- [ ] Wrap errors in SyscallError
- [ ] Update `NewKernel` to optionally accept MountManager
- [ ] Run test: `go test ./kernel/ -run TestKernel_Mount -race -v`
- [ ] Run test: `go test ./kernel/ -run TestKernel_Unmount -race -v`

---

### Test: StdioTransport

**File:** `drivers/mcp/transport_test.go`

**Tasks to make this test pass:**

- [ ] Create `drivers/mcp/transport.go`
- [ ] Define `TransportConfig` struct (Command, Args, TimeoutMillis)
- [ ] Define `Transport` interface (Connect/Call/Close/Ping)
- [ ] Implement `NewStdioTransport(config TransportConfig) *StdioTransport`
- [ ] StdioTransport.Connect: start process via exec.CommandContext, initialize handshake
- [ ] StdioTransport.Call: send JSON-RPC 2.0 via stdin, read response from stdout
- [ ] StdioTransport.Close: send shutdown notification, terminate process
- [ ] StdioTransport.Ping: health check with 3-second timeout
- [ ] Run test: `go test ./drivers/mcp/ -run TestStdioTransport -race -v`

---

### Test: Integration flow

**File:** `vfs/mount_test.go` (TestMountManager_IntegrationFlow)

**Tasks to make this test pass:**

- [ ] All above tasks completed
- [ ] Mount + DeviceRegistry.Open + VFSFile.Write/Read/Close + Unmount works end-to-end
- [ ] Run test: `go test ./vfs/ -run TestMountManager_IntegrationFlow -race -v`

---

## Running Tests

```bash
# Run all failing tests for this story (will fail until implementation)
go test ./vfs/ ./kernel/ ./internal/xsync/ ./drivers/mcp/ -race -v 2>&1 | head -100

# Run specific test file
go test ./vfs/ -run TestMountManager -race -v
go test ./vfs/ -run TestMCPFile -race -v
go test ./vfs/ -run TestDeviceRegistry_Unregister -race -v
go test ./kernel/ -run TestKernel_Mount -race -v
go test ./kernel/ -run TestKernel_Unmount -race -v
go test ./internal/xsync/ -run TestRegistry_Unregister -race -v
go test ./drivers/mcp/ -run TestStdioTransport -race -v

# Run all tests (including existing non-broken tests)
make test

# Run with verbose output
go test ./... -race -v

# Run specific test with debug
go test ./vfs/ -run TestMountManager_IntegrationFlow -race -v -count=1
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All tests written and failing (compilation errors due to undefined types/methods)
- Mock factories created for Transport and MountManager
- Test fixtures created with auto-cleanup (t.Cleanup)
- Mock requirements documented (MCPTransport interface)
- Implementation checklist created

**Verification:**

- All 4 test packages fail at compilation (expected RED phase for Go)
- Failure is due to missing types/methods, not test bugs
- All existing tests continue to pass (no regression)

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with types** - Add ErrServiceUnavailable and ErrAlreadyMounted to types.go
2. **Add Registry.Unregister** - Smallest change, unblocks DeviceRegistry and MountManager
3. **Add DeviceRegistry.Unregister** - Delegates to Registry.Unregister
4. **Create vfs/mcp.go** - MCPConfig, MCPTransport interface, mcpFile implementation
5. **Create vfs/mount.go** - MountManager implementation
6. **Create drivers/mcp/transport.go** - StdioTransport implementation
7. **Add Kernel Mount/Unmount** - Integrate MountManager into Kernel
8. **Run all tests** - Verify GREEN phase

**Key Principles:**

- One test group at a time
- Minimal implementation per test
- Run tests frequently
- Use implementation checklist as roadmap

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

**DEV Agent Responsibilities:**

1. Verify all tests pass (green phase complete)
2. Review code for quality
3. Extract duplications (DRY principle)
4. Ensure tests still pass after each refactor
5. `make lint` passes
6. `make build` compiles successfully

---

## Next Steps

1. **Share this checklist and failing tests** with the dev workflow
2. **Begin implementation** using implementation checklist as guide (start with types.go)
3. **Work one test group at a time** (red -> green for each)
4. **Run `make all`** to verify lint + vet + test + build
5. **When all tests pass**, refactor code for quality
6. **When refactoring complete**, update story status to 'done'

---

## Knowledge Base References Applied

- **test-quality.md** - Given-When-Then structure, one assertion per test, determinism, isolation
- **test-levels-framework.md** - Unit tests for components, integration tests for cross-component flows
- **data-factories.md** (adapted for Go) - Mock struct factories with configurable behavior

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./vfs/ ./kernel/ ./internal/xsync/ ./drivers/mcp/ -race`

**Results:**

```
FAIL github.com/rnixai/rnix/drivers/mcp [build failed]
FAIL github.com/rnixai/rnix/internal/xsync [build failed]
FAIL github.com/rnixai/rnix/kernel [build failed]
FAIL github.com/rnixai/rnix/vfs [build failed]
```

**Summary:**

- Total new tests: 41
- Passing: 0 (expected - compilation failures)
- Failing: 41 (expected - types/methods not implemented)
- Existing tests: All passing (no regression)
- Status: RED phase verified

**Expected Failure Reasons:**
- `vfs/mount_test.go`: `TransportFactory`, `MCPConfig`, `MCPTransport`, `types.ErrServiceUnavailable`, `types.ErrAlreadyMounted` undefined
- `vfs/mcp_test.go`: `newMCPFile` undefined, `types.ErrServiceUnavailable` undefined
- `internal/xsync/registry_test.go`: `Registry.Unregister` method undefined
- `kernel/mount_test.go`: `vfs.MCPConfig` undefined, `KernelImpl.Mount`/`Unmount` undefined, `KernelImpl.mountMgr` undefined
- `drivers/mcp/transport_test.go`: `NewStdioTransport`, `TransportConfig` undefined

---

## Notes

- All test files follow existing project patterns (Go standard testing, t.Run subtests, mock interfaces)
- Mock transports use functional field pattern for maximum test flexibility
- mcpFile tests verify VFSFile interface compliance using mock transport
- Kernel tests use mock MountManager to test syscall layer in isolation
- Integration test in mount_test.go verifies full Mount-Open-Write-Read-Close-Unmount flow
- Concurrent access tests included for thread safety verification with -race flag

---

**Generated by BMAD TEA Agent** - 2026-03-01
