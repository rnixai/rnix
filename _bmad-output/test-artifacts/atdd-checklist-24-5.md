---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-13'
workflowType: testarch-atdd
inputDocuments:
  - _bmad-output/implementation-artifacts/24-5-rnix-serve-cli-e2e-integration.md
  - _bmad/tea/testarch/knowledge/test-quality.md
  - _bmad/tea/testarch/knowledge/test-levels-framework.md
  - _bmad/tea/testarch/knowledge/test-healing-patterns.md
---

# ATDD Checklist - Epic 24, Story 5: rnix serve CLI 命令与端到端集成

**Date:** 2026-03-13
**Author:** Decker
**Primary Test Level:** Unit (Go `testing.T`)

---

## Story Summary

通过 `rnix serve` Cobra 子命令一键启动 OpenAI 兼容 HTTP 网关，让所有支持 OpenAI API 的外部工具通过 Rnix 统一消费 LLM 能力。

**As a** 用户
**I want** 通过 `rnix serve` 命令一键启动 OpenAI 兼容网关
**So that** 我可以让所有支持 OpenAI API 的外部工具通过 Rnix 统一消费 LLM 能力

---

## Acceptance Criteria

1. `rnix serve --port 8080` 启动 OpenAI 兼容 HTTP 服务器监听 `127.0.0.1:8080`，终端输出 `Serving N providers on http://127.0.0.1:8080`
2. `--port` 默认 8080，支持自定义端口（如 `--port 9090`）
3. 读取 `rnix-providers.yaml` 配置注册驱动并启动 HTTP 服务器
4. 10 个并发 HTTP 请求全部正常处理
5. Python `openai` 库可通过 `base_url="http://localhost:8080/v1"` 正常调用
6. SIGTERM/SIGINT 触发优雅关闭（5 秒超时）

---

## Failing Tests Created (RED Phase)

### Unit Tests (5 tests)

**File:** `cmd/rnix/serve_test.go` (136 lines)

- **Test:** `TestServeCmd_Exists`
  - **Status:** RED — `expected 'serve' subcommand registered on rootCmd`
  - **Verifies:** AC#1 — serve 子命令已注册到 rootCmd
  - **Priority:** P0

- **Test:** `TestServeCmd_DefaultPort`
  - **Status:** RED — `serve command not found`
  - **Verifies:** AC#2 — `--port` flag 默认值为 8080
  - **Priority:** P0

- **Test:** `TestServeCmd_CustomPort`
  - **Status:** RED — `serve command not found`
  - **Verifies:** AC#2 — `--port 9090` 可正确解析
  - **Priority:** P1

- **Test:** `TestServeCmd_HelpOutput`
  - **Status:** RED — `serve command not found`
  - **Verifies:** AC#1 — help 输出包含 "OpenAI" 和 "--port"
  - **Priority:** P1

- **Test:** `TestServeCmd_HasRunE`
  - **Status:** RED — `serve command not found`
  - **Verifies:** AC#1 — 命令具有可执行的 RunE 函数
  - **Priority:** P1

---

## 已有测试覆盖（不重复测试）

以下 AC 由 Stories 24-1 ~ 24-4 的测试全面覆盖，本 Story 不新增重复测试：

| AC | 覆盖文件 | 测试数量 |
|----|----------|----------|
| AC#3 (配置加载) | `ipc/http_openai_test.go` | /health, /v1/models 端点测试 |
| AC#4 (并发请求) | `ipc/http_openai_test.go` | 并发安全测试 |
| AC#5 (OpenAI 客户端) | 手工验证 | N/A |
| AC#6 (优雅关闭) | `ipc/http_openai_test.go` | Shutdown 方法测试 |

---

## Data Factories Created

N/A — Go 后端项目使用 `testing.T` + helper 函数模式，无需 faker/factory 库。

**Helper 函数：**

- `findServeCmd(t *testing.T) *cobra.Command` — 在 rootCmd 中查找 serve 子命令

---

## Fixtures Created

N/A — Go 测试使用标准 `testing.T` 框架，无 Playwright fixture 需求。

---

## Mock Requirements

N/A — 单元测试仅验证 Cobra 命令注册和 Flag 解析，不需要 Mock 外部服务。HTTP 端点行为已由 `ipc/http_openai_test.go` 中的 `stubDriver` 和 `configurableDriver` 覆盖。

---

## Required data-testid Attributes

N/A — 纯后端 Go 项目，无 UI 组件。

---

## Implementation Checklist

### Test: TestServeCmd_Exists

**File:** `cmd/rnix/serve_test.go`

**Tasks to make this test pass:**

- [ ] 新建 `cmd/rnix/serve.go`
- [ ] 定义 `var serveCmd = &cobra.Command{Use: "serve", ...}`
- [ ] 在 `cmd/rnix/main.go` 的 `init()` 中添加 `rootCmd.AddCommand(serveCmd)`
- [ ] Run test: `go test -race -run TestServeCmd_Exists ./cmd/rnix/...`
- [ ] Test passes (green phase)

---

### Test: TestServeCmd_DefaultPort

**File:** `cmd/rnix/serve_test.go`

**Tasks to make this test pass:**

- [ ] 在 `serve.go` 的 `init()` 中添加 `serveCmd.Flags().IntVar(&flagServePort, "port", 8080, "HTTP listen port")`
- [ ] Run test: `go test -race -run TestServeCmd_DefaultPort ./cmd/rnix/...`
- [ ] Test passes (green phase)

---

### Test: TestServeCmd_CustomPort

**File:** `cmd/rnix/serve_test.go`

**Tasks to make this test pass:**

- [ ] `--port` flag 已注册（依赖 TestServeCmd_DefaultPort 的实现）
- [ ] Run test: `go test -race -run TestServeCmd_CustomPort ./cmd/rnix/...`
- [ ] Test passes (green phase)

---

### Test: TestServeCmd_HelpOutput

**File:** `cmd/rnix/serve_test.go`

**Tasks to make this test pass:**

- [ ] `serveCmd.Short` 包含 "OpenAI"（如 "Start OpenAI-compatible HTTP gateway"）
- [ ] `--port` flag 已注册
- [ ] Run test: `go test -race -run TestServeCmd_HelpOutput ./cmd/rnix/...`
- [ ] Test passes (green phase)

---

### Test: TestServeCmd_HasRunE

**File:** `cmd/rnix/serve_test.go`

**Tasks to make this test pass:**

- [ ] 实现 `func runServe(cmd *cobra.Command, args []string) error`
- [ ] 设置 `serveCmd.RunE = runServe`
- [ ] `runServe` 实现完整的服务器启动逻辑：
  - [ ] 调用 `llm.LoadOrDefaultProvidersConfig()`
  - [ ] 创建 `llm.NewDriverRegistry()` + `vfs.NewDeviceRegistry()`
  - [ ] 调用 `llm.RegisterProviders()` 和 `llm.RunHealthChecks()`
  - [ ] 创建 `ipc.NewOpenAIServer(driverReg, addr)`
  - [ ] 输出 `Serving N providers on http://127.0.0.1:PORT` 到 stderr
  - [ ] goroutine 中 `ListenAndServe()`
  - [ ] 信号监听 SIGINT/SIGTERM
  - [ ] 5 秒超时优雅关闭
- [ ] Run test: `go test -race -run TestServeCmd_HasRunE ./cmd/rnix/...`
- [ ] Test passes (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story
go test -race -run "TestServeCmd|TestServe_" ./cmd/rnix/... -v

# Run specific test
go test -race -run TestServeCmd_Exists ./cmd/rnix/... -v

# Run all package tests (includes existing tests)
go test -race ./cmd/rnix/...

# Run with coverage
go test -race -cover -run "TestServeCmd|TestServe_" ./cmd/rnix/...

# Run full project test suite
make test
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All tests written and failing
- Test helper `findServeCmd()` created
- Implementation checklist created
- Verification: 5 tests fail with `serve command not found`

**Verification:**

```
=== RUN   TestServeCmd_Exists
    serve_test.go:41: expected 'serve' subcommand registered on rootCmd
--- FAIL: TestServeCmd_Exists (0.00s)
=== RUN   TestServeCmd_DefaultPort
    serve_test.go:56: serve command not found
--- FAIL: TestServeCmd_DefaultPort (0.00s)
=== RUN   TestServeCmd_CustomPort
    serve_test.go:76: serve command not found
--- FAIL: TestServeCmd_CustomPort (0.00s)
=== RUN   TestServeCmd_HelpOutput
    serve_test.go:99: serve command not found
--- FAIL: TestServeCmd_HelpOutput (0.00s)
=== RUN   TestServeCmd_HasRunE
    serve_test.go:128: serve command not found
--- FAIL: TestServeCmd_HasRunE (0.00s)
FAIL
```

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Pick one failing test** from implementation checklist (start with TestServeCmd_Exists)
2. **Read the test** to understand expected behavior
3. **Implement minimal code** to make that specific test pass
4. **Run the test** to verify it now passes (green)
5. **Check off the task** in implementation checklist
6. **Move to next test** and repeat

**Key Principles:**

- One test at a time (don't try to fix all at once)
- Minimal implementation (don't over-engineer)
- Run tests frequently (immediate feedback)
- Use implementation checklist as roadmap

**Recommended implementation order:**

1. `TestServeCmd_Exists` → create `serve.go` with `serveCmd` + register
2. `TestServeCmd_DefaultPort` → add `--port` flag
3. `TestServeCmd_CustomPort` → already passes after step 2
4. `TestServeCmd_HelpOutput` → set `Short` description with "OpenAI"
5. `TestServeCmd_HasRunE` → implement `runServe` function

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all tests pass: `go test -race -run "TestServeCmd" ./cmd/rnix/... -v`
2. Run `make all` (lint + vet + test + build)
3. Review code quality
4. Ensure no duplicate code with `runDaemon` provider init sequence

---

## Next Steps

1. **运行失败测试** 确认 RED 阶段: `go test -race -run "TestServeCmd" ./cmd/rnix/... -v`
2. **开始实现** 按 implementation checklist 逐个完成
3. **每个测试通过后** 运行完整测试套件验证无回归
4. **全部通过后** 运行 `make all` 验证项目完整性
5. **手工验证** AC#5 (Python OpenAI client) 和 AC#6 (SIGTERM graceful shutdown)
6. **完成后** 更新 sprint-status.yaml 中 Story 24.5 状态

---

## Knowledge Base References Applied

- **test-quality.md** — 确定性测试、隔离性、显式断言原则
- **test-levels-framework.md** — 测试级别选择（后端项目优先 Unit + Integration）
- **test-healing-patterns.md** — 常见失败模式识别（此 Story 为纯后端，不涉及 UI 测试修复模式）

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -race -run "TestServeCmd|TestServe_" ./cmd/rnix/... -v`

**Results:**

```
=== RUN   TestServeCmd_Exists
    serve_test.go:41: expected 'serve' subcommand registered on rootCmd
--- FAIL: TestServeCmd_Exists (0.00s)
=== RUN   TestServeCmd_DefaultPort
    serve_test.go:56: serve command not found
--- FAIL: TestServeCmd_DefaultPort (0.00s)
=== RUN   TestServeCmd_CustomPort
    serve_test.go:76: serve command not found
--- FAIL: TestServeCmd_CustomPort (0.00s)
=== RUN   TestServeCmd_HelpOutput
    serve_test.go:99: serve command not found
--- FAIL: TestServeCmd_HelpOutput (0.00s)
=== RUN   TestServeCmd_HasRunE
    serve_test.go:128: serve command not found
--- FAIL: TestServeCmd_HasRunE (0.00s)
FAIL    github.com/rnixai/rnix/cmd/rnix    0.019s
```

**Summary:**

- Total tests: 5
- Passing: 0 (expected)
- Failing: 5 (expected)
- Status: RED phase verified

---

## Notes

- 本 Story 变更范围极小：新增 `serve.go` (~60 行) + 修改 `main.go` (+1 行)
- HTTP 端点行为由 Stories 24-1~24-4 的 70+ 测试完整覆盖，不重复测试
- `runServe` 复用 `runDaemon` 的完整 provider 初始化序列
- 安全绑定 `127.0.0.1`（不暴露外部网络），由 `OpenAIServer` 层保障

---

**Generated by BMad TEA Agent** — 2026-03-13
