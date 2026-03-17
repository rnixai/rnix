---
title: '项目级环境变量与 Provider 配置加载'
slug: 'project-env-provider-config'
created: '2026-03-17'
status: 'implementation-complete'
stepsCompleted: [1, 2, 3, 4, 5, 6, 7, 8, 9]
tech_stack: [Go]
files_to_modify:
  - internal/config/dotenv.go (new)
  - internal/config/dotenv_test.go (new)
  - drivers/llm/factory.go
  - drivers/llm/factory_test.go
  - ipc/protocol.go
  - ipc/server.go
  - cmd/rnix/main.go
  - internal/config/types.go
  - kernel/kernel.go
code_patterns:
  - 'CreateDriverWithEnv accepts envLookup func(string) string'
  - 'resolveProjectContext loads .env files and merges providers via raw YAML bytes DeepMergeYAML'
  - 'SpawnRequest carries RnixEnv field from CLI'
  - 'ProjectConfig.EnvSnapshot stores only .env-sourced vars (not full os.Environ)'
  - 'ProjectConfig.LLMFileOpener callback avoids kernel→llm circular import'
  - 'VFS.RegisterFD bypasses DeviceRegistry for project-level drivers'
test_patterns:
  - 'table-driven tests for dotenv parser edge cases'
  - 'tempdir-based .env file creation for integration tests'
  - 'factory_test with envLookup injection'
  - 'save/restore global vars pattern for test isolation'
---

# Tech-Spec: 项目级环境变量与 Provider 配置加载

**Created:** 2026-03-17

## Overview

### Problem Statement

Daemon 只在启动时加载全局 `~/.config/rnix/providers.yaml` 并通过 `os.Getenv()` 从 daemon 进程环境读取 API Key。项目目录下的 `.env` 文件中定义的环境变量（如 `OPENROUTER_API_KEY`）无法被 daemon 读取，导致 LLM 驱动认证失败（`status 401`）。同时，项目级 `.rnix/providers.yaml` 无法覆盖全局 provider 配置。

Epic 25 已实现 `DeepMergeYAML`、`ShadowResolve` 等配置合并工具，但尚未应用到 provider 配置的项目级覆盖场景。

### Solution

Daemon 在处理 spawn 请求时，根据 `ProjectDir` 加载项目级 `.env` 文件（多环境支持）和项目级 `.rnix/providers.yaml`，与全局配置 merge 后生成项目专属的环境快照和 provider 配置。API Key 解析从 daemon 启动时延迟到 spawn 请求时，使用环境快照而非 `os.Getenv()`。不同项目的环境变量互相隔离，不污染 daemon 进程环境。

### Scope

**In Scope:**

- 自实现 `.env` parser（支持 `KEY=VALUE`、`#` 注释、单/双引号、空行）
- 多环境 `.env` 加载：`.env` → `.env.local` → `.env.{RNIX_ENV}` → `.env.{RNIX_ENV}.local`（后者覆盖前者）
- `RNIX_ENV` 从 CLI 调用者环境获取，默认 `development`
- 环境快照隔离：每次 spawn 请求生成独立的 `map[string]string`，不写入 `os.Setenv`
- 项目 `.rnix/providers.yaml` 与全局 `providers.yaml` 通过 `DeepMergeYAML` 合并（raw YAML bytes 层面）
- Provider/Driver 创建支持使用环境快照解析 `api_key_env`（替代 `os.Getenv`）
- CLI 端通过 IPC 传递 `RNIX_ENV` 值给 daemon
- `.env.local` / `.env.{RNIX_ENV}` / `.env.{RNIX_ENV}.local` 多环境文件支持
- 通过 `VFS.RegisterFD` 绕过全局 DeviceRegistry 注入项目级 LLM driver

**Out of Scope:**

- CLI 端 `.env` 加载（仅 daemon 端加载）
- `.env` 文件变更的热重载/watch
- 变量插值（如 `BASE_URL=$HOST:$PORT`）
- 项目级 driver 的健康检查（全局 driver 已有健康检查，项目级按需创建，生命周期短，不需要）
- 按 ProjectDir 缓存 driver 实例（后续优化）

## Context for Development

### Codebase Patterns

- **Provider 注册是全局一次性的**：`runDaemon()` (main.go:1142-1147) 在 daemon 启动时调用 `RegisterProviders()` 创建所有 driver 实例并注册到 `DriverRegistry` 和 `DeviceRegistry`。API Key 在 `CreateDriver()` (factory.go:44) 中通过 `os.Getenv(cfg.APIKeyEnv)` 读取 — 这是问题根源。
- **项目级 provider 加载已有骨架但不完整**：`resolveProjectContext()` (server.go:1447-1491) 已加载项目 `.rnix/providers.yaml`，但只是**替换**全局配置（不是 merge），且加载的 `ProvidersConfig` 赋给 `ProjectConfig.Providers` 后**从未用于创建新 driver 实例**。
- **SpawnRequest 已有 ProjectDir**：CLI (main.go:467) 已通过 `config.ProjectDir(cwd)` 发现项目目录并传给 daemon。
- **SpawnOpts.ProjectConfig 已传入 Spawn**：kernel (kernel.go:234) 将 `ProjectConfig` 设到 `proc.ProjectConfig`，但仅用于存储，不参与 driver 选择。
- **DeepMergeYAML 已实现**：`internal/config/merge.go` 提供通用 YAML map merge，可用于 providers.yaml 合并。
- **环境快照模式已有先例**：`ExecScriptRequest.Env` (protocol.go:405) 已用 `map[string]string` 传递环境快照给脚本执行。
- **VFS.RegisterFD 已存在**：`vfs/vfs.go:158` 提供 `RegisterFD(pid, file) FD`，专门用于绕过 DeviceRegistry 直接注册 VFSFile 到进程 FD table（已用于 pipe 端点）。
- **kernel 不导入 `drivers/llm`**：kernel 只导入 `vfs`、`config`、`types`、`agents` 等包。不能直接引用 `*llm.DriverRegistry` 类型，需通过接口或回调避免循环依赖。
- **LLM 设备打开流程**：kernel.go:460 `llmFD, err = k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)` — 通过全局 VFS Open 从 DeviceRegistry 查找设备。

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `drivers/llm/factory.go:22-55` | `CreateDriver()` — 使用 `os.Getenv(cfg.APIKeyEnv)` 读 API Key，需改为接受环境快照 |
| `drivers/llm/factory.go:60-85` | `RegisterProviders()` — daemon 启动时一次性注册 |
| `drivers/llm/config.go:29-41` | `ProvidersConfig` / `ProviderConfig` 结构体定义 |
| `drivers/llm/config.go:74-92` | `LoadProvidersConfig()` — YAML 加载和验证 |
| `drivers/llm/registry.go` | `DriverRegistry` — 线程安全的 driver 注册表 |
| `drivers/llm/vfsfile.go` | `FileFactory(driver, path)` — 返回 `VFSFileFactory` |
| `ipc/server.go:672-731` | `handleSpawn()` — 调用 `resolveProjectContext` 后传 `ProjectConfig` 给 kernel |
| `ipc/server.go:1447-1491` | `resolveProjectContext()` — 需扩展：加载 .env、merge providers、创建项目级 driver |
| `ipc/protocol.go:77-88` | `SpawnRequest` — 需增加 `RnixEnv` 字段 |
| `internal/config/types.go:31-40` | `ProjectConfig` — 需增加 `EnvSnapshot` 和 `LLMFileOpener` 字段 |
| `internal/config/merge.go:13-40` | `DeepMergeYAML()` — 已有，用于 providers merge |
| `cmd/rnix/main.go:461-468` | CLI 构造 `SpawnRequest` — 需传 `RNIX_ENV` |
| `cmd/rnix/main.go:1116-1215` | `runDaemon()` — 全局 provider 注册（保持不变） |
| `kernel/kernel.go:193-217` | `resolveLLMDevice()` — 需支持项目级 provider 验证 |
| `kernel/kernel.go:444-469` | `Spawn()` 中 LLM 设备打开 — 需改为项目级 driver 时用 `VFS.RegisterFD` 绕过全局 Open |
| `vfs/vfs.go:158-161` | `VFS.RegisterFD()` — 已有，可直接用于注入项目级 driver 创建的 VFSFile |

### Technical Decisions

1. **避免 kernel → llm 循环依赖：使用回调函数**。`ProjectConfig` 中不存 `*llm.DriverRegistry`，而是存一个 `LLMFileOpener` 回调。**为避免 `config` 包导入 `vfs`（违反包层级方向），回调签名定义在 `internal/types` 中**：`type LLMFileOpener func(provider string, flags int) (any, error)` — 返回 `any`（实际为 `vfs.VFSFile`），调用方在 kernel 中做类型断言。该回调在 `ipc/server.go` 的 `resolveProjectContext` 中构造（闭包捕获项目级 driver），kernel 调用它获取 VFSFile，无需知道 `llm` 包的任何类型。

2. **VFS 设备注入方案：使用已有的 `VFS.RegisterFD`**。当 `ProjectConfig.LLMFileOpener` 非 nil 时，kernel 在 `Spawn` 中不调用 `k.vfs.Open(pid, llmDevice, flags)`，而是：(1) 调用 `LLMFileOpener(provider, int(flags))` 获取 `any`，(2) 类型断言为 `vfs.VFSFile`，(3) 调用 `k.vfs.RegisterFD(pid, file)` 获取 FD。这完全绕过 `DeviceRegistry`，改动集中在 kernel.go:460 附近的 5-10 行代码。注意：`RegisterFD` 后还需更新 `proc.FDTable` 追踪（与现有 `Open` 路径保持一致）。

3. **环境快照只存 `.env` 增量，不拷贝 `os.Environ()`**。`LoadDotenvDir` 返回仅 `.env` 文件中的变量（`map[string]string`）。`NewEnvLookup(dotenvVars)` 返回闭包：先查 dotenvVars 再 fallback `os.Getenv`。`ProjectConfig.EnvSnapshot` 只存 dotenvVars（轻量），不暴露 daemon 全量环境。`envLookup` 用于 `CreateDriverWithEnv`。

4. **Providers merge 在 raw YAML bytes 层面**。不将已解析的 `*ProvidersConfig` 回转为 map。直接读两个文件的 raw bytes → 各自 Unmarshal 为 `map[string]any` → `DeepMergeYAML` → Marshal 为 bytes → Unmarshal 为 `*ProvidersConfig`。全局 providers.yaml 的 raw bytes 在 daemon 启动时缓存。传递方式：`ipc.NewServer()` 新增 `globalProvidersRaw []byte` 参数（或在 `runDaemon` 中通过 setter 注入）。

5. **RNIX_ENV 值验证**：只允许 `^[a-zA-Z0-9_-]+$`（与 provider name 相同的正则），防止路径遍历。

6. **`.env` 文件大小限制**：`LoadDotenvFile` 使用 `io.LimitReader(f, 1<<20)` 限制 1MB。

7. **ProjectDir 安全验证**：`resolveProjectContext` 中验证 `projectDir` 必须包含 `.rnix/` 子目录（`os.Stat(filepath.Join(projectDir, ".rnix"))`），否则视为无项目配置，回退全局模式。这已是现有行为（`config.ProjectDir` 搜索 `.rnix/` 目录），但在 daemon 端做二次验证防止恶意 CLI。

8. **`ExecScriptRequest` 不添加 `RnixEnv`**。脚本执行已有完整的 `Env` 快照传递机制，不需要 daemon 端再加载 `.env`。CLI 端的 `NewEnvironmentFromOS()` 已包含调用者的环境变量。

9. **Pipeline 中 RnixEnv 传递**：`handleSpawnPipeline` 中每个子 spawn 调用 `resolveProjectContext(req.ProjectDir, req.RnixEnv)` 时统一使用 pipeline request 中的 `RnixEnv`。

10. **`.env` 文件解析错误 fail-fast**：文件不存在 → 跳过（返回 nil, nil）。文件存在但解析失败（IO 错误、格式错误）→ 立即返回 error，不继续加载后续 .env 文件。

11. **Fallback provider 兜底逻辑**：`attemptFallback` 中，如果 `LLMFileOpener` 返回 "provider not found"（项目级未覆盖 fallback provider），则 fallback 到全局 `k.vfs.Open`。即：项目级 driver 优先，不存在时用全局 driver。

12. **项目级 default provider**：合并后的 `ProvidersConfig.ResolveDefaultProvider()` 的返回值通过 `ProjectConfig.DefaultProvider string` 字段传递给 kernel。kernel 在 `resolveLLMDevice` 中，当 `ProjectConfig` 非 nil 且 `ProjectConfig.DefaultProvider` 非空时，使用项目级 default provider 替代全局 `k.defaultProvider`。

## Implementation Plan

### Tasks

**Task 1: 实现 `.env` parser** (`internal/config/dotenv.go`, new)

- [x] 1.1 创建 `ParseDotenv(r io.Reader) (map[string]string, error)` 函数
  - File: `internal/config/dotenv.go` (new)
  - Action: 实现逐行解析器，支持：
    - `KEY=VALUE`（无引号）— 值去除首尾空白
    - `KEY="VALUE"`（双引号）— 保留内部空白，支持 `\n`、`\"`、`\\` 转义
    - `KEY='VALUE'`（单引号）— 保留字面值，不转义
    - `KEY=`（空值）
    - `# 注释行` 和行尾 `# 注释`（仅无引号值）
    - 空行忽略
    - `export KEY=VALUE` 前缀（可选 `export` 关键字，忽略）
  - Notes: 不支持变量插值（`$VAR`）。使用 `io.LimitReader(r, 1<<20)` 限制读取 1MB。

- [x] 1.2 创建 `LoadDotenvFile(path string) (map[string]string, error)` 函数
  - File: `internal/config/dotenv.go`
  - Action: 读取文件（经过 `io.LimitReader` 限制），调用 `ParseDotenv`。文件不存在返回 `(nil, nil)` 而非报错。

- [x] 1.3 创建 `ValidateEnvName(name string) bool` 函数
  - File: `internal/config/dotenv.go`
  - Action: 验证 RNIX_ENV 值只匹配 `^[a-zA-Z0-9_-]+$`。
  - Notes: 防止路径遍历（如 `../../etc/passwd`）。

- [x] 1.4 创建 `LoadDotenvDir(dir string, rnixEnv string) (map[string]string, error)` 函数
  - File: `internal/config/dotenv.go`
  - Action: 先用 `ValidateEnvName(rnixEnv)` 验证。`rnixEnv` 为空时默认 `"development"`。按顺序加载并合并：`.env` → `.env.local` → `.env.{rnixEnv}` → `.env.{rnixEnv}.local`。后者覆盖前者。返回仅包含 .env 文件中变量的 map（不含 os.Environ）。

- [x] 1.5 创建 `NewEnvLookup(dotenvVars map[string]string) func(string) string` 函数
  - File: `internal/config/dotenv.go`
  - Action: 返回闭包：先查 `dotenvVars`，未找到则 fallback 到 `os.Getenv`。`dotenvVars` 为 nil 时直接返回 `os.Getenv`。

**Task 2: `.env` parser 单元测试** (`internal/config/dotenv_test.go`, new)

- [x] 2.1 table-driven 测试 `ParseDotenv`
  - File: `internal/config/dotenv_test.go` (new)
  - Action: 覆盖用例：基本 KEY=VALUE、双引号、单引号、空值、注释、export 前缀、转义字符（`\n`、`\"`、`\\`）、空行、行尾注释、多行文件、超过 1MB 截断
- [x] 2.2 测试 `LoadDotenvFile` — 文件存在/不存在/解析错误
- [x] 2.3 测试 `ValidateEnvName` — 合法值（`development`、`prod-1`）、非法值（`../etc`、空字符串、含空格）
- [x] 2.4 测试 `LoadDotenvDir` — 多文件合并顺序、覆盖优先级、非法 rnixEnv 值报错
- [x] 2.5 测试 `NewEnvLookup` — dotenv 变量优先于 os.Getenv、nil map 回退到 os.Getenv

**Task 3: `CreateDriverWithEnv` 支持环境快照** (`drivers/llm/factory.go`)

- [x] 3.1 新增 `CreateDriverWithEnv(cfg ProviderConfig, envLookup func(string) string) (LLMDriver, error)` 函数
  - File: `drivers/llm/factory.go`
  - Action: 从 `CreateDriver` 提取逻辑。在 `DriverOpenAICompat` 分支中，将 `os.Getenv(cfg.APIKeyEnv)` 替换为 `envLookup(cfg.APIKeyEnv)`。`ClaudeCLI` 和 `CursorCLI` 分支不使用 envLookup（它们不需要 API Key）。
- [x] 3.2 将 `CreateDriver` 改为调用 `CreateDriverWithEnv(cfg, os.Getenv)`
  - File: `drivers/llm/factory.go`
  - Action: 保持签名不变，一行委托。

**Task 4: `CreateDriverWithEnv` 测试** (`drivers/llm/factory_test.go`)

- [x] 4.1 测试 envLookup 被正确调用
  - File: 现有 `drivers/llm/factory_test.go` 或新增测试
  - Action: 构造自定义 envLookup（记录调用的 key 并返回 mock value），验证 OpenAI-compat driver 使用 envLookup 读取 API Key。验证 claude-cli driver 不触发 envLookup。

**Task 5: IPC 协议扩展** (`ipc/protocol.go`)

- [x] 5.1 `SpawnRequest` 新增 `RnixEnv` 字段
  - File: `ipc/protocol.go`
  - Action: 在 `SpawnRequest` struct 中 `ProjectDir` 之后添加 `RnixEnv string \`json:"rnix_env,omitempty"\``
- [x] 5.2 `SpawnPipelineRequest` 同样新增 `RnixEnv` 字段
  - File: `ipc/protocol.go`
  - Action: 在 `SpawnPipelineRequest` struct 中添加 `RnixEnv string \`json:"rnix_env,omitempty"\``

**Task 6: 类型定义与 `ProjectConfig` 扩展** (`internal/types`, `internal/config/types.go`)

- [x] 6.1 在 `internal/types` 中定义 `LLMFileOpener` 类型
  - File: `internal/types/types.go`
  - Action: 添加 `type LLMFileOpener func(provider string, flags int) (any, error)` — 返回 `any` 避免 types → vfs 依赖。调用方（kernel）做 `vfs.VFSFile` 类型断言。
- [x] 6.2 新增 `EnvSnapshot`、`LLMFileOpener` 和 `DefaultProvider` 字段
  - File: `internal/config/types.go`
  - Action: 在 `ProjectConfig` struct 中添加：
    ```go
    EnvSnapshot     map[string]string   // .env file vars only (not full os.Environ)
    LLMFileOpener   types.LLMFileOpener // nil = use global VFS Open
    DefaultProvider string              // Merged providers' resolved default; "" = use global
    ```
  - Notes: 不需要 `import "github.com/rnixai/rnix/vfs"`。需新增 `import "github.com/rnixai/rnix/internal/types"`（config 包当前未导入 types）。

**Task 7: CLI 传递 RNIX_ENV** (`cmd/rnix/main.go`)

- [x] 7.1 在 SpawnRequest 构造处添加 RnixEnv
  - File: `cmd/rnix/main.go` — `req := ipc.SpawnRequest{...}` 处
  - Action: 添加 `RnixEnv: os.Getenv("RNIX_ENV")`
- [x] 7.2 在 `runPipeline` 中添加 RnixEnv
  - File: `cmd/rnix/main.go` — `SpawnPipelineRequest` 构造处
  - Action: 添加 `RnixEnv: os.Getenv("RNIX_ENV")`
  - Notes: `ExecScriptRequest` 不添加 RnixEnv，因为它已有完整的 `Env` 快照机制。

**Task 8: 扩展 `resolveProjectContext`** (`ipc/server.go`)

- [x] 8.1 在 `Server` 结构体中添加 `globalProvidersRaw []byte` 字段
  - File: `ipc/server.go`
  - Action: 在 `runDaemon()` 加载 providers.yaml 时，同时保存 raw bytes 到 Server。用于后续 DeepMergeYAML（避免从结构体回转 YAML）。

- [x] 8.2 增加 `rnixEnv` 参数
  - File: `ipc/server.go` — `resolveProjectContext` 签名
  - Action: 改为 `resolveProjectContext(projectDir, rnixEnv string)`

- [x] 8.3 ProjectDir 安全验证
  - File: `ipc/server.go` — `resolveProjectContext` 开头
  - Action: 在现有 `projectDir == ""` 检查之后，增加 `os.Stat(filepath.Join(projectDir, ".rnix"))` 验证。不存在则回退全局模式（返回 nil ProjectConfig）。

- [x] 8.4 加载 `.env` 文件
  - File: `ipc/server.go` — `resolveProjectContext` 内
  - Action: 调用 `config.LoadDotenvDir(projectDir, rnixEnv)` 获取 `dotenvVars`。构造 `envLookup := config.NewEnvLookup(dotenvVars)`。

- [x] 8.5 使用 raw YAML bytes DeepMergeYAML 合并 providers
  - File: `ipc/server.go` — 替换当前简单替换逻辑
  - Action:
    1. 读取项目 `.rnix/providers.yaml` raw bytes（`os.ReadFile`）
    2. 将 `s.globalProvidersRaw` 和项目 raw bytes 各 Unmarshal 为 `map[string]any`
    3. 调用 `config.DeepMergeYAML(globalMap, projectMap)`
    4. Marshal 合并结果为 YAML bytes
    5. Unmarshal 为 `*llm.ProvidersConfig` 并 Validate
    6. 如果项目无 providers.yaml，直接用全局 ProvidersConfig

- [x] 8.6 使用环境快照创建项目级 driver 并构造 `LLMFileOpener`
  - File: `ipc/server.go`
  - Action: 对合并后的 `ProvidersConfig` 中每个 provider：
    1. 调用 `llm.CreateDriverWithEnv(pc, envLookup)` 创建 driver
    2. 调用 `llm.FileFactory(driver, "/dev/llm/"+pc.Name)` 获取 VFSFileFactory
    3. 存入 `map[string]vfs.VFSFileFactory`（provider name → factory）
    4. 构造 `LLMFileOpener` 闭包（注意签名使用 `int` flags 和返回 `any`）：
    ```go
    factories := map[string]vfs.VFSFileFactory{...} // 上面构建的
    projCfg.LLMFileOpener = func(provider string, flags int) (any, error) {
        factory, ok := factories[provider]
        if !ok {
            return nil, fmt.Errorf("project provider %q not found", provider)
        }
        return factory("", vfs.OpenFlag(flags)) // subpath="" for /dev/llm/{name}
    }
    ```

- [x] 8.7 设置项目级 default provider
  - File: `ipc/server.go`
  - Action: `projCfg.DefaultProvider = mergedProvidersCfg.ResolveDefaultProvider()`

- [x] 8.8 将 `dotenvVars` 存入 `ProjectConfig.EnvSnapshot`
  - File: `ipc/server.go`
  - Action: `projCfg.EnvSnapshot = dotenvVars`

- [x] 8.9 更新所有 `resolveProjectContext` 调用处传入 `rnixEnv`
  - File: `ipc/server.go` — `handleSpawn`、`handleSpawnPipeline`、以及其他调用处
  - Action: 从 `req.RnixEnv` 提取并传入。搜索 `resolveProjectContext(req.ProjectDir` 找到所有调用点。

**Task 9: Kernel 支持项目级 LLM 设备** (`kernel/kernel.go`)

- [x] 9.1 修改 `Spawn` 中 LLM 设备打开逻辑
  - File: `kernel/kernel.go` — 约 444-469 行
  - Action: 在 `if !opts.SkipReasonLoop {` 块内，将：
    ```go
    // 现有代码（约 446-464 行）：
    llmDevice, resolveErr := k.resolveLLMDevice(agent, opts.Provider)
    // ... error handling ...
    llmFD, err = k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)
    ```
    改为：
    ```go
    llmDevice, resolveErr := k.resolveLLMDevice(agent, opts.Provider, opts.ProjectConfig)
    // ... error handling (same) ...

    // Open LLM device: project-level driver via LLMFileOpener, or global VFS Open
    openStart := time.Now()
    var err error
    if opts.ProjectConfig != nil && opts.ProjectConfig.LLMFileOpener != nil {
        fileAny, openErr := opts.ProjectConfig.LLMFileOpener(proc.Provider, int(vfs.O_RDWR))
        if openErr != nil {
            err = openErr
        } else {
            file, ok := fileAny.(vfs.VFSFile)
            if !ok {
                err = fmt.Errorf("LLMFileOpener returned non-VFSFile type")
            } else {
                llmFD = k.vfs.RegisterFD(proc.PID, file)
            }
        }
    } else {
        llmFD, err = k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)
    }
    k.emitEvent(proc, "Open", map[string]any{
        "path":  llmDevice,
        "flags": vfs.O_RDWR,
    }, llmFD, err, time.Since(openStart))
    if err != nil {
        // ... existing error handling (CtxFree + return SyscallError) ...
    }
    ```
  - Notes: `VFS.RegisterFD` 已有（vfs.go:158），返回 FD。后续 `reasonStep` 中的 `Read`/`Write`/`Close` 都通过 FD 操作，不关心 file 来源。

- [x] 9.2 修改 `resolveLLMDevice` 增加项目级 provider 验证
  - File: `kernel/kernel.go` — `resolveLLMDevice` 函数
  - Action: 新增第三个参数 `projectCfg *config.ProjectConfig`。
    - 当 `projectCfg != nil && projectCfg.LLMFileOpener != nil` 时：
      - Default provider 回退使用 `projectCfg.DefaultProvider`（非空时）替代 `k.defaultProvider`
      - 跳过 `k.hasProvider` 验证（项目级 provider 不在全局 registry）
    - 其他情况保持原有逻辑不变
  - **所有调用点都必须更新**（签名变更）：
    1. kernel.go — `Spawn` 中主设备解析（约 line 446）：传入 `opts.ProjectConfig`
    2. kernel.go — `Spawn` 中 fallback 设备解析（约 line 364）：传入 `opts.ProjectConfig`
    3. 测试文件中约 30 处调用：传入 `nil`（测试不涉及项目级 provider）

- [x] 9.3 同步修改 `attemptFallback` 中的 fallback 设备打开
  - File: `kernel/kernel.go` — `attemptFallback` 函数约 729 行
  - Action: fallback 设备打开也需支持项目级 driver。提取 fallback provider name（`strings.TrimPrefix(proc.FallbackDevice, "/dev/llm/")`），检查 `proc.ProjectConfig.LLMFileOpener`：
    1. 如果 `LLMFileOpener` 非 nil，先尝试 `LLMFileOpener(fbProvider, int(vfs.O_RDWR))`
    2. 如果返回 "not found" error（项目级未覆盖 fallback provider），fallback 到全局 `k.vfs.Open(proc.PID, proc.FallbackDevice, vfs.O_RDWR)`
    3. 如果 `LLMFileOpener` 为 nil，直接用全局 `k.vfs.Open`（现有行为）

### Acceptance Criteria

**AC1: `.env` 文件加载**

- [x] Given 项目目录下有 `.env` 文件含 `OPENROUTER_API_KEY=sk-xxx`，全局 providers.yaml 配置 openrouter 的 `api_key_env: OPENROUTER_API_KEY`
  When 从该项目目录执行 `rnix "hello"` 触发 spawn
  Then daemon 使用 `sk-xxx` 作为 API Key 调用 OpenRouter，不出现 401 错误

**AC2: 多环境 `.env` 覆盖**

- [x] Given 项目目录下有 `.env` 含 `API_KEY=base`、`.env.local` 含 `API_KEY=local`
  When spawn 请求到达 daemon
  Then 环境快照中 `API_KEY` 值为 `local`（`.env.local` 覆盖 `.env`）

**AC3: RNIX_ENV 环境选择**

- [x] Given 项目目录下有 `.env.production` 含 `API_KEY=prod`
  When CLI 调用者设置 `RNIX_ENV=production` 后执行 spawn
  Then 环境快照中 `API_KEY` 值为 `prod`

**AC4: RNIX_ENV 默认值**

- [x] Given 调用者未设置 `RNIX_ENV` 环境变量
  When spawn 请求到达 daemon
  Then 默认使用 `development`，尝试加载 `.env.development` 和 `.env.development.local`

**AC5: 项目级 providers.yaml merge**

- [x] Given 全局 providers.yaml 定义 provider `openrouter`（api_key_env=`GLOBAL_KEY`），项目 `.rnix/providers.yaml` 仅覆盖 `api_key_env=PROJECT_KEY`
  When spawn 请求指定该项目
  Then 使用合并后的 provider 配置，API Key 从 `PROJECT_KEY` 环境变量读取（而非 `GLOBAL_KEY`）

**AC6: 项目级新增 provider**

- [x] Given 全局 providers.yaml 只有 `claude`，项目 `.rnix/providers.yaml` 新增 `openrouter` provider
  When 从该项目 spawn 并指定 `--provider openrouter`
  Then 成功使用项目级定义的 openrouter provider

**AC7: 项目间环境隔离**

- [x] Given 项目 A 的 `.env` 有 `API_KEY=aaa`，项目 B 的 `.env` 有 `API_KEY=bbb`
  When 先从项目 A spawn 再从项目 B spawn
  Then 项目 A 进程使用 `aaa`，项目 B 进程使用 `bbb`，互不污染

**AC8: 无 `.env` 文件回退**

- [x] Given 项目目录下没有任何 `.env` 文件
  When spawn 请求到达 daemon
  Then 回退到 daemon 进程自身的环境变量（`os.Getenv`），行为与修改前一致

**AC9: 无项目目录回退**

- [x] Given CLI 不在任何 `.rnix/` 项目中（`ProjectDir` 为空）
  When spawn 请求到达 daemon
  Then 使用全局 provider 配置和 daemon 进程环境变量，行为与修改前一致

**AC10: `.env` parser 鲁棒性**

- [x] Given `.env` 文件含单引号值 `KEY='hello world'`、双引号值 `KEY2="line\nbreak"`、`export` 前缀 `export KEY3=value`、注释 `# comment`、空行
  When 解析该文件
  Then 正确提取所有键值对，单引号保留字面值，双引号处理转义

**AC11: 现有测试不回归**

- [x] Given 代码修改完成
  When 执行 `make all`
  Then 所有现有测试通过，无回归

**AC12: RNIX_ENV 路径遍历防护**

- [x] Given RNIX_ENV 值为 `../../etc`
  When spawn 请求到达 daemon
  Then `LoadDotenvDir` 返回验证错误，不尝试加载任何文件

**AC13: ProjectDir 安全验证**

- [x] Given CLI 传入 ProjectDir 为 `/etc`（无 `.rnix/` 子目录）
  When spawn 请求到达 daemon
  Then 回退到全局模式，不尝试加载 `/etc/.env`

## Additional Context

### Dependencies

无新外部依赖。使用 `internal/config/merge.go` 已有的 `DeepMergeYAML`。`.env` parser 纯 Go 标准库实现。`internal/config/types.go` 需新增 `import "github.com/rnixai/rnix/internal/types"`。

### Testing Strategy

- **单元测试**：
  - `internal/config/dotenv_test.go` — `.env` parser 的 table-driven 测试，覆盖所有语法变体、边界情况、安全验证
  - `drivers/llm/factory_test.go` — `CreateDriverWithEnv` 的 envLookup 注入测试
  - `ipc/server_test.go` — `resolveProjectContext` 的 .env 加载、provider merge、LLMFileOpener 构造测试

- **集成测试**：
  - 使用 `t.TempDir()` 创建项目目录结构（`.rnix/providers.yaml`、`.env` 文件），模拟完整的 spawn 流程验证 API Key 正确传递

- **手动验证**：
  - 在实际项目中配置 `.env` + `.rnix/providers.yaml`，执行 `rnix "hello"` 验证 401 错误消失
  - 切换 `RNIX_ENV=production` 验证多环境切换

### Notes

- **性能考虑**：每次 spawn 都会加载 `.env` 文件和创建 driver 实例。对于频繁 spawn 的场景（如 compose），后续优化为按 `(ProjectDir, rnixEnv)` 缓存 `resolveProjectContext` 的结果。当前版本不做缓存。
- **安全考虑**：`EnvSnapshot` 只存 `.env` 文件中的变量，不包含 daemon 进程的完整环境。API Key 通过闭包传递给 driver，不会出现在日志或 IPC 响应中。`RNIX_ENV` 和 `ProjectDir` 都做了安全验证。
- **向后兼容**：`CreateDriver` 保持原签名不变。`SpawnRequest.RnixEnv` 为 `omitempty`，旧版 CLI 不传时 daemon 默认 `development`。无 `.env` 文件时行为与修改前完全一致。
- **资源生命周期**：项目级 driver 实例（闭包中的 `factories` map）的生命周期与 `ProjectConfig` 绑定。当进程退出并被 reap 后，`proc.ProjectConfig` 被 GC 回收，driver 实例随之释放。OpenAI-compat driver 内部的 HTTP client 无需显式 Close。

## Review Notes

- 第一轮对抗性审查：14 项发现，全部处理（4 修复、5 已记录/跳过、5 直接修复）
- 第二轮对抗性审查：6 项发现，全部处理：
  - R2-F1 (Medium): 已修复 — `LLMFileOpener` 类型定义移至 `internal/types`，签名用 `(string, int) (any, error)` 避免 config→vfs 依赖
  - R2-F2 (Medium): 已修复 — Task 9.3 明确 fallback 兜底逻辑：LLMFileOpener "not found" → 全局 vfs.Open
  - R2-F3 (Low): 已修复 — Technical Decision 12 + Task 8.7 + Task 9.2 明确项目级 default provider 传递链
  - R2-F4 (Low): 已修复 — Technical Decision 4 明确 raw bytes 传递方式
  - R2-F5 (Low): 已修复 — Technical Decision 10 明确 fail-fast 策略
  - R2-F6 (Low): 已修复 — Task 9.1 Notes 中提醒保持 proc.FDTable 追踪一致性
- 第三轮对抗性审查：6 项发现，全部修复：
  - R3-F1 (Medium): 已修复 — Task 6.2 Notes 更正为"需新增 import internal/types"
  - R3-F2 (Medium): 已修复 — Task 9.2 明确列出所有 3 个生产代码调用点 + 测试文件约 30 处
  - R3-F3 (Low): 已知 — 伪代码变量名歧义，不阻塞实现
  - R3-F4 (Low): 已修复 — 文件路径 `llm_file.go` → `vfsfile.go`
  - R3-F5 (Low): 已修复 — Dependencies 节 import 指引与正文一致
  - R3-F6 (Info): 已修复 — Task 8.8/8.9 编号去重

## Review Notes

- Adversarial review completed (implementation phase)
- Findings: 19 total, 8 fixed, 11 skipped (4 Noise + 7 Low)
- Resolution approach: auto-fix (Real findings at Critical/High/Medium severity)
- Fixed findings:
  - F-01 (Critical): projectDir 路径清理 — 添加 filepath.Clean + filepath.IsAbs 验证
  - F-02 (High): rnixEnv 入口验证 — resolveProjectContext 增加 ValidateEnvName 检查
  - F-05 (High): 变量遮蔽 — 所有 err 变量重命名为语义化名称
  - F-08 (Medium): parseDoubleQuoted 闭引号后内容检查
  - F-09 (Medium): parseSingleQuoted 闭引号后内容检查
  - F-10 (Medium): 类型断言失败添加 log.Printf 警告
  - F-12 (Medium): 全局 ProvidersConfig 浅拷贝保护 immutability
  - F-16 (Low→fixed): 路径拼接改用 filepath.Join
- Skipped findings:
  - F-03 (High/deferred): driver 缓存/池化 — 性能优化，不阻塞功能正确性
  - F-04 (High/Noise): 闭包 map 并发 — 当前每次新建，安全
  - F-06 (Medium): DeepMergeYAML slice 替换 — 已有设计决策
  - F-07 (Medium): bufio.Scanner 默认 buffer — 1MB 限制已足够
  - F-11 (Medium/Noise): 空值屏蔽系统环境变量 — by design
  - F-13 (Medium): 错误分支 nil loader — 调用方检查 err，安全
  - F-14~F-19 (Low): 非阻塞性改进
