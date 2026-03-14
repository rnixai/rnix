# Epic 22 手工验证指南：适应性安全与自愈

## 概述

本文档提供 Epic 22 所有 Story 的手工验证步骤，用于在自动化测试之外对功能进行端到端的人工确认。Epic 22 为 Rnix 引入 Immune Daemon 安全监控系统，包含行为基线建立、异常检测与威胁记忆、安全状态管理、能力迁移与相似度矩阵、以及协作拓扑与强化路径功能。

## 前置准备

### 构建

```bash
make build
```

### 启动 Daemon

```bash
# 确保旧 daemon 已停止
./rnix daemon stop 2>/dev/null; true

# 启动 daemon（Immune Daemon 随 daemon 自动启动）
./rnix daemon start
```

### 验证 Daemon 状态

```bash
# 确认 daemon 运行正常
./rnix daemon status

# 确认 immune daemon 已启动
./rnix immune status
```

### 准备测试 Agent

```bash
# 确认 lib/agents/ 下有可用的 Agent 模板
ls lib/agents/

# 记录可用 Agent 名称，后续测试中替换使用
```

### 持久化目录

```bash
# Immune 数据存储目录
# $PROJECT/.rnix/immune/          -- 行为样本
# $PROJECT/.rnix/immune/profiles/ -- NormalProfile
# $PROJECT/.rnix/immune/threats.jsonl -- 威胁记忆库
```

### 验证所需工具

```bash
# 确认 rnix 二进制可用
./rnix --version

# 确认 jq 可用（可选，用于 JSON 格式化）
jq --version
```

---

## Story 22.1: Immune Daemon 与行为基线

### Immune Daemon 启动与生命周期

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | Daemon 启动含 Immune | (1) `./rnix daemon start` (2) `./rnix immune status` | 显示 "Immune Daemon: running"，ProfileCount 和 ActivePIDs 正确 | [ ] |
| 2 | Immune 状态表格输出 | `./rnix immune status` | 显示表格：AGENT TEMPLATE / SAMPLES / TOKEN RATE (avg) / DURATION (avg) / LAST UPDATED | [ ] |
| 3 | Immune 状态 JSON 输出 | `./rnix immune status --json` | 返回合法 JSON，含 running、profile_count、profiles、active_pids 字段 | [ ] |
| 4 | 无 Profile 时提示 | (1) 首次启动或清空 immune 数据 (2) `./rnix immune status` | 显示 "No behavior profiles established." | [ ] |

### 行为数据采集

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | Spawn 触发行为采集 | (1) `./rnix spawn <agent-name> "test task"` (2) 等待进程完成 | Immune Daemon 创建 BehaviorCollector 并在进程退出后生成 BehaviorSample | [ ] |
| 6 | 行为样本持久化 | (1) 进程完成后 (2) 检查 `$PROJECT/.rnix/immune/<agent-template>.jsonl` | 文件存在，包含 JSON Lines 格式的行为样本，含 agent_template、syscall_counts、tokens_used、token_rate、duration_ms、timestamp | [ ] |
| 7 | 多进程并发采集 | (1) 同时 Spawn 多个进程 (2) 等待全部完成 (3) 检查各 Agent 的样本文件 | 每个进程的行为样本独立记录，无数据混淆 | [ ] |

### Normal Profile 建立

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 8 | Profile 不足样本 | (1) 执行同一 Agent 模板 4 次（不足 MinSamplesForProfile=5）(2) `./rnix immune status` | 该 Agent 模板无 Profile（不显示或显示样本不足） | [ ] |
| 9 | Profile 建立 | (1) 执行同一 Agent 模板至少 5 次 (2) `./rnix immune status` | 该 Agent 模板出现在 Profile 表格中，显示 SAMPLES >= 5、TOKEN RATE (avg)、DURATION (avg) | [ ] |
| 10 | Profile 持久化 | (1) 确认 Profile 已建立 (2) 检查 `$PROJECT/.rnix/immune/profiles/<agent>-profile.json` | Profile JSON 文件存在，含 agent_template、sample_count、syscall_mean、syscall_std_dev、token_rate_mean 等字段 | [ ] |
| 11 | Profile 重启加载 | (1) 确认 Profile 已持久化 (2) `./rnix daemon stop` (3) `./rnix daemon start` (4) `./rnix immune status` | 重启后 Profile 自动加载，显示与重启前相同的 Profile 数据 | [ ] |

### 性能与空闲开销

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 12 | 空闲无开销 | (1) 无运行中进程 (2) 观察 daemon 进程 CPU 占用 | CPU 接近 0%（无主动轮询、无 busy-wait）—— Immune Daemon 采用事件驱动设计 | [ ] |
| 13 | 10 并发进程 CPU 开销 | (1) 同时 Spawn 10 个进程 (2) 观察 daemon CPU 占用 | CPU 开销 <= 3%（NFR47）。Immune Daemon 采集不应显著增加 CPU 负载 | [ ] |

---

## Story 22.2: 异常检测与威胁记忆

### 异常检测与自动挂起

> **前提**：需要有已建立的 NormalProfile（至少 5 次执行的历史数据）。异常检测阈值为 3 倍标准差（DefaultDeviationThreshold=3.0）。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | Syscall 频率异常挂起 | (1) 有 NormalProfile 的 Agent 模板 (2) 触发异常行为（syscall 频率远超基线）(3) 观察进程状态 | 进程被自动挂起（SIGPAUSE），`./rnix immune status` 显示告警信息 | [ ] |
| 2 | 告警信息展示 | (1) 进程被异常检测挂起后 (2) `./rnix immune status` | 显示 ALERTS 段落：PID、Agent 模板、异常类型（如 syscall_freq）、偏离程度描述、触发时间 | [ ] |
| 3 | 告警操作提示 | 查看 ALERTS 中的告警项 | 每项显示可用操作：`rnix immune resume <pid>` 和 `rnix kill <pid>` | [ ] |

### 进程恢复与终止

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | 恢复挂起进程 | (1) 进程被 Immune Daemon 挂起 (2) `./rnix immune resume <pid>` | 输出 "Process <pid> resumed successfully."，进程恢复运行（SIGRESUME） | [ ] |
| 5 | 恢复后告警清除 | (1) 恢复进程后 (2) `./rnix immune status` | 该 PID 的告警从 ALERTS 列表中移除 | [ ] |
| 6 | 终止挂起进程 | (1) 进程被挂起 (2) `./rnix kill <pid>` | 进程被终止，告警同步清除 | [ ] |
| 7 | Resume 无 Daemon 错误处理 | (1) daemon 未运行 (2) `./rnix immune resume 42` | 输出连接错误信息，不 panic | [ ] |

### 威胁记忆库

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 8 | 威胁签名自动记录 | (1) 异常检测触发后 (2) 检查 `$PROJECT/.rnix/immune/threats.jsonl` | 文件存在，包含 JSON Lines 格式的威胁签名：id、type、agent_template、metric、threshold、created_at | [ ] |
| 9 | 已知威胁立即拦截 | (1) 威胁记忆库有记录 (2) 相同 Agent 模板再次出现相同异常模式 | 立即拦截（无需重新检测），匹配已知签名的速度更快 | [ ] |
| 10 | 威胁记忆持久化 | (1) 威胁签名已记录 (2) `./rnix daemon stop` (3) `./rnix daemon start` | 重启后威胁记忆自动加载，`./rnix immune status` 显示正确的 Threat Memory 数量 | [ ] |
| 11 | 威胁记忆数量显示 | `./rnix immune status` | 输出包含 "Threat Memory: N signatures"（N 为已知签名数） | [ ] |

### 向后兼容

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 12 | 无 Profile 时不检测 | (1) 新 Agent 模板无历史数据 (2) Spawn 该 Agent | 正常执行，不触发异常检测（无基线无法检测） | [ ] |
| 13 | suspendFn nil 降级 | ImmuneDaemon 的 suspendFn 未设置时 | 检测到异常仅记录告警，不执行挂起（降级模式），不 panic | [ ] |

---

## Story 22.3: 安全状态管理

### 完整安全状态展示

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | Daemon 状态和运行时间 | `./rnix immune status` | 输出包含 "Immune Daemon: running (uptime: Xh Xm)" 或类似格式 | [ ] |
| 2 | 安全态势摘要 OK | (1) 无告警无挂起进程 (2) `./rnix immune status` | 输出包含 "Security: OK" | [ ] |
| 3 | 安全态势摘要 Warning | (1) 有活跃告警或挂起进程 (2) `./rnix immune status` | 输出包含 "Security: N alerts, M suspended"（具体数量） | [ ] |
| 4 | 告警列表展示 | (1) 有活跃告警 (2) `./rnix immune status` | ALERTS 段落显示每项告警：PID、Agent 模板、异常类型、偏离程度、触发时间 | [ ] |
| 5 | 挂起进程列表 | (1) 有挂起进程 (2) `./rnix immune status` | SUSPENDED PROCESSES 段落显示每项：PID、异常原因、可用操作（resume / kill） | [ ] |
| 6 | 威胁记忆统计 | `./rnix immune status` | 输出包含 "Threat Memory: N signatures" | [ ] |
| 7 | Profile 表格 | `./rnix immune status`（有已建立的 Profile） | 显示 AGENT TEMPLATE / SAMPLES / TOKEN RATE (avg) / DURATION (avg) / LAST UPDATED 表格 | [ ] |

### JSON 输出

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 8 | JSON 完整字段 | `./rnix immune status --json` | JSON 包含所有字段：running、uptime_ms、profile_count、profiles、active_pids、suspended_pids、alerts、threat_count、security_status | [ ] |
| 9 | security_status 值 | 检查 JSON 中 security_status | 无告警 = "ok"，有告警或挂起 = "warning" | [ ] |

### Uptime 格式化

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 10 | 短时 uptime | daemon 启动不到 1 分钟后 `./rnix immune status` | 显示格式 "Xs"（如 "42s"） | [ ] |
| 11 | 中等 uptime | daemon 运行数分钟后 | 显示格式 "XmXs"（如 "5m30s"） | [ ] |
| 12 | 长时 uptime | daemon 运行超过 1 小时后 | 显示格式 "XhXm"（如 "2h15m"） | [ ] |

---

## Story 22.4: 能力迁移与相似度矩阵

### 能力相似度矩阵

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 相似度查询 CLI | `./rnix immune similarity <agent-name>` | 显示该 Agent 的能力相似度排行榜：AGENT / SIMILARITY / SKILL OVERLAP | [ ] |
| 2 | 相似度 JSON 输出 | `./rnix immune similarity <agent-name> --json` | JSON 包含 agent 和 similarities 数组，每项含 agent_a、agent_b、skill_score、coop_score、score | [ ] |
| 3 | Skill 重叠计算 | (1) 有两个共享部分 Skill 的 Agent (2) `./rnix immune similarity <agent>` | 相似度分数反映 Jaccard 系数：|A ∩ B| / |A ∪ B| | [ ] |
| 4 | 完全不同 Skill | 两个 Agent 无任何共同 Skill | SkillScore = 0.0 | [ ] |
| 5 | 完全相同 Skill | 两个 Agent Skill 集合完全相同 | SkillScore = 1.0 | [ ] |
| 6 | 协作历史加权 | (1) 有协作历史的 Agent 对 (2) 查看综合分数 | Score = 0.7 * SkillScore + 0.3 * CoopScore | [ ] |

### IPC 相似度查询

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 7 | IPC similarity_query | 通过 IPC 发送 `similarity_query` 请求 | 返回 SimilarityQueryResponse，含指定 Agent 的相似 Agent 列表（按 Score 降序） | [ ] |

### 能力迁移

> **前提**：需要 Supervisor 管理的进程环境。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 8 | 重启超限触发迁移 | (1) Supervisor 管理的子进程重启失败超过 MaxRestarts (2) 存在相似度 >= 0.3 的替代 Agent | 系统尝试将任务迁移到替代 Agent，迁移成功则不执行 shutdownAll | [ ] |
| 9 | 迁移失败回退 | (1) Supervisor 重启超限 (2) 无满足阈值的替代 Agent（或迁移执行失败）| 迁移放弃，继续执行原有的 shutdownAll + finishProcess 流程 | [ ] |
| 10 | 迁移性能 | 触发能力迁移时计时 | 总迁移时间（选择替代 + Spawn 新进程 + 上下文注入）<= 10s（NFR48） | [ ] |
| 11 | 最小相似度阈值 | 所有候选 Agent 相似度 < 0.3 时 | 迁移放弃，返回 Success=false | [ ] |
| 12 | 声誉加权选择 | 多个候选相似度相近但声誉分数不同 | 选择 similarity * 0.6 + reputation * 0.4 综合得分最高的 Agent | [ ] |

---

## Story 22.5: 协作拓扑与强化路径

### 协作拓扑查询

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 拓扑文本输出 | `./rnix topology` | 显示协作拓扑图：NODES 段落（Agent / Reputation / Connections）+ EDGES 段落（From / To / Spawn / Msg / Total / Reinforced）+ REINFORCED PATHS 段落 | [ ] |
| 2 | 拓扑 JSON 输出 | `./rnix topology --json` | JSON 包含 nodes、edges、reinforced_paths 三个数组 | [ ] |
| 3 | 空数据提示 | (1) 无协作历史数据 (2) `./rnix topology` | 显示无数据提示信息，不报错不 panic | [ ] |
| 4 | Daemon 不可用错误 | (1) daemon 未运行 (2) `./rnix topology` | 输出连接错误信息，不 panic | [ ] |

### 节点信息

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | 节点包含声誉分数 | `./rnix topology`（有声誉数据的 Agent） | NODES 段落中每个 Agent 显示声誉分数（从 ReputationStore 查询） | [ ] |
| 6 | 节点连接数 | 查看 NODES 段落的 CONNECTIONS 列 | 显示每个 Agent 参与的协作边数量 | [ ] |

### 边和协作类型

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 7 | Spawn 协作记录 | (1) Agent A spawn Agent B (2) `./rnix topology` | EDGES 中 A→B 的 SPAWN 列计数增加 | [ ] |
| 8 | 消息协作记录 | (1) Agent A 通过 IPC 向 Agent B 发送消息 (2) `./rnix topology` | EDGES 中 A→B 的 MSG 列计数增加 | [ ] |
| 9 | Total 计算 | 查看 EDGES 段落 | TOTAL = SPAWN + MSG | [ ] |

### 强化路径

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 10 | 强化路径标记 | (1) 两个 Agent 之间协作 >= 5 次（DefaultReinforcementThreshold=5）(2) `./rnix topology` | EDGES 中该行 REINFORCED 列显示 `*`，且出现在 REINFORCED PATHS 段落 | [ ] |
| 11 | 强化路径未达阈值 | 协作次数 < 5 的 Agent 对 | REINFORCED 列无标记，不出现在 REINFORCED PATHS 段落 | [ ] |
| 12 | 强化路径排序 | REINFORCED PATHS 段落 | 按 Total 降序排列（协作频率高的在前） | [ ] |

### IPC 查询

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 13 | IPC topology_query | 通过 IPC 发送 `topology_query` 请求 | 返回 TopologyQueryResponse，含 nodes、edges、reinforced_paths 数据 | [ ] |

---

## 端到端完整流程验证

> **前提**：Daemon 运行中，有可用的 Agent 模板。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 行为基线到异常检测 | (1) 执行同一 Agent 5+ 次建立基线 (2) 触发异常行为 (3) `./rnix immune status` | NormalProfile 正确建立，异常检测触发告警，进程被挂起，告警信息显示异常类型和偏离程度 | [ ] |
| 2 | 异常检测到威胁记忆 | (1) 异常检测触发 (2) 检查 threats.jsonl (3) 相同模式再次出现 | 威胁签名被记录，后续相同模式立即拦截 | [ ] |
| 3 | 挂起到恢复/终止 | (1) 进程被挂起 (2) `./rnix immune resume <pid>` 或 `./rnix kill <pid>` (3) `./rnix immune status` | 恢复或终止成功，告警从列表中清除，Security 状态正确更新 | [ ] |
| 4 | 安全状态全视图 | `./rnix immune status` | 一次命令显示：daemon 状态、uptime、安全态势、Profile 表格、告警列表、挂起进程、威胁记忆统计 | [ ] |
| 5 | 相似度到能力迁移 | (1) 有多个共享 Skill 的 Agent (2) `./rnix immune similarity <agent>` (3) Supervisor 重启失败触发迁移 | 相似度矩阵正确计算，迁移选择最佳替代 Agent | [ ] |
| 6 | 协作拓扑全链路 | (1) 执行多次 Agent 间协作 (2) `./rnix topology` | 拓扑图正确显示节点、边、强化路径，声誉分数集成正确 | [ ] |
| 7 | 数据持久化全链路 | (1) 积累行为样本、Profile、威胁签名 (2) `./rnix daemon stop` (3) `./rnix daemon start` (4) `./rnix immune status` | 重启后所有持久化数据（行为样本、Profile、威胁记忆）正确加载 | [ ] |
| 8 | 向后兼容 | (1) 不使用任何 Immune 功能 (2) 正常 Spawn、Compose 操作 | ImmuneDaemon 存在但不干扰现有功能，所有操作行为不变 | [ ] |

---

## 测试清理

```bash
# 停止 daemon
./rnix daemon stop 2>/dev/null; true

# 清理测试 Immune 数据（可选）
# rm -rf $PROJECT/.rnix/immune/

# 清理测试威胁记忆（可选）
# rm -f $PROJECT/.rnix/immune/threats.jsonl
```

---

## 关键注意事项

1. **Immune Daemon 事件驱动** -- 完全被动监听，无主动轮询。通过 OnProcessStart/OnSyscallEvent/OnProcessExit 三个钩子接收事件，空闲时 CPU 开销为 0。
2. **NormalProfile 最少 5 条样本** -- `MinSamplesForProfile = 5`，不足 5 条时不建立基线，不进行异常检测。
3. **异常检测阈值 3 倍标准差** -- `DefaultDeviationThreshold = 3.0`。实际值 > 均值 + 3*标准差 时视为异常（99.7% 置信区间外）。
4. **挂起机制复用 SIGPAUSE/SIGRESUME** -- 异常检测通过 `SIGPAUSE` 挂起进程，`rnix immune resume` 通过 `SIGRESUME` 恢复。
5. **威胁签名三元组标识** -- `(agent_template, anomaly_type, metric)`，匹配已知签名立即拦截。
6. **行为样本 JSON Lines 格式** -- `$PROJECT/.rnix/immune/{agent-template}.jsonl`，追加写入。
7. **Profile 完整 JSON 格式** -- `$PROJECT/.rnix/immune/profiles/{agent}-profile.json`，整体覆写。
8. **威胁记忆 JSON Lines 格式** -- `$PROJECT/.rnix/immune/threats.jsonl`，追加写入。
9. **SecurityStatus 计算** -- 无告警无挂起 = "ok"，有告警或有挂起 = "warning"。
10. **Uptime 纯内存值** -- daemon 重启后重新计算，不从磁盘恢复。
11. **相似度 Jaccard 系数** -- `|A ∩ B| / |A ∪ B|`，空集合返回 0.0。综合分 = 0.7 * SkillScore + 0.3 * CoopScore。
12. **最小迁移相似度 0.3** -- `MinMigrationSimilarity = 0.3`，低于此阈值的候选被跳过。
13. **迁移选择加权** -- similarity * 0.6 + reputation * 0.4 选择最佳替代 Agent。
14. **NFR47** -- Immune Daemon CPU 开销 <= 3%（10 并发进程场景）。
15. **NFR48** -- 能力迁移总时间 <= 10s。
16. **强化路径阈值** -- `DefaultReinforcementThreshold = 5`，协作次数 >= 5 标记为强化路径。
17. **协作类型** -- 区分 Spawn 父子关系和 IPC 消息发送两种类型。
18. **`rnix topology` 是顶级命令** -- 不是 `rnix immune topology`，而是 `rnix topology`。
19. **nil 保护** -- ImmuneDaemon 为 nil 时所有方法返回安全默认值，不 panic。
20. **suspendFn 注入** -- 通过 `SetSuspendFunc` 注入 `kernel.Kill(pid, SIGPAUSE)` 闭包，避免循环依赖。

## 验证记录

| 字段 | 值 |
|------|-----|
| 验证人 | |
| 验证日期 | |
| 构建版本 | |
| 总用例数 | 63 |
| 通过数 | |
| 失败数 | |
| 备注 | |
