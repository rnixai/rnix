# Epic 11: AgentShell 高级语法（AgentShell Advanced Syntax）

从单命令到 Shell 脚本——管道组合、变量环境、流程控制，让智能体编排像写 bash 一样自然。

## Story 11.1: 管道语法

As a 用户,
I want 在 AgentShell 中通过管道语法组合智能体执行链,
So that 前一个智能体的输出自动成为后一个的输入。

**Acceptance Criteria:**

**Given** `shell/pipe.go` 已实现
**When** 执行 `spawn "分析代码" | spawn "写文档"`
**Then** 系统解析管道语法
**And** Spawn 第一个智能体执行"分析代码"
**And** 其输出通过 Pipe 自动注入第二个智能体"写文档"的上下文（FR66）

**Given** 管道链包含 ≥ 3 个智能体
**When** 执行 `spawn "A" | spawn "B" | spawn "C"`
**Then** 按顺序链式传递，A→B→C

**Given** 管道中某个智能体失败
**When** 退出非零码
**Then** 下游智能体不启动，管道中断并报告错误位置

## Story 11.2: 变量与环境传递

As a 用户,
I want 在 AgentShell 中定义变量和传递环境给智能体,
So that 智能体可以引用动态参数。

**Acceptance Criteria:**

**Given** `shell/script.go` 已实现
**When** 执行 `export TARGET=./src/auth.go`
**Then** 变量 `TARGET` 存储在 shell 环境中

**Given** 变量已定义
**When** Spawn 的智能体 intent 中引用 `$TARGET`
**Then** 变量值被替换后注入 intent（FR67）

**Given** 多个变量
**When** 在脚本中使用
**Then** 支持标准 `$VAR` 和 `${VAR}` 引用语法

## Story 11.3: 最小控制结构

As a 用户,
I want 在 AgentShell 中使用 if-else 和 on-error 编排执行流程,
So that 智能体工作流可以有条件分支和错误处理。

**Acceptance Criteria:**

**Given** `shell/script.go` 中控制结构已实现
**When** 执行多行脚本：
```
result = spawn "分析代码"
if $result.exitcode == 0
  spawn "生成报告"
else
  spawn "记录失败原因"
end
```
**Then** 按条件分支正确执行（FR68）

**Given** 使用 on-error
**When** 执行：
```
spawn "危险操作" on-error spawn "回滚"
```
**Then** "危险操作"失败时自动执行"回滚"

**Given** 嵌套控制结构
**When** 超过 1 层嵌套
**Then** 正确执行（完整脚本语言能力推迟至 Phase 3）

---
