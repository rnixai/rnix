# Rnix

**AI 智能体操作系统 — 用 Unix 哲学驱动智能体**

[English](README.md) | [文档站点](https://rnix.ai/docs) | [GitHub](https://github.com/rnixai/rnix)

---

Rnix 将 Unix 操作系统的核心抽象引入 AI 智能体领域。每一次智能体执行都是一个**进程**，拥有独立的 PID 和状态机；每一个外部资源（LLM、文件系统、Shell）都是一个**文件**，通过虚拟文件系统统一访问；每一次与内核的交互都是一次**系统调用**。如果你熟悉 Unix，你已经理解了 Rnix。

## 核心特性

- **一切皆进程** — 每次智能体执行拥有独立的 PID、状态机、FD 表、线程和协程。IPC 消息、管道、信号和进程组实现多智能体协作。

- **一切皆文件** — LLM、文件系统、Shell、MCP 工具统一为 VFS 设备。多 Provider LLM 支持，`rnix serve` 提供 OpenAI 兼容网关。

- **自主智能体** — OODA 推理循环实现自主决策。干细胞分化让通用智能体根据意图自动特化。声明式意图系统配合 Reconciler 驱动目标执行。

- **深度调试工具链** — strace、GDB 风格交互式调试器（attach/断点/单步/检查）、时间旅行回放与 fork-continue、分布式因果追踪、可视化 TUI 面板、agtest 回归测试。

- **Compose 与 AgentShell** — YAML DAG 编排（含预算池和 SLA 合约）。完整脚本语言：管道、变量、if/else、循环、函数、并行块和模块导入。

- **Token 经济与安全** — 预算池优先级分配、合约 SLA 评估、智能体声誉系统、Skill 协同涌现。适应性免疫安全，异常检测与自愈。

## 架构

```
CLI (cmd/rnix) → IPC Client → Unix Socket → IPC Server → Kernel
                                                            │
                                              Process ReasonStep Loop
                                                  │           ▲
                                             VFS Devices    Context
                                            (LLM, FS, Shell, MCP)
```

Rnix 运行为后台 **daemon** 进程，持有内核和进程表。CLI 命令通过 Unix domain socket 通信。daemon 在首次使用时自动启动，空闲 60 秒后自动退出。

## 安装

### 前置条件

- **Go 1.26+** — [下载](https://go.dev/dl/)
- **至少一个 LLM 提供商** — Claude Code CLI (`npm install -g @anthropic-ai/claude-code`)、Cursor CLI，或任意 OpenAI 兼容 API（Ollama、Groq、DeepSeek）通过 `~/.config/rnix/providers.yaml` 配置

### 安装

```bash
go install github.com/rnixai/rnix/cmd/rnix@latest
```

验证：

```bash
$ rnix version
rnix v0.1.0
commit:  cd9c568
built:   2026-03-15T07:23:57Z
```

### 从源码构建

```bash
git clone https://github.com/rnixai/rnix.git
cd rnix
make build    # → ./rnix
make all      # lint + vet + test + build
```

## 快速上手

### 运行第一个智能体

```bash
$ rnix -i "分析 ./README.md"
[kernel] spawning PID 1 (claude/haiku)...
[agent/1] reasoning step 1...
[agent/1] reasoning step 2...
══ Result ══════════════════════════════════════════════════════════════════════
  ## README.md 分析
  ...
════════════════════════════════════════════════════════════════════════════════
[kernel] PID 1 exited(0) | claude/haiku | tokens: 1024 | elapsed: 5.3s
```

### 使用命名 Agent

```bash
$ rnix -i "分析 ./cmd/rnix/main.go" --agent=code-analyst
```

### 追踪系统调用

```bash
# 终端 A：运行智能体
$ rnix -i "分析当前项目结构"

# 终端 B：attach strace 到 PID 1
$ rnix strace 1
[strace] attached to PID 1 (state: running)
[  0.013s] Open(path="/dev/llm/claude") → 3    1ms
[  0.014s] Write(fd=3, size=1234) → <nil>    5.20s  ← LLM 调用
[  5.214s] Read(fd=3, length=1048576) → 892    2ms
...
```

### 进程管理

```bash
$ rnix ps                    # 列出所有进程
$ rnix kill 1                # 向 PID 1 发送 SIGTERM
$ rnix top                   # 实时 TUI 进程监控
$ rnix log 1                 # 查看推理日志
$ rnix daemon status         # 查看 daemon 状态
$ rnix daemon stop           # 停止 daemon
```

### 交互式调试 (gdb)

```bash
$ rnix gdb 1
(rnix-gdb) break syscall Write
(rnix-gdb) continue
(rnix-gdb) inspect context
(rnix-gdb) step reason
(rnix-gdb) detach
```

### 多智能体编排

```bash
$ rnix compose up            # 运行 rnix-compose.yaml 工作流
$ rnix -i 'spawn "分析代码" | spawn "生成文档"'  # 管道语法
$ rnix intent apply "重构认证模块为 JWT"          # 声明式意图
```

### LLM 网关服务

```bash
$ rnix serve                 # OpenAI 兼容 API，监听 localhost:8080
$ curl localhost:8080/v1/chat/completions -d '{"model":"claude","messages":[...]}'
```

## CLI 参考

| 命令 | 说明 |
|------|------|
| `rnix -i "意图"` | 以给定意图 Spawn 智能体进程 |
| `rnix -i "意图" --agent=NAME` | 使用命名 Agent 定义 |
| `rnix -i "意图" --provider=cursor` | 使用指定 LLM 提供商 |
| `rnix ps` | 列出所有进程 |
| `rnix kill <pid>` | 终止进程 |
| `rnix strace <pid>` | 追踪进程的系统调用 |
| `rnix gdb <pid>` | 交互式调试器（断点/单步/检查） |
| `rnix record <pid>` | 录制执行轨迹 |
| `rnix replay <id>` | 回放录制的执行 |
| `rnix trace <id>` | 查看分布式追踪 |
| `rnix trace blame <id>` | 根因分析 |
| `rnix ctx-profile <pid>` | 上下文内存分析 |
| `rnix agtest <file>` | 运行回归测试 |
| `rnix dashboard` | 可视化调试面板（多窗格 TUI） |
| `rnix top` | 实时进程监控（TUI） |
| `rnix log <pid>` | 分类推理日志 |
| `rnix compose up/down` | 多智能体 DAG 工作流 |
| `rnix intent apply/status` | 声明式意图分解 |
| `rnix serve` | OpenAI 兼容 LLM 网关 |
| `rnix skill install/search/update/list` | Skill 包管理 |
| `rnix providers status` | LLM 提供商健康检查 |
| `rnix reputation [agent]` | 智能体声誉评分 |
| `rnix lineage <pid>` | 干细胞分化路径 |
| `rnix topology` | 智能体协作拓扑 |
| `rnix synergy list` | 有效 Skill 组合 |
| `rnix immune status` | 安全监控状态 |
| `rnix run <script.ash>` | 执行 AgentShell 脚本 |
| `rnix daemon status/stop` | Daemon 管理 |
| `rnix version` | 显示版本信息 |

**全局标志：** `--json`、`--verbose` (`-v`)、`--quiet` (`-q`)

## VFS 设备路径

| 路径 | 用途 |
|------|------|
| `/dev/llm/<provider>` | LLM 推理（claude、cursor、ollama、groq、deepseek 等） |
| `/dev/fs` | 宿主文件系统访问 |
| `/dev/shell` | Shell 命令执行 |
| `/dev/mcp/*` | MCP 工具服务器（按进程自动挂载） |
| `/proc/{pid}/status` | 进程状态（JSON） |
| `/proc/{pid}/intent` | 进程意图（纯文本） |
| `/mnt/mcp/{pid}-{server}` | MCP 挂载点（自动生命周期） |

## 项目结构

```
cmd/rnix/          ← CLI 入口（Cobra）
├── kernel/        ← 微内核：进程表、spawn、kill、wait、reaper
│   ├── vfs/       ← VFS 文件抽象、设备注册表、FD 表
│   ├── context/   ← 进程上下文（对话历史）
│   └── debug/     ← strace、录制、分布式追踪
├── drivers/       ← VFS 设备驱动实现
│   ├── llm/       ← /dev/llm/*（claude、cursor、ollama、groq 等）
│   ├── fs/        ← /dev/fs — 沙箱化宿主文件系统
│   ├── shell/     ← /dev/shell — 子进程执行
│   └── mcp/       ← /dev/mcp/* — MCP 服务器传输
├── ipc/           ← 客户端/服务端，NDJSON over Unix socket
├── intent/        ← 声明式意图分解与协调
├── compose/       ← 基于 DAG 的多智能体编排
├── shell/         ← AgentShell 脚本语言
├── agents/        ← Agent 加载器 (lib/agents/)
├── skills/        ← Skill 加载器 (lib/skills/)
├── skillpkg/      ← Skill 包管理
└── internal/      ← 共享工具库 (types, xsync, ui)
```

## 配置文件

| 文件 | 用途 |
|------|------|
| `rnix-providers.yaml` | LLM 提供商定义（驱动、模型、API 密钥） |
| `rnix-init.yaml` | 引导服务和 Supervisor 树 |
| `rnix-compose.yaml` | 多智能体工作流 DAG |
| `lib/agents/*/agent.yaml` | Agent 清单 |
| `lib/skills/*/SKILL.md` | Skill 定义（YAML frontmatter + Markdown） |

## Agent 与 Skill 模型

**Agent** = "我是谁" — 身份、模型偏好、Skill 引用

```yaml
# lib/agents/code-analyst/agent.yaml
name: code-analyst
description: "代码质量分析智能体"
models:
  provider: claude
  preferred: sonnet
context_budget: 8192
skills:
  - code-analysis
```

**Skill** = "如何做 X" — 程序性知识、工具权限

```markdown
# lib/skills/code-analysis/SKILL.md
---
name: code-analysis
description: 分析代码质量并识别问题。
allowed-tools: /dev/fs /dev/shell
---
# Code Analysis
## 工作流程
1. 通过 /dev/fs 读取源代码文件
2. 通过 /dev/shell 运行 linter
3. 生成报告
```

## 开发

```bash
make build          # 构建二进制文件 → ./rnix
make test           # 运行所有测试（带竞态检测）
make lint           # golangci-lint
make vet            # go vet
make all            # lint + vet + test + build
```

运行单个测试：

```bash
go test -race -run TestFunctionName ./package/...
```

## 贡献

欢迎贡献！请确保：

1. 所有测试通过：`make test`
2. Linter 无报错：`make lint`
3. 代码遵循现有规范（详见 `CLAUDE.md`）

## 许可证

MIT License — 详见 [LICENSE](LICENSE)
