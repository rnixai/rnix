# Story 18.4: Spawn 返回值捕获与并行执行

Status: ready-for-dev

## Story

As a 应用开发者,
I want 捕获 spawn 的返回值到变量，并使用 parallel 块并行执行多个 spawn,
So that 我可以组合智能体结果并加速并行任务。

## Acceptance Criteria

1. **Given** 脚本包含 `result = spawn "分析代码" --agent=analyst`
   **When** 智能体完成
   **Then** 智能体的最终输出绑定到 `result` 变量

2. **Given** 脚本包含 parallel 块（内含三个 spawn）
   **When** parallel 块执行
   **Then** 三个 spawn 并行启动，块结束时等待全部完成

3. **Given** parallel 块中一个 spawn 失败
   **When** 其他 spawn 仍在执行
   **Then** 不影响其他 spawn 继续执行，块结束时汇总报告所有结果

4. **And** 循环/函数调用运行时开销 <= 1ms/次（NFR39）

5. **Given** parallel 块中多个 spawn 带赋值（`r1 = spawn "A"; r2 = spawn "B"`）
   **When** parallel 块完成
   **Then** 每个变量正确绑定对应 spawn 的结果，`$r1.exitcode` / `$r2.result` 可在后续条件中使用

6. **Given** parallel 块中 spawn 带 on-error handler
   **When** 该 spawn 失败
   **Then** on-error handler 在同一并行任务中执行，结果覆盖原 spawn 的捕获值

7. **Given** parallel 块中包含 pipeline 语句
   **When** parallel 块执行
   **Then** pipeline 与其他 spawn 并行执行

8. **Given** parallel 块中 spawn intent 引用未定义变量 `${undefined}`
   **When** parallel 块执行前展开 intent
   **Then** 报告错误并指出行号和未定义的变量名

9. **Given** parallel 块内包含非 spawn/pipeline 语句（如 export、if）
   **When** 脚本解析
   **Then** 报告解析错误，指出 parallel 块内只允许 spawn 和 pipeline 语句

10. **Given** 空 parallel 块 `parallel\nend`
    **When** 脚本执行
    **Then** 块为 no-op，脚本继续执行

## Tasks / Subtasks

### Task 1: AST 扩展（AC: #2, #9）

- [ ] 1.1 新增 `StmtParallel StatementKind = "parallel"`
- [ ] 1.2 新增 `ParallelBlock` 结构体：
  ```go
  type ParallelBlock struct {
      Body []Statement
  }
  ```
- [ ] 1.3 扩展 `Statement` 结构体新增字段 `Parallel *ParallelBlock`

### Task 2: 解析器扩展 — parallel 块（AC: #2, #7, #9, #10）

- [ ] 2.1 在 `parseBlock` 中，在 `for` 检测之前，新增 `parallel` 关键字检测：
  ```go
  if lower == "parallel" {
      parallelBlock, nextIdx, err := parseParallelBlock(lines, i)
      if err != nil {
          return nil, 0, err
      }
      stmts = append(stmts, Statement{Kind: StmtParallel, Parallel: parallelBlock, Raw: trimmed, Line: i + 1})
      i = nextIdx
      continue
  }
  ```
- [ ] 2.2 实现 `parseParallelBlock(lines []string, parallelLineIdx int) (*ParallelBlock, int, error)`：
  - 调用 `parseBlock(lines, parallelLineIdx+1, true)` 解析 body
  - 检查 `nextIdx` 处是否为 `end`，否则报错 `unclosed parallel block (missing 'end')`
  - 遍历 body 语句，校验每条语句的 Kind 必须为 `StmtSpawn` 或 `StmtPipeline`
  - 其他类型 → `fmt.Errorf("line %d: only spawn and pipeline statements allowed inside parallel block, got %s", stmt.Line, stmt.Kind)`
  - 空 body 合法（空 parallel 块为 no-op）
  - 返回 `&ParallelBlock{Body: body}, nextIdx + 1, nil`

### Task 3: 校验器扩展（AC: #2）

- [ ] 3.1 在 `validateFnCalls` 中新增 `StmtParallel` 分支：
  ```go
  case StmtParallel:
      if err := validateFnCalls(stmt.Parallel.Body, functions); err != nil {
          return err
      }
  ```

### Task 4: 执行器扩展 — parallel 块执行（AC: #1, #2, #3, #5, #6, #7, #8, #10）

- [ ] 4.1 新增 `"sync"` 到 `script.go` 的 import
- [ ] 4.2 在 `executeBlock` 中新增 `case StmtParallel` 分支，实现三阶段执行：

**阶段 A — 顺序展开 intent（主 goroutine，确保线程安全）：**
```go
type parallelTask struct {
    idx              int
    stmt             Statement
    expandedIntent   string
    expandedOnError  string
    expandedPipeline *Pipeline
}
```
- 遍历 `stmt.Parallel.Body`，对每个 StmtSpawn 调用 `e.env.ExpandStrict(intent)`
- 如果 spawn 有 OnError → 也预展开 on-error intent
- 对 StmtPipeline → 调用 `expandPipelineIntentsStrict(e.env, pipeline)`
- 任何展开错误 → 立即返回带行号的 error（AC8）
- 空 body → 直接 break（no-op）

**阶段 B — 并行执行 spawn（goroutine per task）：**
```go
type parallelResult struct {
    result   string
    exitCode int
    tokens   int
    err      error
}
```
- 预分配 `results := make([]parallelResult, len(tasks))`
- 对每个 task 启动 goroutine：
  - StmtSpawn → `e.spawner.SpawnAndWait(ctx, task.expandedIntent, agent, model)`
  - 如果 exitCode != 0 且有 OnError → 在同一 goroutine 中执行 on-error spawn（AC6）
  - on-error 结果覆盖原结果（与顺序执行行为一致）
  - StmtPipeline → `NewPipelineExecutor(e.spawner).Execute(ctx, task.expandedPipeline)`
  - 将结果写入 `results[idx]`（索引访问，无需锁）
- `sync.WaitGroup` 等待全部完成

**阶段 C — 顺序收集结果（主 goroutine）：**
- 遍历 results（按声明顺序）：
  - 如果 `err != nil` → 收集错误但不立即返回（AC3 — 不影响其他）
  - `result.TotalTokens += tokens`
  - `result.LastResult = res`（最终为最后一个 spawn 的结果）
  - `result.LastExitCode = exitCode`（最终为最后一个 spawn 的 exitCode）
  - 如果 `stmt.Assign != ""` → `e.captures[assign] = &SpawnResult{...}` + `e.env.Set(assign, res)`
  - `*stageNum++`（每个 spawn 算一个 stage）
- 如果有任何 `err != nil` → 返回第一个 error
- parallel 块不因非零 exitCode 提前终止脚本（AC3 行为：容错）

### Task 5: 复杂度统计扩展（AC: #2）

- [ ] 5.1 在 `countStagesInBlock` 中新增 `StmtParallel`：
  ```go
  case StmtParallel:
      n += countStagesInBlock(stmt.Parallel.Body)
  ```
  parallel 块内每个 spawn 独立计为一个 stage（与顺序执行一致）

### Task 6: 测试（AC: #1-#10）

- [ ] 6.1 `shell/parallel_test.go` — 解析测试：
  - `TestParseScript_Parallel_BasicBlock` — 基本 parallel 块解析（3 个 spawn）
  - `TestParseScript_Parallel_WithAssignment` — 带赋值的 spawn
  - `TestParseScript_Parallel_WithOnError` — spawn 带 on-error
  - `TestParseScript_Parallel_WithPipeline` — pipeline 在 parallel 块内
  - `TestParseScript_Parallel_Empty` — 空 parallel 块（合法 no-op）
  - `TestParseScript_Error_ParallelUnclosed` — 无 end → 错误
  - `TestParseScript_Error_ParallelInvalidContent_Export` — export 在 parallel 内 → 错误
  - `TestParseScript_Error_ParallelInvalidContent_If` — if 在 parallel 内 → 错误
  - `TestParseScript_Error_ParallelInvalidContent_For` — for 在 parallel 内 → 错误
  - `TestParseScript_Error_ParallelInvalidContent_FnCall` — fn call 在 parallel 内 → 错误
  - `TestParseScript_Error_ParallelNested` — 嵌套 parallel → 错误

- [ ] 6.2 `shell/parallel_test.go` — 执行测试：
  - `TestScriptExecutor_Parallel_AllSucceed` — 3 spawn 全成功，验证所有结果和 tokens 汇总（AC2）
  - `TestScriptExecutor_Parallel_Assignment` — 赋值 spawn，验证 env 和 captures（AC1, AC5）
  - `TestScriptExecutor_Parallel_OneFails_OthersContinue` — 一个失败不影响其他（AC3）
  - `TestScriptExecutor_Parallel_OnError` — 失败 spawn 的 on-error handler 执行（AC6）
  - `TestScriptExecutor_Parallel_AllFail` — 全部失败，结果全部捕获
  - `TestScriptExecutor_Parallel_LastResult_DeclarationOrder` — LastResult 是声明顺序的最后一个
  - `TestScriptExecutor_Parallel_TokenAggregation` — 所有 tokens 汇总到 TotalTokens
  - `TestScriptExecutor_Parallel_CapturedResult_Condition` — parallel 后 `if $r.exitcode == 0` 条件（AC5）
  - `TestScriptExecutor_Parallel_IntentExpansion` — intent 中 `${var}` 正确展开
  - `TestScriptExecutor_Parallel_UndefinedVar_Error` — ExpandStrict 未定义变量 → 报错含行号（AC8）
  - `TestScriptExecutor_Parallel_Empty` — 空 parallel 块 no-op（AC10）
  - `TestScriptExecutor_Parallel_ContextCancel` — context 取消传播到所有并行任务
  - `TestScriptExecutor_Parallel_Pipeline` — pipeline 在 parallel 块中并行执行（AC7）
  - `TestScriptExecutor_Parallel_StageCount` — countStages 正确统计 parallel 内 spawn

- [ ] 6.3 `shell/parallel_test.go` — 组合测试：
  - `TestScriptExecutor_Parallel_InForLoop` — for 循环内使用 parallel 块
  - `TestScriptExecutor_Parallel_InFunction` — 函数内使用 parallel 块
  - `TestScriptExecutor_Parallel_AfterDataStructures` — parallel 前数组/映射赋值，intent 中 `${arr[0]}` 展开
  - `TestScriptExecutor_Parallel_ResultInWhileCondition` — parallel 结果用于 while 条件

- [ ] 6.4 竞态测试：`go test -race ./shell/...`

## Dev Notes

### 关键架构约束

- **解析器架构**：手写递归下降解析器（Decision 10），不使用 parser generator
- **执行模型**：AST walker 解释执行，瓶颈在 LLM 调用（秒级），解释器开销 ≤ 1ms/次（NFR39）
- **FR100**：spawn 表达式返回值捕获，将智能体输出绑定到变量
- **FR101**：并行执行块，块内所有 spawn 并行执行，块结束时等待全部完成
- **NFR38**：脚本解析时间 ≤ 50ms（≤ 1000 行脚本）
- **NFR39**：循环/函数调用运行时开销（不含 spawn/LLM） ≤ 1ms/次

### Spawn 返回值捕获 — 现有实现分析

**AC1 已被 Phase 2（Story 11.2）+ 18.x 系列实现。** 当前代码中 `result = spawn "..." --agent=analyst` 的工作流程：

1. `parseStatement` 中 `isAssignment(line)` 检测 `VAR = spawn "..."` 模式
2. 解析为 `Statement{Kind: StmtSpawn, Spawn: &cmd, Assign: varName}`
3. `executeBlock` 中 `StmtSpawn` 分支：
   - `e.spawner.SpawnAndWait(ctx, expandedIntent, agent, model)` → `(res, exitCode, tokens, err)`
   - `e.captures[varName] = &SpawnResult{ExitCode, Result, Tokens}`
   - `e.env.Set(varName, res)`
4. 后续可通过 `$result`（字符串值）、`$result.exitcode`（在 if 条件中）、`$result.result`（在 return 中）访问

**本 Story 需确保此机制在 parallel 块内同样正确工作。** 唯一区别：parallel 块内的多个赋值 spawn 并行执行，结果在块结束后顺序写入 captures 和 env。

### parallel 块语法设计

**遵循现有 `keyword ... end` 块结构模式**（与 for/while/if/fn 一致）：

```
parallel
  r1 = spawn "分析代码" --agent=analyst
  r2 = spawn "审查架构" --agent=reviewer
  spawn "生成报告"
end
```

**关键设计决策：**

1. **parallel 不接受参数**：`parallel` 单独成行（不同于 `for VAR in LIST`、`while COND`）
2. **body 限制为 spawn 和 pipeline**：禁止 export/if/for/while/fn-call/fn-def/return/array-lit/map-lit/assign-index/assign-prop/nested-parallel
3. **理由**：Environment 不是线程安全的（Story 11.1 注释："Not thread-safe — shell execution is sequential"）。只允许 spawn/pipeline 确保 goroutine 仅调用 `e.spawner.SpawnAndWait`（线程安全的 kernel 接口），不直接操作 env
4. **intent 预展开**：在启动 goroutine 前，在主 goroutine 中顺序展开所有 intent（确保 env 读取线程安全）

### 执行模型 — 三阶段设计

```
┌─────────────────────────────────────────────┐
│ 阶段 A: 顺序展开 (主 goroutine)              │
│  - ExpandStrict(intent) for each spawn      │
│  - ExpandStrict(on-error intent) if exists   │
│  - expandPipelineIntentsStrict for pipelines │
│  - 任何展开错误 → 立即返回                     │
├─────────────────────────────────────────────┤
│ 阶段 B: 并行执行 (goroutine per task)        │
│  goroutine[0]: SpawnAndWait(r1)              │
│  goroutine[1]: SpawnAndWait(r2)              │
│  goroutine[2]: SpawnAndWait(r3)              │
│  - 每个 goroutine 独立处理 on-error           │
│  - 结果写入 results[idx] (无锁)              │
│  - sync.WaitGroup.Wait() 等待全部             │
├─────────────────────────────────────────────┤
│ 阶段 C: 顺序收集 (主 goroutine)              │
│  - 按声明顺序遍历 results                     │
│  - 更新 captures, env, TotalTokens           │
│  - LastResult = 最后一个 spawn 的结果          │
│  - LastExitCode = 最后一个 spawn 的 exitCode   │
│  - parallel 块不因 exitCode!=0 终止脚本       │
└─────────────────────────────────────────────┘
```

**线程安全保证：**
- 阶段 A：单线程读 env → 安全
- 阶段 B：goroutine 仅调用 `e.spawner.SpawnAndWait`（KernelSpawner 接口要求线程安全）+ 写入 `results[idx]`（每个 goroutine 写自己的索引，无竞争）→ 安全
- 阶段 C：单线程写 env / captures → 安全

### parallel 块与 sequential spawn 的行为差异

| 行为 | 顺序 spawn | parallel 块 |
|------|-----------|------------|
| 执行方式 | 逐个 SpawnAndWait | goroutine 并行 SpawnAndWait |
| 失败时 | 无赋值的 spawn 失败 → 停止脚本 | 任何 spawn 失败 → 不影响其他，块后脚本继续 |
| on-error | spawn 失败后立即在主 goroutine 执行 | spawn 失败后在同一并行 goroutine 中执行 |
| LastResult | 最后执行的 spawn | 声明顺序最后一个 spawn |
| LastExitCode | 最后执行的 spawn（含 on-error） | 声明顺序最后一个 spawn（含 on-error） |
| intent 展开 | 执行时展开 | 全部预展开后再启动 goroutine |

**关键区别：parallel 块是容错的**——即使某个 spawn 返回非零 exitCode，脚本也不会停止。开发者通过 `$var.exitcode` 在块后检查结果并决定后续逻辑。

### parseBlock 中的检测位置

在 `parseBlock` 的关键字检测顺序中，`parallel` 应在 `for` 之前插入：

```
检测顺序（在 parseBlock 中）:
1. end / else (块结束标记)
2. fn 定义 (仅顶层)
3. return (仅块内)
4. if 块
5. parallel 块      ← 新增
6. for 块
7. while 块
8. builtin (wait/sleep/exit)
9. parseStatement (export/spawn/pipeline/etc.)
```

**理由**：`parallel` 是无参数关键字（`lower == "parallel"` 精确匹配），不会与其他关键字冲突。放在 for 之前只是逻辑分组——所有块级结构放在一起。

### on-error 在 parallel 中的行为

每个 spawn 的 on-error handler 在同一 goroutine 中执行：

```go
// 伪代码 — goroutine 内
res, exitCode, tokens, err := spawner.SpawnAndWait(ctx, intent, agent, model)
if exitCode != 0 && onError != nil {
    hRes, hExitCode, hTokens, hErr := spawner.SpawnAndWait(ctx, onErrorIntent, onErrorAgent, onErrorModel)
    // on-error 结果覆盖原始结果
    res, exitCode, tokens = hRes, hExitCode, tokens + hTokens
}
results[idx] = parallelResult{result: res, exitCode: exitCode, tokens: tokens, err: err}
```

**on-error intent 也在阶段 A 预展开**：因为 on-error intent 可能引用变量（`${var}`），展开必须在主 goroutine 中完成。

### 现有代码模式（必须遵循）

**块级解析模式** — 参考 `parseForBlock` / `parseWhileBlock`：
- 调用 `parseBlock(lines, startIdx+1, true)` 解析 body
- 检查 nextIdx 处为 `end`
- 返回 `(block, nextIdx+1, error)`

**执行模式** — 参考 `executeBlock` 中各 `stmt.Kind` 分支：
- switch on `stmt.Kind` 分发
- 每步检查 `ctx.Err()` 支持取消
- 失败时返回 error 并停止执行

**测试模式** — 参考 `data_test.go`（Story 18.3）：
- mock `KernelSpawner` 实现 `SpawnAndWait` + `Wait`
- `mockSpawner` 记录 `calls` 和预设 `results`
- 验证调用次数、参数、执行顺序
- 测试函数命名 `TestParseScript_*` / `TestScriptExecutor_*`
- Story ID 注释格式：`// 18.4-UNIT-001: ...`

**错误传播模式** — 参考 `ErrScriptExit` / `ErrFnReturn`：
- parallel 块内的 `err` 收集后在阶段 C 处理
- `SpawnAndWait` 返回的 `err`（非 exitCode）是真正的系统错误（如 context.Canceled）
- 系统错误在阶段 C 遍历时返回第一个 error

### 并行 mock 测试的注意事项

测试 parallel 执行时，`mockSpawner` 需要支持并发调用：

1. **thread-safe mock**：现有 `mockSpawner` 使用 `calls []string` 和 `results map[string]...`。并行调用时需要：
   - `calls` 改为 `sync.Mutex` 保护或使用 `atomic` 计数
   - `results` 只读（预设后不修改），天然线程安全
   - 或者：使用并发安全的 results（`sync.Map` 或预分配 slice）

2. **验证并行性**：测试可通过 sleep/timing 或 call 记录验证多个 spawn 确实并行执行
   - 方法 A：mock spawner 中加入短 sleep，验证总耗时 < 串行耗时
   - 方法 B：使用 `sync.WaitGroup` 在 mock 中确保多个 goroutine 同时活跃
   - 推荐方法 B（确定性更强，不依赖时间）

3. **推荐 mock 增强**：
   ```go
   type concurrentMockSpawner struct {
       mu      sync.Mutex
       calls   []string
       results map[string]mockResult
   }
   
   func (m *concurrentMockSpawner) SpawnAndWait(ctx context.Context, intent, agent, model string) (string, int, int, error) {
       m.mu.Lock()
       m.calls = append(m.calls, intent)
       m.mu.Unlock()
       
       if r, ok := m.results[intent]; ok {
           return r.result, r.exitCode, r.tokens, r.err
       }
       return "", 0, 0, nil
   }
   ```

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| parallel 块 | spawn 赋值 | 块内 `r = spawn "..."` 捕获结果到 captures + env | 是 |
| parallel 块 | on-error | 块内 spawn 的 on-error 在同一 goroutine 中执行 | 是 |
| parallel 块 | pipeline | 块内 pipeline 也并行执行 | 是 |
| parallel 块 | if 条件 | 块后 `if $r.exitcode == 0` 检查 parallel 结果 | 是 |
| parallel 块 | for 循环 | for 循环内使用 parallel 块 | 是 |
| parallel 块 | 函数 | 函数内使用 parallel 块 | 是 |
| parallel 块 | 数组/映射 | intent 中 `${arr[0]}` 在 parallel 前展开 | 是 |
| parallel 块 | ExpandStrict | parallel 内 spawn intent 使用 strict 展开 | 是 |
| parallel 块 | context cancel | 取消传播到所有并行 goroutine | 是 |
| parallel 块 | stage counting | 每个 spawn/pipeline 算独立 stage | 是 |
| parallel 块 | 嵌套 parallel | 禁止嵌套 parallel（解析阶段报错） | 是 |
| parallel 块 | while 条件 | parallel 结果用于 while 条件判断 | 是 |
| parallel 块 | 空块 | 空 parallel 块为 no-op | 是 |
| spawn 赋值 | parallel | 多个赋值 spawn 并行执行后各自独立捕获 | 是 |

### 不支持的特性（本 Story 范围外）

- **嵌套 parallel 块**：`parallel` 内不能嵌套 `parallel`（解析报错）
- **parallel 内非 spawn 语句**：不支持 export、if、for、while、fn-call 等
- **动态并行度控制**：不支持 `parallel max=3` 之类的并行度限制
- **parallel 内 return**：不支持在 parallel 块内 return（语义不明确）
- **parallel 结果数组**：不自动将 parallel 结果收集为数组（需手动赋值各变量）
- **失败时取消其他**：不支持 `parallel fail-fast` 模式（AC3 明确要求容错）

### 保留关键字表

已注册（18.1 + 18.2 + 18.3 已实现）：
`for`, `in`, `while`, `if`, `else`, `end`, `fn`, `return`, `parallel`, `source`, `wait`, `sleep`, `exit`, `export`, `spawn`

`parallel` **已经在** `reservedKeywords` 中预注册（Phase 3 预留），本 Story 将其从预留变为已实现。

### Project Structure Notes

所有改动限制在 `shell/` 包内：
- `shell/script.go` — AST 类型扩展 + 解析器扩展 + 执行器扩展 + `"sync"` import
- `shell/parallel_test.go` — 新增测试文件

不涉及 `kernel/`、`vfs/`、`drivers/`、`cmd/`、`ipc/` 等其他包。无需修改 `KernelSpawner` 接口。

### 与 Story 18.1/18.2/18.3 的关系

- Story 18.1 实现了 for/while 循环 + wait/sleep/exit 内置命令
- Story 18.2 实现了函数定义/调用/return + `validateFnCalls`
- Story 18.3 实现了数组/映射数据结构 + 字符串插值 + 内置函数（len/append/keys）
- 本 Story **不修改**任何现有 Statement 类型或执行路径
- 本 Story 新增 `StmtParallel` 和对应的解析/执行逻辑
- 本 Story 新增 `"sync"` import 到 script.go（首次在 shell 包中使用）
- 本 Story 新增 parallel_test.go 测试文件（隔离测试，不修改现有测试文件）

### Git Intelligence

最近提交（Story 18.3 完成）：
- `d048a15` feat: add traceability matrix for Story 18.3
- `c76d36c` feat: cr Story 18.3
- `61d6df1` feat: ds Story 18.3
- `1464034` feat: atdd 18-3
- `a2ae5b5` feat: cs 18-3

Story 18.3 的 code review 修复了 3 个问题：
1. [HIGH] `len($undefined)` 静默返回 "0" → 修复：统一使用 LenOf，未定义变量传播 error
2. [MEDIUM] `parseMapLiteral` 缺少 key 标识符校验 → 修复：添加 `isValidIdentifier` 检查
3. [MEDIUM] 死代码 `expandPipelineIntents`（非 strict 版本）→ 删除

本 Story 应确保：
- 所有新增解析错误包含行号
- parallel 块内容校验给出明确错误信息
- 并行执行使用 `sync.WaitGroup`（不是 channel-based，保持与 Go 并发惯例一致）
- mock spawner 支持并发调用（thread-safe）
- 无死代码

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-18-agentshell完整脚本语言-agentshell-complete-scripting.md#Story 18.4]
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 10: AgentShell 解析器架构]
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#AgentShell 语法模式]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR100, FR101]
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR38, NFR39]
- [Source: _bmad-output/project-context.md]
- [Source: _bmad-output/implementation-artifacts/18-3-data-structures-and-string-interpolation.md]
- [Source: shell/script.go — parseBlock/parseForBlock/parseWhileBlock/executeBlock/countStagesInBlock]
- [Source: shell/pipe.go — KernelSpawner 接口/PipelineExecutor]
- [Source: shell/env.go — Environment/ExpandStrict]

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
