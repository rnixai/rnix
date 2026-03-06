# Epic 10b: Supervisor 与系统引导（Supervisor & System Bootstrap）

Supervisor 容错树自动管理子智能体生命周期 + init 引导序列在 daemon 启动时初始化系统级服务——多智能体系统的可靠性基础。

**Dependencies:** Epic 6（进程组用于 Supervisor 树）

## Story 10b.1: Supervisor 树与重启策略

As a 平台构建者,
I want 系统提供 Supervisor 树管理子智能体，自动重启异常退出的子进程,
So that 多智能体系统具备容错能力。

**Acceptance Criteria:**

**Given** `kernel/supervisor.go` 已实现
**When** 创建 Supervisor 进程（`SpawnSupervisor(spec)`）
**Then** Supervisor 监控其子智能体的健康状态

**Given** 子智能体异常退出
**When** Supervisor 检测到
**Then** 在 5 秒内按配置的策略自动重启（FR63）

**Given** 重启策略为 `one_for_one`
**When** 子进程 B 崩溃
**Then** 仅重启 B

**Given** 重启策略为 `one_for_all`
**When** 子进程 B 崩溃
**Then** 重启所有子进程

**Given** 重启策略为 `rest_for_one`
**When** 子进程 B 崩溃（B 是第 2 个启动的）
**Then** 重启 B 及其之后按启动顺序的所有子进程（FR64）

**Given** 子进程短时间内反复崩溃
**When** 超过重启频率阈值（MaxRestarts / MaxWindow）
**Then** Supervisor 自身退出，上报错误（避免重启风暴）

## Story 10b.2: init 引导序列

As a 系统,
I want daemon 启动时按配置初始化系统级服务和 Supervisor 树,
So that 系统启动后所有基础设施就位。

**Acceptance Criteria:**

**Given** `kernel/init.go` 中 Bootstrap 函数已实现
**When** daemon 启动
**Then** 按 `crux-init.yaml` 配置文件初始化系统级服务（FR65）：
**And** 日志聚合服务启动（`log_aggregator` 类型）
**And** Skill 注册表初始化（`skill_registry` 类型，扫描 `lib/skills/`）
**And** MCP 服务管理器初始化（`mcp_manager` 类型）
**And** Supervisor 树按配置构建

**Given** 初始化过程中某服务启动失败
**When** 为必须服务（`required: true`）
**Then** daemon 启动失败，输出具体错误信息和恢复建议
**And** 已启动的 Supervisor 自动回滚（rollbackSupervisors）

**Given** 初始化过程中某服务启动失败
**When** 为可选服务（`required: false`）
**Then** 记录警告，继续启动其余服务

---
