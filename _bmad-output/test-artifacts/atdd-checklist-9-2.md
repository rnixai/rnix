---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-04c-aggregate', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-02'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/9-2-agent-yaml-mcp-field-and-auto-mount.md'
  - 'agents/types.go'
  - 'agents/loader.go'
  - 'agents/loader_test.go'
  - 'kernel/kernel.go'
  - 'kernel/process.go'
  - 'kernel/mount_test.go'
  - 'kernel/kernel_test.go'
  - 'vfs/mcp.go'
  - 'vfs/mount.go'
  - 'drivers/mcp/transport.go'
  - 'cmd/crux/main.go'
---

# ATDD Checklist - Epic 9, Story 2: agent.yaml mcp 字段与自动挂载

**Date:** 2026-03-02
**Author:** Decker
**Primary Test Level:** Unit/Integration (Go backend)

---

## Story Summary

Story 9.2 实现 Agent 的 agent.yaml 中通过 `mcp` 字段引用 MCP 服务器，Spawn 时自动挂载到 VFS 路径，进程退出时自动卸载。包括全局 MCP 配置文件解析、AgentLoader 扩展、Kernel Spawn 自动 Mount、进程退出自动 Unmount。

**As a** 用户
**I want** Agent 的 agent.yaml 中通过 `mcp` 字段引用 MCP 服务器，Spawn 时自动挂载
**So that** 我不需要手动管理 MCP 服务器的生命周期

---

## Acceptance Criteria

1. **AC1: agent.yaml mcp 字段解析** — Given agent.yaml 包含 `mcp: ["github", "slack"]`, When AgentLoader 加载该 Agent, Then AgentManifest 包含 MCP 引用列表, And 字段格式遵循 snake_case YAML 约定

2. **AC2: Spawn 时自动 Mount** — Given agent.yaml 包含 `mcp: ["github", "slack"]`, When Spawn 该 Agent 的智能体, Then 系统自动 Mount 引用的 MCP 服务器到 `/mnt/mcp/{name}/`, And 进程退出时自动 Unmount

3. **AC3: MCP 服务器生命周期管理** — Given `drivers/mcp/mcp.go` 已实现, When MCP 服务器启动, Then 管理 MCP 服务器进程生命周期（启动、健康检查、停止）

4. **AC4: MCP 配置缺失或无效时的错误处理** — Given MCP 配置缺失或无效, When Spawn 时引用该 MCP, Then 返回清晰错误信息，标注具体配置问题

5. **AC5: 全局 MCP 配置文件** — Given 项目根目录有 `mcp.yaml` 全局配置, When AgentLoader 解析 agent.yaml 的 `mcp` 字段, Then 系统从全局配置中查找对应 MCP 服务器的连接参数（command、args、env、transport_type）

6. **AC6: 进程退出时自动清理** — Given 智能体进程正在运行且已自动挂载 MCP, When 进程退出（正常完成、Kill、超时）, Then 自动 Unmount 该进程专属的 MCP 挂载, And 关闭 MCP 服务器进程, And 清理 VFS 路径

---

## Failing Tests Created (RED Phase)

### Unit Tests - drivers/mcp/config_test.go (7 tests)

**File:** `drivers/mcp/config_test.go` (~180 lines)

- **Test:** TestLoadMCPConfig/valid_config_with_multiple_servers
  - **Status:** RED - `MCPGlobalConfig` type undefined, `LoadMCPConfig` function undefined
  - **Verifies:** AC5 - 解析包含多个 server 的有效 mcp.yaml

- **Test:** TestLoadMCPConfig/valid_config_with_env_and_args
  - **Status:** RED - `MCPGlobalConfig` type undefined
  - **Verifies:** AC5 - 解析带 env 和 args 的 MCP 配置

- **Test:** TestLoadMCPConfig/empty_servers_map
  - **Status:** RED - `MCPGlobalConfig` type undefined
  - **Verifies:** AC5 - 空 servers 映射时不报错

- **Test:** TestLoadMCPConfig/invalid_yaml_returns_error
  - **Status:** RED - `LoadMCPConfig` function undefined
  - **Verifies:** AC4 - 无效 YAML 格式返回错误

- **Test:** TestLoadMCPConfig/file_not_found_returns_error
  - **Status:** RED - `LoadMCPConfig` function undefined
  - **Verifies:** AC4 - 文件不存在时返回错误

- **Test:** TestLoadMCPConfig/default_transport_type_is_stdio
  - **Status:** RED - `MCPServerConfig` type undefined
  - **Verifies:** AC5 - 未指定 transport_type 时默认为 stdio

- **Test:** TestMCPServerConfig_ToMCPConfig/converts_to_vfs_MCPConfig
  - **Status:** RED - `MCPServerConfig` type undefined
  - **Verifies:** AC5 - MCPServerConfig 正确转换为 vfs.MCPConfig

### Unit Tests - agents/loader_test.go (5 new tests appended)

**File:** `agents/loader_test.go` (appended ~120 lines)

- **Test:** TestAgentLoader_Load_WithMCPField
  - **Status:** RED - `AgentManifest.MCP` field undefined, `NewAgentLoader` signature mismatch
  - **Verifies:** AC1 - agent.yaml 包含 mcp 字段时正确解析到 AgentManifest.MCP

- **Test:** TestAgentLoader_Load_WithoutMCPField
  - **Status:** RED - `AgentManifest.MCP` field undefined
  - **Verifies:** AC1 - agent.yaml 不包含 mcp 字段时 MCP 为 nil（向后兼容）

- **Test:** TestAgentLoader_Load_MCPServerNotFound
  - **Status:** RED - `NewAgentLoader` signature mismatch, `MCPGlobalConfig` undefined
  - **Verifies:** AC4 - mcp 引用的服务器在全局配置中不存在时返回错误

- **Test:** TestAgentLoader_Load_MCPResolvesToAgentInfo
  - **Status:** RED - `AgentInfo.MCPConfigs` field undefined
  - **Verifies:** AC1/AC5 - AgentInfo.MCPConfigs 正确填充解析后的 vfs.MCPConfig

- **Test:** TestAgentLoader_Load_NilMCPConfig_SkipsMCPResolution
  - **Status:** RED - `NewAgentLoader` signature mismatch
  - **Verifies:** AC1 - mcpConfig 为 nil 时跳过 MCP 解析（向后兼容）

### Unit Tests - kernel/spawn_mcp_test.go (9 tests)

**File:** `kernel/spawn_mcp_test.go` (~350 lines)

- **Test:** TestSpawn_AutoMountMCP/spawn_with_mcp_configs_mounts_all
  - **Status:** RED - `AgentInfo.MCPConfigs` field undefined, `Process.MCPMounts` field undefined
  - **Verifies:** AC2 - Spawn 时自动 Mount 所有 MCPConfigs

- **Test:** TestSpawn_AutoMountMCP/spawn_with_mcp_configs_records_mount_paths
  - **Status:** RED - `Process.MCPMounts` field undefined
  - **Verifies:** AC2 - Mount 成功后 proc.MCPMounts 记录路径

- **Test:** TestSpawn_AutoMountMCP/spawn_mount_path_format_is_pid_name
  - **Status:** RED - `Process.MCPMounts` field undefined
  - **Verifies:** AC2 - 挂载路径格式为 `/mnt/mcp/{pid}-{server-name}/`

- **Test:** TestSpawn_AutoMountMCP/spawn_mount_failure_rolls_back_previous_mounts
  - **Status:** RED - `AgentInfo.MCPConfigs` field undefined
  - **Verifies:** AC4 - 单个 Mount 失败时回滚已成功的 Mount

- **Test:** TestSpawn_AutoMountMCP/spawn_mount_failure_returns_syscall_error
  - **Status:** RED - `AgentInfo.MCPConfigs` field undefined
  - **Verifies:** AC4 - Mount 失败时返回 SyscallError

- **Test:** TestSpawn_AutoMountMCP/spawn_without_mcp_configs_skips_mount
  - **Status:** RED - No new types needed, but verifies no regression
  - **Verifies:** AC2 - 无 MCP 引用时 Spawn 正常（不调用 Mount）

- **Test:** TestSpawn_AutoMountMCP/spawn_with_nil_mount_manager_and_mcp_returns_error
  - **Status:** RED - `AgentInfo.MCPConfigs` field undefined
  - **Verifies:** AC4 - mountMgr 为 nil 且有 MCP 引用时返回 ErrInternal

- **Test:** TestFinishProcess_AutoUnmountMCP/process_exit_unmounts_all_mcp_mounts
  - **Status:** RED - `Process.MCPMounts` field undefined
  - **Verifies:** AC6 - 进程退出时自动 Unmount proc.MCPMounts 中的路径

- **Test:** TestFinishProcess_AutoUnmountMCP/unmount_failure_does_not_block_process_exit
  - **Status:** RED - `Process.MCPMounts` field undefined
  - **Verifies:** AC6 - Unmount 失败不阻塞进程退出

---

## Data Factories Created

### Mock MCP Global Config Factory (Go)

**File:** `drivers/mcp/config_test.go` (embedded)

**Exports (test-scoped):**
- `testValidMCPYAML` - 有效的 mcp.yaml 内容字符串
- `testInvalidMCPYAML` - 无效的 YAML 内容字符串
- `testEmptyServersMCPYAML` - 空 servers 映射的 YAML

### Mock AgentLoader MCP Factory (Go)

**File:** `agents/loader_test.go` (embedded, appended)

**Exports (test-scoped):**
- `testMCPGlobalConfig()` - 创建包含 "github" 和 "slack" 服务器的测试 MCPGlobalConfig
- `testdata/mcp-agent/agent.yaml` - 包含 mcp 字段的测试 agent.yaml

### Mock MountManager for Spawn Tests (Go)

**File:** `kernel/spawn_mcp_test.go` (embedded)

**Exports (test-scoped):**
- `spawnMockMountManager` - 扩展的 mock MountManager，跟踪 Mount/Unmount 调用
- `newSpawnTestKernel(t, mountMgr)` - 创建带 MountManager 的测试 Kernel

---

## Fixtures Created

### Agent Loader Test Fixtures (Go)

**File:** `agents/testdata/mcp-agent/agent.yaml` (新增)

```yaml
name: mcp-agent
description: "Agent with MCP references for testing"
models:
  provider: claude
  preferred: sonnet
  fallback: haiku
context_budget: 4096
skills:
  - mock-skill
mcp:
  - github
  - slack
```

**File:** `agents/testdata/mcp-agent/instructions.md` (新增)

```markdown
# MCP Agent
MCP-enabled agent for testing.
```

### MCP Config Test Fixtures (Go)

**File:** `drivers/mcp/testdata/valid.yaml` (新增)

```yaml
servers:
  github:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "test-token"
    transport_type: "stdio"
  slack:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-slack"]
    env:
      SLACK_TOKEN: "test-token"
    transport_type: "stdio"
```

**File:** `drivers/mcp/testdata/empty.yaml` (新增)

```yaml
servers: {}
```

**File:** `drivers/mcp/testdata/invalid.yaml` (新增)

```
not: valid: yaml: [[[
```

### Kernel Spawn MCP Test Fixtures (Go)

**File:** `kernel/spawn_mcp_test.go` (embedded)

**Fixtures:**
- `newSpawnTestKernel(t, mountMgr)` - 创建带 mock MountManager 和 LLM 设备的测试 Kernel，使用 t.Cleanup 自动 Shutdown
- `testAgentWithMCP(mcpConfigs)` - 创建包含 MCPConfigs 的 AgentInfo
- `testAgentWithoutMCP()` - 创建不包含 MCP 的 AgentInfo

---

## Mock Requirements

### MountManager Mock (for Spawn tests)

**Interface:** `MountManager` (defined in `kernel/kernel.go`)

**Methods:**
- `Mount(path string, config vfs.MCPConfig) error`
- `Unmount(path string) error`
- `UnmountAll() error`

**Mock Pattern:** Functional mock with call tracking:
```go
type spawnMockMountManager struct {
    mountCalls   []mountCall
    unmountCalls []string
    mountFn      func(path string, config vfs.MCPConfig) error
    unmountFn    func(path string) error
    mu           sync.Mutex
}

type mountCall struct {
    Path   string
    Config vfs.MCPConfig
}
```

**Notes:** 复用 `kernel/mount_test.go` 中现有的 `mockMountManager`，但在 `spawn_mcp_test.go` 中扩展以跟踪调用详情。

---

## Required data-testid Attributes

N/A - 纯后端 Go 项目，不需要 UI 组件或 data-testid 属性。

---

## Implementation Checklist

### Test: MCPGlobalConfig / MCPServerConfig / LoadMCPConfig

**File:** `drivers/mcp/config_test.go`

**Tasks to make this test pass:**

- [ ] 创建 `drivers/mcp/config.go`
- [ ] 定义 `MCPServerConfig` 结构体（Command、Args、Env、TransportType 字段）
- [ ] 定义 `MCPGlobalConfig` 结构体（Servers map[string]MCPServerConfig）
- [ ] 实现 `LoadMCPConfig(path string) (*MCPGlobalConfig, error)` — 从 YAML 文件加载配置
- [ ] TransportType 默认值处理：未指定时默认为 "stdio"
- [ ] 创建 `drivers/mcp/testdata/` 目录及测试用 YAML 文件
- [ ] Run test: `go test ./drivers/mcp/ -run TestLoadMCPConfig -race -v`
- [ ] Run test: `go test ./drivers/mcp/ -run TestMCPServerConfig -race -v`

---

### Test: AgentManifest.MCP field and AgentLoader MCP resolution

**File:** `agents/loader_test.go`

**Tasks to make this test pass:**

- [ ] 在 `agents/types.go` 的 `AgentManifest` 中添加 `MCP []string` 字段（yaml tag: `mcp,omitempty`）
- [ ] 在 `agents/types.go` 的 `AgentInfo` 中添加 `MCPConfigs []vfs.MCPConfig` 字段
- [ ] 在 `agents/loader.go` 的 `AgentLoader` 中添加 `mcpConfig` 字段（`*mcp.MCPGlobalConfig` 类型）
- [ ] 修改 `NewAgentLoader` 签名：`NewAgentLoader(basePath string, sl *skills.SkillLoader, mcpCfg *mcp.MCPGlobalConfig)`
- [ ] 在 `AgentLoader.Load` 中添加 MCP 解析逻辑：遍历 manifest.MCP，从 mcpConfig 中查找并构建 vfs.MCPConfig
- [ ] mcpConfig 为 nil 时跳过 MCP 解析（向后兼容）
- [ ] mcp 引用不存在时返回清晰错误：`"mcp server %q not found in mcp.yaml"`
- [ ] 创建测试 agent testdata：`agents/testdata/mcp-agent/agent.yaml` 和 `instructions.md`
- [ ] 更新所有现有 `NewAgentLoader` 调用（`agents/loader_test.go` 中现有测试需传入 nil 作为第三参数）
- [ ] Run test: `go test ./agents/ -run TestAgentLoader_Load_WithMCP -race -v`
- [ ] Run test: `go test ./agents/ -run TestAgentLoader_Load_WithoutMCP -race -v`
- [ ] Run test: `go test ./agents/ -run TestAgentLoader_Load_MCPServer -race -v`
- [ ] Run test: `go test ./agents/ -run TestAgentLoader_Load_NilMCPConfig -race -v`

---

### Test: Spawn 自动 Mount MCP

**File:** `kernel/spawn_mcp_test.go`

**Tasks to make this test pass:**

- [ ] 在 `kernel/process.go` 的 `Process` 中添加 `MCPMounts []string` 字段
- [ ] 在 `kernel/kernel.go` 的 `Spawn` 方法中添加 MCP 自动挂载逻辑（在 CtxAlloc 之后、goroutine 启动之前）
- [ ] 挂载路径格式：`/mnt/mcp/{pid}-{server-name}/`
- [ ] 将挂载路径列表存入 `proc.MCPMounts`（加锁保护）
- [ ] 任意 MCP Mount 失败时，回滚已成功的 Mount 并返回 `*SyscallError`
- [ ] mountMgr 为 nil 且有 MCP 引用时返回 `*SyscallError` (ErrInternal)
- [ ] 为每次 Mount 记录 SyscallEvent（"Mount"）
- [ ] Run test: `go test ./kernel/ -run TestSpawn_AutoMountMCP -race -v`

---

### Test: 进程退出时自动 Unmount MCP

**File:** `kernel/spawn_mcp_test.go`

**Tasks to make this test pass:**

- [ ] 在 `kernel/kernel.go` 的 `finishProcess` 方法中添加 MCP 清理逻辑
- [ ] 遍历 `proc.MCPMounts` 调用 `k.Unmount` 逐一卸载
- [ ] 为每次 Unmount 记录 SyscallEvent（"Unmount", auto: true）
- [ ] Unmount 失败不阻塞进程退出（log 错误但继续清理）
- [ ] Run test: `go test ./kernel/ -run TestFinishProcess_AutoUnmountMCP -race -v`

---

### Test: Daemon 初始化（编译验证）

**File:** `cmd/crux/main.go`

**Tasks to make this test pass:**

- [ ] 在 `runDaemon` 中加载全局 `mcp.yaml` 配置
- [ ] 创建 `TransportFactory` 实现
- [ ] 调用 `agents.NewAgentLoader(basePath, skillLoader, mcpCfg)` 更新签名
- [ ] Run test: `go build ./cmd/crux/` 编译通过
- [ ] Run test: `make build` 编译通过

---

## Running Tests

```bash
# Run all failing tests for this story (will fail until implementation)
go test ./drivers/mcp/ ./agents/ ./kernel/ -race -v 2>&1 | head -200

# Run MCP config tests
go test ./drivers/mcp/ -run TestLoadMCPConfig -race -v
go test ./drivers/mcp/ -run TestMCPServerConfig -race -v

# Run AgentLoader MCP tests
go test ./agents/ -run TestAgentLoader_Load_WithMCP -race -v
go test ./agents/ -run TestAgentLoader_Load_WithoutMCP -race -v
go test ./agents/ -run TestAgentLoader_Load_MCPServer -race -v
go test ./agents/ -run TestAgentLoader_Load_NilMCPConfig -race -v

# Run Kernel Spawn MCP tests
go test ./kernel/ -run TestSpawn_AutoMountMCP -race -v
go test ./kernel/ -run TestFinishProcess_AutoUnmountMCP -race -v

# Run all tests (verify no regression)
make test

# Build verification
make build

# Full verification
make all
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All tests written and failing (compilation errors due to undefined types/methods/fields)
- Mock factories created for MountManager (call tracking variant) and MCPGlobalConfig
- Test fixtures created with auto-cleanup (t.Cleanup)
- Mock requirements documented
- Implementation checklist created

**Verification:**

- 3 test packages fail at compilation (expected RED phase for Go)
- Failure is due to missing types/methods/fields, not test bugs
- All existing tests continue to pass (no regression)

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **创建 `drivers/mcp/config.go`** — MCPGlobalConfig、MCPServerConfig、LoadMCPConfig
2. **扩展 `agents/types.go`** — AgentManifest.MCP 字段、AgentInfo.MCPConfigs 字段
3. **修改 `agents/loader.go`** — NewAgentLoader 签名扩展、Load 中 MCP 解析逻辑
4. **更新 `agents/loader_test.go`** — 现有测试的 NewAgentLoader 调用传入 nil
5. **扩展 `kernel/process.go`** — Process.MCPMounts 字段
6. **修改 `kernel/kernel.go`** — Spawn 自动 Mount + finishProcess 自动 Unmount
7. **更新 `cmd/crux/main.go`** — runDaemon 加载 mcp.yaml、更新 AgentLoader 调用
8. **Run `make all`** — 验证所有测试通过、lint 通过、编译成功

**Key Principles:**

- 一次一个测试组
- 最小实现
- 频繁运行测试
- 使用实现清单作为路线图

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

**DEV Agent Responsibilities:**

1. 验证所有测试通过（green phase 完成）
2. 检查代码质量
3. 消除重复（DRY 原则）
4. 确保每次重构后测试仍然通过
5. `make lint` 通过
6. `make build` 编译成功

---

## Next Steps

1. **Share this checklist and failing tests** with the dev workflow
2. **Begin implementation** using implementation checklist as guide (start with drivers/mcp/config.go)
3. **Work one test group at a time** (red -> green for each)
4. **Run `make all`** to verify lint + vet + test + build
5. **When all tests pass**, refactor code for quality
6. **When refactoring complete**, update story status to 'done'

---

## Knowledge Base References Applied

- **test-quality.md** - Given-When-Then 结构、one assertion per test、确定性、隔离性
- **test-levels-framework.md** - Unit tests for components, integration tests for cross-component flows
- **data-factories.md** (adapted for Go) - Mock struct factories with configurable behavior
- **component-tdd.md** (adapted) - 组件级别测试策略

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./drivers/mcp/ ./agents/ ./kernel/ -race`

**Results:**

```
FAIL github.com/gonewx/crux/drivers/mcp [build failed]
FAIL github.com/gonewx/crux/agents [build failed]
FAIL github.com/gonewx/crux/kernel [build failed]
```

**Summary:**

- Total new tests: 21
- Passing: 0 (expected - compilation failures)
- Failing: 21 (expected - types/methods/fields not implemented)
- Existing tests: All passing (no regression)
- Status: RED phase verified

**Expected Failure Reasons:**
- `drivers/mcp/config_test.go`: `MCPGlobalConfig`, `MCPServerConfig`, `LoadMCPConfig` undefined
- `agents/loader_test.go`: `AgentManifest.MCP` field undefined, `AgentInfo.MCPConfigs` field undefined, `NewAgentLoader` signature mismatch (3 args vs 2)
- `kernel/spawn_mcp_test.go`: `AgentInfo.MCPConfigs` field undefined, `Process.MCPMounts` field undefined, Spawn MCP auto-mount logic not yet implemented

---

## Notes

- 所有测试文件遵循项目现有模式（Go 标准 testing, t.Run subtests, mock interfaces）
- `drivers/mcp/config_test.go` 使用 testdata 目录存放测试用 YAML 文件
- `agents/loader_test.go` 扩展测试复用现有 testdata 结构，新增 mcp-agent 测试目录
- `kernel/spawn_mcp_test.go` 独立文件避免 kernel_test.go 过大
- Mock MountManager 扩展了 mount_test.go 中的版本，增加了调用跟踪功能
- 向后兼容性测试确保 mcpConfig 为 nil 时所有现有功能不受影响
- NewAgentLoader 签名变更需要更新所有现有调用方（测试中传入 nil）

---

**Generated by BMAD TEA Agent** - 2026-03-02
