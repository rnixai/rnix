---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
  - 'step-04c-aggregate'
  - 'step-05-validate-and-complete'
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-02'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/10-2-crux-log-categorized-reasoning-logs.md'
  - '_bmad-output/implementation-artifacts/sprint-status.yaml'
  - 'internal/types/types.go'
  - 'kernel/process.go'
  - 'kernel/kernel.go'
  - 'kernel/reap.go'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'ipc/client.go'
  - 'cmd/crux/main.go'
---

# ATDD 清单 - Epic 10, Story 10.2: crux log 分类推理日志

**日期:** 2026-03-02
**作者:** Decker
**主要测试级别:** Unit + Integration (Go backend)
**堆栈类型:** backend (Go 1.26)

---

## 故事摘要

通过 `crux log <pid>` 查看智能体的推理日志，按 `[think]`、`[tool]`、`[output]` 三段式分类显示，支持过滤、JSON 输出和实时流式。

**As a** 用户
**I want** 通过 `crux log <pid>` 查看智能体的推理日志，按类别分类显示
**So that** 我无需深入内核就能排查问题

---

## 验收标准

1. **AC1: 基本日志输出** — 执行 `crux log 5` 输出 PID 5 的推理日志，按 `[think]`/`[tool]`/`[output]` 三段式分类
2. **AC2: 过滤功能** — `crux log 5 --filter tool` 仅显示 `[tool]` 类别的日志
3. **AC3: 低延迟** — 从推理事件到终端显示延迟 ≤ 200ms
4. **AC4: PID 不存在处理** — `crux log 999` 输出 `✗ PID 999: process not found` + 建议
5. **AC5: JSON 输出** — `crux log 5 --json` 输出 NDJSON 格式（每行一个 JSON 对象）
6. **AC6: 实时流式** — 实时流式输出新产生的日志条目，进程退出后自动断开
7. **AC7: Ctrl+C 安全断开** — 按 Ctrl+C 断开日志流，不影响被追踪进程

---

## 测试策略

| AC | 描述 | 测试级别 | 优先级 | 测试 ID 范围 |
|----|------|---------|--------|-------------|
| AC1 | 基本日志输出 | Unit + Integration | P0 | 10.2-UNIT-001~017, 10.2-INTEG-002 |
| AC2 | 过滤功能 | Unit | P1 | 10.2-UNIT-030~032 |
| AC3 | 低延迟 | Unit (non-blocking) | P1 | 10.2-UNIT-011 |
| AC4 | PID 不存在 | Unit + Integration | P1 | 10.2-UNIT-014, 10.2-INTEG-001 |
| AC5 | JSON 输出 | Unit | P1 | 10.2-UNIT-020~026, 10.2-UNIT-035~036 |
| AC6 | 实时流式 | Integration | P0 | 10.2-INTEG-002~004 |
| AC7 | Ctrl+C 断开 | (运行时验证) | P1 | — |

---

## 失败测试清单 (RED Phase)

### 类型层测试 (5 tests)

**文件:** `internal/types/log_test.go` (98 行)

- **10.2-UNIT-001:** `TestLogCategory_Constants` — 验证 LogThink/LogTool/LogOutput 常量值
  - **状态:** RED — LogCategory 类型不存在
  - **验证:** AC1 — 三段式分类常量定义
- **10.2-UNIT-002:** `TestLogCategory_StringType` — 验证 LogCategory 是 string 基类型
  - **状态:** RED — LogCategory 类型不存在
  - **验证:** AC1 — 类型系统正确性
- **10.2-UNIT-003:** `TestLogEntry_ThinkFields` — 验证 LogEntry think 类别字段
  - **状态:** RED — LogEntry 结构体不存在
  - **验证:** AC1 — 数据模型完整性
- **10.2-UNIT-004:** `TestLogEntry_ToolFields` — 验证 LogEntry tool 类别包含 ToolPath
  - **状态:** RED — LogEntry 结构体不存在
  - **验证:** AC1 — tool 类别专有字段
- **10.2-UNIT-005:** `TestLogEntry_OutputFields` — 验证 LogEntry output 类别字段
  - **状态:** RED — LogEntry 结构体不存在
  - **验证:** AC1 — output 类别数据

### 内核层测试 (12 tests)

**文件:** `kernel/log_test.go` (243 行)

- **10.2-UNIT-006:** `TestNewProcess_LogChan` — 验证 NewProcess 初始化 LogChan (buf 256)
  - **状态:** RED — Process.LogChan 字段不存在
  - **验证:** AC1, AC3 — 通道基础设施
- **10.2-UNIT-007:** `TestEmitLog_SendsEntry` — 验证 emitLog 发送正确的 LogEntry
  - **状态:** RED — emitLog 方法不存在
  - **验证:** AC1 — 基本日志 emit 机制
- **10.2-UNIT-008:** `TestEmitLog_ToolCategory` — 验证 emitLog tool 类别保留 ToolPath
  - **状态:** RED — emitLog 方法不存在
  - **验证:** AC1 — tool 分类正确性
- **10.2-UNIT-009:** `TestEmitLog_OutputCategory` — 验证 emitLog output 类别
  - **状态:** RED — emitLog 方法不存在
  - **验证:** AC1 — output 分类正确性
- **10.2-UNIT-010:** `TestEmitLog_TimestampRelative` — 验证时间戳相对于进程创建时间
  - **状态:** RED — emitLog 方法不存在
  - **验证:** AC1 — 时间戳准确性
- **10.2-UNIT-011:** `TestEmitLog_NonBlocking_BufferFull` — 验证缓冲区满时不阻塞
  - **状态:** RED — emitLog 方法不存在
  - **验证:** AC3 — 低延迟保障（非阻塞写入）
- **10.2-UNIT-012:** `TestEmitLog_NilLogChan` — 验证 LogChan 为 nil 时安全
  - **状态:** RED — emitLog 方法不存在
  - **验证:** AC1 — 边界情况安全性
- **10.2-UNIT-013:** `TestGetLogChan_ValidPID` — 验证有效 PID 返回通道
  - **状态:** RED — GetLogChan 方法不存在
  - **验证:** AC1, AC6 — 通道获取
- **10.2-UNIT-014:** `TestGetLogChan_InvalidPID` — 验证无效 PID 返回 false
  - **状态:** RED — GetLogChan 方法不存在
  - **验证:** AC4 — PID 不存在处理
- **10.2-UNIT-015:** `TestGetLogChan_NilAfterClose` — 验证 nil-out 后返回 false
  - **状态:** RED — GetLogChan 方法不存在
  - **验证:** AC6 — 进程退出后的状态
- **10.2-UNIT-016:** `TestReapProcess_ClosesLogChan` — 验证 reapProcess 关闭 LogChan
  - **状态:** RED — LogChan 关闭逻辑未实现
  - **验证:** AC6 — 进程退出自动断开
- **10.2-UNIT-017:** `TestLogChan_IndependentOfDebugChan` — 验证 LogChan 与 DebugChan 独立
  - **状态:** RED — emitLog/LogChan 不存在
  - **验证:** AC1 — 通道隔离

### IPC 协议层测试 (13 tests)

**文件:** `ipc/log_test.go` (292 行)

**单元测试:**

- **10.2-UNIT-018:** `TestLogEntryToWire` — 验证 LogEntry → LogEntryWire 转换
  - **状态:** RED — LogEntryToWire 函数不存在
  - **验证:** AC1, AC5 — Wire 格式转换
- **10.2-UNIT-019:** `TestLogEntryToWire_ToolPath` — 验证 tool 类别保留 ToolPath
  - **状态:** RED — LogEntryToWire 函数不存在
  - **验证:** AC1 — tool 专有字段传输
- **10.2-UNIT-020:** `TestLogEntryWire_JSONRoundTrip` — 验证 JSON 序列化往返一致
  - **状态:** RED — LogEntryWire 类型不存在
  - **验证:** AC5 — JSON 输出正确性
- **10.2-UNIT-021:** `TestLogEntryWire_ToolPathOmitEmpty` — 验证空 ToolPath 被 JSON 省略
  - **状态:** RED — LogEntryWire 类型不存在
  - **验证:** AC5 — JSON 紧凑性
- **10.2-UNIT-022:** `TestAttachLogRequest_MarshalRoundTrip` — 验证请求序列化
  - **状态:** RED — AttachLogRequest 类型不存在
  - **验证:** AC1 — IPC 请求格式
- **10.2-UNIT-023:** `TestMethodAttachLog_Constant` — 验证方法常量值
  - **状态:** RED — MethodAttachLog 常量不存在
  - **验证:** AC1 — IPC 协议定义
- **10.2-UNIT-024:** `TestStreamLogEntry_Constant` — 验证流事件类型常量
  - **状态:** RED — StreamLogEntry 常量不存在
  - **验证:** AC6 — 流式协议定义
- **10.2-UNIT-025:** `TestMethodAttachLog_Unique` — 验证 Method 常量唯一性
  - **状态:** RED — MethodAttachLog 常量不存在
  - **验证:** AC1 — 协议冲突预防
- **10.2-UNIT-026:** `TestStreamLogEntry_Unique` — 验证 StreamEventType 常量唯一性
  - **状态:** RED — StreamLogEntry 常量不存在
  - **验证:** AC6 — 流类型冲突预防

**集成测试:**

- **10.2-INTEG-001:** `TestHandleAttachLog_NotFound` — 验证 PID 不存在返回 NOT_FOUND
  - **状态:** RED — handleAttachLog 未实现
  - **验证:** AC4 — PID 不存在处理
- **10.2-INTEG-002:** `TestIntegration_AttachLog_ReceivesEntries` — 验证接收 3 个有序条目
  - **状态:** RED — AttachLog 未实现
  - **验证:** AC1, AC6 — 端到端日志流
- **10.2-INTEG-003:** `TestIntegration_AttachLog_EOFOnClose` — 验证通道关闭触发 EOF
  - **状态:** RED — AttachLog 未实现
  - **验证:** AC6 — 进程退出后自动断开
- **10.2-INTEG-004:** `TestIntegration_AttachLog_WireTimestamp` — 验证 Wire 时间戳毫秒精度
  - **状态:** RED — AttachLog 未实现
  - **验证:** AC1, AC5 — 时间戳传输精度

### CLI 命令层测试 (11 tests)

**文件:** `cmd/crux/log_test.go` (186 行)

- **10.2-UNIT-027:** `TestLogCmd_Registered` — 验证 logCmd 注册到 rootCmd
  - **状态:** RED — log.go 不存在
  - **验证:** AC1 — 命令可用性
- **10.2-UNIT-028:** `TestLogCmd_HasFilterFlag` — 验证 --filter flag 存在
  - **状态:** RED — log.go 不存在
  - **验证:** AC2 — 过滤功能入口
- **10.2-UNIT-029:** `TestLogCmd_HasJSONFlag` — 验证 --json flag 存在
  - **状态:** RED — log.go 不存在
  - **验证:** AC5 — JSON 输出入口
- **10.2-UNIT-030:** `TestIsValidLogFilter_Valid` — 验证合法过滤值通过
  - **状态:** RED — isValidLogFilter 不存在
  - **验证:** AC2 — 过滤值校验
- **10.2-UNIT-031:** `TestIsValidLogFilter_Invalid` — 验证非法过滤值拒绝
  - **状态:** RED — isValidLogFilter 不存在
  - **验证:** AC2 — 输入验证
- **10.2-UNIT-032:** `TestShouldShowEntry_Filter` — 验证过滤逻辑（表驱动测试）
  - **状态:** RED — shouldShowEntry 不存在
  - **验证:** AC2 — 过滤核心逻辑
- **10.2-UNIT-033:** `TestFormatLogEntry_ContainsCategory` — 验证人类可读输出含分类标签
  - **状态:** RED — formatLogEntry 不存在
  - **验证:** AC1 — 分类显示
- **10.2-UNIT-034:** `TestFormatLogEntry_Timestamp` — 验证输出包含格式化时间戳
  - **状态:** RED — formatLogEntry 不存在
  - **验证:** AC1 — 时间戳显示
- **10.2-UNIT-035:** `TestFormatLogEntryJSON_ValidJSON` — 验证 JSON 输出有效
  - **状态:** RED — formatLogEntryJSON 不存在
  - **验证:** AC5 — JSON 格式正确性
- **10.2-UNIT-036:** `TestFormatLogEntryJSON_NDJSON` — 验证 NDJSON 每行一个 JSON
  - **状态:** RED — formatLogEntryJSON 不存在
  - **验证:** AC5 — NDJSON 格式
- **10.2-UNIT-037:** `TestLogCmd_RequiresOneArg` — 验证命令需要恰好 1 个参数
  - **状态:** RED — logCmd 不存在
  - **验证:** AC1 — 参数校验

---

## 测试统计

| 级别 | 数量 | 文件 |
|------|------|------|
| Unit (types) | 5 | `internal/types/log_test.go` |
| Unit (kernel) | 12 | `kernel/log_test.go` |
| Unit (ipc protocol) | 9 | `ipc/log_test.go` |
| Integration (ipc) | 4 | `ipc/log_test.go` |
| Unit (cli) | 11 | `cmd/crux/log_test.go` |
| **合计** | **41** | **4 文件** |

---

## Mock 需求

### 无外部服务 Mock 需求

Story 10.2 不涉及外部服务。所有交互通过：
- 内核进程表（直接访问）
- Unix socket IPC（进程内集成测试）

测试使用 `setupTestServer` / `setupIntegrationServer` 创建真实的 IPC 服务器和内核实例，发送真实的 LogEntry 到 LogChan。

---

## 实现清单

### Phase 1: 类型定义 (使测试可编译)

**目标:** 添加类型桩使 37 个单元测试可编译

- [ ] 在 `internal/types/types.go` 添加 `LogCategory` 类型和 `LogThink`/`LogTool`/`LogOutput` 常量
- [ ] 在 `internal/types/types.go` 添加 `LogEntry` 结构体（Timestamp, PID, Step, Category, Content, ToolPath）
- [ ] 在 `kernel/process.go` 的 `Process` 结构体添加 `LogChan chan types.LogEntry` 字段
- [ ] 在 `kernel/process.go` 的 `NewProcess` 中初始化 `LogChan: make(chan types.LogEntry, 256)`
- [ ] 在 `ipc/protocol.go` 添加 `MethodAttachLog`、`AttachLogRequest`、`LogEntryWire`、`StreamLogEntry`、`LogEntryToWire`
- [ ] 创建 `cmd/crux/log.go` 空壳（logCmd 定义 + 辅助函数签名）
- [ ] 运行: `go build ./...` — 验证编译通过
- [ ] 运行: `go test ./internal/types/ ./ipc/ -run "10.2" -count=1` — 验证类型测试通过

### Phase 2: 内核 emitLog + GetLogChan

**目标:** 使内核层测试通过 (10.2-UNIT-006 ~ 017)

- [ ] 在 `kernel/kernel.go` 添加 `emitLog(proc, step, cat, content, toolPath)` 方法
- [ ] 在 `kernel/kernel.go` 添加 `GetLogChan(pid) (chan types.LogEntry, bool)` 方法
- [ ] 在 `kernel/reap.go` 的 `reapProcess` 添加 LogChan nil-out-under-lock + close 逻辑
- [ ] 运行: `go test ./kernel/ -run "10.2" -count=1 -v`
- [ ] 所有 12 个内核测试通过 (GREEN)

### Phase 3: IPC 协议 + 服务端 + 客户端

**目标:** 使 IPC 层测试通过 (10.2-UNIT-018 ~ 026, 10.2-INTEG-001 ~ 004)

- [ ] 在 `ipc/server.go` 的 `handleConn` switch 添加 `case MethodAttachLog` + `return`
- [ ] 在 `ipc/server.go` 实现 `handleAttachLog`（与 handleAttachDebug 对齐）
- [ ] 在 `ipc/client.go` 添加 `AttachLog(pid, onEntry func(LogEntryWire)) error`
- [ ] 运行: `go test ./ipc/ -run "10.2" -count=1 -v`
- [ ] 所有 13 个 IPC 测试通过 (GREEN)

### Phase 4: CLI 命令

**目标:** 使 CLI 层测试通过 (10.2-UNIT-027 ~ 037)

- [ ] 创建 `cmd/crux/log.go` — logCmd cobra 命令定义
- [ ] 实现 `--filter` 和 `--json` flag
- [ ] 实现 `isValidLogFilter(filter string) bool`
- [ ] 实现 `shouldShowEntry(entry LogEntryWire, filter string) bool`
- [ ] 实现 `formatLogEntry(entry LogEntryWire) string`（人类可读格式）
- [ ] 实现 `formatLogEntryJSON(entry LogEntryWire) string`（NDJSON 格式）
- [ ] 实现 `runLog` 完整命令处理（解析 PID、Dial、信号处理、AttachLog）
- [ ] 在 `cmd/crux/main.go` 的 `init()` 中注册 `logCmd`
- [ ] 运行: `go test ./cmd/crux/ -run "10.2" -count=1 -v`
- [ ] 所有 11 个 CLI 测试通过 (GREEN)

### Phase 5: 内核 reasonStep 集成

**目标:** 在 reasonStep 中 emit 日志条目

- [ ] 在 `kernel/kernel.go` 的 `reasonStep` 中 LLM 响应后 emit `[think]` 条目
- [ ] 在 `kernel/kernel.go` 的 `reasonStep` 中工具调用后 emit `[tool]` 条目
- [ ] 在 `kernel/kernel.go` 的 `reasonStep` 中最终输出时 emit `[output]` 条目
- [ ] 工具 Content 截断到 500 字符 + `... (truncated, N bytes total)`
- [ ] 运行: `go test ./... -count=1` — 全部测试通过

### Phase 6: 端到端验证

- [ ] 启动 crux daemon (`crux spawn "分析 README"`)
- [ ] 执行 `crux log <pid>` — 验证分类输出
- [ ] 执行 `crux log <pid> --filter tool` — 验证过滤
- [ ] 执行 `crux log <pid> --json` — 验证 NDJSON
- [ ] 执行 `crux log 999` — 验证 PID 不存在错误
- [ ] 按 Ctrl+C — 验证安全断开
- [ ] 运行: `go test ./... -count=1` — 完整回归

---

## Red-Green-Refactor 工作流

### RED Phase (完成) ✅

**TEA Agent 职责:**

- ✅ 41 个测试已编写，全部处于 RED 状态
- ✅ 测试文件结构合理（4 个文件，按架构层分离）
- ✅ 实现清单已创建（6 个阶段）
- ✅ 所有验收标准已映射到测试
- ✅ 测试遵循项目现有模式（表驱动、t.Helper()、t.Cleanup()）

**验证:**

- 测试引用不存在的类型/函数 → 编译失败（Go TDD RED Phase）
- 编译错误消息明确指向缺失的实现
- 测试设计基于故事 Dev Notes 中的架构约束

---

### GREEN Phase (DEV 团队 — 下一步)

**DEV Agent 职责:**

1. **Phase 1:** 添加类型桩使测试可编译
2. **Phase 2:** 实现 emitLog + GetLogChan + reapProcess LogChan 关闭
3. **Phase 3:** 实现 IPC 协议扩展 + server handler + client method
4. **Phase 4:** 实现 CLI 命令 + 格式化函数
5. **Phase 5:** 在 reasonStep 中集成 emit 调用
6. **Phase 6:** 端到端验证

**关键原则:**

- 一次一个阶段（每个阶段对应一组测试）
- 每完成一个阶段就运行对应测试验证
- 最小实现（先让测试通过，再优化）

---

### REFACTOR Phase (DEV 团队 — 所有测试通过后)

1. **验证所有 41 个测试通过** (green phase complete)
2. **审查代码质量** — emitLog 是否与 emitEvent 对齐
3. **提取重复** — formatLogEntry 的颜色逻辑是否可复用
4. **性能优化** — LogChan 缓冲是否足够
5. **确保测试仍通过**
6. **更新文档**

---

## 运行测试

```bash
# 运行所有 Story 10.2 测试 (需先完成 Phase 1 使代码可编译)
go test ./internal/types/ ./kernel/ ./ipc/ ./cmd/crux/ -run "10\.2|Log" -count=1 -v

# 运行特定文件
go test ./internal/types/ -run "LogCategory|LogEntry" -count=1 -v
go test ./kernel/ -run "EmitLog|GetLogChan|ReapProcess_Closes|LogChan" -count=1 -v
go test ./ipc/ -run "LogEntry|AttachLog|HandleAttachLog" -count=1 -v
go test ./cmd/crux/ -run "LogCmd|LogFilter|FormatLog|ShouldShow" -count=1 -v

# 运行全部测试（回归）
go test ./... -count=1

# 运行带覆盖率
go test ./internal/types/ ./kernel/ ./ipc/ ./cmd/crux/ -run "10\.2|Log" -count=1 -cover
```

---

## 知识库引用

- **test-quality.md** — 测试质量原则（确定性、隔离性、原子性）
- **data-factories.md** — 数据工厂模式（Go 测试中使用 helper 函数构造测试数据）
- **test-levels-framework.md** — 测试级别选择（Unit vs Integration，避免重复覆盖）
- **test-priorities-matrix.md** — P0-P3 优先级框架
- **test-healing-patterns.md** — 常见失败模式和修复（非阻塞写入、超时防护）

---

## 风险和假设

### 风险

1. **R1: LogChan 单消费者限制** — MVP 阶段与 DebugChan 对齐，只支持单消费者。多个 `crux log` 连接同一 PID 时第二个看不到事件。后续可考虑 fan-out。
2. **R2: 工具输出截断** — `[tool]` 条目 Content 截断到 500 字符，可能丢失调试信息。需要在截断后追加 `... (truncated, N bytes total)`。

### 假设

1. `FormatDuration` 已在 `internal/ui/table.go` 导出（Story 10-1 已完成）
2. `internal/ui/styles.go` 的颜色常量可直接使用
3. reasonStep 的执行流是同步的（emit 时机明确）

---

## 下一步

1. **开发者领取 Story 10-2** — 将状态从 `ready-for-dev` 改为 `in-progress`
2. **按 Phase 1-6 顺序实现** — 每个阶段运行对应测试验证
3. **所有 41 个测试通过后** — 进行 REFACTOR Phase
4. **重构完成后** — 更新 sprint-status.yaml 状态为 `done`

---

## 备注

- Go TDD RED Phase = 编译失败（引用不存在的类型/函数），不同于 JS 的 `test.skip()` 模式
- 测试文件遵循项目惯例：`_test.go` 在同包内，使用现有测试 helper（`newSimpleKernel`、`setupTestServer`、`setupIntegrationServer`）
- AC7 (Ctrl+C 安全断开) 需要运行时手动验证，无法纯粹通过单元测试覆盖

---

**Generated by BMad TEA Agent** — 2026-03-02
