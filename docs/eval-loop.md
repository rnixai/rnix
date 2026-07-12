# Eval Loop（Agent 行为回归回路）

本文档说明 Rnix 的 agent 行为回归测试体系（`agtest`）如何运转，目标是让回归回路闭环："每次 agent 出丑，测试集就增长"。体系分两层：

- **Tier1**（`tests/agtest/tier1/`）：确定性、离线、<5 分钟，PR 级硬门禁。由 Story 68.1 的 replay driver（脚本化 LLM 响应）驱动，不碰真实 provider / API key。
- **Tier2**（`tests/agtest/tier2/`）：真实 LLM，advisory（不阻塞任何 CI 门禁），手动 / 夜间跑。

设计与实现背景见 `_bmad-output/implementation-artifacts/68-1-replay-scripted-llm-driver.md`、`68-2-golden-suite-first-batch.md`、`68-3-ci-wiring-failure-to-case.md`。基础套件写法（case/script 配对、命名约定、Tier1 四条纪律）见 [`tests/agtest/README.md`](../tests/agtest/README.md)——本文档聚焦"怎么跑"和"怎么把生产失败变成回归用例"。

## 跑法

三种方式，覆盖不同场景：

| 方式 | 命令 | 隔离性 | 用途 |
|------|------|--------|------|
| **隔离 daemon（推荐）** | `make agtest` | 全隔离：独立 socket / 数据目录 / 全局配置（`XDG_RUNTIME_DIR` / `RNIX_DATA_DIR` / `XDG_CONFIG_HOME` 三环境变量），绝不撞用户常驻 daemon | 本地验证 + CI PR 门禁走的就是这条路 |
| **Tier2 advisory** | `make agtest-live` | 不隔离，用你的 ambient daemon + 真实 `providers.yaml`/API key | 手动 / 夜间跑活 LLM 用例，定位 prompt/工具面的真实回归 |
| **直接调用** | `rnix agtest <file-or-dir> [--tier1] [--dry-run]` | 用当前已启动的 daemon（不自动隔离） | 迭代单个用例时更快；`--dry-run` 只解析校验不执行；`--tier1` 额外强制 Tier1 四条纪律 |

`make agtest` 内部依赖 `tests/agtest/run-tier1.sh`：mktemp 三个临时目录 → 写入 `tests/agtest/providers.example.yaml` 的 replay provider 声明到隔离的全局配置 → 启动隔离 daemon → `rnix agtest tests/agtest/tier1/ --tier1` → **无论成败**都 `trap` 清理隔离 daemon 与临时目录。**不进 `make all`**——它跑的是真实 spawn/daemon/VFS 全链路，和 `go test` 是不同的失败面，CI 里作为独立并行 job（见下）。

```bash
make agtest        # Tier1，隔离，几秒到十几秒
make agtest-live   # Tier2，ambient 环境，真实 LLM 延迟
rnix agtest tests/agtest/tier1/ --dry-run --tier1   # 只校验不执行，快速检查新用例是否合规
```

### CI

`.github/workflows/test.yml` 有独立的 `agtest` job，与 `lint` / `vet` 并行（不 `needs` 任何 job——replay 全离线，并行不拉长 PR 总时长），`timeout-minutes: 5` 兜底。job 失败即 workflow 失败，直接阻断合入；不影响 `report` job 的 coverage / pass-rate 结算（那是 `go test` 覆盖率体系，agtest 是独立的行为回归体系）。Tier2 **不接入任何 CI required check**——它的失败可能只是 API 限流/模型漂移，不代表代码回归。

## 加用例

两条路：

### 1. 手写

直接参考 `tests/agtest/tier1/` 里任意一对 `NN-slug.yaml` + `scripts/NN-slug.responses.yaml`，写法规范见 [`tests/agtest/README.md`](../tests/agtest/README.md)。适合你已经知道要测什么行为（新工具、新 action 类型、边界条件）的场景。

### 2. 从生产失败 import（见下一节）

适合"agent 刚刚在真实环境里搞砸了，我想把这次失败原样固化成回归用例"的场景——不用凭记忆手写脚本，直接从已落盘的 `steps.jsonl` 生成骨架。

## 失败转用例流程

这是"回归回路闭环"的核心机制：

```
1. rnix ps -a --uuid                     找到出问题进程的 UUID（或短 ID）
2. rnix agtest import <uuid>             生成用例骨架 + 响应脚本到 tests/agtest/imported/
3. 人工 review                            填真实 assert:，核对 warnings 注释里的疑点
4. 移入 tests/agtest/tier1/               重命名为下一个 NN-slug 序号（case + scripts/ 下的脚本）
5. make agtest                            验证新用例通过；确认没有破坏既有用例
```

展开：

**Step 1 — 定位 UUID。** `rnix ps -a --uuid` 列出所有进程（含已结束的）及其 UUID。`rnix agtest import` 接受三种定位方式：完整 UUID、后 6 位短 ID（dashboard 展示的 `~xxxxxx` 惯例）、前缀（`rnix replay` 的既有先例）。歧义（多个候选匹配同一个短 ID/前缀）会报错并列出候选，绝不静默取第一个。

**Step 2 — 生成骨架。**

```bash
rnix agtest import a1b2c3          # 短 ID
rnix agtest import a1b2c3d4-...    # 完整 uuid
rnix agtest import a1b2c3 --out /tmp/scratch   # 覆盖输出目录（默认 tests/agtest/imported/）
```

工具**直接读盘**（`steps.jsonl` / `proc-info.json` / `events.jsonl`），零 IPC——daemon 是否在跑都能用。生成两个文件：`<slug>.yaml`（用例骨架）+ `scripts/<slug>.responses.yaml`（回放脚本，68-1 格式）。`slug` 固定为 `import-<uuid 后 6 位>`。

**Step 3 — Review。** 生成的骨架**故意不可直接用**：

- 用例文件里**没有**存活的 `assert:` 字段——只有注释形式的建议（`# output.contains` 候选取自 `proc-info.result` 首行、`# syscalls.includes` 候选取自 events.jsonl 里去重的事件类型）。这是特意设计：`agtest.ValidateTier1` 规则 2 要求非空断言，一个没有真实断言的骨架必定被拒——倒逼你真的读一遍再决定断言什么。
- 响应脚本里每一处"尽力重建"（工具输入 JSON 解析失败、meta action 猜测的工具名、legacy 字段回退等）都在文件顶部注释里列出 warning，逐条核对。
- 脚本**永不生成 `usage` 字段**（大 token 数会触发 Compactor，破坏 Tier1 确定性）；如果最后一步不是 `Complete`，也会有 warning 提醒（脚本跑到底会 fail-loud 或撞 `max_steps`，不是真的"跑完"）。

**Step 4 — 移入套件。** Review 通过后，手动把两个文件移到 `tests/agtest/tier1/` 和 `tests/agtest/tier1/scripts/`，按现有序号规律重命名为下一个 `NN-slug`（见 `tests/agtest/README.md` 的命名约定）。工具**不会**自动做这一步——`tests/agtest/imported/` 已加入 `.gitignore`，不会被误提交。

**Step 5 — 验证。** `make agtest` 全绿即完成闭环；顺手确认没有破坏其余用例（`agtest/tier1_guard_test.go` 也会在 `make test` 时再校验一次整个 `tier1/` 目录）。

## Tier1 vs Tier2 边界

| | Tier1 | Tier2 |
|---|---|---|
| LLM | replay driver（脚本化，确定性） | 真实 provider（claude / 其他，需 API key） |
| CI | PR 硬门禁（`agtest` job，<5 分钟） | 不接入任何 CI required check，advisory |
| 断言 | 仅 `output` / `syscalls`（`agtest.ValidateTier1` 强制），**禁止** `quality` | 允许 `assert.quality`（LLM judge，默认 haiku），本身就依赖非确定性判断 |
| `agent.provider` | 必须是 `"replay"`（约定死的实例名） | 具名真实 provider（如 `claude`），或留空走 `default_provider` |
| 目录 | `tests/agtest/tier1/`（`ParseDir` 非递归扫描，case 与 `scripts/` 下的响应脚本分离） | `tests/agtest/tier2/` |
| 运行方式 | `make agtest`（隔离 daemon） | `make agtest-live`（ambient daemon，需你自己配置好 provider/key） |
| 失败含义 | 代码/行为回归——阻断合入 | 可能只是模型漂移/限流/网络——仅供人工排查，不代表回归 |

选哪层的经验法则：**能用脚本化响应稳定复现的行为验证，一律进 Tier1**（这是常规做法）；**只有验证"真实模型在这个 prompt 下大概率做对某件事"这类本质非确定性的问题**，才值得写一条 Tier2 advisory 用例（如 `tests/agtest/tier2/01-live-sample.yaml`）。

## 引用

- [`tests/agtest/README.md`](../tests/agtest/README.md) — 套件目录结构、Tier1 四条断言纪律、response script 写法
- `_bmad-output/implementation-artifacts/68-1-replay-scripted-llm-driver.md` — replay driver / 响应脚本 schema
- `_bmad-output/implementation-artifacts/68-2-golden-suite-first-batch.md` — `ValidateTier1` 四规则 / 首批 Tier1 用例
- `_bmad-output/implementation-artifacts/68-3-ci-wiring-failure-to-case.md` — 隔离 daemon / CI 接线 / `rnix agtest import` 本文档所述机制的实现依据
