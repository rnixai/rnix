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
| Provider 设备 | `/dev/llm/` + provider 名 | `/dev/llm/ollama`, `/dev/llm/groq` |
| PID 段 | 纯数字 | `/proc/42/status` |
| Skill 名 | 全小写，连字符分隔 | `/lib/skills/code-analysis/` |
| Agent 名 | 全小写，连字符分隔 | `/lib/agents/code-analyst/` |

**文件与目录命名：**

| 对象 | 规则 | 示例 |
|------|------|------|
| Go 源文件 | 全小写，下划线分隔 | `kernel.go`, `claude_cli.go`, `strace.go` |
| 测试文件 | `_test.go` 后缀，同目录 | `kernel_test.go`, `claude_cli_test.go` |
| YAML 配置 | 全小写，连字符分隔，`.yaml` 后缀 | `agent.yaml`（不用 `.yml`） |
| Provider 配置 | 全小写，连字符分隔，`.yaml` 后缀 | `rnix-providers.yaml` |
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

**strace 输出格式（终端）：**

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
| Phase 1 | `spawn`, `kill`, `wait`, `ps`, `attach_debug` | 核心进程管理 + strace |
| Phase 2 | `compose_up`, `compose_down` | Compose 引擎 |
| Phase 2 | `skill_install`, `skill_search`, `skill_list`, `skill_update` | Skill 包管理 |
| Phase 2 | `send_signal`, `send_msg`, `recv_msg` | 信号 + IPC 消息 |
| Phase 3 | `attach_gdb`, `record_start`, `record_stop`, `replay` | 调试工具链 |
| Phase 3 | `apply_intent`, `intent_status` | 声明式意图 |

## LLM Serve Gateway 模式

**HTTP 端点命名：** 遵循 OpenAI API 路径规范，使用 `/v1/` 前缀。

| 端点 | 方法 | 对应 OpenAI API |
|------|------|----------------|
| `/v1/chat/completions` | POST | Chat Completion |
| `/v1/models` | GET | Models List |
| `/health` | GET | 内部健康检查（非 OpenAI 标准） |

**model 参数解析规则：**

```go
// parseModel 将 OpenAI model 字段解析为 provider + model
// 格式：[provider:]model
// "ollama:llama3"  → provider="ollama", model="llama3"
// "ollama"         → provider="ollama", model="" (使用 default_model)
// "claude"         → provider="claude", model="" (使用 default_model)
func parseModel(model string) (provider, modelName string)
```

**SSE 流式输出规则：**

| 规则 | 说明 |
|------|------|
| Content-Type | `text/event-stream` |
| 事件格式 | `data: {json}\n\n`（每个 chunk 一行） |
| 终止标记 | `data: [DONE]\n\n` |
| Flush 时机 | 每个 chunk 后立即 Flush |
| 超时处理 | 使用 `r.Context()` 传播客户端断开 |

**错误响应格式（OpenAI 兼容）：**

```go
type OpenAIError struct {
    Error struct {
        Message string `json:"message"`
        Type    string `json:"type"`
        Code    string `json:"code"`
    } `json:"error"`
}
```

| 场景 | HTTP 状态码 | error.type | error.code |
|------|------------|------------|------------|
| provider 不存在 | 404 | `invalid_request_error` | `model_not_found` |
| 请求体格式错误 | 400 | `invalid_request_error` | `invalid_request` |
| LLM 驱动超时 | 504 | `server_error` | `timeout` |
| LLM 驱动内部错误 | 502 | `server_error` | `upstream_error` |
| 并发限制 | 429 | `rate_limit_error` | `rate_limit_exceeded` |

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
- Provider 配置：`rnix-providers.yaml`（项目根目录或 `$XDG_CONFIG_HOME/rnix/`）

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

---

## 配置系统实现模式（Epic 23 增量）

_以下模式补充上述 25 个通用模式，确保 AI Agent 实现配置系统时行为一致。_

### 命名模式（Config 专项）

**包与导入别名：**

| 对象 | 规则 | 示例 | 反例 |
|------|------|------|------|
| 包路径 | `internal/config` | `import "github.com/rnixai/rnix/internal/config"` | `internal/cfg` |
| 导入别名 | 不用别名（`config` 无 stdlib 冲突） | `config.GlobalDir()` | `rnixconfig.GlobalDir()` |
| 嵌入包引用 | 根包别名 `rnix` | `import rnix "github.com/rnixai/rnix"` | `import root "..."` |

**配置文件命名（新旧对照）：**

| 旧名称（根目录） | 新名称（.rnix/ 或 ~/.config/rnix/） | 说明 |
|------------------|--------------------------------------|------|
| `rnix-providers.yaml` | `providers.yaml` | 去 `rnix-` 前缀 |
| `rnix-init.yaml` | `init.yaml` | 去 `rnix-` 前缀 |
| `rnix-compose.yaml` | `compose.yaml` | 去 `rnix-` 前缀 |
| `mcp.yaml` | `mcp.yaml` | 无前缀，保持原名 |
| N/A | `config.yaml` | 新增：通用配置 |

**Config API 函数命名：**

| 函数 | 命名规则 | 说明 |
|------|---------|------|
| 路径解析 | 名词/形容词 | `GlobalDir()`, `ProjectDir(dir)`, `ResolvePath(...)` |
| 合并操作 | 动词 + 对象 | `DeepMergeYAML(base, override)` |
| 查找操作 | 动词 + 语义 | `ShadowResolve(name, dirs...)`, `ListMerged(dirs...)` |
| 嵌入提取 | 动词 + 形容词 | `ExtractEmbedded(...)`, `ExtractEmbeddedForce(...)` |
| 兼容检测 | `Warn` + 对象 | `WarnLegacyFiles(dir)` |

### 结构模式（Config 专项）

**`internal/config/` 文件组织：**

```
internal/config/
├── paths.go          // GlobalDir, ProjectDir, ResolvePath, ResolveDir
├── paths_test.go
├── merge.go          // DeepMergeYAML, ShadowResolve, ListMerged
├── merge_test.go
├── embed.go          // ExtractEmbedded, ExtractEmbeddedForce
├── embed_test.go
├── compat.go         // WarnLegacyFiles, LegacyFiles
├── compat_test.go
├── types.go          // Scope, GlobalConfig, ProjectConfig
└── doc.go            // package doc
```

**每文件职责边界（禁止越界）：**

| 文件 | 负责 | 禁止 |
|------|------|------|
| `paths.go` | 纯路径计算 | 不读 YAML、不解析内容 |
| `merge.go` | 纯数据结构合并 | 不做 I/O、不读文件 |
| `embed.go` | 嵌入资源提取到磁盘 | 不做路径解析 |
| `compat.go` | 旧文件检测 + 警告输出 | 不做迁移（迁移在 cmd/ 中） |
| `types.go` | 类型定义 | 不含任何函数实现 |

**依赖方向扩展：**

```
internal/config → (标准库 only: os, path/filepath, io/fs, embed, fmt)
    ↑
agents/loader.go ← 调用 ShadowResolve, ListMerged
skills/loader.go ← 调用 ShadowResolve, ListMerged
drivers/llm/config.go ← 调用 GlobalDir, ResolvePath, DeepMergeYAML
kernel/init.go ← 调用 ResolvePath
cmd/rnix/main.go ← 调用 ProjectDir, WarnLegacyFiles, GlobalConfig/ProjectConfig
cmd/rnix/init.go ← 调用 ExtractEmbedded, GlobalDir
cmd/rnix/migrate.go ← 调用 WarnLegacyFiles, ResolvePath
```

### 格式模式（Config 专项）

**ProjectDir() 返回值约定：**

| 情况 | 返回值 | 调用方行为 |
|------|--------|-----------|
| 找到 `.rnix/` | `("/path/to/project", nil)` | 正常加载项目级配置 |
| 未找到 | `("", nil)` | 仅使用全局配置，不报错 |
| stat 系统调用失败 | `("", err)` | 记录错误日志，降级为仅全局配置 |

**规则：空字符串 ≠ 错误。** 调用方用 `if projectDir != ""` 判断是否在项目中，不要用 `err != nil`。

**配置合并结果不可变性：**

```go
// ✅ 正确：ProjectConfig 字段全部为值类型或不可变引用
type ProjectConfig struct {
    ProjectDir  string              // 值类型，不可变
    Providers   *llm.ProvidersConfig // 指针指向新分配的副本，不共享
    AgentDirs   []string            // 新分配的 slice，不与 GlobalConfig 共享底层数组
}

// ❌ 错误：直接引用 GlobalConfig 的 slice
cfg.AgentDirs = globalCfg.AgentDirs  // 共享底层数组，可能被修改
```

**embed.FS 路径前缀处理：**

```go
// embed.FS 保留目录结构，WalkDir 的 path 带前缀
// //go:embed lib/agents → fsys 中路径为 "lib/agents/code-analyst/agent.yaml"
// 必须用 srcRoot="lib/agents" 剥离前缀

// ✅ 正确
relPath, _ := filepath.Rel("lib/agents", path)  // "code-analyst/agent.yaml"
targetPath := filepath.Join(targetDir, relPath)

// ❌ 错误
targetPath := filepath.Join(targetDir, path)  // 会多出 "lib/agents/" 前缀
```

### 过程模式（Config 专项）

**配置加载错误处理分级：**

| 严重程度 | 场景 | 处理方式 |
|---------|------|---------|
| Fatal | 全局 `providers.yaml` YAML 语法错误 | daemon 启动失败，输出详细错误 |
| Warning | 全局 `providers.yaml` 不存在 | 使用内置默认配置 + 输出 info 日志 |
| Warning | 项目 `providers.yaml` 不存在 | 仅使用全局配置，静默 |
| Warning | 检测到旧文件 `rnix-providers.yaml` | stderr 输出 deprecation warning |
| Error | 项目 `providers.yaml` YAML 语法错误 | spawn 失败，返回 IPC 错误 |
| Info | `rnix init` 跳过已存在文件 | 输出 "skipped: file exists" |

**Deprecation Warning 统一格式：**

```
⚠️  Deprecated: rnix-providers.yaml found in project root.
    Run 'rnix migrate' to move to .rnix/providers.yaml
```

规则：输出到 stderr，`RNIX_ASCII=1` 时用 `WARNING:` 前缀，每个旧文件每次 CLI 调用只输出一次。

**`rnix init` 幂等性规则：**

| 操作 | 目标已存在 | 行为 |
|------|-----------|------|
| 创建 `~/.config/rnix/` | 目录存在 | 跳过，不报错 |
| 提取 agent | 同名目录存在 | 跳过整个目录 |
| 提取 skill | 同名 `SKILL.md` 存在 | 跳过文件 |
| 创建 `.rnix/` | 目录存在 | 跳过，不报错 |
| 写 `providers.yaml` | 文件存在 | 跳过，输出 "skipped" |

### 测试模式（Config 专项）

**Config 测试 helper 标准：**

```go
// ✅ 正确：使用 t.TempDir() 创建隔离环境
func TestProjectDir_Found(t *testing.T) {
    root := t.TempDir()
    os.MkdirAll(filepath.Join(root, "sub", "deep", ".rnix"), 0755)

    got, err := config.ProjectDir(filepath.Join(root, "sub", "deep"))
    require.NoError(t, err)
    require.Equal(t, filepath.Join(root, "sub", "deep"), got)
}

// ✅ 正确：使用 t.Setenv() mock XDG 变量
func TestGlobalDir_XDG(t *testing.T) {
    tmp := t.TempDir()
    t.Setenv("XDG_CONFIG_HOME", tmp)

    got, err := config.GlobalDir()
    require.NoError(t, err)
    require.Equal(t, filepath.Join(tmp, "rnix"), got)
}
```

**DeepMergeYAML 测试覆盖矩阵：** 必须覆盖空 map、单侧有值、嵌套递归、类型冲突（map vs scalar）、slice 替换、三层以上深度。

### 配置系统强制执行指南（增量）

**AI Agent 额外必须遵循：**

9. 所有配置路径解析必须调用 `config.GlobalDir()` / `config.ProjectDir()` / `config.ResolvePath()`，禁止直接拼接路径
10. `ProjectDir()` 返回空字符串时用 `if projectDir != ""` 判断，禁止 `err != nil` 判断
11. `ProjectConfig` 创建后禁止修改任何字段——如需变更，创建新快照
12. embed.FS 提取时必须用 `filepath.Rel(srcRoot, path)` 剥离前缀
13. Deprecation warning 输出到 stderr，格式遵循统一模板
14. `rnix init` 所有操作必须幂等——已存在则跳过
15. 测试中配置路径必须用 `t.TempDir()` + `t.Setenv()`，禁止依赖真实 `$HOME`
16. YAML 配置合并用 `DeepMergeYAML`，Agent/Skill 查找用 `ShadowResolve`，禁止混用

---

### 统一观察系统实现模式（Unified Observation System 增量）

### 命名模式（观察系统专项）

| 组件 | 命名规范 | 示例 |
|------|---------|------|
| StepRecord 类型 | `types.StepRecord` | `internal/types/step_record.go` |
| JSONL 写入器 | `StepWriter` | `kernel/step_writer.go` |
| 磁盘存储路径 | `.rnix/data/steps/<uuid>/steps.jsonl` | `.rnix/data/steps/01968a3e-.../steps.jsonl`（Decision 27: UUID 替代 PID） |
| 进程元数据文件 | `.rnix/data/steps/<uuid>/process-meta.json` | reaper 清理前写入（Decision 27: UUID 替代 PID） |
| IPC 方法名 | `get_step_detail` | 小写下划线，与现有方法一致 |
| CLI 命令 | `rnix dashboard` | 已有命令，增强时间线窗格 |
| CLI Flag | `--dashboard` | `rnix spawn --dashboard "意图"` |

### 结构模式（观察系统专项）

**StepWriter 标准实现：**

```go
// ✅ 正确：每步完成时 append + flush
type StepWriter struct {
    file   *os.File
    writer *bufio.Writer
    mu     sync.Mutex
}

func (w *StepWriter) WriteStep(rec types.StepRecord) error {
    w.mu.Lock()
    defer w.mu.Unlock()
    data, err := json.Marshal(rec)
    if err != nil {
        return err
    }
    if _, err := w.writer.Write(data); err != nil {
        return err
    }
    if err := w.writer.WriteByte('\n'); err != nil {
        return err
    }
    return w.writer.Flush() // 每步 flush，保证读取端可见
}

// ✅ 正确：读取端独立打开文件，无需加锁
func ReadStep(path string, targetStep int) (*types.StepRecord, error) {
    f, err := os.Open(path)
    // 顺序扫描至目标 step...
}
```

**Messages 深拷贝规则：**

```go
// ✅ 正确：复用 BuildPrompt 已有的拷贝
promptResult, _ := k.ctxMgr.BuildPrompt(proc.CtxID)
// promptResult.Messages 已经是深拷贝，直接赋值给 StepRecord
rec.Messages = promptResult.Messages

// ❌ 错误：从 Context 再做一次拷贝（浪费）
msgs, _ := k.ctxMgr.GetMessages(proc.CtxID)
rec.Messages = deepCopy(msgs)
```

**FinalSystemPrompt 捕获时机：**

```go
// ✅ 正确：在 reasonStep 循环中构建完整 sysPrompt 后保存
sysPrompt := promptResult.SystemPrompt
sysPrompt += proc.generatedProtocol // 注入 protocol
sysPrompt += skillsSection          // 注入 skills
proc.mu.Lock()
if proc.FinalSystemPrompt == "" {
    proc.FinalSystemPrompt = sysPrompt // 首次保存
}
proc.mu.Unlock()

// ❌ 错误：在 Spawn 时保存（此时 protocol/skills 尚未注入）
```

### 过程模式（观察系统专项）

**StepRecord 捕获流程（在 reasonStep 循环内）：**

```
1. BuildPrompt() → promptResult（Messages 已拷贝）
2. 保存 FinalSystemPrompt（首次）
3. 构建 LLM 请求 → 发送 → 接收响应
4. 解析 action type
5. 执行工具（如果是 tool_call）
6. 组装 StepRecord{Messages, RawResponse, ToolResult, ...}
7. stepWriter.WriteStep(rec)
8. OnStepComplete 回调
9. AppendMessage 写回 Context
```

注意：步骤 7（写 StepRecord）必须在步骤 9（修改 Context）之前，确保 Messages 快照反映的是该步 LLM 实际看到的输入。

**进程生命周期与数据保留：**

```
Created → Running → Zombie → Dead → Reaped
                                      │
                                      ├─ 写入 process-meta.json
                                      └─ steps/ 目录保留 7 天（可配置）
```

### 测试模式（观察系统专项）

**StepWriter 测试 helper：**

```go
func TestStepWriter_AppendAndRead(t *testing.T) {
    dir := t.TempDir()
    w, err := NewStepWriter(dir, 1)
    require.NoError(t, err)
    defer w.Close()

    rec := types.StepRecord{Step: 1, Action: "tool_call", Summary: "test"}
    require.NoError(t, w.WriteStep(rec))

    // 验证可立即读取（flush 生效）
    got, err := ReadStep(filepath.Join(dir, "steps.jsonl"), 1)
    require.NoError(t, err)
    require.Equal(t, "tool_call", got.Action)
}
```

**并发读写测试：**

```go
func TestStepWriter_ConcurrentReadWrite(t *testing.T) {
    // 写入 goroutine 持续 append
    // 读取 goroutine 持续扫描
    // 验证读取端永远看到完整行（不会读到半截 JSON）
}
```

### 观察系统强制执行指南（增量）

**AI Agent 额外必须遵循：**

17. reasonStep 中 StepRecord 的 Messages 必须来自 `BuildPrompt()` 返回的拷贝，禁止从 Context Manager 二次读取
18. StepWriter.WriteStep() 必须在 `AppendMessage()` 之前调用，确保快照一致性
19. FinalSystemPrompt 仅在首次 reasonStep 中保存，后续步骤不覆盖（除非有 gdb override 变更）
20. GetStepDetail handler 读取 steps.jsonl 时必须独立 open 文件，禁止复用 StepWriter 的 file handle
21. steps.jsonl 中每行必须是完整的 JSON 对象，禁止跨行写入
22. 进程 reap 前必须将 FinalSystemPrompt + NativeToolDefs 写入 process-meta.json

---

### 进程标识体系实现模式（Process Identity System 增量）

### 命名模式（PID/UUID 专项）

| 组件 | 命名规范 | 示例 |
|------|---------|------|
| Process UUID 字段 | `UUID string` | `proc.UUID` |
| 反向索引表 | `byUUID` | `*xsync.SyncMap[string, types.PID]` |
| UUID 生成函数 | `uuid.NewV7()` | `github.com/google/uuid` |
| 磁盘存储路径 | `.rnix/data/steps/<uuid>/` | `.rnix/data/steps/01968a3e-.../steps.jsonl` |
| 进程元数据文件 | `.rnix/data/steps/<uuid>/process-meta.json` | reaper 清理前写入 |
| IPC 请求 UUID 字段 | `UUID string \`json:"uuid,omitempty"\`` | 可选字段，omitempty |
| CLI 输出 UUID 列 | `UUID` 列头 | `rnix ps` 输出末列 |

**UUID 显示截断规则：**

| 场景 | 格式 | 示例 |
|------|------|------|
| `rnix ps` 列表 | 前 8 字符 | `01968a3e` |
| `rnix spawn` 输出 | 完整 36 字符 | `01968a3e-7b2c-7000-...` |
| 日志/strace | 前 8 字符 | `[01968a3e]` |
| IPC 传输 | 完整 36 字符 | 不截断 |

### 结构模式（PID/UUID 专项）

**ProcessTable 双向索引标准实现：**

```go
// ✅ 正确：双表同步维护
func (k *KernelImpl) Spawn(...) (types.PID, error) {
    pid := k.nextPID()
    procUUID := uuid.NewV7().String()

    proc := &Process{
        PID:  pid,
        UUID: procUUID,
        // ...
    }

    k.procs.Store(pid, proc)
    k.procsByUUID.Store(procUUID, pid)
    return pid, nil
}

// ✅ 正确：reaper 双表同步删除
func (k *KernelImpl) reapProcess(pid types.PID) {
    proc, ok := k.procs.Load(pid)
    if !ok { return }

    // 写入 process-meta.json 到 UUID 路径
    k.writeProcessMeta(proc)

    k.procsByUUID.Delete(proc.UUID)
    k.procs.Delete(pid)
}

// ❌ 错误：只删一张表
func (k *KernelImpl) reapProcess(pid types.PID) {
    k.procs.Delete(pid)  // byUUID 泄漏！
}
```

**UUID 查询解析（IPC handler）：**

```go
// ✅ 正确：UUID 优先，PID 回退
func (s *Server) resolveProcess(req GetStepDetailRequest) (*Process, error) {
    if req.UUID != "" {
        pid, ok := s.kernel.procsByUUID.Load(req.UUID)
        if !ok {
            return nil, fmt.Errorf("process uuid %s not found", req.UUID)
        }
        return s.kernel.procs.Load(pid)
    }
    return s.kernel.procs.Load(req.PID)
}

// ❌ 错误：遍历进程表查 UUID
func (s *Server) resolveProcess(req GetStepDetailRequest) (*Process, error) {
    var found *Process
    s.kernel.procs.Range(func(_ types.PID, p *Process) bool {
        if p.UUID == req.UUID { found = p; return false }
        return true
    })  // O(n) 遍历，禁止
}
```

### 过程模式（PID/UUID 专项）

**StepWriter 路径初始化：**

```go
// ✅ 正确：用 UUID 作为目录名
func NewStepWriter(dataDir string, procUUID string) (*StepWriter, error) {
    dir := filepath.Join(dataDir, "steps", procUUID)
    os.MkdirAll(dir, 0755)
    f, err := os.OpenFile(filepath.Join(dir, "steps.jsonl"),
        os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    // ...
}

// ❌ 错误：用 PID 作为目录名
func NewStepWriter(dataDir string, pid types.PID) (*StepWriter, error) {
    dir := filepath.Join(dataDir, "steps", fmt.Sprintf("%d", pid))
    // daemon 重启后 PID 复用，数据覆盖！
}
```

**Dashboard 同一性验证流程：**

```
刷新周期（每 1s）：
1. IPC 获取进程列表（包含 PID + UUID）
2. if selectedPID != 0 {
3.   查找 selectedPID 对应的进程
4.   if 未找到 || 进程.UUID != selectedUUID {
5.     清除选中状态（进程已死或被 PID 复用替换）
6.   }
7. }
```

### 测试模式（PID/UUID 专项）

**UUID 双索引测试 helper：**

```go
func TestProcessTable_DualIndex(t *testing.T) {
    k := newTestKernel(t)

    pid, err := k.Spawn("test", testAgent, SpawnOpts{})
    require.NoError(t, err)

    // 通过 PID 查到进程
    proc, ok := k.procs.Load(pid)
    require.True(t, ok)
    require.NotEmpty(t, proc.UUID)

    // 通过 UUID 反查 PID
    gotPID, ok := k.procsByUUID.Load(proc.UUID)
    require.True(t, ok)
    require.Equal(t, pid, gotPID)

    // reap 后双表都清理
    k.reapProcess(pid)
    _, ok = k.procs.Load(pid)
    require.False(t, ok)
    _, ok = k.procsByUUID.Load(proc.UUID)
    require.False(t, ok)
}
```

**UUID 唯一性跨重启测试：**

```go
func TestUUID_UniqueAcrossRestart(t *testing.T) {
    // 模拟两次 daemon 生命周期
    k1 := newTestKernel(t)
    pid1, _ := k1.Spawn("test", testAgent, SpawnOpts{})
    proc1, _ := k1.procs.Load(pid1)
    uuid1 := proc1.UUID

    k2 := newTestKernel(t)  // 新 kernel = daemon 重启
    pid2, _ := k2.Spawn("test", testAgent, SpawnOpts{})
    proc2, _ := k2.procs.Load(pid2)
    uuid2 := proc2.UUID

    // PID 可能相同（都是 1），但 UUID 必须不同
    require.NotEqual(t, uuid1, uuid2)
}
```

### 进程标识体系强制执行指南（增量）

**AI Agent 额外必须遵循：**

23. 所有磁盘持久化路径必须使用 `proc.UUID` 而非 `proc.PID`，禁止 `fmt.Sprintf("%d", pid)` 作为目录名
24. `ProcessTable` 双表（byPID + byUUID）必须在 Spawn/Reaper 中同步维护，禁止只操作单表
25. IPC handler 接收到同时包含 PID 和 UUID 的请求时，UUID 优先
26. UUID 查询必须通过 `byUUID` 反向索引 O(1) 查找，禁止 `Range()` 遍历
27. Dashboard 刷新时必须通过 UUID 验证进程同一性，禁止仅靠 PID 判断
28. `rnix ps` 输出的 UUID 列使用前 8 字符截断，完整 UUID 仅在详情/IPC 中展示
