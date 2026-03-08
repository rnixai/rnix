---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-08'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/15-4-context-usage-analysis.md'
  - '_bmad-output/implementation-artifacts/15-3-trace-blame-root-cause-analysis.md'
  - '_bmad-output/implementation-artifacts/15-1-trace-id-generation-and-span-recording.md'
  - '_bmad/tea/config.yaml'
  - 'debug/ctx_profile.go'
  - 'cmd/rnix/ctx_profile.go'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'ipc/client.go'
---

# ATDD Checklist - Epic 15, Story 4: Context Usage Analysis (ctx-profile)

**Date:** 2026-03-08
**Author:** Decker
**Primary Test Level:** Unit + Integration (Backend Go)

---

## Step 1: Preflight & Context Loading

### Stack Detection
- **Detected Stack:** `backend` (Go 1.26, go.mod detected, no frontend indicators)
- **Test Framework:** Go standard `testing` package with `go test -race`
- **Test Stack Type:** auto -> resolved to `backend`

### Prerequisites Verified
- Story 15-4 approved with 4 clear acceptance criteria (AC #1-4)
- Story 15-1 completed: SpanReader, SpanWriter, Span types (patterns for debug package)
- Story 15-3 completed: BlameResult format patterns, FormatBlameResult 文本符号风格
- Story 13-3 completed: context.Manager.GetContextInfo, CtxRead, ProcInfo
- Test framework configured: Go `testing` + existing `*_test.go` patterns across 19+ packages

### Story Context Loaded
- **Story File:** `_bmad-output/implementation-artifacts/15-4-context-usage-analysis.md`
- **Acceptance Criteria:** 4 ACs covering context classification, top consumers/suggestions, error handling, JSON output
- **Affected Components:** `debug/` (new ctx_profile.go), `cmd/rnix/` (new ctx_profile.go), `ipc/` (protocol, server, client)
- **Dependencies:** context.Manager.CtxRead, kernel.GetProcInfo, IPC 非流式请求模式

### Framework & Existing Patterns
- Existing test patterns in `debug/trace_blame_test.go` (makeBlameSpan, buildBlameTree, table-driven)
- Existing CLI test patterns in `cmd/rnix/trace_test.go` (Cobra 执行、setupTraceTestDir)
- Existing IPC test patterns in `ipc/server_test.go` (setupTestServer, dial, sendRequest, AddProcess)
- Test pattern: Go table-driven tests, `t.TempDir()` for filesystem, `t.Helper()`, `-race` detector

---

## Step 2: Generation Mode

- **Mode:** AI Generation (backend Go project, no browser recording needed)
- **Reason:** All acceptance criteria involve backend Go code (debug/ctx_profile, ipc handleCtxProfile, cmd/rnix ctx-profile CLI)

---

## Step 3: Test Strategy

### Acceptance Criteria -> Test Mapping

| AC | Description | Test Level | Priority |
|---|---|---|---|
| AC#1 | Running/Zombie 智能体 → ctx-profile 分类 active/warm/cold/leaked，延迟 ≤ 1s | Unit (debug) + IPC (server) + Integration (CLI) | P0 |
| AC#2 | 识别最大 token 消费者，给出优化建议 | Unit (debug) | P0 |
| AC#3 | 无效 PID 或错误进程状态 → 友好错误信息 | IPC (server) + Integration (CLI) | P0 |
| AC#4 | `--json` 标志 → JSON 格式输出 | Unit (debug MarshalJSON) + Integration (CLI) | P0 |

### Test Level Allocation

| Level | Count | Coverage Focus |
|---|---|---|
| Unit Tests (debug/ctx_profile_test.go) | ~12 | AnalyzeContext、classification 逻辑、TopConsumers、Suggestions、FormatCtxProfile、MarshalJSON |
| IPC Handler Tests (ipc/server_test.go) | ~4 | handleCtxProfile：有效 PID、无效 PID、错误状态、上下文读取 |
| CLI Integration Tests (cmd/rnix/ctx_profile_test.go) | ~4 | 正常输出、JSON、错误处理、daemon 不可用 |
| **Total** | **~20** | |

---

## Step 4: Failing Tests (RED Phase)

### Unit Tests — debug/ctx_profile_test.go

**File:** `debug/ctx_profile_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 1 | `TestAnalyzeContext_EmptyContext` | #1 | P0 | 空上下文：Classification 全 0，TopConsumers 空，Suggestions 空 |
| 2 | `TestAnalyzeContext_SystemPromptOnly` | #1 | P0 | 仅 system prompt：Active 含 system，Cold 空 |
| 3 | `TestAnalyzeContext_TenMessages_Classification` | #1 | P0 | 10 条消息：Active=最后 4，Warm=5～10，Cold=1～4 |
| 4 | `TestAnalyzeContext_LeakedInColdZone` | #1 | P0 | 大工具结果（>1000 chars）在 Cold 区 → Leaked 非空 |
| 5 | `TestAnalyzeContext_LeakedExcludedInActiveWarm` | #1 | P0 | 大工具结果在 Active/Warm 区 → 不计入 Leaked |
| 6 | `TestAnalyzeContext_TopConsumers_Ranking` | #2 | P0 | TopConsumers 按 tokens 降序，Rank 正确 |
| 7 | `TestAnalyzeContext_TopConsumers_ToolNameExtraction` | #2 | P0 | tool 名称提取（ToolCallID 或 content 模式） |
| 8 | `TestAnalyzeContext_Suggestions_SystemPrompt` | #2 | P0 | system prompt > 25% total → "Consider trimming system prompt" |
| 9 | `TestAnalyzeContext_Suggestions_ToolDominant` | #2 | P0 | tool results > 50% total → 对应建议 |
| 10 | `TestAnalyzeContext_Suggestions_LeakedAndCold` | #2 | P0 | leaked > 0、cold > 40% 触发对应建议 |
| 11 | `TestFormatCtxProfile_WithSuggestions` | #1, #2 | P0 | 输出含 "Classification"、"Top Consumers"、"Suggestions" 段落，← 标注 |
| 12 | `TestFormatCtxProfile_NoSuggestions` | #1 | P0 | 无 Suggestions 时省略该段落 |
| 13 | `TestCtxProfileResult_MarshalJSON` | #4 | P0 | snake_case、pct 一位小数、classification 结构正确 |

### IPC Handler Tests — ipc/server_test.go

**File:** `ipc/server_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 14 | `TestServer_CtxProfile_ValidPID_Running` | #1 | P0 | 有效 PID + Running → OK，result 非空，含 Classification |
| 15 | `TestServer_CtxProfile_InvalidPID` | #3 | P0 | 无效 PID → NOT_FOUND |
| 16 | `TestServer_CtxProfile_WrongState` | #3 | P0 | 非 Running/Zombie 状态（如 Dead）→ INVALID |
| 17 | `TestServer_CtxProfile_ContextRead` | #1 | P0 | 验证 CtxRead 数据正确解析，TopConsumers 非空 |

### CLI Integration Tests — cmd/rnix/ctx_profile_test.go

**File:** `cmd/rnix/ctx_profile_test.go`

| # | Test Name | AC | Priority | Verifies |
|---|-----------|----|----|----------|
| 18 | `TestCtxProfileCmd_ValidPID` | #1, #2 | P0 | `ctx-profile <valid-pid>` → 输出含 "Classification" 和 "Top Consumers" |
| 19 | `TestCtxProfileCmd_InvalidPID` | #3 | P0 | `ctx-profile <invalid-pid>` → 友好错误信息 |
| 20 | `TestCtxProfileCmd_JSON` | #4 | P0 | `ctx-profile <pid> --json` → JSON 输出（JSONResponse 包装） |
| 21 | `TestCtxProfileCmd_DaemonUnavailable` | #3 | P0 | daemon 不可用时输出明确错误 |

---

## Fixtures & Helpers

### Context Test Helpers

**位置:** `debug/ctx_profile_test.go` 内部

- `makeContextData(systemPrompt string, messages ...ctxMessage) *contextData` — 创建测试用 contextData
- `makeCtxMessage(role, content, toolCallID string) ctxMessage` — 创建单条消息
- `estimateTokens(s string) int` — chars/4 估算（与 AnalyzeContext 一致）

### IPC Test Helpers

**位置:** `ipc/server_test.go` 内部

- 复用 `setupTestServer`（已含 ctxMgr）
- `setupCtxProfileProc(t, srv, ctxMgr, pid types.PID, state types.ProcessState, systemPrompt string, messages []ctxMessage) *kernel.Process` — 创建带 CtxID 和上下文的进程：CtxAlloc → SetSystemPrompt → AppendMessage → proc.CtxID = cid → proc 设置 State/TokensUsed/ContextBudget → AddProcess

### CLI Test Helpers

**位置:** `cmd/rnix/ctx_profile_test.go` 内部

- `setupCtxProfileTestServer(t) (sockPath string, kern *kernel.KernelImpl, ctxMgr *rnixctx.Manager)` — 创建 IPC server + SetContextManager + 返回 kern/ctxMgr 便于创建进程
- 复用 Cobra 命令执行模式（SetOut、SetArgs、Execute）
- `ipc.SocketPathOverride` 指向测试 socket

---

## Mock Requirements

### 无外部服务 Mock

本 Story 不涉及外部 LLM 或网络服务。所有操作通过 IPC 连接 daemon，daemon 使用 kernel + context.Manager。

### Test 进程与上下文

- 使用 `ctxMgr.CtxAlloc` 分配上下文
- 使用 `ctxMgr.SetSystemPrompt`、`ctxMgr.AppendMessage` 填充测试数据
- 使用 `kernel.NewProcess` + 手动设置 CtxID、State、TokensUsed、ContextBudget
- 使用 `kern.AddProcess(proc)` 注册进程

---

## Implementation Checklist

### Phase 1: 数据类型定义 (Tests 1, 2, 13)

- [ ] 在 `debug/ctx_profile.go` 中定义 CtxProfileResult、ClassificationResult、ClassBucket、ConsumerEntry 类型
- [ ] 定义 contextData、ctxMessage 内部结构
- [ ] 定义 AnalyzeContext 函数签名
- [ ] Run: `go test -race ./debug/ -run TestAnalyzeContext` (should compile but tests fail)
- [ ] ✅ Types compile correctly

### Phase 2: 分类逻辑 (Tests 1, 2, 3, 4, 5)

- [ ] 实现 Active = system_prompt + messages[len-4:len]
- [ ] 实现 Warm = messages[len-10:len-4]
- [ ] 实现 Cold = messages[0:len-10]
- [ ] 实现 Leaked = Cold 区内 role=tool 且 len(content)>1000
- [ ] token 估算：chars/4
- [ ] Run: `go test -race ./debug/ -run TestAnalyzeContext`
- [ ] ✅ Tests 1-5 pass

### Phase 3: TopConsumers 与 Suggestions (Tests 6-10)

- [ ] 实现 `findTopConsumers(data *contextData, totalTokens int, topN int) []ConsumerEntry`
- [ ] 实现 tool 名称提取启发式
- [ ] 实现 `generateSuggestions(result *CtxProfileResult) []string`
- [ ] Run: `go test -race ./debug/ -run TestAnalyzeContext`
- [ ] ✅ Tests 6-10 pass

### Phase 4: AnalyzeContext 集成 (Tests 1-10)

- [ ] 实现 `AnalyzeContext(data *contextData, pid types.PID, ctxID types.CtxID, tokensUsed, contextBudget int) *CtxProfileResult`
- [ ] 调用分类逻辑 + findTopConsumers + generateSuggestions
- [ ] Run: `go test -race ./debug/ -run TestAnalyzeContext`
- [ ] ✅ Tests 1-10 pass

### Phase 5: FormatCtxProfile (Tests 11-12)

- [ ] 实现 `FormatCtxProfile(result *CtxProfileResult) string`
- [ ] 段落：Classification（← 标注）、Top Consumers（#N 排名）、Suggestions（• 列表）
- [ ] 无 Suggestions 时省略该段落
- [ ] Run: `go test -race ./debug/ -run TestFormatCtxProfile`
- [ ] ✅ Tests 11-12 pass

### Phase 6: MarshalJSON (Test 13)

- [ ] 为 CtxProfileResult 实现 MarshalJSON（snake_case、pct 一位小数）
- [ ] Run: `go test -race ./debug/ -run TestCtxProfileResult_MarshalJSON`
- [ ] ✅ Test 13 pass

### Phase 7: IPC 协议与 Handler (Tests 14-17)

- [ ] 在 `ipc/protocol.go` 新增 MethodCtxProfile、CtxProfileRequest
- [ ] 在 `ipc/server.go` handleConn 增加 case MethodCtxProfile
- [ ] 实现 `handleCtxProfile(conn net.Conn, rawPayload json.RawMessage)`
- [ ] 校验 GetProcInfo、State Running/Zombie、CtxRead、AnalyzeContext
- [ ] 分析过程加 context.WithTimeout 1s（NFR34）
- [ ] 在 `ipc/client.go` 实现 `CtxProfile(pid types.PID) (*debug.CtxProfileResult, error)`
- [ ] Run: `go test -race ./ipc/ -run TestServer_CtxProfile`
- [ ] ✅ Tests 14-17 pass

### Phase 8: CLI ctx-profile 命令 (Tests 18-21)

- [ ] 在 `cmd/rnix/ctx_profile.go` 定义 ctxProfileCmd
- [ ] 实现 `runCtxProfile(cmd *cobra.Command, args []string) error`
- [ ] 在 `cmd/rnix/main.go` init 中 `rootCmd.AddCommand(ctxProfileCmd)`
- [ ] 支持 --json 全局标志
- [ ] daemon 不可用时输出明确错误
- [ ] Run: `go test -race ./cmd/rnix/ -run TestCtxProfileCmd`
- [ ] ✅ Tests 18-21 pass

---

## Running Tests

```bash
# Run all tests for story 15-4 (affected packages)
go test -race -v ./debug/ ./ipc/ ./cmd/rnix/ -run "TestAnalyzeContext|TestFormatCtxProfile|TestCtxProfileResult_MarshalJSON|TestServer_CtxProfile|TestCtxProfileCmd"

# Run ctx-profile unit tests
go test -race -v ./debug/ -run "TestAnalyzeContext|TestFormatCtxProfile|TestCtxProfileResult"

# Run IPC handler tests
go test -race -v ./ipc/ -run TestServer_CtxProfile

# Run CLI integration tests
go test -race -v ./cmd/rnix/ -run TestCtxProfileCmd

# Run ALL project tests (regression check)
go test -race ./...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 21 tests designed and specified
- ✅ Test strategy mapped to all 4 acceptance criteria
- ✅ Implementation checklist created with 8-phase approach
- ✅ Tests designed to fail before implementation (functions/types don't exist yet)

**Verification:**

- All tests reference types and functions that don't exist yet (debug.CtxProfileResult, debug.AnalyzeContext, debug.FormatCtxProfile, MethodCtxProfile, handleCtxProfile, ctxProfileCmd, etc.)
- Tests fail with compilation errors until implementation

---

### GREEN Phase (DEV Team)

1. Implement Phase 1 (types) → Types compile
2. Implement Phase 2 (classification) → Tests 1-5 pass
3. Implement Phase 3 (TopConsumers, Suggestions) → Tests 6-10 pass
4. Implement Phase 4 (AnalyzeContext) → Tests 1-10 pass
5. Implement Phase 5 (FormatCtxProfile) → Tests 11-12 pass
6. Implement Phase 6 (MarshalJSON) → Test 13 pass
7. Implement Phase 7 (IPC) → Tests 14-17 pass
8. Implement Phase 8 (CLI) → Tests 18-21 pass
9. Run full suite: `go test -race ./...` → All packages pass

---

## Validation

- [x] Prerequisites satisfied (story approved, 15-1/15-3/13-3 patterns available, test framework configured)
- [x] Test strategy maps to all 4 acceptance criteria
- [x] Tests cover positive, negative, and edge cases
- [x] Tests designed to fail before implementation
- [x] IPC tests require setup with ctxMgr + process with CtxID
- [x] Implementation checklist covers all 6 tasks from story
- [x] Temp artifacts stored in `_bmad-output/test-artifacts/`

---

## Notes

- ctx-profile 是顶级命令（rootCmd.AddCommand），非 trace 子命令
- 分类规则：Active=最后 4 条，Warm=5～10，Cold=更早，Leaked=Cold 区内大工具结果
- FormatCtxProfile 使用纯文本符号（←、#、•），放在 debug 包中
- handleCtxProfile 需 context.WithTimeout 1s 满足 NFR34
- CLI 测试需 setup 含 SetContextManager；可复用 main_test 的 setupTestIPCServer 模式并补充 SetContextManager
- 进程需 CtxID、State、TokensUsed、ContextBudget；通过 CtxAlloc + AppendMessage 预填上下文

---

**Generated by BMad TEA Agent** - 2026-03-08
