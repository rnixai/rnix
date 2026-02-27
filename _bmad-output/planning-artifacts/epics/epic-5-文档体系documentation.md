# Epic 5: 文档体系（Documentation）

新用户可以通过概念文档理解 Crux 的 OS 范式，通过快速上手指南在 15 分钟内跑通 demo，通过参考手册查阅所有 syscall、VFS 路径和 CLI 命令。

## Story 5.1: 概念文档

As a 新用户,
I want 阅读概念文档理解 Crux 的核心 OS 范式,
So that 我能建立正确的心智模型来使用 Crux。

**Acceptance Criteria:**

**Given** 概念文档已编写
**When** 阅读文档
**Then** 覆盖四个核心概念：进程（Process）、虚拟文件系统（VFS）、Skill、系统调用（Syscall）
**And** 每个概念包含：定义、与 Unix 对应概念的类比、至少一个具体示例
**And** 概念之间的关系清晰（进程通过 syscall 访问 VFS，Skill 注入进程能力）

## Story 5.2: 快速上手指南

As a 新用户,
I want 按照快速上手指南从安装到跑通第一个 demo,
So that 我在 15 分钟内体验到 Crux 的核心价值。

**Acceptance Criteria:**

**Given** 快速上手指南已编写
**When** 按步骤操作
**Then** 覆盖完整流程：安装 Go → 安装 Crux（`go install`）→ 验证（`crux version`）→ 首次执行（`crux "分析 ./README.md"`）→ 查看结果 → 首次 astrace（`crux astrace 1`）
**And** 目标完成时间 ≤ 15 分钟（FR39）
**And** 每一步包含预期输出示例，用户可对照验证

## Story 5.3: 参考手册

As a 开发者,
I want 查阅参考手册获取所有 syscall、VFS 路径和 CLI 命令的完整规范,
So that 我在编写 Skill 或调试时有权威参考。

**Acceptance Criteria:**

**Given** 参考手册已编写
**When** 查阅内容
**Then** 包含 MVP 全部 15 个 syscall 的签名、参数、返回值、错误码、示例
**And** 包含完整 VFS 路径规范（`/proc/{pid}/`、`/dev/llm/`、`/dev/fs`、`/dev/shell`、`/lib/skills/`）
**And** 包含 agent.yaml 全部字段说明和示例、SKILL.md（Agent Skills 行业标准）全部字段说明和示例
**And** 包含 CLI 命令完整列表（`crux "意图"`、`crux ps`、`crux astrace`、`crux kill`、`crux version`）及其 flags
**And** 包含 IPC 架构说明：daemon 生命周期（自动启动/自动停止/stale socket 清理）、Unix domain socket 通信机制、IPC 协议概述（NDJSON 消息格式、Method 枚举、流式 StreamEvent）、连接复用语义（非流式请求 Ping/ListProcs/Kill 复用同一连接，流式请求 Spawn/AttachDebug 终结连接）

---
