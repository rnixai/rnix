---
type: ux-design-amendment
parent: ux-design-specification.md
scope: Dashboard Timeline 面板重设计
date: '2026-03-25'
author: Sally (UX Designer)
status: draft
supersedes:
  - 'D.4 时间线三级详细度设计（Story 27-3）'
  - 'D.14 时间线事件详情增强（Issue #11）'
---

# Timeline 面板统一重设计

## 1. 设计背景

### 问题诊断

当前 Dashboard Timeline 面板有两个独立显示模式：

- **Syscall 模式**（默认）：展示底层 VFS 事件流（Open/Read/Write/Close），附带时间线密度条和缩放功能
- **Step 模式**：展示高层推理步骤（tool_call/plan/text/complete），支持三级展开

用户按 `s` 键在两者之间切换。**核心问题：**

1. **上下文断裂** —— 切换模式后丢失位置感，用户无法同时看到"做了什么"和"怎么做的"
2. **信息重叠** —— Step Detail 已包含 ToolPath、ToolInput、ToolResult、ToolError，与 Syscall 事件中的 VFS 调用高度重复
3. **Syscall 独有价值极低** —— VFS 子操作分解（Open→Read→Close）和 TraceID/SpanID 仅在极少数高级调试场景有用，`rnix strace <pid>` 命令行工具已覆盖此需求

### 设计决策

**移除 Syscall 视图，统一为 Step 视图**。Step 是用户的自然心智模型——"智能体做了哪几步"，而非"操作系统执行了哪些系统调用"。

**决策依据**：
- **心智模型不匹配**：Syscall 视图展示的 VFS 级操作（Open/Read/Write/Close）是 OS 实现细节，而非用户关心的 agent 行为。用户的自然问题是"agent 做了什么"而非"OS 执行了哪些系统调用"
- **功能完全覆盖**：`rnix strace <pid>` 命令行工具提供了完整的 Syscall 实时追踪能力，高级调试场景不受影响
- **信息冗余**：Step Detail 的 ToolPath/ToolInput/ToolResult 已包含 Syscall 事件的核心信息，两个视图之间约 80% 信息重叠
- **交互成本**：双模式切换（`s` 键）导致上下文断裂，用户在两个视图间频繁切换以拼凑完整画面
- **实施风险可控**：移除的代码（~400 行）在 git 历史中完整保留，如后续发现遗漏场景可快速恢复。TUI 内部重构不需要 feature flag——无外部 API 变更、无数据迁移、影响面限于 Dashboard 渲染层

---

## 2. 信息架构

### 数据来源

所有展示数据来自已有的 `StepSummaryWire` 和 `GetStepDetailResponse`，无需新增 IPC 接口。

| 字段 | 来源 | Level 1 | Level 2 | Level 3 |
|------|------|---------|---------|---------|
| Step 号 | StepSummaryWire.Step | ● | ● | ● |
| Action 类型 | StepSummaryWire.Action | ● | ● | ● |
| Summary | StepSummaryWire.Summary | ● | ● | ● |
| 错误标记 | StepSummaryWire.HasError | ● | ● | ● |
| 耗时 | StepSummaryWire.DurationMs | ● | ● | ● |
| 总 Token | GetStepDetailResponse.TokenCount | ● | ● | ● |
| ToolInput | GetStepDetailResponse.ToolInput | | ● | ● |
| ToolResult / ToolError | GetStepDetailResponse | | ● | ● |
| Token 明细 | RequestTokens / ResponseTokens | | ● | ● |
| RawResponse | GetStepDetailResponse | | ●¹ | ● |
| Messages[] | GetStepDetailResponse | | | ● |
| SystemPrompt | GetStepDetailResponse | | | ● |
| Tools[] | GetStepDetailResponse | | | ● |

¹ 仅 plan/text/complete 类型展示 RawResponse 片段

### 宽度自适应策略

面板的水平空间分配采用**弹性列宽**模型：

```
[固定区域]                              [弹性区域]               [固定区域]
 ▸ Step 3   tool_call  ←── Summary 自适应填满 ──→   890 tok   1.2s  ✗
 |_________|__________|________________________________|_________|_____|__|
   9 chars   ~12 chars         剩余全部宽度             8 chars  6ch  2ch
```

| 列 | 宽度策略 | 最小宽度 |
|----|---------|---------|
| 光标 + Step 号 | 固定：`▸ Step NN` | 9 chars |
| Action 类型 | 固定：按最长 action 对齐 | 12 chars |
| Summary | **弹性**：填满剩余空间 | 20 chars |
| Token | 固定：右对齐 | 8 chars |
| 耗时 | 固定：右对齐 | 6 chars |
| 错误标记 | 固定 | 2 chars |

**Summary 截断规则**：
- 可用宽度 = 面板总宽 - 固定列宽度总和 - 间距
- 截断时末尾显示 `…`（单字符省略号）
- 窗口宽度变化时实时重算（Bubbletea 的 WindowSizeMsg 已支持）
- 最小 20 字符保底，低于此阈值隐藏 Token 列以释放空间
- 宽度计算使用 `go-runewidth`（或等效库）处理 CJK 双宽字符，避免按 byte/rune 计算导致中文截断错位
- 极端窄屏（< 60 cols）：隐藏 Token 和耗时列后仍不足时，隐藏 Action 列，仅保留 Step 号 + Summary

---

## 3. 视觉规范

### Level 1：折叠态（默认视图）

每个 Step 一行。这是用户进入 Timeline 面板时看到的全局画面——快速扫描智能体的推理进展。

**渲染模板**（宽屏 ≥ 120 cols）：

```
┌─ Timeline │ PID 1 │ 6 steps │ 1,847 tok ─────────────────────────────────┐
│                                                                           │
│  Step 1   text       根据对 Rnix 项目的分析，这是一个面向 AI 智能体的…     245 tok   0.3s     │
│  Step 2   tool_call  /dev/fs → read main.go                               412 tok   0.2s     │
│ ▸Step 3   tool_call  /dev/shell → go build -o rnix                        890 tok   1.2s  ✗  │
│  Step 4   plan       重新规划构建策略，先修复 ProcessManager 接口定义        156 tok   0.3s     │
│  Step 5   tool_call  /dev/fs → write build.go                             520 tok   0.1s     │
│  Step 6   complete   构建成功，输出二进制文件 rnix (4.2MB)                   98 tok   0.1s     │
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘
```

**渲染模板**（窄屏 80 cols）：

```
┌─ Timeline │ PID 1 │ 6 steps ──────────────────┐
│                                                │
│  Step 1  text       根据对 Rnix 项目…   245tok │
│  Step 2  tool_call  /dev/fs → read…     412tok │
│ ▸Step 3  tool_call  /dev/shell → go…    890tok │
│  Step 4  plan       重新规划构建策…     156tok │
│  Step 5  tool_call  /dev/fs → write…    520tok │
│  Step 6  complete   构建成功，输出…      98tok │
│                                                │
└────────────────────────────────────────────────┘
```

窄屏降级规则：
- < 100 cols：隐藏耗时列
- < 90 cols：Token 缩写为 `NNNtok`（去掉空格和 " tok" 后缀）
- < 80 cols：Action 类型缩写（`tool_call` → `tool`，`complete` → `done`）

**色彩编码**：

| 元素 | 颜色 | 依据 |
|------|------|------|
| Step 号 | 暗灰 `#888` | 次要信息 |
| Action `tool_call` | 绿色 `#6BCB77` | 沿用设计系统 success 色 |
| Action `plan` | 蓝色 `#5B9BD5` | 沿用设计系统 agent 色 |
| Action `text` | 白色 | 默认 |
| Action `complete` | 绿色加粗 | 终态强调 |
| Action `spawn` | 紫色 | 沿用 IPC 色 |
| Action `specialize` | 青色 `#4EC9B0` | 技能加载 |
| Action `replan` | 橙色 `#E5C07B` | 重规划警示 |
| Summary | 白色 | 主要阅读内容 |
| Token | 暗灰 `#888` | 次要信息 |
| 耗时 | 暗灰 `#888` | 次要信息 |
| 错误标记 `✗` | 红色 `#FF6B6B` | 沿用设计系统 error 色 |
| 光标行 | 反色（背景高亮） | 标准 TUI 选中态 |

**自动行为**：
- 新 Step 到达时：如光标在最后一行，自动跟随滚动（类似 `tail -f`）
- 错误 Step：整行加红色背景提示
- 慢 Step（>1s）：耗时数字高亮为黄色
- 自动展开不中断阅读：若光标不在最后一行（用户正在浏览历史），新到达的错误/慢 Step 仅标记展开状态，不触发滚动；仅当光标在末尾时才自动滚动并展开

---

### Level 2：展开态

用户在某个 Step 上按 `Enter` 或 `v`，展开该步骤的执行细节。展开区域紧跟在该 Step 行下方，其余 Step 行保持可见。

#### 场景 A：tool_call 类型

```
│  Step 2   tool_call  /dev/fs → read main.go                            412 tok   0.2s     │
│ ▾Step 3   tool_call  /dev/shell → go build -o rnix                     890 tok   1.2s  ✗  │
│   ┊ Input   go build -o rnix ./cmd/rnix                                                   │
│   ┊ Error   exit status 1: undefined: ProcessManager                                      │
│   ┊ Token   734 req → 156 resp                                                            │
│  Step 4   plan       重新规划构建策略                                    156 tok   0.3s     │
```

**展示字段**：
- `Input`：ToolInput 内容。单行显示，超出面板宽度时截断并显示 `… (N bytes)`
- `Result` 或 `Error`：优先显示 ToolError（如有），否则显示 ToolResult。多行结果最多显示 3 行 + `… (N more lines)`
- `Token`：`RequestTokens req → ResponseTokens resp` 格式

#### 场景 B：plan / text 类型

```
│ ▾Step 4   plan       重新规划构建策略，先修复 ProcessManager 接口定义     156 tok   0.3s     │
│   ┊ 需要先修复 ProcessManager 接口定义，然后重新编译。                                      │
│   ┊ 错误来源于 kernel.go 第 42 行缺少方法实现。                                            │
│   ┊ … (3 more lines)                                                                      │
│   ┊ Token   120 req → 36 resp                                                             │
```

**展示字段**：
- 内容：RawResponse 截取前 3 行，超出显示 `… (N more lines)`
- Token：同上

#### 场景 C：spawn 类型

```
│ ▾Step 7   spawn      子进程: code-reviewer (PID 5)                      320 tok   0.4s     │
│   ┊ Agent   code-reviewer                                                                 │
│   ┊ Model   claude/haiku                                                                  │
│   ┊ Token   280 req → 40 resp                                                             │
```

#### 场景 D：complete 类型

```
│ ▾Step 6   complete   构建成功，输出二进制文件 rnix (4.2MB)                98 tok   0.1s     │
│   ┊ 构建产物已保存至 ./rnix，大小 4.2MB。                                                  │
│   ┊ 共执行 6 步，总耗时 2.2s，总消耗 2,321 tokens。                                        │
│   ┊ Token   82 req → 16 resp                                                              │
```

**展开区域视觉规则**：
- 缩进：相对 Step 行缩进 3 字符
- 引导线：`┊` 竖虚线，使用暗灰色，视觉引导归属关系
- 字段标签（Input/Error/Token/Agent/Model）：暗灰色，固定 8 字符宽右对齐
- 字段内容：白色，占满剩余宽度
- 错误内容（Error 行）：红色 `#FF6B6B`

**自动展开规则**：
- `HasError == true` 的 Step 自动展开至 Level 2
- `DurationMs > 1000` 的 Step 自动展开至 Level 2（保留现有逻辑）

---

### Level 3：调试态

用户按 `V` 进入调试态，在 Level 2 基础上追加 LLM 上下文信息。面向需要深入分析 LLM 行为的场景。

```
│ ▾Step 3   tool_call  /dev/shell → go build -o rnix                     890 tok   1.2s  ✗  │
│   ┊ Input   go build -o rnix ./cmd/rnix                                                   │
│   ┊ Error   exit status 1: undefined: ProcessManager                                      │
│   ┊ Token   734 req → 156 resp                                                            │
│   ┊─────────────────────────────────────────────────────────────────────                   │
│   ┊ Messages (12)                                                      累计 4,280 tok      │
│   ┊  [system] 你是一个代码构建助手，负责编译和修复 Go 项目…                                  │
│   ┊  [user]   请编译 rnix 项目                                                             │
│   ┊  [asst]   我来执行 go build 命令…                                                      │
│   ┊  [tool]   exit status 1: undefined: ProcessManager                                    │
│   ┊                                      ↳ 按 p 查看完整 prompt                            │
```

**展示字段（在 Level 2 基础上追加）**：
- 分隔线：`┊───…` 暗灰色虚线，区隔执行细节和 LLM 上下文
- Messages 总数 + 累计 Token
- 消息预览：显示最后 N 条消息（N = min(消息总数, 面板可用行数 - Level2行数 - 3)，下限 2 条，上限 6 条），格式为 `[role] 内容前 60 字符…`
- 消息不足 2 条时：全部显示，不截断
- 操作提示：`↳ 按 p 查看完整 prompt`

**角色标签色彩**：
- `[system]`：暗灰
- `[user]`：蓝色
- `[asst]`：绿色
- `[tool]`：黄色

---

## 4. 交互设计

### 键位映射

| 键 | 作用 | 说明 |
|----|------|------|
| `j` / `↓` | 光标下移 | 跳过展开区域的子行，直接到下一个 Step |
| `k` / `↑` | 光标上移 | 同上 |
| `Enter` 或 `v` | 切换 Level 1 ↔ Level 2 | 展开/折叠选中 Step |
| `V` | 切换到 Level 3 | Level 1 → Level 3；Level 3 → Level 1；Level 2 → Level 3 |
| `p` | 打开 Prompt 查看器 | Level 2/3 时可用（过滤模式下此键被过滤功能占用），全屏覆盖查看完整 prompt |
| `f` | 按 Action 类型过滤 | 进入过滤模式（见下） |
| `g` / `Home` | 跳到第一个 Step | |
| `G` / `End` | 跳到最后一个 Step | |
| `PgUp` | 向上翻页 | |
| `PgDn` | 向下翻页 | |

### 移除的键位

| 原键位 | 原功能 | 移除理由 |
|--------|--------|---------|
| `s` | Syscall/Step 模式切换 | Syscall 视图已移除 |
| `h` / `←` | 时间轴水平平移 | 时间线条已移除 |
| `l` / `→` | 时间轴水平平移 | 同上 |
| `+` / `=` | 时间轴缩放 | 同上 |
| `-` | 时间轴缩放 | 同上 |

### 过滤模式

按 `f` 进入过滤编辑模式，按 Action 类型过滤（替代原有的 Syscall 类别过滤）：

| 键 | 切换 |
|----|------|
| `t` | tool_call |
| `p` | plan |
| `x` | text |
| `c` | complete |
| `s` | spawn |
| `r` | replan |
| `z` | specialize |
| `a` | 全部显示 |
| `f` / `Esc` | 退出过滤模式 |

面板标题显示过滤状态：`filter: tool_call,plan (3/6)`

---

### Prompt 查看器（全屏覆盖）

用户在 Level 2/3 时按 `p` 进入 Prompt 查看器。这是一个全屏覆盖层，用于沉浸式审查该 Step 的完整 LLM 上下文——system prompt、消息历史、工具定义。

#### 布局

```
┌─ Prompt Viewer │ Step 3 │ 12 messages │ 4,280 tok ─────── [Tab: Messages] ──┐
│                                                                              │
│  [system]                                                           120 tok  │
│  你是一个代码构建助手，负责编译和修复 Go 项目。                                │
│  你拥有以下工具：                                                             │
│  - /dev/fs：读写文件系统                                                      │
│  - /dev/shell：执行 shell 命令                                                │
│  …                                                                           │
│                                                                              │
│  ─────────────────────────────────────────────────────────────────────────    │
│  [user]                                                              45 tok  │
│  请编译 rnix 项目                                                             │
│                                                                              │
│  ─────────────────────────────────────────────────────────────────────────    │
│  [asst]                                                             280 tok  │
│  我来执行 go build 命令来编译项目…                                             │
│                                                                              │
│  ─────────────────────────────────────────────────────────────────────────    │
│  [tool]                                                             156 tok  │
│  exit status 1: undefined: ProcessManager                                    │
│                                                                              │
│──────────────────────────────────────────────────── 4/12 ── 35% ─────────────│
└──────────────────────────────────────────────────────────────────────────────┘
```

#### Tab 页签

标题栏右侧显示当前激活的 Tab，按 `Tab` 键循环切换：

| Tab | 内容 | 说明 |
|-----|------|------|
| Messages | 完整消息历史 | 默认激活，按角色分段显示所有消息 |
| System | System Prompt 全文 | 独立查看 system prompt |
| Tools | 工具定义列表 | 每个工具一段：名称 + 描述 + 参数 schema |

#### 消息渲染规则

- 每条消息一个块：`[role]` 标签 + token 数（右对齐）+ 内容
- 消息之间用暗灰虚线 `───` 分隔
- 角色标签沿用 Level 3 色彩：system=暗灰、user=蓝、asst=绿、tool=黄
- 长消息完整显示（可滚动），不截断
- 当前选中消息高亮背景

#### Tools Tab 渲染规则

- 每个工具一段：工具名（绿色加粗）+ 描述（白色）+ 参数 JSON schema（暗灰等宽字体）
- 工具之间用暗灰虚线分隔

#### 键位

| 键 | 作用 |
|----|------|
| `j` / `↓` | 向下滚动 |
| `k` / `↑` | 向上滚动 |
| `PgDn` / `Space` | 向下翻页 |
| `PgUp` | 向上翻页 |
| `g` / `Home` | 跳到顶部 |
| `G` / `End` | 跳到底部 |
| `Tab` | 切换 Tab（Messages → System → Tools → Messages） |
| `/` | 搜索（输入关键词，`n` 跳下一个匹配，`N` 跳上一个） |
| `p` / `Esc` / `q` | 退出 Prompt 查看器，回到 Timeline |

#### 底部状态栏

`消息序号/总数 ── 滚动百分比`，如 `4/12 ── 35%`

#### 数据来源

全部来自已缓存的 `GetStepDetailResponse`（进入 Prompt 查看器前 Level 2/3 已获取并缓存）。无需额外 IPC 调用。

---

## 5. 数据获取策略

### Level 1 数据

- 来源：`ListSteps(pid, afterStep)` 返回 `StepSummaryWire[]`
- **变更**：需在 `StepSummaryWire` 中增加 `TokenCount` 字段（当前缺失）
- **向后兼容**：历史 `steps.jsonl` 中无此字段，反序列化后为零值 `0`；Level 1 渲染时对 `TokenCount == 0` 显示 `—` 而非 `0 tok`，以区分"无数据"和"零消耗"
- 轮询：每 tick 增量获取新 Step（保留现有逻辑）

### Level 2/3 数据

- 来源：`GetStepDetail(pid, stepNum)` 返回 `GetStepDetailResponse`
- 触发：用户展开 Step 时按需获取
- 缓存：`stepDetailCache map[int]*GetStepDetailResponse`（保留现有逻辑）
- 自动展开的 Step 在到达时立即预取 Detail
- Level 2/3 展开加载中：展开区域显示 `┊ Loading…`（暗灰 + spinner），Detail 返回后替换为实际内容

### 移除的数据流

| 数据流 | 移除 |
|--------|------|
| `AttachDebug(pid, callback)` | 不再需要实时 Syscall 事件流 |
| `ListEvents(pid, uuid)` | 不再需要磁盘加载 Syscall 事件 |
| `timelineEvents[]` | 不再维护事件数组 |
| `timelineEventCh` | 不再监听事件 channel |
| Timeline Bar 渲染 | 移除时间线密度条 |

---

## 6. 状态模型变更

### 移除的字段

```
- stepTimelineMode       // 不再需要模式切换标志
- timelineEvents[]       // 不再存储 Syscall 事件
- timelineAttachedPID    // 不再追踪 debug attach
- timelineEventCh        // 不再监听事件 channel
- timelineStopCh         // 不再需要停止信号
- timelineZoomLevel      // 不再需要缩放级别
- timelineViewStart      // 不再需要时间轴偏移
- timelineEventCursor    // 不再需要事件级光标
- timelineFilters        // 替换为 stepFilters
- timelineFilterMode     // 替换为 stepFilterMode
- timelineExpandedIdx    // 移除（由 stepExpandedIdx 替代）
```

### 保留/复用的字段

```
  stepEntries[]          // Step 摘要列表
  stepCursor             // 当前选中 Step 索引
  stepDetailCache        // Step Detail 缓存
  lastFetchedStep        // 增量获取标记
  fetchingDetail         // Detail 获取中标志
```

### 新增的字段

```
+ stepFilters map[string]bool   // Action 类型过滤器 {"tool_call":true, "plan":true, ...}
+ stepFilterMode bool           // 过滤编辑模式
+ stepExpandedIdx int           // 当前展开的 Step 索引（-1 表示无展开）
```

---

## 7. 边界处理

| 场景 | 行为 |
|------|------|
| 进程无 Step（刚启动） | 显示 `Waiting for steps…`（带 spinner） |
| 进程只有 1 个 Step | 正常显示，无特殊处理 |
| Step 数 > 可见行数 | 支持滚动，右侧显示滚动位置指示 |
| ToolResult 过大（>10KB） | Level 2 显示前 3 行 + `… (10.2KB total)` |
| GetStepDetail 请求失败 | 展开区显示 `⚠ Failed to load detail` 暗灰提示 |
| 全部 Step 被过滤掉 | 显示 `No steps match filter` + 过滤条件 |
| 已结束进程 | 从 steps.jsonl 加载（保留现有逻辑） |

---

## 8. 实施影响评估

### 需修改的文件

| 文件 | 变更范围 | 说明 |
|------|---------|------|
| `cmd/rnix/dashboard_timeline.go` | **大幅重写** | 移除 Syscall 渲染/键位/数据获取，统一为 Step 视图 |
| `cmd/rnix/dashboard_nav.go` | 中等 | 移除 Syscall 键位分发，简化为单一模式 |
| `cmd/rnix/dashboard_types.go` | 小 | 移除 Syscall 相关类型定义 |
| `cmd/rnix/dashboard.go` | 中等 | 移除 Syscall 状态字段，更新 tick/数据获取逻辑 |
| `ipc/protocol.go` | 小 | `StepSummaryWire` 增加 `TokenCount` 字段 |
| `ipc/server.go` | 小 | `handleListSteps` 填充 `TokenCount` |

### 需修改的测试文件

| 文件 | 变更范围 | 说明 |
|------|---------|------|
| `cmd/rnix/atdd_27_3_dashboard_timeline_test.go` | 大幅重写 | Timeline 面板主测试，需移除 Syscall 模式测试，新增 Step 统一视图测试 |
| `cmd/rnix/dashboard_test.go` | 中等 | 移除 Syscall 相关状态断言 |
| 其他 `atdd_27_*` / `atdd_29_*` 测试 | 小 | 涉及 Timeline 面板交互的引用可能需要更新 |

### 不需修改的

- `kernel/event_writer.go` — Syscall 事件仍然写入磁盘（`rnix strace` 命令行工具依赖）
- `debug/` 包 — 保持不变
- `kernel/kernel.go` 的 `emitEvent()` — 保持不变
- `ipc/client.go` 的 `AttachDebug` / `ListEvents` — 保留（`rnix strace` 命令使用 `AttachDebug`）

### 预估代码行数变化

- 删除：~400 行（Syscall 渲染、时间线条、缩放、事件处理）
- 新增：~150 行（宽度自适应、Action 过滤、Level 标签渲染优化）
- 净减少：~250 行
- Prompt 查看器：~120 行新增（全屏覆盖、Tab 切换、搜索）
