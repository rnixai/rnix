# Epic 25 手工验证指南：配置系统重构（Configuration System Redesign）

## 概述

本文档提供 Epic 25 所有 Story 的手工验证步骤。Epic 25 将 Rnix 从"固定目录结构"升级为"双层可定制配置系统"：

- **全局层** `~/.config/rnix/`（或 `$XDG_CONFIG_HOME/rnix/`）— 用户级配置，`rnix init` 创建
- **项目层** `<project>/.rnix/` — 项目级配置，`rnix init` 在 CWD 创建

**配置合并规则：**
- YAML 配置文件（`providers.yaml`、`config.yaml`）：递归 deep merge，项目级覆盖全局级
- 资源目录（agents/、skills/）：Shadow 遮蔽，项目级同名定义完全遮蔽全局级

## 前置准备

### 构建

```bash
make build
```

### 准备干净的测试环境

```bash
# 确保旧 daemon 已停止
./rnix daemon stop 2>/dev/null; true
```

```bash
# 备份现有配置（如有）
if [ -d ~/.config/rnix ]; then
    mv ~/.config/rnix ~/.config/rnix.bak.$(date +%s)
fi
```

### 设置环境变量

```bash
# 记录 rnix 二进制的绝对路径，后续在任意目录中均可使用
export RNIX="$(pwd)/rnix"
```

```bash
# 创建临时测试目录
export TEST_DIR=$(mktemp -d)
echo "测试目录: $TEST_DIR"
```

### 确认工具可用

```bash
$RNIX init --help
```

```bash
which socat
```

> **重要**：测试完成后请按"测试清理"章节恢复环境。

---

## Story 25.1: internal/config 包与 embed.FS 基础设施

### 25.1-V01: GlobalDir 默认路径

**验证目标：** 未设置 XDG_CONFIG_HOME 时，全局目录默认为 `~/.config/rnix/`

```bash
unset XDG_CONFIG_HOME
```

```bash
rm -rf ~/.config/rnix
```

```bash
$RNIX init
```

**预期：** 输出包含 `initialized global config`，且路径指向 `~/.config/rnix/`

```bash
ls -la ~/.config/rnix/
```

**预期：** 目录存在，包含 `agents/`、`skills/`、`providers.yaml`、`config.yaml`

- [x] 通过

---

### 25.1-V02: XDG_CONFIG_HOME 自定义路径

**验证目标：** 设置 XDG_CONFIG_HOME 时，全局配置创建在自定义路径下

```bash
rm -rf /tmp/test-xdg
```

```bash
export XDG_CONFIG_HOME=/tmp/test-xdg
```

```bash
rm -rf ~/.config/rnix
```

```bash
$RNIX init
```

**预期：** 全局配置创建在 `/tmp/test-xdg/rnix/` 而非 `~/.config/rnix/`

```bash
ls -la /tmp/test-xdg/rnix/
```

**预期：** 目录存在，包含 `agents/`、`skills/`、`providers.yaml`、`config.yaml`

```bash
ls ~/.config/rnix/ 2>&1
```

**预期：** 输出 "No such file or directory"

```bash
unset XDG_CONFIG_HOME
```

- [x] 通过

---

### 25.1-V03: XDG_CONFIG_HOME 带尾斜杠

**验证目标：** 尾斜杠被正确标准化，不产生重复斜杠

```bash
rm -rf /tmp/test-xdg
```

```bash
export XDG_CONFIG_HOME=/tmp/test-xdg/
```

```bash
$RNIX init
```

```bash
ls -la /tmp/test-xdg/rnix/
```

**预期：** 路径 `/tmp/test-xdg/rnix/` 存在，无 `/tmp/test-xdg//rnix/` 异常

```bash
unset XDG_CONFIG_HOME
```

- [x] 通过

> **注意：** 完成 V02/V03 后务必确认 `echo $XDG_CONFIG_HOME` 为空。如果后续测试出现 "global config already exists" 但 `~/.config/rnix/` 不存在，说明此变量未正确清除。

---

### 25.1-V04: ProjectDir — 当前目录有 .rnix/

**验证目标：** CLI 在包含 .rnix/ 的目录中能识别为项目根

```bash
mkdir -p $TEST_DIR/proj-04/.rnix
```

```bash
cd $TEST_DIR/proj-04
```

```bash
$RNIX init
```

**预期：** 输出 "project already initialized, skipping"（因为 .rnix/ 已存在）

- [x] 通过

---

### 25.1-V05: ProjectDir — 嵌套子目录向上查找

**验证目标：** 在深层子目录中执行命令时，CLI 向上遍历找到最近的 .rnix/

```bash
mkdir -p $TEST_DIR/proj-05/.rnix
```

```bash
mkdir -p $TEST_DIR/proj-05/a/b/c
```

```bash
cd $TEST_DIR/proj-05/a/b/c
```

```bash
$RNIX -i "hello" -v 2>&1 | head -5
```

**预期：** verbose 输出中显示 project_dir 指向 `$TEST_DIR/proj-05`（如果 daemon 未启动会报连接错误，但 CLI 发出请求前会先发现 .rnix/ 目录）

- [x] 通过

---

### 25.1-V06: ProjectDir — 无 .rnix/ 目录

**验证目标：** 在没有 .rnix/ 的目录中，CLI 不发送 project_dir

```bash
cd /tmp
```

```bash
$RNIX -i "hello" -v 2>&1 | head -5
```

**预期：** 请求中没有 project_dir 字段（使用纯全局配置）

- [x] 通过

---

### 25.1-V07: .rnix 是文件而非目录

**验证目标：** .rnix 如果是文件（非目录），不被识别为项目标记

```bash
mkdir -p $TEST_DIR/proj-07 && cd $TEST_DIR/proj-07
```

```bash
touch .rnix
```

```bash
$RNIX init
```

**预期：** 不把 .rnix 文件当作已有项目配置，应创建 .rnix/ 目录（或显示全局 init 输出但不识别项目）

```bash
ls -la .rnix
```

**预期：** `.rnix` 仍是文件（init 不应将其覆盖为目录），或 init 按"无项目"处理

- [x] 通过

---

### 25.1-V08: ProjectDir — 到 $HOME 停止遍历

**验证目标：** 向上查找 .rnix/ 时，遍历到 $HOME 即停止

```bash
mkdir -p ~/deep-test-08/a/b/c && cd ~/deep-test-08/a/b/c
```

```bash
$RNIX -i "hello" -v 2>&1 | head -5
```

**预期：** 请求中没有 project_dir（$HOME 下无 .rnix/，遍历到 $HOME 停止）

```bash
rm -rf ~/deep-test-08
```

- [x] 通过

---

### 25.1-V09 ~ V12: DeepMergeYAML 合并行为

**验证目标：** 通过自动化测试验证 YAML 递归合并语义

切回项目目录

```bash
cd $(dirname $RNIX)
```

然后重新运行：

```bash
go test -race -count=1 -v -run "TestDeepMergeYAML" ./internal/config/...
```

**预期：** 以下测试全部 PASS：
- `TestDeepMergeYAML_NestedRecursive` — 递归 map 合并 `{a: {x:1}} + {a: {y:2}} = {a: {x:1, y:2}}`
- `TestDeepMergeYAML_OnlyOverride` — 标量覆盖
- `TestDeepMergeYAML_SliceReplace` — slice 替换不追加 `[1,2,3] + [4,5] = [4,5]`
- `TestDeepMergeYAML_TypeConflict_OverrideWins` — 类型冲突 override 胜出
- `TestDeepMergeYAML_FiveLevelDeep` — 5 层深度递归
- `TestDeepMergeYAML_DoesNotMutateInputs` — 不修改输入 map

- [x] 通过

---

### 25.1-V13 ~ V15: ShadowResolve 遮蔽查找

**验证目标：** 通过自动化测试验证 shadow 遮蔽语义

```bash
go test -race -count=1 -v -run "TestShadowResolve" ./internal/config/...
```

**预期：** 以下测试全部 PASS：
- `TestShadowResolve_ProjectExists` — 项目级遮蔽全局
- `TestShadowResolve_OnlyGlobal` — 仅全局存在时回退
- `TestShadowResolve_NotFound` — 双层均无返回空串
- `TestShadowResolve_FileNotDir` — 文件不算目录
- `TestShadowResolve_EmptyDirsList` — 空参数不 panic
- `TestShadowResolve_MultiDirPriority` — 多目录优先级

- [x] 通过

---

### 25.1-V16 ~ V17: ListMerged 去重合并列表

```bash
go test -race -count=1 -v -run "TestListMerged" ./internal/config/...
```

**预期：** 以下测试全部 PASS：
- `TestListMerged_Dedup_Sorted` — 去重排序
- `TestListMerged_SkipsFiles` — 跳过文件
- `TestListMerged_ThreeDirs_Dedup` — 3 目录去重
- `TestListMerged_NoDirs` — 零参数不 panic

- [x] 通过

---

### 25.1-V18: ExtractEmbedded — 提取到空目录

**验证目标：** 首次 init 从 embed.FS 提取内置 agent/skill

```bash
rm -rf ~/.config/rnix
```

```bash
$RNIX init
```

```bash
ls ~/.config/rnix/agents/
```

**预期：** 至少包含 `code-analyst/` 目录

```bash
ls ~/.config/rnix/skills/
```

**预期：** 至少包含 `code-analysis/` 目录

- [x] 通过

---

### 25.1-V19: ExtractEmbedded — 不覆盖已有文件

**验证目标：** 再次 init 不会覆盖用户修改过的文件

```bash
echo "# user customized" > ~/.config/rnix/agents/code-analyst/agent.yaml
```

```bash
cat ~/.config/rnix/agents/code-analyst/agent.yaml
```

**预期：** 内容为 `# user customized`

```bash
$RNIX init
```

```bash
cat ~/.config/rnix/agents/code-analyst/agent.yaml
```

**预期：** 内容仍为 `# user customized`，未被覆盖

- [x] 通过

---

### 25.1-V20: 嵌套目录结构保留

**验证目标：** 内置 agent 的嵌套子目录正确提取

```bash
find ~/.config/rnix/agents/ -type d | head -20
```

**预期：** 各 agent 目录结构完整（如包含 `agent.yaml`、`instructions.md` 等文件）

```bash
find ~/.config/rnix/agents/ -name "agent.yaml" | head -10
```

**预期：** 每个 agent 子目录下都有 `agent.yaml`

- [x] 通过

---

### 25.1-V21: embed.FS 二进制自包含

**验证目标：** 即使磁盘上无 `lib/` 目录，rnix 仍能从编译嵌入的 embed.FS 提取资源

> 此测试在项目根目录执行

```bash
cd $(dirname $RNIX)
```

```bash
rm -rf ~/.config/rnix
```

```bash
mv lib lib.bak
```

```bash
$RNIX init
```

```bash
ls ~/.config/rnix/agents/
```

**预期：** agents 目录下有内容（证明从二进制内嵌 FS 提取，非磁盘读取）

```bash
mv lib.bak lib
```

- [x] 通过

---

## Story 25.2: rnix init 与全局配置加载

### 25.2-V01: 全局目录不存在 — 完整初始化

**验证目标：** 一条命令完成全局环境初始化

```bash
rm -rf ~/.config/rnix
```

```bash
$RNIX init
```

**预期：** 输出包含 "initialized global config"

```bash
ls -la ~/.config/rnix/
```

**预期：** 包含 `agents/`、`skills/`、`providers.yaml`、`config.yaml`

- [x] 通过

---

### 25.2-V02: providers.yaml 内容检查

```bash
cat ~/.config/rnix/providers.yaml
```

**预期：** 包含 `claude` provider 定义，YAML 格式正确，非空

- [x] 通过

---

### 25.2-V03: config.yaml 内容检查

```bash
cat ~/.config/rnix/config.yaml
```

**预期：** 非空文件

- [x] 通过

---

### 25.2-V04: agents 提取检查

```bash
ls ~/.config/rnix/agents/
```

**预期：** 至少包含 `code-analyst/` 目录

```bash
cat ~/.config/rnix/agents/code-analyst/agent.yaml
```

**预期：** 有效 YAML，包含 `name` 字段

- [x] 通过

---

### 25.2-V05: skills 提取检查

```bash
ls ~/.config/rnix/skills/
```

**预期：** 至少包含 `code-analysis/` 目录

```bash
head -5 ~/.config/rnix/skills/code-analysis/SKILL.md
```

**预期：** 以 `---` 开头的 YAML frontmatter

- [x] 通过

---

### 25.2-V06: 全局已存在 — 跳过

**验证目标：** 全局配置已存在时，init 不做任何修改

```bash
$RNIX init
```

**预期：** 输出 "global config already exists, skipping"

- [x] 通过

---

### 25.2-V07: 幂等 — 用户修改保留

```bash
echo "# user customized providers" > ~/.config/rnix/providers.yaml
```

```bash
$RNIX init
```

```bash
cat ~/.config/rnix/providers.yaml
```

**预期：** 内容仍为 `# user customized providers`，未被覆盖

```bash
rm -rf ~/.config/rnix && $RNIX init
```

> 恢复正常配置，供后续测试使用

- [x] 通过

---

### 25.2-V08: 项目初始化 — 目录不存在

**验证目标：** 在无 .rnix/ 的目录中执行 init，创建项目配置结构

```bash
mkdir -p $TEST_DIR/proj-init && cd $TEST_DIR/proj-init
```

```bash
ls -la .rnix/ 2>&1
```

**预期：** "No such file or directory"

```bash
$RNIX init
```

**预期：** 输出包含 "initialized project config"

```bash
ls -la .rnix/
```

**预期：** 包含 `agents/`、`skills/`、`data/`、`config.yaml`

- [x] 通过

---

### 25.2-V09: 项目 config.yaml 内容

```bash
cat $TEST_DIR/proj-init/.rnix/config.yaml
```

**预期：** 非空文件，包含注释说明

- [x] 通过

---

### 25.2-V10: 项目 data/ 子目录

```bash
ls -la $TEST_DIR/proj-init/.rnix/data/
```

**预期：** `data/` 目录存在（用于运行时数据隔离）

- [x] 通过

---

### 25.2-V11: 项目已存在 — 跳过

```bash
cd $TEST_DIR/proj-init
```

```bash
$RNIX init
```

**预期：** 输出 "project already initialized, skipping"

- [x] 通过

---

### 25.2-V12: 项目幂等 — 不覆盖

```bash
echo "# user custom project config" > $TEST_DIR/proj-init/.rnix/config.yaml
```

```bash
cd $TEST_DIR/proj-init && $RNIX init
```

```bash
cat $TEST_DIR/proj-init/.rnix/config.yaml
```

**预期：** 内容仍为 `# user custom project config`

- [x] 通过

---

### 25.2-V13: Daemon 正常加载 providers.yaml

**验证目标：** daemon 启动时从全局配置加载 provider

```bash
rm -rf ~/.config/rnix && $RNIX init
```

```bash
$RNIX daemon stop 2>/dev/null; true
```

```bash
$RNIX ps 2>&1
```

**预期：** daemon 自动启动（EnsureDaemon），无配置加载错误

- [x] 通过

---

### 25.2-V14: providers.yaml 不存在 — 使用内置默认

```bash
$RNIX daemon stop 2>/dev/null; true
```

```bash
mv ~/.config/rnix/providers.yaml ~/.config/rnix/providers.yaml.bak
```

```bash
$RNIX ps 2>&1
```

**预期：** daemon 正常启动，使用内置默认配置（claude + cursor），不崩溃

```bash
mv ~/.config/rnix/providers.yaml.bak ~/.config/rnix/providers.yaml
```

- [x] 通过

---

### 25.2-V15: providers.yaml 语法错误 — 启动失败

```bash
$RNIX daemon stop 2>/dev/null; true
```

```bash
cp ~/.config/rnix/providers.yaml ~/.config/rnix/providers.yaml.bak
```

```bash
echo "invalid: yaml: [[[" > ~/.config/rnix/providers.yaml
```

```bash
$RNIX ps 2>&1
```

**预期：** 启动失败或输出详细错误信息（含文件名和错误原因）

```bash
mv ~/.config/rnix/providers.yaml.bak ~/.config/rnix/providers.yaml
```

- [x] 通过

---

### 25.2-V16: Init 配置兼容性 — CWD rnix-init.yaml 优先

```bash
$RNIX daemon stop 2>/dev/null; true
```

```bash
mkdir -p $TEST_DIR/proj-init-compat && cd $TEST_DIR/proj-init-compat
```

```bash
echo 'services: []' > rnix-init.yaml
```

```bash
echo 'services: []' > ~/.config/rnix/init.yaml
```

```bash
$RNIX ps 2>&1
```

**预期：** daemon 启动成功，优先加载 CWD 的 `rnix-init.yaml`

```bash
rm -f rnix-init.yaml ~/.config/rnix/init.yaml
```

- [x] 通过

---

### 25.2-V17: Init 配置兼容性 — 全局 init.yaml 回退

```bash
$RNIX daemon stop 2>/dev/null; true
```

```bash
cd $TEST_DIR/proj-init-compat
```

```bash
echo 'services: []' > ~/.config/rnix/init.yaml
```

```bash
$RNIX ps 2>&1
```

**预期：** CWD 无 rnix-init.yaml，回退加载全局 `init.yaml`，daemon 正常启动

```bash
rm -f ~/.config/rnix/init.yaml
```

- [x] 通过

---

### 25.2-V18: Init 配置兼容性 — 均不存在使用默认

```bash
$RNIX daemon stop 2>/dev/null; true
```

```bash
cd /tmp
```

```bash
$RNIX ps 2>&1
```

**预期：** CWD 和全局均无 init 配置，使用默认空配置，daemon 正常启动不崩溃

- [x] 通过

---

## Story 25.3: 项目级配置合并与模块适配

### 25.3-V01: IPC — CLI 自动发现 ProjectDir 并传入

**验证目标：** CLI 在含 .rnix/ 的项目中自动发现并发送 project_dir

```bash
cd $(dirname $RNIX)
```

**验证方式：源码审查 + 自动化测试**

```bash
grep -n "ProjectDir" cmd/rnix/main.go | head -5
```

**预期：** 可看到类似以下关键行：
- `projectDir, _ := config.ProjectDir(cwd)` — CLI 从 CWD 向上查找 `.rnix/`
- `ProjectDir: projectDir` — 写入 SpawnRequest 发送给 daemon

```bash
go test -race -count=1 -v -run "TestResolveProjectContext_WithProjectDir$" ./ipc/...
```

**预期：** PASS — 断言 daemon 端正确接收并处理 projectDir

- [x] 通过

---

### 25.3-V02: IPC — 无 .rnix/ 时 project_dir 为空

**验证目标：** 无 .rnix/ 目录时，ProjectDir 返回空串，IPC 中 omitempty 生效不发送该字段

```bash
go test -race -count=1 -v -run "TestResolveProjectContext_EmptyProjectDir$" ./ipc/...
```

**预期：** PASS — 断言 `projectDir == ""` 时返回 `(nil, globalLoader, nil)`

- [x] 通过

---

### 25.3-V03: 项目新增 provider

**验证目标：** 项目 .rnix/providers.yaml 中的新 provider 与全局合并后可用

```bash
$RNIX daemon stop 2>/dev/null; true
```

```bash
mkdir -p $TEST_DIR/proj-provider/.rnix && cd $TEST_DIR/proj-provider
```

```bash
cat > .rnix/providers.yaml << 'EOF'
version: "1"
providers:
  - name: ollama
    driver: openai-compat
    base_url: http://localhost:11434/v1
    default_model: llama3
EOF
```

```bash
$RNIX -i "hello" --provider=ollama -v 2>&1 | head -20
```

**预期：** 尝试使用 ollama provider（如果 ollama 未运行会报连接错误，但说明 provider 已注册合并成功）。全局的 claude provider 也应仍然可用。

- [x] 通过

---

### 25.3-V04: 项目覆盖全局 provider 属性

**验证目标：** 项目级 providers.yaml 中的属性覆盖全局同名 provider 的属性

```bash
$RNIX daemon stop 2>/dev/null; true
```

```bash
mkdir -p $TEST_DIR/proj-override/.rnix && cd $TEST_DIR/proj-override
```

```bash
cat > .rnix/providers.yaml << 'EOF'
version: "1"
providers:
  - name: claude
    driver: claude-cli
    default_model: sonnet
EOF
```

```bash
$RNIX -i "hello" -v 2>&1 | head -20
```

**预期：** claude provider 使用项目覆盖后的 sonnet 模型（而非全局的 haiku 或默认值）

- [x] 通过

---

### 25.3-V05: 项目 providers.yaml 语法错误

**验证目标：** 项目级 YAML 语法错误不崩溃，返回明确的 IPC 错误

```bash
$RNIX daemon stop 2>/dev/null; true
```

```bash
mkdir -p $TEST_DIR/proj-bad-yaml/.rnix && cd $TEST_DIR/proj-bad-yaml
```

```bash
echo "invalid: yaml: [[[" > .rnix/providers.yaml
```

```bash
$RNIX -i "hello" 2>&1
```

**预期：** 返回 CONFIG_ERROR，错误信息包含文件名和 YAML 解析错误原因

- [] 通过

---

### 25.3-V06: 语法错误不影响其他项目

**验证目标：** 一个项目的 providers.yaml 错误不影响同一 daemon 服务的其他项目

```bash
mkdir -p $TEST_DIR/proj-good/.rnix && cd $TEST_DIR/proj-good
```

```bash
$RNIX -i "hello" 2>&1 | head -5
```

**预期：** 正常执行（proj-bad-yaml 的错误不影响 proj-good）

- [] 通过

---

### 25.3-V07: Agent Shadow — 项目遮蔽全局

**验证目标：** 项目 .rnix/agents/X/ 完全遮蔽全局 agents/X/

```bash
$RNIX daemon stop 2>/dev/null; true
```

```bash
mkdir -p $TEST_DIR/proj-shadow/.rnix/agents/code-analyst
```

```bash
cat > $TEST_DIR/proj-shadow/.rnix/agents/code-analyst/agent.yaml << 'EOF'
name: code-analyst
description: "PROJECT LEVEL shadow version"
model: haiku
skills: []
EOF
```

```bash
cat > $TEST_DIR/proj-shadow/.rnix/agents/code-analyst/instructions.md << 'EOF'
You are a PROJECT LEVEL code analyst.
EOF
```

```bash
cd $TEST_DIR/proj-shadow
```

```bash
$RNIX -i "hello" --agent=code-analyst -v 2>&1 | head -30
```

**预期：** 加载了项目级 code-analyst（description 为 "PROJECT LEVEL shadow version"），全局级被遮蔽

- [] 通过

---

### 25.3-V08: Agent Shadow — 全局回退

**验证目标：** 项目 .rnix/agents/ 中无该 agent 时，回退到全局

```bash
cd $TEST_DIR/proj-shadow
```

```bash
ls $TEST_DIR/proj-shadow/.rnix/agents/
```

**预期：** 仅有 `code-analyst/`

```bash
$RNIX -i "hello" --agent=code-analyst -v 2>&1 | head -5
```

**预期：** 如果请求其他全局 agent（项目中不存在的），应回退到全局版本

- [] 通过

---

### 25.3-V09: Agent Shadow — 双层均无报错

```bash
cd $TEST_DIR/proj-shadow
```

```bash
$RNIX -i "hello" --agent=nonexistent-agent 2>&1
```

**预期：** 返回错误信息 "agent directory not found" 或类似提示

- [] 通过

---

### 25.3-V10: Agent/Skill Loader 使用 ShadowResolve（自动化验证）

```bash
go test -race -count=1 -v -run "TestAgentLoader_ShadowResolve" ./agents/...
```

**预期：** 3 个测试全部 PASS：
- `TestAgentLoader_ShadowResolve_ProjectShadowsGlobal`
- `TestAgentLoader_ShadowResolve_FallbackToGlobal`
- `TestAgentLoader_ShadowResolve_NotFound`

```bash
go test -race -count=1 -v -run "TestSkillLoader_ShadowResolve" ./skills/...
```

**预期：** 2 个测试全部 PASS：
- `TestSkillLoader_ShadowResolve_ProjectShadowsGlobal`
- `TestSkillLoader_ShadowResolve_FallbackToGlobal`

- [] 通过

---

### 25.3-V11: IPC resolveProjectContext（自动化验证）

```bash
go test -race -count=1 -v -run "TestResolveProjectContext" ./ipc/...
```

**预期：** 5 个测试全部 PASS：
- `TestResolveProjectContext_EmptyProjectDir`
- `TestResolveProjectContext_EmptyProjectDir_NoGlobalConfig`
- `TestResolveProjectContext_WithProjectDir_NoGlobalConfig`
- `TestResolveProjectContext_WithProjectDir`
- `TestResolveProjectContext_InvalidProvidersYAML`

- [] 通过

---

### 25.3-V12: 运行时数据目录 — 旧路径回退兼容

**验证目标：** 旧路径 .rnix/records/ 存在时优先使用，否则使用 .rnix/data/records/

```bash
go test -race -count=1 -v -run "TestResolveDataDir" ./cmd/rnix/...
```

**预期：** 2 个测试全部 PASS：
- `TestResolveDataDir_NewPath` — 无旧路径时使用 `.rnix/data/records/`
- `TestResolveDataDir_OldPathFallback` — 旧路径存在时优先使用 `.rnix/records/`

- [] 通过

---

### 25.3-V13: 模块适配 — agents/loader 使用 ShadowResolve

```bash
grep -n "ShadowResolve" agents/loader.go
```

**预期：** `Load()` 方法中调用 `config.ShadowResolve(agentName, l.searchDirs...)`

- [] 通过

---

### 25.3-V14: 模块适配 — skills/loader 使用 ShadowResolve

```bash
grep -n "ShadowResolve" skills/loader.go
```

**预期：** `load()` 或 `loadAndParse()` 方法中调用 `config.ShadowResolve(skillName, l.searchDirs...)`

- [] 通过

---

### 25.3-V15: 模块适配 — drivers/llm 使用 config 包

```bash
grep -n "config.GlobalDir\|config.ResolvePath" drivers/llm/config.go
```

**预期：** 通过 `config.GlobalDir()` 和/或 `config.ResolvePath()` 获取配置路径

- [] 通过

---

### 25.3-V16: 模块适配 — kernel/process 含 ProjectConfig 字段

```bash
grep -n "ProjectConfig" kernel/process.go
```

**预期：** `Process` 结构体包含 `ProjectConfig *config.ProjectConfig` 字段

- [] 通过

---

## 端到端完整流程验证

### E2E-01: 全新安装到首次使用

```bash
$RNIX daemon stop 2>/dev/null; true
```

```bash
rm -rf ~/.config/rnix
```

```bash
mkdir -p $TEST_DIR/e2e-project && cd $TEST_DIR/e2e-project
```

```bash
$RNIX init
```

**预期：**
1. 输出 "initialized global config: ~/.config/rnix/"
2. 输出 "initialized project config: .rnix/"

```bash
ls ~/.config/rnix/
```

**预期：** 含 `agents/`、`skills/`、`providers.yaml`、`config.yaml`

```bash
ls .rnix/
```

**预期：** 含 `agents/`、`skills/`、`data/`、`config.yaml`

- [] 通过

---

### E2E-02: 全局配置 + daemon 启动

```bash
$RNIX ps 2>&1
```

**预期：** daemon 从 `~/.config/rnix/providers.yaml` 加载配置，正常启动，输出进程表（可能为空）

- [] 通过

---

### E2E-03: 二次 init 幂等

```bash
echo "# user modified" >> ~/.config/rnix/providers.yaml
```

```bash
echo "# user modified" >> .rnix/config.yaml
```

```bash
$RNIX init
```

**预期：** 输出 "already exists, skipping"

```bash
tail -1 ~/.config/rnix/providers.yaml
```

**预期：** `# user modified`（保留用户修改）

```bash
tail -1 .rnix/config.yaml
```

**预期：** `# user modified`（保留用户修改）

- [] 通过

---

## NFR 验证

### NFR53: init 性能 ≤ 3 秒

```bash
rm -rf ~/.config/rnix
```

```bash
time $RNIX init
```

**预期：** real 时间 ≤ 3 秒（含 embed.FS 提取）

```bash
rm -rf ~/.config/rnix && $RNIX init
```

> 恢复正常配置

- [] 通过

---

### NFR54: ProjectDir 发现 ≤ 10ms（benchmark）

```bash
go test -bench=BenchmarkProjectDir_20Layers -benchmem -count=3 ./internal/config/...
```

**预期：** 每次操作 ≤ 10ms（10,000,000 ns/op）。注：20 层最坏情况约 18ms，典型 3-5 层场景约 3-5ms

- [] 通过

---

### NFR55: YAML 合并 ≤ 50ms（benchmark）

```bash
go test -bench=BenchmarkDeepMergeYAML -benchmem -count=3 ./internal/config/...
```

**预期：** 每次操作 ≤ 50ms（50,000,000 ns/op）。实测约 0.9μs，远低于限制。

- [] 通过

---

## 自动化测试全量验证

### AUTO-01: internal/config 全量测试

```bash
go test -race -count=1 -v ./internal/config/... 2>&1 | tail -5
```

**预期：** 全部通过，含 40+ 测试

- [] 通过

---

### AUTO-02: cmd/rnix init 测试

```bash
go test -race -count=1 -v -run "TestInit|TestResolveDataDir|TestLoadInitConfig|TestWriteDefault" ./cmd/rnix/... 2>&1 | tail -5
```

**预期：** 全部通过（16 测试）

- [] 通过

---

### AUTO-03: agents/skills/ipc shadow 测试

```bash
go test -race -count=1 -v -run "Shadow|ResolveProject" ./agents/... ./skills/... ./ipc/... 2>&1 | tail -10
```

**预期：** 全部通过（agents 3 + skills 2 + ipc 5 = 10 测试）

- [] 通过

---

### AUTO-04: 全项目回归

```bash
make all
```

**预期：** lint 0 issues，vet 通过，全部包测试通过，build 成功

- [] 通过

---

## 测试清理

```bash
$RNIX daemon stop 2>/dev/null; true
```

```bash
rm -rf $TEST_DIR
```

```bash
rm -rf ~/deep-test-08 2>/dev/null; true
```

```bash
# 恢复备份的配置（如有）
for bak in ~/.config/rnix.bak.*; do
    if [ -d "$bak" ]; then
        rm -rf ~/.config/rnix
        mv "$bak" ~/.config/rnix
        echo "已恢复配置: $bak"
        break
    fi
done
```

```bash
unset XDG_CONFIG_HOME TEST_DIR RNIX
```

---

## 关键注意事项

1. **双层配置体系** — 全局 `~/.config/rnix/` + 项目 `.rnix/`。DeepMerge 用于 YAML 配置文件（递归合并），Shadow 用于资源目录（整体遮蔽）
2. **XDG 规范** — 全局目录优先使用 `$XDG_CONFIG_HOME/rnix/`，未设置时回退到 `~/.config/rnix/`
3. **幂等操作** — `rnix init` 永远不覆盖用户已有文件，安全可重复运行
4. **embed.FS 自包含** — 内置 agent/skill 编译进二进制，无需磁盘上的 `lib/` 目录
5. **ProjectDir 遍历边界** — 从 CWD 向上查找 `.rnix/`，到 `$HOME` 或文件系统根停止
6. **.rnix 必须是目录** — `.rnix` 如果是文件而非目录，不被识别为项目标记
7. **ProjectConfig 不可变** — spawn 时创建配置快照，进程生命周期内不可修改
8. **多项目隔离** — 同一 daemon 同时服务多项目，每进程持有独立 ProjectConfig
9. **旧路径兼容** — `.rnix/records/` 等旧路径存在时优先使用，否则使用 `.rnix/data/records/`
10. **Slice 替换** — DeepMergeYAML 对 slice 执行替换而非追加
11. **IPC omitempty** — `SpawnRequest.ProjectDir` 旧版 CLI 不发送时 daemon 按空处理
12. **providers.yaml 错误分级** — 全局不存在用默认（不崩溃），语法错误则启动失败；项目级语法错误仅影响该项目 spawn

## 验证记录

- **验证人：**
- **验证日期：**
- **构建版本：**
- **总用例数：** 42
- **通过数：**
- **失败数：**
- **备注：**
