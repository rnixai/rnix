# Implementation Patterns & Consistency Rules

## Pattern Categories Defined

**已识别 6 大类 25 个潜在冲突点**——以下模式确保多 AI Agent 协同编码时写出兼容、一致的代码。

## 命名模式（Naming Patterns）

**Go 代码命名：**

| 对象 | 规则 | 示例 | 反例 |
|------|------|------|------|
| 包名 | 全小写单词，不用下划线 | `kernel`, `vfs`, `context` | `Kernel`, `my_pkg` |
| 导出类型 | PascalCase | `Process`, `SyscallEvent`, `LLMDriver` | `process`, `syscall_event` |
| 非导出类型 | camelCase | `pidCounter`, `fdTable` | `pid_counter` |
| 导出函数 | PascalCase 动词开头 | `Spawn()`, `CtxAlloc()`, `Open()` | `spawn()`, `ctx_alloc()` |
| 接口 | 名词或 `-er` 后缀 | `FileSystem`, `Debugger`, `LLMDriver` | `IFileSystem`, `DebuggerInterface` |
| 常量 | PascalCase（导出）或 camelCase（非导出） | `MaxTokens`, `ErrTimeout` | `MAX_TOKENS`, `ERR_TIMEOUT` |
| 错误变量 | `Err` 前缀 | `ErrNotFound`, `ErrTimeout` | `NotFoundError`, `TimeoutErr` |
| 泛型类型参数 | 单字母大写或语义短词 | `T`, `K`, `V`, `Item` | `Type`, `TKey` |

**Syscall 命名：**

| 规则 | 示例 | 说明 |
|------|------|------|
| PascalCase 动词 | `Spawn`, `Kill`, `Wait` | 进程管理类 |
| `Ctx` 前缀 + PascalCase | `CtxAlloc`, `CtxRead`, `CtxWrite`, `CtxFree` | 上下文类 |
| Unix 风格动词 | `Open`, `Read`, `Write`, `Close`, `Stat` | 文件系统类 |
| `Debug` 前缀 | `DebugRecord` | 调试类 |

**VFS 路径命名：**

| 路径段 | 规则 | 示例 |
|--------|------|------|
| 顶级目录 | 全小写 Unix 风格 | `/proc/`, `/dev/`, `/lib/skills/` |
| 设备名 | 全小写，连字符分隔 | `/dev/llm/claude`, `/dev/shell` |
| PID 段 | 纯数字 | `/proc/42/status` |
| Skill 名 | 全小写，连字符分隔 | `/lib/skills/code-analysis/` |
| Agent 名 | 全小写，连字符分隔 | `/lib/agents/code-analyst/` |

**文件与目录命名：**

| 对象 | 规则 | 示例 |
|------|------|------|
| Go 源文件 | 全小写，下划线分隔 | `kernel.go`, `claude_cli.go`, `astrace.go` |
| 测试文件 | `_test.go` 后缀，同目录 | `kernel_test.go`, `claude_cli_test.go` |
| YAML 配置 | 全小写，连字符分隔，`.yaml` 后缀 | `agent.yaml`（不用 `.yml`） |
| SKILL.md | 大写固定名 | `SKILL.md`（Agent Skills 标准要求） |
| Markdown | 全小写，连字符分隔 | `instructions.md` |
| 目录名 | 全小写单词 | `kernel/`, `drivers/`, `internal/ui/` |

## 结构模式（Structure Patterns）

**依赖方向（严格单向）：**

```
cmd/ → kernel/ → vfs/     → drivers/
                → context/
                → agents/  → skills/
cmd/ → debug/  → kernel/（仅类型）
cmd/ → internal/ui/
```

**禁止的依赖：**
- `kernel/` 不导入 `cmd/` 或 `internal/ui/`
- `vfs/` 不导入 `kernel/`（通过接口解耦）
- `drivers/` 不导入 `kernel/`（通过接口解耦）
- `skills/` 不导入 `agents/`（单向：agents → skills）
- 任何包不导入 `cmd/rnix/`

**文件组织规则：**

| 规则 | 说明 |
|------|------|
| 每文件单一职责 | `kernel.go` = Kernel 结构体 + Spawn + reasonStep；`process.go` = Process + 状态机 |
| 接口定义在使用方 | `LLMDriver` 接口定义在 `drivers/llm/driver.go` |
| 共享类型独立文件 | PID, FD, ErrCode 等放在 `kernel/types.go` |
| 测试同目录 | `kernel/kernel_test.go` 与 `kernel/kernel.go` 同目录 |
| 内部包隔离 | UI 组件放 `internal/ui/`，不可被外部导入 |

## 格式模式（Format Patterns）

**JSON 输出格式（`--json` flag）：**

```go
// 统一 JSON 响应包装（泛型）
type JSONResponse[T any] struct {
    OK    bool       `json:"ok"`
    Data  T          `json:"data,omitempty"`
    Error *JSONError `json:"error,omitempty"`
}

type JSONError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Syscall string `json:"syscall,omitempty"`
    Device  string `json:"device,omitempty"`
}
```

**JSON 字段命名：** 全部 `snake_case`（Go 结构体用 PascalCase + json tag）

**时间格式：**
- JSON 中：毫秒整数（`elapsed_ms: 6200`）
- 终端显示：人类可读（`6.2s`、`100ms`）
- 日志中：RFC3339（`2026-02-23T14:30:00Z`）

**agent.yaml 格式：** 字段名全小写 `snake_case`，列表用序列语法 `- item`，缩进 2 空格。

**SKILL.md 格式（Agent Skills 标准）：** YAML frontmatter（`---` 包裹）+ Markdown body。frontmatter 字段名全小写连字符分隔（`allowed-tools`），body 为程序性知识指令。

## 通信模式（Communication Patterns）

**SyscallEvent 事件命名：** Syscall 字段值与接口方法名完全一致（`"Spawn"`, `"Open"`, `"CtxWrite"`）。

**astrace 输出格式（终端）：**

```
[  0.000] Spawn("分析代码", agent="code-analyst")       = PID(1)        12ms
[  0.012] CtxAlloc(4096)                          = CtxID(1)       0ms
[  0.013] Open("/dev/llm/claude", O_RDWR)         = FD(3)          1ms
```

**日志格式（logfmt 风格）：**

```
[kernel] level=info msg="process spawned" pid=1 intent="分析代码" agent="code-analyst" skills=["code-analysis"]
[kernel] level=error msg="llm timeout" pid=1 device="/dev/llm/claude" elapsed=30s
```

级别：`debug`, `info`, `warn`, `error`。前缀：`[组件名]`。

**Channel 使用规则：**

| 规则 | 说明 |
|------|------|
| DebugChan 缓冲 256 | 防止 syscall 阻塞在写入 |
| Done 缓冲 1 | 确保写入不阻塞 |
| nil channel 检查 | 写入前 `if ch != nil`，零开销跳过 |
| 关闭责任在生产者 | DebugChan 由进程退出时关闭 |

**IPC Method 命名规范：**

**规则：** 所有 IPC method 使用 `snake_case`，与 syscall 命名风格一致。

**已确定的 method 清单：**

| 阶段 | Method | 说明 |
|------|--------|------|
| Phase 1 | `spawn`, `kill`, `wait`, `ps`, `attach_debug` | 核心进程管理 + astrace |
| Phase 2 | `compose_up`, `compose_down` | Compose 引擎 |
| Phase 2 | `skill_install`, `skill_search`, `skill_list`, `skill_update` | Skill 包管理 |
| Phase 2 | `send_signal`, `send_msg`, `recv_msg` | 信号 + IPC 消息 |
| Phase 3 | `attach_agdb`, `record_start`, `record_stop`, `replay` | 调试工具链 |
| Phase 3 | `apply_intent`, `intent_status` | 声明式意图 |

## 过程模式（Process Patterns）

**错误处理模式：**

```go
// ✅ 正确：包装为 SyscallError
func (k *KernelImpl) Open(path string, flags int) (FD, error) {
    file, err := k.devRegistry.Get(path)
    if err != nil {
        return 0, &SyscallError{Syscall: "Open", PID: k.currentPID(), Device: path, Err: err, Code: ErrNotFound}
    }
    // ...
}

// ❌ 错误：丢失上下文
func (k *KernelImpl) Open(path string, flags int) (FD, error) {
    file, err := k.devRegistry.Get(path)
    if err != nil {
        return 0, err  // 丢失 syscall 名称、PID、设备路径
    }
}
```

**context.Context 传播规则：**

| 规则 | 说明 |
|------|------|
| Kernel 方法不接受 ctx 参数 | 使用 `Process.cancel` 控制生命周期 |
| Driver 方法接受 ctx 参数 | `LLMDriver.Call(ctx, req)` 支持取消 |
| 外部调用必须带 timeout | `exec.CommandContext(ctx)` 中 ctx 必须有 deadline |
| cancel() 后等待 wg.Done() | 确保所有子 goroutine 退出后再转 Dead |

**进程状态转移规则：**

```
合法转移：
  Created  → Running    （reasonStep 开始执行）
  Running  → Zombie     （正常完成 / 错误 / 超时 / kill）
  Zombie   → Dead       （wait 回收 + 资源释放）

非法转移（禁止）：
  Running  → Created
  Zombie   → Running
  Dead     → 任何状态
```

**资源释放顺序：** cancel() → wg.Wait() → 关闭 FD → 关闭 DebugChan → CtxFree → 状态转 Dead → 移除进程表。

## Compose 引擎模式

**失败策略：**
- 默认 `fail-all`：任意节点失败 → SignalGroup(SIGTERM) 全组 → Compose 状态标记 Failed
- 可选 `fail-fast`：仅停止依赖失败节点的下游，独立分支继续执行
- 配置方式：`rnix-compose.yaml` 顶层字段 `failure_strategy: fail-all | fail-fast`

**全生命周期路径：**
- `compose up` → 创建 ProcGroup → DAG 拓扑排序 → 按层级并行 Spawn → 等待全部完成/失败
- `compose down` → SignalGroup(SIGTERM) → 等待超时 → 强制 Kill 残留 → 释放 ProcGroup
- 异常路径：节点 Zombie → 触发 failure_strategy → 回调通知 → 更新 Compose 状态

## AgentShell 语法模式

**保留关键字（Phase 2）：**
- `spawn`、`export`、`if`、`else`、`on-error`、`end`
- Phase 3 追加：`for`、`in`、`while`、`fn`、`return`、`parallel`、`source`、`wait`、`sleep`、`exit`

**引号规则：**
- 双引号 `"..."` 内支持变量插值（`${VAR}`）
- 单引号 `'...'` 内为纯文本，不展开变量
- 无引号的文本按空格分词

**管道语义：**
- 默认 `pipefail`：管道中任意节点失败 → 整条管道标记失败
- 管道数据流：前一进程的 `result`（文本输出）自动注入后一进程的上下文
- 管道错误传播：失败节点的 ExitCode + ErrorMessage 传递给 `on-error` 处理器

## 文件持久化路径模式

**统一根目录：** `$PROJECT/.rnix/`

**子目录命名规则：** 全小写 + 连字符分隔

| 路径 | 用途 | 阶段 |
|------|------|------|
| `$PROJECT/.rnix/records/<pid>-<timestamp>/` | 时间旅行录制 | Phase 3 |
| `$PROJECT/.rnix/reputation/` | Agent 声誉数据 | Phase 3 |
| `$PROJECT/.rnix/immune/` | 行为基线 + 威胁记忆 | Phase 3 |
| `$PROJECT/.rnix/traces/` | 分布式追踪数据 | Phase 3 |
| `$PROJECT/.rnix/tests/` | agtest 测试结果缓存 | Phase 3 |

**运行时文件：**
- Socket：`$XDG_RUNTIME_DIR/rnix/rnix.sock`（备选 `/tmp/rnix-$UID/rnix.sock`）
- PID：socket 目录下 `rnix.pid`
- 缓存：`$RNIX_CACHE/registry.json`（Skill 本地注册表）

## Skill 元数据扩展模式

**SKILL.md frontmatter 扩展规则：**
- 只追加新字段，不修改已有字段的语义
- 新字段必须有合理的零值默认（未声明 = 不启用）
- 解析器忽略未知字段（前向兼容）

**已规划的 frontmatter 字段演进：**

| 字段 | 阶段 | 默认值 | 说明 |
|------|------|--------|------|
| `name` | Phase 1 | 必填 | Skill 名称 |
| `description` | Phase 1 | 必填 | Skill 描述 |
| `allowed-tools` | Phase 1 | `[]` | 工具白名单 |
| `version` | Phase 2 | `"0.0.0"` | SemVer 版本号 |
| `requires` | Phase 2 | `[]` | 依赖的其他 Skill |
| `synergy` | Phase 3 | `[]` | 组合涌现声明 |
| `tags` | Phase 2 | `[]` | 搜索标签 |
| `author` | Phase 2 | `""` | 作者信息 |

## 泛型使用模式（Generics Patterns）

| 场景 | 用泛型 | 说明 |
|------|--------|------|
| Registry | ✅ | 设备、驱动、Agent、Skill 共享泛型 `Registry[T]` |
| 并发 Map | ✅ | `SyncMap[K, V]` 替代 `sync.Mutex + map` |
| Future/Result | ✅ | 类型安全异步返回和错误处理链 |
| JSON 响应 | ✅ | `JSONResponse[T]` |
| 配置加载 | ✅ | `LoadYAML[T](path) (T, error)` agent.yaml 和配置文件反序列化；`ParseSKILLMD(path)` SKILL.md frontmatter 解析 |
| Kernel 接口 | ❌ | 方法签名固定 |
| Process 结构体 | ❌ | 单一具体类型 |
| SyscallEvent | ❌ | 需运行时类型灵活性 |

**泛型命名：** 领域类型用语义参数名（`Registry[Item]`, `SyncMap[K, V]`），通用工具允许 `T`。

## 强制执行指南

**所有 AI Agent 必须遵循：**

1. 严格遵循命名表，不自创格式
2. 禁止反向依赖（`golangci-lint` 检查）
3. 所有 syscall 实现必须返回 `*SyscallError`，不允许裸 error
4. 所有 syscall 入口/出口必须写入 SyscallEvent（DebugChan 非 nil 时）
5. 进程状态转移必须遵循合法转移表
6. 资源释放必须按指定顺序
7. 注册表、并发 Map、Future、JSON 包装必须用泛型
8. 所有 JSON 输出字段用 snake_case

**模式验证：** `go vet` + `golangci-lint` + `go test -race` + 代码审查 checklist。
