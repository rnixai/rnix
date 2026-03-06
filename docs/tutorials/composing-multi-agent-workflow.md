# 教程 3：组合多智能体工作流

本教程带你使用 Rnix Compose 编排多个智能体协作完成一个复杂任务，并用 `rnix top` 实时监控执行过程。

---

## 前置条件

- 已完成 [教程 1：编写第一个 Skill](writing-first-skill.md)（了解 Skill 和 Agent 的创建流程）
- Rnix 已安装并可运行
- 对 Rnix 的进程、VFS 概念有基本了解（参考 [核心概念文档](../concepts.md)）

---

## 你将学到什么

1. 如何设计多智能体 DAG 工作流
2. 如何编写 `rnix-compose.yaml` 定义智能体依赖关系
3. 如何用 `rnix compose up` 启动工作流
4. 如何用 `rnix top` 实时监控执行
5. 如何用管道语法和 AgentShell 脚本实现更灵活的编排

---

## 步骤一：设计多智能体工作流

### 场景

我们要构建一个代码审查工作流，包含三个阶段：

1. **分析器（analyzer）** — 读取代码文件，输出代码质量分析
2. **文档生成器（doc-gen）** — 基于分析结果，生成改进文档
3. **质量检查器（checker）** — 检查分析和文档的质量

### DAG 依赖关系

```
analyzer ──→ doc-gen ──→ checker
```

`doc-gen` 依赖 `analyzer` 完成后才启动，`checker` 依赖 `doc-gen` 完成后才启动。这是一个简单的线性 DAG。

Rnix Compose 的 DAG 调度引擎会自动解析依赖，按拓扑排序确定执行顺序。如果依赖图允许并行（比如 A 和 B 都依赖 C，则 A 和 B 可以并行执行），引擎会自动并行调度。

### 准备 Agent

你可以复用教程 1 中创建的 Agent，或者使用已有的 `code-analyst`。本教程使用内置的 `code-analyst` Agent 以及默认 Agent（无需额外创建）。

---

## 步骤二：编写 rnix-compose.yaml

在项目根目录创建 `rnix-compose.yaml`：

```yaml
version: "1.0"
intent: "代码审查工作流"
model: "haiku"
agents:
  analyzer:
    intent: "分析 kernel/kernel.go 的代码质量"
    agent: "code-analyst"
  doc-gen:
    intent: "基于分析结果生成改进建议文档"
    depends_on:
      analyzer: completed
  checker:
    intent: "检查分析和建议的质量与完整性"
    depends_on:
      doc-gen: completed
```

### 字段说明

| 字段 | 说明 |
|------|------|
| `version` | Compose 规范版本（当前为 `"1.0"`） |
| `intent` | 工作流的整体意图描述 |
| `model` | 全局默认模型（各 Agent 可覆盖） |
| `agents` | Agent 定义映射表 |
| `agents.<name>.intent` | 该 Agent 的执行意图 |
| `agents.<name>.agent` | 指定使用的 Agent 定义（可选，默认使用通用 Agent） |
| `agents.<name>.depends_on` | 依赖关系：`<上游agent名>: completed` |

### DAG 调度引擎工作原理

Compose 引擎读取 `rnix-compose.yaml` 后：

1. **解析依赖图** — 将所有 Agent 和 `depends_on` 关系构建为有向无环图（DAG）
2. **拓扑排序** — 确定执行层级（无依赖的 Agent 在第一层，依赖它们的在第二层，以此类推）
3. **层级并行** — 同一层级的 Agent 可并行执行
4. **结果传递** — 上游 Agent 的输出注入下游 Agent 的上下文

---

## 步骤三：运行 rnix compose up

```bash
rnix compose up
```

Compose 引擎启动工作流：

```
compose | 代码审查工作流 | starting
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
[layer 1/3] analyzer
  PID 5 | code-analyst | running...
  PID 5 | completed | 0 | 3.8s | 1,450 tokens ✓

[layer 2/3] doc-gen
  PID 6 | default | running...
  PID 6 | completed | 0 | 4.2s | 1,180 tokens ✓

[layer 3/3] checker
  PID 7 | default | running...
  PID 7 | completed | 0 | 2.5s | 890 tokens ✓

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
compose | completed | 3/3 agents | 10.5s | 3,520 tokens
```

每个 Agent 按 DAG 顺序依次执行，后续 Agent 自动获得上游 Agent 的输出作为上下文。

---

## 步骤四：使用 rnix top 实时监控

在工作流运行期间，打开另一个终端运行：

```bash
rnix top
```

你会看到一个 TUI（终端用户界面）实时显示所有进程的状态：

```
rnix top — 实时监控                           刷新: 1s
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
PID  STATE    AGENT         TOKENS   ELAPSED  INTENT
5    running  code-analyst  1,200    2.3s     分析 kernel/kernel.go…
6    created  default       0        -        基于分析结果生成…
7    created  default       0        -        检查分析和建议…
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
进程: 3 | 运行中: 1 | 等待: 2 | 完成: 0
Token 总量: 1,200 | 已用时: 2.3s
```

`rnix top` 会持续刷新，你可以实时观察：
- 哪些 Agent 正在运行（`running`）
- 哪些在等待依赖完成（`created`）
- Token 消耗和执行时间

按 `q` 退出 `rnix top`。

---

## 步骤五：查看结果

### 查看 compose 完成输出

`rnix compose up` 完成后会输出各 Agent 的执行摘要。要获取更详细的结果，可以使用 JSON 输出：

```bash
rnix compose up --json
```

JSON 输出包含每个 Agent 的完整结果：

```json
{
  "ok": true,
  "data": {
    "intent": "代码审查工作流",
    "agents": [
      {"name": "analyzer", "pid": 5, "exit_code": 0, "elapsed_ms": 3800, "tokens": 1450},
      {"name": "doc-gen", "pid": 6, "exit_code": 0, "elapsed_ms": 4200, "tokens": 1180},
      {"name": "checker", "pid": 7, "exit_code": 0, "elapsed_ms": 2500, "tokens": 890}
    ],
    "total_elapsed_ms": 10500,
    "total_tokens": 3520
  }
}
```

### 查看推理日志

用 `rnix log` 查看各智能体的推理过程：

```bash
rnix log
```

日志按时间和进程分组，展示每个 Agent 的推理步骤和决策过程。

---

## 步骤六：清理

如果工作流中途失败或需要停止，使用：

```bash
rnix compose down
```

这会终止所有 compose 启动的进程并清理资源。

---

## 扩展场景

### 管道语法替代

对于简单的线性工作流，可以用管道语法代替 compose 文件：

```bash
rnix -i 'spawn "分析 kernel/kernel.go" --agent=code-analyst | spawn "生成改进文档" | spawn "质量检查"'
```

管道语法 `|` 将前一个 Agent 的输出自动注入为下一个 Agent 的 `[PIPE_INPUT]` 上下文。

### 使用变量和环境传递

结合 AgentShell 的环境变量，让工作流更灵活：

```bash
rnix -i '
export TARGET=./kernel/kernel.go
spawn "分析 $TARGET 的代码质量" --agent=code-analyst | spawn "生成改进文档"
'
```

或在 `rnix-compose.yaml` 中使用 environment：

```yaml
agents:
  analyzer:
    intent: "分析代码质量"
    agent: "code-analyst"
```

### 使用 if/else 条件分支

当分析发现问题时生成修复方案，否则输出通过报告：

```
result = spawn "分析 kernel/kernel.go" --agent=code-analyst
if $result.exitcode == 0
  spawn "生成通过报告"
else
  spawn "生成修复方案" on-error spawn "记录分析失败"
end
```

AgentShell 支持完整的控制结构：
- **`if/else/end`** — 根据上游结果条件分支
- **`on-error`** — 内联错误处理（失败时自动执行备选操作）
- **变量赋值** — `result = spawn "..."` 捕获执行结果
- **属性访问** — `$result.exitcode` 访问退出码

### rnix compose down

如果工作流中有残留进程（比如某个 Agent 挂起），用 `compose down` 强制清理：

```bash
rnix compose down
```

---

## 下一步

恭喜！你已经掌握了 Rnix 的三大核心技能：

1. **编写 Skill 和 Agent** — 创建可复用的智能体能力
2. **调试问题** — 用 strace 追踪和定位错误
3. **编排工作流** — 用 Compose 和管道组合多智能体协作

### 进阶学习

- [核心概念文档](../concepts.md) — 深入理解 Rnix 的 OS 设计哲学
- [参考手册](../reference.md) — 查阅所有 Syscall、VFS 路径、CLI 命令的完整定义
- [教程 1：编写第一个 Skill](writing-first-skill.md) — 回顾 Skill 编写细节
- [教程 2：调试第一个 bug](debugging-first-bug.md) — 回顾调试技巧

## 相关文档

- [核心概念](../concepts.md) — 进程、VFS、Skill 的心智模型
- [参考手册：CLI 命令参考](../reference.md) — compose up/down、top、log 的完整参数说明
- [参考手册：IPC 架构](../reference.md) — Compose 引擎的内部通信机制
