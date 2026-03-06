# Epic 8: Skill 包管理与生态（Skill Package Management）

用户通过 CLI 命令管理社区 Skill：搜索、安装、更新、列出——构建 Crux 的能力生态系统。

> **基础设施前置条件**
>
> 本 Epic 的 install/search/update 功能依赖社区注册中心服务（`https://registry.crux.dev`）。客户端代码（`skillpkg/client.go`）已实现，通过 HTTP API 与注册中心交互。注册中心需提供以下端点：
>
> | 端点 | 用途 |
> |------|------|
> | `GET /index.yaml` | Skill 索引（搜索用） |
> | `GET /packages/{name}/latest.yaml` | 版本解析 |
> | `GET /packages/{name}/{version}.tar.gz` | 包下载 |
>
> **当前状态**：客户端已实现并测试（通过 mock HTTP），注册中心服务端部署为独立运维任务，不阻塞客户端功能的交付。`skill list` 命令仅依赖本地注册表，无需注册中心即可工作。

## Story 8.1: skill install 安装

As a 用户,
I want 通过 `skill install <name>` 从社区仓库安装 Skill,
So that 我可以快速获取社区共享的能力模块。

**Acceptance Criteria:**

**Given** `skillpkg/client.go` 已实现
**When** 调用社区仓库 API
**Then** 支持 Skill 下载、版本解析、完整性验证（SHA256 校验）

**Given** `cmd/crux/skill.go` 中 install 子命令已注册
**When** 执行 `skill install code-analysis`
**Then** 从社区仓库下载 Skill 包（`.tar.gz` 格式）
**And** 安装到本地 `lib/skills/code-analysis/` 目录
**And** 更新本地 Skill 注册表（`lib/skills/.registry.yaml`）

**Given** 批量安装
**When** 执行 `skill install pr-reviewer code-analyst tech-writer`
**Then** 依次安装三个 Skill，每个显示安装进度

**Given** Skill 已安装
**When** 再次执行 `skill install code-analysis`
**Then** 提示已安装且显示当前版本，使用 `--force` 可覆盖

**Given** 安装的 Skill 包含有效的 SKILL.md
**When** Agent 引用该 Skill
**Then** 无需任何修改即可使用（NFR30）

## Story 8.2: skill search 搜索

As a 用户,
I want 通过 `skill search <keyword>` 搜索社区仓库中可用的 Skill,
So that 我可以发现适合我需求的能力模块。

**Acceptance Criteria:**

**Given** `cmd/crux/skill.go` 中 search 子命令已注册
**When** 执行 `skill search code`
**Then** 返回匹配的 Skill 列表（从注册中心 `/index.yaml` 获取）
**And** 每条结果包含：名称、描述、版本、下载量

**Given** 搜索结果为空
**When** 无匹配关键词
**Then** 输出 `No skills found for "keyword".` + 建议（检查拼写或浏览全部 Skill）

**Given** 使用 `--json` flag
**When** 搜索
**Then** 输出 JSON 数组，字段 snake_case

## Story 8.3: skill update 更新

As a 用户,
I want 通过 `skill update [name]` 更新已安装的 Skill,
So that 我始终使用最新兼容版本的能力模块。

**Acceptance Criteria:**

**Given** `cmd/crux/skill.go` 中 update 子命令已注册
**When** 执行 `skill update code-analysis`
**Then** 检查社区仓库中的最新兼容版本
**And** 如果有更新，下载并替换本地版本
**And** 更新本地注册表

**Given** 不指定名称
**When** 执行 `skill update`
**Then** 检查所有已安装的社区 Skill 的更新
**And** 显示可更新列表，确认后批量更新

**Given** 已是最新版本
**When** 执行更新
**Then** 输出 `code-analysis is already up to date (v1.2.0).`

## Story 8.4: 本地 Skill 注册表与 skill list

As a 用户,
I want 通过 `skill list` 查看所有已安装的 Skill,
So that 我了解本地可用的能力模块。

**Acceptance Criteria:**

**Given** 本地 Skill 注册表（`lib/skills/.registry.yaml`）已维护
**When** 执行 `skill list`
**Then** 输出表格：NAME、VERSION、SOURCE（builtin/community）、DESCRIPTION
**And** 包含系统自带 Skill 和社区安装的 Skill

**Given** 无已安装 Skill（除系统自带）
**When** 执行 `skill list`
**Then** 显示系统自带 Skill + `Tip: skill search <keyword> 发现更多 Skill`

**Given** 使用 `--json` flag
**When** 列出
**Then** 输出 JSON 数组

---
