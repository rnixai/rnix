# Story 18.3: 数据结构与字符串插值

Status: review

## Story

As a 应用开发者,
I want 在 AgentShell 中使用数组、映射和字符串插值,
So that 我可以处理结构化数据并动态构建智能体意图。

## Acceptance Criteria

1. **Given** 脚本定义 `files = ["a.go", "b.go", "c.go"]`
   **When** 访问 `${files[0]}`
   **Then** 返回 "a.go"

2. **Given** 脚本定义 `config = {model: "sonnet", budget: 5000}`
   **When** 访问 `${config.model}`
   **Then** 返回 "sonnet"

3. **Given** 脚本包含 `spawn "分析 ${file_path} 的代码质量"`
   **When** `file_path` 变量值为 "main.go"
   **Then** 实际 intent 为 "分析 main.go 的代码质量"

4. **Given** 字符串插值中引用未定义变量
   **When** 脚本执行
   **Then** 报告错误并指出行号和未定义的变量名

5. **And** 脚本解析时间 <= 50ms（NFR38）

6. **Given** 脚本使用 `for item in $files`（files 为数组变量）
   **When** 脚本执行
   **Then** 循环遍历数组每个元素

7. **Given** 脚本调用 `l = len(files)`
   **When** files 是包含 3 个元素的数组
   **Then** l 的值为 "3"

8. **Given** 脚本执行 `append(files, "d.go")`
   **When** 之后访问 `${files[3]}`
   **Then** 返回 "d.go"

9. **Given** 脚本执行 `k = keys(config)`
   **When** config 包含 model 和 budget 两个键
   **Then** k 为包含 "model" 和 "budget" 的数组

10. **Given** 访问数组越界索引 `${files[99]}`
    **When** 脚本执行
    **Then** 报告错误并指出行号和越界的索引

11. **Given** 脚本修改数组元素 `files[0] = "z.go"`
    **When** 之后访问 `${files[0]}`
    **Then** 返回 "z.go"

12. **Given** 脚本修改映射属性 `config.model = "opus"`
    **When** 之后访问 `${config.model}`
    **Then** 返回 "opus"

## Tasks / Subtasks

### Task 1: Environment 数据结构扩展（AC: #1, #2）

- [x] 1.1 在 `shell/env.go` 的 `Environment` 结构体中新增并行存储
  ```go
  type Environment struct {
      vars   map[string]string
      arrays map[string][]string
      maps   map[string]map[string]string
  }
  ```
- [x] 1.2 新增方法：
  ```go
  func (e *Environment) SetArray(key string, arr []string)
  func (e *Environment) GetArray(key string) ([]string, bool)
  func (e *Environment) SetMap(key string, m map[string]string)
  func (e *Environment) GetMap(key string) (map[string]string, bool)
  func (e *Environment) GetValueKind(key string) string // "string", "array", "map", ""
  ```
- [x] 1.3 `SetArray` 设置时清除同名 `vars` 和 `maps` 条目（类型互斥）；`SetMap` 同理
- [x] 1.4 更新 `Set(key, value string)` 使其清除同名 `arrays` 和 `maps` 条目
- [x] 1.5 更新 `Delete(key string)` 从三个 map 中全部删除
- [x] 1.6 更新 `NewEnvironment()` 和 `NewEnvironmentFromOS()` 初始化新增 map
- [x] 1.7 `All()` 保持返回 `map[string]string`（仅字符串变量，向后兼容）

### Task 2: 字符串插值增强（AC: #1, #2, #3, #4, #10）

- [x] 2.1 增强 `Expand` 方法，在 `${...}` 解析中支持索引访问和属性访问：
  - `${VAR[N]}` — 检测 `[` 分隔，提取 varName 和 index，从 `arrays` 查找
  - `${VAR.KEY}` — 检测 `.` 分隔，提取 varName 和 key，从 `maps` 查找
  - 若 varName 不在 `arrays`/`maps` 中，回退到现有 `vars` 查找行为
  - `$VAR` 短格式不支持索引/属性访问（`$files[0]` 仍展开为 `$files` + `[0]` 字面量）
- [x] 2.2 新增 `ExpandStrict(input string) (string, error)` 方法：
  - 逻辑与 `Expand` 相同，但遇到未定义变量返回 error
  - 错误格式：`undefined variable "VAR_NAME"`
  - 数组越界：`array "VAR_NAME" index N out of range (length M)`
  - 映射缺失键：`map "VAR_NAME" has no key "KEY"`
- [x] 2.3 `Expand` 保持现有行为（未定义 → 空字符串，向后兼容）

### Task 3: AST 节点扩展（AC: #1, #2, #11, #12）

- [x] 3.1 新增 `StatementKind` 常量：
  ```go
  StmtAssignArray StatementKind = "assign-array"
  StmtAssignMap   StatementKind = "assign-map"
  StmtAssignIndex StatementKind = "assign-index"
  StmtAssignProp  StatementKind = "assign-prop"
  ```
- [x] 3.2 新增 AST 结构体：
  ```go
  type ArrayAssign struct {
      VarName string
      Items   []string // 元素列表（字面量或 $VAR 引用）
  }

  type MapAssign struct {
      VarName string
      Entries []MapEntry // 保持插入顺序
  }

  type MapEntry struct {
      Key   string
      Value string
  }

  type IndexAssign struct {
      VarName string
      Index   string // 数字字面量或 $VAR 引用
      Value   string // 字面量或 $VAR 引用
  }

  type PropAssign struct {
      VarName  string
      Property string
      Value    string // 字面量或 $VAR 引用
  }
  ```
- [x] 3.3 扩展 `Statement` 结构体新增字段：
  ```go
  ArrayAssign *ArrayAssign
  MapAssign   *MapAssign
  IndexAssign *IndexAssign
  PropAssign  *PropAssign
  ```

### Task 4: 解析器扩展 — 数据结构赋值（AC: #1, #2, #11, #12）

- [x] 4.1 在 `parseStatement` 中，在 spawn 赋值检查（步骤 2）和 fn-call 赋值检查（步骤 3）**之前**，新增数据结构赋值检测：
  ```
  解析优先级：
  1. export
  2. 数组赋值: VAR = [...]        ← 新增
  3. 映射赋值: VAR = {...}        ← 新增
  4. 索引赋值: VAR[N] = VALUE     ← 新增
  5. 属性赋值: VAR.KEY = VALUE    ← 新增
  6. spawn 赋值: VAR = spawn "..."
  7. fn-call 赋值: VAR = NAME(ARGS)
  8. fn-call: NAME(ARGS)
  9. on-error split
  10. pipeline
  11. spawn (default)
  ```
- [x] 4.2 实现 `isArrayAssignment(line string) (varName string, rest string, ok bool)`
  - 检测模式：`IDENTIFIER = [` 开头
  - 使用 `isValidVarName` 校验变量名
- [x] 4.3 实现 `parseArrayLiteral(s string) ([]string, error)`
  - 输入：`[` 到 `]` 之间的内容（含括号）
  - 元素分隔符：逗号
  - 支持双引号字符串元素（去除引号）、变量引用 `$VAR`（保留原样，执行时展开）、字面量
  - 空数组 `[]` 合法
  - 未闭合 `[` → 错误
- [x] 4.4 实现 `isMapAssignment(line string) (varName string, rest string, ok bool)`
  - 检测模式：`IDENTIFIER = {` 开头
- [x] 4.5 实现 `parseMapLiteral(s string) ([]MapEntry, error)`
  - 输入：`{` 到 `}` 之间的内容（含大括号）
  - 格式：`key: value, key2: value2`
  - key 为无引号标识符
  - value 支持引号字符串、变量引用、数字字面量
  - 空映射 `{}` 合法
  - 未闭合 `{` → 错误
  - 重复 key → 错误
- [x] 4.6 实现 `isIndexAssignment(line string) (varName, index, value string, ok bool)`
  - 检测模式：`IDENTIFIER[EXPR] = VALUE`
  - index 可以是数字字面量或 `$VAR`
  - value 可以是引号字符串、变量引用、字面量
- [x] 4.7 实现 `isPropAssignment(line string) (varName, prop, value string, ok bool)`
  - 检测模式：`IDENTIFIER.IDENTIFIER = VALUE`
  - 避免与 spawn 赋值冲突：`IDENTIFIER.IDENTIFIER` 中第一个标识符不能是保留关键字
  - 避免与条件表达式冲突：`=` 后面不能紧跟 `=`

### Task 5: 解释器执行 — 数据结构操作（AC: #1, #2, #3, #4, #10, #11, #12）

- [x] 5.1 在 `executeBlock` 中处理 `StmtAssignArray`：
  - 对每个 Items 元素调用 `env.Expand(item)` 展开变量引用
  - 调用 `env.SetArray(varName, expandedItems)`
- [x] 5.2 在 `executeBlock` 中处理 `StmtAssignMap`：
  - 对每个 Entry.Value 调用 `env.Expand(value)` 展开变量引用
  - 构建 `map[string]string`，调用 `env.SetMap(varName, m)`
- [x] 5.3 在 `executeBlock` 中处理 `StmtAssignIndex`：
  - `env.Expand(index)` 展开索引表达式
  - `strconv.Atoi` 解析为整数
  - 从 `env.GetArray(varName)` 获取数组
  - 数组不存在 → 运行时错误
  - 索引越界 → 运行时错误
  - `env.Expand(value)` 展开值
  - 就地修改数组元素，`env.SetArray(varName, updatedArr)` 回写
- [x] 5.4 在 `executeBlock` 中处理 `StmtAssignProp`：
  - 从 `env.GetMap(varName)` 获取映射
  - 映射不存在 → 运行时错误
  - `env.Expand(value)` 展开值
  - 设置属性，`env.SetMap(varName, updatedMap)` 回写
- [x] 5.5 **严格插值**：修改 `executeBlock` 中 `StmtSpawn` 的 intent 展开改用 `ExpandStrict`
  - spawn intent 使用 `env.ExpandStrict(intent)` 替代 `env.Expand(intent)`
  - 展开失败时返回带行号的错误（`fmt.Errorf("line %d: %w", stmt.Line, err)`）
  - pipeline 中各 spawn intent 同理
  - on-error handler intent 同理
  - **不影响** export value 展开（仍用 `Expand`，export 允许引用不存在的变量得到空值）
  - **不影响** condition 评估（evalCondition 不调用 Expand）

### Task 6: for-in 数组变量集成（AC: #6）

- [x] 6.1 在 `executeBlock` 的 `StmtFor` 处理中，执行前检查列表是否为单个数组变量引用：
  ```go
  case StmtFor:
      list := stmt.For.List
      // 如果列表只有一个元素且是 $VAR 引用，尝试展开为数组
      if len(list) == 1 && strings.HasPrefix(list[0], "$") {
          varName := list[0][1:]
          if arr, ok := e.env.GetArray(varName); ok {
              list = arr
          }
      }
      for _, item := range list {
          // 现有逻辑
      }
  ```
- [x] 6.2 如果 `$VAR` 不是数组类型，回退到现有行为（展开为字符串，作为单元素列表）

### Task 7: 内置函数（AC: #7, #8, #9）

- [x] 7.1 定义内置函数名集合：
  ```go
  var builtinFunctions = map[string]bool{
      "len": true, "append": true, "keys": true,
  }
  ```
- [x] 7.2 更新 `validateFnCalls`：跳过 `builtinFunctions` 中的函数名（不要求在 `script.Functions` 中定义）
  - 校验参数数量：`len` 期望 1 个参数，`append` 期望 2 个参数，`keys` 期望 1 个参数
- [x] 7.3 更新 `isFnCallExpr`：`builtinFunctions` 中的函数名也应被识别为函数调用（当前 `isReservedKeyword` 检查不阻止它们，因为 len/append/keys 不在保留关键字中）
- [x] 7.4 在 `executeBlock` 的 `StmtFnCall` 处理中，**优先检查内置函数**：
  ```go
  case StmtFnCall:
      if builtinFunctions[stmt.FnCall.Name] {
          returnVal, err := e.executeBuiltinFn(ctx, stmt.FnCall)
          if err != nil { return err }
          if stmt.Assign != "" {
              e.env.Set(stmt.Assign, returnVal) // len 返回字符串
          }
          break // 或 continue
      }
      // 现有用户函数调用逻辑...
  ```
- [x] 7.5 实现 `executeBuiltinFn(ctx context.Context, call *FnCallStmt) (string, error)`：
  - **len(collection)**：
    - 参数为变量名（裸标识符或 `$VAR`）
    - 展开参数后，查找 `env.GetArray` → 返回 `strconv.Itoa(len(arr))`
    - 不是数组则查找 `env.GetMap` → 返回 `strconv.Itoa(len(m))`
    - 都不是则查找 `env.Get` → 返回 `strconv.Itoa(len(str))`（字符串长度，按 rune 计）
    - 完全未定义 → 错误
  - **append(array, element)**：
    - 第一个参数为数组变量名（裸标识符）
    - 第二个参数为要追加的值（字面量或 `$VAR`，展开后追加）
    - 从 `env.GetArray(name)` 获取数组，追加元素，`env.SetArray(name, updated)` 回写
    - 变量不是数组 → 错误
    - **append 无返回值**（返回空字符串）
  - **keys(map)**：
    - 参数为映射变量名（裸标识符）
    - 从 `env.GetMap(name)` 获取映射
    - 将所有 key 收集为排序后的 `[]string`，`env.SetArray(stmt.Assign, keys)` 直接设为数组
    - 如果无 Assign → 只返回空字符串（keys 结果无处存放）
    - 变量不是映射 → 错误
    - **特殊处理**：keys 的返回值需设为数组而非字符串。在 executeBlock 的 StmtFnCall 内置处理中，为 keys 特殊处理 Assign — 调用 `env.SetArray` 而非 `env.Set`

### Task 8: 复杂度统计扩展

- [x] 8.1 在 `countStagesInBlock` 中处理新 StmtKind：
  - `StmtArrayLit`：0（纯赋值，无 spawn）
  - `StmtMapLit`：0
  - `StmtAssignIndex`：0
  - `StmtAssignProp`：0

### Task 9: 测试（AC: #1-#12）

- [x] 9.1 `shell/env_test.go` — Environment 扩展测试
  - SetArray / GetArray 基本操作
  - SetMap / GetMap 基本操作
  - 类型互斥：Set 覆盖同名 array/map、SetArray 覆盖同名 string/map
  - Delete 从三个 map 中全部删除
  - GetValueKind 返回正确类型
  - Expand `${arr[0]}`、`${map.key}` 正确展开
  - Expand 越界索引回退到空字符串（非 strict 模式）
  - ExpandStrict 未定义变量返回 error
  - ExpandStrict 数组越界返回 error
  - ExpandStrict 映射缺失键返回 error
  - ExpandStrict 正常情况返回无 error

- [x] 9.2 `shell/data_test.go` — ParseScript 解析测试
  - 数组赋值 `files = ["a.go", "b.go"]` 解析
  - 数组赋值空数组 `empty = []` 解析
  - 映射赋值 `config = {model: "sonnet", budget: 5000}` 解析
  - 映射赋值空映射 `empty = {}` 解析
  - 映射赋值重复 key → 错误
  - 索引赋值 `files[0] = "new.go"` 解析
  - 属性赋值 `config.model = "opus"` 解析
  - 未闭合 `[` → 错误
  - 未闭合 `{` → 错误
  - 内置函数调用 `len(files)` / `append(files, "x")` / `keys(config)` 解析
  - 内置函数参数数量不匹配 → 错误

- [x] 9.3 `shell/data_test.go` — 执行器测试
  - 数组赋值 + 索引访问 `${files[0]}` 在 spawn intent 中展开
  - 映射赋值 + 属性访问 `${config.model}` 在 spawn intent 中展开
  - 字符串插值 `spawn "分析 ${file_path}"` 正确展开（AC3）
  - ExpandStrict 未定义变量 → spawn 报错含行号（AC4）
  - 数组越界 → spawn 报错含行号（AC10）
  - 索引赋值修改元素 + 后续访问正确（AC11）
  - 属性赋值修改属性 + 后续访问正确（AC12）
  - for-in 数组变量 `for item in $files` 遍历数组元素（AC6）
  - len(array) 返回 "3"（AC7）
  - len(map) 返回正确数量
  - len(string) 返回字符数（rune 计）
  - append 追加元素 + 后续索引访问正确（AC8）
  - keys(map) 结果为排序后的数组（AC9）
  - append/keys 变量类型不匹配 → 错误
  - 数据结构与函数调用组合：函数参数传递数组元素
  - 数据结构与 for/while 组合：for 循环内修改数组元素
  - export 值中使用数组/映射插值

- [x] 9.4 竞态测试：`go test -race ./shell/...`

## Dev Notes

### 关键架构约束

- **解析器架构**：手写递归下降解析器（Decision 10），不使用 parser generator
- **执行模型**：AST walker 解释执行，瓶颈在 LLM 调用（秒级），解释器开销 ≤ 1ms/次（NFR39）
- **FR99**：数组和映射数据结构，支持遍历、索引、长度、追加
- **FR104**：字符串插值，在 intent 和参数中引用变量值
- **NFR38**：脚本解析时间 ≤ 50ms（≤ 1000 行脚本）

### Environment 存储设计决策

**选择并行 map 方案**（而非重构为 `map[string]interface{}`）：

```go
type Environment struct {
    vars   map[string]string            // 字符串变量（现有）
    arrays map[string][]string          // 数组变量（新增）
    maps   map[string]map[string]string // 映射变量（新增）
}
```

**理由**：
- `Set`/`Get`/`Expand`/`Delete`/`All` 签名不变，所有现有调用点零改动
- 类型互斥通过 set 方法内部清除其他类型保证
- `Expand` 内部新增索引/属性查找分支，不影响简单 `$VAR` 路径
- 序列化/调试时三个 map 各自独立，无 type assertion

**类型互斥规则**：同一 key 在任意时刻只存在于一个 map 中。`SetArray("x", ...)` 会 `delete(vars, "x")` + `delete(maps, "x")`。

### 字符串插值增强设计

**`${VAR[N]}` 解析逻辑**（在 Expand 的 `${...}` 分支内）：

```
${VAR[N]} 解析:
1. 提取 ${...} 内容
2. 检查是否包含 '['
3. 是 → 分割 varName 和 index
4. varName 在 arrays 中 → 返回 arrays[varName][index]
5. varName 不在 arrays 中 → 回退到 vars[整个名称]（保持兼容）
```

**`${VAR.KEY}` 解析逻辑**：

```
${VAR.KEY} 解析:
1. 提取 ${...} 内容
2. 检查是否包含 '.'
3. 是 → 分割 varName 和 property
4. varName 在 maps 中 → 返回 maps[varName][property]
5. varName 不在 maps 中 → 回退到 vars[整个名称]（保持兼容）
```

**解析优先级**：先检查 `[`（数组索引），再检查 `.`（映射属性），最后简单变量名。这是因为 `VAR.KEY[0]` 应优先被解析为数组索引而非映射属性。

**`$VAR` 短格式**不支持索引/属性（`$files[0]` 不解析为数组访问，只展开 `$files` 为字符串后拼接 `[0]`）。只有 `${files[0]}` 才触发数组访问。

### ExpandStrict 与向后兼容

**严格模式**仅用于 spawn intent 展开。这是 AC4 的要求。

```go
// 非 strict（保持现有行为）— 用于 export value、condition 中的变量展开
result := env.Expand(value)

// strict — 用于 spawn intent（AC4: 未定义变量报错）
result, err := env.ExpandStrict(intent)
if err != nil {
    return fmt.Errorf("line %d: %w", stmt.Line, err)
}
```

**影响范围**：仅修改 `executeBlock` 中 `StmtSpawn` 和 `StmtPipeline` 的 intent 展开。不影响：
- `export VALUE` 展开（空值合法）
- 函数参数展开（函数内部可能自行处理空值）
- for 循环变量展开（`e.env.Expand(item)`）
- condition 评估（`evalCondition` 独立处理）

### 数组/映射字面量语法

**数组字面量**：

```
files = ["a.go", "b.go", "c.go"]
empty = []
mixed = ["text", $VAR, "more"]
```

解析规则：
- `[` 和 `]` 定界
- 逗号分隔元素
- 元素支持：双引号字符串（去引号）、`$VAR` 引用（保留原样，执行时展开）、无引号字面量
- 空数组合法

**映射字面量**：

```
config = {model: "sonnet", budget: 5000}
empty = {}
dynamic = {name: $VAR, count: 3}
```

解析规则：
- `{` 和 `}` 定界
- 逗号分隔条目
- 条目格式：`KEY: VALUE`
- key 为无引号标识符（`isValidIdentifier`）
- value 支持：双引号字符串（去引号）、`$VAR` 引用、数字字面量、无引号字面量
- 空映射合法
- 重复 key → 解析错误

### parseStatement 中的赋值检测顺序

**关键**：数据结构赋值检测必须在 spawn 赋值和 fn-call 赋值之前。

现有 `isAssignment` 只匹配 `VAR = spawn ...`。新增的 `isArrayAssignment` 匹配 `VAR = [...]`，`isMapAssignment` 匹配 `VAR = {...}`。

```go
func parseStatement(line string) (Statement, error) {
    // 1. export
    // 2. 数组赋值: VAR = [...]
    if varName, rest, ok := isArrayAssignment(line); ok { ... }
    // 3. 映射赋值: VAR = {...}
    if varName, rest, ok := isMapAssignment(line); ok { ... }
    // 4. 索引赋值: VAR[N] = VALUE
    if varName, idx, val, ok := isIndexAssignment(line); ok { ... }
    // 5. 属性赋值: VAR.KEY = VALUE
    if varName, prop, val, ok := isPropAssignment(line); ok { ... }
    // 6. spawn 赋值 (isAssignment - 已有)
    // 7. fn-call 赋值 (parseAssignmentFnCall - 已有)
    // ...
}
```

**索引赋值 vs spawn 赋值**不冲突：`files[0] = "new"` 不匹配 `isAssignment`（因为 `files[0]` 不是合法变量名）。

**属性赋值 vs spawn 赋值**不冲突：`config.model = "opus"` 不匹配 `isAssignment`（因为 `config.model` 不是合法变量名——含 `.`）。

### 内置函数设计

**与用户定义函数共存**：

```
fn analyze(file)
  spawn "分析 ${file}"
end

# 内置函数调用
l = len(files)
append(files, "new.go")
k = keys(config)

# 用户函数调用
analyze("main.go")
```

**解析区分**：`isFnCallExpr` 已经能识别 `NAME(ARGS)` 格式。`len`/`append`/`keys` 不在 `reservedKeywords` 中，所以 `isValidIdentifier` + `!isReservedKeyword` 检查通过。

**校验区分**：`validateFnCalls` 需要跳过 `builtinFunctions`。对内置函数做参数数量校验：
- `len` → 1 个参数
- `append` → 2 个参数
- `keys` → 1 个参数

**执行区分**：在 `StmtFnCall` 分支内，先检查 `builtinFunctions[name]`，是则调用 `executeBuiltinFn`，否则走现有用户函数路径。

**len 参数解析**：参数为变量名标识符（如 `len(files)` 中 `files` 是裸标识符，不是 `$files`）。`parseFnCallArgs` 会返回 `["files"]`。在 `executeBuiltinFn` 中直接用这个标识符查找 `env.GetArray`/`env.GetMap`/`env.Get`。

如果参数是 `$VAR` 形式（如 `len($name)`），展开后得到字符串值，再按字符串长度返回。

**keys 特殊处理**：`keys(config)` 返回值应该是数组。在 executeBuiltinFn 中，`keys` 函数计算结果后，直接对 `stmt.Assign` 调用 `env.SetArray`（而非 `env.Set`）。这需要在 `StmtFnCall` 的内置分支中特殊处理 keys 的赋值行为。

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| 数组赋值 | for-in 循环 | `for item in $arr` 展开数组元素 | 是 |
| 数组赋值 | fn 参数 | 函数参数传递 `${arr[0]}`（展开为字符串值） | 是 |
| 数组赋值 | spawn intent | `${arr[0]}` 在 intent 中展开 | 是 |
| 映射赋值 | spawn intent | `${map.key}` 在 intent 中展开 | 是 |
| 映射赋值 | fn 参数 | 函数参数传递 `${map.key}` | 是 |
| 索引赋值 | for 循环 | for 循环内修改数组元素 | 是 |
| 属性赋值 | if 条件 | 修改属性后条件判断（通过 export 中间变量） | 是 |
| ExpandStrict | StmtSpawn | 未定义变量 → 错误含行号 | 是 |
| ExpandStrict | StmtPipeline | pipeline 各段 intent 使用 strict 模式 | 是 |
| ExpandStrict | on-error | on-error handler intent 使用 strict 模式 | 是 |
| ExpandStrict | export | export 值展开**不**使用 strict（空值合法） | 是 |
| len 内置函数 | while 条件 | `l = len(arr)` 后 `while $l != 0` | 是 |
| append 内置函数 | for 循环 | 循环内 append 元素到数组 | 是 |
| keys 内置函数 | for-in | `k = keys(m)` 后 `for key in $k` | 是 |
| 数组/映射赋值 | fn return | 函数内可操作数组/映射，修改对外部可见（共享 env） | 是 |

### 不支持的特性（本 Story 范围外）

- **嵌套数据结构**：`[[1,2],[3,4]]` 或 `{a: {b: 1}}` — 不支持
- **数组/映射作为函数参数**：函数参数传递仍为字符串值
- **数组/映射作为函数返回值**：return 值仍为字符串
- **切片操作**：`arr[1:3]` — 不支持
- **数组/映射比较**：`if $arr1 == $arr2` — 不支持
- **数组/映射在条件表达式中**：`if $arr[0] == "x"` — 不直接支持（需通过中间变量：`v = ${arr[0]}; if $v == "x"`）
- **映射中带空格的 key**：`{my key: value}` — 不支持（key 必须是合法标识符）

### 现有代码模式（必须遵循）

**解析模式** — 参考 `parseForBlock` / `parseBuiltinStatement`：
- 关键字匹配大小写不敏感（`strings.EqualFold`）
- 行号追踪用于错误报告（`lineIdx` 参数）
- 块级语句以 `end` 关键字结束

**执行模式** — 参考 `executeBlock` 中各 `stmt.Kind` 分支：
- switch on `stmt.Kind` 分发
- 每步检查 `ctx.Err()` 支持取消
- 失败时返回 error 并停止执行

**测试模式** — 参考 `script_test.go`：
- mock `KernelSpawner` 实现 `SpawnAndWait` + `Wait`
- `mockSpawner` 记录 `calls` 和预设 `results`
- 验证调用次数、参数、执行顺序
- 测试函数命名 `TestParseScript_*` / `TestScriptExecutor_*`
- Story ID 注释格式：`// 18.3-UNIT-001: ...`

**错误传播模式** — 参考 `ErrScriptExit` / `ErrFnReturn`：
- `ErrFnReturn` 在函数调用处捕获
- `ErrScriptExit` 在 Execute 顶层捕获
- 数据结构操作的错误为普通 error，直接向上传播

### 保留关键字表

已注册（18.1 + 18.2 已实现）：
`for`, `in`, `while`, `if`, `else`, `end`, `fn`, `return`, `parallel`, `source`, `wait`, `sleep`, `exit`, `export`, `spawn`

`len`, `append`, `keys` **不在**保留关键字中（它们是内置函数，不是关键字）。用户可以定义 `len` 变量或 `len` 函数名（但不建议）。如果用户定义了同名函数，内置函数优先。

### Project Structure Notes

所有改动限制在 `shell/` 包内：
- `shell/env.go` — Environment 数据结构扩展 + ExpandStrict
- `shell/env_test.go` — 新增测试
- `shell/script.go` — AST 类型扩展 + 解析器扩展 + 执行器扩展
- `shell/script_test.go` — 新增测试

不涉及 `kernel/`、`vfs/`、`drivers/`、`cmd/`、`ipc/` 等其他包。无需修改 `KernelSpawner` 接口。

### 与 Story 18.1/18.2 的关系

- Story 18.1 修改了 `shell/pipe.go`（KernelSpawner 接口新增 Wait）和 `ipc/server.go`
- Story 18.2 新增了函数定义/调用/return + `validateFnCalls` + `ErrFnReturn`
- 本 Story **不修改** `shell/pipe.go` 或 `ipc/server.go`
- 本 Story 扩展 `validateFnCalls` 以支持内置函数参数校验
- 本 Story 修改 `executeBlock` 的 `StmtFnCall` 分支以优先处理内置函数

### Git Intelligence

最近提交（Story 18.2 完成）：
- `e829c66` refactor: update traceability report for Story 18.2
- `812fc61` feat: cr 18-2
- `6e3362b` feat: ds 18-2
- `dd35fc7` feat: atdd 18-2
- `d4e0f51` feat: cs 18-2

Story 18.2 的 code review 修复了 3 个问题：
1. [HIGH] validateFnCalls 错误消息缺少行号 → 修复：Statement 新增 Line 字段
2. [MEDIUM] parseFnDef 参数名缺少标识符校验 → 修复：添加 isValidIdentifier 检查
3. [LOW] isFnCallExpr 冗余检查 → 已移除

本 Story 应确保：
- 所有新增解析错误包含行号
- 新增赋值检测函数做完整的标识符/语法校验
- 无冗余检查

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-18-agentshell完整脚本语言-agentshell-complete-scripting.md#Story 18.3]
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 10: AgentShell 解析器架构]
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#AgentShell 语法模式]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR99, FR104]
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR38, NFR39]
- [Source: _bmad-output/project-context.md]
- [Source: _bmad-output/implementation-artifacts/18-1-loop-structures-and-builtin-commands.md]
- [Source: _bmad-output/implementation-artifacts/18-2-function-definition-and-invocation.md]
- [Source: shell/script.go — ParseScript/parseBlock/parseStatement/executeBlock 实现]
- [Source: shell/env.go — Environment 变量管理]

## Dev Agent Record

### Agent Model Used

claude-4.6-opus

### Debug Log References

无需调试日志——所有 ATDD 测试和新增测试一次通过。

### Completion Notes List

- ✅ Task 1: Environment 结构体扩展为三并行 map（vars/arrays/maps），类型互斥规则通过 Set/SetArray/SetMap 内部互删实现
- ✅ Task 2: Expand 重构为 `expand(input, strict)` 统一内核，通过 `resolveExpr` 处理 `${VAR[N]}` 和 `${VAR.KEY}` 语法；ExpandStrict 用于 spawn intent 展开
- ✅ Task 3: 新增 StmtArrayLit/StmtMapLit/StmtAssignIndex/StmtAssignProp 四个 StatementKind 及对应 AST 结构体
- ✅ Task 4: parseStatement 优先级更新——数组/映射/索引/属性赋值在 spawn 赋值和 fn-call 赋值之前检测；新增 isArrayAssignment/parseArrayLiteral/isMapAssignment/parseMapLiteral/isIndexAssignment/isPropAssignment 六个解析函数
- ✅ Task 5: executeBlock 新增四个 case 分支处理数据结构操作；spawn/pipeline intent 展开改用 ExpandStrict，on-error handler 同理；export 保持 Expand（非 strict）
- ✅ Task 6: for-in 循环支持 `$VAR` 数组变量引用——单元素 `$VAR` 列表时尝试 GetArray 展开，非数组回退到字符串
- ✅ Task 7: builtinFunctions map 定义 len/append/keys 及参数数量；validateFnCalls 跳过内置函数校验；executeBuiltinFn 实现三个内置函数；keys 特殊处理——结果通过 SetArray 存为数组
- ✅ Task 8: countStagesInBlock 新增 StmtArrayLit/StmtMapLit/StmtAssignIndex/StmtAssignProp 均计为 0
- ✅ Task 9: data_test.go 从 27 个 ATDD 测试扩展至 51 个测试，覆盖类型互斥、空集合、重复 key、索引/属性赋值、内置函数、参数校验、越界错误、非类型匹配错误、for-in 回退、变量引用元素等场景
- ✅ parseCondition 增强支持 `${VAR.KEY}` 语法——braced 表达式在 evalCondition 中通过 env.Expand 展开
- ✅ evalCondition 增强——先检查 `${...}` braced 表达式，再检查 spawn 捕获属性，最后检查 map 属性访问
- ✅ 新增 expandPipelineIntentsStrict 用于 pipeline intent 的严格展开
- ✅ 新增 LenOf 方法支持 len 内置函数的统一长度查询
- ✅ 所有测试通过 -race 竞态检测
- ✅ Lint 检查通过（修复了 2 个 S1017 strings.TrimPrefix 建议）

### File List

- `shell/env.go` — Environment 重构：三并行 map + SetArray/GetArray/SetMap/GetMap/GetValueKind/ExpandStrict/LenOf
- `shell/script.go` — AST 扩展 + 解析器扩展 + 执行器扩展 + builtinFunctions + expandPipelineIntentsStrict + sortStrings
- `shell/data_test.go` — 51 个测试（27 ATDD + 24 新增），覆盖 AC1-AC12 + 组合矩阵
