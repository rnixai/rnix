# Rnix — Agent OS 技术架构规范

**产品名称：** Rnix
**版本：** 0.1.0
**日期：** 2026-02-23
**状态：** 草稿（来源：头脑风暴 + 多智能体深度探讨）
**作者：** Decker

> **Rnix** /krʌks/ — 十字星座，南半球导航基准点；亦指"问题的关键"。
> 现有多智能体框架都在绕圈子，Rnix 直指核心——智能体需要的不是编排框架，而是操作系统。

---

## 一、核心定位

**Rnix 不是"给智能体加一个操作系统"，而是"把智能体当作操作系统的一等计算单元"。**

所有现有多智能体框架（LangGraph、AutoGen、CrewAI、MetaGPT）本质上都在应用层重新发明操作系统的功能。Rnix 的范式突破在于：**不在应用层做编排，而是在 OS 层提供智能体作为一等计算单元的完整原语支持。**

### 核心决策

| 决策项 | 结论 | 理由 |
|--------|------|------|
| 实现语言 | **Go** | goroutine = 进程，channel = IPC，接口 = syscall 契约，单二进制部署 |
| 架构路线 | **Gamma 混合** | 底层 Alpha（可靠微内核）+ 上层 Beta（涌现层）|
| 用户层叠 | **A → B → C** | 平台工程师先用，开发者跟进，最终用户是 Phase 3 |
| Phase 1 目标 | **自举验证** | Agent OS 用自身 syscall 层完成一个 Agent OS 开发任务 |
| Phase 1 用户 | **构建者自己** | 先让自己能用，再让别人能用 |

---

## 二、总体架构（Gamma 路线）

```
┌─────────────────────────────────────────────────────────────────┐
│                        用户/应用层                               │
│  AgentShell │ Agent Compose │ API                               │
│  声明式意图 → 控制器调和 → 交付结果                              │
├─────────────────────────────────────────────────────────────────┤
│                        涌现服务层（Phase 3）                     │
│  干细胞分化 │ 适应性免疫 │ Token 经济 │ OODA 自主 │ 声誉进化    │
├─────────────────────────────────────────────────────────────────┤
│                     Skills 层 (/lib/skills/)                    │
│  skillpkg 包管理 │ 动态加载 │ 版本管理 │ 依赖解析 │ Synergy 涌现 │
├─────────────────────────────────────────────────────────────────┤
│                     系统服务层（用户态）                          │
│  Compose Controller │ Reconciler │ 监控 │ 日志 │ Supervisor 树  │
├─────────────────────────────────────────────────────────────────┤
│          MCP 层 (/mnt/mcp/)    │    Tools 层 (/dev/)            │
│  GitHub │ Slack │ DB │ ...    │  shell │ browser │ fs │ llm    │
├─────────────────────────────────────────────────────────────────┤
│                     调试与可观测性层                              │
│  agdb │ astrace │ 时间旅行 │ 分布式追踪 │ ctx-profiler          │
├─────────────────────────────────────────────────────────────────┤
│                  系统调用接口（~45 个 syscall）                   │
│  进程: spawn/kill/wait/clone/signal/getpid/ps/nice              │
│  上下文: ctx_alloc/read/write/free/share/swap/snapshot          │
│  文件: open/read/write/close/stat/mount/umount/ioctl            │
│  通信: send/recv/pub/sub/unsub/pipe                             │
│  权限: cap_grant/revoke/check/elevate/list                      │
│  技能: skill_load/invoke_async/unload/query/deps                │
│  调试: debug_attach/detach/break/step/record/replay             │
├─────────────────────────────────────────────────────────────────┤
│                        微内核                                    │
│  调度器 │ IPC │ Capability │ 虚拟上下文 │ VFS │ 事件循环        │
│  中断处理 │ 故障隔离 │ OOM Killer │ init (PID=1)                │
├─────────────────────────────────────────────────────────────────┤
│                     硬件抽象层（驱动）                            │
│  LLM 驱动 │ 工具驱动 │ MCP 驱动 │ FS 驱动 │ 网络              │
└─────────────────────────────────────────────────────────────────┘
```

---

## 三、系统调用层（ABI）

### 设计原则

- 严格控制在 **45 个以内**，分 7 个类别
- 所有 I/O 类 syscall 返回 `Future[T]`，调用方决定是否 `await`
- LLM 通过 VFS 的 `/dev/llm/` 设备访问，**不是** syscall
- 接口优先于实现：MVP 内部可硬编码，但调用形式必须遵循此 ABI

### Kernel 接口（Go）

```go
type Kernel interface {
    // 进程管理（8个）
    Spawn(intent string, skills []string, opts SpawnOpts) (PID, error)
    Kill(pid PID, signal Signal) error
    Wait(pid PID) (ExitStatus, error)
    Clone(pid PID, opts CloneOpts) (PID, error)
    Signal(pid PID, sig Signal, handler Handler) error
    GetPID() PID
    PS(filter PSFilter) ([]ProcInfo, error)
    Nice(pid PID, priority int) error

    // 上下文管理（7个）
    CtxAlloc(size int) (CtxID, error)
    CtxRead(cid CtxID, offset, length int) ([]byte, error)
    CtxWrite(cid CtxID, offset int, data []byte) error
    CtxFree(cid CtxID) error
    CtxShare(cid CtxID, pid PID, perm Permission) error
    CtxSwap(cid CtxID) error
    CtxSnapshot(cid CtxID) (SnapID, error)

    // 文件系统（8个）
    Open(path string, flags int) (FD, error)
    Read(fd FD, length int) ([]byte, error)
    Write(fd FD, data []byte) error
    Close(fd FD) error
    Stat(path string) (FileStat, error)
    Mount(src, dst, fstype string, opts MountOpts) error
    Umount(path string) error
    Ioctl(fd FD, cmd int, args any) (any, error)

    // 进程间通信（6个）
    Send(pid PID, msg Message) error
    Recv(timeout time.Duration) (Message, error)
    Pub(topic string, msg Message) error
    Sub(topic string, handler MsgHandler) (SubID, error)
    Unsub(sid SubID) error
    Pipe(pidA, pidB PID) error

    // 权限管理（5个）
    CapGrant(pid PID, cap Capability) error
    CapRevoke(pid PID, cap Capability) error
    CapCheck(cap Capability) bool
    CapElevate(cap Capability, reason string) (Token, error)
    CapList(pid PID) ([]Capability, error)

    // Skill 管理（5个）
    SkillLoad(name, version string) (SkillID, error)
    SkillInvokeAsync(sid SkillID, args any) (FutureID, error)
    SkillUnload(sid SkillID) error
    SkillQuery(filter string) ([]SkillInfo, error)
    SkillDeps(sid SkillID) ([]Dependency, error)

    // 调试（6个）
    DebugAttach(pid PID) error
    DebugDetach(pid PID) error
    DebugBreak(pid PID, condition Condition) error
    DebugStep(pid PID) (Frame, error)
    DebugRecord(pid PID) (RecID, error)
    DebugReplay(rid RecID, opts ReplayOpts) error
}
```

### SpawnOpts

```go
type SpawnOpts struct {
    ParentPID    PID
    ContextSize  int       // token 预算
    InitialCtx   []Message // 初始上下文（来自 inputs 或 pipe）
    Model        string    // 覆盖默认模型
    ReconcilerID string    // Phase 3: 声明式意图控制器 ID
}
```

---

## 四、进程模型

### 三级模型（Go 实现）

```
Agent OS 概念         Go 实现
────────────────────────────────────────────
进程 (Process)   →   goroutine + 独立 Context + 独立 SkillSet
线程 (Thread)    →   goroutine + 共享父进程 Context（CoW）
协程 (Coroutine) →   goroutine + 进程内部 channel 调度
```

差异**仅在 Context 隔离程度**，底层均为 goroutine。

### 进程数据结构

```go
type Process struct {
    PID      PID
    PPID     PID
    State    ProcessState    // Created / Running / Sleeping / Zombie / Dead
    Intent   string          // 原始意图（不可变）
    Skills   []SkillID
    Ctx      *Context
    Caps     CapabilitySet
    Children []PID
    Inbox    chan Message
    Signal   chan Signal
    Done     chan ExitStatus
    Budget   TokenBudget     // Phase 3 Token 经济
}
```

### 进程生命周期

```
spawn()
   │
[Created] ──────────────────────────────────────┐
   │                                             │
[Running] ←── recv() / skill_invoke() ──→ [Sleeping]
   │
kill(SIGTERM)
   │
[Zombie] ── wait() by parent ──→ [Dead]
   │
无人 wait()（孤儿）→ reparent to init (PID=1)
```

### reasonStep（心跳）

```go
func (k *Kernel) reasonStep(proc *Process) (done bool, err error) {
    prompt   := proc.Ctx.BuildPrompt()
    fd, _    := k.vfs.Open("/dev/llm/claude-sonnet", O_RDWR)
    k.vfs.Write(fd, prompt)
    response := k.vfs.Read(fd, MaxTokens)
    action   := k.parseAction(response)  // 解析 XML action 标签

    switch action.Type {
    case ActionText:
        proc.Ctx.Append(response)
        return true, nil
    case ActionToolCall:
        result := k.execTool(proc, action)
        proc.Ctx.Append(result)
        return false, nil
    case ActionSpawn:
        k.Spawn(action.Intent, action.Skills, SpawnOpts{ParentPID: proc.PID})
        return false, nil
    }
    return false, nil
}
```

### LLM Action 格式（XML）

```xml
<!-- 工具调用 -->
<action type="tool_call">
  <tool>/dev/fs</tool>
  <args>{"path": "kernel/scheduler.go", "op": "read"}</args>
</action>

<!-- 最终输出 -->
<action type="text">
  分析结果...
</action>

<!-- 创建子智能体 -->
<action type="spawn">
  <intent>实现 JWT 认证</intent>
  <skills>["go-dev"]</skills>
</action>
```

---

## 五、VFS 目录结构

```
/
├── proc/                         # 进程信息（内核动态生成）
│   ├── {pid}/
│   │   ├── status                # {"pid":42,"state":"running","tokens_used":1240}
│   │   ├── intent                # 原始意图（只读）
│   │   ├── context               # 完整上下文对话历史（纯文本）
│   │   ├── skills/{name}         # 已加载 Skill 状态
│   │   ├── caps                  # Capability 集合
│   │   ├── fd/                   # 打开的文件描述符
│   │   ├── children              # 子进程 PID 列表
│   │   └── budget                # token 预算状态
│   └── self → /proc/{current}   # 当前进程软链接
│
├── dev/                          # 设备驱动（Tools 层）
│   ├── llm/
│   │   ├── claude-sonnet         # Claude Sonnet 驱动
│   │   └── claude-haiku          # Claude Haiku 驱动（低成本）
│   ├── shell                     # Shell 执行设备
│   ├── browser                   # 浏览器控制设备
│   ├── fs                        # 宿主文件系统访问
│   ├── http                      # HTTP 客户端设备
│   └── null                      # 空设备
│
├── mnt/mcp/                      # MCP 服务挂载点
│   ├── github/repos/{owner}/{repo}/files|issues|prs
│   ├── slack/channels/{name}/messages
│   └── postgres/query|schema/
│
├── lib/skills/                   # Skill 共享库
│   └── {skill-name}/
│       ├── manifest.yaml
│       ├── instructions.md
│       └── examples/
│
├── etc/                          # 系统配置
│   ├── agents.conf
│   ├── skills.conf
│   ├── mcp.conf
│   ├── limits.conf
│   ├── security/caps.policy
│   └── init.d/
│
├── var/
│   ├── log/kernel.log|agents/{pid}.log|syscall.log
│   ├── cache/ctx/{ctx-id}.ctx    # 上下文冷存储（swap）
│   └── run/{pid}.sock
│
└── tmp/                          # 临时文件（进程退出自动清理）
```

---

## 六、三层能力栈

### 边界定义

```
层级       本质          回答的问题        挂载位置      Unix 类比
──────────────────────────────────────────────────────────────
Tools      原子能力      "能做什么？"      /dev/         设备驱动
MCP        外部状态      "连接了什么？"    /mnt/mcp/     NFS/FUSE
Skills     领域知识      "知道怎么做？"    /lib/skills/  共享库(.so)
```

### 归属决策树

```
这个能力是否访问外部服务（有自己的状态和API）?
  → 是：MCP 层（挂载到 /mnt/mcp/）
  → 否：是否包含"如何完成任务"的领域知识?
        → 是：Skills 层（安装到 /lib/skills/）
        → 否：Tools 层（注册到 /dev/）
```

### 单向依赖约束

```
Skills  → 描述如何使用 Tools 和 MCP（向下调用）
MCP     → 内部可使用 Tools（向下调用）
Tools   → 不知道 Skills 和 MCP 的存在
```

### 与现有框架对比

```
框架         能力组织方式         问题
────────────────────────────────────────────────────
LangChain    扁平 tool 列表       无层次，组合靠硬编码
AutoGen      agent 自带所有工具   角色和能力耦合
CrewAI       角色内嵌工具         无复用，换任务要重定义
Agent OS     三层分离             各层独立演化，Skills 跨任务复用
```

---

## 七、Skill 格式规范

### 目录结构

```
/lib/skills/{skill-name}/
├── manifest.yaml        # 元信息（内核读）
├── instructions.md      # 核心指令（注入 system prompt）
└── examples/            # few-shot 示例（可选）
    └── {n}-{name}/
        ├── input.md
        └── output.md
```

### manifest.yaml 完整规范

```yaml
name: code-analyst
version: 0.2.1
description: 分析代码质量、性能瓶颈、安全漏洞
author: decker
license: MIT
tags: [code, analysis, performance, security]

tools:
  - /dev/fs
  - /dev/shell

caps:
  - read:/dev/fs
  - exec:/dev/shell

models:
  preferred: claude-sonnet
  fallback:  claude-haiku

context_budget:
  soft: 4096    # 超出时发出警告
  hard: 8192    # 超出时拒绝加载

dependencies:
  skills: []

synergy:
  - skill: python-dev
    description: "同时加载时可生成修复建议并直接执行"
  - skill: tech-writer
    description: "同时加载时可将分析结果直接转成技术文档"

load:
  inject:         system_prompt
  priority:       10      # 越小越优先注入
  merge_strategy: append  # append / replace / section
```

### Synergy 涌现机制

多个声明了互相 `synergy` 的 Skill 同时加载时，内核自动注入协同指令。能力超越单 Skill 叠加，产生涌现效果。**驱动 Skill 生态网络效应。**

---

## 八、声明式意图机制

### 两层自治

```
宏观（控制器 Reconciler）：
  期望："Go HTTP 服务有 JWT 认证"
  观察：代码库里没有 JWT 相关文件
  决策：spawn go-dev 智能体
  行动：kernel.Spawn("实现 JWT 认证", ["go-dev"])

微观（智能体内部 OODA 循环）：
  Observe → Orient → Decide → Act → 循环
```

### 调和循环

```go
func (r *Reconciler) Run(intent string) error {
    desired := r.parseIntent(intent)
    for {
        actual := r.observe()
        if r.satisfied(desired, actual) { return nil }
        if r.deadlocked(actual)         { return r.escalate(desired, actual) }
        for _, action := range r.Diff(desired, actual) {
            r.execute(action)
        }
        time.Sleep(r.reconcileInterval)
    }
}
```

### 完成验证

```go
type ValidationRule struct {
    Type   string  // "file_exists" / "test_passes" / "build_success"
    Path   string
    Cmd    string
    Expect Expectation
}
```

---

## 九、AgentShell 语法

### 核心命令

```bash
# 进程管理
spawn "意图" --skill=name1,name2 --budget=8000 --model=claude-haiku
ps / ps --tree / ps 42
kill 42 / kill 42 --signal=SIGKILL
wait 42 / wait --all

# VFS 操作（与 Unix 一致）
ls /proc/42/skills/
cat /proc/42/context
mount -t mcp github /mnt/mcp/github
umount /mnt/mcp/github

# Skill 管理
skill list / skill search "python"
skill install code-analyst / skill install go-dev@0.3.1
skill load code-analyst --pid=42

# 上下文操作
ctx show 42 / ctx inject 42 "额外背景"
ctx snapshot 42 / ctx restore 42 --snap=snap-001

# 权限
cap list 42 / cap grant 42 exec:/dev/shell
```

### 管道（最强组合原语）

```bash
# 智能体管道（PipeMessage 结构化传递）
spawn "分析代码" --skill=code-analyst \
  | spawn "写技术文档" --skill=tech-writer

# 和 VFS 配合
cat /mnt/mcp/github/repos/decker/agent-os/prs/42 \
  | spawn "审查此 PR" --skill=pr-reviewer

# 并行执行
spawn "写前端" --skill=react-dev &
spawn "写后端" --skill=go-dev    &
wait --all
```

### 监控命令

```bash
top    # 实时监控（token 消耗、状态）
log 42 # 实时追踪推理日志（[think] / [tool] / [output] 分类）
```

---

## 十、Agent Compose 格式

```yaml
version: "1.0"
intent: "构建 Go HTTP 服务，支持 JWT 认证和 PostgreSQL"

constraints:
  token_budget: 100000
  max_agents:   10
  timeout:      3600
  model:        claude-sonnet

mcp:
  github:
    mount: /mnt/mcp/github
    config: { repo: decker/agent-os, token: ${GITHUB_TOKEN} }

skills:
  - go-dev@1.2.0
  - api-designer
  - tech-writer

agents:
  architect:
    intent: "设计整体架构，输出 architecture.md"
    skills: [api-designer]
    budget: 8000
    outputs:
      - path: /tmp/architecture.md
        required: true

  backend:
    intent: "根据架构文档实现 Go HTTP 服务"
    skills: [go-dev]
    budget: 20000
    depends_on:
      architect: completed
    inputs:  [/tmp/architecture.md]

  tester:
    intent: "编写并运行完整测试"
    skills: [go-dev, code-analyst]
    budget: 10000
    depends_on:
      backend: completed
    validation:
      - cmd: "go test ./..."
        expect: { exit_code: 0 }

on_complete:
  - cmd: "go build ."
    assert: exit_code 0
```

### depends_on 语义

```yaml
depends_on:
  agent-name: completed  # 正常退出(exit 0)后才 spawn
  agent-name: started    # 启动即可（服务型 agent）
  agent-name: healthy    # /proc/{pid}/health 返回 ok
```

---

## 十一、上下文虚拟内存

### 概念映射

```
Unix 虚拟内存        Agent OS 上下文虚拟内存
────────────────────────────────────────────
物理 RAM        →   token 窗口（硬上限）
虚拟地址空间    →   逻辑上下文空间（无限）
内存页          →   上下文段（一组对话轮次）
Swap 磁盘       →   冷存储 (/var/cache/ctx/)
LRU 置换        →   语义 LRU 置换
```

### 段温度分级

```
🔥 Hot    当前在 token 窗口，活跃使用
🌡 Warm   最近访问，优先保留
❄️ Cold   长时间未访问，候选换出
🚨 Leaked 应释放未释放（内存泄漏）
```

### 语义 LRU 评分

```
score = 时间分（近期访问权重）
      + 频率分（访问次数对数）
      + 语义相关分（与当前推理步骤的余弦相似度 × 2.0）
```

语义权重最高——**保留和当前任务最相关的历史，即使它很久没被直接访问。**

### 换出时压缩

段换出前，LLM 自动生成摘要（~50 tokens）留在窗口中，原文存入 `/var/cache/ctx/`。召回时按语义相似度从冷存储检索，换入时优先换出低分段。

### CoW（写时复制）

进程 fork 时共享父进程上下文引用，写入时才复制。**用途：** `debug_replay` 时间旅行——从历史检查点 fork，修改后重新执行，原进程不受影响。

### ctx-profiler 输出示例

```
上下文内存分析 PID=42
窗口容量: 200,000 tokens | 已用: 47,320 (23.7%)
🔥 Hot  12段  41,200 tok | 🌡 Warm 3段 6,120 tok | ❄️ Cold 8段 在Swap中
预测：约 340 步后触发首次换出
建议：段 #007 可压缩，预计节省 ~7,400 tokens
```

---

## 十二、MVP 竖切片（Phase 1）

### 目标 Demo

```bash
$ rnix "分析 ./kernel/scheduler.go 并找出性能瓶颈"

[kernel] spawning PID 1...
[agent/1] reading /dev/fs/kernel/scheduler.go...
[agent/1] reasoning step 1/3...
══ 分析结果 ══════════════════════════════
发现 2 个性能瓶颈：
1. scheduler.go:47 — 全局锁，建议分片锁
2. scheduler.go:89 — O(n) 线性扫描，建议改堆
══════════════════════════════════════════
[kernel] PID 1 exited(0) | tokens: 1,847 | elapsed: 6.2s
```

### 文件结构（11 个文件）

```
rnix/
├── cmd/rnix/main.go              # CLI 入口
├── kernel/
│   ├── kernel.go                 # Kernel + Spawn + reasonStep
│   ├── process.go                # Process 结构 + 生命周期
│   └── reap.go                   # 进程回收
├── vfs/
│   ├── vfs.go                    # VFS 接口
│   ├── proc.go                   # /proc 动态文件系统
│   └── dev.go                    # /dev 设备注册
├── drivers/
│   ├── llm/claude.go             # Claude API 驱动
│   └── shell/shell.go            # Shell 执行驱动
├── context/context.go            # Context 分配/读写
├── skills/loader.go              # Skill 加载器
└── lib/skills/code-analyst/      # 第一个 Skill
    ├── manifest.yaml
    └── instructions.md
```

### MVP 阶段明确排除

```
✗ Capability 检查（直接放行）
✗ token 预算管理
✗ 进程间通信（IPC）
✗ ctx_swap 换出
✗ skillpkg 包管理
✗ AgentShell 完整语法（只实现 spawn）
✗ 调试工具链
✗ 声明式意图 / Reconciler
```

---

## 十三、实施路线图

### Phase 1：内核奠基（自举验证）

**验证标准：** Rnix 能用自身 syscall 层完成一个 Rnix 开发任务

**核心交付：**
- 微内核（Go goroutine 调度器 + Channel IPC）
- 核心 syscall（spawn/kill/wait/open/read/write ~15 个）
- VFS 框架（/proc + /dev 基本挂载）
- 进程模型（spawn/kill/wait + reasonStep 循环）
- LLM 驱动（/dev/llm/claude-sonnet）
- Shell 驱动（/dev/shell）
- 上下文管理（分配/读写/组装 prompt）
- 第一个 Skill（code-analyst）
- AgentShell MVP（单 spawn 命令）

### Phase 2：能力栈建设

**验证标准：** 能通过 agent-compose.yaml 声明多智能体项目并执行

**核心交付：**
- Tools 层完整驱动框架
- MCP 层挂载实现（mount -t mcp）
- Skills 层完整实现（skill_load/invoke/unload + VFS 映射）
- skillpkg 包管理器
- 三级智能体模型（进程/线程/协程）
- 事件驱动 + 异步 I/O
- Supervisor 树容错
- init 引导系统
- AgentShell 完整语法（管道/变量/脚本）
- Agent Compose 控制器

### Phase 3：涌现与智能

**验证标准：** 系统能从声明式意图自动规划、分化智能体、自主执行、自愈恢复

**核心交付：**
- 声明式意图 + Reconciler 控制器
- OODA 自主决策模型
- Token 经济 + 合约 SLA + 声誉系统
- 干细胞分化 + Skill 驱动特化
- 适应性免疫安全
- 完整调试工具链（agdb/astrace/时间旅行/分布式追踪/ctx-profiler）
- Agent Compose 完整版
- 可视化调试面板

---

## 十四、与 NewX 现有设计映射

| NewX 现有概念 | Agent OS 对应 | 升级点 |
|--------------|--------------|--------|
| 元智能体 | 用户态系统服务 | 从"上帝编排器"降级为可替换用户态服务 |
| 执行智能体 | 进程（fork/exec/signal）| 从一次性工人升级为有生命周期的一等公民 |
| 智能体模板 | Skill 共享库 + 干细胞分化 | 从静态模板升级为动态能力组合 |
| 工作流(阶段) | 事件驱动 + 声明式意图 | 从预规划瀑布升级为响应式自适应 |
| MCP 配置 | mount -t mcp 挂载 | 从配置文件升级为运行时动态挂载 |
| 工具注册表 | /dev 设备文件 + 驱动模型 | 从 RPC 函数表升级为一切皆文件 |

---

*文档来源：2026-02-23 头脑风暴会话（37个创意）+ 多智能体深度探讨（10个主题）*
*下一步：开始 Phase 1 竖切片实现*
