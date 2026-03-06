# Project Context Analysis

## 需求概览

**功能需求（140 个 FR，3 阶段）：**

| 阶段 | FR 范围 | 核心功能域 | 架构含义 |
|------|---------|-----------|---------|
| Phase 1（MVP） | FR1-FR40 | 进程生命周期、推理引擎、VFS、上下文、Agent/Skill、strace、CLI、文档 | 微内核 + VFS + 单进程推理循环 + 调试通道 |
| Phase 2（能力栈） | FR41-FR70 | IPC、Compose、skillpkg、MCP、监控、Supervisor、AgentShell、文档 | 多进程通信 + 编排引擎 + 外部服务集成 + 容错树 |
| Phase 3（涌现） | FR71-FR140 | agdb、时间旅行、分布式追踪、脚本语言、声明式意图、OODA、干细胞分化、Token 经济、免疫安全 | 完整调试工具链 + 自主决策循环 + 涌现层服务 |

**Phase 1 FR 领域细分：**

| 领域 | FR 范围 | 核心架构含义 |
|------|---------|------------|
| 智能体生命周期 | FR1-FR7 | 进程模型是内核骨架——spawn/kill/wait/ps + 孤儿 reparent + zombie 回收，决定 Kernel 结构体核心 API |
| 智能体推理 | FR8-FR12 | reasonStep 循环是系统心跳——LLM 调用→解析 action→工具执行→上下文追加→循环。关键依赖：Claude Code CLI |
| 文件系统与资源 | FR13-FR18 | VFS 是统一抽象层——`/proc/{pid}/`、`/dev/`、`/dev/fs`、`/dev/llm/`、`/dev/shell` 通过同一接口 |
| 上下文管理 | FR19-FR22 | 每个智能体独立上下文空间——分配/读写/组装 prompt/释放 |
| Agent 管理 | FR23-FR25 | agent.yaml + instructions.md 定义智能体身份、模型偏好、Skill 引用，注入 system prompt |
| Skill 管理 | FR25a-FR27 | SKILL.md（Agent Skills 行业标准格式）渐进式加载，allowed-tools 聚合映射为 `/dev/` 权限白名单 |
| 调试与可观测 | FR28-FR32 | strace 差异化核心——实时 syscall 追踪，DebugRecord 数据采集贯穿所有 syscall |
| CLI | FR33-FR37 | 三命令入口（`rnix "意图"` / `rnix strace` / `rnix ps`），go install 单二进制 |
| 文档 | FR38-FR40 | 概念文档 + 快速上手 + 参考手册 |

**非功能需求（46 个 NFR）：**

| 类别 | 关键指标 | 架构驱动因素 |
|------|---------|------------|
| 性能 | spawn ≤ 30s、ps ≤ 100ms、strace ≤ 500ms、VFS 额外延迟 < 10ms | 进程表用 RWMutex SyncMap、DebugChan 缓冲 256 非阻塞写入 |
| 可靠性 | 20 次连续 ≥ 95%、超时 5s 内转 Zombie、无 goroutine 泄漏 | 严格状态机 + reapOnce 幂等回收 + context.Cancel 级联 |
| 集成 | Claude Code CLI 参数 + stream-json | 驱动层封装 CLI 交互，stream-json 为 strace 数据源 |
| 安全 | Skill allowed-tools 白名单、无提权 | 设备权限聚合在 Spawn 时计算 |
| 可维护性 | go vet/golint 零警告、ABI 向后兼容 | 接口组合模式、子接口独立演进 |
| Phase 2 性能 | 10 并发进程 ≤ 2x 延迟、IPC ≤ 50ms、Pipe ≥ 1MB/s | SyncMap 读多写少优化、channel 缓冲策略 |

**规模与复杂度：**

| 维度 | 评估 |
|------|------|
| 项目复杂度 | **高** — 范式级创新，微内核 + VFS + 进程模型 + syscall ABI |
| 主要技术域 | 系统编程（Go 运行时框架 / CLI 工具） |
| MVP 规模 | ~12 核心文件，~15 syscall，3 CLI 命令 |
| 关键外部依赖 | Claude Code CLI（唯一 LLM 通道） |
| 实时特性 | strace 流式输出（stream-json） |
| 多租户 / 合规 | 无（单用户本地运行） |
| 预估架构组件 | ~15 个核心模块（kernel、vfs、drivers/llm、drivers/fs、drivers/shell、context、agents、skills、debug、ipc、cmd/rnix、internal/types、internal/xsync、internal/ui、compose） |

### 技术约束与依赖

**语言与工具链约束：**
- Go 1.26 + 单二进制编译
- 泛型必用场景（Registry、SyncMap、Future 等）vs 禁用场景（Kernel 接口、Process 结构体）
- Claude Code CLI 作为外部 LLM 驱动（非 Go SDK 直接调用）

| 约束 | 来源 | 影响范围 |
|------|------|---------|
| Go 语言 | 产品简报核心决策 | 全局——goroutine=进程, channel=IPC, interface=syscall |
| Claude Code CLI | PRD LLM 驱动策略 | 驱动层——`claude -p` + `--stream-json` 核心调用模式 |
| 单二进制 | Go + 用户体验 | 部署——无外部依赖（除 Claude Code CLI） |
| ABI 向后兼容 | NFR19 | syscall 接口——15 个必须是 45 个的稳定子集 |
| Charm 生态 | UX 设计决策 | CLI 层——cobra + lipgloss + bubbles |

**依赖方向（宪法级约束）：**

```
cmd/ → kernel/ → vfs/ → drivers/{llm,shell,fs}
cmd/ → kernel/ → context/
cmd/ → kernel/ → agents/ → skills/
cmd/ → debug/（仅依赖 internal/types/）
```

绝对禁止：kernel/ ← cmd/、vfs/ ← kernel/、drivers/ ← kernel/、agents/ ← kernel/

**ABI 稳定性约束：**
- Phase 1 的 15 个 syscall 是 Phase 2（~45 个）的稳定子集
- 扩展方式：新增子接口 + KernelImpl 嵌入，不破坏现有接口签名
- Kernel = ProcessManager + ContextManager + FileSystem + Debugger（Phase 1）+ IPCManager + CapManager + ...（Phase 2+）

**外部依赖约束：**
- Claude Code CLI 必须预装（非 Rnix 控制的外部依赖）
- Cobra v1.10.2（CLI 框架）
- Charm 生态（lipgloss + bubbles，TUI 渲染）
- testify（测试断言/mock）

### 跨域关注点

1. **并发安全**：进程表、FD 表、上下文空间、DebugChan、消息队列、信号状态——所有共享可变状态需要 SyncMap/Mutex/Once 保护
2. **资源生命周期管理**：12 步严格释放序列（孤儿处理 → Cancel → wg.Wait → 关闭通道 → 清理信号/线程/协程 → CtxFree → Reap → 移除），顺序不可打乱
3. **错误传播链**：DriverError → VFSError → SyscallError 三层包装，每层携带上下文信息（设备路径、PID、syscall 名称、错误码）
4. **调试可观测性**：所有 syscall 自动记录 SyscallEvent 到 DebugChan，贯穿进程→VFS→驱动全链路
5. **IPC 架构**：Daemon 单例持有内核 + 进程表，CLI 作为客户端通过 Unix domain socket 通信，支持跨终端操作
6. **渐进式加载**：Skill 分两级加载（frontmatter ~100 tokens → full body < 5000 tokens），优化 LLM token 消耗

### UX 架构含义

| UX 决策 | 架构影响 |
|---------|---------|
| Charm 生态（cobra + lipgloss + bubbletea） | Go 依赖 `github.com/charmbracelet/*`，MVP 仅用 cobra + lipgloss |
| 6 个自定义 UI 组件 | `internal/ui/` 包，组件通过 `io.Writer`，支持 TTY/Pipe/JSON |
| 三级输出 + JSON | 输出通过 Renderer 抽象，`TerminalProfile` 启动时检测 |
| 实时流式输出 | strace 事件流（channel → 格式化 → stdout），reasonStep 逐行汇报 |
| 颜色/无色降级 | lipgloss 自动 + `NO_COLOR` / ASCII 显式回退 |
