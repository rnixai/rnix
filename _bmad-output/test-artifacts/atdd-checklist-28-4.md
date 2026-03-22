---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-22'
storyId: '28-4'
storyTitle: Dashboard PID 有效性检查
detectedStack: backend
generationMode: ai-generation
inputDocuments:
  - _bmad-output/implementation-artifacts/28-4-dashboard-pid-validity-check.md
---

# ATDD Checklist — Story 28.4: Dashboard PID 有效性检查

## 1. Preflight & Context

- **Stack**: backend (Go 1.26)
- **Test framework**: `go test` with `testing` package
- **Story**: 28-4 Dashboard PID 有效性检查
- **Affected file**: `cmd/rnix/dashboard.go` (单文件修改)
- **Test file**: `cmd/rnix/atdd_28_4_dashboard_pid_validity_test.go`

## 2. Generation Mode

- **Mode**: AI Generation (backend project, acceptance criteria clear)
- **No browser recording needed** (pure Go model-level unit tests)

## 3. Test Strategy

### Acceptance Criteria → Test Mapping

| AC | 场景 | 测试函数 | 级别 | 优先级 |
|----|------|---------|------|--------|
| AC-1 | 选中进程时同时设置 selectedUUID | `TestATDD_28_4_AC1_SelectProcessSetsUUID` | Unit | P0 |
| AC-1 | 清除选中时同步清除 selectedUUID | `TestATDD_28_4_AC1_ClearSelectionClearsUUID` | Unit | P0 |
| AC-2 | PID 复用（同 PID 不同 UUID）清除选中 | `TestATDD_28_4_AC2_PIDReuseDetection` | Unit | P0 |
| AC-2 | 同 PID 同 UUID 保持选中 | `TestATDD_28_4_AC2_SamePIDSameUUID_PreservesSelection` | Unit | P1 |
| AC-3 | 进程被 reap 后清除选中 | `TestATDD_28_4_AC3_ProcessReapClearsSelection` | Unit | P0 |
| AC-4 | procDetailCache 使用 UUID 键 | `TestATDD_28_4_AC4_ProcDetailCacheByUUID` | Unit | P1 |
| AC-5 | recording 映射使用 UUID 键 | `TestATDD_28_4_AC5_RecordingByUUID` | Unit | P1 |
| AC-1 | 空 UUID 回退到纯 PID 检查 | `TestATDD_28_4_AC1_EmptyUUID_FallbackToPIDOnly` | Unit | P1 |

### Test Levels

- **Unit**: 全部为 dashboard model 状态转换测试，直接构造 `dashboardModel`，无需 daemon 或 IPC

### Red Phase 验证

- **编译失败**: `dashboardModel` 尚无 `selectedUUID` 字段 → 所有测试编译失败
- **TDD 合规**: 测试在功能实现前编写，验证预期行为

## 4. Test Files

| 文件 | 测试数量 | 状态 |
|------|---------|------|
| `cmd/rnix/atdd_28_4_dashboard_pid_validity_test.go` | 8 | RED (编译失败) |

## 5. Validation

- [x] Story acceptance criteria 全覆盖 (AC-1 ~ AC-5)
- [x] 测试设计为实现前失败 (TDD red phase)
- [x] 遵循现有测试模式 (`atdd_27_6` 风格)
- [x] 无临时文件或孤立进程
- [x] 测试文件位于正确目录 (`cmd/rnix/`)

## 6. Key Risks & Assumptions

- **假设**: `vfs.ProcInfo.UUID` 字段已存在（Story 28-1 完成）
- **假设**: `handlePIDChange()` 方法签名不变
- **风险**: AC-4 和 AC-5 的测试依赖缓存键类型从 PID 改为 UUID，实现时需同步修改所有访问点

## 7. Next Steps

- 执行 `bmad-dev-story` 实现 Story 28-4
- 实现后所有 8 个测试应从 RED → GREEN
- 运行 `make all` 确保无回归
