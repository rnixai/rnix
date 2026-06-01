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

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/dashboard/detail"
	dashboarddebug "github.com/rnixai/rnix/internal/dashboard/debug"
	"github.com/rnixai/rnix/internal/dashboard/eval"
	"github.com/rnixai/rnix/internal/dashboard/heatmap"
	"github.com/rnixai/rnix/internal/dashboard/inspector"
	"github.com/rnixai/rnix/internal/dashboard/intent"
	"github.com/rnixai/rnix/internal/dashboard/security"
	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/dashboard/trace"
	"github.com/rnixai/rnix/internal/dashboard/tree"
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
		if !m.hasProcessSelected() {
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
		if !m.timeline.StepFilterMode && len(m.alertStrip.Events) > 0 {
			m.alertStrip.Expanded = !m.alertStrip.Expanded
			if !m.alertStrip.Expanded {
				visible := alertStripHeight(len(m.alertStrip.Events), false)
				if m.alertStrip.Cursor >= visible {
					m.alertStrip.Cursor = 0
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
		if m.alertStrip.Expanded && len(m.alertStrip.Events) > 0 {
			if m.alertStrip.Cursor > 0 {
				m.alertStrip.Cursor--
			}
			return true, m, nil
		}
		return false, m, nil
	}
	d.Layer0.Docs["["] = ui.KeyDoc{Key: "[", Description: "Alert cursor up", ContextNote: "alert strip expanded"}

	// ] — Alert cursor down (nav.go:272-285)
	d.Layer0.Bindings["]"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		if m.alertStrip.Expanded && len(m.alertStrip.Events) > 0 {
			maxLines := alertStripHeight(len(m.alertStrip.Events), true)
			maxCursor := maxLines - 1
			if len(m.alertStrip.Events) > maxLines {
				maxCursor = maxLines - 2
			}
			if m.alertStrip.Cursor < maxCursor {
				m.alertStrip.Cursor++
			}
			return true, m, nil
		}
		return false, m, nil
	}
	d.Layer0.Docs["]"] = ui.KeyDoc{Key: "]", Description: "Alert cursor down", ContextNote: "alert strip expanded"}

	// enter — Alert jump (nav.go:286-321) — handled at Layer 0 only when alert is expanded.
	// When alert strip is collapsed, fall through to Layer 1/2 (Timeline/Tree enter handlers).
	//
	// Story 38-4 AC#2: when the focused alert is an EventImmune (Security
	// daemon) item, route the jump to paneSecurity instead of paneTimeline
	// — Security pane carries the deviation breakdown, while Timeline does
	// not display synthetic immune entries (Dev Notes 2). All branches now
	// also clear the unread mark for the destination pane (AC#1 cleanup).
	d.Layer0.Bindings["enter"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		if !m.alertStrip.Expanded || len(m.alertStrip.Events) == 0 || m.alertStrip.Cursor < 0 || m.alertStrip.Cursor >= len(m.alertStrip.Events) {
			return false, m, nil
		}
		alert := m.alertStrip.Events[m.alertStrip.Cursor]
		// C5: alertExpanded 已激活时统一 consume；PID<=0 时仅给用户反馈，不 fall-through
		// 避免触发 Tree drill-in / Timeline expand / Intent drill 等下层副作用。
		if alert.PID <= 0 {
			m.statusMsg = "Alert has no associated process"
			m.statusMsgTTL = statusMsgDefaultTTL
			return true, m, nil
		}
		// AC#2: route Immune alerts to Security pane; everything else to Timeline.
		targetPane := paneTimeline
		if alert.Type == EventImmune {
			targetPane = paneSecurity
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
			m.activePane = targetPane
			m.alertStrip.Expanded = false
			m2, cmd := m.handlePIDChange()
			m2.activePane = targetPane // handlePIDChange may reset; keep our intent
			m2 = m2.clearPaneUnread(targetPane)
			m2.alertStrip.JumpTarget = &alert
			return true, m2, cmd
		}
		// Same PID: scroll to matching item.
		m.activePane = targetPane
		m.alertStrip.Expanded = false
		m = m.clearPaneUnread(targetPane)
		if targetPane == paneSecurity {
			// AC#2 / patch P5 (2026-05-03): locate cursor inside
			// securityAlerts by matching PID + TimestampMs together.
			// Falling back to PID-only would put the cursor on the first
			// matching alert when an agent has multiple deviations
			// (device_access + syscall_freq) — visually disconnected from
			// the row the user clicked. If the precise pair is not found
			// (e.g. securityAlerts pruned between tick and keypress) we
			// keep the current cursor untouched rather than guessing.
			alertMs := alert.Timestamp.UnixMilli()
			for i, sa := range m.security.Alerts {
				if types.PID(sa.PID) == alert.PID && sa.TimestampMs == alertMs {
					m.security.Cursor = i
					securityAdjustScroll(&m)
					break
				}
			}
		} else {
			filtered := m.filteredUnifiedEvents()
			for i, ev := range filtered {
				if ev.Type == alert.Type && ev.Timestamp.Equal(alert.Timestamp) && ev.PID == alert.PID {
					m.timeline.StepCursor = i
					m.autoExpandGroupForCursor()
					break
				}
			}
		}
		return true, m, nil
	}
	d.Layer0.Docs["enter"] = ui.KeyDoc{Key: "enter", Description: "Alert jump", ContextNote: "alert strip expanded"}

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
			if m.search.Mode {
				// Story 38-5 PR11 Step 4(c)：4 字段重置委托 SearchPlugin.ExitSearchMode
				// （保留 Reverse/CrossLens/NoMatchExpireAt cross-pane 持久状态 · 与
				// 完整 Reset() 区别参见 plugin/search.go godoc）.
				m.search.ExitSearchMode()
				return true, m, nil
			}
			if m.activePane == paneTrace && m.trace.ViewMode != 0 {
				newM, cmd := m.handleTraceKey("esc")
				return true, newM, cmd
			}
			if m.expandedPane == paneTree && (m.tree.SearchMode || m.tree.SearchQuery != "") {
				m.tree.SearchMode = false
				m.tree.SearchQuery = ""
				m.tree.SearchCursor = 0
				m.tree.SearchOffset = 0
				return true, m, nil
			}
			m.tree.SearchQuery = ""
			m.tree.SearchMode = false
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

	// 1 — Focus Tree pane（M1: 上提到 Layer 0 让所有 view 都生效，不仅 viewDefault）
	// 在 viewExpanded 时按 1 退出 expanded（Tree 此时本被隐藏）。
	// Story 38-4 AC#1: clear unread mark on the pane the user just focused.
	d.Layer0.Bindings["1"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		m = m.clearSearchState()
		m.activePane = paneTree
		m = m.clearPaneUnread(paneTree)
		if m.viewMode == viewExpanded {
			m.viewMode = viewDefault
		}
		return true, m, nil
	}
	d.Layer0.Docs["1"] = ui.KeyDoc{Key: "1", Description: "Focus Tree pane"}

	// : — Command mode entry（spec AC3 缺失键之一）
	// 占位实现：本 Story 仅注册键 + 显示 placeholder 反馈。
	// 完整 vim-style command parser（:q :help :goto-pid 等）由后续 Story 补全。
	d.Layer0.Bindings[":"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		m.statusMsg = "Command mode placeholder (use ? for help, q to quit)"
		m.statusMsgTTL = statusMsgDefaultTTL
		return true, m, nil
	}
	d.Layer0.Docs[":"] = ui.KeyDoc{Key: ":", Description: "Command mode (placeholder)"}

	// X — Dismiss expired alerts（spec AC3 "Alert TTL 触发键"）
	// 注：spec 中 Alert TTL 主要是 30s 自动清理 timer（参见 Epic 38），
	// 这里提供"手动触发清理"的对应键，满足 spec 列举的"Alert TTL 触发键"语义。
	d.Layer0.Bindings["X"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		if len(m.alertStrip.Events) == 0 {
			m.statusMsg = "No alerts to dismiss"
			m.statusMsgTTL = statusMsgDefaultTTL
			return true, m, nil
		}
		dismissed := len(m.alertStrip.Events)
		m.alertStrip.Events = nil
		m.alertStrip.Expanded = false
		m.alertStrip.Cursor = 0
		m.statusMsg = fmt.Sprintf("Dismissed %d alert(s)", dismissed)
		m.statusMsgTTL = statusMsgDefaultTTL
		return true, m, nil
	}
	d.Layer0.Docs["X"] = ui.KeyDoc{Key: "X", Description: "Dismiss all alerts (manual TTL)"}

	// 2-8 — switch right pane（M1: 上提到 Layer 0 让所有 view 都生效）
	// Story 38-4 AC#1: clear unread mark on the pane the user focuses.
	digitKeys2to8 := []string{"2", "3", "4", "5", "6", "7", "8"}
	for _, dk := range digitKeys2to8 {
		d.Layer0.Bindings[dk] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
			m := ctx.(dashboardModel)
			m = m.clearSearchState()
			n, err := strconv.Atoi(dk)
			if err != nil || n < 2 || n > 8 {
				return false, m, nil
			}
			p := paneType(n - 1)
			m.rightPane = p
			m.activePane = p
			m = m.clearPaneUnread(p)
			if m.viewMode == viewExpanded {
				m.expandedPane = p
			}
			return true, m, nil
		}
	}
	d.Layer0.Docs["2-8"] = ui.KeyDoc{Key: "2-8", Description: "Switch right pane (Time/Heat/Detail/Intent/Sec/Trace/Eval)"}
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
	// Story 38-4 AC#1: clear unread mark on the pane that becomes active.
	cycleFocus := func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		m = m.clearSearchState()
		if m.viewMode == viewDefault {
			if m.activePane == paneTree {
				m.activePane = m.rightPane
			} else {
				m.activePane = paneTree
			}
			m = m.clearPaneUnread(m.activePane)
		}
		return true, m, nil
	}
	l.Bindings["tab"] = cycleFocus
	l.Bindings["shift+tab"] = cycleFocus
	l.Docs["tab"] = ui.KeyDoc{Key: "Tab", Description: "Cycle pane focus"}

	// 1-8 已上提到 Layer 0（M1）：在所有 view 下生效，包含 viewDebug。

	// z — Enter expanded view (nav.go:233-251)
	// 注：viewExpanded 下的 z 由 Layer 1 Expanded 的 z handler 处理，本 handler
	// 仅注册于 Layer1[viewDefault]，dispatcher 路由保证 viewMode==viewDefault 时进入。
	l.Bindings["z"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		m.viewMode = viewExpanded
		m.expandedPane = m.activePane
		if m.activePane == paneTree {
			m.tree.SearchQuery = ""
			m.tree.SearchMode = false
			m.tree.SearchCursor = 0
			m.tree.SearchOffset = 0
		}
		return true, m, nil
	}
	l.Docs["z"] = ui.KeyDoc{Key: "z", Description: "Expand current pane"}

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

	// f — Timeline filter mode + Story 42.3 fork fallback.
	// Layer 1 Default: Timeline takes priority (existing behavior); when not
	// applicable, `f` falls through to forkProcessHandler so Tree pane selection
	// on a Dead/Zombie process triggers a fork. Both `f` chains delegate to
	// their respective handlers' fallback-false return when the conditions are
	// not met, leaving any earlier behavior unchanged.
	l.Bindings["f"] = func(msg tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		if consumed, newCtx, cmd := timelineFilterHandler(msg, ctx); consumed {
			return consumed, newCtx, cmd
		}
		return forkProcessHandler(msg, ctx)
	}
	l.Docs["f"] = ui.KeyDoc{Key: "f", Description: "Timeline filter / fork Dead-Zombie (Tree pane)"}

	// p / r 统一语义（Story 43.x）：p = pause 专属（不再 toggle）；
	// r = smart resume（覆盖 SIGPAUSE'd / Suspended / Dead / Zombie 三种"停"）。
	l.Bindings["p"] = pauseHandler
	l.Docs["p"] = ui.KeyDoc{Key: "p", Description: "Pause process tree"}
	l.Bindings["r"] = resumeHandler
	l.Docs["r"] = ui.KeyDoc{Key: "r", Description: "Resume process (paused / suspended / dead / zombie)"}

	// shift+R / R — strace 录制 toggle（原 `r` 在 dispatchPaneKey 的 fallthrough
	// 路径，因 `r` 统一归 resume 后迁移至 Layer 1）。
	l.Bindings["R"] = recordToggleHandler
	l.Bindings["shift+R"] = recordToggleHandler
	l.Docs["R"] = ui.KeyDoc{Key: "R", Description: "Toggle strace recording"}

	// k / l / shift+K 不在此 Layer 注册：原 nav.go 把这些"全局进程操作"
	// 放在 dispatchPaneKey 的末端（晚于 pane-specific 导航 j/k），避免 Tree 的
	// k=up 被 kill 抢占。Layer 2 Fallback → dispatchPaneKey 兜底已覆盖。

	d.Layer1[ui.ViewID(viewDefault)] = l
}

// pauseHandler — Story 44.4: `p` 专属 pause（不再 toggle）。
// 仅对 Running / Created 进程发起子树暂停（走 client.PauseSubtree，不再发原始
// SIGPAUSE 信号）；其它状态返回区分化 status 提示。
//
// 与 resumeHandler 配对：用户视角的语义合约是 p=暂停、r=恢复，对称且互不重叠。
// Story 44.1 后进程暂停后进入 StateSuspended（不再是旧 SoftPause 的
// Running+IsPaused），故旧的 proc.IsPaused 死分支已删除。
func pauseHandler(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
	m := ctx.(dashboardModel)
	// Timeline filter mode 拥有 'p'，让位。
	if m.timeline.StepFilterMode {
		return false, m, nil
	}
	if m.selectedPID == 0 {
		m.statusMsg = "Select a process first"
		m.statusMsgTTL = statusMsgDefaultTTL
		return true, m, nil
	}
	if !m.connected {
		return true, m, nil
	}
	proc := findSelectedProcess(&m)
	if proc == nil {
		return true, m, nil
	}
	switch proc.State {
	case types.StateRunning, types.StateCreated:
		return true, m, pauseTreeSubtreeCmd(m.selectedPID)
	case types.StateSuspended:
		m.statusMsg = "Already suspended — press r to resume"
		m.statusMsgTTL = statusMsgDefaultTTL
		return true, m, nil
	default:
		m.statusMsg = "Cannot pause: process is " + proc.State.String()
		m.statusMsgTTL = statusMsgDefaultTTL
		return true, m, nil
	}
}

// resumeHandler — Story 44.4: `r` 专属 smart resume。
// 按状态优先级路由（始终 consume，避免漏到 dispatchPaneKey 的旧 `r` 录制路径）：
//
//  1. StateSuspended              → resumeSubtreeCmd（子树恢复，client.ResumeSubtree）
//  2. StateDead / StateZombie     → resumeProcessCmd（保 UUID 续跑，Epic 42）
//  3. 其他（Running 未暂停等）    → status "Nothing to resume"
//
// Story 44.1 后进程暂停后进入 StateSuspended，旧的 proc.IsPaused+Running SIGRESUME
// 死分支已删除。Suspended 改走子树恢复（修复 Decker 第 4-6 步"父进程橙色恢复后整树
// 停滞"的根因——旧实现走单进程 resumeProcessCmd 不联动子树）；Dead/Zombie 仍是
// 单进程 UUID 续跑语义，不走子树。
func resumeHandler(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
	m := ctx.(dashboardModel)
	if m.selectedPID == 0 {
		m.statusMsg = "Select a process first"
		m.statusMsgTTL = statusMsgDefaultTTL
		return true, m, nil
	}
	if !m.connected {
		return true, m, nil
	}
	proc := findSelectedProcess(&m)
	if proc == nil {
		return true, m, nil
	}
	if proc.State == types.StateSuspended {
		// selectedPID != 0 is guaranteed by the early return above.
		return true, m, resumeSubtreeCmd(m.selectedPID)
	}
	if proc.State == types.StateDead || proc.State == types.StateZombie {
		if m.selectedUUID == "" {
			m.statusMsg = "Cannot resume: no UUID for this process"
			m.statusMsgTTL = statusMsgDefaultTTL
			return true, m, nil
		}
		m.statusMsg = "Resuming UUID " + shortUUIDForStatus(m.selectedUUID) + "..."
		m.statusMsgTTL = statusMsgDefaultTTL
		return true, m, resumeProcessCmd(m.selectedUUID)
	}
	m.statusMsg = "Nothing to resume: process is " + proc.State.String()
	m.statusMsgTTL = statusMsgDefaultTTL
	return true, m, nil
}

// recordToggleHandler — Story 43.x: shift+R / R 触发 strace 录制 toggle。
// 原 `r` 键在 dashboard_pane_dispatcher.go 的 fallthrough 录制路径迁移至此，
// 因 `r` 现在专属 resume。触发条件与原路径一致：选中进程 + 已连接 daemon。
func recordToggleHandler(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
	m := ctx.(dashboardModel)
	if m.selectedPID == 0 || !m.connected {
		return true, m, nil
	}
	recordID := m.recording[m.selectedUUID]
	return true, m, toggleRecordCmd(m.selectedPID, m.selectedUUID, recordID)
}

// forkProcessHandler — Story 42.3.
// Tree pane 选中 Dead/Zombie 进程时按 `f` 触发 fork（fork=true → 新 UUID +
// origin tracking）。AC#5 明确"Tree pane 选中"：从 Detail/Heatmap/History 等
// 其它 pane 上误触 `f` 不应导致 fork。Timeline pane 由 timelineFilterHandler
// 在前面消费时处理。
func forkProcessHandler(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
	m := ctx.(dashboardModel)
	// AC#5 spec contract: 仅 Tree pane 选中 Dead/Zombie 才触发 fork。
	if m.activePane != paneTree {
		return false, m, nil
	}
	if m.selectedPID == 0 || m.selectedUUID == "" || !m.connected {
		return false, m, nil
	}
	proc := findSelectedProcess(&m)
	if proc == nil {
		return false, m, nil
	}
	if proc.State != types.StateDead && proc.State != types.StateZombie {
		return false, m, nil
	}
	m.statusMsg = "Forking UUID " + shortUUIDForStatus(m.selectedUUID) + "..."
	m.statusMsgTTL = statusMsgDefaultTTL
	return true, m, forkProcessCmd(m.selectedUUID)
}

// shortUUIDForStatus returns the first 8 chars of a UUID for status display.
// Story 42.3 stub helper — dashboard-wide CJK-safe truncation already in
// detail.TruncateUUID; this thin wrapper keeps cmd/rnix package self-contained.
func shortUUIDForStatus(uuid string) string {
	if len(uuid) > 8 {
		return uuid[:8]
	}
	return uuid
}

// timelineFilterHandler — Timeline filter mode 入口（M2：viewDefault + viewExpanded 共享）
// 原 nav.go:227-232 行为：rightPane / activePane 任一为 paneTimeline 即生效。
func timelineFilterHandler(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
	m := ctx.(dashboardModel)
	if m.rightPane == paneTimeline || m.activePane == paneTimeline || m.expandedPane == paneTimeline {
		m = m.handleTimelineKey("f")
		return true, m, nil
	}
	return false, m, nil
}

// registerLayer1Expanded registers viewExpanded keys.
func registerLayer1Expanded(d *ui.Dispatcher) {
	l := &ui.KeyLayer{
		Name:     "Expanded View",
		Bindings: map[string]ui.KeyHandler{},
		Docs:     map[string]ui.KeyDoc{},
	}

	// 1-8 已上提到 Layer 0（M1）：viewDefault / viewExpanded / viewDebug 共享。

	// z — collapse to default view
	l.Bindings["z"] = func(_ tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		m := ctx.(dashboardModel)
		m.tree.SearchQuery = ""
		m.tree.SearchMode = false
		m.viewMode = viewDefault
		return true, m, nil
	}
	l.Docs["z"] = ui.KeyDoc{Key: "z", Description: "Collapse to default view"}

	// p / r / shift+R — Story 43.x: 统一语义。viewExpanded 与 viewDefault 一致。
	l.Bindings["p"] = pauseHandler
	l.Docs["p"] = ui.KeyDoc{Key: "p", Description: "Pause process tree"}
	l.Bindings["r"] = resumeHandler
	l.Docs["r"] = ui.KeyDoc{Key: "r", Description: "Resume process (paused / suspended / dead / zombie)"}
	l.Bindings["R"] = recordToggleHandler
	l.Bindings["shift+R"] = recordToggleHandler
	l.Docs["R"] = ui.KeyDoc{Key: "R", Description: "Toggle strace recording"}

	// M2: viewExpanded + paneTree + f 仍能进入 Timeline filter mode（rightPane 命中）。
	// Story 42.3: same f chain as Layer 1 Default — timeline filter wins when
	// Timeline pane is the alternate, otherwise fork takes over for Tree pane.
	l.Bindings["f"] = func(msg tea.KeyPressMsg, ctx ui.KeyContext) (bool, ui.KeyContext, tea.Cmd) {
		if consumed, newCtx, cmd := timelineFilterHandler(msg, ctx); consumed {
			return consumed, newCtx, cmd
		}
		return forkProcessHandler(msg, ctx)
	}
	l.Docs["f"] = ui.KeyDoc{Key: "f", Description: "Timeline filter / fork Dead-Zombie (Tree pane)"}

	d.Layer1[ui.ViewID(viewExpanded)] = l
}

// registerLayer1StepInspector — viewStepInspector keys are entirely handled by
// inspectorKey (cmd/rnix/dashboard_inspector.go). We register a single
// catch-all that delegates so the chain is uniform.
//
// Story 38-5 PR10 Step 2: 注册体（Name + 11 Docs + ActiveModesFn）迁出至
// internal/dashboard/inspector.KeyLayer 以解耦 cmd/rnix 与 inspector 元数据；
// 本函数仅做 dispatcher 注册（行为零变化 · 与 PR2-PR9 同模式）。
//
// 包边界约束：dashboardModel 必须实现 inspector.StateProvider interface（已在
// dashboard.go 通过 InspectorState() 方法满足），ActiveModesFn 通过该接口读取
// 最新 InspectorState（DiffMode / FollowLive 子模式）。
func registerLayer1StepInspector(d *ui.Dispatcher) {
	d.Layer1[ui.ViewID(viewStepInspector)] = inspector.KeyLayer(nil)
}

// registerLayer1Debug — viewDebug keys mostly handled by handleDebugKey.
//
// Story 38-5 PR11 Step 2: 注册体（Name + 5 Docs + new ActiveModesFn）迁出至
// internal/dashboard/debug.KeyLayer 以解耦 cmd/rnix 与 debug 元数据；本函数仅做
// dispatcher 注册（行为零变化 · 与 PR2-PR10 同模式）。
//
// 包边界约束：dashboardModel 必须实现 dashboarddebug.StateProvider interface
// （在 PR11 Step 3 通过 DebugState() 方法落地），ActiveModesFn 通过该接口读取
// 最新 DebugState（ShowStrace / AutoScroll 子模式）。
func registerLayer1Debug(d *ui.Dispatcher) {
	d.Layer1[ui.ViewID(viewDebug)] = dashboarddebug.KeyLayer(nil)
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
// via paneFallback). Story 38-5 PR2 Step 2: 注册体迁出至 internal/dashboard/tree.KeyLayer
// 以解耦 cmd/rnix 与 tree pane 元数据；本函数仅做 dispatcher 注册（行为零变化）。
//
// 包边界约束：dashboardModel 必须实现 tree.StateProvider interface（已在 dashboard.go:302
// 通过 TreeState() 方法满足），ActiveModesFn 通过该接口读取最新 TreeState。
func registerLayer2Tree(d *ui.Dispatcher) {
	d.Layer2[ui.PaneID(paneTree)] = tree.KeyLayer(paneFallback)
}

// registerLayer2Timeline — Timeline pane: f/F/v/V/e/E/C/o/P/enter (delegates to dispatchPaneKey via paneFallback).
// Story 38-5 PR4 Step 2: 注册体迁出至 internal/dashboard/timeline.KeyLayer 以解耦 cmd/rnix
// 与 timeline pane 元数据；本函数仅做 dispatcher 注册（行为零变化）。
//
// 包边界约束：dashboardModel 必须实现 timeline.StateProvider interface（已在 dashboard.go
// 通过 TimelineState() 方法满足 · PR4 Step 1 落地）。
func registerLayer2Timeline(d *ui.Dispatcher) {
	d.Layer2[ui.PaneID(paneTimeline)] = timeline.KeyLayer(paneFallback)
}

// registerLayer2Heatmap — Heatmap pane: =/%/t/f (delegates to dispatchPaneKey via paneFallback).
// Story 38-5 PR3 Step 2: 注册体迁出至 internal/dashboard/heatmap.KeyLayer 以解耦 cmd/rnix
// 与 heatmap pane 元数据；本函数仅做 dispatcher 注册（行为零变化）。
//
// 包边界约束：dashboardModel 必须实现 heatmap.StateProvider interface（已在 dashboard.go
// 通过 HeatmapState() 方法满足 · PR3 Step 1 落地）。
func registerLayer2Heatmap(d *ui.Dispatcher) {
	d.Layer2[ui.PaneID(paneHeatmap)] = heatmap.KeyLayer(paneFallback)
}

func registerLayer2Detail(d *ui.Dispatcher) {
	d.Layer2[ui.PaneID(paneDetail)] = detail.KeyLayer(paneFallback)
}

func registerLayer2Intent(d *ui.Dispatcher) {
	// Story 38-5 PR6 Step 2: KeyLayer 注册体迁出至 internal/dashboard/intent.KeyLayer。
	// dashboardModel 通过 `IntentState() intent.IntentState` getter 满足
	// intent.StateProvider interface（PR6 Step 1 deprecated getter）。
	// 行为零变化：1 个 Docs 键 enter + ActiveModesFn{view:tree, nodes:N}。
	d.Layer2[ui.PaneID(paneIntent)] = intent.KeyLayer(paneFallback)
}

func registerLayer2Security(d *ui.Dispatcher) {
	// Story 38-5 PR7 Step 2: KeyLayer 注册体迁出至 internal/dashboard/security.KeyLayer。
	// dashboardModel 通过 `SecurityState() security.SecurityState` getter 满足 StateProvider。
	// 行为零变化：1 个 Docs 键 enter + ActiveModesFn{view:list, alerts:N}。
	// 38-4 Alert Immune 路由（dashboard_keylayers.go Layer 0 enter handler 中的 EventImmune
	// → paneSecurity 路由）保持在 cmd/rnix 端不变。
	d.Layer2[ui.PaneID(paneSecurity)] = security.KeyLayer(paneFallback)
}

func registerLayer2Trace(d *ui.Dispatcher) {
	// Story 38-5 PR8 Step 2: KeyLayer 注册体迁出至 internal/dashboard/trace.KeyLayer。
	// dashboardModel 通过 `TraceState() trace.TraceState` getter 满足 StateProvider。
	// 行为零变化：3 个 Docs 键 (enter/c/f) + ActiveModesFn{view: overview|spans}。
	d.Layer2[ui.PaneID(paneTrace)] = trace.KeyLayer(paneFallback)
}

func registerLayer2Eval(d *ui.Dispatcher) {
	// Story 38-5 PR9 Step 2: KeyLayer 注册体迁出至 internal/dashboard/eval.KeyLayer。
	// dashboardModel 通过 `EvalState() eval.EvalState` getter 满足 StateProvider。
	// 行为零变化：2 个 Docs 键 (1/2/3, o) + ActiveModesFn{view: reputation|topology|synergy}。
	d.Layer2[ui.PaneID(paneEval)] = eval.KeyLayer(paneFallback)
}
