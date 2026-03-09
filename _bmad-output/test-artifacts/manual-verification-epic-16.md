# Epic 16 手工验证指南：推理回归测试（agtest）

## 概述

本文档提供 Epic 16 所有 Story 的手工验证步骤，用于在自动化测试之外对功能进行端到端的人工确认。

## 前置准备

Daemon 由 rnix 自动按需启动（`EnsureDaemon`），无需手动管理。

```bash
# 1. 构建最新版本
make build

# 2. 准备一个测试用例 YAML 文件
mkdir -p /tmp/agtests

cat > /tmp/agtests/greeting.yaml << 'EOF'
version: "1.0"
name: "test-greeting"
intent: "向用户打招呼并介绍自己"
agent:
  name: "greeter"
timeout: 30000
assert:
  output:
    contains: ["你好"]
  syscalls:
    includes: ["CtxWrite"]
EOF

# 3. 准备一个测试套件 YAML 文件
cat > /tmp/agtests/suite.yaml << 'EOF'
version: "1.0"
name: "basic-suite"
tests:
  - name: "greeting-test"
    intent: "向用户打招呼"
    agent:
      name: "greeter"
    assert:
      output:
        contains: ["你好"]
  - name: "analysis-test"
    intent: "分析当前目录结构"
    agent:
      name: "analyst"
    assert:
      syscalls:
        includes: ["Read"]
EOF
```

> **提示**：agtest 执行需要 daemon 运行（通过 IPC spawn agent）。`--dry-run` 模式不需要 daemon。

---

## Story 16.1: 声明式测试用例定义

### YAML 解析

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 解析单个测试用例 | `rnix agtest /tmp/agtests/greeting.yaml --dry-run` | 显示解析结果摘要：测试用例数量（1）、名称列表 | [ ] |
| 2 | 解析测试套件 | `rnix agtest /tmp/agtests/suite.yaml --dry-run` | 显示解析结果摘要：测试用例数量（2）、各用例名称 | [ ] |
| 3 | 解析目录 | `rnix agtest /tmp/agtests/ --dry-run` | 扫描目录下所有 `.yaml` 文件，显示合并后的用例总数 | [ ] |
| 4 | 空目录 | `rnix agtest /tmp/empty-dir/ --dry-run`（空目录） | 显示错误：无可用的测试文件 | [ ] |
| 5 | 不存在的路径 | `rnix agtest /tmp/nonexistent.yaml --dry-run` | 显示文件/目录不存在的错误 | [ ] |

### 校验与错误报告

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 6 | 缺少 intent | 创建 YAML（无 intent 字段），执行 `rnix agtest <file> --dry-run` | 报告校验错误：`intent` 字段缺失，包含行号 | [ ] |
| 7 | 缺少 agent.name | 创建 YAML（agent 无 name），执行 `rnix agtest <file> --dry-run` | 报告校验错误：`agent.name` 字段缺失，包含行号 | [ ] |
| 8 | 缺少 version | 创建 YAML（无 version 字段），执行 --dry-run | 报告校验错误：`version` 字段缺失 | [ ] |
| 9 | 无效 version | 创建 YAML（`version: "2.0"`），执行 --dry-run | 报告校验错误：version 必须为 "1.0" | [ ] |
| 10 | 无效 YAML 语法 | 创建语法错误的 YAML，执行 --dry-run | 报告 YAML 解析错误 | [ ] |
| 11 | 多个校验错误 | 创建同时缺少 intent 和 agent 的 YAML | 一次性报告所有校验错误 | [ ] |
| 12 | 行号准确性 | 检查校验错误中的行号信息 | 行号与 YAML 文件中字段实际位置匹配 | [ ] |

### 完整 Schema 测试

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 13 | 所有可选字段 | 创建包含 model、skills、context_budget、timeout、assert 等所有可选字段的 YAML，执行 --dry-run | 解析成功，所有字段正确识别 | [ ] |
| 14 | 最小有效用例 | 创建仅包含 version、intent、agent.name 的 YAML | 解析成功，可选字段为默认值 | [ ] |

---

## Story 16.2: 三种断言类型

> 前提：已有可用的测试用例 YAML 文件和运行中的 daemon。

### 输出断言（Output Assert）

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | contains 通过 | 创建 YAML：`assert.output.contains: ["你好"]`，agent 输出包含"你好"，执行 agtest | 测试通过（✓） | [ ] |
| 2 | contains 失败 | 创建 YAML：`assert.output.contains: ["不存在的文本xyz"]`，执行 agtest | 测试失败（✗），显示期望值和实际值 | [ ] |
| 3 | not_contains 通过 | 创建 YAML：`assert.output.not_contains: ["错误"]`，agent 输出不含"错误" | 测试通过 | [ ] |
| 4 | not_contains 失败 | 创建 YAML：`assert.output.not_contains: ["你好"]`，agent 输出包含"你好" | 测试失败，显示不应包含的文本出现了 | [ ] |
| 5 | 混合 contains + not_contains | 同时设置 contains 和 not_contains | 两种检查均执行，每项返回独立结果 | [ ] |

### Syscall 断言（Syscall Assert）

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 6 | includes 通过 | 创建 YAML：`assert.syscalls.includes: ["CtxWrite"]` | 测试通过，agent 调用了 CtxWrite | [ ] |
| 7 | includes 失败 | 创建 YAML：`assert.syscalls.includes: ["Mount"]`（agent 不使用 Mount） | 测试失败，显示缺失的 syscall | [ ] |
| 8 | excludes 通过 | 创建 YAML：`assert.syscalls.excludes: ["Kill"]`（正常不调用 Kill） | 测试通过 | [ ] |
| 9 | excludes 失败 | 创建 YAML：`assert.syscalls.excludes: ["CtxWrite"]`（必然调用） | 测试失败，显示不应出现的 syscall | [ ] |
| 10 | 大小写敏感 | 创建 YAML：`assert.syscalls.includes: ["ctxwrite"]`（小写） | 测试失败（syscall 名称大小写敏感） | [ ] |

### 质量断言（Quality Assert）

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 11 | 质量通过 | 创建 YAML：`assert.quality.criteria: "输出必须包含问候语"` | 通过 LLM 评估，测试通过（附评估分数和原因） | [ ] |
| 12 | 质量失败 | 创建 YAML：`assert.quality.criteria: "输出必须包含完整的代码实现"`（agent 只是打招呼） | 测试失败，附 LLM 评估原因 | [ ] |

### 断言配置校验

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 13 | 空 output 断言 | 创建 YAML：`assert.output:` 但 contains 和 not_contains 均为空 | 校验报错：output 断言不能为空 | [ ] |
| 14 | 空 syscalls 断言 | 创建 YAML：`assert.syscalls:` 但 includes 和 excludes 均为空 | 校验报错：syscalls 断言不能为空 | [ ] |
| 15 | 空 quality criteria | 创建 YAML：`assert.quality.criteria: ""` | 校验报错：criteria 必须非空 | [ ] |
| 16 | 无断言 | 创建 YAML 不含 assert 字段 | 按 agent exit code 判定（0=通过，非零=失败） | [ ] |

---

## Story 16.3: 批量测试运行与报告

> 前提：daemon 正在运行，已有测试 YAML 文件。

### 单文件执行

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 执行单个测试文件 | `rnix agtest /tmp/agtests/greeting.yaml` | 执行测试用例，显示结果报告（✓ 或 ✗） | [ ] |
| 2 | 通过的测试 | 执行一个断言能通过的测试 | 显示 `✓ test-name`，报告摘要为 `1 passed, 0 failed` | [ ] |
| 3 | 失败的测试 | 执行一个断言会失败的测试 | 显示 `✗ test-name`，附失败断言详情：断言类型、期望值、实际值 | [ ] |

### 批量执行

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | 执行测试套件 | `rnix agtest /tmp/agtests/suite.yaml` | 按顺序运行所有测试用例，逐个显示结果 | [ ] |
| 5 | 执行整个目录 | `rnix agtest /tmp/agtests/` | 扫描目录下所有 YAML 文件，按顺序执行所有用例 | [ ] |
| 6 | 混合通过/失败 | 目录中包含会通过和会失败的测试 | 每个用例独立报告，最终显示汇总：`N passed, M failed` | [ ] |

### 报告格式

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 7 | 纯文本报告 | 正常执行 agtest | 每个用例以 `✓`（通过）或 `✗`（失败）开头，失败用例附详情 | [ ] |
| 8 | 失败断言详情 | 有失败用例时查看报告 | 显示断言类型（output/syscall/quality）、期望值、实际值、差异说明 | [ ] |
| 9 | JSON 报告 | `rnix agtest /tmp/agtests/greeting.yaml --json` | 输出 JSON 格式的 SuiteResult，包含每个用例的 CaseResult | [ ] |
| 10 | 汇总统计 | 多用例执行完成 | 报告底部显示总用例数、通过数、失败数、错误数 | [ ] |

### 超时与错误处理

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 11 | 用例超时 | 创建 YAML 设置 `timeout: 1000`（1 秒），intent 是复杂任务 | 超时后用例标记为 error，显示超时信息 | [ ] |
| 12 | 全局超时 | `rnix agtest tests/ --timeout 5000` | 使用 5 秒全局超时 | [ ] |
| 13 | Daemon 不可用 | 停止 daemon 后 `rnix agtest test.yaml` | 显示 daemon 不可用的友好错误 | [ ] |
| 14 | 退出码 | 有失败用例时检查进程退出码 | 退出码为 1（有失败时非零退出） | [ ] |
| 15 | 全部通过退出码 | 所有用例通过时检查退出码 | 退出码为 0 | [ ] |

### Spawn 错误

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 16 | Agent spawn 失败 | 创建 YAML 指定不存在的 agent name | 用例标记为 error，显示 spawn 失败原因 | [ ] |

---

## 端到端完整流程验证

> 此节验证从定义到执行的完整工作流。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 完整 agtest 流程 | ① 编写测试 YAML 文件（含三种断言类型） ② `rnix agtest test.yaml --dry-run` 验证解析 ③ `rnix agtest test.yaml` 执行测试 ④ 查看报告 | dry-run 解析正确，执行后报告展示每种断言类型的结果 | [ ] |
| 2 | 回归测试场景 | ① 为项目功能编写一组测试用例 ② `rnix agtest tests/` 批量执行 ③ 修改代码后再次执行 | 两次执行结果可对比，验证修改是否引入回归 | [ ] |
| 3 | 三种断言混合 | 创建包含 output + syscall + quality 三种断言的用例，执行 agtest | 三种断言独立评估，每种都在报告中显示结果 | [ ] |
| 4 | 目录批量 + JSON | ① 目录下放多个 YAML 文件 ② `rnix agtest tests/ --json` | JSON 输出包含所有用例结果和汇总统计 | [ ] |

---

## 关键注意事项

1. **agtest 需要 daemon** — 测试执行通过 IPC spawn agent，需要 daemon 运行；`--dry-run` 不需要
2. **顺序执行** — 测试用例按定义顺序依次执行，不并行
3. **YAML Schema** — version 必须为 `"1.0"`，intent 和 agent.name 为必填字段
4. **Syscall 名称大小写敏感** — 必须与 kernel 实际 emit 的 Syscall 字段值完全匹配（如 `CtxWrite` 而非 `ctxwrite`）
5. **质量断言使用 LLM** — quality assert 通过 LLM 评估，会额外 spawn 一次评估任务
6. **NFR35** — 单个测试用例框架开销（不含 LLM 调用）<= 500ms
7. **退出码** — 有失败用例时进程退出码为 1，全部通过时为 0
8. **超时优先级** — 用例级 `timeout` > CLI `--timeout` 全局超时（默认 60000ms）
9. **无断言判定** — 无 assert 字段时按 agent exit code 判定（0=通过，非零=失败）
10. **格式检测** — 自动检测 YAML 是单个测试用例还是测试套件（通过顶层 `tests` 键判断）

## 验证记录

| 字段 | 值 |
|------|-----|
| 验证人 | |
| 验证日期 | |
| 构建版本 | |
| 总用例数 | 50 |
| 通过数 | |
| 失败数 | |
| 备注 | |
