# Story 1.8: 端到端集成与验收

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want 完整的端到端体验：`crux "意图"` → 实时进度 → 结果 → 汇总,
So that 我确认整个系统协同工作正常。

## Acceptance Criteria

1. **完整输出流** — Given 所有 Story 1.1-1.7 已完成，When 执行 `crux "分析 ./README.md"`，Then 看到完整输出流：`[kernel] spawning PID 1...` → `[agent/1] reasoning step N/M...` → `══ Result ══...══` → `[kernel] PID 1 exited(0) | tokens: N | elapsed: Ns`
2. **可靠性** — Given Claude Code CLI 已安装并可用，When 连续执行 20 次简单任务，Then 成功率 ≥ 95%（NFR6），And 简单任务端到端耗时 ≤ 30 秒（NFR1）
3. **超时处理** — Given LLM 调用超时，When 30 秒后超时触发，Then 进程在 5 秒内转为 Zombie（NFR7），And CLI 进程不崩溃（NFR10），And 显示三行错误结构
4. **进程表一致性** — Given 进程异常退出，When 查看内核进程表，Then 进程表保持一致性，无悬挂 PID、无状态不一致（NFR9）
5. **版本与依赖检测** — Given `crux version` 执行，When Claude Code CLI 未安装，Then 输出版本号并警告 `✗ claude-code CLI not found` + 安装建议
6. **帮助输出** — Given 执行 `crux --help`，When 查看帮助输出，Then 显示 Usage + 可用命令列表 + 全局 flags + 示例
7. **信号处理** — Given 用户按 Ctrl+C（首次），When 当前智能体执行中，Then 当前智能体优雅中断，进程转 zombie，输出中断摘要，And 2 秒内二次 Ctrl+C 强制退出

## Tasks / Subtasks

- [ ] Task 1: 增强 version 命令 — 检测 Claude Code CLI 可用性 (AC: #5)
  - [ ] 1.1 在 `cmd/crux/main.go` 的 `versionCmd` 中新增 Claude Code CLI 检测：执行 `claude --version` 检查是否可用
  - [ ] 1.2 可用时输出 `crux v0.1.0` + `claude-code: v{版本号}`
  - [ ] 1.3 不可用时输出 `crux v0.1.0` + `✗ claude-code CLI not found` + `→ 建议: npm install -g @anthropic-ai/claude-code`
  - [ ] 1.4 `--json` 模式下输出 JSON 格式的版本和依赖信息

- [ ] Task 2: 完善 help 输出 (AC: #6)
  - [ ] 2.1 确保 `crux --help` 显示 Usage、可用命令列表（version）、全局 flags（--json/--verbose/--quiet）
  - [ ] 2.2 在 rootCmd.Long 或 rootCmd.Example 中添加使用示例：`crux "分析 ./README.md"`

- [ ] Task 3: 编写 E2E 集成测试 — 完整输出流 (AC: #1)
  - [ ] 3.1 创建 `cmd/crux/integration_test.go`：使用 mock LLM 驱动的完整 E2E 测试
  - [ ] 3.2 `TestE2E_SuccessFlow`：mock LLM 返回 ActionText → 验证完整输出包含 `[kernel] spawning PID`、`reasoning step`、`══ Result ══`、`exited(0)`
  - [ ] 3.3 `TestE2E_ErrorFlow`：mock LLM 返回错误 → 验证输出包含 `✗` 错误块 + Summary Footer
  - [ ] 3.4 `TestE2E_JSONOutput`：`--json` 模式 → 验证输出为有效 JSON、包含 `ok`/`data`/`pid`/`result`/`tokens_used`/`elapsed_ms`
  - [ ] 3.5 `TestE2E_JSONErrorOutput`：`--json` 错误模式 → 验证 `ok:false` + `error` 结构

- [ ] Task 4: 超时处理集成测试 (AC: #3)
  - [ ] 4.1 `TestE2E_LLMTimeout`：mock LLM 在 Write 时 sleep 超过 timeout → 验证进程转 Zombie，CLI 不崩溃，输出三行错误结构
  - [ ] 4.2 `TestE2E_TimeoutProcessCleanup`：超时后验证进程 Done channel 被写入、ExitStatus.Code != 0、进程表中进程状态为 Zombie
  - [ ] 4.3 验证 NFR7：超时后 5 秒内进程转 Zombie

- [ ] Task 5: 进程表一致性测试 (AC: #4)
  - [ ] 5.1 `TestProcessTableConsistency_AfterNormalExit`：Spawn → 等待完成 → 验证进程表中进程存在且状态为 Zombie
  - [ ] 5.2 `TestProcessTableConsistency_AfterError`：Spawn → LLM 报错 → 验证进程表无悬挂 PID、状态一致
  - [ ] 5.3 `TestProcessTableConsistency_MultipleProcesses`：连续 Spawn 多个进程 → 全部完成 → 验证进程表中每个进程都有正确状态
  - [ ] 5.4 `TestProcessTableConsistency_ConcurrentSpawn`：并发 Spawn 多个进程 → 验证无 data race（-race 测试）

- [ ] Task 6: 信号处理集成测试 (AC: #7)
  - [ ] 6.1 `TestSignalHandling_GracefulShutdown`：Spawn 后发送 SIGINT → 验证进程被 Cancel、转 Zombie、输出中断摘要
  - [ ] 6.2 验证 `proc.Cancel()` 被调用后 reasonStep 检测到 ctx.Done() 并退出

- [ ] Task 7: version 命令测试 (AC: #5)
  - [ ] 7.1 `TestVersion_WithClaude`：mock exec 使 `claude --version` 成功 → 验证输出包含版本号
  - [ ] 7.2 `TestVersion_WithoutClaude`：mock exec 使 `claude --version` 失败 → 验证输出包含 `✗ claude-code CLI not found` + 安装建议
  - [ ] 7.3 `TestVersion_JSON`：`--json` 模式版本输出验证

- [ ] Task 8: help 输出测试 (AC: #6)
  - [ ] 8.1 `TestHelp_ContainsUsage`：验证 `crux --help` 输出包含 Usage、version 子命令、全局 flags
  - [ ] 8.2 `TestHelp_ContainsExample`：验证包含使用示例

- [ ] Task 9: 全量回归测试 (AC: all)
  - [ ] 9.1 `go test -race ./...` 全部通过
  - [ ] 9.2 无新增 golangci-lint 警告

## Dev Notes

### 架构模式与约束

- **此 Story 的核心本质：** 这是 Epic 1 的收官 Story，重点是**集成测试和验收**，不是新功能开发。主要工作量在编写全面的集成测试，以及少量的 version 命令增强
- **新增代码量少，测试代码量大：** 预计新增约 50 行业务代码（version 增强 + help 示例），300+ 行测试代码
- **文件位置严格遵循架构文档：** 测试文件与源文件同目录（Go 惯例）
- **依赖方向不变：** 本 Story 不引入新的包级依赖方向

### 已有代码（必须复用，禁止重新实现）

**`cmd/crux/main.go` — 当前已完整实现的功能：**

```go
// 已有的核心组件：
var rootCmd     // cobra 根命令，Use: "crux [intent]"，RunE: runRoot
var versionCmd  // cobra version 子命令，仅输出 fmt.Printf("crux v%s\n", version)
var flagJSON, flagVerbose, flagQuiet bool  // 全局 flags
type JSONResponse struct { OK bool; Data any; Error any }
type cliCallbacks struct { progress *ui.ProgressReporter }  // 实现 KernelCallbacks
func runRoot(cmd, args) error  // 依赖注入 + Spawn + Wait + 输出
func outputSuccess(...)  // JSON/普通模式成功输出
func outputError(...)    // JSON/普通模式错误输出
func main()              // rootCmd.Execute() + exitCode 处理
```

**`kernel/kernel.go` — Spawn + reasonStep 完整实现：**

```go
func NewKernel(v *vfs.VFS, ctxMgr *cruxctx.Manager, cb KernelCallbacks) *KernelImpl
func (k *KernelImpl) Spawn(intent string, skills []string, opts SpawnOpts) (PID, error)
func (k *KernelImpl) GetProcess(pid PID) (*Process, bool)
func (k *KernelImpl) reasonStep(proc *Process, llmFD types.FD, opts SpawnOpts)
func (k *KernelImpl) finishProcess(proc *Process, exit ExitStatus)
```

**`kernel/process.go` — Process 完整实现：**

```go
type Process struct { PID, PPID, State, Intent, Skills, FDTable, DebugChan, Done, CtxID, Result, TokensUsed, cancel, ctx, wg }
func NewProcess(ppid PID, intent string, skills []string) *Process
func (p *Process) Cancel()     // 导出的取消方法
func (p *Process) Terminate(exit ExitStatus) error  // Running → Zombie
func (p *Process) GetState() ProcessState
```

**`cmd/crux/main_test.go` — 已有 12 个测试：**

已有测试涵盖：runRoot 成功/失败、JSON 输出、无参数显示 help、outputError JSON 模式、exitCode 处理等。**本 Story 需要扩展此文件，而非覆盖已有测试。**

**`kernel/kernel_test.go` — 已有的 mock 基础设施：**

```go
// 已有的 mock 工具函数（禁止重新实现）：
func newTestKernel(respContent string, respTokens int) (*KernelImpl, *vfs.DeviceRegistry)
// 此函数创建：mock VFS + 真实 CtxMgr + mock LLM 设备 + nil callbacks
// 响应通过 mockLLMFile 固定返回
```

### version 命令增强设计

**当前 versionCmd（仅输出版本号）：**
```go
var versionCmd = &cobra.Command{
    Use:   "version",
    Short: "Show version and dependencies",
    Run: func(cmd *cobra.Command, args []string) {
        fmt.Printf("crux v%s\n", version)
    },
}
```

**增强后需要：**
```go
func runVersion(cmd *cobra.Command, args []string) {
    fmt.Printf("crux v%s\n", version)

    // 检测 Claude Code CLI
    out, err := exec.Command("claude", "--version").Output()
    if err != nil {
        // 使用 ErrorBlock 风格但简化
        fmt.Println("✗ claude-code CLI not found")
        fmt.Println("  → 建议: npm install -g @anthropic-ai/claude-code")
        return
    }
    fmt.Printf("claude-code: %s\n", strings.TrimSpace(string(out)))
}
```

**`--json` 模式版本输出：**
```json
{
  "ok": true,
  "data": {
    "version": "0.1.0",
    "claude_code": "1.x.x",
    "claude_code_available": true
  }
}
```

**测试方案：** Claude CLI 检测通过注入可替换的 command builder 来 mock（与 `drivers/llm/claude_cli.go` 的测试策略一致），或使用 `exec.LookPath` + 环境变量控制测试行为。

### E2E 集成测试设计

**核心方法：** 复用 `kernel_test.go` 中的 `newTestKernel` 模式，在 `cmd/crux/` 级别创建完整的 E2E 测试，使用 mock LLM 但真实的 Kernel + VFS + Context + UI 组件。

**测试架构：**
```go
// cmd/crux/integration_test.go

// testE2E 是 E2E 测试的辅助函数
// 创建完整的依赖注入链（mock LLM），捕获 stdout 输出
func testE2E(t *testing.T, intent string, mockResp string, mockTokens int, mode ui.OutputMode) (output string, exitCode int) {
    var buf bytes.Buffer
    renderer := ui.NewRenderer(&buf, mode)
    ui.InitStyles(renderer.Profile)  // 无色模式（非 TTY）
    progress := ui.NewProgressReporter(renderer)
    cb := &cliCallbacks{progress: progress}

    devReg := vfs.NewDeviceRegistry()
    vfsInst := vfs.NewVFS(devReg)
    // 注册 mock LLM 驱动（固定返回 mockResp）
    mockDriver := ... // 使用类似 kernel_test.go 的 mock 方式
    devReg.Register("/dev/llm/claude", llm.FileFactory(mockDriver, "/dev/llm/claude"))
    ctxMgr := cruxctx.NewManager()
    kern := kernel.NewKernel(vfsInst, ctxMgr, cb)

    pid, err := kern.Spawn(intent, nil, kernel.SpawnOpts{})
    // ... wait for Done, render output ...
    return buf.String(), exitStatus.Code
}
```

**关键测试场景：**

| 测试 | Mock 行为 | 验证点 |
|------|-----------|--------|
| `TestE2E_SuccessFlow` | LLM 返回 `{"content":"分析完成","tokens_used":100}` | 输出包含 spawning + step + Result Box + Summary |
| `TestE2E_ErrorFlow` | LLM 返回错误 | 输出包含 ErrorBlock + Summary（exit ≠ 0） |
| `TestE2E_JSONSuccess` | 同上成功 + ModeJSON | 输出为有效 JSON，ok=true |
| `TestE2E_JSONError` | 同上错误 + ModeJSON | 输出为有效 JSON，ok=false |
| `TestE2E_LLMTimeout` | mock Write sleep > timeout | 进程转 Zombie，CLI 不 panic |
| `TestE2E_ProcessCleanup` | 正常完成 | Done channel 写入，状态正确 |
| `TestE2E_MultiSpawn` | 连续 3 次 Spawn | 每个进程独立完成，无 PID 冲突 |
| `TestE2E_ConcurrentSpawn` | 并发 5 个 Spawn | -race 无数据竞争 |

### 超时测试设计

**mock 超时场景：**

```go
// mockSlowLLMFile 模拟 LLM 超时
type mockSlowLLMFile struct {
    delay time.Duration
}

func (m *mockSlowLLMFile) Write(data []byte, opts ...vfs.WriteOpt) error {
    time.Sleep(m.delay) // 模拟慢响应
    return context.DeadlineExceeded
}
```

**注意：** 当前 `ClaudeCliDriver` 使用 `exec.CommandContext` 实现超时，但在集成测试中我们 mock 的是 VFS 级别的 `LLMFile`，因此超时行为需要在 mock Write 方法中模拟。当 `proc.ctx` 被 cancel 时，reasonStep 循环在下一次迭代开头检测 `ctx.Done()` 并退出。

### 进程表一致性验证方法

```go
func assertProcessTableConsistency(t *testing.T, kern *kernel.KernelImpl) {
    t.Helper()
    procs := kern.ListProcesses()
    for _, p := range procs {
        state := p.GetState()
        // 完成的进程应该是 Zombie 状态（Wait/Reap 在 Story 4.1 实现）
        if state == types.StateRunning {
            t.Errorf("PID %d still Running after expected completion", p.PID)
        }
        // 检查 Exit 字段一致性
        if state == types.StateZombie && p.Exit == nil {
            t.Errorf("PID %d is Zombie but Exit is nil", p.PID)
        }
    }
}
```

### 前序 Story 经验教训（必须吸收）

1. **NewKernel 签名（Story 1.6 → 1.7）：** Story 1.7 将 `NewKernel` 从 `NewKernel(vfs, ctxMgr)` 扩展为 `NewKernel(vfs, ctxMgr, callbacks)`。当前所有测试已更新。本 Story **不修改 NewKernel 签名**，直接复用
2. **data race 敏感（Story 1.2/1.7）：** Process 字段并发访问需加锁。CLI 层读取 `proc.Result`/`proc.TokensUsed` 前必须等待 `<-proc.Done` 完成同步
3. **VFSFileFactory basePath（Story 1.3/1.5）：** `llm.FileFactory(driver, basePath)` 需要 `"/dev/llm/claude"` 作为第二个参数
4. **finishProcess 统一出口（Story 1.6）：** 所有进程终止都通过 `finishProcess` → `Terminate` → `Done channel`。集成测试应验证此流程的完整性
5. **UI 组件 JSON 模式抑制（Story 1.7 Review Fix）：** `RenderResult`/`RenderError`/`RenderSummary` 在 `ModeJSON` 下静默不输出。集成测试的 JSON 模式验证需要检查这些组件不产生额外输出
6. **测试辅助函数复用（Story 1.6）：** `kernel_test.go` 中的 `newTestKernel` 和 `mockLLMFile` 是 `kernel` 包内部的（非导出）。`cmd/crux/integration_test.go` 需要创建自己的 E2E 测试辅助函数，但应复用相同的 mock 模式
7. **.gitignore 修复（Story 1.7 Review）：** 已从 `crux` 改为 `/crux`，防止忽略 `cmd/crux/` 目录

### Go 代码命名规则（必须遵循）

| 对象 | 规则 | 示例 |
|------|------|------|
| 测试函数 | `Test<Type>_<Method>` 或 `TestE2E_<Scenario>` | `TestE2E_SuccessFlow`、`TestVersion_WithClaude` |
| mock 类型 | camelCase（非导出） | `mockSlowLLMFile`、`mockLLMDriver` |
| 测试辅助 | camelCase（非导出） | `testE2E`、`assertProcessTableConsistency` |
| 文件名 | 全小写下划线分隔 | `integration_test.go` |

### 测试规范

**测试文件位置：**
- `cmd/crux/main_test.go` — 扩展已有测试（version/help 增强测试）
- `cmd/crux/integration_test.go` — 新增 E2E 集成测试
- `kernel/kernel_test.go` — 扩展进程表一致性测试

**必须包含的测试场景：**

| 测试 | 文件 | 验证点 |
|------|------|--------|
| `TestE2E_SuccessFlow` | integration_test.go | 完整输出流 |
| `TestE2E_ErrorFlow` | integration_test.go | 错误处理输出 |
| `TestE2E_JSONSuccess` | integration_test.go | JSON 成功输出 |
| `TestE2E_JSONError` | integration_test.go | JSON 错误输出 |
| `TestE2E_LLMTimeout` | integration_test.go | 超时处理 |
| `TestE2E_ProcessCleanup` | integration_test.go | 进程清理 |
| `TestE2E_MultiSpawn` | integration_test.go | 连续多次 Spawn |
| `TestE2E_ConcurrentSpawn` | integration_test.go | 并发 Spawn（-race） |
| `TestProcessTableConsistency_*` | kernel_test.go | 进程表一致性 |
| `TestVersion_WithClaude` | main_test.go | 版本 + Claude 检测 |
| `TestVersion_WithoutClaude` | main_test.go | 版本 + Claude 缺失 |
| `TestVersion_JSON` | main_test.go | JSON 版本输出 |
| `TestHelp_ContainsUsage` | main_test.go | help 输出格式 |
| `TestSignal_GracefulShutdown` | integration_test.go | SIGINT 优雅退出 |

**测试模式：**
- 使用 Go 标准 `testing` 包，`t.Run` 子测试
- UI 输出到 `bytes.Buffer`，不依赖 TTY
- Mock LLM 通过实现 `vfs.VFSFile` 接口（与 `kernel_test.go` 一致）
- 全部通过 `go test -race ./...`

### 需要新增的依赖

无新增外部依赖。所有需要的包（`os/exec`、`bytes`、`strings`、`testing`、`time`、`context`）都是 Go 标准库。

### Project Structure Notes

**本 Story 修改的文件：**

```
cmd/crux/
├── main.go              (修改 — version 命令增强：Claude CLI 检测 + JSON 版本输出 + help 示例)
├── main_test.go         (修改 — 新增 version/help 测试)
├── integration_test.go  (新增 — E2E 集成测试套件)

kernel/
├── kernel_test.go       (修改 — 新增进程表一致性测试)
```

**不要触碰的文件：**
- `kernel/kernel.go` — Spawn/reasonStep 已完成，无需修改
- `kernel/process.go` — Process/状态机已完成，无需修改
- `kernel/errors.go` — 错误类型已完成
- `vfs/` 下任何文件
- `context/` 下任何文件
- `drivers/` 下任何文件
- `internal/types/types.go`
- `internal/xsync/` 下任何文件
- `internal/ui/` 下任何文件 — UI 组件已完成且测试通过

### References

- [Source: epics.md#Story 1.8] — 原始用户故事和验收标准
- [Source: architecture.md#Decision 2: 进程模型与并发] — 进程状态机和资源释放
- [Source: architecture.md#Decision 4: Claude Code CLI 集成] — CLI 调用模式和超时处理
- [Source: architecture.md#Implementation Patterns > 过程模式] — context.Context 传播、资源释放顺序
- [Source: architecture.md#Implementation Patterns > 格式模式] — JSONResponse[T]、JSON snake_case
- [Source: architecture.md#Project Structure & Boundaries > cmd/ 依赖注入点] — 唯一组装点
- [Source: architecture.md#修正后的依赖方向] — 单向依赖
- [Source: prd.md#NFR1] — 端到端延迟 ≤ 30s
- [Source: prd.md#NFR6] — 连续 20 次成功率 ≥ 95%
- [Source: prd.md#NFR7] — 超时 5s 内转 Zombie
- [Source: prd.md#NFR9] — 进程表一致性
- [Source: prd.md#NFR10] — CLI 进程不崩溃
- [Source: prd.md#FR32] — 智能体完成时输出汇总信息
- [Source: prd.md#FR33] — `crux "意图"` 单命令启动
- [Source: prd.md#FR36] — CLI 提供清晰错误信息
- [Source: prd.md#FR37] — `go install` 安装
- [Source: project-context.md#CLI 命令结构] — 根命令、子命令、全局 flags
- [Source: project-context.md#关键防错规则] — 禁止反向依赖、禁止裸 error
- [Source: 1-7-cli-entry-and-ui-components.md] — 前序 Story 完整产出和经验教训

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
