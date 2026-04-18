package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// =============================================================================
// Unified Key Dispatch (Story 29.2)
// =============================================================================
//
// Layered dispatch:
//   Layer 0: 全局退出 (q, ctrl+c)
//   Layer 1: Prompt Pager 覆盖层
//   Layer 2: History 覆盖层 (Story 29-5)
//   Layer 3: Kill 确认
//   Layer 4: Replay 模式
//   Layer 5: 主视图全局快捷键 (Esc, Tab, Shift-Tab, digit keys, L, H)
//   Layer 6: 面板内按键分发

func (m dashboardModel) dashboardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// === Layer 0: 全局退出（任何模式） ===
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	// === Layer 1: Step Inspector 覆盖层 (Story 36-1, replaces Prompt Pager) ===
	// (Inspector is handled at Layer 2.5 via viewMode check below)

	// === Layer 1.5: Help 覆盖层 ===
	if m.helpOverlay {
		if key == "?" || key == "esc" || key == "q" {
			m.helpOverlay = false
		}
		return m, nil
	}

	// === Layer 2.5: Step Inspector 覆盖层 (Story 36-1) ===
	if m.viewMode == viewStepInspector {
		return m.inspectorKey(msg)
	}

	// === Layer 2.6: Debug 模式 (Story 34.6) ===
	// Debug is a view mode, not a modal overlay. Only intercept debug-specific
	// keys; unhandled keys fall through to Layer 3+ so global shortcuts
	// (q, ?, L, p, 1-8, etc.) and tree navigation continue to work.
	if m.viewMode == viewDebug {
		if key == "d" || key == "esc" {
			m = m.exitDebugMode()
			return m, nil
		}
		if m2, cmd, handled := m.handleDebugKey(key); handled {
			return m2, cmd
		}
		// Unhandled keys fall through to Layer 3+
	}

	// === Layer 3: Kill 确认 ===
	if m.confirmKill {
		switch key {
		case "y":
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
		default:
			m.confirmKill = false
			m.confirmPID = 0
		}
		return m, nil
	}

	// q 退出（非覆盖层模式）
	if key == "q" {
		return m, tea.Quit
	}

	// === Layer 4: Replay 模式 ===
	if m.replayMode {
		return m.handleReplayKey(key)
	}

	// === Layer 5: 主视图全局快捷键 ===
	switch key {
	case "?":
		m.helpOverlay = true
		return m, nil
	case "d":
		// Debug mode (Story 34.6): only from viewDefault, requires selected process
		if m.viewMode == viewDefault {
			if m.selectedPID == 0 {
				m.statusMsg = "Select a process first"
				m.statusMsgTTL = statusMsgDefaultTTL
				return m, nil
			}
			m2, cmd := m.enterDebugMode()
			return m2, cmd
		}
	case "L", "shift+L":
		return m.enterStepInspector()
	case "R", "shift+R":
		// Resume suspended process (Story 30.8 AC#4)
		if m.selectedPID > 0 && m.selectedUUID != "" && m.connected {
			proc := findSelectedProcess(&m)
			if proc != nil && proc.State == types.StateSuspended {
				return m, resumeProcessCmd(m.selectedUUID)
			}
		}
		return m, nil
	case "p":
		// Pause/resume process tree (SIGPAUSE/SIGRESUME cascade)
		// Skip when timeline filter mode is active — 'p' toggles plan filter in Layer 6
		if m.stepFilterMode {
			break
		}
		if m.selectedPID > 0 && m.connected {
			proc := findSelectedProcess(&m)
			if proc != nil && proc.State == types.StateRunning {
				sig := types.SIGPAUSE
				if proc.IsPaused {
					sig = types.SIGRESUME
				}
				return m, pauseTreeCmd(m.selectedPID, sig)
			}
		}
		if m.selectedPID == 0 {
			m.statusMsg = "Select a process first"
			m.statusMsgTTL = statusMsgDefaultTTL
		}
		return m, nil
	case "esc":
		if m.viewMode == viewExpanded {
			// 让面板先处理内部 Esc（如 Trace 的 tree→list）
			if m.activePane == paneTrace && m.traceViewMode != 0 {
				return m.handleTraceKey(key)
			}
			// If tree search is active, esc clears search first
			if m.expandedPane == paneTree && (m.treeSearchMode || m.treeSearchQuery != "") {
				m.treeSearchMode = false
				m.treeSearchQuery = ""
				m.treeSearchCursor = 0
				m.treeSearchOffset = 0
				return m, nil
			}
			// z 还原：显示 Tree
			m.treeSearchQuery = ""
			m.treeSearchMode = false
			m.viewMode = viewDefault
			return m, nil
		}
	case "tab":
		// Tab：在 Tree 和右侧面板之间切换焦点
		if m.viewMode == viewDefault {
			if m.activePane == paneTree {
				m.activePane = m.rightPane
			} else {
				m.activePane = paneTree
			}
		}
		return m, nil
	case "shift+tab":
		// Shift+Tab：同 Tab，双向切换
		if m.viewMode == viewDefault {
			if m.activePane == paneTree {
				m.activePane = m.rightPane
			} else {
				m.activePane = paneTree
			}
		}
		return m, nil
	case "1":
		// 1：聚焦 Tree 侧边栏
		m.activePane = paneTree
		if m.viewMode == viewExpanded {
			m.viewMode = viewDefault // Tree 不可见时，切回 default 显示 Tree
		}
		return m, nil
	case "2", "3", "4", "5", "6", "7", "8":
		// 2-8：切换右侧面板内容
		n, _ := strconv.Atoi(key)
		p := paneType(n - 1)
		m.rightPane = p
		m.activePane = p
		if m.viewMode == viewExpanded {
			m.expandedPane = p
		}
		return m, nil
	// 展开 Eval 面板时 !/@ /#（Shift+1/2/3）切换子视图
	case "!", "@", "#":
		if m.activePane == paneEval {
			return m.handleEvalKey(key)
		}
	case "f":
		// f 键进入过滤模式：Timeline 可见时可用
		if m.rightPane == paneTimeline || m.activePane == paneTimeline {
			m = m.handleTimelineKey(key)
			return m, nil
		}
	case "z":
		// z 键：最大化当前活动面板
		switch m.viewMode {
		case viewDefault:
			m.viewMode = viewExpanded
			m.expandedPane = m.activePane
			// Reset search state when entering expanded tree
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
		return m, nil
	case "a":
		// Alert strip toggle (Story 34.4): only in non-filter mode
		if !m.stepFilterMode && len(m.alertEvents) > 0 {
			m.alertExpanded = !m.alertExpanded
			if !m.alertExpanded {
				visible := alertStripHeight(len(m.alertEvents), false)
				if m.alertCursor >= visible {
					m.alertCursor = 0
				}
			}
			return m, nil
		}
	case "[":
		// Alert cursor up (Story 34.4)
		if m.alertExpanded && len(m.alertEvents) > 0 {
			if m.alertCursor > 0 {
				m.alertCursor--
			}
			return m, nil
		}
	case "]":
		// Alert cursor down (Story 34.4)
		if m.alertExpanded && len(m.alertEvents) > 0 {
			maxLines := alertStripHeight(len(m.alertEvents), true)
			// F6: reserve last line for overflow indicator when alerts exceed visible lines
			maxCursor := maxLines - 1
			if len(m.alertEvents) > maxLines {
				maxCursor = maxLines - 2
			}
			if m.alertCursor < maxCursor {
				m.alertCursor++
			}
			return m, nil
		}
	case "enter":
		// F1: Alert jump — Enter on expanded alert strip jumps to associated process
		if m.alertExpanded && len(m.alertEvents) > 0 && m.alertCursor < len(m.alertEvents) {
			alert := m.alertEvents[m.alertCursor]
			if alert.PID > 0 {
				// Switch to the alert's process if different
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
					// After PID change, scroll to matching event on next tick
					m2.alertJumpTarget = &alert
					return m2, cmd
				}
				// Same PID: find the matching event in unified timeline and scroll to it
				m.activePane = paneTimeline
				m.alertExpanded = false
				filtered := m.filteredUnifiedEvents()
				for i, ev := range filtered {
					if ev.Type == alert.Type && ev.Timestamp.Equal(alert.Timestamp) && ev.PID == alert.PID {
						m.stepCursor = i
						break
					}
				}
			}
			return m, nil
		}
	}

	// === Layer 6: 面板内按键分发 ===
	return m.dispatchPaneKey(msg)
}

// dispatchPaneKey 将按键分发给当前活动面板处理
func (m dashboardModel) dispatchPaneKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// 过滤模式：无论哪个面板活跃，都路由到 timeline 处理
	if m.stepFilterMode {
		m = m.handleTimelineKey(key)
		return m, nil
	}

	// Step Timeline 按键（enter/v/V/p）— V must be checked before v
	if m.activePane == paneTimeline && (len(m.stepEntries) > 0 || len(m.unifiedEvents) > 0) {
		// Aggregation group toggle (Story 30.8 AC#5)
		// F3: Use step-only count from filtered unified events, not filteredStepEntries
		filteredStep := m.filteredStepEntries()
		if len(filteredStep) > 100 && key == "enter" {
			const aggGroupSize = 50
			// F3: Convert unified cursor to step-only index for group calculation
			stepIdx := 0
			filtered := m.filteredUnifiedEvents()
			cursorPos := min(m.stepCursor, len(filtered)-1)
			for i := 0; i < cursorPos && i < len(filtered); i++ {
				if filtered[i].StepEntry != nil {
					stepIdx++
				}
			}
			groupIdx := stepIdx / aggGroupSize
			if m.expandedAggGroups == nil {
				m.expandedAggGroups = make(map[int]bool)
			}
			m.expandedAggGroups[groupIdx] = !m.expandedAggGroups[groupIdx]
			return m, nil
		}

		// Semantic tool aggregation group toggle (Story 36.3 AC#4)
		if key == "enter" && len(filteredStep) <= 100 {
			filtered := m.filteredUnifiedEvents()
			if len(filtered) > 0 {
				aggGroups := buildToolAggGroups(filtered)
				cursorPos := min(m.stepCursor, len(filtered)-1)
				for _, g := range aggGroups {
					if cursorPos >= g.startIdx && cursorPos < g.endIdx {
						if m.expandedAggGroups == nil {
							m.expandedAggGroups = make(map[int]bool)
						}
						m.expandedAggGroups[g.stepNums[0]] = !m.expandedAggGroups[g.stepNums[0]]
						return m, nil
					}
				}
			}
		}

		idx := m.resolveStepIndex()
		if idx >= 0 && idx < len(m.stepEntries) {
			// V (Shift+V) → Level 3 debug toggle — MUST be before v check
			if key == "V" || key == "shift+V" || msg.ShiftedCode == 'V' {
				entry := &m.stepEntries[idx]
				switch entry.level {
				case levelSummary, levelExpanded:
					entry.level = levelDebug
					if m.stepDetailCache[entry.summary.Step] == nil && !m.fetchingDetail && m.selectedPID > 0 {
						m.fetchingDetail = true
						m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
						return m, fetchStepDetailCmd(m.selectedPID, entry.summary.Step)
					}
				case levelDebug:
					entry.level = levelExpanded
				}
				m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
				return m, nil
			}
			// v or enter → Level 2 expand toggle
			if key == "v" || key == "enter" {
				entry := &m.stepEntries[idx]
				if entry.level == levelSummary {
					// Check if detail is loaded and has no expandable content
					if cached, ok := m.stepDetailCache[entry.summary.Step]; ok && !hasExpandableContent(cached, entry.summary) {
						m.statusMsg = "(no additional detail)"
						m.statusMsgTTL = statusMsgDefaultTTL
						return m, nil
					}
					entry.level = levelExpanded
					if m.stepDetailCache[entry.summary.Step] == nil && !m.fetchingDetail && m.selectedPID > 0 {
						m.fetchingDetail = true
						m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
						return m, fetchStepDetailCmd(m.selectedPID, entry.summary.Step)
					}
				} else {
					entry.level = levelSummary
				}
				m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
				return m, nil
			}
			if key == "P" || key == "shift+P" || msg.ShiftedCode == 'P' {
				// Story 36-1: P key enters Inspector with System Lens
				m2, cmd := m.enterStepInspector()
				m3 := m2.(dashboardModel)
				m3.inspectorLens = lensSystem
				return m3, cmd
			}
		}

		// P key fallback: process has 0 completed steps → enter Inspector anyway
		if (key == "P" || key == "shift+P" || msg.ShiftedCode == 'P') && len(m.stepEntries) == 0 {
			m2, cmd := m.enterStepInspector()
			m3 := m2.(dashboardModel)
			m3.inspectorLens = lensSystem
			return m3, cmd
		}
	}

	// P key on timeline when no events at all
	if m.activePane == paneTimeline && (key == "P" || key == "shift+P" || msg.ShiftedCode == 'P') && len(m.stepEntries) == 0 {
		m2, cmd := m.enterStepInspector()
		m3 := m2.(dashboardModel)
		m3.inspectorLens = lensSystem
		return m3, cmd
	}

	// Tree 面板
	if m.activePane == paneTree {
		// In expanded mode, route to search-aware handler first
		if m.viewMode == viewExpanded && m.expandedPane == paneTree {
			if result, cmd, handled := m.handleExpandedTreeKey(key); handled {
				return result, cmd
			}
		}
		prevPID := m.selectedPID
		visibleLines := m.dashboardVisibleLines()
		// Story 36-5: 六方向导航统一走 HandleListKey
		navOpts := ui.ListNavOpts{
			PageSize: max(visibleLines-1, 1),
			OnCursorChange: func(newCursor int) {
				m.userManualSelect = true // AC5
				if newCursor < len(m.treeRows) {
					m = selectProcess(m, m.treeRows[newCursor])
				}
				if newCursor < m.treeOffset {
					m.treeOffset = newCursor
				}
				if visibleLines > 0 && newCursor >= m.treeOffset+visibleLines {
					m.treeOffset = newCursor - visibleLines + 1
				}
			},
		}
		if ui.HandleListKey(key, nil, &m.treeCursor, len(m.treeRows), navOpts) {
			m.userManualSelect = true // AC5
			if m.selectedPID != prevPID {
				m2, cmd := m.handlePIDChange()
				return m2, cmd
			}
			return m, nil
		}
		switch key {
		case "enter", " ":
			m.userManualSelect = true // AC5
			// AC3: Toggle dead subtree collapse on Enter/Space
			if m.treeCursor < len(m.treeRows) {
				row := m.treeRows[m.treeCursor]
				if (row.proc.State == types.StateDead || row.proc.State == types.StateZombie) && row.proc.UUID != "" {
					// Check if this dead node has children in the tree
					hasChildren := false
					roots := buildProcessTree(m.processes, m.treeSortMode, m.treeSortAsc)
					var findNode func(nodes []*treeNode) *treeNode
					findNode = func(nodes []*treeNode) *treeNode {
						for _, n := range nodes {
							if n.proc.UUID == row.proc.UUID {
								return n
							}
							if found := findNode(n.children); found != nil {
								return found
							}
						}
						return nil
					}
					if node := findNode(roots); node != nil && len(node.children) > 0 {
						hasChildren = true
					}
					if hasChildren {
						m.collapsedDeadTrees[row.proc.UUID] = !m.collapsedDeadTrees[row.proc.UUID]
						m.treeRows = flattenTreeWithCollapse(roots, m.collapsedDeadTrees)
						if m.treeCursor >= len(m.treeRows) {
							m.treeCursor = max(0, len(m.treeRows)-1)
						}
						return m, nil
					}
				}
				m = selectProcess(m, m.treeRows[m.treeCursor])
			}
		case "s":
			// Cycle tree sort mode: Time → PID → State → Time
			m.treeSortMode = (m.treeSortMode + 1) % 3
			roots := buildProcessTree(m.processes, m.treeSortMode, m.treeSortAsc)
			m.treeRows = flattenTreeWithCollapse(roots, m.collapsedDeadTrees)
			m.treeCursor = 0
			m.treeOffset = 0
			if len(m.treeRows) > 0 {
				m = selectProcess(m, m.treeRows[0])
			}
			label := "Time"
			if m.treeSortMode < len(treeSortLabels) {
				label = treeSortLabels[m.treeSortMode]
			}
			m.statusMsg = fmt.Sprintf("Tree sort: %s", label)
			m.statusMsgTTL = statusMsgDefaultTTL
		case "o", "S", "shift+S":
			// Toggle sort direction: asc ↔ desc
			m.treeSortAsc = !m.treeSortAsc
			roots := buildProcessTree(m.processes, m.treeSortMode, m.treeSortAsc)
			m.treeRows = flattenTreeWithCollapse(roots, m.collapsedDeadTrees)
			m.treeCursor = 0
			m.treeOffset = 0
			if len(m.treeRows) > 0 {
				m = selectProcess(m, m.treeRows[0])
			}
			dir := "desc"
			if m.treeSortAsc {
				dir = "asc"
			}
			m.statusMsg = fmt.Sprintf("Tree sort: %s %s", treeSortLabels[m.treeSortMode], dir)
			m.statusMsgTTL = statusMsgDefaultTTL
		default:
			if (msg.Code == 'K' || msg.ShiftedCode == 'K') && msg.Mod&tea.ModShift != 0 {
				if len(m.treeRows) > 0 && m.treeCursor < len(m.treeRows) {
					m.confirmKill = true
					m.confirmPID = m.treeRows[m.treeCursor].proc.PID
				}
			}
		}
		if m.selectedPID != prevPID {
			m2, cmd := m.handlePIDChange()
			return m2, cmd
		}
		return m, nil
	}

	// Intent 面板
	if m.activePane == paneIntent {
		// Story 36-5: 统一导航键集合
		navOpts := ui.ListNavOpts{
			PageSize: max(m.dashboardVisibleLines()-1, 1),
			OnCursorChange: func(int) {
				intentAdjustScroll(&m)
			},
		}
		if ui.HandleListKey(key, nil, &m.intentCursor, len(m.intentFlatNodes), navOpts) {
			return m, nil
		}
		switch key {
		case "enter":
			if m.intentCursor < len(m.intentFlatNodes) {
				n := m.intentFlatNodes[m.intentCursor]
				if n.node != nil && n.node.PID > 0 {
					targetPID := types.PID(n.node.PID)
					pidFound := false
					var targetUUID string
					for _, p := range m.processes {
						if p.PID == targetPID {
							pidFound = true
							targetUUID = p.UUID
							break
						}
					}
					if !pidFound {
						m.statusMsg = "该进程已不存在"
						m.statusMsgTTL = statusMsgDefaultTTL
						return m, nil
					}
					m.selectedPID = targetPID
					m.selectedUUID = targetUUID
					m.activePane = paneTimeline
					m2, cmd := m.handlePIDChange()
					return m2, cmd
				} else if n.node != nil {
					m.statusMsg = "该节点尚未分配进程"
					m.statusMsgTTL = statusMsgDefaultTTL
				}
			}
			return m, nil
		}
	}

	// Security 面板
	if m.activePane == paneSecurity {
		switch key {
		case "down", "j":
			if len(m.securityAlerts) > 0 && m.securityCursor < len(m.securityAlerts)-1 {
				m.securityCursor++
				securityAdjustScroll(&m)
			}
			return m, nil
		case "up", "k":
			if m.securityCursor > 0 {
				m.securityCursor--
				securityAdjustScroll(&m)
			}
			return m, nil
		case "enter":
			if len(m.securityAlerts) > 0 && m.securityCursor < len(m.securityAlerts) {
				alert := m.securityAlerts[m.securityCursor]
				targetPID := types.PID(alert.PID)
				pidFound := false
				var targetUUID string
				for _, p := range m.processes {
					if p.PID == targetPID {
						pidFound = true
						targetUUID = p.UUID
						break
					}
				}
				if !pidFound {
					m.statusMsg = "该进程已不存在"
					m.statusMsgTTL = statusMsgDefaultTTL
					return m, nil
				}
				m.selectedPID = targetPID
				m.selectedUUID = targetUUID
				m.activePane = paneTimeline
				m2, cmd := m.handlePIDChange()
				return m2, cmd
			}
			return m, nil
		}
	}

	// Trace 面板
	if m.activePane == paneTrace {
		return m.handleTraceKey(key)
	}

	// Eval 面板
	if m.activePane == paneEval {
		return m.handleEvalKey(key)
	}

	// 全局進程操作（k/l/r）— 需排除面板内冲突
	isPaneNavConflict := (m.activePane == paneTimeline && (key == "l" || key == "h" || key == "k")) ||
		(m.activePane == paneHeatmap && key == "k")
	if !isPaneNavConflict && m.selectedPID > 0 && m.connected {
		switch key {
		case "k":
			m.confirmKill = true
			m.confirmPID = m.selectedPID
			return m, nil
		case "l":
			c := exec.Command(os.Args[0], "log", fmt.Sprint(m.selectedPID))
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				return execResultMsg{err: err}
			})
		case "r":
			recordID := m.recording[m.selectedUUID]
			return m, toggleRecordCmd(m.selectedPID, m.selectedUUID, recordID)
		}
	}

	if (msg.Code == 'K' || msg.ShiftedCode == 'K') && msg.Mod&tea.ModShift != 0 && m.selectedPID > 0 {
		m.confirmKill = true
		m.confirmPID = m.selectedPID
		return m, nil
	}

	// Timeline / Heatmap 通用按键
	switch m.activePane {
	case paneTimeline:
		m = m.handleTimelineKey(key)
	case paneHeatmap:
		m = m.handleHeatmapKey(key)
	}

	return m, nil
}

// handleExpandedTreeKey handles keys specific to the expanded Agent Tree view
// (search mode, filtered navigation). Returns (model, cmd, handled).
// When handled=false the caller should fall through to the normal tree handler.
func (m dashboardModel) handleExpandedTreeKey(key string) (dashboardModel, tea.Cmd, bool) {
	// --- Search input mode ---
	if m.treeSearchMode {
		switch key {
		case "esc":
			m.treeSearchMode = false
			m.treeSearchQuery = ""
			m.treeSearchCursor = 0
			m.treeSearchOffset = 0
			return m, nil, true
		case "enter":
			m.treeSearchMode = false
			return m, nil, true
		case "backspace":
			runes := []rune(m.treeSearchQuery)
			if len(runes) > 0 {
				m.treeSearchQuery = string(runes[:len(runes)-1])
				m.treeSearchCursor = 0
				m.treeSearchOffset = 0
			}
			return m, nil, true
		default:
			if len([]rune(key)) == 1 {
				m.treeSearchQuery += key
				m.treeSearchCursor = 0
				m.treeSearchOffset = 0
			}
			return m, nil, true
		}
	}

	// '/' enters search input mode
	if key == "/" {
		m.treeSearchMode = true
		m.treeSearchCursor = 0
		m.treeSearchOffset = 0
		return m, nil, true
	}

	// Without an active query, fall through to the normal tree handler
	if m.treeSearchQuery == "" {
		return m, nil, false
	}

	// --- Navigate within filtered results ---
	filtered := m.filteredExpandedRows()
	visibleLines := m.dashboardVisibleLines()
	prevPID := m.selectedPID

	navigate := func(newCursor int) {
		if len(filtered) == 0 {
			return
		}
		m.treeSearchCursor = max(0, min(newCursor, len(filtered)-1))
		// Scroll to keep cursor visible
		if m.treeSearchCursor < m.treeSearchOffset {
			m.treeSearchOffset = m.treeSearchCursor
		}
		if visibleLines > 0 && m.treeSearchCursor >= m.treeSearchOffset+visibleLines {
			m.treeSearchOffset = m.treeSearchCursor - visibleLines + 1
		}
		// Sync treeCursor to the same process in treeRows
		row := filtered[m.treeSearchCursor]
		for i, r := range m.treeRows {
			if (row.proc.UUID != "" && r.proc.UUID == row.proc.UUID) ||
				(row.proc.UUID == "" && r.proc.PID == row.proc.PID) {
				m.treeCursor = i
				break
			}
		}
		m = selectProcess(m, row)
		m.userManualSelect = true
	}

	switch key {
	case "up", "k":
		navigate(m.treeSearchCursor - 1)
	case "down", "j":
		navigate(m.treeSearchCursor + 1)
	case "pgdown":
		navigate(m.treeSearchCursor + max(visibleLines-1, 1))
	case "pgup":
		navigate(m.treeSearchCursor - max(visibleLines-1, 1))
	case "home", "g":
		navigate(0)
	case "end", "G", "shift+G":
		navigate(len(filtered) - 1)
	case "enter", " ":
		navigate(m.treeSearchCursor)
		if m.selectedPID != prevPID {
			m2, cmd := m.handlePIDChange()
			return m2, cmd, true
		}
		return m, nil, true
	default:
		return m, nil, false
	}

	if m.selectedPID != prevPID {
		m2, cmd := m.handlePIDChange()
		return m2, cmd, true
	}
	return m, nil, true
}
