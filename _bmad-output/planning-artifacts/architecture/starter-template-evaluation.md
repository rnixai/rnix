# Starter Template Evaluation

## Primary Technology Domain

**Go 系统编程 / CLI 工具 / 运行时框架。** Rnix 不适用常规 Web 应用 starter，评估的是 Go 项目结构和工具链方案。

## Starter Options Considered

| 方案 | 描述 | 适合度 |
|------|------|--------|
| A: golang-standards/project-layout | 社区"标准"布局（`cmd/`, `internal/`, `pkg/`） | ⭐⭐⭐ 结构清晰但可能过度设计 |
| B: 最小平铺 + 按需增长 | 从 `main.go` 开始，随代码增长再分层 | ⭐⭐ 简单但 Rnix 已知需要多模块 |
| C: 领域驱动的 OS 隐喻结构 | `cmd/rnix/` + `kernel/` + `vfs/` + `drivers/` + `context/` + `skills/` + `debug/` | ⭐⭐⭐⭐ 与 OS 隐喻一致 |

## Selected Approach: 方案 C — 领域驱动的 OS 隐喻结构

**选择理由：**

1. Rnix 的模块边界由 OS 隐喻天然确定（kernel、vfs、drivers、context、skills、debug），不需要从通用布局反推
2. ~12 文件结构经过充分思考，与 PRD 功能需求领域一一对应
3. Go 标准布局的 `cmd/` + `internal/` 约定叠加在此结构上

**初始化命令：**

```bash
mkdir rnix && cd rnix
go mod init github.com/rnixai/rnix
```

## Architectural Decisions Established by Project Foundation

**Language & Runtime：**
- Go 1.26（利用 Green Tea GC、Goroutine Leak Profiler、自引用泛型等最新特性）
- 模块路径：`github.com/rnixai/rnix`
- 单 `main` 入口：`cmd/rnix/main.go`

**技术栈决策（已确立）：**

- **Go 1.26** — goroutine = 智能体进程，channel = IPC，interface = syscall 契约
- 单二进制编译，`go install` 分发，零额外依赖（需预装 Claude Code CLI）
- Go 1.26 新特性：`new(expr)` 初始化、自引用泛型约束、Goroutine Leak Profiler

**CLI 框架：** Cobra（`github.com/spf13/cobra`）
- 根命令：`rnix "意图"` — spawn 智能体（`--agent=<name>` 指定 Agent 定义）
- 子命令：`astrace`、`ps`、`kill`、`version`（Phase 2 追加：`compose`、`skill`、`top`、`log`）
- 全局 flags：`--json`、`--verbose`、`--quiet`

**终端 UI：** Charm 生态
- lipgloss — 样式化终端输出（表格、边框、颜色）
- bubbles — spinner 组件
- bubbletea — rnix top 交互式 TUI（Phase 2）

**测试框架：**
- Go 标准 `testing` + testify（assertions/mocks）
- 默认 `-race` 竞态检测
- 并发测试模式：100 goroutine 并发验证线程安全
- Mock 策略：接口抽象（LLMDriver、VFSFile、ProcessInfoProvider）

**代码质量：**
- golangci-lint（errcheck、govet、staticcheck、unused、gosimple）
- `make all` = lint → vet → test → build 质量门禁

**YAML 解析：** goccy/go-yaml — 统一 .yaml 后缀

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

**构建工具链：**

```bash
make all     # lint → vet → test → build（质量门禁）
make build   # go build -o rnix ./cmd/rnix/
make test    # go test -race ./...
make lint    # golangci-lint run
```

**安装方式：**

```bash
go install github.com/rnixai/rnix/cmd/rnix@latest
```

**依赖管理：** Go Modules（`go.mod`）

**Note:** 项目初始化（`go mod init` + 目录结构 + 基础 Makefile）应作为第一个实现 story。项目初始化不需要 Starter Template——Go 项目结构已完全确立，代码库已有 ~35 个 Go 文件和完整的模块体系。
