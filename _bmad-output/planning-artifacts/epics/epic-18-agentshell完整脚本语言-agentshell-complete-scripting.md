# Epic 18: AgentShell 完整脚本语言

用户可以编写包含循环、函数、数据结构、并行执行的完整脚本，实现复杂的智能体编排自动化。

## Story 18.1: 循环结构与内置命令

As a 应用开发者,
I want 在 AgentShell 脚本中使用 for/while 循环和内置命令（wait/sleep/exit）,
So that 我可以编写重复和定时的智能体编排逻辑。

**Acceptance Criteria:**

**Given** AgentShell 脚本包含 `for item in [a, b, c]`
**When** 脚本执行
**Then** 循环体对每个元素执行一次，`${item}` 正确绑定当前元素

**Given** AgentShell 脚本包含 `while` 条件循环
**When** 条件为真时
**Then** 循环体重复执行，条件变假时退出

**Given** 脚本中使用 `wait <pid>`
**When** 指定进程完成
**Then** 脚本继续执行下一行

**Given** 脚本中使用 `sleep 5s`
**When** 执行到该行
**Then** 脚本暂停 5 秒后继续

## Story 18.2: 函数定义与调用

As a 应用开发者,
I want 在 AgentShell 脚本中定义和调用函数,
So that 我可以复用脚本逻辑。

**Acceptance Criteria:**

**Given** 脚本定义函数 `fn analyze(file) { ... }`
**When** 脚本调用 `analyze("config.yaml")`
**Then** 函数体执行，参数正确传递，返回值可用

**Given** 函数内部使用 `return result`
**When** 函数执行完毕
**Then** 调用方获得返回值

**Given** 函数调用参数数量不匹配
**When** 脚本解析时
**Then** 报告错误并指出行号和期望参数数量

## Story 18.3: 数据结构与字符串插值

As a 应用开发者,
I want 在 AgentShell 中使用数组、映射和字符串插值,
So that 我可以处理结构化数据并动态构建智能体意图。

**Acceptance Criteria:**

**Given** 脚本定义 `files = ["a.go", "b.go", "c.go"]`
**When** 访问 `files[0]`
**Then** 返回 "a.go"

**Given** 脚本定义 `config = {model: "sonnet", budget: 5000}`
**When** 访问 `config.model`
**Then** 返回 "sonnet"

**Given** 脚本包含 `spawn "分析 ${file_path} 的代码质量"`
**When** `file_path` 变量值为 "main.go"
**Then** 实际 intent 为 "分析 main.go 的代码质量"

**Given** 字符串插值中引用未定义变量
**When** 脚本执行
**Then** 报告错误并指出行号和未定义的变量名
**And** 脚本解析时间 <= 50ms（NFR38）

## Story 18.4: Spawn 返回值捕获与并行执行

As a 应用开发者,
I want 捕获 spawn 的返回值到变量，并使用 parallel 块并行执行多个 spawn,
So that 我可以组合智能体结果并加速并行任务。

**Acceptance Criteria:**

**Given** 脚本包含 `result = spawn "分析代码" --agent=analyst`
**When** 智能体完成
**Then** 智能体的最终输出绑定到 `result` 变量

**Given** 脚本包含 `parallel { spawn "A"; spawn "B"; spawn "C" }`
**When** parallel 块执行
**Then** 三个 spawn 并行启动，块结束时等待全部完成

**Given** parallel 块中一个 spawn 失败
**When** 其他 spawn 仍在执行
**Then** 不影响其他 spawn 继续执行，块结束时汇总报告所有结果
**And** 循环/函数调用运行时开销 <= 1ms/次（NFR39）

## Story 18.5: 模块化与脚本执行

As a 应用开发者,
I want 通过 `source` 导入其他脚本并通过 `rnix run` 执行脚本文件,
So that 我可以模块化组织脚本并直接运行。

**Acceptance Criteria:**

**Given** 脚本包含 `source ./lib/helpers.ash`
**When** 脚本执行
**Then** helpers.ash 中定义的函数和变量在当前脚本中可用

**Given** 一个 AgentShell 脚本文件 `deploy.ash`
**When** 用户执行 `rnix run deploy.ash`
**Then** 脚本按顺序执行，实时输出 spawn 进度，结束时显示汇总

**Given** 脚本首行为 `#!/usr/bin/env rnix run`
**When** 用户直接执行 `./deploy.ash`（已 chmod +x）
**Then** 脚本通过 shebang 正确执行

**Given** 脚本执行中出现语法错误
**When** 错误发生
**Then** 报告脚本名、行号和具体语法问题
