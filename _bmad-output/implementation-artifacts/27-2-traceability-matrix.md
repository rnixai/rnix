# Traceability Matrix — Story 27.2: GetStepDetail IPC 方法

**Generated:** 2026-03-21
**Story Status:** done
**Test Suite:** `ipc/atdd_27_2_getstepdetail_test.go`
**Test Execution:** 14/14 PASS (race detector enabled, 1.032s)

---

## 1. Requirements → Implementation Traceability

| AC | 需求描述 | 实现文件 | 实现位置 |
|----|----------|----------|----------|
| AC-1 | IPC 协议新增 GetStepDetail 方法 + Wire 类型 | `ipc/protocol.go` | L53 (Method), L593-636 (6 structs) |
| AC-2 | Server handler — 进程仍在运行 | `ipc/server.go` | L1320-1388 (handler), L1391-1402, L1431-1468 (helpers) |
| AC-3 | Server handler — 进程已死但仍在内存 | `ipc/server.go` | L1320-1388 (same handler, zombie path) |
| AC-4 | Server handler — 进程已被 reaper 清理 | `ipc/server.go` | L1320-1388 (disk fallback), L1405-1412, L1419-1429 |
| AC-5 | 错误处理 — PID 不存在 | `ipc/server.go` | L1320-1388 (not_found error branch) |
| AC-6 | 错误处理 — Step 超出范围 | `ipc/server.go` | L1320-1388 (step not_found branch) |
| AC-7 | Client 方法 | `ipc/client.go` | L505-516 |
| AC-8 | 性能要求 ≤500ms | `ipc/server.go` + `kernel/step_writer.go` | ReadStep sequential scan |

### 辅助实现

| 组件 | 文件 | 位置 | 说明 |
|------|------|------|------|
| GetStepDataDir() getter | `kernel/kernel.go` | L272-275 | 新增，为 Server fallback 路径解析提供 stepDataDir |
| GetFinalSystemPrompt() | `kernel/process.go` | L170-175 | 新增，线程安全读取 FinalSystemPrompt |
| GetNativeToolDefs() | `kernel/process.go` | L177-180 | 新增，读取 nativeToolDefs（Spawn 后不可变） |
| GetProjectConfig() | `kernel/process.go` | L182-185 | 新增，读取 ProjectConfig（Spawn 后不可变） |
| SetFinalSystemPrompt() | `kernel/process.go` | — | 测试辅助 setter |
| SetNativeToolDefs() | `kernel/process.go` | — | 测试辅助 setter |
| Finish() | `kernel/process.go` | — | 测试辅助：模拟进程完成 |
| switch case 路由 | `ipc/server.go` | L357-358 | MethodGetStepDetail → handleGetStepDetail |

---

## 2. Requirements → Tests Traceability Matrix

| AC | 测试函数 | 验证内容 | 结果 |
|----|----------|----------|------|
| **AC-1** | `TestATDD_27_2_AC1_MethodConstant` | MethodGetStepDetail == "get_step_detail" | PASS |
| **AC-1** | `TestATDD_27_2_AC1_GetStepDetailRequest_Serialization` | Request JSON 序列化/反序列化 roundtrip | PASS |
| **AC-1** | `TestATDD_27_2_AC1_GetStepDetailResponse_Serialization` | Response 全字段 JSON roundtrip（含 ToolDurationMs） | PASS |
| **AC-1** | `TestATDD_27_2_AC1_MessageWire_WithToolCalls` | MessageWire + ToolCallWire roundtrip | PASS |
| **AC-1** | `TestATDD_27_2_AC1_MessageWire_ToolResult` | Tool result MessageWire (ToolCallID) roundtrip | PASS |
| **AC-1** | `TestATDD_27_2_AC1_ToolDefWire_Serialization` | ToolDefWire JSON roundtrip | PASS |
| **AC-2** | `TestATDD_27_2_AC2_RunningProcess` | Running 进程: SystemPrompt, Tools, Step, Action, MessageCount, Tokens | PASS |
| **AC-2** | `TestATDD_27_2_AC2_MessagesConversion` | json.RawMessage → []MessageWire 三种角色转换 (user/assistant+toolcalls/tool) | PASS |
| **AC-3** | `TestATDD_27_2_AC3_ZombieProcess` | Zombie 进程: SystemPrompt, Step 正确返回 | PASS |
| **AC-4** | `TestATDD_27_2_AC4_ReapedProcess` | Reaped 进程: 从磁盘 process-meta.json + steps.jsonl 读取 | PASS |
| **AC-5** | `TestATDD_27_2_AC5_PIDNotFound` | 不存在 PID: error code == "not_found" | PASS |
| **AC-6** | `TestATDD_27_2_AC6_StepNotFound` | Step 越界: error code == "not_found" | PASS |
| **AC-7** | `TestATDD_27_2_AC7_ClientMethod` | Client.GetStepDetail() 完整 roundtrip (IPC client → server → response) | PASS |
| **AC-8** | `TestATDD_27_2_AC8_Performance` | 30-step 文件查询 ≤ 500ms (NFR61) | PASS |

---

## 3. Coverage Analysis

### AC 覆盖率汇总

| AC | 测试数 | 覆盖等级 | 说明 |
|----|--------|----------|------|
| AC-1 | 6 | **Full** | 全部 Wire 类型均有独立序列化测试 |
| AC-2 | 2 | **Full** | 基础路径 + Messages 深度转换 |
| AC-3 | 1 | **Full** | Zombie/Dead 状态验证 |
| AC-4 | 1 | **Full** | 磁盘 fallback 路径（meta + steps） |
| AC-5 | 1 | **Full** | 错误码验证 |
| AC-6 | 1 | **Full** | 错误码验证 |
| AC-7 | 1 | **Full** | 端到端 Client roundtrip |
| AC-8 | 1 | **Full** | NFR61 性能阈值验证 |

**总计: 8/8 AC 完全覆盖, 14 个测试函数, 0 个 GAP**

### 测试层级分布

| 层级 | 数量 | 说明 |
|------|------|------|
| 单元测试（Wire 类型序列化） | 6 | AC-1 纯数据结构验证 |
| 集成测试（IPC Server roundtrip） | 7 | AC-2~AC-8 通过真实 Unix socket 通信 |
| 端到端测试（Client API） | 1 | AC-7 Client → Server → Response 全链路 |

### 测试质量评估

| 维度 | 评分 | 说明 |
|------|------|------|
| AC 覆盖完整性 | 10/10 | 每个 AC 至少 1 个测试，AC-1/AC-2 有多个 |
| 正向路径 | 10/10 | Running/Zombie/Reaped 三种进程状态均覆盖 |
| 错误路径 | 10/10 | PID 不存在 + Step 越界 |
| 边界条件 | 8/10 | Messages 含 user/assistant(+toolcalls)/tool 三角色；缺 nil Messages、空 ToolDefs 边界 |
| 并发安全 | 9/10 | 使用 `-race` flag 运行，覆盖 mu.Lock 路径 |
| 性能 | 10/10 | NFR61 显式验证 |
| 命名规范 | 10/10 | ATDD 命名清晰映射到 AC 编号 |

---

## 4. Implementation File Inventory

| 文件 | 变更类型 | 涉及 AC |
|------|----------|---------|
| `ipc/protocol.go` | Modified — 新增 1 常量 + 6 结构体 | AC-1 |
| `ipc/server.go` | Modified — 新增 handler + 5 辅助函数 + switch case | AC-2, AC-3, AC-4, AC-5, AC-6 |
| `ipc/client.go` | Modified — 新增 1 方法 | AC-7 |
| `kernel/kernel.go` | Modified — 新增 1 getter | AC-2, AC-4 (路径解析) |
| `kernel/process.go` | Modified — 新增 6 exported 方法 | AC-2, AC-3 (数据访问) |
| `ipc/atdd_27_2_getstepdetail_test.go` | New — 14 测试 + 2 helper | AC-1~AC-8 |

---

## 5. Quality Gate Decision

### Decision: **PASS**

| 准则 | 状态 | 备注 |
|------|------|------|
| 所有 AC 有对应测试 | **PASS** | 8/8 AC 全覆盖 |
| 所有测试通过 | **PASS** | 14/14 PASS |
| Race detector 无报警 | **PASS** | `-race` 模式运行 |
| 错误路径覆盖 | **PASS** | AC-5, AC-6 |
| NFR 验证 | **PASS** | AC-8 (≤500ms) |
| 无未实现的 Task | **PASS** | 5/5 Tasks 完成 |
| `make all` 通过 | **PASS** | 22 包全绿 (记录于 story) |

### Minor Observations (non-blocking)

1. **边界测试可增强**: `nil` Messages 和空 ToolDefs 的防御式编程逻辑在代码中存在，但无显式测试。可在未来补充。
2. **AC-4 可补充**: 测试 `process-meta.json` 中 `tool_defs` 为 null 的场景（进程无 native tools）。
3. **AC-8 仅验证 30 步**: 对于 >30 步的极端场景无验证，但 story 定义的 NFR 范围即为 30 步。

**结论**: Story 27.2 的实现完全满足所有 8 个 AC 和 NFR61 性能要求，测试覆盖充分且命名规范，质量门 **PASS**。
