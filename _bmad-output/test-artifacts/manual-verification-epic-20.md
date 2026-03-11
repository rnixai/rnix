# Epic 20 手工验证指南：自主智能体（OODA + 干细胞分化）

## 概述

本文档提供 Epic 20 所有 Story 的手工验证步骤，用于在自动化测试之外对功能进行端到端的人工确认。

## 前置准备

Daemon 由 rnix 自动按需启动（`EnsureDaemon`），无需手动管理。

```bash
# 1. 构建最新版本
make build

# 2. 确认 daemon 可用
./rnix ps

# 3. 确认示例 OODA agent 存在
ls lib/agents/ooda-demo/agent.yaml
cat lib/agents/ooda-demo/agent.yaml
# 应包含 reasoning: ooda

# 4. 确认 stem agent 存在
ls lib/agents/stem/agent.yaml
cat lib/agents/stem/agent.yaml
# 应包含 name: stem, reasoning: ooda, skills: []
```

> **提示**：OODA 模式的智能体每轮循环消耗 2-3 次 LLM 调用（Orient + Decide + 可选 Act），token 消耗高于线性模式。Stem Agent 分化本身不调用 LLM（使用关键词匹配），但后续 OODA 循环会调用 LLM。

---

## Story 20.1: OODA 循环核心实现

### OODA 四阶段验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | Observe 阶段 | 启用 strace 后 spawn 一个 OODA 智能体，观察事件流 | 出现 `OODAObserve` 事件，包含环境快照（进程列表、上下文信息） | [ ] |
| 2 | Orient 阶段 | 继续观察事件流 | 出现 `OODAOrient` 事件，LLM 评估当前状态与目标的偏差 | [ ] |
| 3 | Decide 阶段 | 继续观察事件流 | 出现 `OODADecide` 事件，LLM 输出结构化 JSON 决策（action/target/data/reason） | [ ] |
| 4 | Act 阶段 | 继续观察事件流 | 出现 `OODAAct` 事件，执行决策结果反馈到下一轮 Observe | [ ] |
| 5 | 完整循环 | 观察事件流直到进程完成 | OODACycle 事件显示循环轮数和框架开销时间 | [ ] |

### 决策类型

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 6 | tool_call 决策 | 给 OODA 智能体一个需要调用工具的任务 | Decide 输出 `action: tool_call`，Act 阶段通过 VFS 调用工具 | [ ] |
| 7 | complete 决策 | 给 OODA 智能体一个简单任务 | 任务完成时 Decide 输出 `action: complete`，进程正常退出（exit code 0） | [ ] |
| 8 | replan 决策 | 给 OODA 智能体一个需要调整计划的任务 | Decide 输出 `action: replan`，不执行外部操作，下一轮重新评估 | [ ] |
| 9 | 最大循环数 | 给 OODA 智能体一个无法完成的任务 | 超过最大循环数后进程终止（exit code 1，reason: max ooda cycles exceeded） | [ ] |

### OODA 与默认模式兼容

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 10 | 默认线性模式不受影响 | `rnix -i "打招呼"` （不指定 OODA agent） | 使用默认线性推理循环，不出现 OODA 相关事件 | [ ] |

### NFR41 性能

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 11 | 框架开销 | 查看 OODACycle 事件中的 `framework_overhead` 字段 | 单轮 OODA 循环框架开销 <= 200ms（不含 LLM 调用时间） | [ ] |

---

## Story 20.2: OODA 配置与任务式指挥

### agent.yaml 配置

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | OODA 模式声明 | `rnix -i "分析代码" --agent=ooda-demo` | 智能体使用 OODA 循环推理（事件流中出现 OODAObserve 等事件） | [ ] |
| 2 | 默认模式 | `rnix -i "分析代码" --agent=greeter`（无 reasoning 字段的 agent） | 智能体使用默认线性推理模式 | [ ] |
| 3 | 无效 reasoning 值 | 创建 agent.yaml 含 `reasoning: invalid`，尝试加载 | Agent loader 报错：invalid reasoning mode | [ ] |
| 4 | linear 显式声明 | 创建 agent.yaml 含 `reasoning: linear` | 加载成功，使用默认线性推理模式 | [ ] |

### 任务式指挥

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | OODA 自主 spawn 子智能体 | 给 OODA 智能体一个复杂任务，需要委派子任务 | Decide 阶段输出 `action: spawn`，Act 阶段自主 spawn 子智能体 | [ ] |
| 6 | spawn 时指定 agent | OODA 智能体 Decide 输出含 `data.agent` 字段 | 使用指定 agent 模板 spawn 子进程 | [ ] |
| 7 | spawn 不指定 agent | OODA 智能体 Decide 输出不含 `data.agent` 字段 | spawn 裸进程（无 agent 定义，使用线性推理） | [ ] |
| 8 | spawn 不存在的 agent | OODA 智能体 Decide 指定不存在的 agent 名 | 返回错误信息，不中断 OODA 循环（下一轮可重新决策） | [ ] |

### 子智能体 OODA 继承

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 9 | 子智能体继承 OODA | OODA 父智能体 spawn 子智能体，子 agent.yaml 也声明 `reasoning: ooda` | 子进程也以 OODA 循环运行（事件流中出现子进程的 OODA 事件） | [ ] |
| 10 | 子智能体使用线性模式 | OODA 父智能体 spawn 子智能体，子 agent.yaml 不声明 reasoning | 子进程使用默认线性推理模式 | [ ] |

### 进程状态查看

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 11 | OODA 进程可见 | `rnix ps`（OODA 进程执行中） | 进程列表中可见 OODA 智能体，状态为 Running | [ ] |

---

## Story 20.3: Stem Agent 与自动分化

### Stem Agent 基本分化

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 基本分化 | `rnix -i "analyze code quality" --agent=stem` | Stem Agent 自动匹配 Skill（如 code-analysis），输出分化过程日志 | [ ] |
| 2 | 分化过程日志 | 观察场景 1 的输出 | 实时显示分化过程：`differentiating: matching skills for intent ...` → `differentiating: loading skills [...]` | [ ] |
| 3 | 无匹配 Skill | `rnix -i "一个完全无关的意图xyz" --agent=stem` | 无 Skill 匹配时以裸进程运行（无额外 Skill 加载），OODA 循环正常启动 | [ ] |
| 4 | 多 Skill 匹配 | `rnix -i "analyze code and git history" --agent=stem` | 匹配到多个相关 Skill，按匹配度排序加载 | [ ] |

### Skill 匹配验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | 关键词匹配 | 意图文本包含 Skill 名称/描述中的关键词 | 正确匹配到对应 Skill | [ ] |
| 6 | 大小写不敏感 | `rnix -i "Code Analysis" --agent=stem` | 不区分大小写匹配 Skill | [ ] |
| 7 | CJK 局限性 | `rnix -i "分析代码" --agent=stem`（纯中文意图） | 已知限制：纯 CJK 意图无法匹配英文 Skill 元数据，以裸进程运行 | [ ] |

### NFR42 性能

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 8 | 分化耗时 | 查看 `StemDifferentiate` 事件中的 `duration_ms` 字段 | Skill 匹配和加载 <= 3s | [ ] |

### 分化后状态

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 9 | ps 显示分化结果 | 分化完成后 `rnix ps` | 进程列表显示加载的 Skill 名称列表 | [ ] |
| 10 | Stem + OODA | 分化完成后观察推理事件 | Stem Agent 分化后以 OODA 循环运行（因 agent.yaml 声明 `reasoning: ooda`） | [ ] |

---

## Story 20.4: 渐进式特化与分化记忆

### 分化记忆

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 记忆记录 | 首次 `rnix -i "analyze code" --agent=stem`，查看 StemDifferentiate 事件 | 事件中 `from_memory: false`，通过关键词匹配 | [ ] |
| 2 | 记忆复用 | 再次执行相同意图 `rnix -i "analyze code" --agent=stem` | 事件中 `from_memory: true`，直接从记忆复用分化路径 | [ ] |
| 3 | 意图规范化 | 先 `rnix -i "analyze code" --agent=stem`，再 `rnix -i "code analyze" --agent=stem` | 两个意图命中同一记忆条目（token 排序后签名相同） | [ ] |
| 4 | 不同意图独立记忆 | 先后执行两个不同意图的 stem spawn | 各自独立记录分化路径 | [ ] |

### OODA 动态特化（specialize）

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | 动态加载 Skill | OODA 循环中 Decide 输出 `action: specialize, target: <skill-name>` | 成功加载额外 Skill，日志显示 `specialized: dynamically loaded skill ...` | [ ] |
| 6 | 重复加载检查 | OODA 尝试再次加载已加载的 Skill | 返回 `skill "xxx" already loaded` 提示，不重复加载 | [ ] |
| 7 | 加载不存在的 Skill | OODA specialize 目标 Skill 不存在 | 返回 `specialize error: skill "xxx" load failed` 错误信息，不中断循环 | [ ] |
| 8 | Skill Body 注入 | specialize 成功后观察下一轮 OODA | 新 Skill 的内容注入上下文，LLM 可感知并使用新能力 | [ ] |
| 9 | 工具权限扩展 | specialize 成功后，新 Skill 的工具路径可用 | 新 Skill 的 allowed_tools 追加到进程的 AllowedDevices，工具调用不被权限拒绝 | [ ] |

### 特化后状态

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 10 | ps 反映动态特化 | 动态特化后 `rnix ps` | 进程的 Skill 列表更新，包含动态加载的 Skill | [ ] |
| 11 | 特化更新记忆 | 动态特化后再次 spawn 相同意图 | 记忆条目包含初始 + 动态加载的全部 Skill | [ ] |

---

## Story 20.5: 分化谱系图

### 基本谱系查询

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 查看初始分化谱系 | `rnix -i "analyze code" --agent=stem`，然后 `rnix lineage <pid>` | 展示从 Stem Agent 到当前特化体的路径，包含 initial 阶段、加载的 Skill 列表、触发意图 | [ ] |
| 2 | 渐进式特化谱系 | Stem Agent 经过初始分化 + OODA 动态特化后 `rnix lineage <pid>` | 谱系包含多个事件：initial 阶段 + 一个或多个 progressive 阶段 | [ ] |
| 3 | 时间点标注 | 查看谱系中每个事件 | 每次 Skill 加载标注时间点（timestamp） | [ ] |
| 4 | 触发原因 | 查看谱系中每个事件 | initial 事件的 trigger 为原始意图；progressive 事件的 trigger 为决策理由 | [ ] |
| 5 | 记忆来源标识 | 从记忆复用分化的进程查看谱系 | initial 事件的 source 显示 "memory-reuse" 而非 "keyword-match" | [ ] |

### 谱系查询边界情况

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 6 | 非 Stem Agent | `rnix lineage <pid>`（非 stem 进程） | 显示 "Process <pid> has no differentiation lineage" | [ ] |
| 7 | PID 不存在 | `rnix lineage 99999` | 显示 NOT_FOUND 错误 | [ ] |
| 8 | 无效 PID | `rnix lineage abc` | 显示 PID 格式错误提示 | [ ] |
| 9 | 缺少参数 | `rnix lineage` | 显示使用帮助 | [ ] |
| 10 | JSON 输出 | `rnix lineage <pid> --json` | JSON 格式输出谱系数据（timestamp_ms 为毫秒整数） | [ ] |

### 谱系独立性

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 11 | 不同进程独立 | 多次 spawn stem agent，各自 `rnix lineage <pid>` | 每个进程有独立的谱系记录，互不干扰 | [ ] |

---

## 端到端完整流程验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | OODA 完整循环 | (1) `rnix -i "分析当前目录结构" --agent=ooda-demo` (2) 观察 OODA 四阶段事件 (3) 等待任务完成 | Observe -> Orient -> Decide -> Act 循环正确执行，任务完成 | [ ] |
| 2 | Stem 分化 + OODA | (1) `rnix -i "analyze code quality" --agent=stem` (2) 观察分化过程 (3) 观察 OODA 循环 | 先分化匹配 Skill → 然后以 OODA 模式执行 → 完成 | [ ] |
| 3 | 分化记忆复用 | (1) 首次 `rnix -i "analyze code" --agent=stem` (2) 第二次 `rnix -i "analyze code" --agent=stem` | 第二次 from_memory=true，分化速度更快 | [ ] |
| 4 | 谱系全链路 | (1) Stem spawn + 分化 (2) OODA 循环中 specialize (3) `rnix lineage <pid>` | 谱系显示 initial + progressive 两阶段，含时间、Skill、触发原因 | [ ] |
| 5 | 任务式指挥层级 | (1) OODA 父智能体执行复杂任务 (2) 父智能体 spawn 子智能体 (3) 子智能体独立完成 (4) `rnix ps` 查看层级 | 父子进程都可见，子进程独立执行（如子 agent.yaml 声明 ooda 则子进程也以 OODA 运行） | [ ] |
| 6 | Stem + 增量意图 | (1) `rnix apply "代码分析项目" --yes` (2) intent 中某子任务使用 stem agent | stem agent 在 intent 执行流程中也正确分化和运行 | [ ] |

---

## 关键注意事项

1. **OODA 每轮 2-3 次 LLM** -- Orient + Decide 各一次，Act 可能再一次。Token 消耗远高于线性模式
2. **MaxSteps 复用为 MaxCycles** -- OODA 模式下 `MaxSteps` 表示最大 OODA 循环轮数，非推理步数
3. **NFR41** -- 单轮 OODA 循环框架开销 <= 200ms（不含 LLM 调用时间）
4. **NFR42** -- Stem Agent Skill 匹配和加载 <= 3s
5. **CJK 意图局限** -- 纯中文意图无法有效匹配英文 Skill 元数据（关键词匹配限制），使用英文关键词可改善
6. **Stem Agent 是 OODA 模式** -- `lib/agents/stem/agent.yaml` 声明 `reasoning: ooda`，分化后以 OODA 循环运行
7. **分化记忆仅内存** -- daemon 重启后分化记忆丢失，首次 spawn 需重新匹配
8. **谱系随进程回收** -- 进程变为 Dead 后谱系数据随 Process 对象回收，需在 Running/Zombie 状态查询
9. **reasoning 优先级** -- `agent.yaml` 的 `reasoning` 字段优先于 `SpawnOpts.ReasoningMode`
10. **specialize 不中断循环** -- 动态特化是 OODA Act 阶段的一个普通操作，不中断循环
11. **ooda-demo agent 的 skills** -- `lib/agents/ooda-demo/agent.yaml` 引用了 `code-analysis` skill，需确保该 skill 存在
12. **Lineage 时间格式** -- IPC 返回的时间戳为 `timestamp_ms`（Unix 毫秒），CLI 渲染为人类可读格式

## 验证记录

| 字段 | 值 |
|------|-----|
| 验证人 | |
| 验证日期 | |
| 构建版本 | |
| 总用例数 | 58 |
| 通过数 | |
| 失败数 | |
| 备注 | |
