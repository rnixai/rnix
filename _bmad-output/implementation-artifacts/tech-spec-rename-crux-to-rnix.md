---
title: '项目全量改名 Crux → Rnix'
slug: 'rename-crux-to-rnix'
created: '2026-03-06'
status: 'ready-for-dev'
stepsCompleted: [1, 2, 3, 4]
tech_stack: ['Go 1.26', 'Cobra', 'lipgloss', 'bubbletea v2']
files_to_modify: ['278 files total - see Implementation Plan for categorized breakdown']
code_patterns: ['module path replacement', 'import alias cruxctx → rnixctx', 'hardcoded string replacement', 'directory rename', 'file rename', 'registry URL replacement']
test_patterns: ['make all (lint+vet+test+build)', 'zero residual grep', 'daemon socket path verification', 'IPC cross-terminal test', 'compose up/down test']
---

# Tech-Spec: 项目全量改名 Crux → Rnix

**Created:** 2026-03-06

## Overview

### Problem Statement

项目品牌从 Crux 重命名为 Rnix。新的 module path 为 `github.com/rnixai/rnix`，CLI 名称为 `rnix`，域名为 `rnix.ai`。需要对代码库进行全量替换，覆盖 278 个文件、3203 处引用，无例外。

### Solution

分层执行全量搜索替换 + 目录/文件重命名。按依赖顺序处理：先改目录结构，再改 go.mod，再改所有 Go 代码，再改非 Go 文件，最后验证。使用 `sed` 批量替换 + `git mv` 目录/文件重命名。验证标准：`make all` 通过 + 零残留搜索 + daemon/IPC/compose 手动验证。

### Scope

**In Scope:**
- go.mod module path：`github.com/usecrux/crux` → `github.com/rnixai/rnix`
- 旧 module path 残留（仅文档）：`github.com/gonewx/crux` → `github.com/rnixai/rnix`
- 所有 .go 文件中的 import 路径（219 处）
- Import 别名：`cruxctx` → `rnixctx`（16 个文件、~90 处使用）
- CLI 二进制名称：`crux` → `rnix`
- 入口目录：`cmd/crux/` → `cmd/rnix/`
- 配置文件重命名：`crux-init.yaml` → `rnix-init.yaml`，`crux-compose.yaml` → `rnix-compose.yaml`
- 配置文件内部引用
- Socket/PID 路径：`crux/crux.sock` → `rnix/rnix.sock`，`crux.pid` → `rnix.pid`，`/tmp/crux-$UID/` → `/tmp/rnix-$UID/`
- Makefile：BINARY、PKG、cmd 路径
- .gitignore 中的二进制名
- scripts/monitor.sh 中的引用（~15 处）
- 品牌名替换：`Crux` → `Rnix`，`crux` → `rnix`，`CRUX` → `RNIX`
- 所有文档（docs/ 11 文件 ~200 引用 + _bmad-output/ 153 文件 ~2421 引用）
- compose testdata 中的注释引用（2-3 处）
- MCP transport 中的 name 字段
- 注册表 URL：`registry.crux.dev` → `registry.rnix.ai`
- 环境变量：`CRUX_LOG_DIR` → `RNIX_LOG_DIR`
- SKILL.md 和 .meta/idea.md 中的引用

**Out of Scope:**
- 第三方依赖（go.sum 由 `go mod tidy` 自动更新）
- git 历史重写
- CI/CD 管道配置（项目尚无）
- 域名 DNS/基础设施配置
- testdata/ 下的测试 fixture 文件（已确认无 crux 引用）

## Context for Development

### Codebase Patterns

**替换模式分类（按优先级，长匹配优先短匹配）：**

| # | 模式 | 替换规则 | 影响范围 |
|---|------|---------|---------|
| 1 | Go module path | `github.com/usecrux/crux` → `github.com/rnixai/rnix` | go.mod + 所有 .go import |
| 2 | 旧 module path | `github.com/gonewx/crux` → `github.com/rnixai/rnix` | 仅文档 |
| 3 | 注册表 URL | `registry.crux.dev` → `registry.rnix.ai` | skillpkg/client.go + 文档 |
| 4 | Import 别名 | `cruxctx` → `rnixctx` | 16 个 .go 文件 |
| 5 | 配置文件名 | `crux-init.yaml` → `rnix-init.yaml` | Go 代码 + 文档 |
| 6 | 配置文件名 | `crux-compose.yaml` → `rnix-compose.yaml` | Go 代码 + 文档 |
| 7 | Socket 文件名 | `crux.sock` → `rnix.sock` | ipc/ + 文档 |
| 8 | PID 文件名 | `crux.pid` → `rnix.pid` | ipc/ + 文档 |
| 9 | 环境变量 | `CRUX_LOG_DIR` → `RNIX_LOG_DIR` | scripts/monitor.sh |
| 10 | 组织名 | `usecrux` → `rnixai` | Makefile、文档 |
| 11 | 组织名（旧） | `gonewx` → `rnixai` | 仅文档 |
| 12 | 品牌名大写 | `CRUX` → `RNIX` | 环境变量、少量文档 |
| 13 | 品牌名首字母大写 | `Crux` → `Rnix` | 文档标题、注释、help text |
| 14 | CLI/路径小写 | `crux` → `rnix` | 命令名、文件名、路径 |

**关键约束：替换必须按上表顺序执行（长匹配优先），防止短模式 `crux` 提前破坏长模式 `cruxctx` 或 `usecrux` 中的子串。**

**潜在冲突：** 无。所有 "crux" 引用均为项目自身标识符，无第三方库名冲突。

### Files to Reference

| File | Purpose | 引用数 |
| ---- | ------- | ------ |
| `go.mod` | Module path 定义 | 1 |
| `Makefile` | 构建配置（BINARY、PKG、cmd 路径） | 4 |
| `cmd/crux/main.go` | CLI 入口、help text、版本输出 | 41 |
| `cmd/crux/compose.go` | compose 命令、默认文件名 | 25 |
| `cmd/crux/compose_test.go` | compose 测试 | 25 |
| `cmd/crux/main_test.go` | CLI 测试 | 15 |
| `cmd/crux/integration_test.go` | 集成测试 | 16 |
| `cmd/crux/top.go` | top 命令标题 | 8 |
| `cmd/crux/log.go` | log 命令 | 15 |
| `cmd/crux/skill.go` | skill 命令 | 17 |
| `ipc/protocol.go` | Socket 路径硬编码 `crux/crux.sock` | 6 |
| `ipc/daemon.go` | Daemon PID 文件 `crux.pid` | 3 |
| `ipc/daemon_test.go` | Daemon 测试 | 11 |
| `kernel/kernel.go` | 内核主体 + cruxctx 别名 | 15 |
| `kernel/kernel_test.go` | 内核测试 + cruxctx 别名 | 27 |
| `kernel/init.go` | `crux-init.yaml` 加载 | 5 |
| `kernel/reap_test.go` | 进程回收测试 + cruxctx | 17 |
| `drivers/mcp/transport.go` | MCP name: `"crux"` | 1 |
| `skillpkg/client.go` | `registry.crux.dev` | 1 |
| `.gitignore` | `/crux` 二进制 | 1 |
| `scripts/monitor.sh` | 监控脚本 | 9 |
| `_bmad-output/project-context.md` | AI 项目上下文 | 8 |
| `docs/quick-start.md` | 快速开始指南 | 28 |
| `docs/reference.md` | 命令参考 | 30 |

### Technical Decisions

- **批量替换工具**：使用 `find` + `sed -i` 按文件类型分组批量替换，效率最高
- **替换顺序严格遵循长匹配优先**：先替换 `github.com/usecrux/crux`（module path），再替换 `cruxctx`（别名），最后替换 `crux`（短模式）
- **目录/文件重命名使用 `git mv`**：保留 git 追踪历史
- **go.sum 不手动修改**：由 `go mod tidy` 自动重新生成
- **单次提交**：所有改动作为一个 atomic commit

## Implementation Plan

### Tasks

#### Phase 1: 目录和文件结构重命名

- [ ] Task 1: 重命名 CLI 入口目录
  - File: `cmd/crux/` → `cmd/rnix/`
  - Action: `git mv cmd/crux cmd/rnix`
  - Notes: 必须第一步执行，后续所有 .go 文件路径依赖此结构

- [ ] Task 2: 重命名根目录配置文件
  - File: `crux-init.yaml` → `rnix-init.yaml`
  - Action: `git mv crux-init.yaml rnix-init.yaml`
  - File: `crux-compose.yaml` → `rnix-compose.yaml`
  - Action: `git mv crux-compose.yaml rnix-compose.yaml`

- [ ] Task 3: 重命名 _bmad-output 中文件名含 crux 的文件（6 个）
  - Files:
    - `_bmad-output/implementation-artifacts/4-4-crux-ps-command-and-process-table-ui.md`
    - `_bmad-output/implementation-artifacts/7-1-crux-compose-yaml-parsing-and-dag-scheduling-engine.md`
    - `_bmad-output/implementation-artifacts/7-2-crux-compose-up-command.md`
    - `_bmad-output/implementation-artifacts/7-3-crux-compose-down-command.md`
    - `_bmad-output/implementation-artifacts/10-1-crux-top-realtime-monitoring-tui.md`
    - `_bmad-output/implementation-artifacts/10-2-crux-log-categorized-reasoning-logs.md`
  - Action: 每个文件 `git mv` 将 `crux` 替换为 `rnix`

#### Phase 2: Go Module Path 和 Import 替换

- [ ] Task 4: 更新 go.mod module path
  - File: `go.mod`
  - Action: `module github.com/usecrux/crux` → `module github.com/rnixai/rnix`

- [ ] Task 5: 替换所有 .go 文件中的 module import path
  - Files: 所有 `**/*.go` 文件（~90 个）
  - Action: `find . -name '*.go' -exec sed -i 's|github.com/usecrux/crux|github.com/rnixai/rnix|g' {} +`
  - Notes: 219 处 import 声明，这是最大批量替换

- [ ] Task 6: 替换 import 别名 `cruxctx` → `rnixctx`
  - Files: 16 个 .go 文件（cmd/rnix/main.go, cmd/rnix/main_test.go, cmd/rnix/integration_test.go, kernel/kernel.go, kernel/kernel_test.go, kernel/reap_test.go, kernel/budget_test.go, kernel/e2e_test.go, kernel/mount_test.go, kernel/init_test.go, kernel/supervisor_test.go, kernel/spawn_mcp_test.go, kernel/phase2_toolerror_test.go, ipc/daemon_test.go, ipc/server_test.go, ipc/idle_test.go, ipc/client_test.go, ipc/integration_test.go, drivers/llm/tools_test.go）
  - Action: `find . -name '*.go' -exec sed -i 's/cruxctx/rnixctx/g' {} +`
  - Notes: ~90 处使用（import 声明 + 函数调用）

#### Phase 3: Go 源码硬编码字符串替换

- [ ] Task 7: 替换注册表 URL
  - File: `skillpkg/client.go`
  - Action: `registry.crux.dev` → `registry.rnix.ai`

- [ ] Task 8: 替换所有 .go 文件中的品牌名和 CLI 名称
  - Files: 所有 `**/*.go` 文件
  - Action: 按顺序执行以下 sed 替换（长匹配优先）：
    1. `sed -i 's/crux-init\.yaml/rnix-init.yaml/g'` — 配置文件名
    2. `sed -i 's/crux-compose\.yaml/rnix-compose.yaml/g'` — 配置文件名
    3. `sed -i 's/crux\.sock/rnix.sock/g'` — Socket 文件名
    4. `sed -i 's/crux\.pid/rnix.pid/g'` — PID 文件名
    5. `sed -i 's/Crux/Rnix/g'` — 品牌名（首字母大写）
    6. `sed -i 's/crux/rnix/g'` — CLI 名称和路径（小写）
  - Notes: Task 5 和 6 已处理 module path 和 cruxctx，此处处理剩余的短模式。由于 `usecrux` 中的 `crux` 已在 Task 5 中随 module path 一起被替换，此处的 `crux` → `rnix` 不会误伤。

#### Phase 4: 非 Go 文件替换

- [ ] Task 9: 更新 Makefile
  - File: `Makefile`
  - Action:
    - `BINARY := crux` → `BINARY := rnix`
    - `PKG := github.com/usecrux/crux` → `PKG := github.com/rnixai/rnix`
    - `./cmd/crux/` → `./cmd/rnix/`

- [ ] Task 10: 更新 .gitignore
  - File: `.gitignore`
  - Action: `/crux` → `/rnix`

- [ ] Task 11: 更新 scripts/monitor.sh
  - File: `scripts/monitor.sh`
  - Action: 按顺序替换（长匹配优先）：
    1. `CRUX_LOG_DIR` → `RNIX_LOG_DIR`
    2. `crux-monitor.log` → `rnix-monitor.log`
    3. `crux.sock` → `rnix.sock`
    4. `crux daemon` → `rnix daemon`
    5. `crux ps` → `rnix ps`
    6. `Crux` → `Rnix`
    7. `crux` → `rnix`（剩余短匹配，如目录名 `/tmp/crux-`）

- [ ] Task 12: 更新 compose testdata YAML 文件
  - Files: `compose/testdata/integration-pipe-equiv.yaml`, `compose/testdata/integration-compose-monitor.yaml`
  - Action: `crux` → `rnix`（注释中的 CLI 示例）

- [ ] Task 13: 更新 rnix-init.yaml 内部注释
  - File: `rnix-init.yaml`（已在 Task 2 重命名）
  - Action: `Crux` → `Rnix`（注释中的品牌名）

- [ ] Task 14: 更新 lib/skills 和 .meta 中的引用
  - File: `lib/skills/code-analysis/SKILL.md`
  - File: `.meta/idea.md`
  - Action: `crux`/`Crux` → `rnix`/`Rnix`

#### Phase 5: 文档全量替换

- [ ] Task 15: 替换 docs/ 目录所有 Markdown 文件
  - Files: `docs/*.md` + `docs/tutorials/*.md`（11 个文件，~200 处引用）
  - Action: 按顺序 sed 替换（长匹配优先）：
    1. `github.com/usecrux/crux` → `github.com/rnixai/rnix`
    2. `github.com/gonewx/crux` → `github.com/rnixai/rnix`
    3. `registry.crux.dev` → `registry.rnix.ai`
    4. `crux-init.yaml` → `rnix-init.yaml`
    5. `crux-compose.yaml` → `rnix-compose.yaml`
    6. `crux.sock` → `rnix.sock`
    7. `crux.pid` → `rnix.pid`
    8. `usecrux` → `rnixai`
    9. `Crux` → `Rnix`
    10. `crux` → `rnix`

- [ ] Task 16: 替换 docs/docs_test.go
  - File: `docs/docs_test.go`
  - Action: 同 Task 15 的替换规则（此文件是 Go 测试但位于 docs/ 且内容为文档验证）

- [ ] Task 17: 替换 _bmad-output/ 所有文件
  - Files: `_bmad-output/**/*.md` + `_bmad-output/**/*.yaml`（153 个文件，~2421 处引用）
  - Action: 同 Task 15 的替换规则
  - Notes: 这是引用量最大的区域，但全部是文档文本，sed 替换安全

#### Phase 6: 构建验证

- [ ] Task 18: 删除 go.sum 并重新生成
  - Action: `rm go.sum && go mod tidy`
  - Notes: module path 改变后 go.sum 需要完全重新生成

- [ ] Task 19: 全量构建验证
  - Action: `make all`（lint → vet → test → build）
  - Notes: 所有测试必须通过，编译产物为 `rnix`

- [ ] Task 20: 零残留验证
  - Action: `grep -r "crux\|Crux\|CRUX\|usecrux\|gonewx" --include="*.go" --include="*.md" --include="*.yaml" --include="*.sh" --include="Makefile" --include=".gitignore"`
  - Notes: 必须返回空结果。如有残留，定位并修复。
  - 额外检查: `find . -name '*crux*' -not -path './.git/*'` 确认无文件名残留

- [ ] Task 21: 删除旧二进制文件
  - Action: `rm -f crux`（旧编译产物）
  - Notes: 新产物为 `./rnix`

### Acceptance Criteria

- [ ] AC 1: Given go.mod 已更新为 `module github.com/rnixai/rnix`，when 运行 `go build ./cmd/rnix/`，then 编译成功生成 `rnix` 二进制文件
- [ ] AC 2: Given 所有 .go 文件 import 已更新，when 运行 `go vet ./...`，then 无错误输出
- [ ] AC 3: Given 所有代码已替换，when 运行 `make all`（lint + vet + test + build），then 全部通过，退出码 0
- [ ] AC 4: Given 改名完成，when 执行 `grep -r "crux\|Crux\|CRUX\|usecrux\|gonewx" --include="*.go" --include="*.md" --include="*.yaml" --include="*.sh" --include="Makefile" --include=".gitignore"`，then 输出为空（零残留）
- [ ] AC 5: Given 改名完成，when 执行 `find . -name '*crux*' -not -path './.git/*' -not -path './_bmad-output/implementation-artifacts/tech-spec-wip.md'`，then 输出为空（无文件名残留）
- [ ] AC 6: Given `rnix-init.yaml` 存在于项目根目录，when 运行 `./rnix -i "hello"`，then daemon 正常启动，socket 创建于 `$XDG_RUNTIME_DIR/rnix/rnix.sock`
- [ ] AC 7: Given daemon 已启动，when 在另一个终端运行 `./rnix ps`，then IPC 连接成功并显示进程列表
- [ ] AC 8: Given `rnix-compose.yaml` 存在于项目根目录，when 运行 `./rnix compose up`，then 正常解析并启动多智能体工作流
- [ ] AC 9: Given compose 工作流已启动，when 运行 `./rnix compose down`，then 正常停止所有智能体
- [ ] AC 10: Given Makefile 已更新，when 运行 `make build`，then 产物名为 `rnix`（非 `crux`）
- [ ] AC 11: Given .gitignore 已更新，when 运行 `git status`，then `rnix` 二进制不被 git 追踪

## Additional Context

### Dependencies

- 无新依赖引入
- `go mod tidy` 更新 go.sum（module path 变更导致所有 checksum 重算）
- 旧 daemon 进程（如有运行中的）需先停止：socket 路径已变更，旧 daemon 无法被新 CLI 连接

### Testing Strategy

**自动化验证：**
- `make all`（lint → vet → test → build）：覆盖所有单元测试、集成测试、代码质量检查
- 零残留 grep：确认 278 个文件、3203 处引用全部替换
- 文件名残留检查：确认无 crux 文件/目录名残留

**手动验证：**
- daemon 启动：`./rnix -i "test intent"` → 检查 socket 路径
- IPC 通信：另一终端 `./rnix ps` → 确认跨终端连接
- compose 工作流：`./rnix compose up` + `./rnix compose down`
- help 文本：`./rnix --help` → 确认所有输出中无 crux 残留

### Notes

**高风险项：**
- sed 替换顺序错误可能导致 double-replacement（如 `cruxctx` 被 `crux` 规则先匹配为 `rnixctx` 不是问题，但 `usecrux` → 如果 `crux` 先被替换会变成 `usernix` 而非 `rnixai`）。必须严格按长匹配优先顺序执行。
- `go mod tidy` 可能失败如果网络不通（需要从 proxy 拉取）。备选：离线模式 `GONOSUMDB=* GOFLAGS=-mod=mod go mod tidy`。

**已知限制：**
- `registry.rnix.ai` 域名可能尚未注册，相关功能（skill install/search/update）在域名就绪前不可用（现有 `registry.crux.dev` 同样不可用，不影响）
- git 历史中仍保留旧名称，不做历史重写

**后续工作（Out of Scope）：**
- 更新 MEMORY.md 中的项目记忆（改名 commit 完成后）
- 更新 `_bmad/bmm/config.yaml` 中的 `project_name`（如需要）
- GitHub 仓库重命名（组织名/仓库名）
- 域名 `rnix.ai` 和 `registry.rnix.ai` 配置
