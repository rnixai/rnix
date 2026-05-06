// Package main — dashboard_eval.go (Story 38-5 PR11 Step 4(c) · 第 6 个完成
// render 主体迁出的 pane · 续 alertstrip / detail / security / intent / trace
// pattern).
//
// 本文件保留 cmd/rnix 端的：
//   - IPC 调用命令（fetchReputationCmd / fetchTopologyCmd / fetchSynergyCmd）·
//     依赖 ipc.Dial · IPC 是 cmd/rnix 端职责；
//   - handleEvalKey · 依赖 dashboardModel + tea.Cmd routing；
//   - evalEnsureSubViewData · 依赖 dashboardModel.connected；
//   - renderEvalPane / renderEvalReputationView / renderEvalTopologyView /
//     renderEvalSynergyView 改为 thin wrapper（保留旧名让 ATDD 27-10 +
//     38-4 dashboard_cross_pane_test grep 契约零行为变化）；
//   - evalScoreColorStyle / evalRepAdjustScroll / evalTopoAdjustScroll /
//     evalSynAdjustScroll / evalBottomInnerH / evalTopoItemCount 全部改为 thin
//     wrapper（与 internal/dashboard/eval 等价 · 1:1 行为）。
//
// **Story 38-4 AC#6 行为契约保留**（迁出 to internal/dashboard/eval）：
//   - SCORE column 用 EvalScoreColorStyle 渲染（gradient · NaN/Inf 边界）；
//   - VS SOLO 颜色（improvement < 0 green / > 0 red）；
//   - Recommended 整行 Bold（patch P8 · 每段独立携带 SGR Bold）；
//   - ★ ASCII 标记（"★"→"*"）。
//
// 全部行为已迁出至 internal/dashboard/eval/render.go · 测试覆盖率 **100%**
// （render_test.go 含 38-4 AC#6 + patch P12 NaN/Inf 显式断言）。
package main

import (
	"github.com/charmbracelet/lipgloss"

	dashboardeval "github.com/rnixai/rnix/internal/dashboard/eval"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// =============================================================================
// Eval Pane (Story 27-10) · cmd/rnix wrapper
// =============================================================================

// evalScoreColorStyle is a thin wrapper委托至 dashboardeval.EvalScoreColorStyle.
//
// Story 38-4 AC#6 + patch P12 行为契约保留 · 所有 NaN/Inf/越界 [0,1] 统一回退
// red（详见 internal/dashboard/eval/render.go::EvalScoreColorStyle godoc）.
//
// 保留旧名（小写）满足 ATDD 27-10 / 38-4 cross-pane 测试 grep 契约。
func evalScoreColorStyle(score float64) lipgloss.Style {
	return dashboardeval.EvalScoreColorStyle(score)
}

// fetchReputationCmd — thin wrapper · Story 38-5 PR11 Step 4(b) Phase 3
//
//nolint:unused // 保留供潜在 caller / 测试 grep（current callers 已迁至 EvalModel.OnTick）
func fetchReputationCmd() tea.Cmd {
	return dashboardeval.FetchReputationCmd(ipc.SocketPath())
}

// fetchTopologyCmd — thin wrapper · Story 38-5 PR11 Step 4(b) Phase 3
//
//nolint:unused // 保留供潜在 caller / 测试 grep
func fetchTopologyCmd() tea.Cmd {
	return dashboardeval.FetchTopologyCmd(ipc.SocketPath())
}

// fetchSynergyCmd — thin wrapper · Story 38-5 PR11 Step 4(b) Phase 3
//
//nolint:unused // 保留供潜在 caller / 测试 grep
func fetchSynergyCmd() tea.Cmd {
	return dashboardeval.FetchSynergyCmd(ipc.SocketPath())
}

func (m dashboardModel) handleEvalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "!", "h", "left":
		if key == "!" {
			m.eval.SubView = 0
		} else {
			// h/left: 向左切换子视图
			m.eval.SubView = (m.eval.SubView + 2) % 3
		}
		return m, m.evalEnsureSubViewData()
	case "@":
		m.eval.SubView = 1
		return m, m.evalEnsureSubViewData()
	case "#", "l", "right":
		if key == "#" {
			m.eval.SubView = 2
		} else {
			// l/right: 向右切换子视图
			m.eval.SubView = (m.eval.SubView + 1) % 3
		}
		return m, m.evalEnsureSubViewData()
	case "down", "j":
		switch m.eval.SubView {
		case 0:
			if len(m.eval.Reputations) > 0 && m.eval.RepCursor < len(m.eval.Reputations)-1 {
				m.eval.RepCursor++
				evalRepAdjustScroll(&m)
			}
		case 1:
			totalItems := evalTopoItemCount(&m)
			if totalItems > 0 && m.eval.TopoCursor < totalItems-1 {
				m.eval.TopoCursor++
				evalTopoAdjustScroll(&m)
			}
		case 2:
			if len(m.eval.Synergies) > 0 && m.eval.SynCursor < len(m.eval.Synergies)-1 {
				m.eval.SynCursor++
				evalSynAdjustScroll(&m)
			}
		}
		return m, nil
	case "up", "k":
		switch m.eval.SubView {
		case 0:
			if m.eval.RepCursor > 0 {
				m.eval.RepCursor--
				evalRepAdjustScroll(&m)
			}
		case 1:
			if m.eval.TopoCursor > 0 {
				m.eval.TopoCursor--
				evalTopoAdjustScroll(&m)
			}
		case 2:
			if m.eval.SynCursor > 0 {
				m.eval.SynCursor--
				evalSynAdjustScroll(&m)
			}
		}
		return m, nil
	}
	return m, nil
}

// evalEnsureSubViewData 确保当前子视图的数据已加载。
//
// 依赖 dashboardModel.connected · IPC 触发是 cmd/rnix 端职责（不能迁至
// internal/dashboard/eval）。
func (m dashboardModel) evalEnsureSubViewData() tea.Cmd {
	switch m.eval.SubView {
	case 1:
		if m.eval.Topology == nil && m.connected {
			return fetchTopologyCmd()
		}
	case 2:
		if m.eval.Synergies == nil && m.connected {
			return fetchSynergyCmd()
		}
	}
	return nil
}

// evalTopoItemCount is a thin wrapper委托至 dashboardeval.TopoItemCount.
func evalTopoItemCount(m *dashboardModel) int {
	if m == nil {
		return 0
	}
	return dashboardeval.TopoItemCount(m.eval)
}

// renderEvalPane is a thin wrapper委托至 dashboardeval.Render + renderFixedPanel border 包裹.
//
// 保留旧名 + dashboardModel receiver 让 ATDD 27-10 + 38-4 cross-pane test
// grep 契约零行为变化（多处 `m.renderEvalPane(80, 30)` 直接调用）。
//
// renderEvalPane / renderEvalReputationView / renderEvalTopologyView /
// renderEvalSynergyView · ATDD 29-1 grep 契约保留（atdd_29_1_dashboard_file_splitting_test.go
// line 192-193 直接 grep 函数名）。
func (m dashboardModel) renderEvalPane(width, height int) string {
	isActive := m.activePane == paneEval

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	ctx := dashboardeval.NewRenderContextFromEnv(isActive)
	body := dashboardeval.Render(m.eval, ctx, innerW, innerH)

	return renderFixedPanel(body, width, height, borderColor)
}

// renderEvalReputationView is a thin wrapper委托至 dashboardeval.RenderReputationView.
//
// 保留 dashboardModel receiver + 旧名让 dashboard_cross_pane_test.go 直接 callsite
// （`m.renderEvalReputationView(120, 10)` 等）零行为变化。
func (m dashboardModel) renderEvalReputationView(width, height int) string {
	ctx := dashboardeval.NewRenderContextFromEnv(false)
	return dashboardeval.RenderReputationView(m.eval, ctx, width, height)
}

// renderEvalTopologyView is a thin wrapper委托至 dashboardeval.RenderTopologyView.
//
// **保留契约**：ATDD 29-1 dashboard_file_splitting_test 行 193 grep 函数名
// `renderEvalTopologyView` 验证 dashboard_eval.go 拆分契约 · 删除该函数会破坏
// 文件拆分契约测试 · 因此使用 nolint:unused（与 trace.traceBottomInnerH /
// heatmap.segmentKindLabel / intent.intentStateIcon 同 pattern）.
//
//nolint:unused // ATDD 29-1 grep 契约保留 (dashboard_eval.go 文件拆分验证)
func (m dashboardModel) renderEvalTopologyView(width, height int) string {
	ctx := dashboardeval.NewRenderContextFromEnv(false)
	return dashboardeval.RenderTopologyView(m.eval, ctx, width, height)
}

// renderEvalSynergyView is a thin wrapper委托至 dashboardeval.RenderSynergyView.
//
// 保留 dashboardModel receiver + 旧名让 dashboard_cross_pane_test.go 直接 callsite
// （`m.renderEvalSynergyView(80, 6)` 等 · 38-4 AC#6 测试 8 处）零行为变化。
func (m dashboardModel) renderEvalSynergyView(width, height int) string {
	ctx := dashboardeval.NewRenderContextFromEnv(false)
	return dashboardeval.RenderSynergyView(m.eval, ctx, width, height)
}

// evalRepAdjustScroll is a thin wrapper委托至 dashboardeval.RepAdjustScroll.
//
// 保留 dashboardModel pointer receiver 让 atdd_27_10 + handleEvalKey 现有 callsite
// 零行为变化（5+ 处直接调用）。
func evalRepAdjustScroll(m *dashboardModel) {
	if m == nil {
		return
	}
	dashboardeval.RepAdjustScroll(&m.eval, m.height)
}

// evalTopoAdjustScroll is a thin wrapper委托至 dashboardeval.TopoAdjustScroll.
func evalTopoAdjustScroll(m *dashboardModel) {
	if m == nil {
		return
	}
	dashboardeval.TopoAdjustScroll(&m.eval, m.height)
}

// evalSynAdjustScroll is a thin wrapper委托至 dashboardeval.SynAdjustScroll.
func evalSynAdjustScroll(m *dashboardModel) {
	if m == nil {
		return
	}
	dashboardeval.SynAdjustScroll(&m.eval, m.height)
}
