# Starter Template Evaluation

## Primary Technology Domain

**Go 系统编程 / CLI 工具 / 运行时框架。** Crux 不适用常规 Web 应用 starter，评估的是 Go 项目结构和工具链方案。

## Starter Options Considered

| 方案 | 描述 | 适合度 |
|------|------|--------|
| A: golang-standards/project-layout | 社区"标准"布局（`cmd/`, `internal/`, `pkg/`） | ⭐⭐⭐ 结构清晰但可能过度设计 |
| B: 最小平铺 + 按需增长 | 从 `main.go` 开始，随代码增长再分层 | ⭐⭐ 简单但 Crux 已知需要多模块 |
| C: 领域驱动的 OS 隐喻结构 | `cmd/crux/` + `kernel/` + `vfs/` + `drivers/` + `context/` + `skills/` + `debug/` | ⭐⭐⭐⭐ 与 OS 隐喻一致 |

## Selected Approach: 方案 C — 领域驱动的 OS 隐喻结构

**选择理由：**

1. Crux 的模块边界由 OS 隐喻天然确定（kernel、vfs、drivers、context、skills、debug），不需要从通用布局反推
2. ~12 文件结构经过充分思考，与 PRD 功能需求领域一一对应
3. Go 标准布局的 `cmd/` + `internal/` 约定叠加在此结构上

**初始化命令：**

```bash
mkdir crux && cd crux
go mod init github.com/usecrux/crux
```

## Architectural Decisions Established by Project Foundation

**Language & Runtime：**
- Go 1.26（利用 Green Tea GC、Goroutine Leak Profiler、自引用泛型等最新特性）
- 模块路径：`github.com/usecrux/crux`
- 单 `main` 入口：`cmd/crux/main.go`

**项目结构：**

```
crux/
├── cmd/crux/main.go              # CLI 入口（cobra 根命令）
├── kernel/                        # 微内核
│   ├── kernel.go                  # Kernel 结构体 + Spawn + reasonStep
│   ├── process.go                 # Process 结构体 + 生命周期状态机
│   └── reap.go                    # 进程回收（wait + orphan reparent）
├── vfs/                           # 虚拟文件系统
│   ├── vfs.go                     # VFS 接口（Open/Read/Write/Close/Stat）
│   ├── proc.go                    # /proc/{pid}/ 动态文件系统
│   └── dev.go                     # /dev/ 设备注册与路由
├── drivers/                       # 硬件抽象层
│   ├── llm/
│   │   ├── driver.go              # LLMDriver 接口 + DriverInfo
│   │   ├── claude_cli.go          # Claude Code CLI 驱动实现（MVP）
│   │   └── registry.go            # 驱动注册表：provider 路径 → 驱动实例
│   └── shell/shell.go             # Shell 执行驱动
├── context/                       # 上下文管理
│   └── context.go                 # ctx_alloc/read/write/free + prompt 组装
├── agents/                        # Agent 加载器
│   ├── types.go                   # AgentManifest + AgentModels + AgentInfo 类型定义
│   └── loader.go                  # agent.yaml 解析 + instructions.md 读取 + Skill 引用解析 + tools 聚合
├── skills/                        # Skill 加载器
│   └── loader.go                  # SKILL.md 解析（Agent Skills 标准格式）+ 渐进式加载
├── debug/                         # 调试工具
│   └── astrace.go                 # syscall 追踪（stream-json 消费）
├── internal/ui/                   # CLI 输出组件
│   ├── styles.go                  # lipgloss 样式集中定义
│   ├── progress.go                # Agent Progress Reporter
│   ├── result.go                  # Result Box
│   ├── error.go                   # Error Block
│   ├── summary.go                 # Summary Footer
│   ├── trace.go                   # Syscall Trace Line
│   └── table.go                   # Process Table
├── lib/agents/code-analyst/        # 参考 Agent 定义
│   ├── agent.yaml                 # Agent 配置：身份 + 模型 + Skill 引用
│   └── instructions.md            # Agent 角色定义 + 行为策略
├── lib/skills/code-analysis/      # 参考 Skill（Agent Skills 标准格式）
│   └── SKILL.md                   # name + description + allowed-tools + 程序性知识
├── go.mod
├── go.sum
├── Makefile                       # 构建 + 测试 + lint
└── README.md
```

**LLM 驱动层架构（多 Provider 设计）：**

VFS 挂载粒度为 **provider 级别**，模型选择通过调用参数传递：

| VFS 路径 | 驱动实现 | 阶段 |
|---------|---------|------|
| `/dev/llm/claude` | `claude_cli.go`（MVP） | MVP |
| `/dev/llm/openai` | `openai.go` | Phase 2 |
| `/dev/llm/deepseek` | OpenAI 兼容驱动 | Phase 2 |

核心接口（MVP 定义）：

```go
type LLMDriver interface {
    Call(ctx context.Context, req LLMRequest) (*LLMResponse, error)
    Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error)
    Info() DriverInfo
}
```

调用示例：

```go
fd, _ := k.vfs.Open("/dev/llm/claude", O_RDWR)
k.vfs.Write(fd, prompt, WriteOpts{Model: "sonnet"})
response := k.vfs.Read(fd, MaxTokens)
```

Agent manifest 映射（模型偏好属于 Agent 层）：

```yaml
# agent.yaml
models:
  provider: claude          # → /dev/llm/claude
  preferred: sonnet         # → 调用参数
  fallback: haiku           # → 失败时降级
skills:
  - code-analysis           # → 引用 Skill 名称
```

**CLI 框架：** Cobra（`github.com/spf13/cobra`）
- 根命令：`crux "意图"` — spawn 智能体（`--agent=<name>` 指定 Agent 定义）
- 子命令：`astrace`、`ps`、`kill`、`version`
- 全局 flags：`--json`、`--verbose`、`--quiet`

**样式与输出：** Charm 生态
- `github.com/charmbracelet/lipgloss` — 声明式终端样式
- `github.com/charmbracelet/bubbles` — spinner 组件
- `github.com/charmbracelet/bubbletea` — 仅 Phase 2 引入

**构建工具链：**
- `go build` / `go install` — 标准编译
- `Makefile` — `make build`、`make test`、`make lint`
- `golangci-lint` — 代码质量检查
- GoReleaser — Phase 2 公开发布时引入

**测试框架：**
- Go 标准 `testing` 包
- `github.com/stretchr/testify` — assertions 和 mocks
- 测试文件与源文件同目录（Go 惯例）

**依赖管理：** Go Modules（`go.mod`）

**Note:** 项目初始化（`go mod init` + 目录结构 + 基础 Makefile）应作为第一个实现 story。
