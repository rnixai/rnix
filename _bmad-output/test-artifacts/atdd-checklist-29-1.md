---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-04c-aggregate', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-23'
inputDocuments:
  - '_bmad-output/implementation-artifacts/29-1-dashboard-file-splitting.md'
  - '_bmad/tea/config.yaml'
  - '_bmad/tea/testarch/tea-index.csv'
  - '_bmad/tea/testarch/knowledge/data-factories.md'
  - '_bmad/tea/testarch/knowledge/test-quality.md'
  - '_bmad/tea/testarch/knowledge/test-healing-patterns.md'
  - '_bmad/tea/testarch/knowledge/test-levels-framework.md'
  - '_bmad/tea/testarch/knowledge/test-priorities-matrix.md'
---

# ATDD Checklist — Story 29.1: Dashboard 文件拆分（纯重构）

## Step 1: 预检 & 上下文加载

### 栈检测
- **detected_stack**: `backend`
- **test_stack_type**: `auto` → Go 项目（go.mod）

### 前置条件
- [x] Story 有明确验收标准（2 条 AC，Given/When/Then 格式）
- [x] 后端测试框架已配置（Go 标准 testing，`*_test.go` 文件存在）
- [x] 开发环境可用

### Story 摘要
- **类型**: 纯重构，零行为变更
- **目标**: 将 `cmd/rnix/dashboard.go`（~4108 行）拆分为 10 个模块化文件
- **约束**: 不修改逻辑、不重命名、不改签名
- **现有测试**: 7657 行（dashboard_test.go + atdd_27_* 等）

### 验收标准
1. 拆分为指定的 10 个文件结构
2. `make all` 通过，行为完全一致

### 知识碎片
- Core: data-factories, test-quality, test-healing-patterns
- Backend: test-levels-framework, test-priorities-matrix

## Step 2: 生成模式选择

- **模式**: AI 生成
- **原因**: 后端项目（detected_stack=backend）始终使用 AI 生成；验收标准清晰明确

## Step 3: 测试策略

### AC → 测试场景映射

| ID | AC | 测试场景 | 级别 | 优先级 | 红阶段失败原因 |
|----|-----|---------|------|--------|--------------|
| 29.1-UNIT-001 | AC1 | 验证 10 个目标文件全部存在 | Unit | P0 | 新文件尚未创建 |
| 29.1-UNIT-002 | AC1 | 验证 dashboard.go 行数 ≤ 700 | Unit | P1 | 当前 ~4108 行 |
| 29.1-UNIT-003 | AC1 | 验证各文件包含预期的关键函数（AST 解析） | Unit | P1 | 函数还在 dashboard.go 中 |
| 29.1-INT-001 | AC2 | go build 编译通过 | Integration | P0 | 由 make all 覆盖 |
| 29.1-INT-002 | AC2 | go vet 通过 | Integration | P1 | 由 make all 覆盖 |
| 29.1-INT-003 | AC2 | 所有现有测试通过（回归保护） | Integration | P0 | 由 make test 覆盖 |

### 测试级别
- **Unit**: 文件结构验证、源码行数检查、AST 函数分布
- **Integration**: 编译/lint/vet/测试（make all 覆盖）
- **No E2E**: 纯后端重构

### 红阶段确认
- 文件存在性 → 新文件不存在 → FAIL ✓
- 行数约束 → ~4108 > 700 → FAIL ✓
- 函数分布 → 函数不在目标文件 → FAIL ✓

## Step 4: 生成失败的验收测试（TDD 红阶段）

### 执行模式
- **模式**: Sequential（后端 Go 项目，无 E2E 子代理）
- **语言**: Go（非 TypeScript，适配项目栈）

### 生成的测试文件
- `cmd/rnix/atdd_29_1_dashboard_file_splitting_test.go`

### 测试清单

| ID | 测试函数 | 优先级 | 红阶段状态 |
|----|---------|--------|-----------|
| 29.1-UNIT-001 | TestDashboardFileSplitting_AllFilesExist | P0 | SKIP ✅ |
| 29.1-UNIT-002 | TestDashboardFileSplitting_MainFileLineCount | P1 | SKIP ✅ |
| 29.1-UNIT-003 | TestDashboardFileSplitting_FunctionDistribution | P1 | SKIP ✅ |
| 29.1-UNIT-004 | TestDashboardFileSplitting_CoreFunctionsRemainInMain | P0 | SKIP ✅ |
| 29.1-UNIT-005 | TestDashboardFileSplitting_AllFilesPackageMain | P1 | SKIP ✅ |
| 29.1-UNIT-006 | TestDashboardFileSplitting_TypesFileContainsTypes | P1 | SKIP ✅ |

### TDD 红阶段验证
- ✅ 所有 6 个测试均使用 `t.Skip()` 标记
- ✅ 所有测试断言预期行为（非占位符）
- ✅ 编译通过（`go build ./cmd/rnix/...`）
- ✅ 测试运行结果：6 SKIP, 0 FAIL, PASS

### 验收标准覆盖
| AC | 测试覆盖 |
|----|---------|
| AC1（文件拆分结构） | UNIT-001, UNIT-002, UNIT-003, UNIT-004, UNIT-005, UNIT-006 |
| AC2（make all 通过） | 由 `make all` 隐式覆盖 + 编译回归保护 |

### 下一步（TDD 绿阶段）
1. 实现文件拆分
2. 移除 `t.Skip()`
3. 运行测试 → 验证 PASS
4. 运行 `make all` → 全部通过
5. 提交

## Step 5: 验证 & 完成

### 验证结果

| 检查项 | 状态 |
|--------|------|
| Story 验收标准已映射到测试 | ✅ |
| 测试文件创建在正确位置 | ✅ |
| 遵循项目 ATDD 命名规范 | ✅ |
| 所有测试使用 `t.Skip()`（红阶段） | ✅ |
| 测试断言预期行为（非占位符） | ✅ |
| 编译通过 | ✅ |
| 测试间无依赖 | ✅ |
| 无 flaky 模式 | ✅ |
| 无临时文件遗留 | ✅ |

### 完成摘要

| 指标 | 值 |
|------|-----|
| **Story ID** | 29.1 |
| **测试级别** | Unit（主）+ Integration（make all 隐式） |
| **测试文件** | `cmd/rnix/atdd_29_1_dashboard_file_splitting_test.go` |
| **测试数量** | 6 个（2 P0 + 4 P1） |
| **红阶段状态** | 全部 SKIP ✅ |
| **输出文件** | `_bmad-output/test-artifacts/atdd-checklist-29-1.md` |
| **知识碎片** | data-factories, test-quality, test-healing-patterns, test-levels-framework, test-priorities-matrix |

### 关键假设与风险

- **假设**: `dashboard_types.go` 中的类型名检查使用字符串匹配（`type paneType` 等），依赖于源码中的精确格式
- **假设**: `topLevelFuncNames` AST 解析覆盖函数和方法，但不包含类型定义；类型验证由 UNIT-006 的字符串匹配补充
- **风险**: 如果拆分后某些函数被重命名（违反 Story 约束），UNIT-003/004 会报告假阳性

### 推荐下一步

1. **`/bmad-dev-story 29-1`** — 执行 Story 实现（文件拆分）
2. 实现后移除所有 `t.Skip()`
3. 运行 `go test -v -run TestDashboardFileSplitting ./cmd/rnix/...` 验证绿阶段
4. 运行 `make all` 验证完整回归
