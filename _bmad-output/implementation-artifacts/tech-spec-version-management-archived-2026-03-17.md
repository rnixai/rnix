---
title: '版本管理体系'
slug: 'version-management'
created: '2026-03-15'
status: 'completed'
stepsCompleted: [1, 2, 3, 4]
tech_stack: [Go, Make, Git]
files_to_modify:
  - cmd/rnix/main.go
  - cmd/rnix/main_test.go
  - Makefile
code_patterns:
  - 'package-level var + ldflags injection'
  - 'Cobra Command.SetOut() for testable output'
  - 'claudeVersionChecker function var injection for test isolation'
  - 'JSONResponse struct for --json output'
  - 'version string passed to ipc.NewServer()'
test_patterns:
  - 'save/restore global vars (claudeVersionChecker, flagJSON)'
  - 'cobra.Command.SetOut(&buf) stdout capture'
  - 'json.Unmarshal into JSONResponse for JSON assertions'
---

# Tech-Spec: 版本管理体系

**Created:** 2026-03-15

## Overview

### Problem Statement

当前 rnix 的版本号硬编码在 `cmd/rnix/main.go:39` 为 `var version = "0.1.0"`，没有构建元数据（git commit、构建时间）注入，没有 git tag，无法区分开发构建和正式发布，也没有标准化的发布流程。

### Solution

建立完整的语义化版本管理体系：通过 Go ldflags 在构建时注入版本号、git commit hash 和构建日期；无 ldflags 时自动显示 `-dev` 标记；增强 `rnix version` 命令输出；在 Makefile 中提供 `release` target 实现一键校验 + 打 tag + 构建的发布流程。

### Scope

**In Scope:**

- `version`、`gitCommit`、`buildDate` 三个变量改为 ldflags 注入
- 无 ldflags 构建时显示 `dev` 标记
- `rnix version` 命令输出增强（含 commit、构建时间）
- `rnix version --json` 输出增强
- Makefile `build` target 自动注入 git 信息
- Makefile `release` target（语义化版本校验 + 打 tag + 构建）
- git tag 使用 `v` 前缀（如 `v0.2.0`）

**Out of Scope:**

- CI/CD 自动化发布（GitHub Actions）
- Changelog 自动生成
- 多平台交叉编译

## Context for Development

### Codebase Patterns

- **Version 变量**：`cmd/rnix/main.go:39` 声明 `var version = "0.1.0"`，是唯一的版本源。Go 标准做法是通过 `-ldflags "-X main.version=..."` 在构建时覆盖。
- **runVersion() 函数**（`main.go:184-211`）：根据 `flagJSON` 输出两种格式 — 纯文本（`rnix v0.1.0` + claude-code 版本）或 JSON（`JSONResponse` 结构体）。
- **Test 隔离模式**：测试通过 save/restore `claudeVersionChecker` 和 `flagJSON` 全局变量隔离副作用；通过 `cobra.Command.SetOut(&buf)` 捕获 stdout。
- **IPC 传递**：`runDaemon()` 在 `main.go:1166` 将 `version` 传给 `ipc.NewServer()`，server 在 `handlePing()` 中返回 `PingResponse{Version: s.version}`。`daemon status` 命令通过 Ping 获取并显示版本。
- **Makefile 风格**：简洁，使用 `$(BINARY)` 和 `$(PKG)` 变量，`.PHONY` 声明，无复杂 shell 逻辑。

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `cmd/rnix/main.go:39` | `var version = "0.1.0"` — 唯一版本源，ldflags 注入目标 |
| `cmd/rnix/main.go:184-211` | `runVersion()` — 纯文本和 JSON 两种输出路径 |
| `cmd/rnix/main.go:190-197` | JSON 输出 — `map[string]any` 构造，含 `version`、`claude_code_available`、`claude_code` |
| `cmd/rnix/main.go:204` | 纯文本输出 — `fmt.Fprintf(w, "rnix v%s\n", version)` |
| `cmd/rnix/main.go:1166` | `ipc.NewServer(nil, agentLoader.Load, version)` — 传递给 daemon |
| `cmd/rnix/main.go:1049-1064` | `runDaemonStatus()` — 通过 IPC Ping 获取 daemon 版本 |
| `cmd/rnix/main_test.go:289-366` | 3 个现有测试：`TestVersion_WithClaude`、`TestVersion_WithoutClaude`、`TestVersion_JSON` |
| `ipc/server.go:65,106,356` | Server 存储 version 字段，`handlePing()` 返回 |
| `ipc/protocol.go:358-360` | `PingResponse{Version string}` |
| `Makefile` | `build` target：`go build -o $(BINARY) ./cmd/rnix/`，无 ldflags |

### Technical Decisions

1. **ldflags 变量放置**：三个变量（`version`、`gitCommit`、`buildDate`）都放在 `cmd/rnix/main.go` 作为 package-level var。这是 Go 社区标准做法，`-X main.version=...` 路径最简。

2. **Dev 构建检测**：通过 `gitCommit` 是否为空判断 — 无 ldflags 注入时 `gitCommit == ""`，显示 `0.1.0-dev`。有 ldflags 时显示精确版本如 `0.2.0 (abc1234, 2026-03-15)`。避免引入额外的 `isDev` 变量。

3. **IPC PingResponse 不扩展**：`PingResponse.Version` 保持 `string` 类型不变。daemon status 显示的版本字符串已包含构建信息（因为注入了 `version` 变量）。详细信息（commit、date）仅在 `rnix version` CLI 命令中展示。

4. **Makefile release 流程**：`make release VERSION=0.2.0` 执行：semver 格式校验 → 工作区干净检查 → 测试 → 打 `v0.2.0` tag → 带 ldflags 构建。不自动 push tag，让用户手动 `git push --tags` 确认。

5. **Makefile build 也注入**：日常 `make build` 自动注入 git commit 和 date（从 `git rev-parse --short HEAD` 和 `date -u`），version 取 `git describe --tags --abbrev=0 2>/dev/null` 或回退到源码中的默认值。全局变量命名为 `GIT_VERSION`（而非 `VERSION`），避免与 `make release VERSION=x.y.z` 的用户参数冲突。

## Implementation Plan

### Tasks

**Task 1: 增加 ldflags 变量声明** (`cmd/rnix/main.go`)

- [x] 1.1 将 `var version = "0.1.0"` 改为三个变量块：
  - File: `cmd/rnix/main.go:39`
  - Action: 替换为：
    ```go
    var (
        version   = "0.1.0"
        gitCommit = ""
        buildDate = ""
    )
    ```
  - Notes: 默认值确保 `go run` / `go build` 无 ldflags 时仍可运行。`version` 保留 `"0.1.0"` 作为源码真实版本号（每次发布后手动 bump）。

**Task 2: 增加 `versionString()` 辅助函数** (`cmd/rnix/main.go`)

- [x] 2.1 新增一个函数统一生成版本显示字符串：
  - File: `cmd/rnix/main.go`（紧接变量声明之后）
  - Action: 新增：
    ```go
    func versionString() string {
        if gitCommit == "" {
            return version + "-dev"
        }
        return version
    }
    ```
  - Notes: 集中判断逻辑，`runVersion()` 和 IPC 传递共用。

**Task 3: 增强 `runVersion()` 纯文本输出** (`cmd/rnix/main.go`)

- [x] 3.1 修改纯文本输出路径，增加 commit 和 date 信息：
  - File: `cmd/rnix/main.go:204`
  - Action: 将 `fmt.Fprintf(w, "rnix v%s\n", version)` 替换为：
    ```go
    fmt.Fprintf(w, "rnix v%s\n", versionString())
    if gitCommit != "" {
        fmt.Fprintf(w, "commit:  %s\n", gitCommit)
    }
    if buildDate != "" {
        fmt.Fprintf(w, "built:   %s\n", buildDate)
    }
    ```

**Task 4: 增强 `runVersion()` JSON 输出** (`cmd/rnix/main.go`)

- [x] 4.1 扩展 JSON 输出字段：
  - File: `cmd/rnix/main.go:190-197`
  - Action: 在 `data` map 中增加字段：
    ```go
    data := map[string]any{
        "version":              versionString(),
        "git_commit":           gitCommit,
        "build_date":           buildDate,
        "claude_code_available": claudeAvailable,
    }
    ```

**Task 5: 更新 IPC Server version 传递** (`cmd/rnix/main.go`)

- [x] 5.1 将传给 `NewServer()` 的 version 改用 `versionString()`：
  - File: `cmd/rnix/main.go:1166`
  - Action: `ipc.NewServer(nil, agentLoader.Load, version)` → `ipc.NewServer(nil, agentLoader.Load, versionString())`

**Task 6: 更新现有测试 + 新增测试** (`cmd/rnix/main_test.go`)

- [x] 6.1 更新 `TestVersion_WithClaude`：将断言从 `strings.Contains(output, "rnix v")` 改为 `strings.Contains(output, "rnix v0.1.0-dev")`，确保实际验证 `-dev` 后缀（测试环境无 ldflags，`gitCommit == ""`）
- [x] 6.2 更新 `TestVersion_WithoutClaude`：同上，将 `"rnix v"` 断言改为 `"rnix v0.1.0-dev"`
- [x] 6.3 更新 `TestVersion_JSON`：验证 JSON 包含 `git_commit` 和 `build_date` 字段
- [x] 6.4 新增 `TestVersionString_Dev`：`gitCommit == ""` 时返回 `version + "-dev"`
- [x] 6.5 新增 `TestVersionString_Release`：临时设置 `gitCommit = "abc1234"` 后返回纯 `version`
- [x] 6.6 新增 `TestVersion_WithBuildInfo`：临时设置 `gitCommit` 和 `buildDate`，验证纯文本输出包含 `commit:` 和 `built:` 行

**Task 7: Makefile 增加 ldflags 注入** (`Makefile`)

- [x] 7.1 在 Makefile 顶部增加版本变量计算：
  - File: `Makefile`
  - Action: 在 `PKG` 行之后新增：
    ```makefile
    GIT_VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "0.1.0")
    GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null)
    BUILD_DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
    LDFLAGS := -X main.version=$(GIT_VERSION) -X main.gitCommit=$(GIT_COMMIT) -X main.buildDate=$(BUILD_DATE)
    ```
  - Notes: 使用 `GIT_VERSION`（而非 `VERSION`）作为自动检测的版本变量，避免与 `release` target 中用户传入的 `VERSION` 参数冲突。
- [x] 7.2 修改 `build` target 使用 ldflags：
  - Action: `go build -o $(BINARY) ./cmd/rnix/` → `go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/rnix/`
- [x] 7.3 修改 `install` target 使用 ldflags：
  - Action: `go install ./cmd/rnix/` → `go install -ldflags "$(LDFLAGS)" ./cmd/rnix/`

**Task 8: Makefile 增加 `release` target** (`Makefile`)

- [x] 8.1 新增 `release` target：
  - File: `Makefile`
  - Action: 新增：
    ```makefile
    .PHONY: release
    release:
    	@test -n "$(VERSION)" || (echo "ERROR: VERSION is required. Usage: make release VERSION=0.2.0"; exit 1)
    	@echo "==> Validating version format..."
    	@echo "$(VERSION)" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$' || (echo "ERROR: VERSION must be semver (e.g. 0.2.0)"; exit 1)
    	@echo "==> Checking working tree is clean..."
    	@git diff --quiet && git diff --cached --quiet || (echo "ERROR: working tree is not clean"; exit 1)
    	@echo "==> Running tests..."
    	$(MAKE) all
    	@echo "==> Creating tag v$(VERSION)..."
    	git tag -a "v$(VERSION)" -m "Release v$(VERSION)"
    	@echo "==> Building release binary..."
    	go build -ldflags "-X main.version=$(VERSION) -X main.gitCommit=$$(git rev-parse --short HEAD) -X main.buildDate=$$(date -u '+%Y-%m-%dT%H:%M:%SZ')" -o $(BINARY) ./cmd/rnix/
    	@echo ""
    	@echo "Done! Release v$(VERSION) tagged and built."
    	@echo "To publish: git push origin v$(VERSION)"
    ```
  - Notes: `release` target 使用独立的 `VERSION` 变量（用户通过命令行传入），与顶部 `GIT_VERSION` 互不干扰。使用 shell 级 `test -n` 检查替代 Make `ifndef` 指令（后者因顶部 `GIT_VERSION` 不会检测到空 `VERSION`，而 `test -n` 在 recipe 执行时检查实际传入值）。

### Acceptance Criteria

**AC1: Dev 构建标识**

- [x] Given 直接 `go build ./cmd/rnix/`（无 ldflags）
  When 执行 `./rnix version`
  Then 输出包含 `rnix v0.1.0-dev`，不包含 `commit:` 和 `built:` 行

**AC2: ldflags 构建标识**

- [x] Given 使用 `make build` 构建
  When 执行 `./rnix version`
  Then 输出包含 `rnix v<tag>` 且不含 `-dev`，包含 `commit: <7位hash>` 和 `built: <ISO时间>`

**AC3: JSON 输出增强**

- [x] Given 使用 `make build` 构建
  When 执行 `./rnix version --json`
  Then JSON 包含 `version`（不含 `-dev`）、`git_commit`（非空）、`build_date`（非空）字段

**AC4: JSON 输出 — dev 构建**

- [x] Given 直接 `go build`（无 ldflags）
  When 执行 `./rnix version --json`
  Then JSON 中 `version` 值为 `"0.1.0-dev"`，`git_commit` 为 `""`，`build_date` 为 `""`

**AC5: Daemon 传递版本**

- [x] Given 使用 `make build` 构建并启动 daemon
  When 执行 `rnix daemon status`
  Then `version:` 行显示不含 `-dev` 的版本号

**AC6: Release — 正常流程**

- [x] Given 工作区干净，测试通过
  When 执行 `make release VERSION=0.2.0`
  Then 创建 annotated tag `v0.2.0`，构建的二进制 `./rnix version` 显示 `rnix v0.2.0`

**AC7: Release — 版本格式校验**

- [x] Given 任意工作区状态
  When 执行 `make release VERSION=abc` 或 `make release VERSION=1.0`
  Then 报错 `VERSION must be semver` 并退出

**AC8: Release — 工作区脏检查**

- [x] Given 工作区有未提交的修改
  When 执行 `make release VERSION=0.2.0`
  Then 报错 `working tree is not clean` 并退出

**AC9: Release — 缺少 VERSION 参数**

- [x] Given 任意状态
  When 执行 `make release`（不传 VERSION）
  Then 报错 `VERSION is required`

**AC10: 向后兼容 — 现有测试通过**

- [x] Given 代码修改完成
  When 执行 `make all`
  Then 所有现有测试通过（含 IPC 集成测试中硬编码的 `"0.1.0-test"`）

## Additional Context

### Dependencies

无新外部依赖。纯 Go 标准库 + Make + Git。

### Testing Strategy

- **单元测试**（`cmd/rnix/main_test.go`）：
  - `versionString()` 在有/无 `gitCommit` 时的返回值
  - `runVersion()` 纯文本输出在 dev/release 模式下的内容差异
  - `runVersion()` JSON 输出包含新增字段
  - 通过临时修改 `gitCommit`/`buildDate` 包级变量测试，遵循现有 save/restore 模式
- **现有测试兼容**：
  - IPC 测试（`ipc/server_test.go`、`ipc/client_test.go`）使用 `"0.1.0-test"` 硬编码版本字符串传给 `NewServer()`，不受影响
  - 现有 version 测试需更新断言：`"rnix v"` → `"rnix v0.1.0-dev"`（因为测试环境 `gitCommit == ""`）
- **手动验证**：
  - `go build ./cmd/rnix/ && ./rnix version`（dev 模式）
  - `make build && ./rnix version`（ldflags 模式）
  - `make release VERSION=99.0.0`（release 流程，使用不会冲突的测试版本号）

### Notes

- 每次发布后需手动更新 `cmd/rnix/main.go` 中的 `version` 默认值为下一个开发版本（如 `0.2.0` → `0.3.0`）。这是有意的设计 — 保持源码中的版本号作为 "当前开发中版本" 的 single source of truth。
- `git describe --tags --abbrev=0` 在无 tag 时回退到 `"0.1.0"`（源码默认值），确保首次 `make build` 不会报错。**注意：Makefile 中的回退值 `"0.1.0"` 必须与 `cmd/rnix/main.go` 中 `var version = "0.1.0"` 保持同步。** 每次 bump 源码版本时，同步更新 Makefile 回退值。
- `make release` 中使用 `git tag -a`（annotated tag）而非轻量 tag，这样 `git describe` 能正确识别。

## Review Notes

- 对抗性代码审查完成
- 发现：9 个总计，4 个已修复，5 个跳过（设计选择/当前可接受）
- 处理方式：自动修复
- 已修复：F1（release 避免双重 build）、F2（测试并发安全注释）、F3（release 检查 untracked files）、F8（测试版本动态化）
- 已跳过：F4（JSON 空字符串为设计选择）、F5（可重现构建当前不需要）、F6（versionString 边界可忽略）、F7（DRY 已在 Notes 记录）、F9（semver 预发布当前不需要）
