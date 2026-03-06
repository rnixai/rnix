---
stepsCompleted: ['step-01-preflight', 'step-02-identify-targets', 'step-03-generate-tests', 'step-03c-aggregate', 'step-04-validate']
lastStep: 'step-04-validate'
lastSaved: '2026-03-05'
source: 'tech-spec-phase2-integration-bugfixes.md'
detected_stack: 'backend'
execution_mode: 'sequential'
---

# Phase 2 集成验证 BUG 修复 — 测试自动化摘要

## 概述

基于 `_bmad-output/implementation-artifacts/tech-spec-phase2-integration-bugfixes.md` 中定义的 8 个 BUG 修复，生成补充测试以填补已识别的覆盖缺口。

## 技术栈

- **语言**: Go 1.26
- **框架**: 标准 `testing` 包 + `testify` 断言
- **竞态检测**: `-race` 已启用
- **命名约定**: `TestType_Method`

## 测试覆盖计划

### P0 — 关键路径

| BUG | 测试文件 | 测试数量 | 覆盖内容 |
|-----|---------|---------|---------|
| BUG-004 (错误传播) | `kernel/phase2_toolerror_test.go` | 4 | HasToolError 标记、三个错误路径（Open/Write/Read 失败）、正常路径对照 |
| BUG-007/008 (日志历史) | `kernel/phase2_loghistory_test.go` | 6 | 基本插入、空历史、顺序保持、环形缓冲溢出、容量边界、返回副本 |

### P1 — 高优先级

| BUG | 测试文件 | 测试数量 | 覆盖内容 |
|-----|---------|---------|---------|
| BUG-002 (Dead TTL) | `kernel/phase2_deadttl_test.go` | 5 | TTL 过期删除、保留最近 Dead、忽略非 Dead 状态、零 DeadAt 防护、混合状态场景 |
| BUG-006 (checkIdle) | `ipc/idle_test.go` | 2 | Zombie 不阻止关闭、Running 阻止关闭 |

### P2 — 中等优先级

| BUG | 测试文件 | 测试数量 | 覆盖内容 |
|-----|---------|---------|---------|
| BUG-005 (Skill 本地检查) | `skillpkg/installer_local_test.go` | 3 | 本地目录存在返回 AlreadyInstalled、--force 绕过、无效 SKILL.md 继续网络安装 |

### 已有充分覆盖（跳过）

| BUG | 原因 |
|-----|------|
| BUG-001 (Token 计数) | `claude_cli_test.go` 已有 15+ 测试覆盖 input/output token 解析 |
| BUG-003 (超时可见性) | 文档改进，不需要代码测试 |

## 生成的测试文件

| 文件 | 新测试数 | 测试级别 | 通过 |
|------|---------|---------|------|
| `kernel/phase2_loghistory_test.go` | 6 | 单元 | ✅ |
| `kernel/phase2_deadttl_test.go` | 5 | 单元 | ✅ |
| `kernel/phase2_toolerror_test.go` | 4 | 集成 | ✅ |
| `skillpkg/installer_local_test.go` | 3 | 单元 | ✅ |
| `ipc/idle_test.go` | 2 | 集成 | ✅ |
| **合计** | **20** | | ✅ |

## 发现的 BUG

### cleanupExpiredDead 死锁 (严重)

**文件**: `kernel/reap.go:194-205`
**问题**: `cleanupExpiredDead` 在 `SyncMap.Range` 回调内部调用 `RemoveProcess(pid)`。`Range` 持有读锁，`RemoveProcess` 调用 `Delete` 需要写锁 → `sync.RWMutex` 死锁。
**修复**: 在 Range 遍历期间收集待删除的 PID，遍历结束后再统一删除。
**状态**: 已修复并通过竞态检测验证。

## 验证结果

```
$ go test -count=1 -race ./kernel/... ./skillpkg/... ./ipc/...
ok   github.com/rnixai/rnix/kernel    3.710s
ok   github.com/rnixai/rnix/skillpkg  1.120s
ok   github.com/rnixai/rnix/ipc       5.914s
```

- 20 个新测试全部通过
- 竞态检测无告警
- 现有测试无回归

## AC 覆盖矩阵

| AC | 测试 | 状态 |
|----|------|------|
| AC 2.2 (TTL 过期删除) | `TestCleanupExpiredDead_RemovesExpired` | ✅ |
| AC 2.3 (Dead 不影响 rnix top) | `TestCleanupExpiredDead_KeepsRecentDead` | ✅ |
| AC 4.1 (tool 错误写入 context) | `TestReasonStep_ToolOpenFails_SetsHasToolError` | ✅ |
| AC 4.2 (HasToolError → exit=1) | `TestReasonStep_Tool{Open,Write,Read}Fails_SetsHasToolError` | ✅ |
| AC 4.5 (LLM 继续推理) | `TestReasonStep_ToolOpenFails_SetsHasToolError` (验证 Result 非空) | ✅ |
| AC 5.1 (本地 skill 返回 AlreadyInstalled) | `TestInstaller_Install_LocalDirExists_NotInRegistry` | ✅ |
| AC 5.2 (--force 绕过) | `TestInstaller_Install_LocalDirExists_ForceBypass` | ✅ |
| AC 5.3 (无效 SKILL.md 继续) | `TestInstaller_Install_LocalDirExists_InvalidSkill` | ✅ |
| AC 6.1 (Zombie 不阻止空闲) | `TestTryAutoShutdown_ZombieOnlyProcs` | ✅ |
| AC 6.2 (Running 阻止空闲) | `TestTryAutoShutdown_RunningProcsPreventShutdown` | ✅ |
| AC 7.5 (环形缓冲 256 溢出) | `TestAppendLogHistory_RingBufferOverflow` | ✅ |
