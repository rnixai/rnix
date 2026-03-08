# Epic 14 手工验证指南：时间旅行调试（Time Travel Debugging）

## 概述

本文档提供 Epic 14 所有 Story 的手工验证步骤，用于在自动化测试之外对功能进行端到端的人工确认。

## 前置准备

Daemon 由 rnix 自动按需启动（`EnsureDaemon`），无需手动管理。

```bash
# 1. 构建最新版本
make build

# 2. 在终端 A：启动一个智能体（daemon 会自动启动）
#    智能体需要执行一个耗时较长的任务，以便在执行期间进行录制
./rnix -i "分析当前项目的目录结构并给出改进建议"

# 3. 在终端 B：确认进程在运行，记下 PID
./rnix ps
```

> **提示**：record/replay 操作需要在另一个终端进行，因为终端 A 被智能体的交互输出占用。

---

## Story 14.1: 执行录制与持久化（Execution Recording & Persistence）

### CLI 命令方式

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 开始录制 | `rnix record start <pid>` | 显示 `Recording started for PID X (record-id: X-XXXXXXXXXX)` | [ ] |
| 2 | 停止录制 | `rnix record stop <pid>` | 显示 `Recording stopped for PID X (N events captured)` | [ ] |
| 3 | 列出录制 | `rnix record list` | 表格列出所有录制：RECORD-ID、PID、STATUS、EVENTS、START、INTENT | [ ] |
| 4 | 无录制时列表 | 在没有任何录制的情况下 `rnix record list` | 显示 `No recordings found.` | [ ] |
| 5 | 无效 PID | `rnix record start abc` | 显示错误：`invalid PID: abc` | [ ] |
| 6 | 不存在的 PID | `rnix record start 99999` | 显示错误信息，提示进程不存在 | [ ] |
| 7 | Daemon 未启动 | 停止 daemon 后执行 `rnix record start 1` | 显示错误：`no active daemon` | [ ] |
| 8 | 重复录制 | 对已在录制的进程再次 `rnix record start <pid>` | 显示错误，提示已在录制中 | [ ] |
| 9 | 停止未录制的进程 | `rnix record stop <pid>`（该进程未在录制） | 显示错误，提示未在录制 | [ ] |
| 10 | JSON 输出 | `rnix record start <pid> --json` | 输出 JSON：`{"ok":true,"data":{"record_id":"...","pid":N}}` | [ ] |

### gdb 会话内方式

> 前提：已通过 `rnix gdb <pid>` 进入调试会话。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 11 | gdb 内开始录制 | `record start` | 显示 `[gdb] recording started (record-id: ...)` | [ ] |
| 12 | gdb 内停止录制 | `record stop` | 显示 `[gdb] recording stopped (N events captured)` | [ ] |
| 13 | gdb record 缺少参数 | `record` | 显示 usage：`record <start\|stop>` | [ ] |
| 14 | gdb record 无效子命令 | `record xyz` | 显示 `unknown record subcommand: xyz (valid: start, stop)` | [ ] |

### 数据持久化验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 15 | 录制文件存在 | 录制后检查 `$PROJECT/.rnix/records/<record-id>/` | 目录存在，包含 `metadata.json` 和 `events.jsonl` | [ ] |
| 16 | metadata 内容 | 查看 `metadata.json` | 包含 record_id、pid、intent、event_count、status 字段 | [ ] |
| 17 | events 格式 | 查看 `events.jsonl` | 每行一个 JSON 对象，包含 seq_num、timestamp、pid、type、payload | [ ] |
| 18 | 事件类型覆盖 | 检查 events.jsonl 中的事件类型 | 包含 syscall、context_snapshot、llm_response、state_change 等类型 | [ ] |

---

## Story 14.2: 录制回放与导航（Recording Replay & Navigation）

> 前提：已有至少一个已完成的录制（status=completed）。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 加载录制 | `rnix replay <record-id>` | 显示 `[replay] Loading record ...`，包含 PID、Intent、Events、Status 信息 | [ ] |
| 2 | next 前进 | 在 `replay>` 输入 `next` 或 `n` | 显示下一个事件详情，包含序号、类型、时间戳，以及位置 `[N/Total]` | [ ] |
| 3 | prev 后退 | 在 `replay>` 输入 `prev` 或 `p` | 显示上一个事件详情 | [ ] |
| 4 | goto 跳转 | `goto 5` | 跳转到序号 5 的事件并显示 | [ ] |
| 5 | list 上下文 | `list` 或 `l` | 显示当前位置附近的事件列表，当前事件有标记 | [ ] |
| 6 | info 摘要 | `info` 或 `i` | 显示录制摘要：PID、Intent、Events、Status、当前位置 | [ ] |
| 7 | help 帮助 | `help` 或 `h` | 列出所有 replay 命令及用法 | [ ] |
| 8 | quit 退出 | `quit` 或 `q` | 显示 `[replay] session ended.`，退出回放 | [ ] |
| 9 | 不存在的录制 | `rnix replay nonexistent-id` | 显示 `[replay] error: recording "nonexistent-id" not found` | [ ] |
| 10 | next 到末尾 | 连续 `next` 直到最后一个事件后再 `next` | 显示已到末尾的提示 | [ ] |
| 11 | prev 到开头 | 在第一个事件处 `prev` | 显示已到开头的提示 | [ ] |
| 12 | goto 无效序号 | `goto abc` | 显示 `invalid seq_num: abc` | [ ] |
| 13 | goto 缺少参数 | `goto` | 显示 usage：`goto <seq_num>` | [ ] |
| 14 | goto 超出范围 | `goto 999999` | 显示序号不存在的错误 | [ ] |
| 15 | 空行输入 | 在 `replay>` 直接回车 | 不报错，重新显示 `replay>` 提示符 | [ ] |
| 16 | 未知命令 | 输入 `xyz` | 显示 `[replay] unknown command: xyz (type 'help' for commands)` | [ ] |
| 17 | JSON 模式 | `rnix replay <record-id> --json` | 所有输出以 JSON 格式呈现 | [ ] |
| 18 | 无 daemon 回放 | 停止 daemon 后 `rnix replay <record-id>` | 正常加载和导航（回放是纯本地操作，不需要 daemon） | [ ] |

---

## Story 14.3: 上下文快照对比（Context Snapshot Diff）

> 前提：已进入 replay 会话，且录制中包含 context_snapshot 类型的事件。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 双参数 diff | `diff 3 6` | 显示事件 #3 与 #6 之间的上下文差异：System Prompt 变化、Messages 增减、Tokens 变化 | [ ] |
| 2 | 单参数 diff | 先 `goto 3`，然后 `diff 6` | 显示当前光标位置（#3）与 #6 之间的差异 | [ ] |
| 3 | System Prompt 未变 | diff 两个 system prompt 相同的点 | 显示 `System Prompt: unchanged (hash=...)` | [ ] |
| 4 | System Prompt 变化 | diff 两个 system prompt 不同的点 | 显示 `System Prompt: changed`，包含前后 hash | [ ] |
| 5 | Messages 增减 | diff 显示消息差异 | 显示新增（+）和删除（-）的消息，按角色标注 | [ ] |
| 6 | Token 增量 | diff 显示 token 变化 | 显示 `Tokens: X → Y (+Z)` 或 `(-Z)` 的增量标记 | [ ] |
| 7 | 无 snapshot 数据 | 在没有 context_snapshot 的录制中执行 diff | 显示 `no context snapshots available` | [ ] |
| 8 | diff 缺少参数 | `diff` | 显示 usage：`diff <seq1> <seq2> or diff <seq>` | [ ] |
| 9 | diff 无效序号 | `diff abc` | 显示 `invalid seq_num: abc` | [ ] |
| 10 | diff 光标未定位 | 未执行 next/goto 就使用 `diff <seq>` | 显示错误（光标在 -1 位置） | [ ] |
| 11 | JSON 模式 diff | 在 `--json` 模式下 `diff 3 6` | 输出 JSON 格式的 diff 结果 | [ ] |

---

## Story 14.4: 分叉继续分支探索（Fork-Continue Branch Exploration）

> 前提：已进入 replay 会话，光标在某个事件上。

### 进入 Fork 模式

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 进入 fork | 在 `replay>` 输入 `fork` | 显示 `[fork] Creating fork from event #XXX...`，包含 Original PID、Intent、Messages 数量，提示符变为 `fork>` | [ ] |
| 2 | fork 光标未定位 | 未 next/goto 就 `fork` | 显示错误（需要先定位到某个事件） | [ ] |

### 上下文修改

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 3 | 修改 system prompt | `set prompt 你是一个专注于安全的分析师` | 显示 `[fork] System prompt updated (len:N)` | [ ] |
| 4 | 追加消息 | `append user 请从安全角度重新分析` | 显示 `[fork] Message appended: [user] 请从安全角度重新分析` | [ ] |
| 5 | 删除消息 | `remove 2` | 显示 `[fork] Removed last 2 message(s). Messages remaining: N` | [ ] |
| 6 | 替换最后消息 | `replace 请用更简洁的方式回答` | 显示 `[fork] Last message replaced with: 请用更简洁的方式回答` | [ ] |
| 7 | 查看上下文摘要 | `show` | 显示当前 fork 上下文概要：System Prompt、Messages 列表 | [ ] |
| 8 | set 缺少参数 | `set` 或 `set abc` | 显示 usage：`set prompt <text>` | [ ] |
| 9 | append 缺少参数 | `append` 或 `append user` | 显示 usage：`append <role> <content>` | [ ] |
| 10 | remove 无效数字 | `remove abc` | 显示 usage，提示 n 必须为正整数 | [ ] |
| 11 | remove 缺少参数 | `remove` | 显示 usage：`remove <n>` | [ ] |
| 12 | replace 缺少参数 | `replace` | 显示 usage：`replace <content>` | [ ] |

### 执行与取消

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 13 | continue 执行 | `continue` 或 `go` | 连接 daemon，显示 `[fork] Creating forked process...`，创建新进程并显示新 PID | [ ] |
| 14 | 新进程可见 | fork continue 后在另一终端 `rnix ps` | 可以看到新创建的分叉进程 | [ ] |
| 15 | strace 新进程 | `rnix strace <new_pid>` | 可以追踪分叉进程的执行 | [ ] |
| 16 | cancel 取消 | `cancel` | 显示 `[fork] Fork cancelled. Returning to replay mode.`，提示符回到 `replay>` | [ ] |
| 17 | daemon 未运行时 continue | 停止 daemon 后 `continue` | 显示 `[fork] error: daemon not running ...`，提示需要运行 daemon | [ ] |
| 18 | fork help | `help` 或 `h` | 列出所有 fork 子命令 | [ ] |
| 19 | fork 未知命令 | 输入 `xyz` | 显示 `[fork] unknown command: xyz (type 'help' for commands)` | [ ] |
| 20 | fork 空行输入 | 在 `fork>` 直接回车 | 不报错，重新显示 `fork>` 提示符 | [ ] |

---

## 端到端完整流程验证

> 此节验证从录制到分叉的完整工作流。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 完整录制回放流程 | ① `rnix -i "分析项目结构"` ② `rnix record start <pid>` ③ 等待智能体执行若干步骤 ④ `rnix record stop <pid>` ⑤ `rnix record list` 获取 record-id ⑥ `rnix replay <record-id>` ⑦ `next` 几次浏览事件 | 每步输出正确，事件内容与智能体执行过程对应 | [ ] |
| 2 | diff 验证上下文变化 | 在 replay 中 `diff 1 <last>` | 能看到从初始到最终的完整上下文变化 | [ ] |
| 3 | fork-what-if 场景 | ① replay 中 `goto 3` ② `fork` ③ `append user 请改用 Python 实现` ④ `continue` ⑤ `rnix ps` 确认新进程 | 分叉进程正常运行，使用修改后的上下文 | [ ] |
| 4 | gdb 录制集成 | ① `rnix gdb <pid>` ② `record start` ③ `step` 几次 ④ `record stop` ⑤ 退出 gdb，用 `rnix replay` 回放 | gdb 中录制的事件可以通过 replay 回放 | [ ] |

---

## 关键注意事项

1. **非阻塞录制** — 录制失败不影响智能体正常执行，仅记录日志
2. **本地优先** — replay 和 diff 是纯本地文件操作，不需要 daemon 运行
3. **Fork Continue 需要 daemon** — 只有 fork 的 `continue/go` 命令需要连接 daemon 创建真实进程
4. **JSONL 格式** — events.jsonl 每行一个 JSON 对象，支持流式读写
5. **录制状态** — 只有 status=completed 的录制才能被 replay（recording 状态表示仍在录制中）
6. **一次一录** — 每个进程同时只能有一个活跃录制
7. **进程终止自动停止** — 进程被 reap 时自动停止录制

## 验证记录

| 字段 | 值 |
|------|-----|
| 验证人 | |
| 验证日期 | |
| 构建版本 | |
| 总用例数 | 55 |
| 通过数 | |
| 失败数 | |
| 备注 | |
