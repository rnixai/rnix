# Story 26.3: Specialize 能力迁移

Status: review

## Story

As a 平台构建者,
I want 在统一推理循环中保留 Stem Cell 的动态 Skill 加载能力（specialize action），
So that 智能体在执行过程中可以按需加载新能力，保持渐进式特化和分化记忆功能。

**FRs:** FR120, FR121

## Previous Story Context

### Story 26-1（已完成）
- 删除了 `kernel/ooda.go`（531 行），包括 `oodaActSpecialize` 函数
- 删除了所有 OODA 类型、测试文件、demo agent
- 统一 Spawn 入口为 `k.reasonStep(proc, llmFD, opts)`

### Story 26-2（已完成）
- ActionType 扩展到 7 种：text, tool_call, plan, spawn, complete, replan, specialize
- `parseAction` 用 `json.RawMessage` 重写，已能正确解析 `ActionSpecialize` 并填充 `ToolPath`（skill name）
- `reasonStep` switch 扩展了 Plan, Spawn, Complete, Replan 的完整实现
- **Specialize 是 STUB 占位**——当前代码仅返回 "specialize action not yet implemented" 作为 tool message
- `AgentManifest.Reasoning string` → `Planning *bool`
- `linearToolProtocol` → `toolProtocol`，新增 `planProtocol`

### 当前 Specialize Stub（kernel/kernel.go 第 1596-1604 行）

```go
case ActionSpecialize:
    errMsg := "specialize action not yet implemented"
    _ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", errMsg)
    k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
    k.emitEvent(proc, "ReasonStep", map[string]any{
        "step":   step,
        "action": "specialize_stub",
    }, nil, nil, time.Since(stepStart))
    continue
```

### 已确认无需修改的文件
- `cmd/rnix/lineage.go` — 已使用 `"specialize"` 而非 `"ooda-specialize"`
- `kernel/stem_integration_test.go` — 无 OODA 引用
- `kernel/diffmemory_integration_test.go` — 无 OODA 引用
- `kernel/lineage_integration_test.go` — 无 OODA 引用
- `kernel/diffmemory_test.go` — 无 OODA 引用

## Acceptance Criteria (AC)

### AC-1: Specialize 完整实现——Skill 加载
**Given** `reasonStep` 中 LLM 返回 `ActionSpecialize`（JSON: `{"action": "specialize", "tool": "skill-name", "data": {}}`）
**When** 解析 specialize action
**Then** 从 `action.ToolPath` 获取 skill 名称
**And** 检查 `k.skillLoader` 是否存在，不存在则返回错误 tool message
**And** 调用 `k.skillLoader(skillName)` 加载 skill
**And** 成功后追加 `proc.Skills` 和 `proc.AllowedDevices`
**And** 通过 `k.ctxMgr.AppendMessage` 以 `RoleUser` 注入 skill body（格式：`[Dynamic Skill Loaded: name]\n{body}`）
**And** 以 tool message 返回 `skill "name" loaded successfully`
**And** 产生 `StemSpecialize` 事件（包含 `skill` 和 `total_skills`）

### AC-2: TOCTOU 双重检查防护
**Given** specialize 请求 skill name
**When** 第一次加锁检查 `slices.Contains(proc.Skills, skillName)`
**Then** 如果已加载，返回 `skill "name" already loaded` 作为 tool message，不重复加载
**And** 解锁后执行 `k.skillLoader(skillName)`（I/O 操作，在锁外）
**And** 加载完成后重新加锁，再次 `slices.Contains` 检查
**And** 如果此时已被并发加载，返回 `skill "name" already loaded`
**And** 通过 `go test -race` 无数据竞争

### AC-3: Progressive Lineage 记录
**Given** specialize 成功加载 skill 且进程有 lineage（`proc.lineage != nil`）
**When** 加载完成后
**Then** 调用 `proc.lineage.Record(LineageEvent{Phase: "progressive", Skills: []string{skillName}, Trigger: reason})`
**And** `FromMemory` 为 false（渐进式特化不来自记忆）
**And** `Trigger` 来自 LLM 的 action content 或 "specialize" fallback

### AC-4: DiffMemory 更新
**Given** specialize 成功加载 skill 且 `k.diffMemory != nil`
**When** 加载完成后
**Then** 调用 `k.diffMemory.Record(proc.Intent, allSkills)`，其中 `allSkills` 是 proc 当前所有 Skills 的快照
**And** DiffMemory 记录包含进程的完整 skill 路径（初始 + 渐进加载的所有 skill）

### AC-5: 不存在的 Skill 错误处理
**Given** specialize 指定不存在的 skill
**When** `k.skillLoader` 返回错误
**Then** 错误信息以 tool message 注入上下文：`specialize error: skill "name" load failed: ...`
**And** 不导致进程崩溃，LLM 在下一步可感知错误并调整策略
**And** 此错误**不计入**熔断计数器（specialize 失败是可恢复的逻辑错误）

### AC-6: Empty Skill Name 错误处理
**Given** specialize 的 skill name 为空字符串
**When** 检查 `action.ToolPath`
**Then** 返回 `specialize error: empty skill name` 作为 tool message
**And** 不调用 skillLoader

### AC-7: AppendMessage 失败容错
**Given** `k.ctxMgr.AppendMessage` 调用失败（上下文已释放或异常）
**When** 注入 skill body 失败
**Then** 输出警告日志（`k.emitLog`）
**And** 继续循环不终止进程
**And** 不 crash

### AC-8: 并发 Specialize 线程安全
**Given** 多个 goroutine 并发触发 specialize
**When** 同时读写 `proc.Skills`、`proc.AllowedDevices`、DiffMemory、Lineage
**Then** 通过 `proc.mu`（Skills/AllowedDevices）和各自的 `sync.RWMutex`（DiffMemory/Lineage）保证线程安全
**And** 通过 `go test -race` 无数据竞争

### AC-9: 编译和测试通过
**Given** 所有修改完成
**When** 运行 `make all`
**Then** lint + vet + test + build 全部通过
**And** 所有 Go 包测试通过（`-race` 检测）

## Tasks / Subtasks

### Task 1: 替换 Specialize Stub 为完整实现 [AC-1, AC-2, AC-3, AC-4, AC-5, AC-6, AC-7] [x]

修改 `kernel/kernel.go` 第 1596-1604 行。

**首先**，在文件顶部 import 块中添加 `"slices"`：

```go
import (
    gocontext "context"
    "encoding/json"
    "errors"
    "fmt"
    "log"
    "path"
    "slices"     // <-- 新增
    "strings"
    "sync"
    "sync/atomic"
    "time"
    // ... 其余不变
)
```

**然后**，替换 `case ActionSpecialize:` 的全部内容（第 1596-1604 行）：

**当前代码：**
```go
case ActionSpecialize:
    errMsg := "specialize action not yet implemented"
    _ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", errMsg)
    k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
    k.emitEvent(proc, "ReasonStep", map[string]any{
        "step":   step,
        "action": "specialize_stub",
    }, nil, nil, time.Since(stepStart))
    continue
```

**替换为：**
```go
case ActionSpecialize:
    skillName := action.ToolPath
    if skillName == "" {
        errMsg := "specialize error: empty skill name"
        _ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", errMsg)
        k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
        k.emitEvent(proc, "ReasonStep", map[string]any{
            "step":   step,
            "action": "specialize_error",
        }, nil, nil, time.Since(stepStart))
        continue
    }
    if k.skillLoader == nil {
        errMsg := "specialize error: no skill loader configured"
        _ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", errMsg)
        k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
        k.emitEvent(proc, "ReasonStep", map[string]any{
            "step":   step,
            "action": "specialize_error",
        }, nil, nil, time.Since(stepStart))
        continue
    }

    // TOCTOU first check: is this skill already loaded?
    proc.mu.Lock()
    alreadyLoaded := slices.Contains(proc.Skills, skillName)
    proc.mu.Unlock()
    if alreadyLoaded {
        resultMsg := fmt.Sprintf("skill %q already loaded", skillName)
        _ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", resultMsg)
        k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
        k.emitEvent(proc, "ReasonStep", map[string]any{
            "step":   step,
            "action": "specialize_already_loaded",
            "skill":  skillName,
        }, nil, nil, time.Since(stepStart))
        continue
    }

    // Load skill outside lock (I/O may be slow)
    skillInfo, loadErr := k.skillLoader(skillName)
    if loadErr != nil {
        errMsg := fmt.Sprintf("specialize error: skill %q load failed: %v", skillName, loadErr)
        _ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", errMsg)
        k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
        k.emitEvent(proc, "ReasonStep", map[string]any{
            "step":   step,
            "action": "specialize_error",
            "skill":  skillName,
        }, nil, loadErr, time.Since(stepStart))
        continue
    }

    // TOCTOU second check under lock to prevent concurrent duplicate
    proc.mu.Lock()
    if slices.Contains(proc.Skills, skillName) {
        proc.mu.Unlock()
        resultMsg := fmt.Sprintf("skill %q already loaded", skillName)
        _ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", resultMsg)
        k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
        k.emitEvent(proc, "ReasonStep", map[string]any{
            "step":   step,
            "action": "specialize_already_loaded",
            "skill":  skillName,
        }, nil, nil, time.Since(stepStart))
        continue
    }
    proc.Skills = append(proc.Skills, skillName)
    proc.AllowedDevices = append(proc.AllowedDevices, skillInfo.Manifest.AllowedTools()...)
    totalSkills := len(proc.Skills)
    allSkills := make([]string, totalSkills)
    copy(allSkills, proc.Skills)
    proc.mu.Unlock()

    // Inject skill body into context as RoleUser
    if skillInfo.Body != "" {
        appendStart := time.Now()
        if appendErr := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser,
            fmt.Sprintf("[Dynamic Skill Loaded: %s]\n%s", skillName, skillInfo.Body)); appendErr != nil {
            k.emitLog(proc, step, types.LogTool, fmt.Sprintf(
                "specialize warning: failed to inject skill body for %q: %v", skillName, appendErr), "specialize")
            k.emitEvent(proc, "CtxWrite", map[string]any{
                "cid":  proc.CtxID,
                "op":   "AppendMessage",
                "role": string(rnixctx.RoleUser),
            }, nil, appendErr, time.Since(appendStart))
        }
    }

    // Emit specialize event
    k.emitEvent(proc, "StemSpecialize", map[string]any{
        "skill":        skillName,
        "total_skills": totalSkills,
    }, nil, nil, 0)

    // Update differentiation memory
    if k.diffMemory != nil {
        k.diffMemory.Record(proc.Intent, allSkills)
    }

    // Record progressive specialization lineage
    if proc.lineage != nil {
        trigger := action.Content
        if trigger == "" {
            trigger = "specialize"
        }
        proc.lineage.Record(LineageEvent{
            Timestamp: time.Now(),
            Phase:     "progressive",
            Skills:    []string{skillName},
            Trigger:   trigger,
        })
    }

    // Return success as tool message
    resultMsg := fmt.Sprintf("skill %q loaded successfully", skillName)
    _ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", resultMsg)
    k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
    k.emitEvent(proc, "ReasonStep", map[string]any{
        "step":   step,
        "action": "specialize",
        "skill":  skillName,
    }, nil, nil, time.Since(stepStart))
    continue
```

### Task 2: 编译验证 [AC-9] [x]

```bash
go build ./cmd/rnix/
go vet ./...
go test -race -count=1 ./kernel/... ./agents/...
make all
```

## Dev Notes

### 从 oodaActSpecialize 迁移的关键差异

原 `oodaActSpecialize`（git show `7744f07^:kernel/ooda.go` 第 461-531 行）是一个独立函数，返回 `string` 结果，由 caller（`oodaAct`）统一写入 context。新实现为 `reasonStep` switch 内的 case 分支，直接使用 `AppendToolResult` 写入。

| 对比项 | 原 oodaActSpecialize | 新 reasonStep case |
|--------|---------------------|-------------------|
| 返回方式 | 返回 `string`，caller 写入 context | 直接 `AppendToolResult` 写入 |
| skill name 来源 | `decision.Target` | `action.ToolPath`（parseAction 已映射 `tool` 字段） |
| 日志类型 | `types.LogOODA` | `types.LogTool`（LogOODA 已在 26-1 删除） |
| 事件名 | `"StemSpecialize"`（保持不变） | `"StemSpecialize"` + `"ReasonStep"` 双事件 |
| lineage trigger | `decision.Reason` | `action.Content`（LLM 的原始响应文本） |
| context role | `rnixctx.RoleUser`（skill body 注入） | `rnixctx.RoleUser`（保持不变） |
| TOCTOU | `proc.mu` Lock-check-unlock-load-lock-check | 保持不变 |

### 需要新增的 import

`kernel/kernel.go` 需要新增 `"slices"` 导入。当前 `kernel.go` 不引用 `slices`（该包仅在 `process.go`、`breakpoint.go` 等文件中使用）。

### Specialize 不计入熔断

Story 26-2 中尚未实现 `consecutiveToolErrors` 熔断（那是 Epic Story 26.3/Sprint Story 26-3 bug 修复的内容，在本 Sprint 中尚未实现）。因此本 Story 无需处理熔断逻辑。如果后续 Story 引入熔断，specialize 错误应被排除在计数之外。

### 关键 API 签名

```go
// skillLoader — 已注入到 KernelImpl
k.skillLoader func(string) (*skills.SkillInfo, error)

// skills.SkillInfo
type SkillInfo struct {
    Manifest SkillManifest
    Body     string
}

// skills.SkillManifest
type SkillManifest struct {
    Name            string            `yaml:"name"`
    AllowedToolsRaw string            `yaml:"allowed-tools"`
    // ...
}
func (m *SkillManifest) AllowedTools() []string  // 空格分隔解析

// DiffMemory
k.diffMemory.Record(intent string, skills []string)

// Lineage
proc.lineage.Record(LineageEvent{
    Timestamp, Phase, Skills, Trigger, FromMemory
})

// Context
k.ctxMgr.AppendMessage(ctxID types.CtxID, role rnixctx.Role, content string) error
k.ctxMgr.AppendToolResult(ctxID types.CtxID, toolName string, result string) error
```

### Process 字段线程安全

| 字段 | 保护机制 | 说明 |
|------|----------|------|
| `proc.Skills` | `proc.mu` | 读写都需加锁 |
| `proc.AllowedDevices` | `proc.mu` | 读写都需加锁 |
| `proc.lineage` | 自身 `sync.RWMutex` | Lineage.Record 内部加锁 |
| `k.diffMemory` | 自身 `sync.RWMutex` | DiffMemory.Record 内部加锁 |
| `proc.Intent` | 不可变 | spawn 后不变，无需锁 |
| `proc.CtxID` | 不可变 | spawn 后不变 |

### Specialize 错误分类

| 错误场景 | 处理方式 | 进程影响 |
|---------|---------|---------|
| 空 skill name | tool message 返回错误 | continue |
| 无 skillLoader | tool message 返回错误 | continue |
| skill 已加载（TOCTOU） | tool message 返回提示 | continue |
| skill 不存在 | tool message 返回错误 | continue |
| AppendMessage 失败 | warning log，继续 | continue |
| AppendToolResult 失败 | 忽略（`_ = `） | continue |

所有错误路径都 `continue` 循环，不终止进程。这是可恢复逻辑错误的设计原则。

### Initial Differentiation vs Progressive Specialization 对比

| 对比项 | 初始分化（Spawn 时） | 渐进特化（Specialize action） |
|--------|-------------------|---------------------------|
| 触发时机 | Spawn 阶段，stem agent 启动时 | reasonStep 运行中，LLM 主动请求 |
| 位置 | `kernel/kernel.go` Spawn 方法 L297-363 | `kernel/kernel.go` reasonStep switch |
| Skills 来源 | `stemMatcher.Match(intent)` 或 DiffMemory | LLM 返回的 `action.ToolPath` |
| Lineage Phase | `"initial"` | `"progressive"` |
| FromMemory | 可能为 true（DiffMemory 命中） | 始终 false |
| Context 注入 | 通过 `agent.SystemPrompt()` 整体注入 | 通过 `AppendMessage(RoleUser)` 增量注入 |
| Event 名 | `"StemDifferentiate"` | `"StemSpecialize"` |

### 代码模式参考

初始分化（Spawn 方法，第 296-363 行）是最佳参考。Specialize 的锁使用模式、DiffMemory 调用、Lineage 记录与之保持一致，但 Phase 不同。

### 文件修改清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `kernel/kernel.go` | 修改 | 新增 `"slices"` import；替换 ActionSpecialize stub 为完整实现 |

### 不修改的文件（确认）

| 文件 | 原因 |
|------|------|
| `cmd/rnix/lineage.go` | 已使用 `"specialize"`，无需修改（AC-6 in Epic = ALREADY DONE） |
| `kernel/stem_integration_test.go` | 无 OODA 引用（AC-7 in Epic = ALREADY DONE） |
| `kernel/diffmemory_integration_test.go` | 无 OODA 引用（AC-7 in Epic = ALREADY DONE） |
| `kernel/lineage_integration_test.go` | 无 OODA 引用（AC-7 in Epic = ALREADY DONE） |
| `kernel/diffmemory_test.go` | 无 OODA 引用（AC-7 in Epic = ALREADY DONE） |
| `kernel/process.go` | 无需新字段 |
| `kernel/lineage.go` | 核心逻辑不变 |
| `kernel/diffmemory.go` | 核心逻辑不变 |

### 执行顺序

1. 在 `kernel/kernel.go` import 块中添加 `"slices"`
2. 替换 `case ActionSpecialize:` stub 为完整实现
3. 运行 `make all` 验证

本 Story 修改范围极小（单文件改动），风险低。核心逻辑是从已删除的 `oodaActSpecialize` 函数直接迁移，经过 Epic 20 充分验证。

## References

- Epic 定义：`_bmad-output/planning-artifacts/epics/epic-26-统一推理循环-unified-reasoning-loop.md`（Story 26.4 部分）
- 前序 Story 26-2：`_bmad-output/implementation-artifacts/26-2-planning-config-and-agent-adaptation.md`
- 前序 Story 26-1：`_bmad-output/implementation-artifacts/26-1-unified-reasonstep-and-actiontype-extension.md`
- 原 oodaActSpecialize 代码：`git show 7744f07^:kernel/ooda.go`（第 461-531 行）
- Sprint Change Proposal：`_bmad-output/planning-artifacts/sprint-change-proposal-2026-03-18.md`
- 统一推理循环提案：`_bmad-output/planning-artifacts/unified-reasoning-loop-proposal.md`

## Dev Agent Record

### Agent Model Used
claude-4.6-opus

### Debug Log References
- 修复 ATDD 测试数据竞争：`TestReasonStep_Specialize_LineageRecorded` 和 `TestReasonStep_Specialize_LineageTriggerFromContent` 在 Spawn 后设置 `proc.lineage`，与 reasonStep goroutine 竞争。引入 `gatedLLMFile` mock 阻塞首次 LLM Read，确保 lineage 设置完成后再释放。

### Completion Notes List
- Task 1: 替换 `kernel/kernel.go` ActionSpecialize stub（~8行）为完整实现（~105行）
  - AC-1: Skill 加载完整流程（skillLoader 调用 → proc.Skills/AllowedDevices 更新 → context 注入 → tool message 返回）
  - AC-2: TOCTOU 双重检查（lock→check→unlock→load→lock→check→update→unlock）
  - AC-3: Progressive lineage 记录（Phase="progressive", FromMemory=false, Trigger=action.Content）
  - AC-4: DiffMemory.Record(intent, allSkills) 调用
  - AC-5: 不存在 skill 错误以 tool message 返回，不崩溃
  - AC-6: 空 skill name 错误处理
  - AC-7: AppendMessage 失败容错（warning log + continue）
  - AC-8: 线程安全通过 `-race` 检测
- Task 2: `make all` 通过（lint 0 issues, vet 通过, 全部测试通过含 race, build 成功）
- 额外修复：ATDD 测试文件数据竞争（新增 `gatedLLMFile` mock）

### File List
- `kernel/kernel.go` — 新增 `"slices"` import；替换 ActionSpecialize stub 为完整实现
- `kernel/atdd_26_3_specialize_migration_test.go` — 新增 `gatedLLMFile` mock；修复 LineageRecorded / LineageTriggerFromContent 两个测试的数据竞争
