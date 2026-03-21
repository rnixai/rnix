---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation']
lastStep: 'step-02-generation'
lastSaved: '2026-03-21'
storyId: '27-2'
storyTitle: 'GetStepDetail IPC 方法'
detectedStack: 'backend'
testFramework: 'go test -race'
inputDocuments:
  - _bmad-output/implementation-artifacts/27-2-getstepdetail-ipc-method.md
  - ipc/protocol.go
  - ipc/server.go
  - ipc/client.go
  - kernel/step_writer.go
  - kernel/process.go
  - kernel/reap.go
  - internal/types/step_record.go
  - context/context.go
---

# ATDD Checklist — Story 27.2: GetStepDetail IPC Method

## Test File

`ipc/atdd_27_2_getstepdetail_test.go`

## Red Phase Status

**编译状态: FAIL (预期)**

编译错误引用了以下尚不存在的符号:

| 符号 | 类型 | 定义位置 |
|------|------|----------|
| `MethodGetStepDetail` | 常量 | `ipc/protocol.go` |
| `GetStepDetailRequest` | 结构体 | `ipc/protocol.go` |
| `GetStepDetailResponse` | 结构体 | `ipc/protocol.go` |
| `MessageWire` | 结构体 | `ipc/protocol.go` |
| `ToolCallWire` | 结构体 | `ipc/protocol.go` |
| `ToolDefWire` | 结构体 | `ipc/protocol.go` |
| `Process.SetFinalSystemPrompt()` | 方法 | `kernel/process.go` |
| `Process.SetNativeToolDefs()` | 方法 | `kernel/process.go` |
| `Client.GetStepDetail()` | 方法 | `ipc/client.go` |
| `Server.handleGetStepDetail()` | 方法 | `ipc/server.go` |

## Test Coverage Map

| AC | 测试函数 | 验证点 |
|----|----------|--------|
| AC-1 | `TestATDD_27_2_AC1_MethodConstant` | Method 常量值 = "get_step_detail" |
| AC-1 | `TestATDD_27_2_AC1_GetStepDetailRequest_Serialization` | Request JSON 序列化/反序列化 |
| AC-1 | `TestATDD_27_2_AC1_GetStepDetailResponse_Serialization` | Response 全字段序列化/反序列化 |
| AC-1 | `TestATDD_27_2_AC1_MessageWire_WithToolCalls` | MessageWire + ToolCallWire 嵌套序列化 |
| AC-1 | `TestATDD_27_2_AC1_MessageWire_ToolResult` | tool 角色 MessageWire ToolCallID |
| AC-1 | `TestATDD_27_2_AC1_ToolDefWire_Serialization` | ToolDefWire 独立序列化 |
| AC-2 | `TestATDD_27_2_AC2_RunningProcess` | Running 状态进程从内存 + 磁盘读取 |
| AC-2 | `TestATDD_27_2_AC2_MessagesConversion` | json.RawMessage → []MessageWire 转换 (含 ToolCalls) |
| AC-3 | `TestATDD_27_2_AC3_ZombieProcess` | Zombie 状态进程仍在内存，正常返回 |
| AC-4 | `TestATDD_27_2_AC4_ReapedProcess` | 进程已清理，从 process-meta.json + steps.jsonl 读取 |
| AC-5 | `TestATDD_27_2_AC5_PIDNotFound` | PID 不存在返回 not_found |
| AC-6 | `TestATDD_27_2_AC6_StepNotFound` | Step 超出范围返回 not_found |
| AC-7 | `TestATDD_27_2_AC7_ClientMethod` | Client.GetStepDetail 端到端往返 |
| AC-8 | `TestATDD_27_2_AC8_Performance` | 30 步 ≤ 500ms (NFR61) |

## Implementation Dependencies

### 需要新增的生产代码

1. **`ipc/protocol.go`** — 类型定义
   - `MethodGetStepDetail` 常量
   - `GetStepDetailRequest`, `GetStepDetailResponse` 结构体
   - `MessageWire`, `ToolCallWire`, `ToolDefWire` 结构体

2. **`ipc/server.go`** — Handler
   - `handleGetStepDetail(conn, rawPayload)` 方法
   - switch case 添加 `case MethodGetStepDetail`

3. **`ipc/client.go`** — 客户端方法
   - `func (c *Client) GetStepDetail(pid, step) (*GetStepDetailResponse, error)`

4. **`kernel/kernel.go`** — Getter
   - `func (k *KernelImpl) GetStepDataDir() string`

5. **`kernel/process.go`** — 测试辅助 setter (可选，或用 mu.Lock 直接设置)
   - `func (p *Process) SetFinalSystemPrompt(s string)`
   - `func (p *Process) SetNativeToolDefs(defs []vfs.ToolDef)`

## Next Steps

进入绿阶段实现时，按以下顺序:
1. 在 `protocol.go` 添加 wire 类型 → AC-1 测试全部通过
2. 在 `kernel/process.go` 添加 setter → 测试 setup 编译通过
3. 在 `kernel/kernel.go` 添加 `GetStepDataDir()` → 编译依赖就绪
4. 在 `server.go` 实现 handler → AC-2 ~ AC-6 通过
5. 在 `client.go` 添加方法 → AC-7, AC-8 通过
6. `make all` 全部通过
