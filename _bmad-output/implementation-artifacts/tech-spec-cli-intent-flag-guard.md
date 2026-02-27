---
title: 'CLI Intent Flag 与命令路由防护'
slug: 'cli-intent-flag-guard'
created: '2026-02-27'
status: 'completed'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go 1.26', 'Cobra v1.10.2', 'lipgloss']
files_to_modify: ['cmd/crux/main.go', 'cmd/crux/main_test.go']
code_patterns: ['cobra rootCmd with ArbitraryArgs', 'global flag vars with save/restore in tests', 'runRoot reads positional args as intent', 'init() registers flags and subcommands']
test_patterns: ['same-package tests (package main)', 'direct function calls (runRoot/runKill)', 'global flag save/restore pattern', 'cobra SetOut/SetArgs mock', 'TestXxx_Yyy naming']
---

# Tech-Spec: CLI Intent Flag 与命令路由防护

**Created:** 2026-02-27

## Overview

### Problem Statement

当前 `crux top` 这样的裸参数被 `rootCmd`（`Args: cobra.ArbitraryArgs`）直接当作 intent 字符串送给 agent，导致：
- 用户打错子命令时误触发 agent，消耗 LLM tokens
- 需要 Ctrl+C 才能中断，体验差
- 用户无法区分"执行子命令"和"提交 intent"两种意图

### Solution

1. 新增 `-i`/`--intent` flag 作为提交意图的唯一入口
2. 裸参数不再当作 intent，而是触发全子命令模糊匹配，给出 "did you mean?" 建议或错误提示

### Scope

**In Scope:**
- 新增 `-i`/`--intent` string flag
- `rootCmd.Args` 从 `ArbitraryArgs` 改为自定义验证器
- 裸参数做全子命令（含 hidden `daemon`）模糊匹配，给出建议
- 无参数且无 `-i` 时显示 help
- 裸参数报错一律纯文本，不受 `--json` 影响
- 更新 rootCmd 的 Use/Example/Long 文本
- 更新 `runDaemon` 中引用旧用法的硬编码错误消息

**Out of Scope:**
- 交互式 REPL 模式
- Intent 内容校验或长度限制
- 其他 CLI 结构性变更

## Context for Development

### Codebase Patterns

- **全局 flag 变量**：`flagJSON`, `flagVerbose`, `flagQuiet`, `flagModel`, `flagMaxSteps`, `flagAgent` 定义在 `main.go:36-43`，新增 `flagIntent` 遵循同一模式
- **Flag 注册**：在 `init()` 函数中通过 `rootCmd.Flags()` 注册（非 `PersistentFlags()`，因为 `-i` 仅在 root 层使用，不需要向子命令传播），如 `rootCmd.Flags().StringVarP(&flagModel, "model", "m", "", "...")`
- **子命令注册**：`init()` 中通过 `rootCmd.AddCommand()` 注册，当前 5 个：`version`, `astrace`, `ps`, `kill`, `daemon`(hidden)
- **rootCmd.Args**：当前 `cobra.ArbitraryArgs`，需改为自定义验证函数。**Cobra 行为**：当 `Args` 验证函数返回 error 时，Cobra 中止执行，不会调用 `RunE`，错误由 `cmd.Execute()` 返回
- **runRoot 入口**：`len(args)==0` → help，否则 `strings.Join(args, " ")` → intent string → `ipc.SpawnRequest`
- **错误输出**：非 JSON 模式通过 `ui.RenderError()` 输出，含设备路径、原因、影响、建议
- **IPC 测试模式**：现有测试使用 `setupTestIPCServer()` 创建测试 IPC 服务器 + `ipc.SocketPathOverride` 覆盖 socket 路径来模拟 IPC 连接（参见 `main_test.go:609-633` 和 `main_test.go:649-670`）
- **全局 flag 测试模式**：每个测试必须 save → defer restore → set → test，防止测试间状态泄漏（参见 `main_test.go:297-299`）
- **Cobra `cmd.Commands()` 返回顺序**：按字母序排列（非注册顺序），模糊匹配遍历结果受此影响

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `cmd/crux/main.go` — 全局 flag 变量块 | `flagJSON`...`flagAgent` 定义区域 |
| `cmd/crux/main.go` — `rootCmd` 变量 | rootCmd 定义（Use/Args/RunE） |
| `cmd/crux/main.go` — `init()` 函数 | flag 注册 + 子命令注册 |
| `cmd/crux/main.go` — `runRoot()` 函数 | 读取 args 构建 intent 并 spawn |
| `cmd/crux/main.go` — `runDaemon()` 函数 | 含引用旧 intent 用法的硬编码错误消息 |
| `cmd/crux/main_test.go` — `TestHelp_*` | Help 输出测试 |
| `cmd/crux/main_test.go` — `TestVersion_*` | flag save/restore 模式参考 |
| `cmd/crux/main_test.go` — `setupTestIPCServer` | IPC 测试基础设施 |
| `cmd/crux/main_test.go` — `TestRunKill_PIDNotFound_ViaIPC` | SocketPathOverride 使用示例 |

**注意**：以上引用的是函数/变量名而非行号。随着 Task 顺序执行，行号会偏移，请以函数/变量名定位。

### Technical Decisions

- **Breaking change**：`crux "意图"` 不再工作，必须用 `crux -i "意图"`
- **裸参数报错不受 `--json` flag 影响**，一律纯文本。Cobra `Args` 验证失败时由 `cmd.Execute()` 返回 error，最终经 `main()` 中 `os.Exit(2)` 退出
- **模糊匹配覆盖所有已注册子命令**（含 hidden `daemon`），使用标准 Levenshtein 距离（非 Damerau-Levenshtein，不考虑转置操作）+ 前缀匹配
- **匹配优先级**（从高到低）：精确匹配 > 前缀匹配 > Levenshtein 匹配。当同一优先级有多个候选时，取字母序第一个（因为 `cmd.Commands()` 按字母序返回）
- **Levenshtein 阈值选择 ≤ 2**：平衡匹配灵敏度和误触发。阈值 1 太严格（仅捕获单字符错误），阈值 3 对短命令（如 `ps`）误触发太高
- **短输入保护**：`len(input) <= 3` 时跳过 Levenshtein 匹配，仅用精确/前缀。注意 `"kil"` 会通过前缀匹配到 `"kill"`（`strings.HasPrefix("kill", "kil")` = true），短输入保护仅禁用 Levenshtein 而不禁用前缀。真正受影响的是如 `"is"` 这类与任何子命令都无精确/前缀关系的短输入
- **`rootCmd.SilenceUsage = true`**：必须设置此字段。否则 Cobra 在 `rejectPositionalArgs` 返回 error 后会自动打印完整 usage 块到 stderr，导致与错误消息中的 mini-usage 提示重复输出
- **无新依赖**：模糊匹配用简单字符串算法实现，不引入第三方库
- **`-i` 使用 `Flags()` 而非 `PersistentFlags()`**：intent flag 仅在 root 命令有意义，不应向子命令传播。`crux -i "intent" version` 时 Cobra 路由到 version 子命令，由于 `-i` 是 root 的 local flag 而非 persistent flag，Cobra 会报 `unknown flag` 错误。这与现有 `--model`/`--agent` 行为一致，是预期的

## Implementation Plan

### Tasks

- [x] Task 1: 添加 `flagIntent` 全局变量并注册 flag
  - File: `cmd/crux/main.go`
  - Action: 在全局 flag 变量块中添加 `flagIntent string`；在 `init()` 函数中注册 `rootCmd.Flags().StringVarP(&flagIntent, "intent", "i", "", "Intent string to spawn an agent")`
  - Notes: 遵循现有 `flagModel`/`flagAgent` 的 `StringVarP` 模式。使用 `Flags()` 而非 `PersistentFlags()`

- [x] Task 2: 实现 `levenshtein()` 字符串距离函数
  - File: `cmd/crux/main.go`
  - Action: 在 `runRoot` 之前添加 `func levenshtein(a, b string) int`，标准 DP 实现（插入/删除/替换，不含转置），返回两个字符串的编辑距离
  - Notes: 不引入第三方库；仅用于模糊匹配，不需要 Unicode 归一化。明确使用标准 Levenshtein 而非 Damerau-Levenshtein

- [x] Task 3: 实现 `suggestCommand()` 模糊匹配函数
  - File: `cmd/crux/main.go`
  - Action: 添加 `func suggestCommand(cmd *cobra.Command, input string) string`。遍历 `cmd.Commands()`（含 hidden，按字母序返回），按以下**严格优先级**匹配：
    1. **精确匹配**：`input == cmd.Name()` → 立即返回该命令名
    2. **前缀匹配**：`strings.HasPrefix(cmd.Name(), input)` → 记录第一个命中的候选（字母序最先）
    3. **Levenshtein 匹配**（仅当 `len(input) > 3`）：距离 ≤ 2 → 记录最小距离候选（距离相同取字母序最先）

    遍历结束后，返回优先级最高的候选：有前缀候选 → 返回前缀候选；否则有 Levenshtein 候选 → 返回 Levenshtein 候选；都没有 → 返回空字符串
  - Notes: `cmd.Commands()` 按字母序返回。由于遍历顺序确定，同优先级多候选取字母序第一个是自然结果

- [x] Task 4: 实现 `rejectPositionalArgs()` 自定义 Args 验证器
  - File: `cmd/crux/main.go`
  - Action: 添加 `func rejectPositionalArgs(cmd *cobra.Command, args []string) error`。如果 `len(args) == 0` 返回 nil。否则将所有 args 用空格 join 为展示文本 `display := strings.Join(args, " ")`，调用 `suggestCommand(cmd, args[0])`（模糊匹配只用第一个词），构建错误信息。**错误消息规范格式**：
    - 有建议时：`unknown command "%s", did you mean "%s"?\n\n  Usage: crux -i <intent>\n  Run 'crux --help' for available commands.`（第一个 `%s` = display，第二个 `%s` = suggestion）
    - 无建议时：`unknown command "%s"\n\n  Usage: crux -i <intent>\n  Run 'crux --help' for available commands.`（`%s` = display）
  - Notes: 函数返回 error 后，Cobra 不会调用 `RunE`，直接由 `cmd.Execute()` 打印错误并返回。测试时直接调用 `rejectPositionalArgs(rootCmd, args)` 并检查返回的 `error.Error()` 字符串内容

- [x] Task 5: 更新 `rootCmd` 定义
  - File: `cmd/crux/main.go` — `rootCmd` 变量 + `init()` 函数
  - Action: 修改以下字段：
    - `Use`: `"crux [command]"`（标准 cobra 模式，`-i` 在 Long/Example 中说明）
    - `Long`: `"Crux is an operating system for AI agents.\n\nUse -i flag to spawn an agent with an intent."`
    - `Example`: 更新为：
      ```
        crux -i "分析 ./README.md"
        crux -i "重构 main.go 中的错误处理"
        crux version
        crux -i "分析项目结构" --json
      ```
    - `Args`: `cobra.ArbitraryArgs` → `rejectPositionalArgs`

    **额外**：在 `init()` 函数中添加 `rootCmd.SilenceUsage = true`，防止 Cobra 在 Args 验证失败时自动打印完整 usage（会与错误消息中的 mini-usage 重复）。**注意**：`SilenceUsage` 是全局设置，也会影响其他错误路径（如 `kill` 无参数时 Cobra 不再自动打印 usage）。现有的 `kill`/`astrace` 的 Args 验证（`cobra.ExactArgs(1)`）在失败时原本会输出 usage，设置后不再输出——但这些命令已有自定义错误处理，不依赖 Cobra 的自动 usage 输出，因此不受影响。开发者实现时应运行完整测试套件确认无回归
  - Notes: `Use` 设为 `"crux [command]"` 是 cobra 标准模式。intent 用法在 Long 和 Example 中展示

- [x] Task 6: 修改 `runRoot` 读取 intent 来源
  - File: `cmd/crux/main.go` — `runRoot()` 函数
  - Action: 将 `intent := strings.Join(args, " ")` 改为 `intent := flagIntent`；将 `if len(args) == 0 { return cmd.Help() }` 改为 `if flagIntent == "" { return cmd.Help() }`
  - Notes: 当 `rejectPositionalArgs` 验证通过时（`len(args)==0`），Cobra 才会调用 `RunE`

- [x] Task 7: 更新 `runDaemon` 错误消息
  - File: `cmd/crux/main.go` — `runDaemon()` 函数
  - Action: 将 `"daemon command is for internal use only; use 'crux \"intent\"' to run agents"` 改为 `"daemon command is for internal use only; use 'crux -i \"intent\"' to run agents"`
  - Notes: 与 breaking change 保持一致

- [x] Task 8: 更新现有测试
  - File: `cmd/crux/main_test.go`
  - Action:
    - `TestHelp_ContainsExample`：将断言更新为同时检查 `strings.Contains(output, "crux -i")` 和 `strings.Contains(output, "分析 ./README.md")`，确保新格式和内容都被验证
    - `TestHelp_ContainsUsage`：新增断言 `strings.Contains(output, "--intent")`，验证 `-i/--intent` flag 出现在 help 的 flags 区域（覆盖 AC 8）
    - `TestDaemonCmd_RequiresInternalFlag`：现有测试仅检查 `err == nil`，不检查消息文本，无需修改
    - 确认 `TestHelp_ContainsPsSubcommand` 仍然通过（不变）
  - Notes: 其他现有测试（version/ps/kill/astrace）不受影响

- [x] Task 9: 新增 intent flag 测试
  - File: `cmd/crux/main_test.go`
  - Action: 添加以下测试，**每个测试必须 save/restore `flagIntent`、`exitCode`**：
    - `TestRunRoot_IntentFlag`: save/restore `flagIntent` 和 `exitCode`。设置 `flagIntent = "test"`。使用 `setupTestIPCServer()` 获取 sockPath，设置 `ipc.SocketPathOverride = sockPath`（defer 还原）。调用 `runRoot(rootCmd, []string{})`。**断言**：`exitCode == 1`（因为没有 LLM 驱动，spawn 会失败并设 exitCode=1）。这证明进入了 spawn 路径而非 help 路径（help 路径不设 exitCode）
    - `TestRunRoot_NoArgsNoIntent_ShowsHelp`: save/restore `flagIntent`。设置 `flagIntent = ""`。通过 `rootCmd.SetOut(&buf)` 捕获输出（defer `rootCmd.SetOut(nil)`）。调用 `runRoot(rootCmd, []string{})`，**断言**：返回值为 nil 且 `buf` 包含 `"Usage:"`
    - `TestRunRoot_IntentWithMultipleFlags`: save/restore `flagIntent`、`flagJSON`、`flagModel`、`exitCode`。设置 `flagIntent = "test"`, `flagJSON = true`, `flagModel = "opus"`。使用 `setupTestIPCServer()` + `ipc.SocketPathOverride`。调用 `runRoot(rootCmd, []string{})`。**断言**：`exitCode == 1`（spawn 失败但证明进入了 spawn 路径且未因 flag 解析问题 panic）。这覆盖 AC 7 的自动化验证
  - Notes: 遵循现有 flag save/restore 模式。注意 `setupTestIPCServer` 返回的 cleanup 通过 `t.Cleanup` 注册，会在测试函数结束时自动执行

- [x] Task 10: 新增裸参数拒绝测试
  - File: `cmd/crux/main_test.go`
  - Action: 添加以下测试（**直接调用 `rejectPositionalArgs(rootCmd, args)` 并检查 `error.Error()` 子串**）：
    - `TestRejectPositionalArgs_Empty`: `rejectPositionalArgs(rootCmd, []string{})` → 返回 nil
    - `TestRejectPositionalArgs_UnknownWord`: `rejectPositionalArgs(rootCmd, []string{"top"})` → `err.Error()` 包含 `unknown command "top"` 和 `crux -i <intent>`，且**不包含** `did you mean`
    - `TestRejectPositionalArgs_SuggestsClose`: `rejectPositionalArgs(rootCmd, []string{"ver"})` → `err.Error()` 包含 `did you mean "version"`
    - `TestRejectPositionalArgs_NoSuggestion`: `rejectPositionalArgs(rootCmd, []string{"zzz"})` → `err.Error()` 不包含 `did you mean`，包含 `unknown command "zzz"`
    - `TestRejectPositionalArgs_MultipleArgs`: `rejectPositionalArgs(rootCmd, []string{"foo", "bar"})` → `err.Error()` 包含 `unknown command "foo bar"`
    - `TestRejectPositionalArgs_IntentPlusBareArgs`: `rejectPositionalArgs(rootCmd, []string{"bar"})` → 返回 error（即使 `flagIntent` 已设置也无影响，因为 `rejectPositionalArgs` 不检查 flag，它只看 positional args）。实际场景：`crux -i "foo" bar` 时 Cobra 先解析 flag 设置 `flagIntent="foo"`，再将 `["bar"]` 传给 Args 验证器，验证失败 → 报错，不启动 agent
  - Notes: 直接调用函数检查 error 返回值，不需要通过 cobra Execute 流程

- [x] Task 11: 新增 `--json` 不影响裸参数报错的端到端测试
  - File: `cmd/crux/main_test.go`
  - Action: 添加 `TestBareArgs_JsonFlagIgnored`：save/restore `flagJSON`。设置 `flagJSON = true`。通过 `rootCmd.SetArgs([]string{"top"})` + `rootCmd.SetErr(&buf)` 捕获 stderr 输出（defer 还原）。调用 `rootCmd.Execute()`。**断言**：`buf` 包含 `unknown command "top"`（纯文本），不包含 `{"ok":`（非 JSON）。defer 还原 `rootCmd.SetArgs(nil)` 和 `rootCmd.SetErr(nil)`
  - Notes: 这是 AC 5 的自动化验证。Cobra 的 Args 错误走 `cmd.SetErr()` 通道，不经过 `runRoot` 的 JSON 输出逻辑

- [x] Task 12: 新增模糊匹配测试
  - File: `cmd/crux/main_test.go`
  - Action: 添加以下测试（**直接调用 `suggestCommand(rootCmd, input)` 测试**，由于 `rootCmd` 是包级 `init()` 注册的全局实例，子命令集在测试运行时已确定且稳定）：
    - `TestSuggestCommand_ExactMatch`: `"ps"` → `"ps"`
    - `TestSuggestCommand_Prefix`: `"ver"` → `"version"`
    - `TestSuggestCommand_Levenshtein`: `"astrce"` → `"astrace"`（距离 1，len=6 > 3）
    - `TestSuggestCommand_Hidden`: `"deamon"` → `"daemon"`（标准 Levenshtein 距离 2：两次替换 `e→a` 和 `a→e`，len=6 > 3）
    - `TestSuggestCommand_ShortInputSkipsLevenshtein`: `"is"` → `""`（len=2 ≤ 3，跳过 Levenshtein）
    - `TestSuggestCommand_ShortInputPrefixStillWorks`: `"ki"` → `"kill"`（len=2 ≤ 3，但前缀匹配仍生效）
    - `TestSuggestCommand_Len3PrefixMatch`: `"kil"` → `"kill"`（len=3 ≤ 3，Levenshtein 被跳过，但 `strings.HasPrefix("kill", "kil")` 为 true，前缀匹配生效）
    - `TestSuggestCommand_NoMatch`: `"zzzzz"` → `""`
    - `TestLevenshtein`: 基础用例 — `("ps","ps")→0`, `("kil","kill")→1`, `("","abc")→3`, `("abc","xyz")→3`, `("deamon","daemon")→2`
  - Notes: `rootCmd` 全局状态在测试间是只读的（子命令集不变），因此无需创建隔离实例

### Acceptance Criteria

- [x] AC 1: Given 用户执行 `crux -i "分析代码"`, when CLI 处理参数, then intent 被正确传递给 `ipc.SpawnRequest.Intent` 并启动 agent
- [x] AC 2: Given 用户执行 `crux`（无参数无 flag）, when CLI 处理参数, then 显示 help 信息
- [x] AC 3: Given 用户执行 `crux top`（单个裸参数，无 `-i`）, when CLI 处理参数, then 输出错误含 `unknown command "top"` 和 `crux -i <intent>` 且不启动 agent
- [x] AC 4: Given 用户执行 `crux ver`（裸参数，近似子命令）, when CLI 处理参数, then 输出含 `unknown command "ver", did you mean "version"?` 且不启动 agent
- [x] AC 5: Given 用户执行 `crux --json top`（带 `--json` 的裸参数）, when CLI 处理参数, then 输出纯文本错误（不含 JSON 格式），不受 `--json` 影响
- [x] AC 6: Given 用户执行 `crux deamon`（hidden 子命令拼写错误）, when CLI 处理参数, then 输出含 `did you mean "daemon"?`
- [x] AC 7: Given 用户执行 `crux -i "分析代码" --json --model opus`, when CLI 处理参数, then 所有 flag 正常解析，intent + model + json 均正确传递
- [x] AC 8: Given 用户执行 `crux --help`, when CLI 处理参数, then help 文本包含 `-i, --intent` flag 说明和新格式的 example（含 `crux -i`）
- [x] AC 9: Given 用户执行 `crux foo bar baz`（多个裸参数）, when CLI 处理参数, then 输出含 `unknown command "foo bar baz"` 且不启动 agent
- [x] AC 10: Given 用户执行 `crux is`（≤ 3 字符短输入）, when CLI 处理参数, then 输出 `unknown command "is"` 且不建议 `"ps"`（短输入不做 Levenshtein）
- [x] AC 11: Given 用户执行 `crux ki`（≤ 3 字符但是合法前缀）, when CLI 处理参数, then 输出含 `did you mean "kill"?`（前缀匹配不受短输入保护限制）
- [x] AC 12: Given 用户执行 `crux kil`（len=3 前缀匹配边界）, when CLI 处理参数, then 输出含 `did you mean "kill"?`（`strings.HasPrefix("kill", "kil")` 为 true）
- [x] AC 13: Given 用户执行 `crux -i "foo" bar`（同时有 intent flag 和裸参数）, when CLI 处理参数, then 输出 `unknown command "bar"` 错误且不启动 agent（Args 验证优先于 RunE）

## Additional Context

### Dependencies

无新依赖。Levenshtein 距离算法内置实现（标准版本，非 Damerau），不引入第三方库。

### Testing Strategy

**单元测试（全部在 `cmd/crux/main_test.go`）：**
- `levenshtein()`: 基础距离计算（相同/不同/空字符串/前缀/转置对比）
- `suggestCommand()`: 精确匹配/前缀/Levenshtein/hidden/短输入保护/短输入前缀/无匹配
- `rejectPositionalArgs()`: 空参数/未知命令/有建议/无建议/多参数
- `runRoot` 路径: intent flag → spawn 路径（通过 `setupTestIPCServer` + `SocketPathOverride` 模拟 IPC）/ 无 intent → help
- `--json` 不影响裸参数报错: 端到端测试通过 `rootCmd.Execute()` + `SetErr()` 验证
- 现有测试更新: help example 断言同步、daemon 错误消息同步
- **每个测试必须 save/restore 所有修改的全局变量**（`flagIntent`, `flagJSON`, `exitCode` 等）

**手动验证：**
- `crux -i "hello"` → 正常启动 agent
- `crux top` → 纯文本错误，无 agent 启动
- `crux ver` → "did you mean version?"
- `crux ki` → "did you mean kill?"（前缀匹配）
- `crux foo bar` → `unknown command "foo bar"`
- `crux is` → `unknown command "is"`（无建议）
- `crux --json top` → 纯文本错误（非 JSON）
- `crux` → 显示 help
- `crux --help` → 显示含 `-i` flag 的帮助

### Notes

- 这是一个 **breaking change**：所有现有的 `crux "意图"` 用法需迁移为 `crux -i "意图"`
- `integration_test.go` 中的 `runE2E` 直接调 `kern.Spawn(intent, ...)`，不经 CLI 层，不受此变更影响
- `docs/quick-start.md` 和 `docs/concepts.md` 中如有 CLI 用法示例需后续更新（不在此 spec 范围内，可作为 follow-up）
- Levenshtein 阈值 ≤ 2 + 短输入保护（len ≤ 3 跳过 Levenshtein）是经分析选择的平衡点
- 短输入保护仅禁用 Levenshtein，前缀匹配不受影响：`"kil"` 通过前缀匹配 `"kill"`，`"ki"` 通过前缀匹配 `"kill"`，`"is"` 无精确/前缀匹配返回空
- `crux -i "intent" version` 时 Cobra 路由到 version 子命令，由于 `-i` 是 root 的 local flag，Cobra 报 `unknown flag` 错误。这与 `--model`/`--agent` 行为一致，是预期的
- `crux -i "intent" -- extra` 中 `--` 后的 tokens 作为 positional args 被 `rejectPositionalArgs` 拦截。这是预期行为：`--` 传递在此 CLI 中无意义
- `crux -i "foo" bar` 中 Cobra 先解析 `-i "foo"` 设置 `flagIntent`，再将 `["bar"]` 传给 `rejectPositionalArgs`，验证失败报错，`RunE` 不会被调用，agent 不会启动

## Review Notes
- Adversarial review completed
- Findings: 12 total, 0 fixed (all validated as noise/spec-mandated/known-limitation), 12 skipped
- Resolution approach: auto-fix (no actionable real issues found after verification)
- Key verification: Cobra v1.10.2 `Commands()` confirmed to sort alphabetically (`commandSorterByName`)
