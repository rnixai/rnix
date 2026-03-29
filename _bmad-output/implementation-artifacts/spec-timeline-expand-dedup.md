---
title: 'Timeline 面板展开去重 + 信息增强'
type: 'refactor'
created: '2026-03-28'
status: 'done'
baseline_commit: 'f4b0c309'
context:
  - '_bmad-output/planning-artifacts/ux-timeline-optimization.md'
  - '_bmad-output/planning-artifacts/ux-timeline-unification.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Dashboard Timeline 面板 Level 2（展开区）的 Path 和 Dur. 字段与 Level 1 完全重复，用户展开后看到的是已经见过的信息，感觉"白展开了"。同时 Header 缺少推理阶段统计和滚动位置，错误行无内容预览，无批量操作和错误跳转能力。

**Approach:** 消除展开区冗余（Path/Dur. 去重，Token 条件化），增强 Header（阶段统计+位置），增加错误行内预览，新增批量展开折叠(e/E)和错误跳转(n/N)键位，统一过滤栏 label。

## Boundaries & Constraints

**Always:**
- `estimateExpandedLines` 必须与 `renderExpandedDetail` 同步修改，否则滚动计算偏移
- Level 1 布局和已有功能（三级展开/Prompt 查看器/过滤模式）保持不变
- 窄屏降级：< 80 cols 时不显示错误预览、不显示 Header 统计和位置
- Token 条件显示：仅当 `RequestTokens + ResponseTokens != TokenCount` 时在 Level 2 显示分解
- Path 去重：当 Level 1 的 `displaySummary == s.ToolPath` 时不重复显示 Path

**Ask First:**
- 如果 `StepSummaryWire` 需要新增 `ErrorPreview` 字段（影响 IPC 协议），HALT 确认

**Never:**
- 不修改 Level 1 的渲染布局
- 不移除 Prompt 查看器或过滤模式
- 不修改 `kernel/` 或 `drivers/` 包

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| 展开 tool_call step，Level 1 已显示 ToolPath | `displaySummary == s.ToolPath` | Level 2 不显示 Path 行 | N/A |
| 展开 tool_call step，Level 1 显示独立 Summary | `displaySummary != s.ToolPath` | Level 2 显示 Path 行 | N/A |
| 展开任意 step | Level 1 耗时列已显示 | Level 2 不显示 Dur. 行 | N/A |
| Token 分解与总数一致 | `req+resp == TokenCount` | Level 2 不显示 Token 行 | N/A |
| Token 分解与总数不一致 | `req+resp != TokenCount` | Level 2 显示 Token 分解 | N/A |
| 错误 step 展开，detail 已缓存 | `HasError && cache hit` | `✗` 右侧显示 ToolError 首行截断 | 宽度不足时降级为仅 `✗` |
| Header 宽屏 ≥ 100 | 有 step 数据 | 显示 `plan:2 tool:6 err:1` + `5/12` | N/A |
| Header 中屏 80-99 | 有 step 数据 | 仅显示 `5/12` | N/A |
| 按 `e` | 有可见 step | 所有可见 step 展开到 Level 2 | detail 未缓存时显示 Loading |
| 按 `n` | 光标后有错误 step | 跳转到下一个 HasError step | 无更多错误时 statusMsg 提示 |

</frozen-after-approval>

## Code Map

- `cmd/rnix/dashboard_timeline.go` -- 主文件：渲染、键位、数据流
  - `renderExpandedDetail` (L422) -- Level 2 渲染，需去重
  - `estimateExpandedLines` (L688) -- 行数估算，需同步
  - `renderStepTimeline` (L225) -- Level 1 渲染（不变）
  - `renderStepHeader` (L603) -- Header 渲染，需增强
  - `handleTimelineKey` (L93) -- 键位分发，需新增 e/E/n/N
  - `renderStepFilterBar` (L630) -- 过滤栏，需统一 label
  - `actionAbbrev` (L45) -- 已语义化，无需修改
- `cmd/rnix/dashboard_timeline.go:323` -- 过时注释需修复
- `cmd/rnix/dashboard_types.go` -- 类型定义（不变）

## Tasks & Acceptance

**Execution:**
- [x] `cmd/rnix/dashboard_timeline.go` -- `renderExpandedDetail` 去重：Path 条件化(L429-438)、Dur. 移除(L505-514)、Token 条件化(L516-520)
- [x] `cmd/rnix/dashboard_timeline.go` -- `estimateExpandedLines` 同步修改(L688-715)
- [x] `cmd/rnix/dashboard_timeline.go` -- `renderStepHeader` 增加阶段统计和滚动位置(L603-628)
- [x] `cmd/rnix/dashboard_timeline.go` -- 错误行内预览渲染(L347-351)
- [x] `cmd/rnix/dashboard_timeline.go` -- `handleTimelineKey` 新增 e/E/n/N 键位(L93-128)
- [x] `cmd/rnix/dashboard_timeline.go` -- `renderStepFilterBar` label 统一为 actionAbbrev(L635-647)
- [x] `cmd/rnix/dashboard_timeline.go` -- 修复 L323 过时注释
- [x] `cmd/rnix/atdd_27_3_dashboard_timeline_test.go` -- 新增验收测试

**Acceptance Criteria:**
- Given 展开 step 且 Level 1 已显示 ToolPath，when 渲染 Level 2，then 不显示 Path 行
- Given 展开任意 step，when 渲染 Level 2，then 不显示 Dur. 行
- Given Token 分解与总数一致，when 渲染 Level 2，then 不显示 Token 行
- Given 宽屏 ≥ 100 且有 step，when 查看 Header，then 显示阶段统计+位置
- Given 错误 step 且 detail 已缓存且 ≥ 80 cols，when 渲染 Level 1，then `✗` 右侧显示 ToolError 首行预览
- Given 按 `e`，when 有可见 step，then 全部展开到 Level 2
- Given 按 `n`，when 光标后有错误 step，then 跳转到该 step
- Given 进入过滤模式，when 查看 label，then 与 Level 1 action 缩写一致

## Verification

**Commands:**
- `make test` -- expected: 全部通过，含新增 AC 测试
- `make lint` -- expected: 无 lint 错误
- `make build` -- expected: 编译成功

## Suggested Review Order

**展开区去重核心逻辑**

- renderExpandedDetail 中 Path 条件化、Dur 移除、Token 条件化
  [`dashboard_timeline.go:478`](../../cmd/rnix/dashboard_timeline.go#L481)

- estimateExpandedLines 与渲染逻辑完全同步
  [`dashboard_timeline.go:765`](../../cmd/rnix/dashboard_timeline.go#L765)

**错误行内预览**

- Level 1 从 stepDetailCache 取 ToolError 首行截断显示
  [`dashboard_timeline.go:417`](../../cmd/rnix/dashboard_timeline.go#L417)

**Header 增强**

- 阶段统计 + 滚动位置，宽屏自适应
  [`dashboard_timeline.go:670`](../../cmd/rnix/dashboard_timeline.go#L670)

**新增键位**

- e/E 批量展开折叠（仅操作 filtered）+ n/N 错误跳转
  [`dashboard_timeline.go:125`](../../cmd/rnix/dashboard_timeline.go#L125)

**过滤栏 label 统一**

- 使用 actionAbbrev() 保持与 Level 1 一致
  [`dashboard_timeline.go:714`](../../cmd/rnix/dashboard_timeline.go#L714)

**验收测试**

- 16 个测试覆盖全部 AC + 审查发现的边界情况
  [`atdd_27_3_dashboard_timeline_test.go:580`](../../cmd/rnix/atdd_27_3_dashboard_timeline_test.go#L580)
