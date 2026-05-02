// Package main — dashboard_keylayers.go (Story 38.1)
//
// 注册 Dashboard 的三层 KeyLayer Dispatcher。本文件按 PR 阶段增量构建：
//   PR1: registerLayer0 — 全局键 (q/ctrl+c/?/a/H/L/d/esc/[/]/enter/...)
//   PR2: registerLayer1Default/Expanded/StepInspector/Debug — view-level
//   PR3: 各 pane 注册函数（在各自 dashboard_*.go 中），合并到 Layer 2 map
//
// 复用约定（Story 38.1 Dev Notes #5）：
//   - 所有 KeyHandler 直接调用现有 dashboardModel 方法（如 enterStepInspector
//     / enterDebugMode / clearSearchState），**禁止重写逻辑**
//   - 每个 KeyHandler 的副作用与重构前 dashboard_nav.go 保持完全等价
package main

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// newDispatcher 构造 Dispatcher 并注册三层 KeyLayer。
// PR1 阶段仅注册 Layer 0；Layer 1/2 在后续 PR 增量加入。
func newDispatcher() *ui.Dispatcher {
	d := ui.NewDispatcher()
	registerLayer0(d)
	registerLayer1Default(d)
	registerLayer1Expanded(d)
	registerLayer1StepInspector(d)
	registerLayer1Debug(d)
	registerLayer2Tree(d)
	registerLayer2Timeline(d)
	registerLayer2Heatmap(d)
	registerLayer2Detail(d)
	registerLayer2Intent(d)
	registerLayer2Security(d)
	registerLayer2Trace(d)
	registerLayer2Eval(d)
	return d
}

// registerLayer0 注册 Global 键集合（15 个键）。
//
// 这些键在任何 view + 任何 pane 都生效，且总是优先于 Layer 1/2。
// 与重构前 dashboard_nav.go 的对应行号在每个 handler 注释中标注。
func registerLayer0(d *ui.Dispatcher) {
	d.Layer0 = &ui.KeyLayer{
		Name:     "Global",
		Bindings: map[string]ui.KeyHandler{},
		Docs:     map[string]ui.KeyDoc{},
	}

	// q — Quit (nav.go:32-33, 89-91)
	d.Layer0.Bindings["q"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		// Help overlay: q closes overlay first, then would quit on next press.
		if m.helpOverlay {
			m.helpOverlay = false
			return true, m, nil
		}
		return true, m, tea.Quit
	}
	d.Layer0.Docs["q"] = ui.KeyDoc{Key: "q", Description: "Quit (closes help first)"}

	// ctrl+c — Force quit (nav.go:32-33)
	d.Layer0.Bindings["ctrl+c"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		return true, ctx, tea.Quit
	}
	d.Layer0.Docs["ctrl+c"] = ui.KeyDoc{Key: "ctrl+c", Description: "Force quit"}

	// ? — Toggle help overlay (nav.go:40-44, 100-102)
	d.Layer0.Bindings["?"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		m.helpOverlay = !m.helpOverlay
		return true, m, nil
	}
	d.Layer0.Docs["?"] = ui.KeyDoc{Key: "?", Description: "Toggle help overlay"}

	// L / shift+L — Step Inspector (nav.go:114-115)
	enterInspector := func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		// Don't enter inspector when already in inspector view.
		if m.viewMode == viewStepInspector {
			return false, m, nil
		}
		// Don't enter inspector when help overlay is open (let q/?/esc close it first).
		if m.helpOverlay {
			return false, m, nil
		}
		newM, cmd := m.enterStepInspector()
		return true, newM, cmd
	}
	d.Layer0.Bindings["L"] = enterInspector
	d.Layer0.Bindings["shift+L"] = enterInspector
	d.Layer0.Docs["L"] = ui.KeyDoc{Key: "L", Description: "Step Inspector"}

	// d — Debug mode toggle (nav.go:103-113, 56-60)
	d.Layer0.Bindings["d"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		// Help overlay: don't intercept.
		if m.helpOverlay {
			return false, m, nil
		}
		// Already in Debug mode — d / esc exits.
		if m.viewMode == viewDebug {
			m = m.exitDebugMode()
			return true, m, nil
		}
		// Only enter Debug mode from viewDefault (preserves original guard).
		if m.viewMode != viewDefault {
			return false, m, nil
		}
		if m.selectedPID == 0 {
			m.statusMsg = "Select a process first"
			m.statusMsgTTL = statusMsgDefaultTTL
			return true, m, nil
		}
		newM, cmd := m.enterDebugMode()
		return true, newM, cmd
	}
	d.Layer0.Docs["d"] = ui.KeyDoc{Key: "d", Description: "Debug mode (requires selected process)"}

	// a — Toggle alert strip (nav.go:252-263)
	d.Layer0.Bindings["a"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		// Defer to Layer 1/2 when in special view modes (but ours is global a).
		if m.helpOverlay {
			return false, m, nil
		}
		if !m.stepFilterMode && len(m.alertEvents) > 0 {
			m.alertExpanded = !m.alertExpanded
			if !m.alertExpanded {
				visible := alertStripHeight(len(m.alertEvents), false)
				if m.alertCursor >= visible {
					m.alertCursor = 0
				}
			}
			return true, m, nil
		}
		return false, m, nil
	}
	d.Layer0.Docs["a"] = ui.KeyDoc{Key: "a", Description: "Toggle alerts strip"}

	// [ — Alert cursor up (nav.go:264-271)
	d.Layer0.Bindings["["] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		if m.alertExpanded && len(m.alertEvents) > 0 {
			if m.alertCursor > 0 {
				m.alertCursor--
			}
			return true, m, nil
		}
		return false, m, nil
	}
	d.Layer0.Docs["["] = ui.KeyDoc{Key: "[", Description: "Alert cursor up", ContextNote: "alert strip expanded"}

	// ] — Alert cursor down (nav.go:272-285)
	d.Layer0.Bindings["]"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		if m.alertExpanded && len(m.alertEvents) > 0 {
			maxLines := alertStripHeight(len(m.alertEvents), true)
			maxCursor := maxLines - 1
			if len(m.alertEvents) > maxLines {
				maxCursor = maxLines - 2
			}
			if m.alertCursor < maxCursor {
				m.alertCursor++
			}
			return true, m, nil
		}
		return false, m, nil
	}
	d.Layer0.Docs["]"] = ui.KeyDoc{Key: "]", Description: "Alert cursor down", ContextNote: "alert strip expanded"}

	// enter — Alert jump (nav.go:286-321) — handled at Layer 0 only when alert is expanded.
	// When alert strip is collapsed, fall through to Layer 1/2 (Timeline/Tree enter handlers).
	d.Layer0.Bindings["enter"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		if !m.alertExpanded || len(m.alertEvents) == 0 || m.alertCursor >= len(m.alertEvents) {
			return false, m, nil
		}
		alert := m.alertEvents[m.alertCursor]
		if alert.PID <= 0 {
			return false, m, nil
		}
		// Switch to alert's process if different
		if alert.PID != m.selectedPID {
			var targetUUID string
			for _, p := range m.processes {
				if p.PID == alert.PID {
					targetUUID = p.UUID
					break
				}
			}
			m.selectedPID = alert.PID
			m.selectedUUID = targetUUID
			m.activePane = paneTimeline
			m.alertExpanded = false
			m2, cmd := m.handlePIDChange()
			m2.alertJumpTarget = &alert
			return true, m2, cmd
		}
		// Same PID: scroll to matching event in unified timeline.
		m.activePane = paneTimeline
		m.alertExpanded = false
		filtered := m.filteredUnifiedEvents()
		for i, ev := range filtered {
			if ev.Type == alert.Type && ev.Timestamp.Equal(alert.Timestamp) && ev.PID == alert.PID {
				m.stepCursor = i
				break
			}
		}
		return true, m, nil
	}
	// enter is documented at the Pane layer (where Timeline/Tree drive its primary semantics).

	// esc — Layered escape (nav.go:146-176; help overlay 41-43; debug 57-59)
	d.Layer0.Bindings["esc"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		// Help overlay: esc closes overlay
		if m.helpOverlay {
			m.helpOverlay = false
			return true, m, nil
		}
		// Debug mode: esc exits
		if m.viewMode == viewDebug {
			m = m.exitDebugMode()
			return true, m, nil
		}
		// confirmKill modal: esc cancels (consistent with nav.go:81-84 default branch)
		if m.confirmKill {
			m.confirmKill = false
			m.confirmPID = 0
			return true, m, nil
		}
		// viewExpanded: layered escape (search → trace tree → tree search → restore)
		if m.viewMode == viewExpanded {
			if m.searchMode {
				m.searchMode = false
				m.searchQuery = ""
				m.searchMatches = nil
				m.searchMatchIdx = 0
				return true, m, nil
			}
			if m.activePane == paneTrace && m.traceViewMode != 0 {
				newM, cmd := m.handleTraceKey("esc")
				return true, newM, cmd
			}
			if m.expandedPane == paneTree && (m.treeSearchMode || m.treeSearchQuery != "") {
				m.treeSearchMode = false
				m.treeSearchQuery = ""
				m.treeSearchCursor = 0
				m.treeSearchOffset = 0
				return true, m, nil
			}
			m.treeSearchQuery = ""
			m.treeSearchMode = false
			m.viewMode = viewDefault
			return true, m, nil
		}
		// viewStepInspector: esc handled by Layer 1 / inspectorKey
		if m.viewMode == viewStepInspector {
			return false, m, nil
		}
		return false, m, nil
	}
	d.Layer0.Docs["esc"] = ui.KeyDoc{Key: "esc", Description: "Back / close (layered)"}

	// y / n — confirmKill modal answers (nav.go:67-86). Only intercept while confirm is active.
	d.Layer0.Bindings["y"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		if !m.confirmKill {
			return false, m, nil
		}
		if m.client != nil && m.confirmPID > 0 {
			if err := m.client.Kill(m.confirmPID, types.SIGTERM); err != nil {
				m.statusMsg = fmt.Sprintf("✗ kill PID %d: %v", m.confirmPID, err)
			} else {
				m.statusMsg = fmt.Sprintf("Killed PID %d", m.confirmPID)
			}
			m.statusMsgTTL = statusMsgDefaultTTL
		}
		m.confirmKill = false
		m.confirmPID = 0
		return true, m, nil
	}
	// y/n are not documented in help overlay (modal, not always available).

	d.Layer0.Bindings["n"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		if !m.confirmKill {
			return false, m, nil
		}
		m.confirmKill = false
		m.confirmPID = 0
		return true, m, nil
	}
}

// ---------------------------------------------------------------------------
// PR2: Layer 1 — View-level KeyLayers
// ---------------------------------------------------------------------------

// registerLayer1Default registers viewDefault keys: 1-8 / tab / shift+tab / z / f / !@#
func registerLayer1Default(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Default View",
		Bindings: map[string]ui.KeyHandler{},
		Docs:     map[string]ui.KeyDoc{},
	}

	// tab / shift+tab — cycle pane focus (nav.go:177-199)
	cycleFocus := func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		m = m.clearSearchState()
		if m.viewMode == viewDefault {
			if m.activePane == paneTree {
				m.activePane = m.rightPane
			} else {
				m.activePane = paneTree
			}
		}
		return true, m, nil
	}
	l.Bindings["tab"] = cycleFocus
	l.Bindings["shift+tab"] = cycleFocus
	l.Docs["tab"] = ui.KeyDoc{Key: "Tab", Description: "Cycle pane focus"}

	// 1 — Tree pane (nav.go:200-209)
	l.Bindings["1"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		m = m.clearSearchState()
		m.activePane = paneTree
		if m.viewMode == viewExpanded {
			m.viewMode = viewDefault
		}
		return true, m, nil
	}
	l.Docs["1"] = ui.KeyDoc{Key: "1", Description: "Focus Tree pane"}

	// 2-8 — switch right pane (nav.go:210-221)
	// Use explicit literal string keys (not strconv.Itoa) so static checks /
	// ATDD tests can find the case strings via grep.
	digitKeys := []string{"2", "3", "4", "5", "6", "7", "8"}
	for _, dk := range digitKeys {
		dkLocal := dk
		l.Bindings[dkLocal] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
			m := ctx.(dashboardModel)
			m = m.clearSearchState()
			n, _ := strconv.Atoi(dkLocal)
			p := paneType(n - 1)
			m.rightPane = p
			m.activePane = p
			if m.viewMode == viewExpanded {
				m.expandedPane = p
			}
			return true, m, nil
		}
	}
	l.Docs["2-8"] = ui.KeyDoc{Key: "2-8", Description: "Switch right pane (Time/Heat/Detail/Intent/Sec/Trace/Eval)"}

	// z — toggle expanded view (nav.go:233-251)
	l.Bindings["z"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		switch m.viewMode {
		case viewDefault:
			m.viewMode = viewExpanded
			m.expandedPane = m.activePane
			if m.activePane == paneTree {
				m.treeSearchQuery = ""
				m.treeSearchMode = false
				m.treeSearchCursor = 0
				m.treeSearchOffset = 0
			}
		case viewExpanded:
			m.treeSearchQuery = ""
			m.treeSearchMode = false
			m.viewMode = viewDefault
		}
		return true, m, nil
	}
	l.Docs["z"] = ui.KeyDoc{Key: "z", Description: "Expand / restore current pane"}

	// !@# — Eval sub-view shortcuts when paneEval is active (nav.go:222-226)
	evalSubView := func(msg tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		if m.activePane != paneEval {
			return false, m, nil
		}
		newM, cmd := m.handleEvalKey(msg.String())
		return true, newM, cmd
	}
	l.Bindings["!"] = evalSubView
	l.Bindings["@"] = evalSubView
	l.Bindings["#"] = evalSubView

	// f — Timeline filter mode: enterable from Tree pane too (nav.go:227-232)
	l.Bindings["f"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		if m.rightPane == paneTimeline || m.activePane == paneTimeline {
			m = m.handleTimelineKey("f")
			return true, m, nil
		}
		return false, m, nil
	}
	l.Docs["f"] = ui.KeyDoc{Key: "f", Description: "Timeline filter (also from Tree pane)"}

	// p — Pause/resume process tree (nav.go:125-145)
	l.Bindings["p"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		// Skip when timeline filter mode is active (Timeline owns 'p' there).
		if m.stepFilterMode {
			return false, m, nil
		}
		if m.selectedPID > 0 && m.connected {
			proc := findSelectedProcess(&m)
			if proc != nil && proc.State == types.StateRunning {
				sig := types.SIGPAUSE
				if proc.IsPaused {
					sig = types.SIGRESUME
				}
				return true, m, pauseTreeCmd(m.selectedPID, sig)
			}
		}
		if m.selectedPID == 0 {
			m.statusMsg = "Select a process first"
			m.statusMsgTTL = statusMsgDefaultTTL
		}
		return true, m, nil
	}
	l.Docs["p"] = ui.KeyDoc{Key: "p", Description: "Pause / resume process tree"}

	// R / shift+R — Resume suspended process (nav.go:116-124)
	resumeSuspended := func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		if m.selectedPID > 0 && m.selectedUUID != "" && m.connected {
			proc := findSelectedProcess(&m)
			if proc != nil && proc.State == types.StateSuspended {
				return true, m, resumeProcessCmd(m.selectedUUID)
			}
		}
		return true, m, nil
	}
	l.Bindings["R"] = resumeSuspended
	l.Bindings["shift+R"] = resumeSuspended
	l.Docs["R"] = ui.KeyDoc{Key: "R", Description: "Resume suspended process"}

	// k / l / r / shift+K 不在此 Layer 注册：原 nav.go 把这些"全局进程操作"
	// 放在 dispatchPaneKey 的末端（晚于 pane-specific 导航 j/k），避免 Tree 的
	// k=up 被 kill 抢占。Layer 2 Fallback → dispatchPaneKey 兜底已覆盖。

	d.Layer1[ui.ViewID(viewDefault)] = l
}

// registerLayer1Expanded registers viewExpanded keys.
func registerLayer1Expanded(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Expanded View",
		Bindings: map[string]ui.KeyHandler{},
		Docs:     map[string]ui.KeyDoc{},
	}

	// 1 / 2-8 — same as Default view (also switch active pane in expanded mode)
	l.Bindings["1"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		m = m.clearSearchState()
		m.activePane = paneTree
		// When in Expanded view, focusing Tree returns to default (Tree was hidden)
		m.viewMode = viewDefault
		return true, m, nil
	}
	digitKeys := []string{"2", "3", "4", "5", "6", "7", "8"}
	for _, dk := range digitKeys {
		dkLocal := dk
		l.Bindings[dkLocal] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
			m := ctx.(dashboardModel)
			m = m.clearSearchState()
			n, _ := strconv.Atoi(dkLocal)
			p := paneType(n - 1)
			m.rightPane = p
			m.activePane = p
			m.expandedPane = p
			return true, m, nil
		}
	}
	l.Docs["1-8"] = ui.KeyDoc{Key: "1-8", Description: "Switch expanded pane"}

	// z — collapse to default view
	l.Bindings["z"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		m.treeSearchQuery = ""
		m.treeSearchMode = false
		m.viewMode = viewDefault
		return true, m, nil
	}
	l.Docs["z"] = ui.KeyDoc{Key: "z", Description: "Collapse to default view"}

	d.Layer1[ui.ViewID(viewExpanded)] = l
}

// registerLayer1StepInspector — viewStepInspector keys are entirely handled by
// inspectorKey (cmd/rnix/dashboard_inspector.go). We register a single
// catch-all that delegates so the chain is uniform.
func registerLayer1StepInspector(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Step Inspector",
		Bindings: map[string]ui.KeyHandler{},
		Docs:     map[string]ui.KeyDoc{},
	}
	// Document the inspector key set for help overlay (handler logic stays in inspectorKey).
	l.Docs["1-5"] = ui.KeyDoc{Key: "1-5", Description: "Switch lens"}
	l.Docs["h/l"] = ui.KeyDoc{Key: "h/l", Description: "Prev / next step"}
	l.Docs["H/L"] = ui.KeyDoc{Key: "H/L", Description: "First / last step"}
	l.Docs["j/k"] = ui.KeyDoc{Key: "j/k", Description: "Scroll lens content"}
	l.Docs["/"] = ui.KeyDoc{Key: "/", Description: "Search"}
	l.Docs["n/N"] = ui.KeyDoc{Key: "n/N", Description: "Next / previous match"}
	l.Docs["d"] = ui.KeyDoc{Key: "d", Description: "Diff mode (dd to pick base)"}
	l.Docs["F"] = ui.KeyDoc{Key: "F", Description: "Follow live"}
	l.Docs["y"] = ui.KeyDoc{Key: "y", Description: "Copy to clipboard"}
	l.Docs["o"] = ui.KeyDoc{Key: "o", Description: "Open in pager"}
	l.Docs["esc"] = ui.KeyDoc{Key: "esc", Description: "Close inspector"}
	d.Layer1[ui.ViewID(viewStepInspector)] = l
}

// registerLayer1Debug — viewDebug keys mostly handled by handleDebugKey.
func registerLayer1Debug(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Debug View",
		Bindings: map[string]ui.KeyHandler{},
		Docs:     map[string]ui.KeyDoc{},
	}
	// Documentation only; actual handlers remain in handleDebugKey for now.
	l.Docs["s"] = ui.KeyDoc{Key: "s", Description: "Toggle strace"}
	l.Docs["f"] = ui.KeyDoc{Key: "f", Description: "Filter events"}
	l.Docs["v"] = ui.KeyDoc{Key: "v", Description: "Expand detail"}
	l.Docs["j/k"] = ui.KeyDoc{Key: "j/k", Description: "Navigate events"}
	l.Docs["d"] = ui.KeyDoc{Key: "d", Description: "Exit debug mode"}
	d.Layer1[ui.ViewID(viewDebug)] = l
}

// ---------------------------------------------------------------------------
// PR3: Layer 2 — Pane-level KeyLayers
// ---------------------------------------------------------------------------
//
// Per-pane KeyLayers route keys based on m.activePane. Each pane handler
// MUST reuse existing dashboardModel methods (selectProcess / handleTimelineKey
// / handleHeatmapKey / etc.) rather than reimplementing logic.

// paneFallback 把所有未被 Layer 0/1/2 Bindings 消费的键路由到原 dispatchPaneKey。
// 这是 Story 38.1 PR3 风险 1（行为回归）的"零迁移成本"兜底——
// 既保证 nav.go 削减至 ≤ 50 行，又确保每个 pane 的现有逻辑零改动。
//
// 后续 PR / Story 可以将 dispatchPaneKey 中的 pane-specific 块再拆到各
// dashboard_*.go 文件中（例如 handleTreePaneKey / handleIntentPaneKey），
// 届时 Layer 2 Fallback 可以从此处的统一委托改为各自直连。
func paneFallback(msg tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
	m, ok := ctx.(dashboardModel)
	if !ok {
		return false, ctx, nil
	}
	result, cmd := m.dispatchPaneKey(msg)
	if mm, ok := result.(dashboardModel); ok {
		// dispatchPaneKey 总是返回（model, cmd）；视为已消费（含 noop 返回 m, nil 的情况）。
		return true, mm, cmd
	}
	return true, m, cmd
}

// registerLayer2Tree — Tree pane: K/P/s/o/R/enter (delegates to dispatchPaneKey
// via Layer 6 fallback). For PR3, we document the keys here and route any
// pane-local handlers via the existing dispatchPaneKey path (which Layer 2
// fallback into via dashboardKey).
func registerLayer2Tree(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Tree Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: paneFallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			m, ok := ctx.(dashboardModel)
			if !ok {
				return nil
			}
			modes := []ui.Mode{}
			label := "time"
			if m.treeSortMode < len(treeSortLabels) {
				label = strings.ToLower(treeSortLabels[m.treeSortMode])
			}
			modes = append(modes, ui.Mode{Name: "sort", Value: label})
			dir := "desc"
			if m.treeSortAsc {
				dir = "asc"
			}
			modes = append(modes, ui.Mode{Name: "dir", Value: dir})
			if m.treeSearchMode || m.treeSearchQuery != "" {
				modes = append(modes, ui.Mode{Name: "search", Value: "on"})
			}
			return modes
		},
	}
	l.Docs["K"] = ui.KeyDoc{Key: "K", Description: "Kill process"}
	l.Docs["s"] = ui.KeyDoc{Key: "s", Description: "Cycle sort mode (Time/PID/State)"}
	l.Docs["o"] = ui.KeyDoc{Key: "o", Description: "Toggle sort direction"}
	l.Docs["enter"] = ui.KeyDoc{Key: "enter", Description: "Select / collapse dead subtree"}
	l.Docs["/"] = ui.KeyDoc{Key: "/", Description: "Search tree (in expanded view)"}
	d.Layer2[ui.PaneID(paneTree)] = l
}

func registerLayer2Timeline(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Timeline Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: paneFallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			m, ok := ctx.(dashboardModel)
			if !ok {
				return nil
			}
			modes := []ui.Mode{}
			if m.stepFilterMode {
				modes = append(modes, ui.Mode{Name: "filter", Value: "on"})
			}
			expandLabel := "collapsed"
			switch m.expandMode {
			case expandModeExpanded:
				expandLabel = "all"
			case expandModeErrorsOnly:
				expandLabel = "errors"
			}
			modes = append(modes, ui.Mode{Name: "expand", Value: expandLabel})
			return modes
		},
	}
	l.Docs["f"] = ui.KeyDoc{Key: "f", Description: "Filter mode (event types)"}
	l.Docs["F"] = ui.KeyDoc{Key: "F", Description: "Toggle follow live"}
	l.Docs["v"] = ui.KeyDoc{Key: "v", Description: "Expand step detail (L2)"}
	l.Docs["V"] = ui.KeyDoc{Key: "V", Description: "Debug detail (L3)"}
	l.Docs["e"] = ui.KeyDoc{Key: "e", Description: "Expand mode: all"}
	l.Docs["E"] = ui.KeyDoc{Key: "E", Description: "Expand mode: errors only"}
	l.Docs["C"] = ui.KeyDoc{Key: "C", Description: "Expand mode: collapsed"}
	l.Docs["o"] = ui.KeyDoc{Key: "o", Description: "Toggle sort direction"}
	l.Docs["P"] = ui.KeyDoc{Key: "P", Description: "Step Inspector (System lens)"}
	l.Docs["enter"] = ui.KeyDoc{Key: "enter", Description: "Expand / collapse"}
	d.Layer2[ui.PaneID(paneTimeline)] = l
}

func registerLayer2Heatmap(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Heatmap Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: paneFallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			m, ok := ctx.(dashboardModel)
			if !ok {
				return nil
			}
			modes := []ui.Mode{}
			if m.heatmapExpanded {
				modes = append(modes, ui.Mode{Name: "view", Value: "expanded"})
			} else {
				modes = append(modes, ui.Mode{Name: "view", Value: "summary"})
			}
			return modes
		},
	}
	l.Docs["="] = ui.KeyDoc{Key: "=", Description: "Absolute scale"}
	l.Docs["%"] = ui.KeyDoc{Key: "%", Description: "Relative scale"}
	l.Docs["t"] = ui.KeyDoc{Key: "t", Description: "Toggle totals"}
	l.Docs["f"] = ui.KeyDoc{Key: "f", Description: "Filter by segment kind"}
	d.Layer2[ui.PaneID(paneHeatmap)] = l
}

func registerLayer2Detail(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Detail Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: paneFallback,
		Docs:     map[string]ui.KeyDoc{},
	}
	l.Docs["v"] = ui.KeyDoc{Key: "v", Description: "Toggle full / compact"}
	l.Docs["y"] = ui.KeyDoc{Key: "y", Description: "Copy"}
	d.Layer2[ui.PaneID(paneDetail)] = l
}

func registerLayer2Intent(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Intent Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: paneFallback,
		Docs:     map[string]ui.KeyDoc{},
	}
	l.Docs["enter"] = ui.KeyDoc{Key: "enter", Description: "Drill in to process timeline"}
	d.Layer2[ui.PaneID(paneIntent)] = l
}

func registerLayer2Security(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Security Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: paneFallback,
		Docs:     map[string]ui.KeyDoc{},
	}
	l.Docs["enter"] = ui.KeyDoc{Key: "enter", Description: "Drill in to process timeline"}
	d.Layer2[ui.PaneID(paneSecurity)] = l
}

func registerLayer2Trace(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Trace Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: paneFallback,
		Docs:     map[string]ui.KeyDoc{},
	}
	l.Docs["enter"] = ui.KeyDoc{Key: "enter", Description: "Drill in to span tree"}
	l.Docs["c"] = ui.KeyDoc{Key: "c", Description: "Collapse"}
	l.Docs["f"] = ui.KeyDoc{Key: "f", Description: "Filter by status"}
	d.Layer2[ui.PaneID(paneTrace)] = l
}

func registerLayer2Eval(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Eval Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: paneFallback,
		Docs:     map[string]ui.KeyDoc{},
	}
	l.Docs["1/2/3"] = ui.KeyDoc{Key: "1/2/3", Description: "Switch sub-view"}
	l.Docs["o"] = ui.KeyDoc{Key: "o", Description: "Sort by score"}
	d.Layer2[ui.PaneID(paneEval)] = l
}
