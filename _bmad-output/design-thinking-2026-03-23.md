# Design Thinking Session: Dashboard 信息架构重设计

**Date:** 2026-03-23
**Facilitator:** Decker
**Design Challenge:** Dashboard 信息架构重设计 — 解决 tab 循环、进程历史丢失、信息过载三大痛点

---

## 🎯 Design Challenge

### 设计挑战声明

单人开发者在终端 TUI 中调试 AI Agent 时，需要在保持全局态势感知的同时，快速定位到关注的信息维度，但当前 8-tab 线性循环的信息架构迫使他们在多个面板间低效地来回切换，且进程结束后历史信息即消失，导致事后分析无法进行。

### 用户画像

- **角色**: 单人开发者
- **场景**: 调试自己开发的 AI Agent
- **核心任务**: 实时监控进程状态、排查问题、理解 Agent 间协作关系、事后分析
- **技术约束**: 纯终端 TUI（Bubbletea v2 + Lipgloss），保留现有技术栈
- **环境**: 终端窗口，通常 80-200 列宽

### 痛点根因分析

| 痛点 | 现状 | 根因 |
|------|------|------|
| Tab 循环 | 8 pane 用 `(activePane+1) % 8` 线性循环 | 要看 Eval 得按 7 次 Tab；无快捷键直达 |
| 进程历史丢失 | `ListProcs()` 只返回存活进程 | Dead 进程在进程表中被 reaper 清除后无法追溯 |
| 信息过载 | 右下角 6 个面板共享同一区域 | 面板太多且各自信息密度高，无优先级分层 |

---

## 👥 EMPATHIZE: 理解用户

### 共情方法

| 方法 | 适用原因 |
|------|----------|
| Journey Mapping | 映射调试全流程，找到每个卡点 |
| Shadowing（代码级） | 通过代码分析模拟"观察用户行为" |
| Jobs to be Done | 理解开发者真正要完成的"任务" |

### 用户旅程地图

```
[1. 启动]          [2. 观察]           [3. 定位]          [4. 深入]          [5. 事后]
rnix spawn →       rnix dashboard      发现异常进程        查看详情/trace      回溯分析
等待 agent 运行     扫一眼全局状态       在树中找到 PID       Tab Tab Tab...     "进程没了？"

情绪：😐 平静      😐→😟 开始关注      😟 焦虑             😤 烦躁            😩 无助
痛点：无           信息多不知看哪      无法快速导航         Tab 循环地狱        历史丢失
```

### 共情地图

| 维度 | 发现 |
|------|------|
| Say | "我只想快速看到这个进程的 token 消耗" |
| Think | "为什么看个 heatmap 还得按这么多次 Tab？" |
| Do | Tab→Tab→Tab→Tab→…→过了→再来一轮 |
| Feel | 紧急调试时被 UI 交互拖慢，挫败感；历史丢失时无助 |

### Jobs to be Done

| 类型 | 具体任务 |
|------|----------|
| 功能性 | 监控状态、定位异常、分析 token、追踪调用链、查看 LLM 完整交互 |
| 情感性 | 感觉"掌控全局"、"我知道发生了什么" |
| 社会性 | 能解释"Agent 为什么失败了"（需要历史 + LLM 对话记录） |

---

## 🎨 DEFINE: 聚焦问题

### POV 陈述

单人 Agent 开发者需要在不离开当前上下文的情况下获取多维度调试信息，因为频繁的面板切换打断了思维流，而进程消亡后信息的不可追溯让事后分析成为不可能。

### How Might We 问题

1. HMW 消除 Tab 循环？— 如何让用户不用线性遍历就能直达目标面板？
2. HMW 让信息分层？— 如何让最重要的信息始终可见，而细节按需展开？
3. HMW 保留进程历史？— 如何让 Dead 进程的信息在 Dashboard 中可追溯？
4. HMW 减少认知负荷？— 如何从 8 个面板中提炼出核心视图？
5. HMW 让上下文跟随焦点？— 如何让选中进程的相关信息自动聚合展示？
6. HMW 深入 LLM 交互？— 如何让开发者查看完整的 LLM 请求/响应内容？

### 关键洞察

1. 8 个面板不是 8 个平等维度 — Tree 和 Timeline 是"总是需要"的，Security/Eval/Trace 是"偶尔需要"的
2. 信息的时间性被忽略 — 进程有生命周期，但 Dashboard 只展示"此刻"
3. 空间导航优于线性导航 — 快捷键直达 > Tab 循环
4. 进程列表应统一展示 — 不应按生死分区，状态用符号标示即可
5. LLM 交互是最底层的调试信息 — 需要专门的查看器

---

## 💡 IDEATE: 方案生成

### 灵感类比

| 类比来源 | 可借鉴之处 |
|----------|-----------|
| htop/btop | 固定多区域布局，无需 tab 切换 |
| tmux | 数字键直达窗格 |
| Grafana | 面板网格，信息分层，时间范围选择 |
| IDE 调试器 | 变量/调用栈/断点并排显示，联动更新 |

### 方案聚类

**A. 导航重构**: 数字键直达、模糊搜索、双面板并排、双向 Tab、Space 弹出选择
**B. 信息分层**: 三层架构、进程卡片、底部状态栏、面板自动隐藏、密度切换
**C. 进程历史**: 统一进程列表（状态符号区分）、历史时间线、快照、历史抽屉
**D. 信息聚合**: 进程聚焦视图、异常高亮、Mini 指标条、Overview 模式、上下文联动
**E. LLM 深度查看**: LLM 对话查看器、步骤导航、请求/响应完整展示

### Top 概念

**推荐方案：「进程聚焦视图 + 统一进程列表 + 数字键直达 + LLM 对话查看器」**

---

## 🛠️ PROTOTYPE: 方案原型

### 状态符号体系

```
● Running    ○ Created    ✓ Done    ✕ Failed    ⏸ Paused
```

所有进程统一展示在一张列表中，仅用状态符号区分，不按生死分区。

### 视图 0：默认 — 进程聚焦卡片

```
╭─ Rnix Dashboard ──── [1]Tree [2]Time [3]Heat [4]Detail [5]Intent [6]Sec [7]Trace [8]Eval ──╮
│                          │                                                                  │
│  AGENT TREE              │  TIMELINE ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  │
│  ─────────               │  10:31:02 ● PID 1 spawn(coder)         [spawn]                  │
│  ▸ ● 1 orchestrator      │  10:31:05 ■ PID 2 write(1024 tok)     [llm]                    │
│    ├─ ● 2 coder          │  10:31:08 ◆ PID 3 open(/dev/shell)    [shell]                  │
│    ├─ ● 3 reviewer       │  10:31:10 ✕ PID 4 exit(0)             [lifecycle]              │
│    └─ ✓ 4 researcher     │  10:31:12 ● PID 2 write(512 tok)      [llm]                    │
│         exit(0) 58s      │                                                                  │
│                          │━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  │
│                          │                                                                  │
│                          │  FOCUS: PID 2 coder ━━━━━━━━━━━━━━━━━━━━━ Running 2m30s         │
│                          │                                                                  │
│                          │  ┌─ Tokens ─────────┐ ┌─ Context ──────┐ ┌─ Status ───────────┐ │
│                          │  │ ██████▓░░ 1.2k/5k│ │ Sys  ██░░ 15%  │ │ State: Running     │ │
│                          │  │ rate: 12 tok/s    │ │ User ████ 30%  │ │ skill: coder       │ │
│                          │  │ steps: 5          │ │ Asst █████ 45% │ │ dev: llm,fs        │ │
│                          │  │ elapsed: 2m30s    │ │ Tool ██░░ 10%  │ │ ppid: 1            │ │
│                          │  └──────────────────┘ └────────────────┘ └────────────────────┘ │
│                          │  ┌─ Intent ──────────┐ ┌─ Trace ────────┐ ┌─ Alerts ──────────┐ │
│                          │  │ ✓ parse-req       │ │ spans: 12      │ │ (none)             │ │
│                          │  │ ● impl-feature    │ │ avg: 340ms     │ │                    │ │
│                          │  │ ○ write-tests     │ │ errors: 0      │ │                    │ │
│                          │  └──────────────────┘ └────────────────┘ └────────────────────┘ │
╰─ 4 procs (3● 1✓) │ 2.9k tok │ ⚠0 ── Tab:cycle 1-8:jump L:llm H:history Esc:back q:quit ──╯
```

### 视图 1：按 `1` — Tree 扩展（统一进程表）

```
╭─ Rnix Dashboard ──── 1:Tree* [2]Time [3]Heat [4]Detail [5]Intent [6]Sec [7]Trace [8]Eval ──╮
│                                                                                              │
│  AGENT TREE (expanded) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  │
│                                                                                              │
│  PID  ST  AGENT            MODEL              TOKENS   ELAPSED   EXIT   SKILLS               │
│  ───  ──  ─────            ─────              ──────   ───────   ────   ──────               │
│  ▸ 1  ●   orchestrator     claude-sonnet-4      820    5m12s      -     planner,coder        │
│  ├ 2  ●   coder            claude-sonnet-4    1,204    2m30s      -     coder                │
│  ├ 3  ●   reviewer         claude-haiku-3       312    1m05s      -     reviewer             │
│  └ 4  ✓   researcher       claude-haiku-3       580    0m58s      0     researcher           │
│                                                                                              │
│                                                                                              │
│  ● Running: 3  │  ✓ Done: 1  │  ✕ Failed: 0  │  Total tokens: 2,916                        │
│                                                                                              │
╰────────────────────────── j/k:navigate  Enter:focus  K:kill  Esc:back  q:quit ───────────────╯
```

### 视图 2：按 `2` — Timeline 扩展

```
╭─ Rnix Dashboard ──── [1]Tree 2:Time* [3]Heat [4]Detail [5]Intent [6]Sec [7]Trace [8]Eval ──╮
│                          │                                                                   │
│  AGENT TREE              │  TIMELINE (expanded) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  │
│  ─────────               │                                                                   │
│  ▸ ● 1 orchestrator      │  Filter: [All] 1:spawn 2:syscall 3:llm 4:error   Zoom: [+][-]   │
│    ├─ ● 2 coder          │                                                                   │
│    ├─ ● 3 reviewer       │  TIME       PID  EVENT                       CATEGORY   DURATION  │
│    └─ ✓ 4 researcher     │  ────       ───  ─────                       ────────   ────────  │
│                          │  10:30:00   1    spawn(orchestrator)         spawn      -         │
│                          │  10:30:02   1    open(/dev/llm/claude)       vfs        2ms       │
│                          │  10:30:02   1    write(system prompt 320t)   llm        1.2s      │
│                          │  10:30:04   1    read(response 180t)         llm        -         │
│                          │  10:31:02   1    spawn(coder) → PID 2       spawn      -         │
│                          │  10:31:03   2    open(/dev/llm/claude)       vfs        1ms       │
│                          │  10:31:05   2    write(1024 tok)             llm        2.8s      │
│                          │  10:31:08   3    open(/dev/shell)            shell      1ms       │
│                          │  10:31:10   4    exit(0)                     lifecycle  -         │
│                          │  10:31:12   2    write(512 tok)              llm        1.5s      │
│                          │                                                                   │
│                          │  ── Step View (v) ──────────────────────────────────────────────  │
│                          │  Step 3/5 │ action: tool_call │ tool: write_file │ 512 tok        │
╰─ 4 procs (3● 1✓) │ 2.9k tok │ ⚠0 ── j/k:scroll h/l:pan +/-:zoom 1-4:filter v:step p:prompt ╯
```

### 视图 3：按 `3` — Heatmap 完整

```
╭─ Rnix Dashboard ──── [1]Tree [2]Time 3:Heat* [4]Detail [5]Intent [6]Sec [7]Trace [8]Eval ──╮
│                          │                                                                   │
│  AGENT TREE              │  CONTEXT HEATMAP ━━━━━━━━━━━━━━━━━━━ PID 2 coder ━━━━━━━━━━━━━━  │
│  ─────────               │                                                                   │
│  ▸ ● 1 orchestrator      │  SEGMENT           TOKENS  PCT   ACTIVITY  HEAT                   │
│    ├─ ● 2 coder          │  ─────────         ──────  ───   ────────  ────                   │
│    ├─ ● 3 reviewer       │  ▸ System Prompt    320    15%   Active    ████████░░░░░░░░░░░░░  │
│    └─ ✓ 4 researcher     │    Agent instructions, skill definitions                          │
│                          │                                                                   │
│                          │    User Message 1   128     6%   Active    ███░░░░░░░░░░░░░░░░░░  │
│                          │    Assistant 1       256    12%   Warm      ██████░░░░░░░░░░░░░░░  │
│                          │    Tool: read_file   64      3%   Warm      █░░░░░░░░░░░░░░░░░░░░  │
│                          │    User Message 2   96      5%   Active    ██░░░░░░░░░░░░░░░░░░░  │
│                          │    Assistant 2       512    24%   Active    ████████████░░░░░░░░░  │
│                          │    Tool: write_file  180     9%   Active    ████░░░░░░░░░░░░░░░░░  │
│                          │    User Message 3   64      3%   Cold      █░░░░░░░░░░░░░░░░░░░░  │
│                          │    Assistant 3       384    18%   Active    █████████░░░░░░░░░░░░  │
│                          │    ⚠ Leaked          100     5%   Leaked    ░░░░░░░░░░░░░░░░░░░░░  │
│                          │                                                                   │
│                          │  Total: 2,104/8,192 (25.7%)  ██████▓░░░░░░░░░░░░░░░░░░░░░░░░░░  │
│                          │  By Kind: Sys 15% │ User 14% │ Asst 54% │ Tool 12% │ Leaked 5%   │
╰─ 4 procs (3● 1✓) │ 2.9k tok │ ⚠0 ────────── j/k:navigate  Enter:expand  Esc:back  q:quit ──╯
```

### 视图 4：按 `4` — Detail 完整

```
╭─ Rnix Dashboard ──── [1]Tree [2]Time [3]Heat 4:Detail* [5]Intent [6]Sec [7]Trace [8]Eval ──╮
│                          │                                                                   │
│  AGENT TREE              │  PROCESS DETAIL ━━━━━━━━━━━━━━━━━━━━━ PID 2 coder ━━━━━━━━━━━━━  │
│  ─────────               │                                                                   │
│  ▸ ● 1 orchestrator      │  ┌─ Identity ────────────────────────────────────────────────────┐│
│    ├─ ● 2 coder          │  │ PID: 2       PPID: 1        UUID: a3f8-c291                   ││
│    ├─ ● 3 reviewer       │  │ Agent: coder Model: claude-sonnet-4                           ││
│    └─ ✓ 4 researcher     │  │ State: Running (step 5/?)   Elapsed: 2m30s                    ││
│                          │  └───────────────────────────────────────────────────────────────┘│
│                          │                                                                   │
│                          │  ┌─ Skills & Devices ────────────────────────────────────────────┐│
│                          │  │ Skills: coder                                                  ││
│                          │  │ Devices: /dev/llm/claude, /dev/fs, /dev/shell                  ││
│                          │  │ MCP: (none)                                                    ││
│                          │  └───────────────────────────────────────────────────────────────┘│
│                          │                                                                   │
│                          │  ┌─ Environment ─────────────────────────────────────────────────┐│
│                          │  │ RNIX_ENV=development  PROJECT_ROOT=/home/decker/myproject      ││
│                          │  │ ANTHROPIC_API_KEY=sk-ant-...****                                ││
│                          │  └───────────────────────────────────────────────────────────────┘│
│                          │                                                                   │
│                          │  ┌─ Recent Steps ────────────────────────────────────────────────┐│
│                          │  │ Step 3: tool_call → read_file(src/main.go)         340ms      ││
│                          │  │ Step 4: tool_call → write_file(src/handler.go)     1.2s       ││
│                          │  │ Step 5: text → "I've updated the handler..."       280ms      ││
│                          │  └───────────────────────────────────────────────────────────────┘│
╰─ 4 procs (3● 1✓) │ 2.9k tok │ ⚠0 ──────────────── j/k:scroll  L:llm  Esc:back  q:quit ─────╯
```

### 视图 5：按 `5` — Intent DAG

```
╭─ Rnix Dashboard ──── [1]Tree [2]Time [3]Heat [4]Detail 5:Intent* [6]Sec [7]Trace [8]Eval ──╮
│                          │                                                                   │
│  AGENT TREE              │  INTENT TREE ━━━━━━━━━━━━━━━━━━━━━━ PID 1 orchestrator ━━━━━━━━  │
│  ─────────               │                                                                   │
│  ▸ ● 1 orchestrator      │  Intent: "Build a REST API for user management"                   │
│    ├─ ● 2 coder          │  State: executing (3/5 tasks done)                                │
│    ├─ ● 3 reviewer       │                                                                   │
│    └─ ✓ 4 researcher     │  ┌─ DAG ────────────────────────────────────────────────────────┐ │
│                          │  │                                                               │ │
│                          │  │  ✓ research-requirements ──┐                                  │ │
│                          │  │    PID 4 (✓) 580 tok       ├─→ ● implement-endpoints          │ │
│                          │  │                            │    PID 2  1.2k tok               │ │
│                          │  │  ✓ design-schema ──────────┘         │                        │ │
│                          │  │    PID 1  200 tok                    ├─→ ○ write-tests        │ │
│                          │  │                                      │    (pending)           │ │
│                          │  │                            ┌────────→│                        │ │
│                          │  │  ● review-code ────────────┘         └─→ ○ deploy             │ │
│                          │  │    PID 3  312 tok                        (blocked)            │ │
│                          │  │                                                               │ │
│                          │  └───────────────────────────────────────────────────────────────┘ │
│                          │                                                                   │
│                          │  Legend: ✓ done  ● running  ○ pending  ✕ failed                   │
│                          │  Total: 3/5 done │ Est. remaining: ~2k tok                        │
╰─ 4 procs (3● 1✓) │ 2.9k tok │ ⚠0 ────────── j/k:navigate  Enter:details  Esc:back  q:quit ──╯
```

### 视图 6：按 `6` — Security

```
╭─ Rnix Dashboard ──── [1]Tree [2]Time [3]Heat [4]Detail [5]Intent 6:Sec* [7]Trace [8]Eval ──╮
│                          │                                                                   │
│  AGENT TREE              │  SECURITY & IMMUNE STATUS ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  │
│  ─────────               │                                                                   │
│  ▸ ● 1 orchestrator      │  Immune System: ENABLED         Policy: default                   │
│    ├─ ● 2 coder          │  Threat Level:  ████░░░░░░ LOW  Scan Interval: 30s                │
│    ├─ ● 3 reviewer       │                                                                   │
│    └─ ✓ 4 researcher     │  ┌─ Active Rules ───────────────────────────────────────────────┐ │
│                          │  │ ✓ path_traversal   Block /dev/fs writes outside sandbox       │ │
│                          │  │ ✓ token_budget     Alert when usage > 80% budget              │ │
│                          │  │ ✓ prompt_injection  Scan LLM inputs for injection patterns    │ │
│                          │  │ ✓ recursive_spawn  Limit spawn depth to 5 levels              │ │
│                          │  │ ○ data_exfil       (disabled) Monitor outbound data volume    │ │
│                          │  └───────────────────────────────────────────────────────────────┘ │
│                          │                                                                   │
│                          │  ┌─ Recent Events ──────────────────────────────────────────────┐ │
│                          │  │ TIME       PID  SEVERITY  EVENT                              │ │
│                          │  │ 10:31:08   3    INFO      shell access granted               │ │
│                          │  │ 10:31:05   2    INFO      LLM write within budget (24%)     │ │
│                          │  │ 10:30:12   1    INFO      spawn depth 1/5                   │ │
│                          │  └───────────────────────────────────────────────────────────────┘ │
│                          │                                                                   │
│                          │  Scans: 12 │ Blocked: 0 │ Alerts: 0 │ Last scan: 3s ago          │
╰─ 4 procs (3● 1✓) │ 2.9k tok │ ⚠0 ────────── j/k:scroll  Enter:details  Esc:back  q:quit ────╯
```

### 视图 7：按 `7` — Trace

```
╭─ Rnix Dashboard ──── [1]Tree [2]Time [3]Heat [4]Detail [5]Intent [6]Sec 7:Trace* [8]Eval ──╮
│                          │                                                                   │
│  AGENT TREE              │  DISTRIBUTED TRACE ━━━━━━━━━━━━━━━━━━━━ Trace: tr-a3f8 ━━━━━━━━  │
│  ─────────               │                                                                   │
│  ▸ ● 1 orchestrator      │  Root: PID 1 orchestrator → "Build REST API"                     │
│    ├─ ● 2 coder          │  Duration: 5m12s (ongoing)    Spans: 18                           │
│    ├─ ● 3 reviewer       │                                                                   │
│    └─ ✓ 4 researcher     │  ┌─ Span Tree ──────────────────────────────────────────────────┐ │
│                          │  │                                                               │ │
│                          │  │  [PID 1] orchestrator ━━━━━━━━━━━━━━━━━━━━━━━━━━━  5m12s     │ │
│                          │  │    ├─ llm.call ████░░░░░░░░░░░░░░░░░░░░░░░░░░░░   1.2s      │ │
│                          │  │    ├─ spawn(2) ░                                  2ms       │ │
│                          │  │    ├─ spawn(4) ░                                  2ms       │ │
│                          │  │    │                                                         │ │
│                          │  │    ├─ [PID 4] researcher ████████░░░░░░░░░░░░░░░  58s ✓     │ │
│                          │  │    │    ├─ llm.call █████░░░░░░░░░░░░░░░░░░░░░░   3.2s      │ │
│                          │  │    │    └─ llm.call ████░░░░░░░░░░░░░░░░░░░░░░░   2.8s      │ │
│                          │  │    │                                                         │ │
│                          │  │    ├─ [PID 2] coder ━━━━━━━━━━━━━━━━━━━━━━━━━━━  2m30s ●   │ │
│                          │  │    │    ├─ llm.call █████████░░░░░░░░░░░░░░░░░░   2.8s      │ │
│                          │  │    │    └─ fs.read  ░                              3ms      │ │
│                          │  │    │                                                         │ │
│                          │  │    └─ [PID 3] reviewer ━━━━━━━━━━━━━━━━━━━━━━━━  1m05s ●   │ │
│                          │  │         └─ shell.exec █████░░░░░░░░░░░░░░░░░░░░   820ms     │ │
│                          │  └───────────────────────────────────────────────────────────────┘ │
╰─ 4 procs (3● 1✓) │ 2.9k tok │ ⚠0 ────────── j/k:navigate  Enter:span  Esc:back  q:quit ─────╯
```

### 视图 8：按 `8` — Eval

```
╭─ Rnix Dashboard ──── [1]Tree [2]Time [3]Heat [4]Detail [5]Intent [6]Sec [7]Trace 8:Eval* ──╮
│                          │                                                                   │
│  AGENT TREE              │  MULTI-AGENT EVALUATION ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  │
│  ─────────               │                                                                   │
│  ▸ ● 1 orchestrator      │  ┌─ Reputation Scores ──────────────────────────────────────────┐ │
│    ├─ ● 2 coder          │  │ AGENT            SCORE  TREND  TASKS   SUCCESS  QUALITY      │ │
│    ├─ ● 3 reviewer       │  │ orchestrator     0.92   ↑      5/5     100%     ████████▓    │ │
│    └─ ✓ 4 researcher     │  │ coder            0.85   →      3/4     75%      ███████░░    │ │
│                          │  │ reviewer          0.88   ↑      2/2     100%     ████████░    │ │
│                          │  │ researcher ✓      0.78   ↓      2/3     67%      ██████░░░    │ │
│                          │  └───────────────────────────────────────────────────────────────┘ │
│                          │                                                                   │
│                          │  ┌─ Collaboration Topology ─────────────────────────────────────┐ │
│                          │  │                                                               │ │
│                          │  │         orchestrator (0.92)                                   │ │
│                          │  │         ╱      │      ╲                                       │ │
│                          │  │      5msg    3msg    2msg                                     │ │
│                          │  │       ╱        │       ╲                                      │ │
│                          │  │   coder    reviewer  researcher✓                              │ │
│                          │  │   (0.85)   (0.88)    (0.78)                                  │ │
│                          │  │       ╲        │                                              │ │
│                          │  │      1msg    1msg                                             │ │
│                          │  │         ╲      │                                               │ │
│                          │  │        (code review handoff)                                  │ │
│                          │  └───────────────────────────────────────────────────────────────┘ │
│                          │                                                                   │
│                          │  Synergy: 0.84 │ Bottleneck: coder │ Conflicts: 0                │
╰─ 4 procs (3● 1✓) │ 2.9k tok │ ⚠0 ──────────── j/k:scroll  Tab:section  Esc:back  q:quit ────╯
```

### 视图 L：按 `L` — LLM 对话查看器（全屏覆盖层）

```
╭─ LLM CONVERSATION ━━━━━━━━━━━━━━━ PID 2 coder ━━━━━━━━━━ Step [3/5] ━━━━━━━━━━━━━━━━━━━━━━╮
│                                                                                               │
│  ┌─ REQUEST → claude-sonnet-4 ─────────────────────────────────────── 1,024 tokens ─────────┐│
│  │                                                                                          ││
│  │  [system]                                                                                ││
│  │  You are a coder agent. Your task is to implement features based on requirements.        ││
│  │  Allowed tools: read_file, write_file, run_command                                       ││
│  │  ...                                                                                     ││
│  │                                                                                          ││
│  │  [user]                                                                                  ││
│  │  Implement the REST API endpoint for user creation. The schema is:                       ││
│  │  - POST /api/users                                                                       ││
│  │  - Body: { "name": string, "email": string }                                             ││
│  │  - Response: 201 with created user object                                                ││
│  │                                                                                          ││
│  │  [assistant] (previous turn)                                                             ││
│  │  I'll start by reading the existing handler structure.                                   ││
│  │                                                                                          ││
│  │  [tool_result] (read_file: src/handlers/health.go)                                      ││
│  │  package handlers                                                                        ││
│  │  func HealthHandler(w http.ResponseWriter, r *http.Request) { ... }                      ││
│  │                                                                                          ││
│  └──────────────────────────────────────────────────────────────────────────────────────────┘│
│                                                                                               │
│  ┌─ RESPONSE ← claude-sonnet-4 ───────────────── 512 tokens ─────── latency: 2.8s ─────────┐│
│  │                                                                                          ││
│  │  [assistant]                                                                             ││
│  │  I'll create the user handler following the same pattern.                                ││
│  │                                                                                          ││
│  │  [tool_call] write_file                                                                  ││
│  │  path: src/handlers/user.go                                                              ││
│  │  content:                                                                                ││
│  │    package handlers                                                                      ││
│  │    import ("encoding/json"; "net/http")                                                   ││
│  │    type CreateUserRequest struct {                                                        ││
│  │        Name  string `json:"name"`                                                        ││
│  │        Email string `json:"email"`                                                       ││
│  │    }                                                                                     ││
│  │    ...                                                                                   ││
│  │                                                                                          ││
│  └──────────────────────────────────────────────────────────────────────────────────────────┘│
│                                                                                               │
│  ◀ Step 2: tool_call(read)  │  Step 3: tool_call(write)* │  Step 4: text ▶                   │
╰─ req:1024 tok │ resp:512 tok │ 2.8s ──── j/k:scroll  h/l:prev/next  y:copy  Esc:close ───────╯
```

**入口方式：**
- 任意视图按 `L` → 打开当前选中进程最新步骤的 LLM 交互
- Detail 视图 Recent Steps 区按 `Enter` → 打开该步骤的 LLM 交互
- Timeline 视图选中 llm 事件按 `Enter` → 打开该事件对应的 LLM 交互
- Heatmap 视图选中 segment 按 `Enter` → 打开该 segment 对应的消息

**导航方式：**
- `h/l` 切换步骤（底栏步骤指示器跟随更新）
- `j/k` 上下滚动长内容
- `Home/End` 跳转首尾
- `y` 复制当前内容到剪贴板
- `Esc` 关闭回到之前视图

### 视图 H：按 `H` — 历史抽屉（统一进程列表）

```
╭─ Rnix Dashboard ──── HISTORY ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╮
│                                                                                               │
│  ALL PROCESSES (7 total)                                          Sort: [time] name pid       │
│                                                                                               │
│  PID  ST  AGENT          MODEL              TOKENS  CREATED     ELAPSED  EXIT  REASON         │
│  ───  ──  ─────          ─────              ──────  ───────     ───────  ────  ──────         │
│    1  ●   orchestrator   claude-sonnet-4      820   10:30:00    5m12s     -     -             │
│    2  ●   coder          claude-sonnet-4    1,204   10:31:02    2m30s     -     -             │
│    3  ●   reviewer       claude-haiku-3       312   10:31:02    1m05s     -     -             │
│  ▸ 4  ✓   researcher     claude-haiku-3       580   10:30:12    0m58s    0     completed     │
│    5  ✓   planner        claude-sonnet-4      890   10:25:00    2m00s    0     completed     │
│    6  ✕   linter         claude-haiku-3        45   10:27:30    0m05s    1     lint failed   │
│    7  ✓   test-runner    claude-haiku-3       120   10:28:00    0m45s    0     completed     │
│                                                                                               │
│                                                                                               │
│                                                                                               │
│  ● Running: 3  │  ✓ Done: 3  │  ✕ Failed: 1  │  Total: 3,971 tok  │  Avg life: 1m42s        │
│                                                                                               │
╰───────────────────────── j/k:navigate  Enter:focus  L:llm  /:search  Esc:close  q:quit ──────╯
```

### 视图 F：选中已结束进程时的聚焦卡片

```
╭─ Rnix Dashboard ──── [1]Tree [2]Time [3]Heat [4]Detail [5]Intent [6]Sec [7]Trace [8]Eval ──╮
│                          │                                                                   │
│  AGENT TREE              │  TIMELINE ━━━━━━━━━━━━━━━━━━━━━━━━━━ (filtered: PID 4) ━━━━━━━━  │
│  ─────────               │  10:30:12 ● PID 4 spawn(researcher)                              │
│    ● 1 orchestrator      │  10:30:14 ◆ PID 4 open(/dev/llm)                                │
│    ├─ ● 2 coder          │  10:30:16 ■ PID 4 write(320 tok)                                │
│    ├─ ● 3 reviewer       │  10:30:45 ■ PID 4 write(260 tok)                                │
│    └─ ▸✓ 4 researcher    │  10:31:10 ✓ PID 4 exit(0)                                       │
│         exit(0) 58s      │                                                                   │
│                          │━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  │
│                          │                                                                   │
│                          │  FOCUS: PID 4 researcher ━━━━━━━━━━━━━━━━ ✓ Done (exit 0) ━━━━  │
│                          │  ┌─────────────────────────────────────────────────────────────┐  │
│                          │  │  Historical snapshot — lived 10:30:12–10:31:10 (58s)        │  │
│                          │  └─────────────────────────────────────────────────────────────┘  │
│                          │                                                                   │
│                          │  ┌─ Tokens ─────────┐ ┌─ Context (final) ─┐ ┌─ Status ────────┐ │
│                          │  │ ██████████░ 580/1k│ │ Sys  ███░░ 20%    │ │ ✓ Done          │ │
│                          │  │ rate: 10 tok/s    │ │ User ████░ 25%    │ │ skill:researcher│ │
│                          │  │ steps: 3 (final)  │ │ Asst █████ 45%    │ │ dev: llm        │ │
│                          │  │ lived: 58s        │ │ Tool ██░░░ 10%    │ │ ppid: 1         │ │
│                          │  └──────────────────┘ └───────────────────┘ └─────────────────┘ │
│                          │  ┌─ Intent ──────────┐ ┌─ Trace ──────────┐ ┌─ Result ────────┐ │
│                          │  │ ✓ research-specs  │ │ spans: 4         │ │ ✓ Completed     │ │
│                          │  │ ✓ summarize       │ │ total: 58s       │ │ output → PID 1  │ │
│                          │  │ (all done)        │ │ errors: 0        │ │                  │ │
│                          │  └──────────────────┘ └──────────────────┘ └─────────────────┘ │
╰─ 4 procs (3● 1✓) │ 2.9k tok │ ⚠0 ── Tab:cycle 1-8:jump L:llm H:history Esc:back q:quit ──╯
```

---

## 完整视图总览表（最终版）

| 快捷键 | 视图 | 布局 | 核心信息 |
|--------|------|------|---------|
| 默认 | 进程聚焦卡片 | 左 Tree + 右上 Timeline + 右下 2×3 卡片 | 选中进程 6 维摘要 |
| `1` | Tree 扩展 | 全屏统一进程表 | 所有进程（状态符号区分，不分生死） |
| `2` | Timeline 扩展 | 左 Tree + 右全高 Timeline | 完整事件流 + 筛选 + Step |
| `3` | Heatmap | 左 Tree + 右下 Heatmap | 逐段 token 分析 + 泄漏检测 |
| `4` | Detail | 左 Tree + 右下 Detail | 身份/能力/环境/步骤 |
| `5` | Intent DAG | 左 Tree + 右下 Intent | 任务依赖图 + 进度 |
| `6` | Security | 左 Tree + 右下 Security | 规则/事件/威胁等级 |
| `7` | Trace | 左 Tree + 右下 Trace | 甘特图 Span 树 |
| `8` | Eval | 左 Tree + 右下 Eval | 声誉/拓扑/协同 |
| **`L`** | **LLM 对话查看器** | **全屏覆盖层** | **完整 request/response + 步骤导航** |
| `H` | 历史抽屉 | 全屏覆盖层 | 统一进程列表 + 搜索排序 |
| 选中✓/✕ | 已结束进程聚焦 | 同默认视图 | 历史快照 + Timeline 自动过滤 |

设计文档已同步更新。还有需要调整的地方吗？