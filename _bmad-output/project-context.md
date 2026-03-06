---
project_name: 'Rnix'
user_name: 'Decker'
date: '2026-02-23'
sections_completed: ['technology_stack', 'language_rules', 'framework_rules', 'testing_rules', 'code_quality', 'workflow_rules', 'critical_rules']
status: 'complete'
rule_count: 75
optimized_for_llm: true
---

# Project Context for AI Agents

_This file contains critical rules and patterns that AI agents must follow when implementing code in this project. Focus on unobvious details that agents might otherwise miss._

---

## Technology Stack & Versions

- **Go 1.26** — 模块路径 `github.com/rnixai/rnix`，单二进制 `cmd/rnix/main.go` 入口
- **Go 1.26 新特性要求**：优先使用 `new(expr)` 初始化结构体、自引用泛型约束、利用 Goroutine Leak Profiler 验证资源释放
- **CLI 框架**：Cobra v1.10.2（`github.com/spf13/cobra`）
- **终端样式**：Charm 生态（lipgloss + bubbles），MVP 仅用 cobra + lipgloss
- **测试**：Go 标准 `testing` + `testify`（assertions/mocks），默认 `-race` 竞态检测
- **Lint**：golangci-lint（errcheck, govet, staticcheck, unused, gosimple）
- **LLM 集成**：Claude Code CLI（`claude -p` + `--output-format stream-json`），不是 Go SDK 调用
- **YAML 后缀**：统一使用 `.yaml`（不用 `.yml`）

## Critical Implementation Rules

### Go 语言特定规则

- **泛型必用场景**：Registry、SyncMap、Future、Result、JSONResponse、LoadYAML 必须用泛型实现，减少 `interface{}` 和类型断言
- **泛型禁用场景**：Kernel 接口（方法签名固定）、Process 结构体（单一具体类型）、SyscallEvent（需运行时灵活性）
- **泛型命名**：领域类型用语义参数名（`Registry[Item]`、`SyncMap[K, V]`），通用工具允许 `T`
- **错误处理**：所有 syscall 必须返回 `*kernel.SyscallError`（含 Syscall/PID/Device/Err/Code），禁止返回裸 `error`
- **错误码**：`ErrTimeout` / `ErrNotFound` / `ErrPermission` / `ErrInternal` / `ErrDriver`，类型为 `ErrCode string`
- **`errors.Unwrap` 支持**：SyscallError 必须实现 `Unwrap() error`
- **PID 分配**：`atomic.AddUint64` 全局递增，不回收
- **并发保护**：进程表用 `SyncMap[PID, *Process]`，设备注册表用 `Registry[T]`
- **goroutine 生命周期**：每进程一个 `context.Context` + `sync.WaitGroup`，退出时严格按顺序释放资源
- **context.Context 传播**：Kernel 方法不接受 ctx 参数（用 Process.cancel 控制），Driver 方法必须接受 ctx 参数支持取消
- **外部命令调用**：必须使用 `exec.CommandContext`，ctx 必须有 deadline
- **模块路径**：`github.com/rnixai/rnix`

### 架构框架规则

#### Syscall ABI 设计
- **分类接口组合**：Kernel = ProcessManager + ContextManager + FileSystem + Debugger，不是单一巨型接口
- **MVP 15 个 syscall**：Spawn/Kill/Wait/GetPID/PS + CtxAlloc/CtxRead/CtxWrite/CtxFree + Open/Read/Write/Close/Stat + DebugRecord
- **Phase 2 扩展**：新增子接口（IPCManager、CapManager、SkillManager），Kernel 嵌入即可，不破坏现有接口

#### VFS 设备模型
- **设备注册在 `cmd/rnix/main.go`**：依赖注入点，所有驱动在此创建和注册
- **VFS 路径约定**：`/proc/{pid}/` 动态进程信息、`/dev/llm/claude` LLM 驱动、`/dev/shell` Shell 驱动、`/dev/fs` 宿主文件系统
- **VFSFile 接口**：所有设备驱动必须实现 `Read/Write(ctx)/Close/Stat` 四个方法，Write 接受 `context.Context` 支持取消传播
- **FD 管理**：每进程独立 `FDTable map[FD]VFSFile`，FD 为进程内递增整数

#### 进程状态机
- **合法状态转移**：Created→Running（reasonStep 开始）、Running→Zombie（完成/错误/超时/kill）、Zombie→Dead（wait 回收）
- **非法转移绝对禁止**：Running→Created、Zombie→Running、Dead→任何状态
- **资源释放顺序**：cancel() → wg.Wait() → 关闭 FD → 关闭 DebugChan → CtxFree → 状态转 Dead → 移除进程表

#### Claude Code CLI 集成
- **调用模式**：每次 LLM 调用 = 一次 `exec.CommandContext`，不维持长连接
- **MVP 参数**：`claude -p <intent> --output-format json --system-prompt <instructions> --model <model>`（不传递 `--max-turns`，让 CLI 使用自身默认值；内核通过 reasonStep 循环自管理推理步骤）
- **stream-json 模式**：`--output-format stream-json`，`bufio.Scanner` 逐行读取 stdout，每行解析为 `StreamEvent`
- **超时处理**：`context.WithTimeout` 包装，超时后 `cmd.Process.Kill()`，进程转 Zombie

#### 依赖方向（严格单向）
- `internal/types/` ← 所有包均可导入（零外部依赖）
- `internal/xsync/` ← 所有包均可导入（仅依赖 internal/types/）
- `cmd/` → kernel/ → vfs/ → drivers/{llm,shell,fs}
- `cmd/` → kernel/ → context/
- `cmd/` → kernel/ → agents/ → skills/
- `cmd/` → agents/ → skills/
- `cmd/` → debug/（仅依赖 internal/types/）
- **绝对禁止**：kernel/ 不导入 cmd/、vfs/ 不导入 kernel/、drivers/ 不导入 kernel/、agents/ 不导入 kernel/、任何包不导入 cmd/rnix/

### 测试规则

- **测试文件位置**：与源文件同目录（Go 惯例），如 `kernel/errors_test.go` 对应 `kernel/errors.go`
- **测试函数命名**：`Test<Type>_<Method>`（如 `TestRegistry_RegisterAndGet`、`TestSyscallError_Unwrap`）
- **竞态检测**：`make test` 默认启用 `-race`，所有并发数据结构必须通过竞态测试
- **并发测试模式**：使用 `sync.WaitGroup` + 多 goroutine 并发访问，验证线程安全（参考 `syncmap_test.go` 的 100 goroutine 并发测试）
- **测试断言**：当前使用 Go 标准 `t.Fatal` / `t.Fatalf` / `t.Errorf`，后续引入 `testify` 后统一用 `assert` / `require`
- **Mock 策略**：通过接口抽象实现可测试性——`LLMDriver`、`VFSFile`、`ProcessInfoProvider` 等接口允许注入 mock 实现
- **Driver 测试**：`exec.Command` 通过注入可替换的 command builder 来 mock，不依赖真实 Claude Code CLI
- **集成测试**：VFS + DeviceRegistry + mock 驱动的组合测试
- **Goroutine 泄漏检测**：利用 Go 1.26 Goroutine Leak Profiler 验证进程退出后无 goroutine 泄漏
- **测试 fixtures**：放在 `testdata/` 子目录（如 `skills/testdata/mock-skill/`）
- **覆盖率**：核心路径（状态机转移、错误传播、syscall 入口/出口）必须有测试覆盖

### 代码质量与风格规则

#### 命名约定
- **包名**：全小写单词，不用下划线（`kernel`、`vfs`、`context`）
- **导出类型**：PascalCase（`Process`、`SyscallEvent`、`LLMDriver`）
- **非导出类型**：camelCase（`pidCounter`、`fdTable`）
- **接口**：名词或 `-er` 后缀（`FileSystem`、`Debugger`、`LLMDriver`），禁止 `I` 前缀（不用 `IFileSystem`）
- **常量**：PascalCase 导出 / camelCase 非导出，错误变量 `Err` 前缀（`ErrNotFound`），禁止全大写下划线（不用 `MAX_TOKENS`）
- **Syscall 命名**：PascalCase 动词（`Spawn`、`Kill`）、`Ctx` 前缀（`CtxAlloc`）、Unix 风格（`Open`、`Read`）、`Debug` 前缀（`DebugRecord`）
- **VFS 路径**：全小写 Unix 风格，连字符分隔（`/dev/llm/claude`、`/lib/skills/code-analyst/`）
- **Go 源文件**：全小写下划线分隔（`claude_cli.go`、`kernel_test.go`）
- **方法接收器**：简短单字母（`r *Registry`、`m *SyncMap`、`k *KernelImpl`）

#### 文件组织
- **每文件单一职责**：`kernel.go` = Kernel + Spawn + reasonStep，`process.go` = Process + 状态机
- **接口定义在使用方**：`LLMDriver` 定义在 `drivers/llm/driver.go`
- **共享类型独立文件**：PID/FD/ErrCode 等放在 `internal/types/types.go`
- **内部包隔离**：UI 组件放 `internal/ui/`，泛型工具放 `internal/xsync/`

#### 输出格式
- **JSON 字段命名**：全部 `snake_case`（Go 用 PascalCase + json tag）
- **统一 JSON 包装**：`JSONResponse[T]`（`ok` + `data` + `error`）
- **时间格式**：JSON 用毫秒整数（`elapsed_ms: 6200`），终端用人类可读（`6.2s`），日志用 RFC3339
- **日志格式**：logfmt 风格 `[组件名] level=info msg="..." key=value`
- **manifest.yaml**：字段名 `snake_case`，缩进 2 空格，列表用序列语法 `- item`

### 开发工作流规则

#### 构建与验证
- **质量门禁**：`make all` = lint → vet → test → build，所有步骤通过后才算构建成功
- **编译目标**：`go build -o rnix ./cmd/rnix/`，单二进制输出
- **安装方式**：`go install ./cmd/rnix/`，用户通过 `go install github.com/rnixai/rnix/cmd/rnix@latest` 安装

#### CLI 命令结构
- **根命令**：`rnix -i "意图"` — spawn 智能体（意图通过 `-i/--intent` flag 传递）
- **子命令**：`strace`、`ps`、`kill`、`version`
- **全局 flags**：`--json`（JSON 输出）、`--verbose`（详细模式）、`--quiet`（静默模式）、`-i/--intent`（意图字符串）
- **输出模式检测**：启动时通过 `TerminalProfile` 检测 TTY/Pipe/JSON，自动切换输出格式

#### Channel 使用规则
- **DebugChan 缓冲 256**：防止 syscall 阻塞在写入
- **Done 缓冲 1**：确保写入不阻塞
- **nil channel 检查**：写入前 `if ch != nil`，零开销跳过
- **关闭责任在生产者**：DebugChan 由进程退出时关闭

#### SyscallEvent 记录
- **所有 syscall 入口/出口必须写入 SyscallEvent**（DebugChan 非 nil 时）
- **Syscall 字段值与接口方法名完全一致**：`"Spawn"`、`"Open"`、`"CtxWrite"`
- **包含耗时**：每个 SyscallEvent 记录 Duration（syscall 执行耗时）

### 关键防错规则

#### 反模式（绝对禁止）
- **禁止返回裸 error**：syscall 实现中 `return 0, err` 丢失 syscall 名称/PID/设备路径，必须包装为 `*SyscallError`
- **禁止反向依赖**：`vfs/` 导入 `kernel/`、`drivers/` 导入 `kernel/` 会产生循环依赖，通过接口解耦
- **禁止非法状态转移**：Running→Created、Zombie→Running、Dead→任何状态，状态机实现必须有守卫检查
- **禁止跳过资源释放步骤**：不能只 cancel() 不 wg.Wait()，不能只转 Dead 不关闭 FD/DebugChan
- **禁止直接用 `sync.Mutex + map`**：已有 `SyncMap[K, V]` 和 `Registry[T]`，使用泛型工具
- **禁止 `.yml` 后缀**：统一 `.yaml`
- **禁止 `I` 前缀接口命名**：不用 `IFileSystem`，用 `FileSystem`
- **禁止全大写常量**：不用 `MAX_TOKENS`，用 `MaxTokens`

#### 边界情况
- **DebugChan 为 nil 时**：跳过事件记录，零开销——写入前必须检查 `if ch != nil`
- **进程退出后 FD 访问**：Dead 状态进程的 FD 已关闭，后续访问必须返回 `ErrNotFound`
- **孤儿进程**：父进程退出后子进程 reparent 到 init（PID=1），不是直接 kill
- **Zombie 进程超时**：进程超时后转 Zombie 而非直接 Dead，必须等 Wait 回收

#### 安全规则
- **Skill tools 白名单**：manifest.yaml 中 `tools` 列表定义允许的 `/dev/` 路径，非白名单路径返回 `ErrPermission`
- **继承用户权限**：`exec.CommandContext` 继承当前用户权限，不做额外提权
- **Claude Code CLI 参数注入**：intent 和 instructions 通过 CLI 参数传递，注意 shell 转义

#### 性能注意
- **strace DebugChan 缓冲 256**：事件过多时消费者跟不上可能丢失，MVP 阶段可接受
- **进程表用 RWMutex（SyncMap 内部）**：读多写少场景优化，不用 sync.Map（类型不安全）
- **stream-json 逐行解析**：不要一次性读完 stdout，`bufio.Scanner` 逐行处理减少内存压力

---

## Usage Guidelines

**For AI Agents:**

- Read this file before implementing any code
- Follow ALL rules exactly as documented
- When in doubt, prefer the more restrictive option
- Update this file if new patterns emerge

**For Humans:**

- Keep this file lean and focused on agent needs
- Update when technology stack changes
- Review periodically for outdated rules
- Remove rules that become obvious over time

Last Updated: 2026-02-23
