# 统一推理循环架构提案

> 交接文档 — 由 Party Mode 讨论生成，供后续 BMAD 流程（航向修正 → 新 Epic）消费
> 日期：2026-03-18
> 参与者：Decker、Winston（架构师）、John（PM）、Amelia（开发者）、Murat（测试架构师）

---

## 1. 变更背景

通过对 rnix 进程的 strace 跟踪分析，发现 OODA 推理循环（Epic 20 交付）存在 6 个问题，其中 2 个为阻塞性 bug，4 个为效率/鲁棒性问题。在问题分析过程中，团队形成了一个更深层的架构决策：**废弃 linear/OODA 双推理模式，统一为单一推理循环。**

---

## 2. 问题清单（strace 分析）

| # | 问题 | 严重度 | 分类 | 根因定位 |
|---|------|--------|------|----------|
| 1 | Orient→Act 链路丢失子路径（空 /dev/fs） | Critical | OODA 执行管线 | LLM 在 Decide 阶段生成了裸 `/dev/fs`，Orient 的计划未被忠实执行 |
| 2 | read-only 设备拒绝读操作（flags 问题） | Critical | VFS 权限模型 | `ooda.go:334` 和 `kernel.go:1256` 硬编码 `O_RDWR`，而 `hostfs.go:82` 要求 `O_RDONLY` |
| 3a | 工具错误未以 tool message 格式注入上下文 | High | 上下文管理 | `ooda.go:336` 错误路径只 return 字符串，不调用 `AppendToolResult`；linear 模式在 `kernel.go:1263` 正确注入 |
| 3b | 缺少重复失败熔断机制 | High | 循环控制 | `oodaReasonStep` 只有 `maxCycles` 限制，无连续错误计数 |
| 4 | Orient/Decide 两步 LLM 调用语义漂移 | Medium | OODA 设计 | 两次独立 LLM 调用不保证一致性 |
| 5 | 不支持并行 tool_call 执行 | Medium | Act 模型 | 每个 cycle 只处理单个 decision |
| 6 | Orient+Decide 双 LLM 调用开销（~20s 额外延迟） | Medium | 性能 | 两阶段职责重叠 |

---

## 3. 架构决策：统一推理循环

### 核心洞察（Decker）

> "不应该分什么 linear 和 planexec，这个选择应该也是智能过程，由 LLM 根据任务智能选择需不需要 plan，还是直接执行。配置可以关闭 plan 功能，这样就默认不需要 plan，直接执行。"

### 决策结论

- **废弃双模式** — 不再区分 `reasoning: "linear"` 和 `reasoning: "ooda"`
- **统一推理循环** — 只保留一个 `reasonStep`，LLM 自主决定行为
- **Planning 作为能力而非模式** — LLM 在循环中可以选择 `plan` action，也可以直接 `tool_call`
- **配置项** — `planning: true|false`（可选，默认 true）；`false` 时 prompt 不注入 plan 指引

### 统一循环架构

```
┌─────────────────────────────────────────────┐
│              统一推理循环                      │
│                                              │
│  LLM 输入：任务 + 上下文 + 工具列表           │
│                    ↓                         │
│  LLM 自主决策（每步一次 LLM 调用）：          │
│    ├─ "plan"       → 输出执行计划，写入上下文  │
│    ├─ "tool_call"  → 直接执行工具调用          │
│    ├─ "spawn"      → 启动子进程               │
│    ├─ "specialize" → 动态加载 skill           │
│    ├─ "replan"     → 修正计划                 │
│    └─ "complete"   → 输出最终结果             │
│                                              │
│  配置开关：planning: true|false (默认 true)   │
│  → false 时不在 prompt 中注入 plan 指引       │
└─────────────────────────────────────────────┘
```

### 与当前设计的对比

| 维度 | 当前（双模式） | 统一循环 |
|------|---------------|---------|
| 代码路径 | 2 条（`reasonStep` + `oodaReasonStep`） | 1 条 |
| 每步 LLM 调用 | linear: 1 次，OODA: 2 次 | 1 次 |
| 配置复杂度 | `reasoning: "linear\|ooda"` | `planning: true\|false`（可选） |
| 源文件 | `kernel.go` + `ooda.go` | 只有 `kernel.go` |
| 智能程度 | 人类硬编码模式选择 | LLM 运行时自主判断 |

---

## 4. 影响范围（全量扫描结果）

### 需删除的文件

| 文件/目录 | 内容 |
|-----------|------|
| `kernel/ooda.go` | OODA 全部类型、函数、常量、模板（~530 行） |
| `kernel/ooda_test.go` | OODA 核心测试（~810 行） |
| `kernel/ooda_reasoning_test.go` | OODA 推理配置测试（~620 行） |
| `lib/agents/ooda-demo/` | OODA 演示 agent（目录） |
| `agents/testdata/ooda-agent/` | OODA 测试 agent（目录） |

### 需修改的源文件

| 文件 | 改动说明 |
|------|----------|
| `kernel/kernel.go` | 删除 OODA 分支（~20 行），扩展 `reasonStep` 添加 `ActionPlan` 处理 + 熔断机制，统一 `toolProtocol` prompt |
| `kernel/process.go` | 删除 `oodaEnabled`、`oodaState`、`IsOODA()`、`GetOODAState()`、`SetOODAPhase()`（~40 行） |
| `agents/types.go` | `Reasoning string` → `Planning *bool`（可选布尔） |
| `agents/loader.go` | 删除 reasoning 验证逻辑，添加 planning 字段处理 |
| `internal/types/types.go` | 删除 `LogOODA` 常量 |
| `cmd/rnix/main.go` | 删除 OODA 相关注释 |
| `cmd/rnix/lineage.go` | `"ooda-specialize"` → `"specialize"` |

### 需修改的测试文件

| 文件 | 改动说明 |
|------|----------|
| `kernel/stem_integration_test.go` | 删除 `reasoning: "ooda"` 和 `IsOODA()` 断言 |
| `kernel/diffmemory_test.go` | 更新 OODA 相关注释 |
| `kernel/diffmemory_integration_test.go` | 重写 specialize 测试（~30 处引用） |
| `kernel/lineage_integration_test.go` | 重写 specialize lineage 测试（~15 处引用） |
| `agents/loader_reasoning_test.go` | 重写为 planning 字段测试 |

### 需修改的配置文件

| 文件 | 改动说明 |
|------|----------|
| `lib/agents/stem/agent.yaml` | `reasoning: ooda` → `planning: true`（或删除此字段，默认为 true） |

### 不修改

- `_bmad-output/` 下所有历史文档 — 保留为历史记录
- `README.md` / `README_zh.md` — 最终统一更新

---

## 5. 方案要点

### 5.1 Bug 修复（融入统一循环）

**#2 VFS flags 降级：**
```go
// reasonStep 中的工具调用：根据 data 内容选择 flags
flags := vfs.O_RDONLY
if len(action.ToolData) > 2 { // "{}" = 2 bytes
    flags = vfs.O_RDWR
}
toolFD, err := k.vfs.Open(proc.PID, action.ToolPath, flags)
```

**#3a 错误注入上下文：**
所有工具调用的错误路径必须调用 `AppendToolResult` 将错误注入 LLM 上下文。当前 linear 模式的 `reasonStep`（kernel.go:1263）已正确实现，统一循环沿用此模式。

**#3b 熔断机制：**
```go
var consecutiveToolErrors int
// 在 tool_call 错误时 consecutiveToolErrors++
// 成功时 reset 为 0
// >= 3 时 finishProcess 并报告 circuit_breaker
```

### 5.2 统一 Prompt 模板

```go
const toolProtocol = `
[Tool Call Protocol]
To use a tool, respond with ONLY a JSON object (no markdown, no extra text):
{"action": "tool_call", "tool": "<vfs-device-path>", "data": {<tool-specific-payload>}}

Available VFS device paths:
  - Read file: tool="/dev/fs/path/to/file", data={}
  - Write file: tool="/dev/fs/path/to/file", data={"content": "..."}
  - List directory: tool="/dev/fs/path/to/dir", data={"op": "list"}
  - Run command: tool="/dev/shell", data={"command": "..."}
  - LLM call: tool="/dev/llm/<provider>", data={"intent": "..."}
  - MCP tool: tool="/dev/mcp/<server>/<tool>", data={...}

If no tool call is needed, respond with plain text (your final answer).`

// planning 为 true 时额外注入：
const planProtocol = `

To plan before executing (for complex multi-step tasks):
{"action": "plan", "steps": ["step1 description", "step2 description", ...], "reason": "..."}

Use planning when the task requires multiple coordinated steps. For simple tasks, use tool_call directly.`
```

### 5.3 ActionType 扩展

```go
const (
    ActionText       ActionType = "text"
    ActionToolCall   ActionType = "tool_call"
    ActionPlan       ActionType = "plan"       // 新增
    ActionSpawn      ActionType = "spawn"      // 从 OODA 继承
    ActionComplete   ActionType = "complete"   // 从 OODA 继承
    ActionReplan     ActionType = "replan"     // 从 OODA 继承
    ActionSpecialize ActionType = "specialize" // 从 OODA 继承
)
```

### 5.4 Specialize 能力保留

`oodaActSpecialize` 的逻辑（动态加载 skill）需要迁移到统一 `reasonStep` 中作为 `ActionSpecialize` 分支，包含：
- skill 加载与注入
- AllowedDevices 更新
- DiffMemory 记录
- Lineage 记录

---

## 6. 测试策略

### 新增测试矩阵

| 场景 | 验证点 |
|------|--------|
| LLM 返回 `tool_call` | 直接执行，结果以 tool message 注入上下文 |
| LLM 返回 `plan` | Plan 写入上下文，下一步 LLM 按 plan 执行 |
| `planning: false` 时 LLM 返回 `plan` | 按 replan 处理或忽略 |
| LLM 返回 `complete` | 正常退出 code=0 |
| LLM 返回 `spawn` | 创建子进程并等待 |
| LLM 返回 `specialize` | 动态加载 skill |
| 连续 3 次 tool 失败 | 熔断退出 code=1 |
| tool 错误 | 错误以 `role: "tool"` 注入上下文 |
| /dev/fs 读取 | flags 自动降级为 O_RDONLY |

### 删除的测试

- `kernel/ooda_test.go`（全部）
- `kernel/ooda_reasoning_test.go`（全部）
- 相关 ATDD checklist 不再适用

---

## 7. 后续流程建议

1. **航向修正** `bmad-correct-course` — 正式评估此变更对现有 PRD 和架构的影响
2. **更新架构文档** — 推理循环章节重写
3. **创建新 Epic** — "统一推理循环"（预估 3-5 个 Story）
4. **Sprint Planning** → **Story** → **Dev** → **Code Review** 循环

---

## 附录：关键代码位置速查

| 内容 | 文件 | 行号 |
|------|------|------|
| OODA 类型定义 | `kernel/ooda.go` | 15-51 |
| OODA prompt 模板 | `kernel/ooda.go` | 53-79 |
| oodaReasonStep 主循环 | `kernel/ooda.go` | 82-221 |
| oodaActToolCall（flags bug） | `kernel/ooda.go` | 333-356 |
| linear reasonStep 主循环 | `kernel/kernel.go` | 866-1200+ |
| linear toolProtocol 注入 | `kernel/kernel.go` | 58-72, 989-991 |
| linear tool_call flags bug | `kernel/kernel.go` | 1256 |
| linear 错误正确注入 | `kernel/kernel.go` | 1262-1271 |
| HostFS flags 检查 | `drivers/fs/hostfs.go` | 82-84 |
| Process OODA 字段 | `kernel/process.go` | 88-90, 298-331 |
| Reasoning 模式分支 | `kernel/kernel.go` | 362-363, 612-628 |
| LogOODA 常量 | `internal/types/types.go` | 170 |
| Agent Reasoning 字段 | `agents/types.go` | 27 |
| Agent loader 验证 | `agents/loader.go` | 68-71 |
