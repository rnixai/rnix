# Epic 13 手工验证指南：交互式智能体调试（gdb）

## 概述

本文档提供 Epic 13 所有 Story 的手工验证步骤，用于在自动化测试之外对功能进行端到端的人工确认。

## 前置准备

Daemon 由 rnix 自动按需启动（`EnsureDaemon`），无需手动管理。

```bash
# 1. 构建最新版本
make build

# 2. 在终端 A：启动一个智能体（daemon 会自动启动）
#    智能体需要执行一个耗时较长的任务，以便在执行期间 attach 调试
./rnix -i "分析当前项目的目录结构并给出改进建议"

# 3. 在终端 B：确认进程在运行，记下 PID
./rnix ps

# 4. 在终端 B：用记下的 PID 进行后续 gdb 验证
./rnix gdb <pid>
```

> **提示**：gdb 需要在另一个终端操作，因为终端 A 被智能体的交互输出占用。

---

## Story 13.1: gdb 调试会话管理（Attach/Detach）

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 正常 Attach | `rnix gdb <pid>` | 显示 `[gdb] attached to PID X (state=Running, intent="...")`，并列出 skills、tokens 信息 | [ ] |
| 2 | Detach 命令 | 在 gdb 会话中输入 `detach`（或 `quit` / `q`） | 显示 `[gdb] detached from PID X`，用 `rnix ps` 确认进程仍为 Running | [ ] |
| 3 | Ctrl+C 退出 | 在 gdb 会话中按 `Ctrl+C` | 优雅断开，不影响目标进程 | [ ] |
| 4 | 不存在的 PID | `rnix gdb 99999` | 显示错误：`process not found or not running` | [ ] |
| 5 | 无效 PID 格式 | `rnix gdb abc` | 显示错误：`invalid PID (expected number)` | [ ] |
| 6 | Daemon 未启动 | 停止 daemon 后执行 `rnix gdb 1` | 显示错误：`no active daemon (process not found)` | [ ] |
| 7 | 重复 Attach | 终端 B 保持 gdb 会话不断开，在终端 C 对同一 PID 执行 `rnix gdb <pid>` | 第二个被拒绝，提示 `already has an active gdb session` | [ ] |
| 8 | Detach 后重新 Attach | 先 detach，再重新 `rnix gdb <pid>` | 可以正常重新 attach（detach 会释放会话锁） | [ ] |

---

## Story 13.2: 断点系统

> 前提：已通过 `rnix gdb <pid>` 进入调试会话。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | Syscall 断点 | `break syscall Read` | 提示断点已创建，`info bp` 可见该断点 | [ ] |
| 2 | Reasoning 断点 | `break reasoning` | 提示断点已创建 | [ ] |
| 3 | Quality 断点（pattern） | `break quality --pattern "安全漏洞"` | 提示断点已创建，类型为 quality/pattern | [ ] |
| 4 | Quality 断点（eval） | `break quality --eval "输出必须包含代码示例"` | 提示断点已创建，类型为 quality/eval | [ ] |
| 5 | Budget 断点 | `break budget 5000` | 提示断点已创建，阈值 5000 tokens | [ ] |
| 6 | 查看所有断点 | `info breakpoints` 或 `info bp` | 表格显示所有断点（ID、Type、Enabled、Hits、Condition） | [ ] |
| 7 | 删除断点 | `delete <bp_id>` | 提示断点已删除，`info bp` 中不再显示 | [ ] |
| 8 | 无效 break 类型 | `break unknown` | 提示错误：`unknown break type: unknown (valid: syscall, reasoning, quality, budget)` | [ ] |
| 9 | 缺少参数 | `break syscall`（无名称） | 提示 usage：`break syscall <name>` | [ ] |
| 10 | Quality 缺少 flag | `break quality` | 提示 usage：`break quality --pattern <pattern> \| --eval <criteria>` | [ ] |
| 11 | Budget 无效数字 | `break budget abc` | 提示错误：`invalid budget value: abc` | [ ] |

---

## Story 13.3: 单步执行与状态检查

> 前提：已进入 gdb 会话，并且智能体在断点处暂停（或设置了断点）。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | Step syscall（默认） | `step` 或 `step syscall` | 执行下一个 syscall 后暂停，显示 `[gdb] step syscall: <name>` 及参数、步骤编号 | [ ] |
| 2 | Step reasoning | `step reasoning` | 执行下一个推理步骤后暂停，显示推理结果摘要和步骤编号 | [ ] |
| 3 | Continue | `continue` 或 `c` | 恢复正常执行直到下一个断点或任务完成 | [ ] |
| 4 | Inspect context | `inspect context` 或 `inspect ctx` | 显示上下文详情：System Prompt (chars/tokens)、Messages 分类统计 (system/user/assistant/tool)、Total tokens、Last Message | [ ] |
| 5 | 无效 step 模式 | `step unknown` | 提示错误：`unknown step mode: unknown (valid: syscall, reasoning)` | [ ] |
| 6 | 无效 inspect 目标 | `inspect foo` | 提示错误：`unknown inspect target: foo (valid: context, ctx)` | [ ] |
| 7 | 空 inspect 参数 | `inspect` | 提示 usage：`inspect <context\|ctx>` | [ ] |

---

## Story 13.4: 运行时参数热修改

> 前提：已进入 gdb 会话，智能体在断点处暂停。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 修改模型 | `set model sonnet` | 提示模型已切换为 sonnet | [ ] |
| 2 | 追加上下文 | `set context append "额外分析指令"` | 提示内容已追加到上下文 | [ ] |
| 3 | 添加 Skill | `set skills add code-review` | 提示 skill 已添加 | [ ] |
| 4 | 设置环境变量 | `set env DEBUG=true` | 提示环境变量已设置 | [ ] |
| 5 | 缺少等号 | `set env INVALID` | 提示错误：`invalid format: expected KEY=VALUE (missing '=')` | [ ] |
| 6 | 无效 set 目标 | `set unknown` | 提示错误：`unknown set target: unknown (valid: model, context, skills, env)` | [ ] |
| 7 | 空 set 参数 | `set` | 提示 usage：`set <model\|context\|skills\|env> <args...>` | [ ] |
| 8 | model 缺少名称 | `set model` | 提示 usage：`set model <name>` | [ ] |
| 9 | skills 缺少动作 | `set skills` | 提示 usage：`set skills add <name>` | [ ] |

---

## 通用功能验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | Help 命令 | `help` 或 `h` | 列出所有 gdb 命令及其用法 | [ ] |
| 2 | Info 命令 | `info` 或 `i` | 显示进程概览（PID、State、Intent、Skills、Tokens） | [ ] |
| 3 | 未知命令 | 输入 `xyz` | 提示 `[gdb] unknown command: xyz (type 'help' for commands)` | [ ] |
| 4 | JSON 输出模式 | `rnix gdb <pid> --json` | 所有事件和输出以 JSON 格式呈现 | [ ] |
| 5 | Verbose 模式 | `rnix gdb <pid> --verbose` | 事件显示完整详细信息 | [ ] |
| 6 | 空行输入 | 在 `gdb>` 提示符直接回车 | 不报错，重新显示 `gdb>` 提示符 | [ ] |

---

## 关键注意事项

1. **单会话限制** — 每个 PID 只允许一个 gdb 会话同时 attach
2. **Detach 无副作用** — detach 后目标进程必须继续正常运行
3. **断点触发时机** — syscall/reasoning 断点依赖智能体实际执行，需要让智能体处于活跃任务中才能观察到触发
4. **Budget 断点一次性** — 达到 token 阈值触发一次后自动失效，不会重复触发
5. **热修改即时生效** — `set` 命令修改的参数应在下一个推理步骤中生效

## 验证记录

| 字段 | 值 |
|------|-----|
| 验证人 | |
| 验证日期 | |
| 构建版本 | |
| 总用例数 | 40 |
| 通过数 | |
| 失败数 | |
| 备注 | |
