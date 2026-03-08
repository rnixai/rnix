# Story 15.4: 上下文使用分析

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 平台构建者,
I want 通过 `rnix ctx-profile <pid>` 查看智能体上下文的使用分析，识别最大消费者,
So that 我可以优化 token 使用效率，减少不必要的上下文消耗。

## Acceptance Criteria

1. **Given** 一个 Running 或 Zombie 状态的智能体
   **When** 用户执行 `rnix ctx-profile <pid>`
   **Then** 系统将上下文分为活跃（当前推理引用）、温（近期使用）、冷（未引用）、泄漏（已无用未释放）四类展示
   **And** 分析结果延迟 <= 1s（NFR34）

2. **Given** 上下文分析结果
   **When** 结果中存在大消费者
   **Then** 系统识别哪个 Skill 或工具结果占用最多 token，并给出具体优化建议

3. **Given** 用户传入不存在的 PID 或非 Running/Zombie 状态的进程
   **When** 执行 `rnix ctx-profile <pid>`
   **Then** 系统返回友好的错误信息

4. **Given** 用户使用 `--json` 标志
   **When** 执行 `rnix ctx-profile <pid> --json`
   **Then** 系统以 JSON 格式输出 ctx-profile 分析结果

## Tasks / Subtasks

- [x] Task 1: 上下文分类与消费者分析引擎 (AC: #1, #2)
  - [x] 1.1 在 `debug/ctx_profile.go` 中定义 `CtxProfileResult` 结构体：
    ```go
    type CtxProfileResult struct {
        PID             types.PID
        CtxID           types.CtxID
        TokensUsed      int
        ContextBudget   int
        TotalTokens     int
        Classification  ClassificationResult
        TopConsumers    []ConsumerEntry
        Suggestions     []string
    }
    type ClassificationResult struct {
        Active  ClassBucket  // 活跃：system prompt + 最近 4 条消息
        Warm    ClassBucket  // 温：接下来 6 条消息
        Cold    ClassBucket  // 冷：更早的消息
        Leaked  ClassBucket  // 泄漏：大工具结果且位于 warm 之前
    }
    type ClassBucket struct {
        Tokens   int
        Messages int
        Pct      float64
    }
    type ConsumerEntry struct {
        Kind       string   // "system_prompt" | "user" | "assistant" | "tool:<name>"
        Tokens     int
        Pct        float64
        Rank       int
        Suggestion string  // 可选，针对该消费者的优化建议
    }
    ```
  - [x] 1.2 定义 `contextData` 输入结构（用于分析引擎，从 `CtxRead` 解析，避免与 `context` 包冲突）：
    ```go
    type contextData struct {
        SystemPrompt string        `json:"system_prompt"`
        Messages     []ctxMessage  `json:"messages"`
    }
    type ctxMessage struct {
        Role       string `json:"role"`
        Content    string `json:"content"`
        ToolCallID string `json:"tool_call_id,omitempty"`
    }
    ```
  - [x] 1.3 实现 `AnalyzeContext(data *contextData, pid types.PID, ctxID types.CtxID, tokensUsed, contextBudget int) *CtxProfileResult`：
    - 解析 data，按分类规则划分消息到 Active/Warm/Cold/Leaked
    - 计算每类 token 估算（chars/4）
    - 调用 `findTopConsumers` 识别大消费者
    - 调用 `generateSuggestions` 生成优化建议
    - pid、ctxID、tokensUsed、contextBudget 填入 result（由 Server 从 ProcInfo 传入，避免 debug 依赖 vfs）
  - [x] 1.4 实现分类逻辑（MVP 启发式）：
    - Active = system prompt + 最后 4 条消息（最近一轮对话）
    - Warm = 倒数第 5～10 条消息（近期历史）
    - Cold = 更早的消息
    - Leaked = 工具结果（role=tool）且 len(content) > 1000 且位于 Warm 窗口之前
  - [x] 1.5 实现 `findTopConsumers(data *contextData, totalTokens int, topN int) []ConsumerEntry`：
    - 按 kind 聚合：system_prompt、user（汇总）、assistant（汇总）、tool（逐条，提取工具名）
    - 工具名提取：从 content 中匹配路径模式（如 `read_file`、`/path/to/file`）或 ToolCallID 前缀，无法提取时用 `tool:<tool_call_id>` 或 `tool:unknown`
    - 按 tokens 降序排列，取 Top-5
    - 计算每项 Pct = tokens / totalTokens * 100
  - [x] 1.6 实现 `generateSuggestions(result *CtxProfileResult) []string`：
    - system prompt > 25% total → "Consider trimming system prompt"
    - tool results > 50% total → "Tool results dominate context; consider more concise tool outputs"
    - leaked tokens > 0 → "Found N leaked tool results using M tokens; consider pruning unused tool outputs"
    - cold pct > 40% → "40% of context is cold; consider context compaction"
    - total tokens > 80% ContextBudget 且 ContextBudget > 0 → "Context is near budget limit; optimization recommended"
    - 每条建议只输出一次，按优先级排序

- [x] Task 2: 格式化输出 (AC: #1, #2)
  - [x] 2.1 在 `debug/ctx_profile.go` 中实现 `FormatCtxProfile(result *CtxProfileResult) string`：
    ```
    Ctx Profile: PID 1  |  CtxID 1  |  ~1200 tok / 8000 budget

    ── Classification ─────────────────────────────────────
    Active (活跃)   400 tok  33.3%  4 msgs   ← 当前推理引用
    Warm (温)       200 tok  16.7%  6 msgs   ← 近期使用
    Cold (冷)       400 tok  33.3%  12 msgs  ← 未引用
    Leaked (泄漏)   200 tok  16.7%  2 msgs   ← 已无用未释放

    ── Top Consumers ──────────────────────────────────────
    #1  system_prompt    350 tok  29.2%
    #2  tool:read_file  280 tok  23.3%   ← 考虑精简工具输出
    #3  assistant       200 tok  16.7%
    #4  user            150 tok  12.5%
    #5  tool:list_dir   120 tok  10.0%

    ── Suggestions ────────────────────────────────────────
    • Consider trimming system prompt
    • Tool results dominate context; consider more concise tool outputs
    ```
  - [x] 2.2 使用文本符号（←）标注每类含义，Top Consumers 以 `#N` 排名格式展示
  - [x] 2.3 无 Suggestions 时省略该段落

- [x] Task 3: JSON 序列化 (AC: #4)
  - [x] 3.1 为 `CtxProfileResult` 实现 `MarshalJSON`，使用 snake_case JSON 字段：
    ```json
    {
      "pid": 1,
      "ctx_id": 1,
      "tokens_used": 1200,
      "context_budget": 8000,
      "total_tokens": 1200,
      "classification": {
        "active": {"tokens": 400, "messages": 4, "pct": 33.3},
        "warm": {...},
        "cold": {...},
        "leaked": {...}
      },
      "top_consumers": [...],
      "suggestions": [...]
    }
    ```
  - [x] 3.2 时间字段如有则使用 `_ms` 后缀毫秒值（与项目规范一致）
  - [x] 3.3 百分比字段保留一位小数

- [x] Task 4: IPC 协议扩展 (AC: #1, #3)
  - [x] 4.1 在 `ipc/protocol.go` 中新增：
    ```go
    MethodCtxProfile Method = "ctx_profile"
    type CtxProfileRequest struct {
        PID types.PID `json:"pid"`
    }
    // Response: OK 时 Payload 为 CtxProfileResult 的 JSON；!OK 时 Error 为 ErrorPayload
    ```
  - [x] 4.2 在 `ipc/server.go` 的 `handleConn` switch 中增加 `case MethodCtxProfile: s.handleCtxProfile(conn, req.Payload)`
  - [x] 4.3 实现 `handleCtxProfile(conn net.Conn, rawPayload json.RawMessage)`：
    - 解析 CtxProfileRequest
    - 调用 `s.kern.GetProcInfo(pid)` 获取进程信息（返回 *vfs.ProcInfo）
    - 校验进程存在且 `info.State == StateRunning || info.State == StateZombie`
    - 若不存在或状态不符，返回 `ErrorPayload{Code: "NOT_FOUND"|"INVALID", Message: "..."}`
    - 从 `s.ctxMgr.CtxRead(info.CtxID, 0, 0)` 读取完整上下文
    - 解析为 contextData，调用 `debug.AnalyzeContext(data, info.PID, info.CtxID, info.TokensUsed, info.ContextBudget)` 得到 CtxProfileResult
    - 写入 Response{OK: true, Payload: json.Marshal(result)}
  - [x] 4.4 分析过程加超时保护（如 context.WithTimeout 1s），确保 NFR34
  - [x] 4.5 在 `ipc/client.go` 中实现 `CtxProfile(pid types.PID) (*debug.CtxProfileResult, error)`：
    - 调用 `c.call(MethodCtxProfile, CtxProfileRequest{PID: pid})`
    - 解析 Response.Payload 为 CtxProfileResult，返回 result 或 error

- [x] Task 5: CLI `ctx-profile` 命令 (AC: #1-4)
  - [x] 5.1 在 `cmd/rnix/` 中新建 `ctx_profile.go`，定义 `ctxProfileCmd`：
    ```go
    var ctxProfileCmd = &cobra.Command{
        Use:   "ctx-profile <pid>",
        Short: "Analyze context usage for an agent process",
        Long:  `Show context classification (active/warm/cold/leaked) and top token consumers.`,
        Example: `  rnix ctx-profile 1
  rnix ctx-profile 1 --json`,
        Args:  cobra.ExactArgs(1),
        RunE:  runCtxProfile,
    }
    ```
  - [x] 5.2 在 `cmd/rnix/main.go` 的 `init()` 中注册：`rootCmd.AddCommand(ctxProfileCmd)`
  - [x] 5.3 实现 `runCtxProfile(cmd *cobra.Command, args []string) error`：
    - 解析 PID（strconv.ParseUint）
    - `ipc.Dial(ipc.SocketPath())`，无 daemon 时输出友好错误
    - `client.CtxProfile(pid)` 获取结果
    - 若 `flagJSON`，输出 `JSONResponse{OK: true, Data: result}`；否则 `FormatCtxProfile(result)`
    - 错误格式：`[ctx-profile] error: ...`（文本）或 JSONResponse（--json）
  - [x] 5.4 daemon 不可用时输出明确错误（与 runKill 一致）；--json 时用 JSONResponse 包装错误

- [x] Task 6: 测试 (AC: #1-4)
  - [x] 6.1 `debug/ctx_profile_test.go`：AnalyzeContext 测试
    - 空上下文：Classification 全 0，TopConsumers 空，Suggestions 空
    - 仅 system prompt：Active 含 system，Cold 空
    - 10 条消息：验证 Active=最后 4，Warm=5～10，Cold=1～4
    - 含大工具结果（>1000 chars）在 Cold 区：Leaked 非空
    - 含大工具结果在 Active/Warm：不计入 Leaked
    - TopConsumers 排名正确，tool 名称提取（有 ToolCallID 或 content 模式）
    - Suggestions 触发条件：system>25%、tool>50%、leaked>0、cold>40%、near budget
  - [x] 6.2 `debug/ctx_profile_test.go`：FormatCtxProfile 测试
    - 验证段落标题、排名格式、百分比
    - 无 Suggestions 时无该段落
  - [x] 6.3 `debug/ctx_profile_test.go`：MarshalJSON 测试
    - snake_case、pct 一位小数
  - [x] 6.4 `ipc/server_test.go`：handleCtxProfile 集成测试
    - 有效 PID + Running → OK，result 非空
    - 有效 PID + Zombie → OK（若 context 仍存在）
    - 无效 PID → NOT_FOUND
    - 非 Running/Zombie 状态 → INVALID
  - [x] 6.5 `ipc/client_test.go`：CtxProfile 客户端测试（如有 mock 环境）
  - [x] 6.6 `cmd/rnix/ctx_profile_test.go`：CLI 集成测试
    - `ctx-profile <valid-pid>` → 输出含 "Classification" 和 "Top Consumers"
    - `ctx-profile <invalid-pid>` → 错误信息
    - `ctx-profile <pid> --json` → JSON 输出

## Dev Notes

### 架构决策

本 Story 是 Epic 15（分布式追踪与上下文分析）的第四层，实现上下文使用分析。核心设计原则：

1. **IPC 独立方法** — 使用 `MethodCtxProfile` 而非 `gdb_command`，因为 gdb 需要先 attach 并持有独占锁；ctx-profile 是轻量级只读查询，不应影响运行中进程
2. **分析逻辑与 IPC 分离** — `debug.AnalyzeContext` 在 `debug/ctx_profile.go` 中，可独立单元测试；Server 仅负责读取 context、调用分析、返回结果
3. **分类启发式** — MVP 使用固定窗口（Active=4, Warm=6），后续可演进为更精细的引用分析
4. **工具名提取** — 从 tool content 或 ToolCallID 推断，无法推断时用 `tool:unknown`，避免阻塞

### 关键设计：分类逻辑

```
Messages (从新到旧):
  [N]   assistant  ← Active (最后 4 条)
  [N-1] user
  [N-2] tool
  [N-3] assistant
  [N-4] user      ← Warm 开始
  ...
  [N-9] assistant ← Warm 结束
  [N-10] ...      ← Cold 开始
  ...
  [1]   tool (len>1000, 在 Cold 区) → Leaked
```

- Active = system_prompt + messages[len-4:len]
- Warm = messages[len-10:len-4]
- Cold = messages[0:len-10]
- Leaked = 在 Cold 区内的 role=tool 且 len(content)>1000 的消息

### 关键设计：消费者分析

- **system_prompt**：单独一项
- **user**：所有 user 消息的 token 汇总
- **assistant**：所有 assistant 消息的 token 汇总
- **tool**：每条 tool 消息单独统计，kind 为 `tool:<extracted_name>` 或 `tool:<tool_call_id>` 或 `tool:unknown`
- 工具名提取启发式：content 中匹配 `read_file`、`list_dir`、`grep` 等常见工具名；或 JSON 中的 `"name":"xxx"`；否则用 ToolCallID 前 8 字符

### 关键复用点

1. **context.Manager.CtxRead(cid, 0, 0)** — 读取完整上下文 JSON
2. **context.Manager.GetContextInfo** — 已有 token 估算逻辑（chars/4），可参考
3. **kernel.GetProcInfo(pid)** — 获取 CtxID、TokensUsed、ContextBudget、State
4. **ipc 模式** — 参考 handleListProcs、handleRecordList 等非流式请求
5. **JSONResponse** — `cmd/rnix/main.go` 统一包装
6. **runPs 错误处理** — daemon 不可用时的输出模式

### 不要做的事情

- **不要**使用 `gdb_command` + `inspect context` 实现 ctx-profile — 需要独立的 `ctx_profile` IPC 方法
- **不要**在 ctx-profile 中 attach gdb — 会阻塞目标进程
- **不要**修改 `context.Manager` 或 `Context` 的现有接口
- **不要**引入新的外部依赖
- **不要**实现 context 增长预测（Story 15-5 的范围）
- **不要**修改 trace/blame 命令 — ctx-profile 是独立顶级命令
- **不要**在 `internal/ui/` 中添加新文件 — 格式化放在 `debug/ctx_profile.go`
- **不要**忽略 NFR34 — 分析必须在 1s 内完成，大上下文时用 context.WithTimeout 保护

### IPC 协议变更

- 新增 `MethodCtxProfile = "ctx_profile"`
- 新增 `CtxProfileRequest`（Request payload）
- Response OK 时 Payload 为 `CtxProfileResult` 的 JSON
- Server 需持有 `ctxMgr` 引用（已有 `SetContextManager`）

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| ctx-profile 命令 | IPC daemon | 集成：通过新 IPC 方法查询上下文数据 | 是 |
| ctx-profile 命令 | context.Manager | 集成：CtxRead 读取上下文消息 | 是 |
| ctx-profile 命令 | GetProcInfo | 集成：获取进程 CtxID、TokensUsed、ContextBudget | 是 |
| ctx-profile --json | JSONResponse | 集成：使用统一 JSON 包装 | 是 |
| ctx-profile 命令 | ps 命令 | 独立：不同命令，可同时运行 | 否 |
| ctx-profile 命令 | gdb inspect | 独立：ctx-profile 不需要 gdb attach | 否 |
| ctx-profile 命令 | trace blame | 独立：不同分析维度 | 否 |
| ctx-profile 分析 | 15-5 增长预测 | 预留：CtxProfileResult 可供 15-5 复用 | 否 |

### Project Structure Notes

新建文件：
- `debug/ctx_profile.go` — CtxProfileResult、ClassificationResult、ConsumerEntry、AnalyzeContext、FormatCtxProfile、MarshalJSON
- `debug/ctx_profile_test.go` — 分析、格式化、JSON 测试
- `cmd/rnix/ctx_profile.go` — ctxProfileCmd、runCtxProfile
- `cmd/rnix/ctx_profile_test.go` — CLI 集成测试

修改文件：
- `ipc/protocol.go` — MethodCtxProfile、CtxProfileRequest
- `ipc/server.go` — handleConn 分支、handleCtxProfile
- `ipc/client.go` — CtxProfile 方法
- `ipc/server_test.go` — handleCtxProfile 测试
- `cmd/rnix/main.go` — init 中 rootCmd.AddCommand(ctxProfileCmd)

### References

- [Source: context/context.go:176-216] — CtxRead(cid, 0, 0) 返回 system_prompt + messages JSON
- [Source: context/context.go:354-408] — GetContextInfo token 估算（chars/4）
- [Source: context/context.go:26-31] — Message 结构 Role、Content、ToolCallID
- [Source: ipc/server.go:777-806] — handleGdbInspect 使用 ctxMgr.GetContextInfo
- [Source: ipc/protocol.go:18-35] — Method 常量定义模式
- [Source: ipc/server.go:283-291] — handleListProcs、handleRecordList 非流式模式
- [Source: kernel/kernel.go:1048-1060] — GetProcInfo 返回 ProcInfo
- [Source: vfs/proc.go:28-43] — ProcInfo 含 CtxID、TokensUsed、ContextBudget、State
- [Source: cmd/rnix/main.go:66-71] — JSONResponse 结构
- [Source: cmd/rnix/main.go:743-751] — runPs daemon 不可用时的行为
- [Source: debug/trace_blame.go:14-45] — BlameResult 结构参考
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md:64] — NFR34: ctx-profiler 分析延迟 ≤ 1s
- [Source: _bmad-output/planning-artifacts/epics/epic-15-分布式追踪与上下文分析-distributed-tracing-context-analysis.md] — Story 15.4 需求

### 技术栈

- Go 1.26 — 标准库满足所有需求
- `encoding/json` — 解析 CtxRead 输出、CtxProfileResult 序列化
- `fmt` / `strings` — 格式化输出
- `sort` — TopConsumers 排序
- `context` — WithTimeout 保证 NFR34
- `github.com/spf13/cobra` — CLI 命令注册
- 无新增外部依赖

### 前置 story 学习总结

**来自 Story 15-3：**
1. BlameResult.MarshalJSON 使用 snake_case + _ms 后缀
2. blame 子命令通过 `traceCmd.AddCommand(blameCmd)` 注册；ctx-profile 是顶级命令，用 `rootCmd.AddCommand(ctxProfileCmd)`
3. FormatBlameResult 使用文本符号（→、✗、↑、#）；FormatCtxProfile 使用 ← 标注
4. 分析逻辑在 debug 包，CLI 在 cmd/rnix
5. 错误格式：`[command] error: ...` 文本，JSONResponse 用于 --json
6. 测试：debug/*_test.go 单元测试，cmd/rnix/*_test.go 集成测试

**来自 Story 13-3（inspect context）：**
1. handleGdbInspect 通过 ctxMgr.GetContextInfo 获取聚合信息；ctx-profile 需要完整消息，用 CtxRead
2. gdb 需要 attach，独占锁；ctx-profile 不 attach，只读查询
3. ProcInfo 含 CtxID，用于 CtxRead 入参

**来自 Story 4-4（ps 命令）：**
1. runPs 在 daemon 不可用时：--json 返回 `{"ok":true,"data":{"processes":[]}}`，否则输出 "No active processes."
2. ctx-profile 在 daemon 不可用时应输出明确错误，因为无法查询任意 PID 的 context

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

- debug/ctx_profile_test.go: 21 个测试全部通过（AnalyzeContext 分类、TopConsumers 排名、Suggestions 生成、FormatCtxProfile 格式化、MarshalJSON 序列化）
- cmd/rnix/ctx_profile_test.go: 5 个 CLI 测试全部通过（命令注册、PID 校验、daemon 不可用、JSON 错误输出）
- 全项目 18 包测试通过（-race 检测），2 个预存 TTY 测试失败不受影响

### Completion Notes List

- AnalyzeContext 实现上下文分类：Active（系统提示 + 最后 4 条消息）、Warm（接下来 6 条）、Cold（更早）、Leaked（冷区大工具结果）
- findTopConsumers 按 token 降序排名 Top-5，支持工具名提取（已知工具名、ToolCallID、unknown 回退）
- generateSuggestions 基于 5 种阈值触发优化建议
- FormatCtxProfile 使用 ← 标注分类含义、#N 排名格式、段落结构化输出
- MarshalJSON 使用 snake_case 字段、空数组而非 null
- IPC: 新增 MethodCtxProfile、CtxProfileRequest，server handleCtxProfile 校验进程状态后读取 CtxRead 并调用 AnalyzeContext
- CLI: ctx-profile 作为 rootCmd 顶级命令，支持 --json，daemon 不可用时友好错误

### File List

新建文件:
- `debug/ctx_profile.go` — CtxProfileResult、ClassificationResult、ConsumerEntry 类型；AnalyzeContext、FormatCtxProfile、MarshalJSON 及内部分析函数
- `debug/ctx_profile_test.go` — 21 个上下文分析测试
- `cmd/rnix/ctx_profile.go` — ctxProfileCmd、runCtxProfile CLI 命令
- `cmd/rnix/ctx_profile_test.go` — 5 个 CLI 集成测试

修改文件:
- `ipc/protocol.go` — 新增 MethodCtxProfile、CtxProfileRequest
- `ipc/server.go` — 新增 handleCtxProfile 处理器、handleConn switch 分支、debug 包导入
- `ipc/client.go` — 新增 CtxProfile 方法、debug 包导入
