# Process Resumption

本文档说明 Rnix 进程恢复的设计哲学、命令、与故障排查。Rnix 自 Epic 42 起把"Dead 是终态"的 Unix 默认语义升级为"Dead 是冻结状态"：进程数据保留在磁盘上直到 gc 清理，任何 Dead/Zombie 进程都可以通过 `rnix resume` 命令复活继续推进。

## 为什么 Dead 不是终态

2026-05-16 EchoMatrix 长任务诊断报告暴露了一个反复出现的痛点：明明磁盘上有完整的 `.rnix/data/steps/<uuid>/` 观察数据（`steps.jsonl` / `events.jsonl` / `ctx-profile.json` / `process-meta.json` / 可选 `checkpoint.json`），但 daemon 崩溃后 / 用户手动 kill 后 / 进程自然 complete 后却无法继续——状态机锁死了恢复入口。

对抗性审查后选定 **Bundle 1（A5 + B1 + C1）** 方案：

- **A5 状态机零改动**：不引入新的"Frozen"状态，沿用 Dead/Zombie/Suspended。Resume 不是状态转换，而是基于历史"造一个新进程"。
- **B1 双模式 UUID**：默认 `rnix resume <uuid>` 继承原 UUID（续跑语义），`--fork` 切到新 UUID + `origin_uuid` 链路（探索语义）。
- **C1 周期 best-effort checkpoint**：每 5 步或 30 秒写一次 `checkpoint.json`，避免长任务从头重放。失败不阻塞主推理循环。

设计文档详见 `_bmad-output/test-artifacts/decision-42-4-time-travel-reuse.md` 和 ADR `Decision 40`。

## Resume 模式对比

| 模式 | 命令 | UUID | 用途 |
|------|------|------|------|
| 续跑 | `rnix resume <uuid>` | 继承原 UUID | daemon 崩溃后无损恢复 |
| 分叉 | `rnix resume --fork <uuid>` | 新 UUID + origin_uuid | git 式探索 |
| 截断分叉 | `rnix resume --fork --from-step N <uuid>` | 新 UUID | 从历史中段重试 |
| Compose 节点 | `rnix compose resume --node <name>` | 复用上述 | DAG 失败节点单独恢复 |

实现层面：
- 续跑 + Suspended 状态：走 `checkpoint.json` 全量上下文恢复（Story 30.4 / 42.2 路径），效率最高。
- 续跑 + Dead/Zombie：走 `resumeFromHistory`（steps.jsonl 重放），无 checkpoint 兜底。
- 分叉：新 PID + 新 UUID，`OriginUUID` 字段链接到父，便于 Dashboard 显示血缘。
- 截断分叉：`--from-step` 仅在 history 路径生效；和 checkpoint 冲突时直接报错（ErrInvalid）。

## 韧性与 gc 协同

42-2 周期 checkpoint（默认每 5 步 / 30 秒）保证：daemon crash 时可以从最近的 checkpoint 而非进程起点重放。checkpoint.json 也是 `.rnix/data/steps/<uuid>/` 下的文件之一，gc 清理 UUID 目录时一并清除——这是预期行为，因为 gc 只清 Dead/Zombie 进程（AC#3），而 Suspended/Running 进程永久豁免（用户主动 resume 之前 checkpoint 不会被回收）。

gc 策略（Story 42.5）：

```yaml
# ~/.config/rnix/config.yaml
gc:
  retention_days: 30   # 删除 dead_at 早于 30 天的条目；0 = 关闭
  max_entries: 500     # 保留最多 500 条历史；超出按 dead_at 升序清理；0 = 关闭
  interval_seconds: 3600  # 后台扫描周期，最小 60，默认 3600 (1h)
```

边界：
- `retention_days` 与 `max_entries` 同时配置时取并集（命中任一即清理）。
- 全零关闭整个 gc daemon，magic 默认值。
- Running/Suspended 永远豁免（即使 `dead_at` 字段为空且 `created_at` 已超期）。
- `proc-info.json` 损坏 / `dead_at` 字段为空 → 保守跳过，warning 日志。

CLI 操作：

```bash
rnix gc --dry-run        # 预览候选（表格）
rnix gc --dry-run --json # 预览候选（JSON，脚本友好）
rnix gc                  # 实际清理；> 100 条会提示 [y/N]
rnix gc --force          # 跳过提示直接清理
rnix gc --json           # JSON 输出（隐含 --force）
```

## 故障排查

常见错误消息（均为 `ErrNotFound` / `ErrInvalid`）：

- `ErrNotFound: process data has been garbage collected (UUID xxx)`
  — gc 已自动清理。检查 `gc.retention_days` / `max_entries` 配置；如果想保留更久，调大数值并重启 daemon。
- `ErrNotFound: no data found for UUID xxx: never spawned or never persisted`
  — UUID 拼写错误，或被手动 `rm -rf .rnix/data/steps/<uuid>` 删除。`rnix ps --history` 查实际可用 UUID。
- `ErrNotFound: checkpoint not found for UUID xxx`
  — Suspended 路径必需 checkpoint.json。如果是 Dead 进程，应该走 history 路径，检查 ResumeWithOpts 的状态分支是否正确。
- `ErrInvalid: from_step N invalid (must be >= 0)`
  — `--from-step` 必须非负。
- `ErrInvalid: from_step N exceeds total steps M`
  — `--from-step` 超过 steps.jsonl 的实际行数。
- `ErrInvalid: from_step N requires history path; checkpoint at step M (full snapshot, no partial replay)`
  — `--from-step` 与 checkpoint 不兼容。先 `--fork` 切到 history 路径。
- `ErrInvalid: process with UUID xxx is in Running state, cannot resume`
  — 进程还在跑。先 `rnix kill -SIGPAUSE <pid>` 暂停，或等待 complete。

### Dashboard 显示

- `rnix top`：默认隐藏 Dead 进程；按 `H` 显示历史视图，看完整 Resume 链路。
- `rnix dashboard`：Inspector → Lineage tab 显示 `OriginUUID` 链 + 截断标记（`Truncated=true` 表示链超 32 层或检测到循环）。
- Process detail panel: 显示 `ResumedFromStep` 字段，方便分辨续跑还是新 spawn。

## 引用

- `_bmad-output/problem-solution-2026-05-16.md` — Bundle 1 决策动机
- `_bmad-output/implementation-artifacts/42-1-resume-mechanism-layer.md` — `ResumeWithOpts` + `OriginUUID` 设计
- `_bmad-output/implementation-artifacts/42-2-resilience-checkpoint-and-daemon-scan.md` — checkpoint 与 daemon 启动扫描
- `_bmad-output/implementation-artifacts/42-3-observability-from-step-dashboard-lineage.md` — `--from-step` 与 Lineage
- `_bmad-output/implementation-artifacts/42-4-ecosystem-time-travel-compose-resume.md` — Compose 节点 resume + Time Travel 边界决策
- `_bmad-output/implementation-artifacts/42-5-governance-gc-and-documentation.md` — gc 策略 + 本文档
- `_bmad-output/test-artifacts/decision-42-4-time-travel-reuse.md` — Time Travel 与 Resume 关系决策
- `_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md` — ADR Decision 40
