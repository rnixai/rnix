# Project Structure & Boundaries

## 需求到架构组件的映射

**Phase 1 Epics → 模块映射：**

| Epic | 核心模块 | FR 覆盖 |
|------|---------|---------|
| Epic 1（第一个智能体运行）| `cmd/rnix/`、`kernel/`、`vfs/`、`drivers/llm/`、`context/`、`internal/ui/` | FR1-2,8-11,13,15,17,19-21,32-33,36-37 |
| Epic 2（Agent 能力与文件访问）| `agents/`、`skills/`、`drivers/fs/`、`drivers/shell/`、`lib/` | FR12,16,18,23-27 |
| Epic 3（调试追踪）| `debug/`、`internal/ui/` | FR28-31,34 |
| Epic 4（进程管理与可靠性）| `kernel/`、`vfs/`、`ipc/` | FR3-7,14,22,35 |
| Epic 5（文档体系）| `docs/` | FR38-40 |

**Phase 2 Epics → 模块映射：**

| Epic | 核心模块 | FR 覆盖 |
|------|---------|---------|
| Epic 6（IPC 跨进程通信）| `kernel/`（signal/procgroup/thread/coroutine）、`ipc/` | FR41-45 |
| Epic 7（Compose 编排）| `compose/`、`cmd/rnix/`、`internal/ui/` | FR46-49 |
| Epic 8（Skill 包管理）| `skillpkg/`、`cmd/rnix/` | FR50-53 |
| Epic 9（MCP 服务集成）| `vfs/`、`drivers/mcp/`、`kernel/` | FR54-57 |
| Epic 10（监控 Supervisor）| `kernel/`、`cmd/rnix/`、`context/`、`internal/ui/` | FR58-65 |
| Epic 11（AgentShell 高级语法）| `shell/` | FR66-68 |
| Epic 12（Phase 2 文档）| `docs/` | FR69-70 |

## 完整项目目录结构

```
newxv6/
├── go.mod                          # Go 模块定义 (github.com/rnixai/rnix)
├── go.sum                          # 依赖哈希
├── Makefile                        # 构建脚本 (all/build/test/lint)
├── .golangci.yml                   # golangci-lint 配置
├── .gitignore
├── rnix-init.yaml                  # Daemon init 引导配置（Phase 2）
│
├── cmd/rnix/                       # CLI 主程序 + 依赖注入点
│   ├── main.go                    # 入口 + Cobra root command
│   ├── spawn.go                   # rnix "意图" / rnix spawn 命令
│   ├── ps.go                      # rnix ps 命令
│   ├── kill.go                    # rnix kill 命令
│   ├── strace.go                 # rnix strace <pid> 命令
│   ├── version.go                 # rnix version 命令
│   ├── compose.go                 # rnix compose up/down 命令（Phase 2）
│   ├── compose_test.go
│   ├── skill.go                   # rnix skill install/search/list/update（Phase 2）
│   ├── skill_test.go
│   ├── top.go                     # rnix top 交互式 TUI（Phase 2）
│   ├── log.go                     # rnix log <pid> 分类日志（Phase 2）
│   ├── log_test.go
│   └── daemon.go                  # rnix daemon 内部子命令
│
├── kernel/                         # 微内核（KernelImpl + 子接口组合）
│   ├── kernel.go                  # KernelImpl 主体 + Spawn + reasonStep
│   ├── kernel_test.go
│   ├── process.go                 # Process 结构体 + 状态机
│   ├── process_test.go
│   ├── reap.go                    # 进程回收 + 孤儿 reparent
│   ├── errors.go                  # SyscallError + ErrCode 定义
│   ├── errors_test.go
│   ├── signal.go                  # Signal/SigBlock/SigUnblock（Phase 2）
│   ├── signal_test.go
│   ├── procgroup.go               # ProcGroup 管理（Phase 2）
│   ├── procgroup_test.go
│   ├── thread.go                  # Thread 并发模型（Phase 2）
│   ├── thread_test.go
│   ├── coroutine.go               # Coroutine 协作调度（Phase 2）
│   ├── coroutine_test.go
│   ├── concurrency.go             # 三级并发模型统一接口（Phase 2）
│   ├── supervisor.go              # Supervisor 树 + 重启策略（Phase 2）
│   ├── init.go                    # Init 引导序列（Phase 2）
│   ├── capability.go              # Capability 权限系统（Phase 2）
│   ├── mount_test.go
│   ├── spawn_mcp_test.go
│   ├── log_test.go
│   └── e2e_test.go                # 端到端集成测试
│
├── vfs/                            # 虚拟文件系统
│   ├── vfs.go                     # VFS 接口 + Open/Read/Write/Close/Stat
│   ├── vfs_test.go
│   ├── dev.go                     # DeviceRegistry + /dev/ 路由
│   ├── dev_test.go
│   ├── proc.go                    # /proc/{pid}/ 动态文件系统
│   ├── mount.go                   # Mount/Unmount syscall（Phase 2）
│   ├── mount_test.go
│   ├── mcp.go                     # /mnt/mcp/ MCP 挂载（Phase 2）
│   └── mcp_test.go
│
├── drivers/                        # 设备驱动层
│   ├── llm/                       # LLM 设备驱动
│   │   ├── claude.go             # Claude Code CLI 驱动（exec.CommandContext）
│   │   ├── vfsfile.go            # LLM VFS 文件实现
│   │   ├── vfsfile_test.go
│   │   ├── registry.go           # LLM 驱动注册表
│   │   └── registry_test.go
│   ├── fs/                        # 宿主文件系统驱动
│   │   ├── hostfs.go             # /dev/fs 实现
│   │   └── hostfs_test.go
│   ├── shell/                     # Shell 执行驱动
│   │   ├── shell.go              # /dev/shell 实现
│   │   └── shell_test.go
│   └── mcp/                       # MCP 服务驱动（Phase 2）
│       ├── config.go              # MCP 配置解析
│       ├── config_test.go
│       ├── transport.go           # MCP 传输层
│       └── transport_test.go
│
├── context/                        # 上下文管理
│   ├── context.go                 # Manager + ctx_alloc/read/write/free + prompt 组装
│   ├── context_test.go
│   └── budget.go                  # Token 预算管理（Phase 2）
│
├── agents/                         # Agent 加载器
│   ├── loader.go                  # agent.yaml + instructions.md → AgentInfo
│   ├── loader_test.go
│   └── types.go                   # AgentInfo 类型定义
│
├── skills/                         # Skill 加载器
│   ├── loader.go                  # SKILL.md 渐进式解析
│   ├── loader_test.go
│   └── types.go                   # SkillInfo 类型定义
│
├── debug/                          # 调试工具
│   ├── event.go                   # SyscallEvent + EmitEvent
│   ├── event_test.go
│   ├── strace.go                 # strace 追踪逻辑
│   └── strace_test.go
│
├── ipc/                            # IPC Daemon + Client + Protocol
│   ├── server.go                  # IPC 服务器（Unix domain socket）
│   ├── server_test.go
│   ├── client.go                  # IPC 客户端（连接复用）
│   ├── client_test.go
│   ├── daemon.go                  # Daemon 生命周期管理
│   ├── daemon_test.go
│   ├── protocol.go                # JSON-RPC 通信协议
│   ├── log_test.go
│   └── integration_test.go        # IPC 集成测试
│
├── compose/                        # Compose 引擎（Phase 2）
│   ├── engine.go                  # 编排引擎主体
│   ├── engine_test.go
│   ├── parser.go                  # rnix-compose.yaml 解析
│   ├── parser_test.go
│   ├── dag.go                     # DAG 拓扑排序 + Kahn 算法
│   └── dag_test.go
│
├── skillpkg/                       # Skill 包管理客户端（Phase 2）
│   ├── client.go                  # Git-based 仓库交互
│   ├── client_test.go
│   ├── registry.go                # 本地注册表管理
│   ├── registry_test.go
│   ├── installer_test.go
│   ├── update_test.go
│   ├── list_test.go
│   └── types.go                   # 包管理类型定义
│
├── shell/                          # AgentShell 解析器（Phase 2）
│   ├── lexer.go                   # 词法分析器
│   ├── parser.go                  # 递归下降语法分析器
│   ├── ast.go                     # AST 节点定义
│   ├── interpreter.go             # AST walker 解释执行
│   ├── pipe.go                    # 管道语法执行
│   └── script.go                  # 变量/环境/多行脚本
│
├── internal/                       # 内部工具库
│   ├── types/                     # 共享类型定义
│   │   ├── types.go              # PID、FD、ErrCode、Signal 等
│   │   ├── types_test.go
│   │   └── log_test.go
│   ├── xsync/                     # 泛型并发工具
│   │   ├── syncmap.go            # SyncMap[K,V]
│   │   ├── syncmap_test.go
│   │   ├── registry.go           # Registry[T]
│   │   ├── registry_test.go
│   │   ├── future.go             # Future[T]
│   │   └── future_test.go
│   └── ui/                        # 终端 UI 组件
│       ├── styles.go              # lipgloss 样式定义
│       ├── styles_test.go
│       ├── renderer.go            # 输出渲染器
│       ├── renderer_test.go
│       ├── result.go              # 结果展示
│       ├── result_test.go
│       ├── error.go               # 错误展示
│       ├── error_test.go
│       ├── summary.go             # 汇总展示
│       ├── summary_test.go
│       ├── progress.go            # 进度展示
│       ├── progress_test.go
│       ├── trace.go               # strace 行渲染
│       ├── trace_test.go
│       ├── table.go               # 表格渲染（ps 等）
│       ├── table_test.go
│       ├── compose.go             # Compose 状态渲染（Phase 2）
│       └── compose_test.go
│
├── lib/                            # 内置 Agent 和 Skill
│   ├── agents/
│   │   └── code-analyst/          # 参考 Agent
│   │       ├── agent.yaml         # Agent 元信息
│   │       └── instructions.md    # Agent 角色定义
│   └── skills/
│       └── code-analysis/         # 参考 Skill
│           └── SKILL.md           # Skill 定义（Agent Skills 标准）
│
├── docs/                           # 项目文档
│   ├── concepts.md                # 核心概念文档（FR38）
│   ├── quick-start.md             # 快速上手指南（FR39）
│   ├── reference.md               # 参考手册（FR40）
│   ├── architecture.md            # 架构文档（FR70）
│   ├── monitoring.md              # 监控指南
│   └── tutorials/                 # 教程文档（Phase 2，FR69）
│       ├── write-first-skill.md
│       ├── debug-first-bug.md
│       └── multi-agent-workflow.md
│
└── _bmad-output/                   # BMAD 系统生成的规划文档（不参与编译）
    └── planning-artifacts/
```

## 架构边界

**Syscall 接口边界（ABI 契约）：**

```
用户空间（cmd/rnix/）
        ↓ IPC protocol (JSON-RPC over Unix socket)
内核空间（kernel/ KernelImpl）
        ↓ 子接口方法调用
   ┌────┼────┬────┬────┬────┐
  VFS  Ctx  Dbg  IPC  Sig  ProcGrp
   ↓
DeviceRegistry（前缀路由）
   ↓
drivers/（llm、fs、shell、mcp）
```

**模块间边界详述：**

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

// 注册在 cmd/rnix/main.go 初始化阶段完成（依赖注入）
devRegistry.Register("/dev/llm/claude", claudeDriver.FileFactory())
```

**其他边界：**
- **cmd/ → kernel/**：通过 IPC client/server 通信，CLI 不直接持有内核引用
- **kernel/ → vfs/**：内核通过 VFS 接口访问所有资源，VFS 是唯一的资源抽象层
- **vfs/ → drivers/**：VFS 通过 DeviceRegistry 前缀匹配路由到具体驱动
- **kernel/ → context/**：内核通过 ContextManager 管理进程上下文。Context 包独立于 Kernel，通过 CtxID 引用，不持有 Process 指针
- **kernel/ → agents/ + skills/**：Spawn 时加载 Agent/Skill 定义，注入上下文。Kernel 通过 AgentLoader 获取 AgentInfo，Agents 包不导入 Kernel
- **agents/ → skills/**：AgentLoader 依赖 SkillLoader 加载引用的 Skill，聚合所有 Skill 的 `allowed-tools` 为统一权限白名单。单向依赖
- **Skills 独立性：** SkillLoader 仅负责解析 SKILL.md 格式，不导入 Agents 或 Kernel，可独立使用
- **Debug ↔ Kernel：** Debug 包仅导入 `kernel/` 的类型（SyscallEvent, PID），通过 DebugChan channel 消费事件

**绝对禁止的依赖方向：**
- `kernel/` ← `cmd/`（内核不依赖 CLI）
- `vfs/` ← `kernel/`（VFS 不依赖内核）
- `drivers/` ← `kernel/`（驱动不依赖内核）
- `agents/` ← `kernel/`（加载器不依赖内核）
- `internal/` ← 任何上层包

**模块间通信模式：**

| 通信对 | 模式 | 说明 |
|--------|------|------|
| CLI ↔ Daemon | IPC（Unix domain socket + JSON-RPC） | 所有运行时操作通过 IPC |
| Kernel → VFS | 同步方法调用 | VFS.Open/Read/Write/Close |
| VFS → Driver | 同步方法调用 | DeviceFile 接口 |
| Kernel → Debug | 非阻塞 channel 写入 | DebugChan 缓冲 256 |
| Process → Process | IPC Send/Recv（Phase 2） | 消息队列 |
| Compose → Kernel | Spawn + ProcGroup API | DAG 调度触发 |

**cmd/ 依赖注入点：** `cmd/rnix/main.go` 是唯一组装点，负责创建所有实例、注册设备、连接组件。

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

## 跨切面关注点映射

| 关注点 | 涉及模块 | 实现位置 |
|--------|---------|---------|
| 并发安全 | kernel/、vfs/、ipc/、debug/ | `internal/xsync/`（SyncMap、Registry） |
| 错误传播 | drivers/ → vfs/ → kernel/ → cmd/ | 各层 `errors.go` + `errors.Unwrap` |
| 资源生命周期 | kernel/、context/、vfs/ | `kernel/reap.go`（12 步释放序列） |
| 调试可观测 | 所有 syscall 路径 | `debug/event.go`（EmitEvent） |
| 终端 UI | cmd/ | `internal/ui/`（统一样式组件） |
| 类型共享 | 所有模块 | `internal/types/`（PID、FD、ErrCode） |

## 数据流

**核心端到端流（`rnix "分析代码" --agent=code-analyst`）：**

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

**strace 数据流：**

```
syscall 入口 → debug/event 构造 SyscallEvent
  → Process.DebugChan → debug/strace 消费
    → internal/ui/trace 格式化 → stdout 实时流式
```

## 测试组织

**测试策略：**
- 单元测试：`*_test.go` 与源文件同目录（Go 标准惯例）
- 集成测试：`ipc/integration_test.go`、`kernel/e2e_test.go`
- 竞态检测：所有测试默认 `-race`
- Mock 策略：接口抽象（LLMDriver、VFSFile、ProcessInfoProvider）

**测试覆盖分布：**

| 模块 | 测试文件数 | 测试重点 |
|------|-----------|---------|
| kernel/ | 8 | 状态机、并发安全、资源释放 |
| ipc/ | 4 + integration | 协议解析、连接复用、daemon 生命周期 |
| vfs/ | 4 | 设备注册、路由、挂载 |
| drivers/ | 6 | 驱动行为、超时、错误处理 |
| internal/ | 8 | 泛型工具、UI 渲染 |

## 开发工作流

**构建流程：**
```
make all = make lint → make vet → make test → make build
                ↓           ↓          ↓           ↓
         golangci-lint   go vet    go test -race   go build -o rnix ./cmd/rnix/
```

**部署结构：**
- 单二进制 `rnix`，`go install github.com/rnixai/rnix/cmd/rnix@latest`
- 运行时自动 fork daemon 进程（`rnix daemon` 子命令）
- Daemon 通过 `Setsid` 独立会话运行，60 秒空闲自动退出
