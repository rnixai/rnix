---
stepsCompleted: ['step-01', 'step-02', 'step-03', 'step-04', 'step-05', 'step-06']
lastStep: 'step-06'
lastSaved: '2026-03-03'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/12-2-architecture-documentation.md'
  - 'docs/concepts.md'
  - 'docs/reference.md'
---

# ATDD Checklist - Epic 12, Story 2: 架构文档（Architecture Documentation）

**Date:** 2026-03-03
**Author:** Decker
**Primary Test Level:** Unit（文档验证测试）

---

## Story Summary

为 Crux 创建面向贡献者的架构文档，包含四个核心章节：微内核设计、进程模型、驱动层、上下文管理。文档必须精确引用代码接口签名和设计决策，帮助贡献者理解内部实现。

**As a** 贡献者
**I want** 阅读架构文档理解 Crux 的内部设计
**So that** 我可以参与内核开发和 Skill 生态贡献

---

## Acceptance Criteria

1. **AC1: 微内核设计章节** — 包含 Kernel 接口组合设计、分类子接口职责、扩展路径的设计决策和数据流说明
2. **AC2: 进程模型章节** — 包含 Process 结构体设计、状态机转移规则、PID 分配策略、goroutine 生命周期管理
3. **AC3: 驱动层章节** — 包含 LLMDriver 接口、VFS 设备注册、MCP 挂载机制
4. **AC4: 上下文管理章节** — 包含上下文分配/读写/释放、prompt 组装、token 预算管理

---

## Test Strategy

### Documentation Story 特殊说明

本 Story 为纯文档 Story（无 Go 代码实现变更），测试策略与 Story 12-1 一致：

1. **文件存在性测试** — 验证架构文档文件已创建且路径正确
2. **内容完整性测试** — 验证每个章节包含必需的技术内容
3. **技术准确性测试** — 验证接口签名、结构体字段、VFS 路径与代码实现一致
4. **交叉引用测试** — 验证指向其他文档的链接正确
5. **回归测试** — 验证现有文档和 Story 12-1 测试未被破坏

### 测试框架

扩展现有 `docs/docs_test.go`，使用 Go 标准 `testing` 包。

---

## Failing Tests Created (RED Phase)

### Unit Tests (13 tests)

**File:** `docs/docs_test.go`

#### 文件存在性测试（1 test）

- ✅ **Test:** `TestArchitectureDoc_Exists`
  - **Status:** RED - 架构文档文件不存在
  - **Verifies:** `docs/architecture.md` 文件存在

#### AC1: 微内核设计章节（3 tests）

- ✅ **Test:** `TestArchitecture_MicrokernelDesign_HasSubInterfaces`
  - **Status:** RED - 文件不存在
  - **Verifies:** 文档包含 6 个分类子接口（ProcessManager、MountManager、IPCManager、SignalManager、ProcGroupManager、SupervisorManager）

- ✅ **Test:** `TestArchitecture_MicrokernelDesign_HasCallbacks`
  - **Status:** RED - 文件不存在
  - **Verifies:** 文档包含 KernelCallbacks 回调机制（OnSpawn、OnStep、OnComplete、OnError）

- ✅ **Test:** `TestArchitecture_MicrokernelDesign_HasDataFlow`
  - **Status:** RED - 文件不存在
  - **Verifies:** 文档包含数据流说明（Spawn、reasonStep、VFS 读写相关内容）

#### AC2: 进程模型章节（3 tests）

- ✅ **Test:** `TestArchitecture_ProcessModel_HasStateMachine`
  - **Status:** RED - 文件不存在
  - **Verifies:** 文档包含状态机（Created、Running、Zombie、Dead）和 PID 分配策略

- ✅ **Test:** `TestArchitecture_ProcessModel_HasReapSequence`
  - **Status:** RED - 文件不存在
  - **Verifies:** 文档包含资源释放顺序（reapProcess、CtxFree、orphan/reparent）

- ✅ **Test:** `TestArchitecture_ProcessModel_HasConcurrencyModel`
  - **Status:** RED - 文件不存在
  - **Verifies:** 文档包含三级并发模型（Thread、Coroutine、goroutine）

#### AC3: 驱动层章节（3 tests）

- ✅ **Test:** `TestArchitecture_DriverLayer_HasDeviceRegistry`
  - **Status:** RED - 文件不存在
  - **Verifies:** 文档包含 DeviceRegistry、VFSFileFactory、VFSFile 接口说明

- ✅ **Test:** `TestArchitecture_DriverLayer_HasLLMDriver`
  - **Status:** RED - 文件不存在
  - **Verifies:** 文档包含 LLMDriver 接口（Call、Stream、LLMRequest、LLMResponse）

- ✅ **Test:** `TestArchitecture_DriverLayer_HasMCPMount`
  - **Status:** RED - 文件不存在
  - **Verifies:** 文档包含 MCP 挂载机制（MCPTransport、Mount、Unmount、/mnt/mcp/）

#### AC4: 上下文管理章节（2 tests）

- ✅ **Test:** `TestArchitecture_ContextMgmt_HasManagerMethods`
  - **Status:** RED - 文件不存在
  - **Verifies:** 文档包含 Context 管理方法（CtxAlloc、CtxFree、BuildPrompt、AppendMessage）

- ✅ **Test:** `TestArchitecture_ContextMgmt_HasTokenBudget`
  - **Status:** RED - 文件不存在
  - **Verifies:** 文档包含 Token 预算管理（ContextBudget、TokensUsed、budget_exceeded）

#### 交叉引用测试（1 test）

- ✅ **Test:** `TestArchitecture_CrossReferences`
  - **Status:** RED - 文件不存在
  - **Verifies:** 架构文档引用 concepts.md、reference.md 和 tutorials/

---

## Data Factories Created

不适用（文档 Story 无需数据工厂）

---

## Fixtures Created

### 文档路径 Fixtures

**用法：** 测试文件中定义辅助函数引用架构文档路径

```go
func readArchDoc(t *testing.T) string {
    t.Helper()
    path := filepath.Join(docsDir(), "architecture.md")
    data, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("架构文档不存在: %s", path)
    }
    return string(data)
}
```

---

## Mock Requirements

不适用（文档 Story 无外部服务依赖）

---

## Implementation Checklist

### Test: TestArchitectureDoc_Exists

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 创建 `docs/architecture.md` 文件
- [ ] Run test: `go test ./docs/ -run TestArchitectureDoc_Exists -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestArchitecture_MicrokernelDesign_HasSubInterfaces

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写微内核设计章节——接口组合模式说明
- [ ] 列出 6 个分类子接口及其职责
- [ ] Run test: `go test ./docs/ -run TestArchitecture_MicrokernelDesign_HasSubInterfaces -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestArchitecture_MicrokernelDesign_HasCallbacks

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写 KernelCallbacks 回调机制说明
- [ ] 列出 OnSpawn、OnStep、OnComplete、OnError
- [ ] Run test: `go test ./docs/ -run TestArchitecture_MicrokernelDesign_HasCallbacks -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestArchitecture_MicrokernelDesign_HasDataFlow

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写数据流说明（spawn → reasonStep → VFS → 完成）
- [ ] Run test: `go test ./docs/ -run TestArchitecture_MicrokernelDesign_HasDataFlow -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestArchitecture_ProcessModel_HasStateMachine

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写状态机转移规则
- [ ] 编写 PID 分配策略
- [ ] Run test: `go test ./docs/ -run TestArchitecture_ProcessModel_HasStateMachine -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestArchitecture_ProcessModel_HasReapSequence

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写 reapProcess 资源释放顺序
- [ ] Run test: `go test ./docs/ -run TestArchitecture_ProcessModel_HasReapSequence -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestArchitecture_ProcessModel_HasConcurrencyModel

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写三级并发模型
- [ ] Run test: `go test ./docs/ -run TestArchitecture_ProcessModel_HasConcurrencyModel -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestArchitecture_DriverLayer_HasDeviceRegistry

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写 DeviceRegistry 注册机制
- [ ] 编写 VFSFile 接口说明
- [ ] Run test: `go test ./docs/ -run TestArchitecture_DriverLayer_HasDeviceRegistry -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestArchitecture_DriverLayer_HasLLMDriver

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写 LLMDriver 接口说明
- [ ] Run test: `go test ./docs/ -run TestArchitecture_DriverLayer_HasLLMDriver -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestArchitecture_DriverLayer_HasMCPMount

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写 MCP 挂载机制
- [ ] Run test: `go test ./docs/ -run TestArchitecture_DriverLayer_HasMCPMount -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestArchitecture_ContextMgmt_HasManagerMethods

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写 Context Manager 方法概述
- [ ] Run test: `go test ./docs/ -run TestArchitecture_ContextMgmt_HasManagerMethods -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestArchitecture_ContextMgmt_HasTokenBudget

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写 Token 预算管理
- [ ] Run test: `go test ./docs/ -run TestArchitecture_ContextMgmt_HasTokenBudget -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestArchitecture_CrossReferences

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 添加指向 concepts.md、reference.md 和 tutorials/ 的链接
- [ ] Run test: `go test ./docs/ -run TestArchitecture_CrossReferences -v`
- [ ] ✅ Test passes (green phase)

---

## Running Tests

```bash
# Run all architecture doc tests
go test ./docs/ -v -run "TestArchitecture"

# Run specific test
go test ./docs/ -v -run TestArchitectureDoc_Exists

# Run all docs tests (including Story 12-1 regression)
go test ./docs/ -v

# Run with race detection
go test ./docs/ -v -race -run "TestArchitecture"

# Run all tests to check for regressions
make test
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 13 tests written and failing (docs/docs_test.go)
- ✅ Test file structure created
- ✅ Implementation checklist created
- ✅ Documentation validation strategy defined

**Verification:**

- All tests run and fail as expected (architecture.md doesn't exist yet)
- Failure messages are clear and actionable
- Tests fail due to missing documentation, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **创建架构文档文件** — 通过 TestArchitectureDoc_Exists
2. **编写微内核设计章节** — 逐步通过 3 个测试
3. **编写进程模型章节** — 逐步通过 3 个测试
4. **编写驱动层章节** — 逐步通过 3 个测试
5. **编写上下文管理章节** — 逐步通过 2 个测试
6. **添加交叉引用** — 通过最后 1 个测试
7. **运行全部测试确认 GREEN**

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. 审读所有章节确保行文流畅连贯
2. 校验所有接口签名、结构体字段名与代码实现一致
3. 确保术语翻译一致（与 concepts.md、reference.md 统一）
4. 运行 `make test` 确认无回归

---

## Notes

- 本 Story 为文档类 Story，"实现"即编写 Markdown 文件，"测试"即验证文档内容完整性和准确性
- 测试通过读取文件内容并检查关键字符串/章节标题来验证
- 接口签名以源码为准：kernel/kernel.go、vfs/dev.go、drivers/llm/driver.go、context/manager.go
- 设计决策描述以 _bmad-output/planning-artifacts/architecture/ 中的 ADR 为准
- 所有文档使用简体中文

---

**Generated by BMad TEA Agent** - 2026-03-03
