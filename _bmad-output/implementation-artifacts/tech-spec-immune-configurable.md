---
title: 'Immune 系统可配置化（阈值 + 默认禁用）'
type: 'feature'
created: '2026-03-23'
status: 'done'
baseline_commit: 'e06a22c'
context: ['CLAUDE.md']
---

# Immune 系统可配置化（阈值 + 默认禁用）

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Immune 系统的异常检测阈值（`DefaultDeviationThreshold=3.0`、`MinSamplesForProfile=5` 等）全部硬编码，且在样本量少时标准差估计不准导致正常流程被误判为异常并暂停。用户无法调整阈值，也无法禁用 Immune 以避免干扰。

**Approach:** 新增 `ImmuneConfig` 结构体，支持通过项目级配置文件 `.rnix/config.yaml` 的 `immune` 字段配置启用/禁用和各项阈值。默认禁用 Immune（`enabled: false`）。daemon 启动时读取配置，条件性初始化 ImmuneDaemon。

## Boundaries & Constraints

**Always:**
- 配置结构体在 `kernel/` 包内定义，避免循环依赖
- 不提供配置时使用合理默认值（enabled=false, deviation_threshold=3.0, min_samples=5, reinforcement_threshold=5, min_migration_similarity=0.3）
- 现有 IPC/CLI 命令在 immune 禁用时返回清晰的 "immune disabled" 状态

**Ask First:**
- 是否需要 `rnix immune config` CLI 子命令来查看/修改运行时配置

**Never:**
- 不修改已有的 immune CLI 命令签名
- 不引入环境变量覆盖机制（保持配置文件单一来源）

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| 无配置文件 | `.rnix/config.yaml` 不存在 | Immune 禁用（默认） | N/A |
| 显式启用 | `immune.enabled: true` | ImmuneDaemon 正常初始化并监控 | N/A |
| 显式禁用 | `immune.enabled: false` | 不创建 ImmuneDaemon，kernel.immuneDaemon=nil | N/A |
| 自定义阈值 | `immune.deviation_threshold: 5.0` | AnomalyDetector 使用 5.0 | N/A |
| 无效阈值 | `immune.deviation_threshold: -1` | 回退到默认值 3.0，打印 warn | 日志警告 |
| immune 禁用时查询状态 | `rnix immune status` | 显示 "immune: disabled" | N/A |

</frozen-after-approval>

## Code Map

- `kernel/immune_config.go` (new) -- ImmuneConfig 结构体定义和默认值工厂
- `cmd/rnix/main.go:1287-1299` -- daemon 初始化中 ImmuneDaemon 创建逻辑，改为条件性
- `cmd/rnix/main.go:1221-1234` -- 全局 config.yaml 加载（当前解析后丢弃），改为提取 immune 配置
- `cmd/rnix/immune.go` -- immune CLI 命令，需处理 disabled 状态
- `ipc/server.go` -- immune IPC handler，需处理 daemon=nil 场景

## Tasks & Acceptance

**Execution:**
- [ ] `kernel/immune_config.go` (new) -- 定义 `ImmuneConfig` 结构体（Enabled, DeviationThreshold, MinSamples, ReinforcementThreshold, MinMigrationSimilarity）和 `DefaultImmuneConfig()` 工厂函数（enabled=false）；添加 `ParseImmuneConfig(data map[string]any) ImmuneConfig` 从 YAML map 解析
- [ ] `kernel/immune.go` -- 将 `DefaultDeviationThreshold`、`MinSamplesForProfile`、`DefaultReinforcementThreshold`、`MinMigrationSimilarity` 4 个常量改为 `ImmuneConfig` 字段引用；`NewImmuneDaemon` 接受 `ImmuneConfig` 参数
- [ ] `cmd/rnix/main.go` -- 在 config.yaml 加载处提取 `immune` 字段并调用 `ParseImmuneConfig`；根据 `cfg.Enabled` 条件性创建 ImmuneDaemon
- [ ] `cmd/rnix/immune.go` -- immune CLI 子命令在 daemon=nil 时输出 disabled 提示而非报错
- [ ] `kernel/immune_config_test.go` (new) -- 单元测试 ParseImmuneConfig 的正常/边界/无效输入场景

**Acceptance Criteria:**
- Given `.rnix/config.yaml` 不存在或无 `immune` 字段，when daemon 启动，then ImmuneDaemon 不被创建
- Given `immune.enabled: true` 且 `deviation_threshold: 5.0`，when daemon 启动，then AnomalyDetector 使用 5.0 阈值
- Given immune 禁用，when 运行 `rnix immune status`，then 输出包含 "disabled"
- Given 无效配置值（负数阈值），when 解析配置，then 回退默认值并输出警告

## Verification

**Commands:**
- `go test -race ./kernel/... -run TestImmuneConfig` -- expected: PASS
- `make all` -- expected: lint+vet+test+build 全部通过
