---
title: 'Phase 2 集成验证 BUG 修复'
slug: 'phase2-integration-bugfixes'
created: '2026-03-04'
status: 'completed'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go 1.26', 'Cobra CLI', 'Claude Code CLI', 'Charm lipgloss/bubbles']
files_to_modify:
  - 'drivers/llm/claude_cli.go'
  - 'drivers/llm/driver.go'
  - 'drivers/llm/claude_cli_test.go'
  - 'kernel/reap.go'
  - 'kernel/reap_test.go'
  - 'kernel/kernel.go'
  - 'kernel/process.go'
  - 'ipc/server.go'
  - 'ipc/server_test.go'
  - 'ipc/protocol.go'
  - 'cmd/rnix/log.go'
  - 'skillpkg/installer.go'
  - 'skillpkg/installer_test.go'
  - 'internal/types/types.go'
  - 'vfs/proc.go'
code_patterns:
  - 'SyscallError wrapping for all kernel errors'
  - 'SyncMap[K,V] for concurrent map access'
  - 'Process state machine: Created→Running→Zombie→Dead'
  - 'reapOnce ensures idempotent cleanup'
  - 'flagJSON global flag for output mode'
  - 'IPC stream protocol: StreamEvent{Type, Payload}'
test_patterns:
  - 'Test files in same directory as source'
  - 'TestType_Method naming convention'
  - 'Race detection enabled by default (-race)'
  - 'Mock via CommandBuilder injection for LLM driver'
  - 'testify assertions'
---

# Tech-Spec: Phase 2 集成验证 BUG 修复

**Created:** 2026-03-04

## Overview

### Problem Statement

Phase 2（Epic 6-12）集成验证发现 8 个 BUG，45 个验证点仅 25 个通过（55.6%）。核心问题包括：Token 计数错误导致预算管理形同虚设、进程完成后立即从 procTable 删除导致 rnix top 无法展示历史进程、日志过滤和 JSON 输出失效、LLM 不返回非零 exit_code 导致管道失败传播和 on-error 机制无法触发。

### Solution

按优先级逐个修复 8 个 BUG，覆盖 LLM 驱动层（BUG-001/003/004）、内核进程管理（BUG-002）、IPC 服务（BUG-006）、CLI 日志命令（BUG-007/008）和 Skill 安装流程（BUG-005）。

### Scope

**In Scope:**
- BUG-001（高）：修正 TokensUsed 从 NumTurns 到真实 token 计数
- BUG-002（中）：Dead 进程 TTL 保留，不立即从 procTable 删除
- BUG-003（低）：LLM 超时可见性改进
- BUG-004（中）：驱动层错误传播，使管道失败传播和 on-error 生效
- BUG-005（低）：Skill 安装前检查本地已有 Skill
- BUG-006（中）：checkIdle() 排除 Zombie 进程
- BUG-007（中）：rnix log --filter 过滤修复（日志历史重放）
- BUG-008（中）：rnix log --json 输出修复（同 BUG-007 根因）

**Out of Scope:**
- 新功能开发
- 性能优化
- MCP 相关功能测试
- 社区 Skill 注册表（registry.rnix.ai 不可达）

## Context for Development

### Codebase Patterns

- **错误处理**：所有 syscall 返回 `*kernel.SyscallError`（含 Syscall/PID/Device/Err/Code）
- **并发保护**：`SyncMap[K, V]` 用于 procTable、msgQueues、procGroups
- **驱动接口**：`LLMDriver` 接口（Call/Stream/Info），通过 `CommandBuilder` 注入实现可测试性
- **CLI 输出模式**：`flagJSON` 全局标志控制 JSON/TUI 输出，`resolveOutputMode()` 检测 TTY/Pipe
- **IPC 流协议**：`StreamEvent{Type, Payload}` 格式，类型包括 `StreamLogEntry`、`StreamEOF` 等
- **进程状态机**：`Created→Running→Zombie→Dead`，`reapOnce` 保证幂等清理
- **资源释放顺序**：cancel() → wg.Wait() → close(DebugChan/LogChan) → close(msgQueue) → ClearSignalState → ClearThreads → ClearCoroutines → CtxFree → Reap() → RemoveProcess()

### Files to Reference

| File | Purpose | Key Lines |
| ---- | ------- | --------- |
| `drivers/llm/claude_cli.go` | Claude CLI 驱动，BUG-001/003 | L79-88 (claudeCliResponse), L129 (TokensUsed=NumTurns), L142-156 (claudeStreamEvent), L215 (stream同问题) |
| `drivers/llm/driver.go` | LLM 接口定义 | L18-21 (LLMResponse), L24-29 (StreamEvent) |
| `kernel/kernel.go` | 内核主体，BUG-004 | L101-123 (KernelImpl), L401-724 (reasonStep), L543-564 (token累加+预算检查), L605-639 (权限检查), L641-718 (tool调用处理), L872-882 (GetLogChan) |
| `kernel/reap.go` | 进程回收，BUG-002 | L13-65 (reapProcess), L63 (RemoveProcess), L151-181 (startReaper) |
| `kernel/process.go` | 进程模型 | L31-73 (Process struct), L43 (CreatedAt) |
| `ipc/server.go` | IPC 服务，BUG-006/007/008 | L135-151 (checkIdle), L161-174 (tryAutoShutdown), L426-451 (handleAttachLog) |
| `ipc/client.go` | IPC 客户端 | L279-317 (AttachLog 流处理) |
| `cmd/rnix/log.go` | 日志命令 | L98-115 (callback过滤+JSON输出), L105 (filter逻辑) |
| `skillpkg/installer.go` | Skill 安装器，BUG-005 | L46-50 (只检查registry不检查文件系统) |
| `skillpkg/registry.go` | Skill 注册表 | L85-96 (Get只读.registry.yaml) |
| `internal/types/types.go` | 类型定义 | L89-112 (ProcessState: Created/Running/Zombie/Dead) |
| `vfs/proc.go` | ProcInfo 结构 | ProcInfo{PID, State, TokensUsed, ...} |
| `ipc/protocol.go` | IPC 协议 Wire 类型 | ProcInfoWire, ProcInfoToWire/WireToProcInfo |

### Technical Decisions

- **BUG-001**：扩展 `claudeCliResponse` 和 `claudeStreamEvent` 添加 token 字段。实现前需先运行 `claude -p "hello" --output-format json` 确认实际字段名。备选方案：若 CLI 不返回 token 字段，则从 `CostUSD` 反算（按已知模型定价）
- **BUG-002**：TTL 保留方案 — reapProcess 不再调用 RemoveProcess，改为设置 `DeadAt` 时间戳。扩展 reaper goroutine 增加 ticker 定期清理超时 Dead 进程（TTL=60s）
- **BUG-003**：已确认 `DefaultTimeout=5min` 且可通过 `SpawnOpts.TimeoutMs` 配置。改进：在 compose YAML 中支持 `timeout` 字段，CLI help 中说明
- **BUG-004**：在 reasonStep tool 调用流程中，VFS 读写错误不再直接终止进程，改为将错误信息写入 context 并设置 `proc.HasToolError=true`。推理循环结束时（action==text），若 `HasToolError` 为 true，设置 `ExitStatus.Code=1`。这样既不中断 LLM 推理，又能在最终结果中传播错误
- **BUG-005**：Install() 在 registry 检查前增加文件系统探测
- **BUG-006**：checkIdle/tryAutoShutdown 只将 Running/Created 计为活跃
- **BUG-007/008**：Process 新增 `logHistory []types.LogEntry` 环形缓冲（cap=256）。`emitLog` 同时写入 LogChan 和 logHistory。`handleAttachLog` 先重放 logHistory，再监听实时 LogChan

## Implementation Plan

### Tasks

#### Phase A: 基础修复（无依赖，可并行）

- [x] Task 1: BUG-006 — checkIdle 排除 Zombie 进程
  - File: `ipc/server.go`
  - Action: 修改 `checkIdle()` (L139) 和 `tryAutoShutdown()` (L165) 的活跃进程判断条件
  - Change: `if p.State != types.StateDead` → `if p.State == types.StateRunning || p.State == types.StateCreated`
  - Notes: 最简单的修复，先做这个。两处函数都需要修改，逻辑完全一致

- [x] Task 2: BUG-001 — 修正 TokensUsed 为真实 token 计数
  - File: `drivers/llm/claude_cli.go`
  - Action 2a: **先验证** — 运行 `claude -p "hello" --output-format json 2>/dev/null` 查看返回 JSON 中的 token 相关字段名（预期：`input_tokens`、`output_tokens`）
  - Action 2b: 扩展 `claudeCliResponse` (L79-88) 添加 token 字段：
    ```go
    InputTokens  int `json:"input_tokens"`
    OutputTokens int `json:"output_tokens"`
    ```
  - Action 2c: 扩展 `claudeStreamEvent` (L142-156) 添加相同字段
  - Action 2d: 修改 Call() 返回 (L129): `TokensUsed: cliResp.InputTokens + cliResp.OutputTokens`
  - Action 2e: 修改 Stream() result 处理 (L215): `TokensUsed: evt.InputTokens + evt.OutputTokens`
  - File: `drivers/llm/driver.go`
  - Action 2f: 可选 — 在 `LLMResponse` 和 `StreamEvent` 中添加 `InputTokens`/`OutputTokens` 分项字段，保留 `TokensUsed` 作为总和
  - Notes: 若 Claude CLI 不返回 token 字段，备选方案为从 `CostUSD` 按模型定价反算。`NumTurns` 保留为参考信息但不再用于 TokensUsed

- [x] Task 3: BUG-005 — Skill 安装本地检查
  - File: `skillpkg/installer.go`
  - Action: 在 `Install()` 方法的 registry 检查 (L46-50) 之后、网络 Resolve (L55) 之前，添加文件系统检查：
    ```go
    // 检查本地目录是否存在有效 skill
    skillDir := filepath.Join(inst.basePath, name)
    if info, err := os.Stat(skillDir); err == nil && info.IsDir() {
        if _, loadErr := inst.skillLoader.LoadMetadata(name); loadErr == nil {
            if !opts.Force {
                return nil, &AlreadyInstalledError{Name: name, Version: "local"}
            }
        }
    }
    ```
  - Notes: `AlreadyInstalledError` 已存在于 installer.go 中。`skillLoader.LoadMetadata()` 可从 `ListAll()` 方法中找到用法参考 (L261-330)

#### Phase B: 内核层修复（Task 1 完成后）

- [x] Task 4: BUG-002 — Dead 进程 TTL 保留
  - File: `kernel/process.go`
  - Action 4a: 在 Process struct (L31-73) 中添加 `DeadAt time.Time` 字段（与 `CreatedAt` 同级）
  - File: `kernel/reap.go`
  - Action 4b: 修改 `reapProcess()` (L13-65)：删除 L63 的 `k.RemoveProcess(proc.PID)`，替换为：
    ```go
    proc.mu.Lock()
    proc.DeadAt = time.Now()
    proc.mu.Unlock()
    ```
  - Action 4c: 扩展 `startReaper()` (L151-181)：在 select 中增加 ticker 分支。ticker 通过新增的 `deadTicker *time.Ticker` 字段存储在 KernelImpl 中（而非局部变量），以便 Shutdown 可以停止它：
    ```go
    // KernelImpl 新增字段：
    deadTicker *time.Ticker

    // startReaper 中创建 ticker：
    k.deadTicker = time.NewTicker(10 * time.Second)
    // select 中增加分支：
    case <-k.deadTicker.C:
        k.cleanupExpiredDead(DeadProcessTTL)
    ```
  - Action 4d: 新增方法 `cleanupExpiredDead(ttl time.Duration)`：遍历 procTable，对 `State==StateDead && !DeadAt.IsZero() && time.Since(DeadAt) > ttl` 的进程调用 `RemoveProcess`
  - Action 4e: 修改 `Shutdown()` (reap.go L185-194)：在 `close(k.stopCh)` 之前调用 `k.deadTicker.Stop()` 停止 ticker，避免 Shutdown 期间 cleanupExpiredDead 并发执行
    ```go
    k.shutdownOnce.Do(func() {
        if k.mountMgr != nil { _ = k.mountMgr.UnmountAll() }
        if k.deadTicker != nil { k.deadTicker.Stop() }
        close(k.stopCh)
    })
    ```
  - File: `vfs/proc.go` (注意：非 `vfs/procinfo.go`，该文件不存在)
  - Action 4f: 在 `ProcInfo` struct 中添加 `DeadAt time.Time` 字段
  - File: `ipc/protocol.go`
  - Action 4g: 在 `ProcInfoWire` struct 中添加 `DeadAt` 字段，并更新 `ProcInfoToWire`/`WireToProcInfo` 转换函数，确保远程 rnix top/ps 能看到 Dead 进程的时间戳
  - File: `kernel/kernel.go`
  - Action 4h: 在 `ListProcs()` (L900-921) 中将 `proc.DeadAt` 复制到 `ProcInfo.DeadAt`
  - Notes: TTL 常量建议 `const DeadProcessTTL = 60 * time.Second`，ticker 间隔 10 秒。Dead 进程的 FD/channels 已经在 reap 时关闭，TTL 保留的只是 procTable 中的元数据。**[F3]** `ipc/server.go` 中 `handleSpawn` 的 `defer Reap(pid)` 和 `SpawnAndWait` 的 `Reap(pid)` 在新设计下会延迟清理进程 60s——这是设计意图，长时间 compose DAG 可能累积 Dead 进程但仅保留元数据，内存开销可控。**[F10]** `SpawnAndWait` 中 `GetProcInfo(pid)` 在 `Reap(pid)` 之前调用——修复后 Reap 不再删除进程，GetProcInfo 总能成功，这是一个正面副作用

- [x] Task 5: BUG-004 — 驱动层错误传播
  - File: `kernel/process.go`
  - Action 5a: 在 Process struct 中添加 `HasToolError bool` 字段（mu 保护）
  - File: `kernel/kernel.go`
  - Action 5b: 修改 reasonStep 的 tool 调用错误处理 (L641-718)。**三个错误路径需要分别处理，注意 FD 资源清理 [F4]**：
    ```go
    // 路径 1: vfs.Open 失败 (L648) — 此时无 FD 需要关闭
    toolFD, err := k.vfs.Open(proc.PID, action.ToolPath, vfs.O_RDWR)
    if err != nil {
        errMsg := fmt.Sprintf("Tool error (%s): open failed: %v", action.ToolPath, err)
        if appendErr := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, errMsg); appendErr != nil {
            k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool error failed", Err: appendErr})
            return
        }
        proc.mu.Lock()
        proc.HasToolError = true
        proc.mu.Unlock()
        continue // 无 FD 泄漏风险
    }

    // 路径 2: vfs.Write 失败 (L661) — 必须先关闭 toolFD
    if err := k.vfs.Write(proc.ctx, proc.PID, toolFD, action.ToolData); err != nil {
        _ = k.vfs.Close(proc.PID, toolFD) // ← 关键：关闭已打开的 FD
        errMsg := fmt.Sprintf("Tool error (%s): write failed: %v", action.ToolPath, err)
        if appendErr := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, errMsg); appendErr != nil {
            k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool error failed", Err: appendErr})
            return
        }
        proc.mu.Lock()
        proc.HasToolError = true
        proc.mu.Unlock()
        continue
    }

    // 路径 3: vfs.Read 失败 (L678) — 同样需要关闭 toolFD
    toolResult, err := k.vfs.Read(proc.PID, toolFD, 1<<20)
    if err != nil {
        _ = k.vfs.Close(proc.PID, toolFD) // ← 关键：关闭已打开的 FD
        errMsg := fmt.Sprintf("Tool error (%s): read failed: %v", action.ToolPath, err)
        if appendErr := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, errMsg); appendErr != nil {
            k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool error failed", Err: appendErr})
            return
        }
        proc.mu.Lock()
        proc.HasToolError = true
        proc.mu.Unlock()
        continue
    }
    ```
  - Action 5c: 修改 ActionText 完成逻辑 (L573-585)。在 `finishProcess` 之前检查 HasToolError，**完整构造 ExitStatus [F19]**：
    ```go
    case ActionText:
        k.emitLog(proc, step, types.LogOutput, action.Content, "")
        proc.mu.Lock()
        proc.Result = action.Content
        hadError := proc.HasToolError
        proc.mu.Unlock()
        // ... emitEvent ...
        exitCode := 0
        reason := "completed"
        if hadError {
            exitCode = 1
            reason = "completed_with_tool_errors"
        }
        k.finishProcess(proc, ExitStatus{Code: exitCode, Reason: reason})
        return
    ```
  - Notes: 三个错误路径各有不同的资源清理需求——Open 失败时无 FD，Write/Read 失败时必须 Close FD。`AppendToolResult` 的第二参数 `toolCallID` 使用 `action.ToolPath`（如 `/dev/fs`），这与当前正常路径中的调用方式一致（L698），assistant message 已在 L590 追加，所以 ToolResult 与之成对，不会破坏 LLM message 序列结构 [F5]

#### Phase C: 日志系统修复

- [x] Task 6: BUG-007/008 — 日志历史重放
  - File: `kernel/process.go`
  - Action 6a: 在 Process struct 中添加日志历史缓冲。**使用 mu 锁而非独立锁 [F18]**——logHistory 的写入在 `emitLog` 中（已持有 mu），读取在 `GetLogHistory` 中（也用 mu），与 LogChan 操作在同一锁保护下，保证原子性：
    ```go
    logHistory []types.LogEntry  // ring buffer, cap=256, 由 mu 保护
    logHistIdx int               // 环形缓冲写入位置
    logHistLen int               // 当前有效条目数
    ```
  - Action 6b: 添加方法（均在 mu 保护下）：
    - `AppendLogHistory(entry types.LogEntry)`: 环形缓冲写入
    - `GetLogHistory() []types.LogEntry`: 返回按时间序的副本快照
  - File: `kernel/kernel.go`
  - Action 6c: 在 `emitLog()` 函数中，**在 mu 锁保护范围内**同时写入 LogChan 和 logHistory，确保两者的数据一致性：
    ```go
    proc.mu.Lock()
    // 1. 写入历史（总是写入）
    proc.AppendLogHistory(entry)
    // 2. 写入实时通道（如果还开着）
    ch := proc.LogChan
    if ch != nil {
        select {
        case ch <- entry:
        default:
        }
    }
    proc.mu.Unlock()
    ```
  - Action 6d: 新增内核方法 `GetLogHistory(pid types.PID) ([]types.LogEntry, bool)` — 从 Process 获取历史日志快照
  - File: `ipc/server.go`
  - Action 6e: **重写 `handleAttachLog()` 入口逻辑 [F7]**。当前代码先调用 `GetLogChan`，若返回 nil 则直接返回 NOT_FOUND——这阻断了 Dead 进程的日志访问。修改为：先检查进程是否存在（通过 `GetLogHistory` 或新增的进程存在检查），再决定重放+实时流行为：
    ```go
    func (s *Server) handleAttachLog(conn net.Conn, rawPayload json.RawMessage) {
        var req AttachLogRequest
        if err := json.Unmarshal(rawPayload, &req); err != nil {
            writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid attach_log request"}})
            return
        }

        // [F7] 改用 GetLogHistory 检查进程是否存在（而非 GetLogChan）
        history, histOK := s.kern.GetLogHistory(req.PID)
        logCh, logOK := s.kern.GetLogChan(req.PID)

        // 进程不存在（既无历史也无通道）
        if !histOK && (!logOK || logCh == nil) {
            writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
            return
        }

        writeResponse(conn, Response{OK: true})
        enc := json.NewEncoder(conn)

        // [F13] 记录已重放的最后一条日志的时间戳，用于去重
        var lastReplayedTs int64
        if histOK && len(history) > 0 {
            // 重放历史日志
            for _, entry := range history {
                lew := LogEntryToWire(entry)
                payload, _ := json.Marshal(lew)
                se := StreamEvent{Type: StreamLogEntry, Payload: payload}
                if err := enc.Encode(se); err != nil {
                    return
                }
                lastReplayedTs = lew.TimestampMs
            }
        }

        // 实时转发（如果进程还活着）
        if logOK && logCh != nil {
            for entry := range logCh {
                lew := LogEntryToWire(entry)
                // [F13] 跳过已在历史中重放过的日志（基于时间戳去重）
                if lew.TimestampMs <= lastReplayedTs {
                    continue
                }
                payload, _ := json.Marshal(lew)
                se := StreamEvent{Type: StreamLogEntry, Payload: payload}
                if err := enc.Encode(se); err != nil {
                    return
                }
            }
        }

        _ = enc.Encode(StreamEvent{Type: StreamEOF})
    }
    ```
  - Notes: **[F7]** 这是审查发现的最严重问题——原方案的入口 NOT_FOUND 检查会阻断所有已完成进程的日志访问。新方案同时检查 logHistory 和 LogChan 来判断进程是否存在。**[F13]** 通过时间戳去重解决运行中进程 attach 时的消息重复问题：logHistory 快照和 LogChan buffer 可能有重叠条目，跳过时间戳 ≤ 最后重放条目的日志。**[F18]** 使用 mu 锁统一保护 LogChan 和 logHistory，在 reapProcess 关闭 LogChan 时 logHistory 数据不受影响（reapProcess 不清理 logHistory，数据在 TTL 过期后随进程一起删除）

#### Phase D: 文档改进

- [x] Task 7: BUG-003 — LLM 超时可见性
  - File: `cmd/rnix/main.go`
  - Action 7a: 在 compose YAML 的 agent 配置中已支持 `timeout_ms` 字段（确认是否存在，不存在则添加到 compose 解析逻辑中）
  - Action 7b: 在 `rnix --help` 或 `rnix compose --help` 中说明超时配置方式
  - Notes: `DefaultTimeout=5min(300s)` 已可通过 `SpawnOpts.TimeoutMs` 和 `WithTimeout()` 配置。此任务主要是文档和可见性改进，不涉及核心逻辑变更

### Acceptance Criteria

#### BUG-001: Token 计数修复

- [x] AC 1.1: Given Claude CLI 返回包含 `input_tokens: 100, output_tokens: 50` 的 JSON, when 内核处理 LLM 响应, then `proc.TokensUsed` 累加 150（非 NumTurns 值）
- [x] AC 1.2: Given 进程设置 `context_budget: 200`, when LLM 响应累计 token ≥ 200, then 进程以 `exit_code=2, reason=budget_exceeded` 终止
- [x] AC 1.3: Given `rnix compose up --json`, when 多个 Agent 完成执行, then `total_tokens` 为各 Agent `input_tokens + output_tokens` 之和
- [x] AC 1.4: Given stream 模式下 result 事件, when 解析 TokensUsed, then 值为 `input_tokens + output_tokens`（非 NumTurns）

#### BUG-002: Dead 进程 TTL 保留

- [x] AC 2.1: Given 进程完成并转为 Dead 状态, when 在 TTL(60s) 内调用 `ListProcs()`, then 结果包含该 Dead 进程及其 `DeadAt` 时间戳
- [x] AC 2.2: Given Dead 进程已超过 TTL, when reaper ticker 触发清理, then 进程从 procTable 移除且 `ListProcs()` 不再返回
- [x] AC 2.3: Given 多个进程依次完成（如 compose DAG）, when rnix top 轮询, then 能同时看到 running 和最近完成的 dead 进程
- [x] AC 2.4: Given kernel Shutdown 被调用, when ticker 运行中, then ticker 正常停止，无 goroutine 泄漏

#### BUG-004: 驱动层错误传播

- [x] AC 4.1: Given LLM 调用 `/dev/fs` 读取不存在的文件, when VFS Read 返回错误, then 错误信息被写入 context（非进程终止），`HasToolError` 被标记为 true
- [x] AC 4.2: Given 推理完成（action=text）且 `HasToolError=true`, when `finishProcess` 被调用, then `ExitStatus.Code = 1`
- [x] AC 4.3: Given 管道 `spawn A | spawn B` 中 A 的 tool 调用失败, when A 完成且 exit_code=1, then B 不执行（管道失败传播）
- [x] AC 4.4: Given `spawn "..." on-error spawn "回滚"` 中主命令 tool 调用失败, when 主命令 exit_code=1, then on-error 处理器被触发
- [x] AC 4.5: Given LLM 调用 tool 失败后自行恢复（如换用其他方法完成任务）, when 推理循环继续, then LLM 仍能正常完成后续 step（不被提前中断）

#### BUG-005: Skill 安装本地检查

- [x] AC 5.1: Given builtin skill `code-analysis` 存在于 `lib/skills/code-analysis/`, when 执行 `rnix skill install code-analysis`, then 返回 `AlreadyInstalledError`（非网络错误）
- [x] AC 5.2: Given `--force` 标志, when 执行 `rnix skill install code-analysis --force`, then 绕过本地检查，继续执行安装流程
- [x] AC 5.3: Given 本地目录存在但 `SKILL.md` 无效, when 执行 `rnix skill install <name>`, then 不返回 AlreadyInstalled，继续网络安装流程

#### BUG-006: checkIdle 排除 Zombie

- [x] AC 6.1: Given procTable 中仅有 Zombie 状态进程, when `checkIdle()` 执行, then `activeProcs == 0`，不阻止空闲计时
- [x] AC 6.2: Given procTable 中有 Running 状态进程, when `checkIdle()` 执行, then `activeProcs > 0`，空闲计时被重置
- [x] AC 6.3: Given 无活跃连接且无 Running/Created 进程, when `tryAutoShutdown()` 执行, then daemon 正常关闭

#### BUG-007/008: 日志历史重放

- [x] AC 7.1: Given 进程已完成（Dead 状态），logHistory 中有 3 条 [think] 日志, when 执行 `rnix log <pid>`, then 输出包含这 3 条 [think] 日志
- [x] AC 7.2: Given 进程已完成, when 执行 `rnix log <pid> --filter think`, then 仅输出 [think] 分类的日志（非空）
- [x] AC 7.3: Given 进程已完成, when 执行 `rnix log <pid> --json`, then 每行输出合法 JSON，包含 `category`/`content`/`timestamp_ms` 字段
- [x] AC 7.4: Given 进程正在运行, when `emitLog()` 被调用, then 日志同时写入 LogChan 和 logHistory
- [x] AC 7.5: Given logHistory 已满 256 条, when 新日志写入, then 最旧的条目被覆盖（环形缓冲行为）

#### BUG-003: 超时可见性

- [x] AC 3.1: Given compose YAML 中 agent 配置了 `timeout_ms: 120000`, when Agent 执行超过 120 秒, then 进程因超时终止

## Additional Context

### Dependencies

- Claude CLI JSON 输出格式（Task 2 实施前需先运行 `claude -p "hello" --output-format json` 确认 token 字段名）
- 测试 fixture 文件：`compose/testdata/` 下的集成测试 YAML
- 集成验证报告：`docs/phase2-integration-validation.md`

### Testing Strategy

**单元测试（每个 BUG 对应）：**
- BUG-001: `drivers/llm/claude_cli_test.go` — mock CommandBuilder 返回带 `input_tokens`/`output_tokens` 的 JSON，验证 `LLMResponse.TokensUsed` 值正确
- BUG-002: `kernel/reap_test.go` — 测试 reapProcess 后进程仍在 procTable 中，等待 TTL 后被清理
- BUG-004: `kernel/kernel_test.go` — mock VFS Read 返回错误，验证 `HasToolError=true` 且 `ExitStatus.Code=1`
- BUG-005: `skillpkg/installer_test.go` — 测试本地目录存在时返回 AlreadyInstalledError
- BUG-006: `ipc/server_test.go` — 测试 Zombie 进程不阻止空闲退出
- BUG-007/008: `kernel/process_test.go` — 测试 logHistory 环形缓冲的 append/get 行为

**集成测试：**
- 修复完成后重新执行 `docs/phase2-integration-validation.md` 的 10 个场景
- 特别关注：场景 2（rnix top 显示 token）、场景 4/5（管道失败传播+on-error）、场景 8（rnix log filter/json）、场景 9（Token 预算）

**回归测试：**
- `make all` 全量通过（lint + vet + test + build）
- 已通过的 25 个验证点不回退

### Notes

**高风险项：**
- BUG-004 的设计张力：tool 错误不终止推理循环可能导致 LLM 在错误上下文中继续推理。但这是合理的妥协——LLM 有能力自行处理错误，只在最终结果层面传播错误标记
- BUG-002 的 TTL 与 BUG-006 的交互：修复顺序为先 BUG-006 再 BUG-002。BUG-006 将 Zombie 排除出活跃计数后，BUG-002 引入的 Dead-with-TTL 进程也不会影响空闲检测（Dead 在修改后的 checkIdle 中本来就不计为活跃）
- BUG-001 的 token 字段名：需要实测确认 Claude CLI 实际返回的字段名。如果 CLI 不返回 token 字段，需改用 CostUSD 反算方案

**已知限制：**
- BUG-004 的 `HasToolError` 是进程级标记，一旦有任何 tool 错误就会设置。如果 LLM 在后续推理中成功恢复了错误，exit_code 仍为 1。后续可通过分析 LLM 最终响应的语义来优化
- BUG-007/008 的 logHistory 使用固定 256 条上限。长时间运行的复杂 Agent 可能丢失早期日志。可通过增大缓冲或引入持久化日志解决
- BUG-001 备选方案"从 CostUSD 反算"实际不可行（模型定价变动、input/output 价格不同无法拆分），如 CLI 不返回 token 字段应等待新版本支持

**对抗性审查修复记录 (Critical+High)：**
- [F7] handleAttachLog 入口 NOT_FOUND 逻辑重写 → Task 6e
- [F4] 三个 tool 错误路径的 FD 资源清理 → Task 5b
- [F3] SpawnAndWait/handleSpawn 的 Reap 调用链影响分析 → Task 4 Notes
- [F13] 日志重放去重机制（时间戳比对） → Task 6e
- [F19] ExitStatus 完整构造（reason="completed_with_tool_errors"） → Task 5c
- [F18] logHistory 锁设计改为 mu 统一保护 → Task 6a/6c
- [F10] GetProcInfo 时序依赖分析 → Task 4 Notes

**修复顺序建议：**
1. Task 1 (BUG-006) → 最简单，解除 daemon 空闲退出阻塞
2. Task 2 (BUG-001) → 根因性问题，影响面最广
3. Task 3 (BUG-005) → 独立修复，无依赖
4. Task 4 (BUG-002) → 依赖 Task 1 完成
5. Task 5 (BUG-004) → 中等复杂度，内核层改动
6. Task 6 (BUG-007/008) → 需要新增日志缓冲机制
7. Task 7 (BUG-003) → 最低优先级，文档改进

## Review Notes

- Adversarial review completed
- Findings: 14 total, 3 fixed, 11 skipped
- Resolution approach: auto-fix (real findings only)
- F1 (Critical/Real): `cleanupExpiredDead` 在 Range RLock 内调用 Delete 导致死锁 → 改为先收集 PID 再删除
- F3 (High/Real): 日志去重 `<=` 丢失同毫秒日志 → 改为 `<`
- F13 (Low/Real): 缺 Dead TTL 清理测试 → 新增 `TestCleanupExpiredDead_RemovesAfterTTL`
- Skipped (noise/undecided): F2, F4, F5, F6, F7, F8, F9, F10, F11, F12, F14
