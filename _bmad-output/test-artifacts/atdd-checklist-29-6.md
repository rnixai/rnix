---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-23'
storyId: '29-6'
detectedStack: backend
generationMode: ai-generation
inputDocuments:
  - _bmad-output/implementation-artifacts/29-6-llm-conversation-viewer.md
  - cmd/rnix/dashboard.go
  - cmd/rnix/dashboard_nav.go
  - cmd/rnix/dashboard_types.go
  - cmd/rnix/dashboard_test.go
  - cmd/rnix/dashboard_history.go
  - cmd/rnix/dashboard_timeline.go
  - cmd/rnix/dashboard_detail.go
  - cmd/rnix/dashboard_heatmap.go
  - ipc/protocol.go
  - ipc/client.go
---

# ATDD Checklist — Story 29.6: LLM 对话查看器

**日期:** 2026-03-23
**作者:** Decker
**主要测试级别:** Unit (Go test, Bubbletea 模型行为验证)

---

## Story 概要

**As a** 平台构建者
**I want** 在 Dashboard 任意视图按 L 键打开全屏 LLM 对话查看器
**So that** 我可以查看选中进程任意步骤的完整 LLM request/response，实现深入调试

---

## 验收准则

1. **AC-1**: L 键进入 LLM 查看器 — `viewLLM` 覆盖层，加载最新步骤
2. **AC-2**: 未选中进程提示 — Status bar 显示 "No process selected"
3. **AC-3**: Request/Response 分块渲染 — REQUEST + RESPONSE 区块
4. **AC-4**: 步骤导航栏 — 底部步骤列表，当前步骤 `*` 标记
5. **AC-5**: h/l 前后翻页 — 切换步骤
6. **AC-6**: j/k 滚动 — viewport 上下滚动
7. **AC-7**: y 复制 — OSC 52 剪贴板复制
8. **AC-8**: Esc 退出 — 回到 viewDefault
9. **AC-9**: Status bar — token 统计 + 快捷键提示
10. **AC-10**: 多入口支持 — Detail/Timeline/Heatmap Enter + 历史视图 L

---

## 测试策略

| 级别 | 理由 |
|------|------|
| Unit | 后端 Go 项目，所有测试通过 Bubbletea Model.Update() + View() 验证行为 |

**检测技术栈:** backend (Go 1.26, go.mod)
**生成模式:** AI generation (无浏览器录制)
**TDD 阶段:** RED — 所有测试编译通过但 FAIL

---

## 测试矩阵

| 测试 ID | 优先级 | AC | 测试描述 | 状态 |
|---------|--------|-----|---------|------|
| 29.6-UNIT-001 | P0 | AC1 | L 键选中进程后进入 viewLLM | RED |
| 29.6-UNIT-002 | P0 | AC1 | L 键返回非 nil cmd（获取步骤数据） | RED |
| 29.6-UNIT-003 | P0 | AC2 | 未选中进程按 L 显示 "No process selected" | RED |
| 29.6-UNIT-004 | P0 | AC3 | viewLLM View 包含 "REQUEST" 区块 | RED |
| 29.6-UNIT-005 | P0 | AC3 | viewLLM View 包含 "RESPONSE" 区块 | RED |
| 29.6-UNIT-006 | P0 | AC4 | viewLLM View 包含步骤导航栏 "Steps:" | RED |
| 29.6-UNIT-007 | P0 | AC5 | viewLLM h 键返回非 nil cmd（获取上一步） | RED |
| 29.6-UNIT-008 | P0 | AC5 | viewLLM l 键返回非 nil cmd（获取下一步） | RED |
| 29.6-UNIT-009 | P0 | AC6 | viewLLM j 键滚动 viewport，不移动 treeCursor | RED |
| 29.6-UNIT-010 | P1 | AC7 | viewLLM y 键返回非 nil cmd（剪贴板复制） | RED |
| 29.6-UNIT-011 | P0 | AC8 | viewLLM Esc 恢复 viewDefault | RED |
| 29.6-UNIT-012 | P1 | AC9 | viewLLM Status bar 包含快捷键提示 | RED |
| 29.6-UNIT-013 | P0 | AC10 | 历史视图 L 键进入 viewLLM | RED |
| 29.6-UNIT-014 | P0 | AC1,3 | viewLLM 渲染全屏覆盖层内容 | RED |
| 29.6-UNIT-015 | P0 | AC6 | viewLLM k 键滚动 viewport，不移动 treeCursor | RED |
| 29.6-UNIT-016 | P1 | AC9 | viewLLM Status bar 包含 token 统计 | RED |

---

## 优先级统计

| 优先级 | 数量 |
|--------|------|
| P0 | 12 |
| P1 | 4 |
| 合计 | 16 |

---

## AC 覆盖矩阵

| AC | 测试 ID |
|-----|---------|
| AC-1 | 001, 002, 014 |
| AC-2 | 003 |
| AC-3 | 004, 005, 014 |
| AC-4 | 006 |
| AC-5 | 007, 008 |
| AC-6 | 009, 015 |
| AC-7 | 010 |
| AC-8 | 011 |
| AC-9 | 012, 016 |
| AC-10 | 013 |

---

## 测试文件

| 文件 | 说明 |
|------|------|
| `cmd/rnix/dashboard_test.go` | 16 个 Story 29.6 ATDD 测试（追加在文件末尾） |

---

## RED 阶段验证

```
$ go test -race -run "TestLLMViewer_" ./cmd/rnix/...
--- FAIL: TestLLMViewer_LKeyEntersViewer
--- FAIL: TestLLMViewer_LKeyReturnsCmd
--- FAIL: TestLLMViewer_LKeyNoProcessSelected
--- FAIL: TestLLMViewer_ViewContainsRequest
--- FAIL: TestLLMViewer_ViewContainsResponse
--- FAIL: TestLLMViewer_ViewContainsStepNav
--- FAIL: TestLLMViewer_HKeyPrevStep
--- FAIL: TestLLMViewer_LKeyNextStep
--- FAIL: TestLLMViewer_JKeyScrollsNotTree
--- FAIL: TestLLMViewer_YKeyCopy
--- FAIL: TestLLMViewer_EscExitsViewer
--- FAIL: TestLLMViewer_ViewContainsKeyHints
--- FAIL: TestLLMViewer_HistoryViewLKey
--- FAIL: TestLLMViewer_ViewFullScreenOverlay
--- FAIL: TestLLMViewer_KKeyScrollsNotTree
--- FAIL: TestLLMViewer_ViewContainsTokenStats
FAIL (16/16 tests RED)
```

**既有测试验证:** 所有非 29.6 的 dashboard 测试仍然 PASS。

---

## 测试设计决策

### 纯行为测试

所有测试通过公共接口（`m.Update()` + `m.View()`）验证行为，不引用尚未存在的内部类型（如 `llmViewerMsg`、`llmStepListMsg`）。这确保：

1. 测试文件在 RED 阶段可编译
2. 测试运行但全部 FAIL（真正的 RED）
3. 不阻塞 `make all` 中的编译步骤
4. 实现完成后测试自动变 GREEN

### RED 失败原因

| 失败类别 | 原因 | 涉及测试 |
|---------|------|---------|
| L 键 placeholder | 当前代码设置"尚未实现"消息而非进入 viewLLM | 001, 002, 003, 013 |
| View 未处理 viewLLM | renderDashboard 没有 `case viewLLM:` 分支 | 004, 005, 006, 012, 014, 016 |
| 无 Layer 2.5 拦截 | viewLLM 按键穿透到 Layer 5/6 | 007, 008, 009, 010, 011, 015 |

---

## 下一步

1. **dev-story**: 实现 Story 29.6（`dashboard_llm_viewer.go` + 修改 `dashboard_nav.go`/`dashboard.go`/`dashboard_types.go`）
2. **GREEN 验证**: `go test -race -run "TestLLMViewer_" ./cmd/rnix/...` 全部 PASS
3. **code-review**: 运行代码审查
4. **traceability**: 生成追溯矩阵
