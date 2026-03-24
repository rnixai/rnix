---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
lastStep: step-04-generate-tests
lastSaved: '2026-03-23'
storyId: '29-4'
detectedStack: backend
generationMode: ai-generation
inputDocuments:
  - _bmad-output/implementation-artifacts/29-4-kernel-process-history-and-ipc.md
  - kernel/reap.go
  - kernel/kernel.go
  - ipc/protocol.go
  - ipc/server.go
  - ipc/client.go
  - internal/ui/renderer.go
  - vfs/proc.go
  - internal/types/types.go
---

# ATDD Checklist — Story 29.4: Kernel 进程历史保留与 IPC

## Test Strategy

| AC | 测试级别 | 优先级 | 场景数 | 说明 |
|----|----------|--------|--------|------|
| AC-1 | Unit | P0 | 5 | ProcessHistory 组件：构造、Add/List、FIFO 淘汰、深拷贝、默认 maxSize |
| AC-2 | Integration | P0 | 2 | Reaper 集成：cleanupExpiredDead 保存快照、字段完整性 |
| AC-3 | Integration | P0 | 3 | ListAllProcs：合并活跃+历史、CreatedAt 排序、去重 |
| AC-4 | Unit | P0 | 3 | IPC list_all_procs：协议常量、客户端方法、服务端 dispatch |
| AC-5 | Unit | P0 | 2 | 并发安全：ProcessHistory 并发读写、cleanup+ListAllProcs 并发 |
| AC-6 | Unit | P1 | 10 | StateSymbol：Unicode 8 场景 + ASCII 5 场景（含重叠） |

## Generated Test Files

| 文件 | 测试数 | 覆盖 AC | 红灯状态 |
|------|--------|---------|----------|
| `kernel/atdd_29_4_process_history_test.go` | 10 | AC-1,2,3,5 | ✅ `undefined: NewProcessHistory`, `ListAllProcs` |
| `ipc/atdd_29_4_list_all_procs_test.go` | 3 | AC-4 | ✅ `undefined: MethodListAllProcs`, `ListAllProcs` |
| `internal/ui/atdd_29_4_state_symbol_test.go` | 13 | AC-6 | ✅ `undefined: StateSymbol` |

## TDD Red Phase Verification

```
kernel:      build failed — NewProcessHistory, ListAllProcs undefined
ipc:         build failed — MethodListAllProcs, Client.ListAllProcs undefined
internal/ui: build failed — StateSymbol undefined
```

所有 26 个测试均因引用未实现的 API 而编译失败 → 红灯阶段正确。

## Test Scenario Details

### AC-1: ProcessHistory 组件 (5 tests)
- [x] `TestATDD_29_4_AC1_NewProcessHistory` — 构造函数返回非 nil，初始 Len()=0
- [x] `TestATDD_29_4_AC1_Add_And_List` — Add 后 List 返回正确条目
- [x] `TestATDD_29_4_AC1_FIFO_Eviction` — maxSize=5 时添加 8 条，最老 3 条被淘汰
- [x] `TestATDD_29_4_AC1_List_Returns_DeepCopy` — 修改返回切片不影响内部数据
- [x] `TestATDD_29_4_AC1_DefaultMaxSize_1000` — Kernel 初始化时 history 可用

### AC-2: Reaper 集成 (2 tests)
- [x] `TestATDD_29_4_AC2_Reaper_Saves_History_Before_Remove` — cleanup 后进程从 procTable 消失但在 ListAllProcs 中可见
- [x] `TestATDD_29_4_AC2_History_Snapshot_Has_Complete_Fields` — 快照保留 Intent/Provider/Model/TokensUsed/Result

### AC-3: ListAllProcs (3 tests)
- [x] `TestATDD_29_4_AC3_ListAllProcs_Merges_Active_And_History` — 同时包含活跃和历史进程
- [x] `TestATDD_29_4_AC3_ListAllProcs_Sorted_By_CreatedAt` — 按 CreatedAt 升序
- [x] `TestATDD_29_4_AC3_ListAllProcs_NoDuplicates` — 无重复 PID

### AC-4: IPC list_all_procs (3 tests)
- [x] `TestATDD_29_4_AC4_MethodListAllProcs_Constant` — 常量值 = "list_all_procs"
- [x] `TestATDD_29_4_AC4_Client_ListAllProcs_Method` — Client 有 ListAllProcs 方法
- [x] `TestATDD_29_4_AC4_Server_Handles_ListAllProcs` — Server dispatch 包含该方法

### AC-5: 并发安全 (2 tests)
- [x] `TestATDD_29_4_AC5_Concurrent_Add_And_List` — 50 writer + 50 reader 并发无 panic
- [x] `TestATDD_29_4_AC5_Concurrent_Cleanup_And_ListAllProcs` — cleanup 与 ListAllProcs 并发无 panic

### AC-6: StateSymbol (13 tests)
- [x] Unicode: Running→●, Created→○, Dead+success→✓, Dead+empty→✕, Dead+error→✕, Dead+fail→✕, Dead+timeout→✕, Zombie→⏸
- [x] ASCII: Running→*, Created→o, Dead+success→+, Dead+error→x, Zombie→=
