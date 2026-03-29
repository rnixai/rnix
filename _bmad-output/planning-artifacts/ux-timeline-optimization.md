---
type: ux-design-amendment
parent: ux-design-specification.md
scope: Dashboard Timeline 面板信息展示与交互优化
date: '2026-03-28'
author: Sally (UX Designer)
status: draft
amends:
  - 'ux-timeline-unification.md'
---

# Timeline 面板信息展示与交互优化

## 1. 设计背景

### 用户故事

Alice 是一个正在用 `rnix dashboard` 调试 AI 代理的开发者。她选中了 PID 3，Timeline 面板里出现了 20 多个 Step。

**场景 A：快速判断进展**

Alice 想一眼看出代理执行到什么阶段了——是正在规划、执行工具调用、还是已经完成？Header 显示 `Timeline │ PID 3 │ 20 steps │ 3.2k tok`，但她看不到"代理完成了多少步"、"有几处错误"。她必须逐行扫视才能拼凑全貌。

**场景 B：定位问题**

代理报错了。Alice 看到一行高亮红色背景和 `✗` 标记，但她不知道这是"命令路径写错了"（30 秒修好）还是"LLM 上游超时"（需要等）。她必须按 `v` 展开，等数据从 IPC 加载，才能判断严重性。

**场景 C：批量查看**

Alice 想快速浏览所有 Step 的输入输出，但必须逐个按 `v` 展开。20 个 Step 意味着 20 次按键 + 20 次等待。

**场景 D：跳过无用步骤**

代理执行了 20 个 Step，其中 3 个出错。Alice 想快速在错误之间跳转，但只能 `j`/`j`/`j` 逐行滚动。

### 当前代码状态确认

以下功能**已经实现**，不在本次优化范围内：

- ✅ Action 缩写已语义化：`tool`/`plan`/`done`/`spec`/`x`（`actionAbbrev`）
- ✅ 错误行红色背景高亮
- ✅ 慢 Step 耗时黄色高亮
- ✅ 过滤模式（`f` 键，7 种 action 类型切换）
- ✅ 三级展开（Summary / Expanded / Debug）
- ✅ Prompt 查看器（`P` 键，Tab 切换 Messages/System/Tools）
- ✅ 宽度自适应（≥90 显示耗时，≥70 显示 Token，否则隐藏）

### 展开前后信息重复分析

这是本次优化的核心问题。Level 1（折叠）和 Level 2（展开）之间存在明显的信息冗余：

**Level 1 渲染内容**（`renderStepTimeline` 第 304-397 行）：
```
│  Step 3  tool  /dev/shell → go build -o rnix  890tok  1.2s  ✗ │
│          ↑     ↑                               ↑       ↑     ↑
        action  Summary 或 ToolPath            总token  耗时  错误
```

**Level 2 渲染内容**（`renderExpandedDetail` 第 422-523 行）：
```
│   ┊  Path  /dev/shell → go build -o rnix      ← Level 1 已显示相同内容
│   ┊ Input  go build -o rnix ./cmd/rnix        ← 新信息 ✓
│   ┊ Error  exit status 1: undefined: Proc...  ← 新信息 ✓
│   ┊  Dur.  1.2s                               ← Level 1 已显示相同内容
│   ┊ Token  734 req → 156 resp                 ← Level 1 显示总数，这里有分解
```

**重复明细**：

| # | Level 2 字段 | 重复情况 | 严重程度 |
|---|-------------|---------|---------|
| 1 | `Path` | 当 Level 1 的 `displaySummary` 已经是 ToolPath（`s.Summary < 8` 时，第 358 行逻辑）时完全重复 | 高 |
| 2 | `Dur.` | 与 Level 1 的耗时列完全重复（`DurationMs` 同一字段） | 高 |
| 3 | `Token` | Level 1 显示 `TokenCount` 总数，Level 2 显示 `RequestTokens → ResponseTokens` 分解——有增量但用户需要对比两个数字判断是否匹配 | 中 |
| 4 | `Result` | Level 1 无预览（正常结果 step），Level 2 展示是有效增量 | 无重复 ✓ |
| 5 | `Input` | Level 1 不显示，Level 2 是有效增量 | 无重复 ✓ |

**用户感知**：按 `v` 展开后，前两行（Path、Dur.）看到的是已经在上面一行里见过的内容，真正的新信息（Input、Result/Error）要到第三行才出现。这让人感觉"展开没什么用"。

### 真正的 UX 差距

| # | 问题 | 影响 |
|---|------|------|
| 1 | **展开区 Path 与 Level 1 Summary 重复** | 展开后前两行无新信息，用户感觉"白展开了" |
| 2 | **展开区 Dur. 与 Level 1 耗时完全重复** | 浪费垂直空间 |
| 3 | Header 缺少推理进展统计 | 用户无法快速判断"进行到哪了"、"有几处错误" |
| 4 | 错误行只有 `✗` 标记，无内容预览 | 延长问题定位时间，必须展开才能看到错误类型 |
| 5 | 无批量展开/折叠能力 | 20 个 Step 需 20 次按键逐个展开 |
| 6 | 无错误间跳转能力 | 在多个 Step 中定位错误只能逐行滚动 |
| 7 | 过滤栏 label 与 action 缩写不一致 | Filter 显示 `tool_call`（9字符）而 Timeline 显示 `tool`（4字符），认知不统一 |
| 8 | 注释过时 | `dashboard_timeline.go:323` 注释写 "3 tc / 1 pl / 4 dn" 但实际渲染 "3 tool / 1 plan / 4 done" |

---

## 2. 优化方案

### 2.1 展开区去重：只显示 Level 1 没有的信息（核心优化）

**问题**：展开区的 `Path` 和 `Dur.` 与 Level 1 完全重复，导致用户感觉"展开没什么新内容"。

**方案**：展开区只显示 Level 1 **不包含**的增量信息，消除重复。

#### 优化前（当前）

Level 1：
```
│  Step 3  tool  /dev/shell → go build -o rnix  890tok  1.2s  ✗ │
```

Level 2 展开区（5 行，2 行重复）：
```
│   ┊  Path  /dev/shell → go build -o rnix      ← 重复
│   ┊ Input  go build -o rnix ./cmd/rnix
│   ┊ Error  exit status 1: undefined: ProcessManager
│   ┊  Dur.  1.2s                               ← 重复
│   ┊ Token  734 req → 156 resp
```

#### 优化后

Level 1 不变。Level 2 展开区（3 行，零重复）：
```
│   ┊ Input  go build -o rnix ./cmd/rnix
│   ┊ Error  exit status 1: undefined: ProcessManager
│   ┊ Token  734 req → 156 resp
```

#### 具体规则

| 字段 | Level 1 有？ | Level 2 显示？ | 规则 |
|------|-------------|---------------|------|
| `Path` | 是（Summary 就是 ToolPath） | **不显示** | 当 `displaySummary == s.ToolPath` 时跳过 |
| `Path` | 否（Summary 是独立文本） | 显示 | 当 `displaySummary != s.ToolPath` 且 `detail.ToolPath != ""` 时显示 |
| `Dur.` | 是（耗时列） | **不显示** | 始终跳过，Level 1 已有 |
| `Input` | 否 | 显示 | 保持不变 |
| `Error` | 否（只有 ✗ 标记） | 显示 | 保持不变 |
| `Result` | 否 | 显示 | 保持不变 |
| `Token` | 是（总数） | **条件显示** | 仅当 `RequestTokens + ResponseTokens` 与 `TokenCount` 不一致时显示分解；否则跳过 |
| `RawResponse` | 否 | 显示 | plan/text/complete 类型保持不变 |

#### 对 `estimateExpandedLines` 的影响

需要同步修改行数估算函数（第 688-715 行），移除对 Path 和 Dur. 的计数，确保滚动计算准确。

**代码位置**：
- `dashboard_timeline.go:429-438`（Path 渲染 → 条件化）
- `dashboard_timeline.go:505-514`（Dur. 渲染 → 移除）
- `dashboard_timeline.go:516-520`（Token 渲染 → 条件化）
- `dashboard_timeline.go:688-715`（`estimateExpandedLines` → 同步修改）

---

### 2.2 Header 增强：推理阶段统计 + 滚动位置

**问题**：Header 只显示 `Timeline │ PID N │ N steps │ N tok`，缺少阶段分布和位置信息。

**方案**：在 Header 右侧增加各 action 类型计数（仅显示数量 > 0 的类型），以及光标位置。

**优化前**：
```
┌─ Timeline │ PID 1 │ 12 steps │ 3.2k tok ─────────────────────────────────┐
```

**优化后**（宽屏 ≥ 100 cols）：
```
┌─ Timeline │ PID 1 │ 12 steps │ 3.2k tok │ plan:2 tool:6 err:1 │ 5/12 ───┐
```

- `plan:2 tool:6 err:1` — 各阶段计数，用 action 颜色渲染，`err` 单独用红色
- `5/12` — 光标在第 5 步，共 12 步
- 仅在 `width >= 100` 时显示阶段统计；`width >= 80` 时仅显示位置 `5/12`

**窄屏降级**：
- < 100 cols：隐藏阶段统计，仅显示位置 `5/12`
- < 80 cols：Header 精简，位置和统计均隐藏

**代码位置**：`dashboard_timeline.go:603-628`（`renderStepHeader`）

---

### 2.3 错误行内预览

**问题**：错误行只显示红色背景 + `✗`，内容需展开后才能看到。

**方案**：在 `✗` 右侧追加 ToolError 首行截断预览。

**优化前**：
```
│ ▸Step 3  tool  /dev/shell → go build -o rnix        890tok  1.2s  ✗ │
```

**优化后**（宽屏 ≥ 80 cols）：
```
│ ▸Step 3  tool  /dev/shell → go build -o rnix  890tok  1.2s  ✗ exit status 1: undefined… │
```

**规则**：
- 预览取 `StepSummaryWire` 中的错误首行（如果 IPC 已提供），或从 `stepDetailCache` 中取 `ToolError` 首行
- 截断至可用宽度，末尾 `…`
- 预览文字用红色 `#FF6B6B`
- < 80 cols 时降级为仅 `✗`
- 需要 IPC 层在 `StepSummaryWire` 中提供错误摘要（`ErrorPreview string`），或在展开后从 cache 中取

**代码位置**：`dashboard_timeline.go:347-351`（错误标记渲染）

**依赖**：如果 `StepSummaryWire` 不包含错误预览字段，需要：
1. `ipc/protocol.go` — `StepSummaryWire` 新增 `ErrorPreview string` 字段（可选）
2. `ipc/server.go` — `handleListSteps` 填充该字段
3. 或者：仅在 detail 已缓存时显示预览（延迟显示）

---

### 2.4 批量展开/折叠

**问题**：20 个 Step 需要 20 次 `v` 按键逐个展开。

**方案**：新增键位 `e`（展开所有可见 Step 到 Level 2）和 `E`（折叠所有到 Level 1）。

| 键 | 作用 |
|---|------|
| `e` | 将所有当前可见的 Step 展开到 Level 2，触发 detail 批量获取 |
| `E` | 将所有 Step 折叠回 Level 1 |

**实现细节**：
- `e` 遍历 `filteredStepEntries()` 中当前可见范围内的 Step，设置 `level = levelExpanded`
- 逐个检查 `stepDetailCache`，对未缓存的 Step 发起 `fetchStepDetailCmd`（可串行或限制并发数 3）
- `E` 遍历所有 `stepEntries`，设置 `level = levelSummary`

**代码位置**：`dashboard_timeline.go:93-128`（`handleTimelineKey`，新增 case）

---

### 2.5 错误间跳转

**问题**：多个错误 Step 分布在列表中，只能逐行 `j` 滚动查找。

**方案**：新增键位 `n`（下一个错误）和 `N`/`Shift+N`（上一个错误）。

| 键 | 作用 |
|---|------|
| `n` | 从当前光标位置向下查找下一个 `HasError == true` 的 Step 并跳转 |
| `N` | 从当前光标位置向上查找上一个 `HasError == true` 的 Step 并跳转 |

**边界处理**：
- 到达列表末尾/开头后无更多错误：statusMsg 显示 "No more errors"
- 当前无任何错误 Step：statusMsg 显示 "No errors in timeline"

**代码位置**：`dashboard_timeline.go:93-128`（`handleTimelineKey`，新增 case）

---

### 2.6 过滤栏 label 统一

**问题**：Filter 栏显示 `tool_call`（9 字符），Timeline 行显示 `tool`（4 字符），两个视图的术语不一致。

**方案**：Filter 栏的 label 改为与 Timeline 行一致的缩写。

**优化前**：
```
Filter: [T]tool_call ✓ [P]plan ✓ [A]assistant ✓ [C]complete ✓ [S]spawn ✓ [R]replan ✓ [Z]specialize ✓
```

**优化后**：
```
Filter: [T]tool ✓ [P]plan ✓ [A]text ✓ [C]done ✓ [S]spawn ✓ [R]repl ✓ [Z]spec ✓  [*]all
```

- label 使用 `actionAbbrev()` 返回值，确保两个视图术语一致
- `A` 的 label 从 `assistant` 改为 `text`（action 名称）

**代码位置**：`dashboard_timeline.go:635-647`（`renderStepFilterBar` 中的 types 数组）

---

### 2.7 修复过时注释

**问题**：`dashboard_timeline.go:323` 注释写 `"3 tc" / "1 pl" / "4 dn"` 但实际渲染已是 `"3 tool" / "1 plan" / "4 done"`。

**方案**：更新注释与实际代码一致。

**代码位置**：`dashboard_timeline.go:323`

---

## 3. 实施优先级

| 优先级 | 优化项 | 工作量 | 影响 |
|--------|--------|--------|------|
| P0 | 2.1 展开区去重 | 中（改 `renderExpandedDetail` + `estimateExpandedLines`） | 高：每次展开都受益，消除重复感 |
| P0 | 2.3 错误行内预览 | 中（可能需改 IPC 协议） | 高：直接缩短问题定位时间 |
| P0 | 2.2 Header 增强 | 小（改 `renderStepHeader`） | 高：每次查看都受益 |
| P1 | 2.4 批量展开 `e`/`E` | 小（新增键位处理） | 中：批量查看效率提升 |
| P1 | 2.5 错误跳转 `n`/`N` | 小（新增键位处理） | 中：错误定位效率提升 |
| P2 | 2.6 过滤栏 label 统一 | 极小（改 label 数组） | 低：认知一致性 |
| P2 | 2.7 修复过时注释 | 极小 | 低：代码维护性 |

---

## 4. 验收标准

### AC-1：展开区 Path 去重
- Given 展开一个 Step，且 Level 1 的 `displaySummary` 等于 `s.ToolPath`（即 Level 1 已显示完整路径），when 渲染 Level 2 展开区，then 不显示 Path 行

### AC-2：展开区 Dur. 去重
- Given 展开一个 Step，when 渲染 Level 2 展开区，then 不显示 Dur. 行（Level 1 耗时列已展示）

### AC-3：展开区 Token 条件显示
- Given 展开一个 Step 且 `RequestTokens + ResponseTokens == TokenCount`，when 渲染 Level 2 展开区，then 不显示 Token 分解行
- Given 展开一个 Step 且 `RequestTokens + ResponseTokens != TokenCount`（或 TokenCount 为 0），when 渲染 Level 2 展开区，then 显示 Token 分解行

### AC-4：Header 阶段统计
- Given Timeline 有 ≥ 1 个 Step 且面板宽度 ≥ 100，when 查看 Header 右侧，then 显示各 action 类型计数（如 `plan:2 tool:6`），仅显示数量 > 0 的类型

### AC-5：Header 滚动位置
- Given Step 数量超过可见行数且面板宽度 ≥ 80，when 查看 Header，then 显示光标位置 `N/total`

### AC-6：错误行内预览
- Given 选中有错误的 Step 且面板宽度 ≥ 80 且 detail 已缓存，when 渲染该行，then `✗` 右侧显示 ToolError 首行截断预览（红色文字）

### AC-7：批量展开
- Given Timeline 面板活跃且有 Step，when 按 `e`，then 所有可见 Step 展开至 Level 2

### AC-8：批量折叠
- Given Timeline 面板有展开的 Step，when 按 `E`，then 所有 Step 折叠回 Level 1

### AC-9：跳转到下一个错误
- Given Timeline 中光标之后存在错误 Step，when 按 `n`，then 光标跳转到下一个 `HasError == true` 的 Step

### AC-10：跳转到上一个错误
- Given Timeline 中光标之前存在错误 Step，when 按 `N`，then 光标跳转到上一个 `HasError == true` 的 Step

### AC-11：过滤栏 label 一致
- Given 进入过滤模式，when 查看过滤栏 label，then label 与 Timeline 行的 action 缩写一致（`tool`/`plan`/`text`/`done`/`spawn`/`repl`/`spec`）

---

## 5. 文件变更清单

| 文件 | 变更 | 说明 |
|------|------|------|
| `cmd/rnix/dashboard_timeline.go` | 修改 | `renderExpandedDetail` 去重（Path/Dur./Token 条件化）；`estimateExpandedLines` 同步；`renderStepHeader` 增加统计+位置；错误预览渲染；`handleTimelineKey` 新增 `e`/`E`/`n`/`N`；`renderStepFilterBar` label 统一；修复注释 |
| `cmd/rnix/atdd_27_3_dashboard_timeline_test.go` | 修改 | 新增 AC-1~AC-11 对应测试用例 |
| `ipc/protocol.go` | 可能修改 | `StepSummaryWire` 新增 `ErrorPreview` 字段（如需行内预览不依赖 detail cache） |
| `ipc/server.go` | 可能修改 | `handleListSteps` 填充 `ErrorPreview` |
