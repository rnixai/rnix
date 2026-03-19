---
title: 'Providers YAML 合并策略：按 name 智能合并'
type: 'bugfix'
created: '2026-03-19'
status: 'done'
baseline_commit: 'a8587ba'
context: []
---

# Providers YAML 合并策略：按 name 智能合并

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `DeepMergeYAML` 对 slice 采用整体替换策略。项目级 `providers.yaml` 若只定义 `[cursor]`，会完全替换全局的 `[claude, cursor, openrouter]`，导致 `default_provider: openrouter` 校验失败。

**Approach:** 在 `internal/config/merge.go` 新增 `MergeNamedSlice` 函数，按 `name` 字段对 slice-of-maps 做智能合并（base 保序 + override 覆盖同名项 + 追加新项）。在 `resolveProjectContext` 中对 `providers` 键调用此函数，替代 `DeepMergeYAML` 的默认 slice 替换行为。

## Boundaries & Constraints

**Always:** 不修改 `DeepMergeYAML` 的通用行为（slice 替换语义保持不变）。`MergeNamedSlice` 是独立函数，仅在 `resolveProjectContext` 显式调用。保持 base 中 provider 的原始顺序。

**Ask First:** 无。

**Never:** 不改变非 `providers` 键的合并行为。不修改 `DeepMergeYAML` 函数签名。

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| 项目覆盖部分 provider | base=[claude,cursor,openrouter], override=[cursor(modified)] | [claude, cursor(merged), openrouter] | N/A |
| 项目新增 provider | base=[claude], override=[cursor] | [claude, cursor] | N/A |
| override 为空 slice | base=[claude,cursor], override=[] | [claude,cursor]（保持 base） | N/A |
| base 为空 slice | base=[], override=[cursor] | [cursor] | N/A |
| 元素无 name 字段 | base=[{a:1}], override=[{b:2}] | 回退 slice 替换 | N/A |
| name 字段非 string | 元素 name 为 int | 回退 slice 替换 | N/A |

</frozen-after-approval>

## Code Map

- `internal/config/merge.go` -- 新增 `MergeNamedSlice` 函数
- `internal/config/merge_test.go` -- 新增测试用例
- `ipc/server.go` -- `resolveProjectContext` 中调用 `MergeNamedSlice`

## Tasks & Acceptance

**Execution:**
- [ ] `internal/config/merge.go` -- 新增 `MergeNamedSlice(base, override []any, keyField string) []any` -- 按 name 字段合并 slice-of-maps
- [ ] `internal/config/merge_test.go` -- 新增覆盖 I/O Matrix 全部场景的测试
- [ ] `ipc/server.go` -- 在 `resolveProjectContext` 的 `DeepMergeYAML` 调用后，对 `providers` 键调用 `MergeNamedSlice`

**Acceptance Criteria:**
- Given 全局 providers=[claude,cursor,openrouter] 和项目 providers=[cursor], when 合并后, then 结果包含全部三个 provider 且 cursor 的字段被项目级覆盖
- Given 项目 providers 包含全局不存在的 provider, when 合并后, then 新 provider 追加到列表末尾
- Given 任一 slice 为空, when 合并后, then 返回非空一方的内容
- Given slice 元素无 keyField, when 合并后, then 回退为 override 整体替换

## Verification

**Commands:**
- `go test -race -run TestMergeNamedSlice ./internal/config/...` -- expected: PASS
- `go test -race ./ipc/...` -- expected: PASS
- `make all` -- expected: 全部通过
