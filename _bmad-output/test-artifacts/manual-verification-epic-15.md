# Epic 15 手工验证指南：分布式追踪与上下文分析（Distributed Tracing & Context Analysis）

## 概述

本文档提供 Epic 15 所有 Story 的手工验证步骤，用于在自动化测试之外对功能进行端到端的人工确认。

## 前置准备

Daemon 由 rnix 自动按需启动（`EnsureDaemon`），无需手动管理。

```bash
# 1. 构建最新版本
make build

# 2. 准备一个 compose 编排文件（用于触发 Trace ID 生成）
#    例如 compose.yaml，包含至少两个 agent 节点
cat > /tmp/test-compose.yaml << 'EOF'
version: "1.0"
agents:
  analyst:
    intent: "分析当前目录结构"
  reviewer:
    intent: "审查分析结果并给出建议"
    depends_on: [analyst]
EOF

# 3. 在终端 A：通过 compose 启动多智能体编排
./rnix compose up /tmp/test-compose.yaml

# 4. 在终端 B：确认进程在运行，记下 PID
./rnix ps
```

> **提示**：trace/blame 操作是纯本地文件操作，不需要 daemon 运行。ctx-profile 和 ctx-growth 需要 daemon。

---

## Story 15.1: Trace ID 生成与 Span 记录

### Compose 编排 TraceID 自动生成

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | Compose 生成 TraceID | 执行 `rnix compose up compose.yaml`，然后 `ls .rnix/traces/` | 存在一个 32 字符 hex 命名的目录 | [ ] |
| 2 | 所有进程共享 TraceID | `rnix strace <pid>` 查看各进程事件 | 所有进程的事件中 TraceID 相同 | [ ] |
| 3 | 各进程有独立 SpanID | 查看 `.rnix/traces/<trace-id>/spans.jsonl` | 每行 JSON 的 `span_id` 各不相同 | [ ] |
| 4 | 父子 Span 关系 | 检查 `spans.jsonl` 的 `parent_span_id` 字段 | 子进程的 `parent_span_id` 指向父进程的 `span_id`，根节点 `parent_span_id` 为空 | [ ] |

### 非 Compose 场景向后兼容

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | 单进程无 TraceID | `./rnix -i "你好"` | 正常执行，`.rnix/traces/` 目录下无新增 | [ ] |
| 6 | strace 无 TraceID | 对非 Compose 进程 `rnix strace <pid>` | 事件中 `trace_id` 和 `span_id` 为空 | [ ] |

### Span 数据记录

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 7 | Span 文件存在 | Compose 编排完成后检查 `.rnix/traces/<trace-id>/` | 目录存在，包含 `spans.jsonl` | [ ] |
| 8 | Span 格式正确 | 查看 `spans.jsonl` | 每行一个 JSON 对象，包含 `trace_id`、`span_id`、`parent_span_id`、`pid`、`name`、`start_time_ms`、`end_time_ms`、`duration_ms`、`syscall_count`、`tokens_used`、`status` | [ ] |
| 9 | Syscall 计数 | 检查 Span 中 `syscall_count` | 与 strace 观察到的 syscall 数量一致 | [ ] |
| 10 | Token 消耗 | 检查 Span 中 `tokens_used` | 非零，与 `rnix ps` 显示的 token 使用量一致 | [ ] |
| 11 | Span 状态 | 检查 Span 中 `status` 字段 | 正常完成为 `ok`，错误为 `error`，超时为 `timeout` | [ ] |

### IPC TraceID 传播

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 12 | Send 携带 TraceID | 在 Compose 编排中 agent 间通过 Send/Recv 通信 | strace 中可见 TraceID 和 SpanID 字段 | [ ] |
| 13 | Recv 继承 TraceID | 检查接收方进程的 Span | 与发送方共享同一 TraceID | [ ] |

---

## Story 15.2: 分布式追踪视图

> 前提：已有至少一个已完成的 Compose 编排（`.rnix/traces/` 下有数据）。

### Trace 列表

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 列出所有 Trace | `rnix trace` | 表格列出所有 Trace：TRACE ID、SPANS、DURATION、ROOT | [ ] |
| 2 | 无 Trace 时 | 删除 `.rnix/traces/` 目录后 `rnix trace` | 显示无 Trace 的提示 | [ ] |
| 3 | 按时间倒序 | 执行两次 Compose 编排后 `rnix trace` | 最近的 Trace 排在前面 | [ ] |

### Trace 树状视图

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | 查看追踪视图 | `rnix trace <trace-id>` | 显示 Trace 摘要（总 Span 数、总耗时、总 Token），然后显示树状视图，包含各 Span 的 Name、PID、Duration、Tokens、Status | [ ] |
| 5 | 树形结构正确 | 查看 `├─`、`└─`、`│` 连接线 | 父子层级关系清晰，子节点缩进正确 | [ ] |
| 6 | 错误状态高亮 | 如有错误 Span | error 状态的 Span 有特殊标记/颜色 | [ ] |
| 7 | verbose 模式 | `rnix trace <trace-id> --verbose` | 额外显示 SpanID、ParentSpanID、SyscallCount、StartTime | [ ] |
| 8 | 不存在的 Trace | `rnix trace nonexistent-id` | 显示友好的错误信息 | [ ] |
| 9 | JSON 输出 | `rnix trace <trace-id> --json` | 输出 JSON 格式的 SpanTree 数据 | [ ] |

### 本地操作验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 10 | 无 daemon 查看 | 停止 daemon 后 `rnix trace <trace-id>` | 正常加载和展示（纯本地文件操作，不需要 daemon） | [ ] |

---

## Story 15.3: Trace Blame 根因定位

> 前提：已有至少一个已完成的 Compose 编排。

### Blame 分析

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 基本 blame 分析 | `rnix trace blame <trace-id>` | 显示四个段落：Blame 摘要、Critical Path、Duration Hotspots、Token Hotspots | [ ] |
| 2 | 关键路径 | 查看 Critical Path 段落 | 使用 `→` 箭头连接从根到叶的最长耗时路径，显示百分比 | [ ] |
| 3 | Duration Hotspots | 查看 Duration Hotspots 段落 | 以 `#1`、`#2`、`#3` 排名格式展示耗时 Top-3 节点，附百分比 | [ ] |
| 4 | Token Hotspots | 查看 Token Hotspots 段落 | 以 `#1`、`#2`、`#3` 排名格式展示 Token 消耗 Top-3 节点 | [ ] |
| 5 | 全 OK 无 Error Chains | 所有 Span 状态为 OK 时 blame | 仍显示 Critical Path 和 Hotspots，但无 Error Chains 段落 | [ ] |
| 6 | 错误链路 | 如有 Span 状态为 ERROR/TIMEOUT | 显示 Error Chains 段落，使用 `✗` 标记错误节点、`↑` 标记传播方向、`[ROOT CAUSE]` 高亮根因 | [ ] |
| 7 | 不存在的 Trace | `rnix trace blame nonexistent-id` | 显示友好的错误信息 | [ ] |
| 8 | JSON 输出 | `rnix trace blame <trace-id> --json` | 输出 JSON 格式的 BlameResult 数据 | [ ] |
| 9 | 无 daemon 分析 | 停止 daemon 后 `rnix trace blame <trace-id>` | 正常分析（纯本地操作） | [ ] |

---

## Story 15.4: 上下文使用分析

> 前提：有一个 Running 或 Zombie 状态的智能体进程，daemon 正在运行。

### CLI 命令方式

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 基本上下文分析 | `rnix ctx-profile <pid>` | 显示三个段落：Classification、Top Consumers、Suggestions | [ ] |
| 2 | 分类展示 | 查看 Classification 段落 | 展示 Active（活跃）、Warm（温）、Cold（冷）、Leaked（泄漏）四类，每类包含 Tokens、百分比、Messages 数量 | [ ] |
| 3 | Top Consumers | 查看 Top Consumers 段落 | 以 `#1`～`#5` 排名格式展示 Token 消耗最大的消费者（system_prompt、user、assistant、tool:xxx） | [ ] |
| 4 | 优化建议 | 查看 Suggestions 段落 | 根据分析结果给出具体优化建议（如 system prompt 过大、工具输出过多等） | [ ] |
| 5 | 无建议时 | 上下文使用健康时 | 省略 Suggestions 段落 | [ ] |

### 错误处理

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 6 | 无效 PID | `rnix ctx-profile abc` | 显示 PID 解析错误 | [ ] |
| 7 | 不存在的 PID | `rnix ctx-profile 99999` | 显示进程不存在的错误 | [ ] |
| 8 | 非 Running/Zombie 进程 | 对已退出的进程执行 ctx-profile | 显示进程状态不适用的错误 | [ ] |
| 9 | Daemon 未运行 | 停止 daemon 后 `rnix ctx-profile 1` | 显示 daemon 不可用的友好错误 | [ ] |
| 10 | JSON 输出 | `rnix ctx-profile <pid> --json` | 输出 JSON 格式的 CtxProfileResult 数据 | [ ] |

### 性能验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 11 | 分析延迟 NFR34 | 对一个上下文较大的进程执行 `rnix ctx-profile <pid>` | 分析结果延迟 <= 1s | [ ] |

---

## Story 15.5: 上下文增长预测与告警

> 前提：有一个 Running 状态的智能体进程且设置了 token 预算。

### CLI 命令方式

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 基本增长预测 | `rnix ctx-growth <pid>` | 显示三个段落：Growth Trend、Prediction、Budget | [ ] |
| 2 | 增长趋势 | 查看 Growth Trend 段落 | 逐步展示历史 Token 消耗（Step N: XXX tok (+YYY)） | [ ] |
| 3 | 预测数据 | 查看 Prediction 段落 | 显示 Avg Rate、Recent Rate、Remaining、Est. Steps、Alert 状态 | [ ] |
| 4 | Budget 进度条 | 查看 Budget 段落 | 显示 ASCII 进度条和百分比 | [ ] |
| 5 | 无 budget | 对无 budget 的进程执行 ctx-growth | 显示 `No budget set`，省略 Prediction 和 Budget 段落 | [ ] |
| 6 | 无 history | 对刚启动的进程执行 ctx-growth | 只显示当前用量，省略 Growth Trend | [ ] |

### 告警级别

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 7 | AlertNone | 剩余预算 >= 20% 时 | Alert 显示 `none ✓` | [ ] |
| 8 | AlertWarning | 剩余预算 10%～20% 时 | Alert 显示 `⚠ WARNING` | [ ] |
| 9 | AlertCritical | 剩余预算 < 10% 时 | Alert 显示 `⚠ CRITICAL` | [ ] |

### 被动告警验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 10 | Log 告警可见 | 当 budget 剩余 < 20% 时 `rnix log <pid>` | 可看到 `warning` 类别的预算告警日志 | [ ] |
| 11 | Strace 告警可见 | 当 budget 剩余 < 20% 时 `rnix strace <pid>` | 可看到 `budget_warning` 类型的事件 | [ ] |

### 错误处理

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 12 | 无效 PID | `rnix ctx-growth abc` | 显示 PID 解析错误 | [ ] |
| 13 | 不存在的 PID | `rnix ctx-growth 99999` | 显示进程不存在的错误 | [ ] |
| 14 | 非 Running 进程 | 对已退出的进程执行 ctx-growth | 显示进程非 Running 状态的错误 | [ ] |
| 15 | Daemon 未运行 | 停止 daemon 后 `rnix ctx-growth 1` | 显示 daemon 不可用的友好错误 | [ ] |
| 16 | JSON 输出 | `rnix ctx-growth <pid> --json` | 输出 JSON 格式的 GrowthPrediction 数据 | [ ] |

---

## 端到端完整流程验证

> 此节验证从追踪到分析的完整工作流。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 完整追踪流程 | ① 准备 compose.yaml ② `rnix compose up compose.yaml` ③ 等待编排完成 ④ `rnix trace` 查看列表 ⑤ `rnix trace <trace-id>` 查看树状视图 | 每步输出正确，Span 树与编排结构对应 | [ ] |
| 2 | Blame 分析 | 在已完成的 Trace 上 `rnix trace blame <trace-id>` | 能看到关键路径、耗时/Token Hotspot 和错误链（如有） | [ ] |
| 3 | 上下文分析完整流程 | ① `./rnix -i "详细分析当前项目并给出改进建议"` ② 在另一终端 `rnix ps` 获取 PID ③ `rnix ctx-profile <pid>` ④ `rnix ctx-growth <pid>` | ctx-profile 展示分类和消费者，ctx-growth 展示趋势和预测 | [ ] |
| 4 | Compose + Trace + Blame 联合 | ① Compose 启动含错误 agent 的编排 ② 编排完成后 `rnix trace <trace-id>` ③ `rnix trace blame <trace-id>` | Trace 视图中可见错误 Span，Blame 分析出 Error Chain 并标记 ROOT CAUSE | [ ] |

---

## 关键注意事项

1. **Trace 纯本地操作** — `rnix trace` 和 `rnix trace blame` 是纯本地文件操作，不需要 daemon 运行
2. **ctx-profile/ctx-growth 需要 daemon** — 这两个命令需要通过 IPC 连接 daemon 查询进程上下文数据
3. **TraceID 仅 Compose 生成** — 只有通过 `compose up` 启动的编排才会自动生成 TraceID，单进程启动不会产生 TraceID
4. **Span 持久化路径** — Span 数据存储在 `$PROJECT/.rnix/traces/<trace-id>/spans.jsonl`
5. **告警层级** — warning（剩余 < 20%）、critical（剩余 < 10%），告警通过 Log 和 SyscallEvent 传递
6. **NFR33** — Trace/Span 传播不增加 IPC 延迟超过 10ms
7. **NFR34** — ctx-profile 分析延迟 <= 1s
8. **环形缓冲区** — Token 历史最多保留 50 条记录
9. **增长预测算法** — 使用双速率模型（全局均值 + 最近 5 步移动均值），后者优先

## 验证记录

| 字段 | 值 |
|------|-----|
| 验证人 | |
| 验证日期 | |
| 构建版本 | |
| 总用例数 | 53 |
| 通过数 | |
| 失败数 | |
| 备注 | |
