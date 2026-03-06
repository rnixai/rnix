# Project Structure & Boundaries

## 完整项目目录结构

```
crux/
├── cmd/crux/
│   └── main.go                           # CLI 入口：cobra 根命令 + 子命令注册
│
├── kernel/
│   ├── types.go                          # 共享类型：PID, FD, CtxID, ErrCode, Signal, ProcessState
│   ├── errors.go                         # SyscallError + ErrCode 常量 + error 辅助函数
│   ├── kernel.go                         # KernelImpl 结构体 + Spawn() + reasonStep() 循环
│   ├── process.go                        # Process 结构体 + 状态机转移 + 生命周期方法
│   ├── reap.go                           # Wait() + 孤儿 reparent + Zombie→Dead 回收
│   ├── generics.go                       # SyncMap[K,V] + Registry[T] + Future[T] + Result[T]
│   ├── kernel_test.go                    # Kernel 单元测试（Spawn/reasonStep/Kill）
│   ├── process_test.go                   # 状态机转移测试
│   ├── reap_test.go                      # Wait/orphan/zombie 回收测试
│   └── generics_test.go                  # 泛型工具测试
│
├── vfs/
│   ├── vfs.go                            # VFSFile 接口 + VFS 结构体 + Open/Read/Write/Close/Stat
│   ├── proc.go                           # ProcFS：/proc/{pid}/status|intent|context 动态生成
│   ├── dev.go                            # DeviceRegistry：/dev/ 路径注册与路由
│   ├── vfs_test.go                       # VFS 集成测试
│   ├── proc_test.go                      # ProcFS 单元测试
│   └── dev_test.go                       # 设备注册路由测试
│
├── drivers/
│   ├── llm/
│   │   ├── driver.go                     # LLMDriver 接口 + LLMRequest/LLMResponse + StreamEvent + DriverInfo
│   │   ├── claude_cli.go                 # ClaudeCliDriver：exec.Command 封装 + stream-json 解析
│   │   ├── registry.go                   # LLM 驱动注册表（基于 Registry[LLMDriver]）
│   │   ├── claude_cli_test.go            # Claude CLI 驱动测试（mock exec.Command）
│   │   └── registry_test.go             # 驱动注册表测试
│   └── shell/
│       ├── shell.go                      # ShellDriver：exec.Command 封装 + stdout/stderr 捕获
│       └── shell_test.go                # Shell 驱动测试
│
├── context/
│   ├── context.go                        # Context 结构体 + Alloc/Read/Write/Free + BuildPrompt()
│   └── context_test.go                   # 上下文管理测试
│
├── agents/
│   ├── types.go                          # AgentManifest + AgentModels + AgentInfo 类型定义
│   ├── loader.go                         # AgentLoader：加载 agent.yaml + instructions.md + 解析 Skill 引用 + 聚合 tools
│   └── loader_test.go                    # Agent 加载器测试
│
├── skills/
│   ├── loader.go                         # SkillLoader：解析 SKILL.md（Agent Skills 标准格式）+ 渐进式加载（metadata-only / full）
│   ├── types.go                          # SkillManifest（Name/Description/AllowedTools/Metadata）+ SkillInfo 类型定义
│   ├── loader_test.go                    # Skill 加载测试
│   └── testdata/                         # 测试用 Skill fixtures
│       └── mock-skill/
│           └── SKILL.md
│
├── debug/
│   ├── astrace.go                        # Astrace：消费 DebugChan + 格式化输出 + stream-json 事件桥接
│   ├── event.go                          # SyscallEvent 结构体 + 事件记录辅助函数
│   ├── astrace_test.go                   # astrace 格式化输出测试
│   └── event_test.go                     # 事件记录测试
│
├── internal/
│   └── ui/
│       ├── renderer.go                   # Renderer 接口 + TerminalProfile 检测 + 输出模式切换
│       ├── styles.go                     # lipgloss 全局样式定义（颜色、间距、边框）
│       ├── progress.go                   # ProgressReporter：reasonStep 实时进度（spinner + 步骤）
│       ├── result.go                     # ResultBox：最终结果框（带边框的输出区域）
│       ├── error.go                      # ErrorBlock：错误输出（设备路径 + 错误码 + 消息）
│       ├── summary.go                    # SummaryFooter：退出汇总（PID + exit code + tokens + elapsed）
│       ├── trace.go                      # TraceLine：astrace 单行格式化
│       └── table.go                      # ProcessTable：ps 命令表格输出
│
├── lib/agents/code-analyst/
│   ├── agent.yaml                       # Agent 配置：name + description + models + context_budget + skills 引用
│   └── instructions.md                  # Agent 角色定义 + 行为策略
│
├── lib/skills/code-analysis/
│   └── SKILL.md                         # Agent Skills 标准格式：frontmatter（name/description/allowed-tools）+ 程序性知识
│
├── go.mod                                # 模块：github.com/usecrux/crux, go 1.26
├── go.sum
├── Makefile                              # build / test / lint / install 目标
├── .golangci.yml                         # golangci-lint 配置
├── .gitignore
└── README.md
```

## 架构边界

**Kernel ↔ VFS 边界：**

```go
// Kernel 持有 VFS 实例，通过 VFS 接口访问所有设备
type KernelImpl struct {
    vfs       *vfs.VFS
    procTable *SyncMap[PID, *Process]
}

// VFS 不知道 Kernel 的存在
// ProcFS 通过初始化时注入的 ProcessInfoProvider 接口读取进程信息
type ProcessInfoProvider interface {
    GetProcInfo(pid PID) (ProcInfo, error)
    ListProcs() []ProcInfo
}
```

**VFS ↔ Drivers 边界：**

```go
// 驱动实现 VFSFile 接口，通过 DeviceRegistry 注册
type DeviceRegistry struct {
    devices Registry[VFSFileFactory]
}

type VFSFileFactory func() (VFSFile, error)

// 注册在 cmd/crux/main.go 初始化阶段完成（依赖注入）
devRegistry.Register("/dev/llm/claude", claudeDriver.FileFactory())
```

**Kernel ↔ Context 边界：** Context 包独立于 Kernel，通过 CtxID 引用，不持有 Process 指针。

**Kernel ↔ Agents 边界：** Kernel 通过 AgentLoader 获取 AgentInfo（包含 Agent 指令、模型偏好、聚合的设备权限白名单），Kernel 负责注入 CLI 参数。Agents 包不导入 Kernel。

**Agents ↔ Skills 边界：** AgentLoader 依赖 SkillLoader 加载引用的 Skill。AgentLoader 从 agent.yaml 读取 `skills` 列表，调用 SkillLoader 逐一解析 SKILL.md，聚合所有 Skill 的 `allowed-tools` 为统一权限白名单。单向依赖：`agents/ → skills/`。

**Skills 独立性：** SkillLoader 仅负责解析 SKILL.md 格式（frontmatter + body），返回 SkillManifest + instructions 内容。Skills 包不导入 Agents 或 Kernel，可独立使用。

**Debug ↔ Kernel 边界：** Debug 包仅导入 `kernel/` 的类型（SyscallEvent, PID），通过 DebugChan channel 消费事件。

**cmd/ 依赖注入点：** `cmd/crux/main.go` 是唯一组装点，负责创建所有实例、注册设备、连接组件。

## 需求到结构映射

| FR 领域 | FR 范围 | 核心文件 |
|---------|---------|---------|
| 智能体生命周期 | FR1-FR7 | `kernel/{kernel,process,reap}.go` |
| 智能体推理 | FR8-FR12 | `kernel/kernel.go`, `drivers/llm/claude_cli.go` |
| 文件系统与资源 | FR13-FR18 | `vfs/{vfs,proc,dev}.go`, `drivers/shell/shell.go` |
| 上下文管理 | FR19-FR22 | `context/context.go` |
| Agent 管理 | FR23-FR25 | `agents/{types,loader}.go`, `lib/agents/code-analyst/` |
| Skill 管理 | FR25a-FR27 | `skills/{loader,types}.go`, `lib/skills/code-analysis/` |
| 调试与可观测 | FR28-FR32 | `debug/{astrace,event}.go` |
| CLI | FR33-FR37 | `cmd/crux/main.go`, `internal/ui/*` |
| 文档 | FR38-FR40 | `README.md` |

**跨切关注点映射：**

| 关注点 | 定义 → 记录 → 消费 |
|--------|-------------------|
| 错误传播 | `kernel/errors.go` → 所有 syscall → `internal/ui/error.go` |
| SyscallEvent | `debug/event.go` → `kernel/kernel.go` → `debug/astrace.go` |
| 输出格式 | `internal/ui/renderer.go` → `internal/ui/*.go` |
| goroutine 生命周期 | `kernel/process.go` → `kernel/reap.go` |
| 泛型工具 | `internal/xsync/` → 所有使用 Registry/SyncMap/Future 的模块 |

## 数据流

**核心端到端流（`crux "分析代码" --agent=code-analyst`）：**

```
用户输入 → cmd/ 解析意图 + --agent 参数
  → agents/ 加载 Agent（agent.yaml + instructions.md）
    → skills/ 加载引用的 Skill（SKILL.md 渐进式：frontmatter → body）
      → 聚合 Skill allowed-tools → 设备权限白名单
  → kernel/ Spawn 创建 Process（AgentInfo + 聚合权限）
    → context/ 分配上下文
    → 组装 system prompt = Agent instructions + Skill instructions
    → reasonStep 循环:
      → vfs/ Open → dev/ 路由 → drivers/ 执行 → 返回结果
      → context/ 追加结果 → 继续循环或完成
    → reap 回收 → ui/ 格式化输出 → stdout
```

**astrace 数据流：**

```
syscall 入口 → debug/event 构造 SyscallEvent
  → Process.DebugChan → debug/astrace 消费
    → internal/ui/trace 格式化 → stdout 实时流式
```

## 开发工作流

**Makefile 目标：** `build` / `install` / `test`（-race） / `lint` / `vet` / `clean` / `all`

**测试策略：**

| 层次 | 方法 |
|------|------|
| 单元测试 | `*_test.go` 同目录，接口 mock |
| 集成测试 | VFS + DeviceRegistry + mock 驱动 |
| 竞态检测 | `go test -race` 默认开启 |
| 泄漏检测 | Go 1.26 Goroutine Leak Profiler |

**依赖注入（cmd/crux/main.go）：**

```go
func main() {
    // 1. VFS + 设备注册表
    devReg := vfs.NewDeviceRegistry()
    vfsInst := vfs.New(devReg)
    // 2. 驱动注册
    devReg.Register("/dev/llm/claude", llm.NewClaudeCliDriver().FileFactory())
    devReg.Register("/dev/shell", shell.NewDriver().FileFactory())
    devReg.Register("/dev/fs", fs.NewHostFSDriver().FileFactory())
    // 3. Skill + Agent 加载器
    skillLoader := skills.NewLoader("./lib/skills")
    agentLoader := agents.NewLoader("./lib/agents", skillLoader)
    // 4. 内核
    kernel := kernel.New(vfsInst, context.NewManager(), agentLoader)
    // 5. CLI（--agent flag 由 cobra 注册）
    cmd.Execute(kernel)
}
```
