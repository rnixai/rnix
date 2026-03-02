---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-04c-aggregate', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-02'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/9-4-four-layer-capability-stack-e2e-validation.md'
  - 'kernel/kernel.go'
  - 'kernel/process.go'
  - 'kernel/kernel_test.go'
  - 'kernel/spawn_mcp_test.go'
  - 'agents/types.go'
  - 'skills/types.go'
  - 'vfs/mcp.go'
  - 'internal/types/types.go'
  - 'debug/event.go'
---

# ATDD Checklist - Epic 9, Story 4: 四层能力栈端到端验证

**Date:** 2026-03-02
**Author:** Decker
**Primary Test Level:** Integration (Go backend)

---

## Story Summary

Story 9.4 是 Epic 9 的最后一个 Story，属于纯验证性 Story。不引入任何新的生产代码，仅通过全面的集成测试验证 Story 9.1-9.3 建立的 Agent -> Skill -> MCP -> Device 四层能力栈正确协同工作，包括 astrace 调用链路可观测性。

**As a** 用户
**I want** 验证 Agent -> Skill -> MCP -> Device 四层能力栈端到端工作
**So that** 确认各层职责分离且协同正确

---

## Acceptance Criteria

1. **AC1: 四层能力栈端到端流程** -- Given 配置了包含 Skill 和 MCP 引用的 Agent, When Spawn 并执行任务, Then Agent 层提供身份和策略, And Skill 层提供程序性知识和工具权限, And MCP 层提供外部服务集成, And Device 层提供原生 I/O (`/dev/`)
2. **AC2: astrace 四层调用链路可观测** -- Given `crux astrace` 追踪该进程, When 查看 syscall 链路, Then 可以清晰看到四层的调用边界和数据流向 (FR57)

---

## Generation Mode

**选择模式：AI 生成**

理由：本项目为 Go 后端项目（detected_stack=backend），无需浏览器录制。Story 9.4 是纯验证性 Story，所有测试均为 Go 集成测试，使用 mock 组件验证四层能力栈协同。验收标准清晰，测试场景从 Story 定义和现有源码分析即可直接生成。

---

## Test Strategy

### AC -> 测试场景映射

| AC | 测试场景 | 测试级别 | 优先级 | 测试文件 |
|----|---------|---------|--------|---------|
| AC1 | TestFourLayerCapabilityStack: 四层完整 Spawn + reasonStep 端到端流程 | Integration | P0 | `kernel/e2e_test.go` |
| AC1 | Agent 层验证：Spawn 成功，agent name 匹配，system prompt 含 Agent instructions | Integration | P0 | `kernel/e2e_test.go` |
| AC1 | Skill 层验证：AllowedDevices 含 Skill 定义的 /dev/ 路径 | Integration | P0 | `kernel/e2e_test.go` |
| AC1 | MCP 层验证：MCPMounts 含 /mnt/mcp/{pid}-{name}，MCP 工具调用通过 VFS 执行 | Integration | P0 | `kernel/e2e_test.go` |
| AC1 | Device 层验证：/dev/shell 工具调用通过 VFS 正确执行 | Integration | P0 | `kernel/e2e_test.go` |
| AC1 | 进程完成后 MCP 自动 Unmount | Integration | P0 | `kernel/e2e_test.go` |
| AC2 | TestFourLayerAstraceVisibility: DebugChan 事件包含四层调用边界 | Integration | P0 | `kernel/e2e_test.go` |
| AC2 | Spawn 事件含 agent/skills/allowed_devices/mcp_mounts args | Integration | P0 | `kernel/e2e_test.go` |
| AC2 | Mount 事件含 path 和 auto=true | Integration | P0 | `kernel/e2e_test.go` |
| AC2 | Device 层 Open/Write/Read/Close 事件序列完整 | Integration | P0 | `kernel/e2e_test.go` |
| AC2 | MCP 层 Open/Write/Read/Close 事件序列完整 | Integration | P0 | `kernel/e2e_test.go` |
| AC2 | Unmount 事件含 path 和 auto=true | Integration | P0 | `kernel/e2e_test.go` |
| AC2 | 事件时间顺序正确 | Integration | P1 | `kernel/e2e_test.go` |
| AC2 | 每个 SyscallEvent 含完整字段（PID、Syscall、Args、Result、Duration） | Integration | P0 | `kernel/e2e_test.go` |
| AC1 | TestAllowedDevicesAggregation: Skill + MCP 路径聚合正确 | Integration | P0 | `kernel/e2e_test.go` |
| AC1 | 权限检查对 Skill 设备路径通过 | Integration | P0 | `kernel/e2e_test.go` |
| AC1 | 权限检查对 MCP 子路径通过 | Integration | P0 | `kernel/e2e_test.go` |
| AC1 | 权限检查对未授权路径拒绝 | Integration | P0 | `kernel/e2e_test.go` |
| AC1 | 边界：仅 Agent+Skill 无 MCP 场景 | Integration | P1 | `kernel/e2e_test.go` |
| AC1 | 边界：仅 Agent+MCP 无 Skill 场景 | Integration | P1 | `kernel/e2e_test.go` |
| AC1 | 边界：MCP Mount 失败回滚 | Integration | P1 | `kernel/e2e_test.go` |
| AC1 | 边界：Kill 后 MCP 自动清理 | Integration | P1 | `kernel/e2e_test.go` |
| AC1 | 边界：多 MCP 服务器 + 多 Skill 路径聚合 | Integration | P1 | `kernel/e2e_test.go` |

### 测试设计原则

- **Given-When-Then 结构**：所有测试用例遵循 GWT 格式
- **一个断言一个测试**：每个子测试验证一个具体行为
- **Mock 隔离**：使用 mock LLM/Shell/MCP 组件，不依赖外部服务
- **Race 安全**：所有测试必须通过 `go test -race`
- **Channel 同步**：使用 `proc.Done` 和 `proc.DebugChan` 进行同步，禁用 `time.Sleep`

---

## Failing Tests Created (RED Phase)

### Integration Tests (5 test functions, ~24 subtests)

**File:** `kernel/e2e_test.go`

#### Test 1: TestFourLayerCapabilityStack (AC1)

- **Test:** `TestFourLayerCapabilityStack/agent_layer_spawn_success_and_identity`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** Spawn 返回有效 PID，proc.Agent.Manifest.Name 与预期一致

- **Test:** `TestFourLayerCapabilityStack/skill_layer_allowed_devices_from_skill`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** proc.AllowedDevices 包含 Skill 定义的 /dev/llm/claude、/dev/shell、/dev/fs

- **Test:** `TestFourLayerCapabilityStack/mcp_layer_mount_and_tool_call`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** MCPMounts 包含 /mnt/mcp/{pid}-mock-server，MCP 工具调用通过 VFS 执行

- **Test:** `TestFourLayerCapabilityStack/device_layer_shell_tool_call`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** /dev/shell 工具调用通过 VFS 完整执行（Open -> Write -> Read -> Close）

- **Test:** `TestFourLayerCapabilityStack/mcp_auto_unmount_on_process_exit`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** 进程完成后 Unmount 被调用

- **Test:** `TestFourLayerCapabilityStack/full_e2e_multi_step_reasoning`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** 多步推理完整流程：tool_call(/dev/shell) -> tool_call(MCP) -> text(完成)

#### Test 2: TestFourLayerAstraceVisibility (AC2)

- **Test:** `TestFourLayerAstraceVisibility/spawn_event_contains_four_layer_args`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** Spawn 事件 Args 包含 agent、skills、allowed_devices、mcp_mounts

- **Test:** `TestFourLayerAstraceVisibility/mount_event_with_auto_flag`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** Mount 事件 Args 含 path 和 auto=true

- **Test:** `TestFourLayerAstraceVisibility/device_layer_open_write_read_close_events`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** /dev/shell 的 Open/Write/Read/Close 事件序列完整

- **Test:** `TestFourLayerAstraceVisibility/mcp_layer_open_write_read_close_events`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** MCP 工具的 Open/Write/Read/Close 事件序列完整

- **Test:** `TestFourLayerAstraceVisibility/unmount_event_with_auto_flag`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** Unmount 事件 Args 含 path 和 auto=true

- **Test:** `TestFourLayerAstraceVisibility/event_chronological_order`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** Spawn -> CtxAlloc -> Open(/dev/llm) -> Mount -> ReasonStep -> ... -> Unmount 时间顺序

- **Test:** `TestFourLayerAstraceVisibility/event_fields_complete`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** 每个 SyscallEvent 包含 PID、Syscall、Args、Result、Duration

#### Test 3: TestAllowedDevicesAggregation (AC1)

- **Test:** `TestAllowedDevicesAggregation/skill_and_mcp_paths_coexist`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** AllowedDevices 同时包含 /dev/ 和 /mnt/mcp/ 路径

- **Test:** `TestAllowedDevicesAggregation/permission_allows_skill_devices`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** reasonStep 权限检查对 /dev/shell、/dev/fs 通过

- **Test:** `TestAllowedDevicesAggregation/permission_allows_mcp_subpaths`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** reasonStep 权限检查对 /mnt/mcp/{pid}-mock-server/tools/query 通过

- **Test:** `TestAllowedDevicesAggregation/permission_denies_unauthorized_path`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** reasonStep 对 /dev/unknown 拒绝

#### Test 4: TestFourLayerBoundaryConditions (AC1, AC2)

- **Test:** `TestFourLayerBoundaryConditions/agent_with_skill_no_mcp`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** 仅 Agent+Skill 无 MCP 正常工作

- **Test:** `TestFourLayerBoundaryConditions/agent_with_mcp_no_skill`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** 仅 Agent+MCP 无 Skill，MCP 路径自动加入 AllowedDevices

- **Test:** `TestFourLayerBoundaryConditions/mcp_mount_failure_rollback`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** MCP Mount 失败时正确回滚，不影响 Device 层

- **Test:** `TestFourLayerBoundaryConditions/kill_triggers_mcp_cleanup`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** Kill 后 MCP Unmount 被调用

- **Test:** `TestFourLayerBoundaryConditions/multiple_mcp_and_skills_aggregation`
  - **Status:** RED -- 测试文件不存在
  - **Verifies:** 多 MCP 服务器 + 多 Skill 的所有路径正确聚合

---

## Test Fixtures Created

### E2E Agent Fixture

**File:** `kernel/testdata/e2e-agent/agent.yaml`

```yaml
name: e2e-test-agent
description: "端到端测试用 Agent，引用 Skill 和 MCP"
models:
  provider: claude
  preferred: sonnet
  fallback: haiku
context_budget: 4096
skills:
  - e2e-skill
mcp:
  - mock-server
```

### E2E Agent Instructions Fixture

**File:** `kernel/testdata/e2e-agent/instructions.md`

```markdown
你是一个端到端测试用 Agent。请根据用户请求执行任务。
```

### E2E Skill Fixture

**File:** `kernel/testdata/e2e-skill/SKILL.md`

```markdown
---
name: e2e-skill
description: "端到端测试用 Skill"
allowed-tools: /dev/llm/claude /dev/shell /dev/fs
---

这是一个端到端测试用的 Skill，用于验证四层能力栈。
```

### Mock Components (in-test)

以下 mock 组件在 `kernel/e2e_test.go` 中内联定义：

- **mockMultiStepLLM** -- 模拟多步推理 LLM 设备（step1: tool_call /dev/shell, step2: tool_call MCP, step3: text done）
- **mockShellVFSFile** -- 模拟 /dev/shell 设备（Write 接收命令，Read 返回结果）
- **mockMCPToolVFSFile** -- 模拟 MCP 工具调用 VFS 文件（Write 接收参数，Read 返回结果）
- **spawnMockMountManager** -- 复用自 `kernel/spawn_mcp_test.go`，带调用跟踪

### 测试辅助函数 (in-test)

- **collectEvents(ch)** -- 从 DebugChan 收集所有事件直到关闭
- **findEvent(events, syscall)** -- 查找指定 syscall 名称的事件
- **findEvents(events, syscall)** -- 查找所有匹配的事件
- **findEventWithArg(events, syscall, argKey, argValue)** -- 查找含特定 arg 的事件

---

## Mock Requirements

### Mock LLM Driver (多步推理)

**设备路径:** `/dev/llm/claude`

**行为：** 返回 3 步响应：

Step 1 Response:
```json
{
  "content": "{\"action\":\"tool_call\",\"tool\":\"/dev/shell\",\"data\":{\"command\":\"echo hello\"}}",
  "tokens_used": 10
}
```

Step 2 Response:
```json
{
  "content": "{\"action\":\"tool_call\",\"tool\":\"/mnt/mcp/{pid}-mock-server/tools/query\",\"data\":{\"query\":\"test\"}}",
  "tokens_used": 10
}
```

Step 3 Response:
```json
{
  "content": "E2E test completed",
  "tokens_used": 5
}
```

### Mock Shell Device

**设备路径:** `/dev/shell`

**Success Response:**
```json
{"output": "hello", "exit_code": 0}
```

### Mock MCP Transport

**endpoint:** `tools/call` (via VFS Open/Write/Read on `/mnt/mcp/{pid}-mock-server/tools/query`)

**Success Response:**
```json
{"result": "query result from mock MCP server"}
```

**Notes:** 通过 DeviceRegistry 注册 mock factory，在 VFS Open 时返回 mock VFSFile 实例。MCP mount 通过 spawnMockMountManager 模拟。

---

## Required data-testid Attributes

N/A -- 本 Story 为纯 Go 后端集成测试，无 UI 组件。

---

## Implementation Checklist

### Test: TestFourLayerCapabilityStack

**File:** `kernel/e2e_test.go`

**Tasks to make this test pass:**

- [ ] 创建 `kernel/testdata/e2e-agent/agent.yaml` fixture 文件
- [ ] 创建 `kernel/testdata/e2e-agent/instructions.md` fixture 文件
- [ ] 创建 `kernel/testdata/e2e-skill/SKILL.md` fixture 文件
- [ ] 实现 `mockMultiStepLLM` struct（实现 VFSFile 接口，3 步响应）
- [ ] 实现 `mockShellVFSFile` struct（/dev/shell mock）
- [ ] 构造包含四层完整配置的 AgentInfo（Agent manifest + Skills + MCPConfigs）
- [ ] 注册 mock LLM 和 Shell 设备到 DeviceRegistry
- [ ] 使用 spawnMockMountManager 模拟 MCP Mount
- [ ] Spawn 进程并等待完成
- [ ] 验证 Agent 层：proc.Manifest.Name、system prompt
- [ ] 验证 Skill 层：AllowedDevices 含 /dev/ 路径
- [ ] 验证 MCP 层：MCPMounts 正确、工具调用通过 VFS
- [ ] 验证 Device 层：/dev/shell 工具调用正确
- [ ] 验证进程完成后 Unmount 被调用
- [ ] Run test: `go test -race -run TestFourLayerCapabilityStack ./kernel/`
- [ ] Test passes (green phase)

**Estimated Effort:** 3 hours

---

### Test: TestFourLayerAstraceVisibility

**File:** `kernel/e2e_test.go`

**Tasks to make this test pass:**

- [ ] 复用 TestFourLayerCapabilityStack 的 AgentInfo 和 mock 组件
- [ ] Spawn 时 DebugChan 已默认创建（NewProcess 初始化 256 缓冲）
- [ ] 等待进程完成后关闭 DebugChan 并收集事件
- [ ] 实现 collectEvents 辅助函数
- [ ] 实现 findEvent / findEvents / findEventWithArg 辅助函数
- [ ] 验证 Spawn 事件 Args 含 agent、skills、allowed_devices、mcp_mounts
- [ ] 验证 Mount 事件 Args 含 path 和 auto=true
- [ ] 验证 Device 层 (/dev/shell) Open/Write/Read/Close 事件序列
- [ ] 验证 MCP 层 Open/Write/Read/Close 事件序列
- [ ] 验证 Unmount 事件 Args 含 path 和 auto=true
- [ ] 验证事件时间顺序正确
- [ ] 验证 SyscallEvent 字段完整性（PID、Syscall、Args、Result、Duration）
- [ ] Run test: `go test -race -run TestFourLayerAstraceVisibility ./kernel/`
- [ ] Test passes (green phase)

**Estimated Effort:** 2 hours

---

### Test: TestAllowedDevicesAggregation

**File:** `kernel/e2e_test.go`

**Tasks to make this test pass:**

- [ ] 构造含 Skill（/dev/ 路径）和 MCP 的 AgentInfo
- [ ] Spawn 后验证 AllowedDevices 同时含 /dev/ 和 /mnt/mcp/ 路径
- [ ] 构造 mock LLM 返回 tool_call 到各类路径
- [ ] 验证 /dev/shell、/dev/fs 权限检查通过
- [ ] 验证 /mnt/mcp/{pid}-mock-server/tools/query 权限检查通过
- [ ] 验证 /dev/unknown 权限检查拒绝（permission denied 写入 context）
- [ ] Run test: `go test -race -run TestAllowedDevicesAggregation ./kernel/`
- [ ] Test passes (green phase)

**Estimated Effort:** 1.5 hours

---

### Test: TestFourLayerBoundaryConditions

**File:** `kernel/e2e_test.go`

**Tasks to make this test pass:**

- [ ] 测试仅 Agent+Skill 无 MCP：构造无 MCPConfigs 的 AgentInfo，验证正常推理
- [ ] 测试仅 Agent+MCP 无 Skill：构造无 Skills 的 AgentInfo，验证 MCP 路径加入 AllowedDevices
- [ ] 测试 MCP Mount 失败回滚：设置 mountFn 第二次失败，验证已挂载路径被 Unmount
- [ ] 测试 Kill 后 MCP 清理：Spawn 后 Kill(SIGKILL)，验证 Unmount 调用
- [ ] 测试多 MCP + 多 Skill：2 个 Skill 各含不同设备 + 2 个 MCP 服务器，验证所有路径聚合
- [ ] Run test: `go test -race -run TestFourLayerBoundaryConditions ./kernel/`
- [ ] Test passes (green phase)

**Estimated Effort:** 2 hours

---

## Running Tests

```bash
# Run all failing tests for this story
go test -race -run "TestFourLayer|TestAllowedDevicesAggregation" ./kernel/ -v

# Run specific test function
go test -race -run TestFourLayerCapabilityStack ./kernel/ -v

# Run with timeout
go test -race -run "TestFourLayer|TestAllowedDevicesAggregation" ./kernel/ -v -timeout 30s

# Run all kernel tests (regression check)
go test -race ./kernel/ -v

# Full project test suite
make test
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All test scenarios defined with Given-When-Then 结构
- Test fixtures 定义完成（agent.yaml, instructions.md, SKILL.md）
- Mock 组件设计完成（mockMultiStepLLM, mockShellVFSFile, spawnMockMountManager）
- 辅助函数设计完成（collectEvents, findEvent, findEvents）
- Implementation checklist 创建完成

**Verification:**

- 测试文件 `kernel/e2e_test.go` 尚不存在 -> RED
- 所有测试在实现前将 fail（文件不存在）
- 实现后必须通过 `go test -race`

---

### GREEN Phase (DEV Team -- Next Steps)

**DEV Agent Responsibilities:**

1. **创建 testdata fixtures**（Task 1: agent.yaml, instructions.md, SKILL.md）
2. **实现 mock 组件**（mockMultiStepLLM, mockShellVFSFile）
3. **实现 TestFourLayerCapabilityStack**（Task 2: 核心四层端到端测试）
4. **实现 TestFourLayerAstraceVisibility**（Task 3: astrace 事件验证）
5. **实现 TestAllowedDevicesAggregation**（Task 4: 权限聚合验证）
6. **实现 TestFourLayerBoundaryConditions**（Task 5: 边界条件测试）
7. **Run `go test -race ./kernel/ -v`** 验证所有测试通过
8. **Run `make test`** 验证无回归
9. **Run `make lint`** 验证代码质量

**Key Principles:**

- 一次实现一个 Test Function
- 使用最少的 mock 代码达到测试目的
- 复用 spawn_mcp_test.go 中已有的 mock 模式
- 频繁运行 `go test -race` 获取即时反馈

**Progress Tracking:**

- 逐个完成 Implementation Checklist 中的任务项

---

### REFACTOR Phase (DEV Team -- After All Tests Pass)

**DEV Agent Responsibilities:**

1. 验证所有测试通过（green phase complete）
2. 检查 mock 组件是否可以与 kernel_test.go 中已有 mock 合并
3. 抽取重复的 AgentInfo 构造逻辑为辅助函数
4. 确保测试命名清晰一致
5. 每次重构后运行 `go test -race`

**Key Principles:**

- 测试是安全网（重构时保持信心）
- 小步重构（每次改一处）
- 不改变测试行为（只改实现）

**Completion:**

- 所有测试通过
- 代码质量满足 `make lint`
- `make all` 全部通过
- 无回归

---

## Next Steps

1. **将此 checklist 和 fixture 定义交给 DEV workflow**
2. **DEV 按 Implementation Checklist 顺序实现**
3. **运行所有失败测试验证 RED phase**: `go test -race -run "TestFourLayer|TestAllowedDevicesAggregation" ./kernel/ -v`
4. **逐个实现测试**，使用 Implementation Checklist 作为指导
5. **每实现一个测试就运行一次**（red -> green）
6. **所有测试通过后，进行 REFACTOR**
7. **REFACTOR 完成后**，运行 `make all` 确认无回归
8. **更新 sprint-status.yaml** 标记 Story 9.4 为 done

---

## Knowledge Base References Applied

本 ATDD workflow 参考了以下知识片段：

- **test-quality.md** -- 测试设计原则（Given-When-Then、一个断言一个测试、确定性、隔离性）
- **test-levels-framework.md** -- 测试级别选择框架（本 Story 全部为 Integration 级别）
- **test-priorities-matrix.md** -- 测试优先级分配（P0 核心路径、P1 边界条件）
- **data-factories.md** -- 数据工厂模式（mock 组件设计参考）
- **test-healing-patterns.md** -- 测试修复模式（避免 flaky 测试的 channel 同步策略）
- **ci-burn-in.md** -- CI 集成验证（`go test -race` 竞态检测）

See `tea-index.csv` for complete knowledge fragment mapping.

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -race -run "TestFourLayer|TestAllowedDevicesAggregation" ./kernel/ -v`

**Expected Results:**

```
--- FAIL: TestFourLayerCapabilityStack (0.00s)
    e2e_test.go: file does not exist
--- FAIL: TestFourLayerAstraceVisibility (0.00s)
    e2e_test.go: file does not exist
--- FAIL: TestAllowedDevicesAggregation (0.00s)
    e2e_test.go: file does not exist
--- FAIL: TestFourLayerBoundaryConditions (0.00s)
    e2e_test.go: file does not exist
FAIL
```

**Summary:**

- Total tests: 5 functions (~24 subtests)
- Passing: 0 (expected)
- Failing: All (expected -- test file does not exist yet)
- Status: RED phase verified

**Expected Failure Reason:** `kernel/e2e_test.go` 尚未创建，所有测试函数不存在。

---

## Notes

- 本 Story 是 Epic 9 的最后一个 Story，纯验证性质，不引入任何生产代码
- 所有测试放在 `kernel/` 包内以访问非导出字段（Process.mu, AllowedDevices 等）
- Mock 组件必须线程安全（sync.Mutex 保护状态），以通过 `go test -race`
- DebugChan 在 NewProcess 中已默认创建（256 缓冲），无需额外设置
- 复用 `kernel/spawn_mcp_test.go` 中的 `spawnMockMountManager` 和 `containsSyscallError`
- 复用 `kernel/kernel_test.go` 中的 `mockLLMFile`、`mockToolFile` 模式
- MCP Mount 失败回滚测试可参考现有 `TestSpawn_AutoMountMCP/spawn_mount_failure_rolls_back`

---

## Contact

**Questions or Issues?**

- Refer to story definition: `_bmad-output/implementation-artifacts/9-4-four-layer-capability-stack-e2e-validation.md`
- Existing test patterns: `kernel/kernel_test.go`, `kernel/spawn_mcp_test.go`
- Architecture docs: `_bmad-output/planning-artifacts/architecture/`

---

**Generated by BMad TEA Agent** - 2026-03-02
