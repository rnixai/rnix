# Story 5.3: 参考手册

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 开发者,
I want 查阅参考手册获取所有 syscall、VFS 路径和 CLI 命令的完整规范,
So that 我在编写 Skill 或调试时有权威参考。

## Acceptance Criteria

1. **Syscall 完整覆盖** — Given 参考手册已编写，When 查阅内容，Then 包含 MVP 全部 15 个 syscall 的签名、参数、返回值、错误码、示例

2. **VFS 路径规范** — Given 参考手册已编写，When 查阅内容，Then 包含完整 VFS 路径规范（`/proc/{pid}/`、`/dev/llm/`、`/dev/fs`、`/dev/shell`、`/lib/skills/`）

3. **Manifest 字段说明** — Given 参考手册已编写，When 查阅内容，Then 包含 agent.yaml 和 SKILL.md 全部字段说明和示例

4. **CLI 命令完整列表** — Given 参考手册已编写，When 查阅内容，Then 包含 CLI 命令完整列表（`crux "意图"`、`crux ps`、`crux astrace`、`crux kill`、`crux version`）及其 flags

5. **IPC 架构说明** — Given 参考手册已编写，When 查阅内容，Then 包含 IPC 架构说明：daemon 生命周期（自动启动/自动停止/stale socket 清理）、Unix domain socket 通信机制、IPC 协议概述（NDJSON 消息格式、Method 枚举、流式 StreamEvent）、连接复用语义（非流式请求 Ping/ListProcs/Kill 复用同一连接，流式请求 Spawn/AttachDebug 终结连接）

## Tasks / Subtasks

- [ ] Task 1: 创建 `docs/reference.md` 文件框架 (AC: #1-5)
  - [ ] 1.1 在 `docs/` 目录下创建 `reference.md`
  - [ ] 1.2 添加文档标题、简介段落（定位和受众说明）
  - [ ] 1.3 添加目录结构（TOC）

- [ ] Task 2: 编写 Syscall 参考章节 (AC: #1)
  - [ ] 2.1 Syscall 概述：分类说明（ProcessManager / ContextManager / FileSystem / Debugger）
  - [ ] 2.2 进程管理 syscall（5 个）：Spawn、Kill、Wait、ListProcs、GetPID
  - [ ] 2.3 上下文管理 syscall（4 个）：CtxAlloc、CtxRead、CtxWrite、CtxFree
  - [ ] 2.4 文件系统 syscall（5 个）：Open、Read、Write、Close、Stat
  - [ ] 2.5 调试 syscall（1 个）：DebugRecord / SyscallEvent
  - [ ] 2.6 每个 syscall 含签名、参数表、返回值、错误码列表、行为描述、示例

- [ ] Task 3: 编写 VFS 路径规范章节 (AC: #2)
  - [ ] 3.1 VFS 概述：设备模型、路径匹配机制（精确匹配 + 最长前缀匹配）
  - [ ] 3.2 `/dev/llm/claude` — LLM 驱动设备
  - [ ] 3.3 `/dev/fs` — 宿主文件系统设备
  - [ ] 3.4 `/dev/shell` — Shell 执行设备
  - [ ] 3.5 `/proc/{pid}/` — 动态进程信息（status、intent、context 三个子路径）
  - [ ] 3.6 `/lib/agents/` 和 `/lib/skills/` — Agent 和 Skill 存储路径
  - [ ] 3.7 VFSFile 接口和 OpenFlag 枚举
  - [ ] 3.8 FD 分配规则（从 3 开始，单调递增）

- [ ] Task 4: 编写 Agent 和 Skill 清单章节 (AC: #3)
  - [ ] 4.1 agent.yaml 字段完整说明（AgentManifest: name、description、models、context_budget、skills）
  - [ ] 4.2 AgentModels 子结构（provider、preferred、fallback）
  - [ ] 4.3 instructions.md 格式和用途
  - [ ] 4.4 Agent 加载流程说明
  - [ ] 4.5 SKILL.md 格式（YAML frontmatter + Markdown body）
  - [ ] 4.6 SkillManifest 字段（name、description、allowed-tools、metadata）
  - [ ] 4.7 渐进式加载策略（LoadMetadata vs LoadFull）
  - [ ] 4.8 完整的 agent.yaml 和 SKILL.md 示例

- [ ] Task 5: 编写 CLI 命令参考章节 (AC: #4)
  - [ ] 5.1 全局 flags（--json、--verbose/-v、--quiet/-q）
  - [ ] 5.2 `crux "意图"` — 根命令（含 --model、--max-steps、--agent flags）
  - [ ] 5.3 `crux ps` — 进程列表命令（含四种输出模式示例）
  - [ ] 5.4 `crux kill <pid>` — 进程终止命令
  - [ ] 5.5 `crux astrace <pid>` — Syscall 追踪命令（含三种输出模式示例）
  - [ ] 5.6 `crux version` — 版本信息命令
  - [ ] 5.7 JSON 响应包装格式（JSONResponse 结构）

- [ ] Task 6: 编写 IPC 架构章节 (AC: #5)
  - [ ] 6.1 Daemon 生命周期（自动启动、空闲超时自动停止、stale socket 清理）
  - [ ] 6.2 Unix domain socket 路径规则（XDG_RUNTIME_DIR 优先，/tmp 降级）
  - [ ] 6.3 NDJSON 协议概述（Request/Response 信封格式）
  - [ ] 6.4 Method 枚举（ping、spawn、list_procs、kill、attach_debug、shutdown）
  - [ ] 6.5 流式 StreamEvent 协议（StreamEventType 枚举、ProgressPayload 结构）
  - [ ] 6.6 连接复用语义（非流式复用连接，流式终结连接）
  - [ ] 6.7 Spawn 流式协议完整示例
  - [ ] 6.8 AttachDebug 流式协议完整示例

- [ ] Task 7: 编写错误处理与类型参考章节 (AC: #1)
  - [ ] 7.1 ErrCode 枚举（TIMEOUT、NOT_FOUND、PERMISSION、INTERNAL、DRIVER、INVALID）
  - [ ] 7.2 SyscallError 结构和格式
  - [ ] 7.3 VFSError 结构和格式
  - [ ] 7.4 DriverError 结构和格式
  - [ ] 7.5 基础类型（PID、FD、CtxID、Signal、ProcessState）

- [ ] Task 8: 编写进程模型参考章节 (AC: #1)
  - [ ] 8.1 ProcessState 状态机（Created → Running → Zombie → Dead）
  - [ ] 8.2 状态转移规则（合法与非法转移）
  - [ ] 8.3 ExitStatus 结构
  - [ ] 8.4 资源释放顺序
  - [ ] 8.5 Signal 定义（SIGTERM=1、SIGKILL=2）

- [ ] Task 9: 校验与完善 (AC: #1-5)
  - [ ] 9.1 交叉验证所有 syscall 签名与源码一致
  - [ ] 9.2 验证所有 VFS 路径与注册代码一致
  - [ ] 9.3 验证所有 CLI flags 与 cobra 注册一致
  - [ ] 9.4 验证 IPC 协议与 protocol.go 定义一致
  - [ ] 9.5 确保文档可独立阅读（不强制依赖概念文档）
  - [ ] 9.6 最终审读：内容完整、格式一致、无遗漏

## Dev Notes

### 这是一个文档类 Story

**重要：** 本 Story 不涉及 Go 代码编写。输出是 `docs/reference.md` 单一 Markdown 文件。开发代理需要理解现有的源码实现和文档体系，创建面向开发者的权威参考手册。

### 文档写作原则

1. **面向开发者** — 读者是使用 Crux 编写 Agent/Skill 或调试问题的开发者，需要精确、权威的技术参考
2. **准确反映实现** — 所有签名、参数、返回值、路径、协议必须与当前代码实现一致。不要写尚未实现的功能
3. **简体中文** — 全文使用简体中文。技术术语首次出现时附英文
4. **结构化参考** — 参考手册不是教程，而是查阅工具。信息按类别组织，便于快速定位
5. **每个条目含示例** — 每个 syscall、每条 CLI 命令都至少有一个用法示例
6. **精确到源码级别** — 关键数据类型、错误码、协议格式必须精确匹配源码

### 文档输出位置

文件路径：`docs/reference.md`

`docs/` 目录当前已有 `concepts.md`（Story 5.1）和 `quick-start.md`（Story 5.2），参考手册是第三个文件。

### 与已有文档的关系

- **概念文档**（`docs/concepts.md`）— 回答"是什么、为什么"：核心概念定义、Unix 类比
- **快速上手**（`docs/quick-start.md`）— 回答"怎么做"：安装步骤、第一个命令
- **参考手册**（`docs/reference.md`）— 回答"具体规范是什么"：完整 API 签名、路径规范、协议定义

参考手册可在适当位置链接到概念文档和快速上手指南，但本身应完全自包含——开发者不需要先读其他文档就能查阅任何条目。

### Syscall 参考数据（从源码提取）

以下是从实际源码中提取的 15 个 MVP Syscall 的完整规范。开发代理必须以此为准，不要从架构文档推断。

#### 进程管理（ProcessManager）— 3+2 个

**Spawn** (`kernel/kernel.go:121-253`)
```
签名: Spawn(intent string, agent *agents.AgentInfo, opts SpawnOpts) (PID, error)
参数:
  - intent: string — 用户意图字符串
  - agent: *agents.AgentInfo — Agent 定义（可选，nil 表示通用模式）
  - opts: SpawnOpts — 配置选项
    - Model: string — LLM 模型名称
    - SystemPrompt: string — 系统提示词
    - MaxTurns: int — 最大推理步数（默认 10）
    - TimeoutMs: int64 — 超时毫秒数
    - ParentPID: PID — 父进程 PID（0=顶层）
返回: (PID, error)
错误码: ErrNotFound（父进程不存在）, ErrInternal（上下文分配失败）, ErrDriver（LLM 设备打开失败）
```

**Kill** (`kernel/kernel.go:615-653`)
```
签名: Kill(pid PID, signal Signal) error
参数:
  - pid: PID — 目标进程 ID
  - signal: Signal — SIGTERM(1) 或 SIGKILL(2)
返回: error
错误码: ErrNotFound（进程不存在）, ErrInvalid（无效信号）
幂等性: Kill Zombie/Dead 进程为 no-op
```

**Wait** (`kernel/reap.go:87-113`)
```
签名: Wait(pid PID) (ExitStatus, error)
参数:
  - pid: PID — 目标进程 ID
返回: (ExitStatus, error)
  ExitStatus: { Code int, Reason string, Err error }
错误码: ErrNotFound（进程不存在）
行为: 阻塞直到进程转为 Zombie，然后执行完整资源释放序列
```

**ListProcs** (`kernel/kernel.go:710-730`)
```
签名: ListProcs() []vfs.ProcInfo
参数: 无
返回: []ProcInfo
  ProcInfo: { PID, PPID, State, Intent, Skills, TokensUsed, CreatedAt, CtxID, Result, AllowedDevices }
```

**GetPID** — 延迟实现，通过 ListProcs 替代

#### 上下文管理（ContextManager）— 4 个

**CtxAlloc** (`context/context.go:79-96`)
```
签名: CtxAlloc(size int) (CtxID, error)
参数:
  - size: int — 最大消息数量
返回: (CtxID, error)
错误码: ErrInternal（size <= 0）
默认值: DefaultCtxSize = 64
```

**CtxRead** (`context/context.go:178-217`)
```
签名: CtxRead(cid CtxID, offset int, length int) ([]byte, error)
参数:
  - cid: CtxID — 上下文 ID
  - offset: int — 消息起始索引（0-based）
  - length: int — 读取消息数量
返回: ([]byte, error) — JSON 序列化的上下文
特殊: offset=0, length=0 读取全部内容
错误码: ErrNotFound（上下文不存在）, ErrInternal（序列化失败）
返回格式: {"system_prompt": "...", "messages": [...]}
```

**CtxWrite** (`context/context.go:131-173`)
```
签名: CtxWrite(cid CtxID, offset int, data []byte) error
参数:
  - cid: CtxID — 上下文 ID
  - offset: int — 0=追加, 1..N=覆写第 offset-1 个消息
  - data: []byte — JSON 序列化的 Message
返回: error
错误码: ErrInternal（JSON 解析失败, 容量已满, offset 越界）
Message 格式: {"role": "system|user|assistant|tool", "content": "...", "tool_call_id": "..."}
```

**CtxFree** (`context/context.go:99-110`)
```
签名: CtxFree(cid CtxID) error
参数:
  - cid: CtxID — 上下文 ID
返回: error
错误码: ErrNotFound（上下文不存在）
```

#### 文件系统（FileSystem）— 5 个

**Open** (`vfs/vfs.go:175-187`)
```
签名: Open(pid PID, path string, flags OpenFlag) (FD, error)
参数:
  - pid: PID — 进程 ID
  - path: string — VFS 路径（如 /dev/llm/claude）
  - flags: OpenFlag — O_RDONLY(0), O_WRONLY(1), O_RDWR(2)
返回: (FD, error) — FD 从 3 开始递增
错误码: ErrNotFound（设备不存在）, ErrDriver（驱动错误）
路径匹配: 精确匹配优先，然后最长前缀匹配
```

**Read** (`vfs/vfs.go:190-204`)
```
签名: Read(pid PID, fd FD, length int) ([]byte, error)
参数:
  - pid: PID — 进程 ID
  - fd: FD — 文件描述符
  - length: int — 最大读取字节数
返回: ([]byte, error)
错误码: ErrNotFound（FD 无效）, ErrDriver（读取失败）
```

**Write** (`vfs/vfs.go:207-220`)
```
签名: Write(ctx context.Context, pid PID, fd FD, data []byte) error
参数:
  - ctx: context.Context — 支持取消（Kill 信号中断）
  - pid: PID — 进程 ID
  - fd: FD — 文件描述符
  - data: []byte — 写入数据
返回: error
错误码: ErrNotFound（FD 无效）, ErrDriver（写入失败）
注意: Write 接受 ctx 参数以支持 Kill 时中断 LLM 调用
```

**Close** (`vfs/vfs.go:223-236`)
```
签名: Close(pid PID, fd FD) error
参数:
  - pid: PID — 进程 ID
  - fd: FD — 文件描述符
返回: error
错误码: ErrNotFound（FD 无效）, ErrDriver（关闭失败）
行为: 调用设备 Close() 并从 FDTable 移除 FD
```

**Stat** (`vfs/vfs.go:239-249`)
```
签名: Stat(path string) (FileStat, error)
参数:
  - path: string — VFS 路径
返回: (FileStat, error)
  FileStat: { Name string, Size int64, IsDevice bool, DevicePath string }
错误码: ErrNotFound（设备不存在）, ErrDriver（元数据获取失败）
```

#### 调试（Debugger）— 1 个

**DebugRecord / SyscallEvent** (`debug/event.go`)
```
事件创建: NewEvent(pid PID, createdAt time.Time, syscall string, args map[string]any) SyscallEvent
事件完成: CompleteEvent(event *SyscallEvent, result any, err error, duration time.Duration)
SyscallEvent 结构:
  - Timestamp: time.Duration — 相对进程创建时间
  - PID: PID
  - Syscall: string — 与接口方法名一致（"Spawn", "Open", "CtxWrite" 等）
  - Args: map[string]any — 参数快照
  - Result: any — 返回值
  - Err: error
  - Duration: time.Duration — 执行耗时
传递: 通过 proc.DebugChan（缓冲 256），满则丢弃（非阻塞）
消费: 通过 IPC MethodAttachDebug 流式获取
```

### VFS 路径参考数据（从源码提取）

#### 已注册设备路径

| VFS 路径 | 驱动模块 | 注册位置 | 说明 |
|---------|---------|---------|------|
| `/dev/llm/claude` | `drivers/llm` | `cmd/crux/main.go:622` | Claude Code CLI 调用 |
| `/dev/fs` | `drivers/fs` | `cmd/crux/main.go:623` | 宿主文件系统（前缀匹配，子路径作为文件路径） |
| `/dev/shell` | `drivers/shell` | `cmd/crux/main.go:625` | Shell 命令执行 |
| `/proc` | `vfs/proc.go` | `cmd/crux/main.go:635` | 动态进程信息（前缀匹配） |

#### /proc/{pid}/ 子路径

| 子路径 | 格式 | 内容 |
|--------|------|------|
| `/proc/{pid}/status` | JSON | `{"pid", "ppid", "state", "intent", "skills", "tokens_used", "elapsed_ms", "allowed_devices"}` |
| `/proc/{pid}/intent` | Plain Text | 原始意图字符串 |
| `/proc/{pid}/context` | Plain Text | 上下文摘要 |

#### DeviceRegistry 匹配规则 (`vfs/dev.go:14-57`)

1. 精确匹配 — 路径完全一致
2. 最长前缀匹配 — 选择最长前缀，剩余作为 subpath 传给驱动工厂
   - 例: `/dev/fs/path/to/file` → 匹配 `/dev/fs`，subpath = `/path/to/file`

#### VFSFile 接口 (`vfs/vfs.go:31-37`)

```go
type VFSFile interface {
    Read(length int) ([]byte, error)
    Write(ctx context.Context, data []byte) error
    Close() error
    Stat() (FileStat, error)
}
```

#### OpenFlag 枚举 (`vfs/vfs.go:14-21`)

```go
O_RDONLY = 0  // 只读
O_WRONLY = 1  // 只写
O_RDWR   = 2  // 读写
```

#### FD 分配规则

- 起始值: 3（0/1/2 预留给 stdin/stdout/stderr）
- 分配方式: 每进程独立计数器，单调递增
- 作用域: 每个 Process 拥有独立的 FDTable

### CLI 命令参考数据（从源码提取）

#### 全局 Flags (`cmd/crux/main.go:194-196`)

| Flag | 短选项 | 类型 | 说明 |
|------|--------|------|------|
| `--json` | — | bool | JSON 格式输出 |
| `--verbose` | `-v` | bool | 详细输出 |
| `--quiet` | `-q` | bool | 静默输出 |

**输出模式优先级**: `--json` > `--quiet` > `--verbose` > 默认

#### 根命令: `crux [intent]`

```
用法: crux [intent]
参数: [intent] — 任意长度意图字符串（多个参数以空格拼接）
```

**私有 Flags** (`cmd/crux/main.go:197-199`):

| Flag | 短选项 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--model` | `-m` | string | `""` | LLM 模型（sonnet/opus/haiku） |
| `--max-steps` | — | int | `0` | 最大推理步数（0=默认 10） |
| `--agent` | — | string | `""` | Agent 定义名称 |

**JSON 成功响应**:
```json
{"ok": true, "data": {"pid": 1, "result": "...", "tokens_used": 1234, "elapsed_ms": 6200, "exit_code": 0}}
```

**JSON 错误响应**:
```json
{"ok": false, "error": {"code": "TIMEOUT", "message": "...", "syscall": "Write", "device": "/dev/llm/claude"}}
```

#### 子命令: `crux version`

```
用法: crux version
```

**默认输出**:
```
crux 0.1.0
claude-code: 1.x.x
```

**Claude CLI 未安装时**:
```
crux 0.1.0
✗ claude-code CLI not found
  → 建议: npm install -g @anthropic-ai/claude-code
```

**JSON 输出**:
```json
{"ok": true, "data": {"version": "0.1.0", "claude_code_available": true, "claude_code": "1.0.3"}}
```

#### 子命令: `crux ps`

```
用法: crux ps
参数: 无 (cobra.NoArgs)
```

**四种输出模式**:

1. **默认** — 表格格式（PID, STATE, SKILL, TOKENS, ELAPSED）
2. **--verbose** — 含 PPID、Intent 额外字段
3. **--quiet** — 逐行输出 PID
4. **--json** — 结构化 JSON

**JSON 响应**:
```json
{
  "ok": true,
  "data": {
    "processes": [
      {"pid": 1, "ppid": 0, "state": "running", "intent": "...", "skills": ["code-analysis"], "tokens_used": 456, "elapsed_ms": 3200}
    ]
  }
}
```

**无活跃进程时**: `No active processes.`

#### 子命令: `crux kill <pid>`

```
用法: crux kill <pid>
参数: <pid> — 进程 ID（十进制数字，必须恰好 1 个参数）
信号: 固定发送 SIGTERM(1)
```

**成功**: `[kernel] PID {pid}: signal sent (SIGTERM)`

#### 子命令: `crux astrace <pid>`

```
用法: crux astrace <pid>
参数: <pid> — 进程 ID（必须恰好 1 个参数）
```

**三种输出模式**:

1. **默认** — 格式化追踪行
```
[astrace] attached to PID 1 (state: running)
[  0.013s] Open(flags=2, path="/dev/llm/claude") → 3    1ms
[  0.014s] Write(fd=3, size=1234) → <nil>    5.20s
[  5.214s] Read(fd=3, length=65536) → 892B      2ms
[astrace] detached from PID 1 (process exited)
```

2. **--verbose** — 完整参数和结果
3. **--json** — 逐行 JSON（SyscallEventWire 结构）

**SIGINT 行为**: 仅 detach 追踪，不影响被追踪进程

### Agent Manifest 参考数据（从源码提取）

#### agent.yaml 结构 (`agents/types.go:17-23`)

```go
type AgentManifest struct {
    Name          string      `yaml:"name"`           // 必需
    Description   string      `yaml:"description"`    // 可选
    Models        AgentModels `yaml:"models"`          // 可选
    ContextBudget int         `yaml:"context_budget"`  // 可选
    Skills        []string    `yaml:"skills"`          // 可选
}

type AgentModels struct {
    Provider  string `yaml:"provider"`   // LLM 提供商（如 claude）
    Preferred string `yaml:"preferred"`  // 首选模型（如 sonnet）
    Fallback  string `yaml:"fallback"`   // 备用模型（如 haiku）
}
```

#### AgentInfo 聚合方法 (`agents/types.go:26-57`)

- `AllowedTools()` — 收集所有 Skill 的 allowed-tools，去重排序，作为设备权限白名单
- `SystemPrompt()` — 拼接 Agent instructions + 所有 Skill body，用 `\n\n` 分隔

#### AgentLoader 加载流程 (`agents/loader.go:25-82`)

1. 路径安全检查（防止目录遍历）
2. 读取 `{agentDir}/agent.yaml` → AgentManifest
3. 验证 `name` 字段必需
4. 读取 `{agentDir}/instructions.md` → string
5. 遍历 `manifest.Skills`，对每个调用 `skillLoader.LoadFull(skillName)`
6. 返回 AgentInfo

#### 参考 Agent 示例 (`lib/agents/code-analyst/agent.yaml`)

```yaml
name: code-analyst
description: "分析代码质量、识别潜在问题并提供改进建议的智能体"
models:
  provider: claude
  preferred: sonnet
  fallback: haiku
context_budget: 8192
skills:
  - code-analysis
```

### Skill Manifest 参考数据（从源码提取）

#### SKILL.md 格式（Agent Skills 行业标准）

```
---
name: <skill-name>              # 必需
description: >                  # 可选
  <multiline description>
allowed-tools: <space-separated> # 关键字段，空格分隔的 VFS 设备路径
metadata:                        # 可选
  <arbitrary key-value pairs>
---

# Markdown Body（程序性知识）

<operational documentation>
```

#### SkillManifest 结构 (`skills/types.go:6-11`)

```go
type SkillManifest struct {
    Name            string            `yaml:"name"`           // 必需
    Description     string            `yaml:"description"`    // 可选
    AllowedToolsRaw string            `yaml:"allowed-tools"`  // 空格分隔设备路径
    Metadata        map[string]string `yaml:"metadata"`       // 可选
}
```

#### SKILL.md 解析规则 (`skills/loader.go:22-48`)

1. 文件必须以 `---` 开头
2. 两个 `---` 之间 → YAML frontmatter
3. 第二个 `---` 之后 → Markdown body
4. 不以 `---` 开头 → 错误 "SKILL.md must start with ---"
5. 缺少结束 `---` → 错误 "SKILL.md missing closing ---"

#### AllowedTools() 方法 (`skills/types.go:13-20`)

- `/dev/fs /dev/shell` → `["/dev/fs", "/dev/shell"]`
- 空字符串 → `nil`（无限制）

#### 渐进式加载 (`skills/loader.go:103-121`)

- `LoadMetadata(skillName)` — 仅 frontmatter，Body 为空（~100 tokens）
- `LoadFull(skillName)` — frontmatter + body（< 5000 tokens）

#### 参考 Skill 示例 (`lib/skills/code-analysis/SKILL.md`)

```markdown
---
name: code-analysis
description: >
  Analyze code quality, identify bugs, performance issues and security
  vulnerabilities.
allowed-tools: /dev/fs /dev/shell
metadata:
  author: crux
  version: "1.0"
---

# Code Analysis

## When to use this skill
...
```

### IPC 架构参考数据（从源码提取）

#### Daemon 生命周期

**自动启动** (`ipc/daemon.go:29-47`):
1. CLI 命令调用 `EnsureDaemon()`
2. 尝试 ping 现有 daemon
3. 失败则清除 stale socket + 启动新 daemon (`crux daemon --internal`)
4. 轮询等待就绪（最多 3 秒）

**自动停止** (`ipc/server.go:64-172`):
- 默认空闲超时: 60 秒 (`DefaultIdleTimeout`)
- 条件: 无活跃进程 AND 无活跃连接
- 检查周期: 每 5 秒

**Stale socket 清理** (`ipc/daemon.go:91-108`):
- ping 超时 → 删除旧 socket 文件 → 启动新 daemon

#### Socket 路径 (`ipc/protocol.go:214-224`)

1. `$XDG_RUNTIME_DIR/crux/crux.sock`（如 `/run/user/1000/crux/crux.sock`）
2. `/tmp/crux-{uid}/crux.sock`（降级方案）

#### NDJSON 协议

**Request 格式**:
```json
{"method": "spawn|list_procs|kill|attach_debug|ping|shutdown", "payload": {...}}
```

**Response 格式**:
```json
{"ok": true|false, "payload": {...}, "error": {"code": "...", "message": "..."}}
```

#### Method 枚举 (`ipc/protocol.go:14-24`)

| Method | 类型 | 说明 |
|--------|------|------|
| `ping` | 请求-响应 | 活性检查，返回版本号 |
| `spawn` | 流式 | 创建进程，流式返回进度事件 |
| `list_procs` | 请求-响应 | 获取所有进程列表 |
| `kill` | 请求-响应 | 发送信号到进程 |
| `attach_debug` | 流式 | 订阅 SyscallEvent 流 |
| `shutdown` | 请求-响应 | 优雅关闭 daemon |

#### 连接复用语义 (`ipc/server.go:199-238`)

- **非流式方法** (ping, list_procs, kill): 发送响应后继续接收下一个请求 → 连接可复用
- **流式方法** (spawn, attach_debug): 接管连接进行流式传输 → 流结束后返回（连接终结）

#### StreamEvent 协议 (`ipc/protocol.go:131-180`)

```json
{"type": "progress|complete|error|syscall_event|eof", "payload": {...}}
```

**StreamEventType 枚举**:
- `progress` — 推理步骤进度
- `complete` — 进程完成
- `error` — 错误
- `syscall_event` — SyscallEvent（astrace 用）
- `eof` — 流结束标记

**ProgressPayload 结构** (`ipc/protocol.go:150-169`):
```go
type ProgressPayload struct {
    Event        string    // "spawn", "step", "complete", "error"
    PID          types.PID
    Intent       string    // OnSpawn
    Step, Total  int       // OnStep
    Result       string    // OnComplete
    ExitCode     int       // OnComplete
    ExitReason   string    // OnComplete
    TokensUsed   int       // OnComplete
    ErrorMessage string    // OnError
}
```

**SyscallEventWire 结构** (`ipc/protocol.go:171-180`):
```go
type SyscallEventWire struct {
    TimestampMs int64          `json:"timestamp_ms"`
    PID         types.PID      `json:"pid"`
    Syscall     string         `json:"syscall"`
    Args        map[string]any `json:"args"`
    Result      any            `json:"result"`
    Error       string         `json:"error"`
    DurationMs  float64        `json:"duration_ms"`
}
```

### 错误处理参考数据（从源码提取）

#### ErrCode 枚举 (`internal/types/types.go:18-27`)

| 错误码 | 值 | 含义 |
|--------|-----|------|
| `ErrTimeout` | `"TIMEOUT"` | 超时 |
| `ErrNotFound` | `"NOT_FOUND"` | 资源不存在 |
| `ErrPermission` | `"PERMISSION"` | 权限拒绝 |
| `ErrInternal` | `"INTERNAL"` | 内部错误 |
| `ErrDriver` | `"DRIVER"` | 驱动错误 |
| `ErrInvalid` | `"INVALID"` | 无效参数 |

#### 错误类型层次

1. **SyscallError** (`kernel/errors.go:10-16`) — 内核层
   - 格式: `[Code] PID N Syscall: Device (Err)`
   - 字段: Syscall, PID, Device, Err, Code

2. **VFSError** (`vfs/vfs.go:43-60`) — VFS 层
   - 格式: `[Code] PID N Op: Device (Err)`
   - 字段: Op, PID, Device, Err, Code

3. **DriverError** (`internal/types/types.go:68-90`) — 驱动层
   - 格式: `[Code] Op: Device (Err)`
   - 字段: Op, Device, Err, Code

4. **ContextError** (`context/context.go:48-63`) — 上下文层
   - 格式: `[Code] CtxID N Op: Err`
   - 字段: CtxID, Op, Err, Code

### 基础类型参考数据（从源码提取）

#### 核心类型 (`internal/types/types.go:8-15`)

| 类型 | Go 定义 | 说明 |
|------|---------|------|
| `PID` | `uint64` | 进程 ID（从 1 递增，不回收） |
| `FD` | `int` | 文件描述符（从 3 递增） |
| `CtxID` | `uint64` | 上下文 ID（从 1 递增） |
| `ErrCode` | `string` | 错误分类码 |
| `Signal` | `int` | 进程信号 |
| `ProcessState` | `int` | 进程状态 |

#### Signal 定义 (`internal/types/types.go:29-35`)

| 常量 | 值 | 说明 |
|------|-----|------|
| `SIGTERM` | `1` | 终止信号（优雅关闭） |
| `SIGKILL` | `2` | 强制杀死 |

#### ProcessState 定义 (`internal/types/types.go:42-66`)

| 常量 | 值 | 字符串表示 |
|------|-----|---------|
| `StateCreated` | `0` | `"created"` |
| `StateRunning` | `1` | `"running"` |
| `StateZombie` | `2` | `"zombie"` |
| `StateDead` | `3` | `"dead"` |

#### 状态转移规则 (`kernel/process.go:73-78`)

```
合法: Created→Running, Running→Zombie, Zombie→Dead
非法: 所有其他转移（Running→Created, Zombie→Running, Dead→任何）
```

#### 资源释放顺序 (`kernel/reap.go:13-43`)

```
1. Cancel context
2. Wait goroutine
3. Close DebugChan
4. CtxFree
5. Reap (状态转 Dead)
6. RemoveProcess (从进程表移除)
```

### 前序 Story 经验

#### Story 5.1（概念文档）经验

- **文档类 Story 不涉及代码修改** — 输出为纯 Markdown 文件
- **源码验证至关重要** — 所有示例中的细节必须与实际代码交叉验证
- **Code Review 发现的典型问题** — 示例中的细节不准确（如 O_RDONLY vs O_RDWR）、数据流步骤遗漏

#### Story 5.2（快速上手指南）经验

- **CLI 命令和输出格式已通过源码交叉验证** — cmd/crux/main.go, internal/ui/*.go, debug/*.go
- **astrace 输出示例精确匹配 trace.go 实现** — key=value 参数格式、`← LLM 调用`/`← 慢操作` 注解逻辑
- **Code Review 修复** — 首次执行示例改为匹配 AC、补充 --json 输出示例、修正 version 输出格式去掉 `v` 前缀
- **Agent 配置格式** — 已确认了 Agent/Skill 实际的文件格式

#### Git 提交模式

文档类 Story 通常 1 个提交（Add Story X.X），commit message 使用英文。

### Project Structure Notes

**创建的新文件：**
```
docs/reference.md          — 参考手册（本 Story 唯一输出）
```

**不修改的文件：**
```
所有 .go 文件              — 本 Story 不涉及代码修改
docs/concepts.md           — Story 5.1 产出
docs/quick-start.md        — Story 5.2 产出
```

### References

**规划文档：**
- [Source: _bmad-output/planning-artifacts/epics.md#Story 5.3] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/prd.md#FR40] — 参考手册功能需求
- [Source: _bmad-output/planning-artifacts/prd.md#Documentation Strategy] — 文档策略
- [Source: _bmad-output/planning-artifacts/prd.md#API Surface] — Syscall ABI 概述
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 1] — Syscall 接口设计
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 3] — VFS 实现策略
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 5] — 调试架构
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 7] — Agent/Skill 分层
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Patterns] — 命名、格式、协议模式
- [Source: _bmad-output/project-context.md] — AI Agent 编码规则和项目上下文

**已完成的相关文档：**
- [Source: docs/concepts.md] — Story 5.1 产出的概念文档
- [Source: docs/quick-start.md] — Story 5.2 产出的快速上手指南
- [Source: _bmad-output/implementation-artifacts/5-2-quick-start-guide.md] — Story 5.2 完整上下文

**源码参考（验证所有规范数据）：**
- kernel/kernel.go: Spawn(121-253), Kill(615-653), ListProcs(710-730), SpawnOpts(27-34), ProcessManager(82-86), KernelCallbacks(73-78)
- kernel/process.go: Process(31-54), ExitStatus(24-29), ProcessState(43-50), validTransitions(73-78)
- kernel/reap.go: Wait(87-113), reapProcess(13-43)
- kernel/errors.go: SyscallError(10-16)
- context/context.go: CtxAlloc(79-96), CtxRead(178-217), CtxWrite(131-173), CtxFree(99-110), Message(16-30), Context(32-39)
- vfs/vfs.go: VFSFile(31-37), OpenFlag(14-21), FileStat, VFSError(43-60), Open(175-187), Read(190-204), Write(207-220), Close(223-236), Stat(239-249), FDTable(74-134)
- vfs/proc.go: ProcInfo(29-40), ProcessInfoProvider(16-19), statusJSON(162-193), /proc 路径解析(100-114)
- vfs/dev.go: DeviceRegistry(14-57), 精确+前缀匹配
- internal/types/types.go: PID/FD/CtxID(8-15), ErrCode(18-27), Signal(29-40), ProcessState(42-66), DriverError(68-90), SyscallEvent(92-101)
- ipc/protocol.go: Method(14-24), Request/Response(26-37), SpawnRequest(48-53), StreamEvent(131-147), ProgressPayload(150-169), SyscallEventWire(171-180), SocketPath(214-224)
- ipc/server.go: handleConn(199-238), idleTimeout(64-172), 连接复用语义
- ipc/daemon.go: EnsureDaemon(29-47), startDaemon(68-89), stale清理(91-108)
- ipc/client.go: Dial, Spawn, AttachDebug, Kill, ListProcs
- cmd/crux/main.go: rootCmd(108-118), versionCmd(120-124), astraceCmd(126-135), psCmd(137-147), killCmd(149-154), daemonCmd(156-160), 全局flags(194-196), 私有flags(197-199), JSON类型(63-85), 设备注册(622-635)
- agents/types.go: AgentManifest(17-23), AgentModels(10-14), AgentInfo(26-30), AllowedTools(33-46), SystemPrompt(49-57)
- agents/loader.go: Load(25-82)
- skills/types.go: SkillManifest(6-11), AllowedTools(13-20), SkillInfo(22-26)
- skills/loader.go: parseSKILLMD(22-48), LoadMetadata(103-109), LoadFull(112-121)
- debug/event.go: NewEvent, CompleteEvent
- drivers/llm/claude_cli.go: ClaudeCliDriver 实现
- drivers/fs/hostfs.go: HostFSDriver 实现
- drivers/shell/shell.go: ShellDriver 实现
- internal/ui/trace.go: FormatTraceLine 输出格式
- internal/ui/table.go: RenderProcessTable 输出格式
- internal/ui/error.go: 三段式错误格式
- internal/ui/summary.go: Summary Footer 格式
- lib/agents/code-analyst/agent.yaml: Agent 配置示例
- lib/agents/code-analyst/instructions.md: Agent 指令示例
- lib/skills/code-analysis/SKILL.md: Skill 定义示例

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
