# Epic 25 手工验证指南：配置系统重构（Configuration System Redesign）

## 概述

本文档提供 Epic 25 所有 Story 的手工验证步骤，用于在自动化测试之外对功能进行端到端的人工确认。Epic 25 将 Rnix 从"固定目录结构的开发工具"升级为"用户可定制的配置化系统"。用户通过 `rnix init` 一条命令即可创建完整的全局配置环境（`~/.config/rnix/`），项目中创建 `.rnix/` 后自动与全局配置 deep merge，项目级 agent/skill shadow 全局同名定义。

## 前置准备

### 构建

```bash
make build
```

### 理解配置体系

Epic 25 引入双层配置体系：

| 层级 | 路径 | 说明 |
|------|------|------|
| 全局 | `~/.config/rnix/`（或 `$XDG_CONFIG_HOME/rnix/`） | 用户级配置，`rnix init` 创建 |
| 项目 | `<project>/.rnix/` | 项目级配置，`rnix init` 在 CWD 创建 |

**配置合并规则：**
- YAML 配置文件（`providers.yaml`、`config.yaml`）：递归 deep merge，项目级覆盖全局级
- 资源目录（agents/、skills/）：Shadow 遮蔽，项目级同名定义完全遮蔽全局级

### 准备干净的测试环境

```bash
# 确保旧 daemon 已停止
./rnix daemon stop 2>/dev/null; true

# 备份现有配置（如有）
if [ -d ~/.config/rnix ]; then
    mv ~/.config/rnix ~/.config/rnix.bak.$(date +%s)
fi

# 记录当前工作目录
export TEST_CWD=$(pwd)
```

### 创建隔离的测试目录

```bash
# 创建临时测试目录
export TEST_DIR=$(mktemp -d)
echo "测试目录: $TEST_DIR"
```

### 验证所需工具

```bash
# 确认 rnix 二进制可用
./rnix --version

# socat 用于 IPC 测试（可选）
which socat
```

> **重要**：测试完成后请按"测试清理"章节恢复环境。

---

## Story 25.1: internal/config 包与 embed.FS 基础设施

### GlobalDir 路径解析

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 默认路径 | (1) `unset XDG_CONFIG_HOME` (2) 在 Go 测试中调用 `config.GlobalDir()` 或查看 `rnix init` 输出 | 返回 `~/.config/rnix/`（`$HOME/.config/rnix/`） | [ ] |
| 2 | XDG_CONFIG_HOME 自定义 | (1) `export XDG_CONFIG_HOME=/tmp/test-xdg` (2) `./rnix init` | 全局配置创建在 `/tmp/test-xdg/rnix/` 而非 `~/.config/rnix/` | [ ] |
| 3 | XDG 带尾斜杠 | (1) `export XDG_CONFIG_HOME=/tmp/test-xdg/` (2) 执行 `./rnix init` | 路径正确标准化，无重复斜杠，创建 `/tmp/test-xdg/rnix/` | [ ] |

### ProjectDir 向上查找

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | 当前目录有 .rnix/ | (1) `cd $TEST_DIR && mkdir -p .rnix` (2) 运行 `./rnix init` 或观察 CLI 行为 | 识别当前目录为项目根 | [ ] |
| 5 | 嵌套子目录查找 | (1) `mkdir -p $TEST_DIR/project/.rnix` (2) `mkdir -p $TEST_DIR/project/a/b/c` (3) `cd $TEST_DIR/project/a/b/c` (4) 执行 spawn 命令（观察 CLI 是否发送 project_dir） | CLI 向上遍历发现 `$TEST_DIR/project` 作为 project_dir | [ ] |
| 6 | 无 .rnix/ 目录 | (1) `cd /tmp` (2) 执行 spawn 命令 | CLI 不发送 project_dir（使用纯全局配置） | [ ] |
| 7 | .rnix 是文件而非目录 | (1) `cd $TEST_DIR && touch .rnix` (2) 尝试检测项目目录 | 不将 .rnix 文件识别为项目标记，等同于"无 .rnix/" | [ ] |
| 8 | 到 $HOME 停止遍历 | (1) `cd ~/some-deep/nested/dir`（确保 `~/` 及以上无 `.rnix/`） | 遍历到 `$HOME` 即停止，不继续向上 | [ ] |

### DeepMergeYAML 合并行为

> 以下通过 `go test -run` 或间接通过配置文件合并验证。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 9 | 递归 map 合并 | (1) 全局 `providers.yaml`: `{a: {x: 1}}` (2) 项目 `.rnix/providers.yaml`: `{a: {y: 2}}` (3) spawn 时观察合并结果 | 合并为 `{a: {x: 1, y: 2}}`（递归合并而非替换） | [ ] |
| 10 | 标量覆盖 | 项目级 `{a: {x: 99}}` 覆盖全局 `{a: {x: 1}}` | 项目值 99 覆盖全局值 1 | [ ] |
| 11 | Slice 替换不追加 | 全局 `{list: [1,2,3]}`，项目 `{list: [4,5]}` | 合并后 `{list: [4,5]}`（替换而非 `[1,2,3,4,5]`） | [ ] |
| 12 | 类型冲突 override 胜出 | 全局 `{a: {x: 1}}`（map），项目 `{a: "scalar"}`（string） | 合并后 `{a: "scalar"}`（override 整体替换） | [ ] |

### ShadowResolve 遮蔽查找

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 13 | 项目级遮蔽全局 | (1) `mkdir -p ~/.config/rnix/agents/coder` (2) `mkdir -p $TEST_DIR/.rnix/agents/coder` (3) 两处放不同 agent.yaml (4) 在项目中 spawn coder | 加载项目级 `coder/`，全局级被完全遮蔽 | [ ] |
| 14 | 仅全局存在时回退 | (1) `mkdir -p ~/.config/rnix/agents/planner` (2) 项目 `.rnix/agents/` 中无 `planner/` (3) 在项目中 spawn planner | 回退到全局级 `planner/` | [ ] |
| 15 | 双层均无 | (1) spawn 一个不存在的 agent 名称 | 返回 "not found" 错误 | [ ] |

### ListMerged 去重合并列表

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 16 | 去重排序 | (1) 全局 agents/: `coder/`, `planner/` (2) 项目 .rnix/agents/: `coder/`, `reviewer/` (3) 列出可用 agent | 返回 `["coder", "planner", "reviewer"]`（去重、排序） | [ ] |
| 17 | 跳过文件 | (1) 在 agents/ 目录中放一个文件（非目录） | 列表中不包含该文件名 | [ ] |

### ExtractEmbedded 嵌入提取

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 18 | 提取到空目录 | (1) 删除 `~/.config/rnix/` (2) `./rnix init` | agents/ 和 skills/ 目录中出现内置 agent 和 skill 文件 | [ ] |
| 19 | 不覆盖已有文件 | (1) 修改 `~/.config/rnix/agents/code-analyst/agent.yaml` 内容 (2) 再次 `./rnix init` | 用户修改的 `agent.yaml` 保持不变，不被覆盖 | [ ] |
| 20 | 嵌套目录结构保留 | (1) 检查提取后的目录结构 | 嵌套子目录（如 agent 内的子文件夹）正确创建 | [ ] |

### embed.FS 编译嵌入

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 21 | 二进制自包含 | (1) `make build` (2) `mv lib lib.bak` (3) `./rnix init` (4) 检查 agents/ 是否提取成功 (5) `mv lib.bak lib` | 即使 `lib/` 目录不在磁盘上，`rnix init` 仍能从二进制内嵌入的 embed.FS 提取 agent/skill | [ ] |

---

## Story 25.2: rnix init 与全局配置加载

### rnix init 全局初始化

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 全局目录不存在 | (1) `rm -rf ~/.config/rnix` (2) `./rnix init` | (a) 创建 `~/.config/rnix/` 目录 (b) 创建 `agents/` 和 `skills/` 子目录 (c) 从 embed.FS 提取内置 agent 到 `agents/` (d) 从 embed.FS 提取内置 skill 到 `skills/` (e) 生成默认 `providers.yaml` (f) 生成默认 `config.yaml` (g) 输出 "initialized global config: ..." | [ ] |
| 2 | providers.yaml 内容检查 | (1) 完成场景 1 后 (2) `cat ~/.config/rnix/providers.yaml` | 包含 `claude` provider 定义，YAML 格式正确 | [ ] |
| 3 | config.yaml 内容检查 | `cat ~/.config/rnix/config.yaml` | 非空文件，包含注释说明 | [ ] |
| 4 | agents 提取检查 | `ls ~/.config/rnix/agents/` | 至少包含 `code-analyst/` 目录 | [ ] |
| 5 | skills 提取检查 | `ls ~/.config/rnix/skills/` | 至少包含 `code-analysis/` 目录 | [ ] |
| 6 | 全局已存在跳过 | (1) `~/.config/rnix/` 已存在 (2) `./rnix init` | 输出 "global config already exists, skipping"，不修改任何已有文件 | [ ] |
| 7 | 幂等——用户修改保留 | (1) 修改 `~/.config/rnix/providers.yaml` (2) 再次 `./rnix init` | 用户修改的 `providers.yaml` 内容保持不变 | [ ] |

### rnix init 项目初始化

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 8 | 项目目录不存在 | (1) `cd $TEST_DIR/clean-project && mkdir -p .` (2) 确认无 `.rnix/` (3) `$TEST_CWD/rnix init` | (a) 创建 `.rnix/` 目录 (b) 创建 `agents/`、`skills/`、`data/` 子目录 (c) 生成 `.rnix/config.yaml` (d) 输出 "initialized project config: ..." | [ ] |
| 9 | 项目 config.yaml 内容 | `cat .rnix/config.yaml` | 包含注释说明，非空 | [ ] |
| 10 | 项目 data/ 子目录 | `ls -la .rnix/` | 包含 `data/` 子目录（运行时数据隔离） | [ ] |
| 11 | 项目已存在跳过 | (1) `.rnix/` 已存在 (2) `./rnix init` | 输出 "project already initialized, skipping" | [ ] |
| 12 | 项目幂等——不覆盖 | (1) 修改 `.rnix/config.yaml` 内容 (2) `./rnix init` | 用户修改的 config.yaml 保持不变 | [ ] |

### Daemon 全局配置加载

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 13 | 正常加载 providers.yaml | (1) `~/.config/rnix/providers.yaml` 存在且格式正确 (2) `./rnix daemon start` 或执行任意 spawn | daemon 启动成功，provider 注册到 DriverRegistry | [ ] |
| 14 | providers.yaml 不存在 | (1) `mv ~/.config/rnix/providers.yaml ~/.config/rnix/providers.yaml.bak` (2) 启动 daemon | 使用内置默认配置（claude + cursor），不崩溃，输出 info 日志 | [ ] |
| 15 | providers.yaml 语法错误 | (1) `echo "invalid: yaml: [[[" > ~/.config/rnix/providers.yaml` (2) 启动 daemon | 启动失败，输出详细错误信息（包含文件名、行号、错误原因） | [ ] |
| 16 | 恢复配置 | `mv ~/.config/rnix/providers.yaml.bak ~/.config/rnix/providers.yaml` | 恢复正常配置 | [ ] |

### Init 配置加载兼容性

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 17 | CWD rnix-init.yaml 优先 | (1) 项目根放 `rnix-init.yaml` (2) 全局放 `~/.config/rnix/init.yaml` (3) 启动 daemon | 优先加载 CWD 的 `rnix-init.yaml` | [ ] |
| 18 | 全局 init.yaml 回退 | (1) CWD 无 `rnix-init.yaml` (2) 全局有 `~/.config/rnix/init.yaml` (3) 启动 daemon | 回退加载全局 `init.yaml` | [ ] |
| 19 | 均不存在使用默认 | (1) CWD 和全局均无 init 配置 (2) 启动 daemon | 使用默认空配置，不崩溃 | [ ] |

---

## Story 25.3: 项目级配置合并与模块适配

### IPC ProjectDir 字段

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | CLI 自动发现并传入 | (1) `cd $TEST_DIR/project`（含 `.rnix/`） (2) 执行 spawn 命令 (3) 通过 strace 或日志观察 IPC payload | SpawnRequest payload 包含 `"project_dir": "$TEST_DIR/project"` | [ ] |
| 2 | 无 .rnix/ 时 omit | (1) `cd /tmp` (2) 执行 spawn 命令 | SpawnRequest payload 中无 `project_dir` 字段（omitempty 生效） | [ ] |
| 3 | SpawnPipelineRequest 含 ProjectDir | (1) 在含 `.rnix/` 的项目中执行 pipeline 命令 | pipeline 请求也携带 project_dir | [ ] |
| 4 | ExecScriptRequest 含 ProjectDir | (1) 在含 `.rnix/` 的项目中执行 script 命令 | script 请求也携带 project_dir | [ ] |

### 项目级 Provider 合并

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | 项目新增 provider | (1) 全局 `providers.yaml` 含 claude (2) 项目 `.rnix/providers.yaml` 新增 ollama provider (3) 在项目中 spawn | 合并后该请求可使用 claude（全局）和 ollama（项目新增）两个 provider | [ ] |
| 6 | 项目覆盖全局 provider 属性 | (1) 全局 claude 配置 `default_model: haiku` (2) 项目 `.rnix/providers.yaml` 覆盖 `default_model: sonnet` (3) 在项目中 spawn | 使用项目覆盖后的 sonnet 模型 | [ ] |
| 7 | 项目 providers.yaml 语法错误 | (1) 项目 `.rnix/providers.yaml` 包含 YAML 语法错误 (2) 在项目中 spawn | spawn 失败，返回 IPC 错误（包含文件名和错误原因） | [ ] |
| 8 | 语法错误不影响其他项目 | (1) 项目 A `.rnix/providers.yaml` 有语法错误 (2) 项目 B 正常 (3) 同一 daemon 下分别 spawn | 项目 A 失败，项目 B 正常（互不影响） | [ ] |

### Agent Shadow 遮蔽加载

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 9 | 项目 agent 遮蔽全局 | (1) 全局 `~/.config/rnix/agents/coder/` 存在 (2) 项目 `.rnix/agents/coder/` 也存在，内容不同 (3) `--agent=coder` spawn | 加载项目级 `coder/`，验证方式：instructions.md 内容或 description 字段匹配项目版本 | [ ] |
| 10 | 全局 agent 回退 | (1) 项目 `.rnix/agents/` 中无 `planner/` (2) 全局有 `~/.config/rnix/agents/planner/` (3) `--agent=planner` spawn | 回退加载全局 `planner/` | [ ] |
| 11 | 双层都无报错 | (1) spawn `--agent=nonexistent` | 返回 "agent directory not found" 错误，列出搜索过的目录 | [ ] |

### Skill Shadow 遮蔽加载

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 12 | 项目 skill 遮蔽全局 | (1) 全局 `~/.config/rnix/skills/review/` 存在 (2) 项目 `.rnix/skills/review/` 也存在 (3) spawn agent 引用 skill `review` | 加载项目级 `review/` | [ ] |
| 13 | 全局 skill 回退 | (1) 项目无 `code-analysis/` (2) 全局有 (3) spawn agent 引用 `code-analysis` | 回退加载全局级 | [ ] |

### 多项目隔离

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 14 | 同一 daemon 服务多项目 | (1) 创建两个项目目录，各有不同 `.rnix/` 配置 (2) 终端 A 在项目 A 中 spawn (3) 终端 B 在项目 B 中 spawn | 各进程使用各自的 ProjectConfig，互不影响 | [ ] |
| 15 | ProjectConfig 不可变 | (1) spawn 一个进程 (2) 修改项目 `.rnix/config.yaml` (3) 检查正在运行的进程行为 | 正在运行的进程不受配置变更影响（快照不可变） | [ ] |

### CLI ProjectDir 发现

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 16 | 深层子目录发现 | (1) `.rnix/` 在 `/a/b/c/` (2) 在 `/a/b/c/src/pkg/` 中执行命令 | CLI 发现 `project_dir="/a/b/c"` 并传入 IPC | [ ] |
| 17 | 无 .rnix/ 空处理 | (1) 在无 `.rnix/` 的目录中执行 | `project_dir` 为空，daemon 仅使用全局配置 | [ ] |

### 运行时数据目录隔离

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 18 | 数据存放在 .rnix/data/ | (1) 在项目中运行产生 records/traces 等数据 | 数据文件存放在 `.rnix/data/` 子目录下，与配置文件物理隔离 | [ ] |
| 19 | 旧路径回退兼容 | (1) `.rnix/records/` 存在（旧路径） (2) 运行 | 优先使用旧路径 `.rnix/records/`（向后兼容） | [ ] |
| 20 | 新路径创建 | (1) 无旧路径 `.rnix/records/` (2) 运行 | 使用新路径 `.rnix/data/records/` | [ ] |

### 模块适配验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 21 | agents/loader 使用 ShadowResolve | (1) 查看 `agents/loader.go` 源码 | `Load()` 方法通过 `config.ShadowResolve(agentName, l.searchDirs...)` 查找 agent | [ ] |
| 22 | skills/loader 使用 ShadowResolve | (1) 查看 `skills/loader.go` 源码 | `load()` 方法通过 `config.ShadowResolve(skillName, l.searchDirs...)` 查找 skill | [ ] |
| 23 | drivers/llm 使用 config 包路径 | (1) 查看 `drivers/llm/config.go` 源码 | 通过 `config.GlobalDir()` 和 `config.ResolvePath()` 获取配置路径，不使用旧 `FindProvidersConfigPath()` | [ ] |
| 24 | kernel/process 含 ProjectConfig | (1) 查看 `kernel/process.go` 源码 | `Process` 结构体包含 `ProjectConfig *config.ProjectConfig` 字段 | [ ] |

---

## 端到端完整流程验证

> **前提**：干净环境（无 `~/.config/rnix/`、无项目 `.rnix/`）。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 全新安装到首次使用 | (1) `rm -rf ~/.config/rnix` (2) `./rnix init` (3) 确认全局目录结构 (4) 确认项目目录结构 | 一条命令完成全局 + 项目初始化，所有子目录和默认配置生成 | [ ] |
| 2 | 全局配置 + daemon 启动 | (1) 场景 1 完成后 (2) `./rnix daemon start` 或执行 spawn | daemon 从 `~/.config/rnix/providers.yaml` 加载配置，正常启动 | [ ] |
| 3 | 项目级配置叠加 | (1) 在项目 `.rnix/providers.yaml` 新增 provider (2) spawn 使用新 provider | daemon 合并全局 + 项目配置，新 provider 可用 | [ ] |
| 4 | 项目 Agent 定制 | (1) 在 `.rnix/agents/` 创建自定义 agent (2) spawn 使用该 agent | 项目级自定义 agent 正常加载和运行 | [ ] |
| 5 | 项目 Agent 遮蔽全局 | (1) 在 `.rnix/agents/code-analyst/` 放置修改版 (2) spawn code-analyst | 使用项目级修改版而非全局内置版 | [ ] |
| 6 | 多终端多项目 | (1) 创建项目 A 和项目 B，各有不同 `.rnix/` 配置 (2) 终端 A 在项目 A spawn (3) 终端 B 在项目 B spawn (4) 两个终端共用同一 daemon | 各项目的 spawn 使用各自项目配置，互不干扰 | [ ] |
| 7 | 二次 init 幂等 | (1) `./rnix init` (2) 修改全局和项目配置文件 (3) 再次 `./rnix init` | 所有用户修改保持不变，init 输出 "already exists, skipping" | [ ] |

---

## NFR 验证

### NFR53: init 性能 ≤ 3 秒

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | init 执行时间 | (1) `rm -rf ~/.config/rnix` (2) `time ./rnix init` | 执行时间 ≤ 3 秒（含 embed.FS 提取） | [ ] |

### NFR54: ProjectDir 发现 ≤ 10ms

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 2 | ProjectDir 延迟 | (1) 运行 `go test -run TestProjectDir -bench -count=1 ./internal/config/...` 或通过 20 层嵌套目录测试 | 发现 `.rnix/` 的时间 ≤ 10ms（≤ 20 层目录深度） | [ ] |

### NFR55: YAML 合并 ≤ 50ms

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 3 | DeepMergeYAML 性能 | 通过 benchmark 或实测配置合并时间 | 合并处理时间 ≤ 50ms | [ ] |

---

## 自动化测试验证

> 作为手工验证的补充，确认自动化测试套件全部通过。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | internal/config 单元测试 | `go test -race -count=1 ./internal/config/...` | 全部通过（30+ 测试），使用 `t.TempDir()` + `t.Setenv()` 隔离 | [ ] |
| 2 | cmd/rnix init 测试 | `go test -race -count=1 -run TestInit ./cmd/rnix/...` | 全部通过（16 测试） | [ ] |
| 3 | agents/loader shadow 测试 | `go test -race -count=1 -run TestAgentLoader_ShadowResolve ./agents/...` | 全部通过（3 测试） | [ ] |
| 4 | skills/loader shadow 测试 | `go test -race -count=1 -run TestSkillLoader_ShadowResolve ./skills/...` | 全部通过（2 测试） | [ ] |
| 5 | IPC protocol ProjectDir 测试 | `go test -race -count=1 -run TestSpawnRequest_ProjectDir ./ipc/...` | 全部通过 | [ ] |
| 6 | 全项目回归 | `make all`（lint + vet + test + build） | 全部通过，lint 0 issues，21 个包测试通过 | [ ] |

---

## 测试清理

```bash
# 停止 daemon（如在运行）
./rnix daemon stop 2>/dev/null; true

# 删除测试目录
rm -rf $TEST_DIR

# 恢复备份的配置（如有）
if [ -d ~/.config/rnix.bak.* ]; then
    rm -rf ~/.config/rnix
    mv ~/.config/rnix.bak.* ~/.config/rnix
fi

# 恢复 XDG 环境变量
unset XDG_CONFIG_HOME
unset TEST_DIR
unset TEST_CWD
```

---

## 关键注意事项

1. **双层配置体系** -- 全局 `~/.config/rnix/` + 项目 `.rnix/`。DeepMerge 用于 YAML 配置文件（递归合并），Shadow 用于资源目录（整体遮蔽）
2. **XDG 规范** -- 全局目录优先使用 `$XDG_CONFIG_HOME/rnix/`，未设置时回退到 `~/.config/rnix/`
3. **幂等操作** -- `rnix init` 永远不覆盖用户已有文件，安全可重复运行
4. **embed.FS 自包含** -- 内置 agent/skill 编译进二进制，无需磁盘上的 `lib/` 目录
5. **ProjectDir 遍历边界** -- 从 CWD 向上查找 `.rnix/`，到 `$HOME` 或文件系统根停止
6. **.rnix 必须是目录** -- `.rnix` 如果是文件而非目录，不被识别为项目标记
7. **ProjectConfig 不可变** -- spawn 时创建配置快照，进程生命周期内不可修改
8. **多项目隔离** -- 同一 daemon 同时服务多项目，每进程持有独立 ProjectConfig
9. **旧路径兼容** -- `.rnix/records/` 等旧路径存在时优先使用，否则使用 `.rnix/data/records/`
10. **配置路径禁止硬编码** -- 所有路径必须通过 `config.GlobalDir()`、`config.ProjectDir()`、`config.ResolvePath()` 获取
11. **config 包零外部依赖** -- `internal/config/` 仅依赖 Go 标准库，不引入项目内其他包
12. **Slice 替换** -- DeepMergeYAML 对 slice 执行替换而非追加
13. **类型冲突** -- 当 base 值为 map、override 值为 scalar 时，override 整体替换（不尝试合并）
14. **IPC omitempty** -- `SpawnRequest.ProjectDir` 标记 `json:"project_dir,omitempty"`，旧版 CLI 不发送时 daemon 按空处理
15. **providers.yaml 错误分级** -- 全局不存在用默认（不崩溃），语法错误则启动失败（严格模式）；项目级语法错误仅影响该项目 spawn

## 验证记录

| 字段 | 值 |
|------|-----|
| 验证人 | |
| 验证日期 | |
| 构建版本 | |
| 总用例数 | 67 |
| 通过数 | |
| 失败数 | |
| 备注 | |
