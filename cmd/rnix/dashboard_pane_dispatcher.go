// Package main — dashboard_pane_dispatcher.go (Story 38.1)
//
// dispatchPaneKey 与 handleExpandedTreeKey 在 Story 38.1 重构中从 dashboard_nav.go
// 中移出。原 nav.go 的 6 层 implicit dispatcher 已替换为 internal/ui/keylayer.go
// 的 3 层 explicit Dispatcher（Layer 0 Global / Layer 1 View / Layer 2 Pane）。
//
// 调度链终点：当 Layer 0/1/2 均未消费一个键时，Layer 2 KeyLayer 的 Fallback
// 会调用本文件的 dispatchPaneKey，对当前 active pane 做最终分发。
//
// 复用约定：本文件的代码与重构前 nav.go:329-812 完全等价（仅函数注释改写），
// 行为不能与原实现有任何偏差——这是 Story 38.1 风险 1（行为回归）的硬契约。
package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// dispatchPaneKey 将按键分发给当前活动面板处理。
// 注：此函数从 dashboard_nav.go 移出（原 nav.go:329-704），逻辑零改动。
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
		// Story 36-5 P-8: Search 不在此面板可用 — 提示用户而不是默默被吞
		if key == "/" {
			m.statusMsg = "Search not available in this pane"
			m.statusMsgTTL = statusMsgDefaultTTL
			return m, nil
		}
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
				// Story 38-4 AC#3: cursor on a non-terminal tree header
				// toggles the user-collapse flag and re-flattens. Terminal
				// trees stay collapsed regardless of toggle.
				if n.isTreeHeader && n.treeWire != nil && !isIntentTreeTerminal(n.treeWire.State) {
					if m.intentTreeCollapsed == nil {
						m.intentTreeCollapsed = make(map[int]bool)
					}
					m.intentTreeCollapsed[n.treeIndex] = !m.intentTreeCollapsed[n.treeIndex]
					m.intentFlatNodes = flattenIntentTreesWithCollapse(m.intentTrees, m.intentTreeCollapsed)
					if m.intentCursor >= len(m.intentFlatNodes) {
						m.intentCursor = max(0, len(m.intentFlatNodes)-1)
					}
					return m, nil
				}
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
					// Story 38-4 AC#3: clear Timeline unread when drilling in.
					m = m.clearPaneUnread(paneTimeline)
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
// 注：此函数从 dashboard_nav.go 移出（原 nav.go:706-812），逻辑零改动。
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
