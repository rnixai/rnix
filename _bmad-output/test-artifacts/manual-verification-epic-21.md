# Epic 21 手工验证指南：Token 经济、声誉与 Skill 协同

## 概述

本文档提供 Epic 21 所有 Story 的手工验证步骤，用于在自动化测试之外对功能进行端到端的人工确认。Epic 21 为 Rnix 引入 Token 预算池管理、合约 SLA 评估、声誉系统与自动选择、Skill Synergy 声明与自动检测、以及 Skill 组合矩阵功能。

## 前置准备

### 构建

```bash
make build
```

### 启动 Daemon

```bash
# 确保旧 daemon 已停止
./rnix daemon stop 2>/dev/null; true

# 启动 daemon
./rnix daemon start
```

### 准备测试用 Agent 和 Skill

```bash
# 确认 lib/agents/ 下有可用的 Agent 模板
ls lib/agents/

# 确认 lib/skills/ 下有可用的 Skill
ls lib/skills/
```

### 准备测试用 rnix-compose.yaml

```bash
# 创建带预算池和 SLA 的 compose 配置
cat > rnix-compose-test.yaml << 'EOF'
version: "1.0"
intent: "预算池测试工作流"
token_budget: 50000

agents:
  reviewer:
    intent: "审查代码变更"
    agent: code-reviewer
    priority: high
    context_budget: 20000
    sla:
      max_tokens: 15000
      max_duration_ms: 60000
      output_format: "json"
  summarizer:
    intent: "生成审查摘要"
    agent: summarizer
    priority: normal
    sla:
      max_tokens: 8000
      max_duration_ms: 30000
      output_format: "markdown"
    depends_on:
      reviewer: completed
  formatter:
    intent: "格式化报告"
    agent: formatter
    priority: low
    depends_on:
      summarizer: completed
EOF
```

### 准备带 Synergy 的 Skill

```bash
# 确认 skills/testdata/with-synergy/ 和 with-synergy-b/ 存在
ls skills/testdata/with-synergy/SKILL.md
ls skills/testdata/with-synergy-b/SKILL.md
```

### 验证所需工具

```bash
# 确认 rnix 二进制可用
./rnix --version

# 确认 jq 可用（可选，用于 JSON 格式化）
jq --version
```

---

## Story 21.1: Token 预算池与分配调度

### Compose 预算池创建与配额分配

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | Compose 预算池创建 | (1) 使用包含 `token_budget: 50000` 的 compose 配置 (2) 执行 `./rnix compose up -f rnix-compose-test.yaml` | 系统创建预算池并按优先级分配初始配额，高优先级（reviewer, priority=high）获得更大比例 | [ ] |
| 2 | 优先级驱动配额分配 | (1) 观察场景 1 启动日志 (2) 通过 IPC 查询预算池状态 | 高优先级 Agent 配额 > 普通优先级 > 低优先级。权重分配：high=10, normal=5, low=1 | [ ] |
| 3 | 预算池状态查询 | (1) Compose 编排运行中 (2) 通过 daemon IPC 发送 `budget_status` 请求 | 返回总预算 50000、已分配配额、已消耗 token、各 Agent 的配额和消耗情况 | [ ] |
| 4 | 预算耗尽处理 | (1) 使用较小总预算（如 `token_budget: 100`）运行 compose (2) 观察执行过程 | 当总消耗 >= 总预算时，后续层的 Agent 不再执行，Compose 标记为 budget_exhausted | [ ] |
| 5 | 无预算池向后兼容 | (1) 使用不含 `token_budget` 字段的 compose 配置 (2) 执行 `compose up` | 行为与之前完全一致——每个 Agent 使用自身 `context_budget`，无预算池创建 | [ ] |

### 配额与进程预算交互

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 6 | 配额与 context_budget 取最小值 | (1) Agent 的 context_budget=20000，预算池分配配额=30000 (2) 观察实际生效的预算 | 实际生效预算为 min(30000, 20000) = 20000 | [ ] |
| 7 | 预算分配延迟 | (1) 在有多个 Agent 的 compose 中观察启动速度 | 配额分配决策延迟 <= 100ms（NFR43），不应有明显的启动延迟 | [ ] |

---

## Story 21.2: 合约 SLA 与评估

### SLA 定义与解析

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | Compose SLA 解析 | (1) 使用包含 `sla` 字段的 compose 配置 (2) 执行 `compose up` | 系统正确解析 `max_tokens`、`max_duration_ms`、`output_format` 约束 | [ ] |
| 2 | Agent.yaml SLA 解析 | (1) 在 agent.yaml 中定义 `sla` 字段 (2) 加载该 Agent | SLA 定义被正确解析，缺省字段使用默认值 | [ ] |
| 3 | 无 SLA 向后兼容 | (1) 使用无 SLA 定义的 compose 配置 (2) 执行 `compose up` | 行为完全不变——不进行 SLA 评估，无声誉记录 | [ ] |

### SLA 自动评估

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | SLA 全部通过 | (1) 设置宽松 SLA（max_tokens=100000, max_duration_ms=300000）(2) Agent 正常完成 | SLA 评估结果 Passed=true，所有检查项均通过 | [ ] |
| 5 | Token 超出 SLA | (1) 设置 max_tokens=10（极小值）(2) Agent 正常执行 | SLA 评估 max_tokens 检查失败，Passed=false，显示实际 token 消耗 | [ ] |
| 6 | 时间超出 SLA | (1) 设置 max_duration_ms=1（极小值）(2) Agent 执行 | SLA 评估 max_duration_ms 检查失败 | [ ] |
| 7 | 输出格式检查 JSON | (1) 设置 output_format="json" (2) Agent 输出非 JSON 内容 | output_format 检查失败，Actual 显示 "invalid_json" | [ ] |
| 8 | 输出格式检查 Markdown | (1) 设置 output_format="markdown" (2) Agent 输出包含 `#` 标题的内容 | output_format 检查通过 | [ ] |

### 声誉记录

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 9 | SLA 结果记录到声誉文件 | (1) Compose 执行完成（含 SLA） (2) 检查 `$PROJECT/.rnix/reputation/{agent_name}.json` | 文件存在，包含 JSON Lines 格式的 SLA 评估记录，含各项通过/失败状态和时间戳 | [ ] |
| 10 | IPC 查询 SLA 结果 | (1) 通过 IPC 发送 `sla_status` 请求 | 返回该 ProcGroup 的所有 Agent 的 SLA 评估结果列表 | [ ] |

---

## Story 21.3: 声誉系统与自动选择

### 声誉分数计算与查询

> **前提**：需有至少一个 Agent 的历史 SLA 评估记录（由 Story 21.2 的 compose 执行产生）。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 声誉列表查询 | `./rnix reputation` | 显示所有已知 Agent 的声誉摘要表格：Agent / Score / Success Rate / Avg Tokens / Avg Duration / Records / Trend | [ ] |
| 2 | 指定 Agent 声誉详情 | `./rnix reputation code-reviewer`（替换为实际 Agent 名） | 显示详细信息：Score、Success Rate（x/y）、Avg Token Usage、Avg Duration、Total Records、Trend，以及近 10 条 SLA 评估记录 | [ ] |
| 3 | JSON 输出 | `./rnix reputation --json` | 输出合法的 JSON 格式，包含 summaries 数组，每项含 agent_name、score、success_rate 等字段 | [ ] |
| 4 | 无声誉数据提示 | (1) 删除或清空 `$PROJECT/.rnix/reputation/` 目录 (2) `./rnix reputation` | 显示无数据提示信息，不报错不 panic | [ ] |

### 声誉分数计算验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | 全部通过 Agent 高分 | (1) 确保某 Agent 的所有 SLA 评估均为 Passed (2) `./rnix reputation <agent>` | Score 接近 1.0，SuccessRate 为 100% | [ ] |
| 6 | 部分通过 Agent 中等分 | (1) 确保某 Agent 有部分 SLA 失败记录 (2) `./rnix reputation <agent>` | Score 在 0~1 之间，SuccessRate 反映实际通过率 | [ ] |
| 7 | 趋势检测 | 多次执行后观察 RecentTrend 字段 | 近期成功率高于整体 +10% 显示 "improving"，低于 -10% 显示 "declining"，否则 "stable" | [ ] |

### IPC 声誉查询

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 8 | IPC 查询指定 Agent | 通过 IPC 发送 `reputation_status` 请求（agent_name="code-reviewer"） | 返回该 Agent 的 ReputationSummary | [ ] |
| 9 | IPC 查询全部 Agent | 通过 IPC 发送 `reputation_status` 请求（agent_name 为空） | 返回所有 Agent 的 ReputationSummary 列表，按 Score 降序 | [ ] |

### 自动选择机制

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 10 | Compose candidates 自动选择 | (1) compose.yaml 中某 Agent 指定 `candidates: [agent-a, agent-b, agent-c]` (2) 各候选有不同声誉分数 (3) 执行 `compose up` | 系统优先选择声誉分数最高的候选 Agent | [ ] |
| 11 | 无声誉数据时默认选择 | (1) compose.yaml 指定 candidates (2) 所有候选均无声誉数据 | 使用 candidates 列表中的第一个 Agent（确定性行为） | [ ] |
| 12 | 无 ReputationStore 时降级 | (1) ReputationStore 为 nil（如未初始化声誉系统）(2) compose.yaml 指定 candidates | 降级为使用 candidates[0]，不 panic | [ ] |

---

## Story 21.4: Skill Synergy 声明与自动检测

### Synergy 字段解析

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 解析含 synergy 的 SKILL.md | (1) 查看 `skills/testdata/with-synergy/SKILL.md` 中的 synergy 字段 (2) 系统加载该 Skill | Synergies 字段正确解析，包含 `with` 和 `instruction` | [ ] |
| 2 | 解析无 synergy 的 SKILL.md | (1) 加载现有的无 synergy 字段的 Skill（如 `code-analysis`） | 解析无错误，Synergies 为 nil，行为完全不变 | [ ] |

### Synergy 自动检测与 Prompt 注入

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 3 | 单向 Synergy 命中 | (1) Agent 同时加载 Skill A（声明 synergy with B）和 Skill B (2) 检查生成的 system prompt | prompt 末尾包含 `[Skill Synergy]` 段落，含 A→B 的涌现指令 | [ ] |
| 4 | 双向 Synergy 命中 | (1) Agent 同时加载 Skill A（with B）和 Skill B（with A）(2) 检查 system prompt | `[Skill Synergy]` 段落包含两条涌现指令 | [ ] |
| 5 | 部分加载不命中 | (1) Agent 只加载 Skill A（声明 synergy with B），不加载 B (2) 检查 system prompt | 无 `[Skill Synergy]` 段落，prompt 与原逻辑完全一致 | [ ] |
| 6 | 指令去重 | (1) 多个 Skill 声明相同的 synergy instruction (2) 检查 system prompt | 相同指令只出现一次 | [ ] |
| 7 | 检测性能 | 加载 100 个 Skill 各含 10 条 synergy | 检测耗时 < 100ms（NFR46） | [ ] |

---

## Story 21.5: Skill 组合矩阵

### 组合记录与查询

> **前提**：需有 Compose 执行历史（含多 Skill Agent 和 SLA 评估），以产生组合矩阵数据。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 基本组合列表 | `./rnix synergy list` | 展示 Skill 组合列表表格：SKILLS / SUCCESS / AVG TOKENS / EXECUTIONS / VS SOLO / TOKEN GAIN / STATUS | [ ] |
| 2 | JSON 输出 | `./rnix synergy list --json` | 输出合法 JSON 格式，含 combos 数组，每项含 combo_key、skills、success_rate、avg_tokens、total_executions 等字段 | [ ] |
| 3 | 空数据优雅处理 | (1) 清空组合矩阵数据 (2) `./rnix synergy list` | 输出 "No synergy combination data available."，不报错不 panic | [ ] |

### 统计计算

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | 成功率计算 | 多次执行包含特定 Skill 组合的 Agent 后查看 | 成功率 = SLA 通过次数 / 总执行次数 | [ ] |
| 5 | 组合 vs 单独对比 | 查看组合列表中的 VS SOLO 列 | 显示组合成功率与各 Skill 单独平均成功率的差值百分比 | [ ] |
| 6 | Token 效率提升 | 查看 TOKEN GAIN 列 | 显示组合 token 效率相比单 Skill 的提升百分比 | [ ] |
| 7 | 推荐组合标记 | 查看 STATUS 列 | 组合成功率比单 Skill 平均成功率高出 10% 以上时标记 "recommended" | [ ] |
| 8 | 排序规则 | 查看组合列表排序 | 推荐组合在前，然后按成功率降序排列 | [ ] |

### IPC 查询

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 9 | IPC synergy_list | 通过 IPC 发送 `synergy_list` 请求 | 返回包含 combos 数组的 SynergyListResponse | [ ] |

---

## 端到端完整流程验证

> **前提**：Daemon 运行中，有可用的 Agent 模板和 Skill。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 预算池到 SLA 全链路 | (1) 使用含 token_budget + priority + sla 的 compose 配置 (2) `./rnix compose up -f config.yaml` (3) 观察执行过程和结果 | 预算池正确创建、配额按优先级分配、SLA 评估在完成后自动执行、结果记录到声誉文件 | [ ] |
| 2 | SLA 到声誉全链路 | (1) 执行多次 Compose（产生声誉数据）(2) `./rnix reputation` (3) `./rnix reputation <agent>` | 声誉分数正确计算，反映 SLA 历史通过率 | [ ] |
| 3 | 声誉到自动选择 | (1) 有不同声誉分数的多个候选 Agent (2) compose 配置使用 candidates 字段 (3) `compose up` | 声誉最高的 Agent 被自动选择 | [ ] |
| 4 | Synergy 检测全链路 | (1) Agent 同时加载含互相 synergy 声明的 Skill (2) 观察 system prompt | system prompt 末尾含 `[Skill Synergy]` 段落和正确的涌现指令 | [ ] |
| 5 | 组合矩阵全链路 | (1) 多次执行含多 Skill Agent 的 Compose (2) `./rnix synergy list` | 组合矩阵正确记录历史数据，统计计算正确 | [ ] |
| 6 | 向后兼容完整验证 | (1) 使用不含任何新字段（无 token_budget、无 sla、无 priority、无 candidates、无 synergy）的配置 (2) 执行 compose 和相关命令 | 所有功能行为与 Epic 21 之前完全一致 | [ ] |

---

## 测试清理

```bash
# 停止 daemon
./rnix daemon stop 2>/dev/null; true

# 清理测试声誉数据（可选）
# rm -rf $PROJECT/.rnix/reputation/

# 清理测试 compose 配置
rm -f rnix-compose-test.yaml
```

---

## 关键注意事项

1. **Token 预算池在 kernel 层** -- BudgetPool 在 kernel 包内，Compose 引擎创建并注册到 Kernel。预算池消耗在 reasonStep 中实时更新。
2. **SLA 评估是后置验证** -- SLA 在 Agent 完成后执行，不会中断正在运行的 Agent。评估失败不导致进程终止。
3. **声誉数据 JSON Lines 格式** -- `$PROJECT/.rnix/reputation/{agent_name}.json`，每行一条 JSON 记录，追加写入。
4. **Score 计算公式** -- Score = 0.7 * SuccessRate + 0.3 * TokenEfficiency。默认中性分 0.5。
5. **RecentTrend** -- 比较最近 5 条记录的 Passed 率与整体 Passed 率，阈值 0.1。
6. **自动选择可选** -- 仅在 `candidates`（Compose）或 `alternatives`（agent.yaml）非空时激活。
7. **Synergy 检测是纯函数** -- O(N*M) 复杂度，N=Skill 数, M=平均 synergy 声明数。结果按字母序排列保证确定性。
8. **Synergy 指令追加格式** -- `\n\n[Skill Synergy]\n\n` + 各条指令换行拼接，追加在 skill bodies 之后。
9. **组合矩阵存储** -- `$PROJECT/.rnix/reputation/synergy-matrix.json`，JSON Lines 格式。
10. **ComboKey 确定性** -- Skill 名称排序后逗号拼接，{A,B} 和 {B,A} 生成相同 key。
11. **推荐阈值 10%** -- 组合成功率 > 各 Skill 单独平均成功率 + 10% 时标记推荐。
12. **NFR43** -- 预算分配决策延迟 <= 100ms。
13. **NFR46** -- Synergy 组合检测开销 <= 100ms。
14. **优先级权重** -- PriorityLow=1, PriorityNormal=5, PriorityHigh=10。
15. **IPC 标准 4 步** -- 所有新增 IPC 方法遵循 protocol.go → server.go → client.go → cmd/ 模式。

## 验证记录

| 字段 | 值 |
|------|-----|
| 验证人 | |
| 验证日期 | |
| 构建版本 | |
| 总用例数 | 46 |
| 通过数 | |
| 失败数 | |
| 备注 | |
