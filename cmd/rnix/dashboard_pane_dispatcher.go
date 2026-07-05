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

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// dispatchPaneKey 将按键分发给当前活动面板处理。
// 注：此函数从 dashboard_nav.go 移出（原 nav.go:329-704），逻辑零改动。
func (m dashboardModel) dispatchPaneKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// 过滤模式：无论哪个面板活跃，都路由到 timeline 处理
	if m.timeline.StepFilterMode {
		m = m.handleTimelineKey(key)
		return m, nil
	}

	// Step Timeline 按键（enter/v/V/p）— V must be checked before v
	if m.activePane == paneTimeline && (len(m.timeline.StepEntries) > 0 || len(m.unifiedEvents) > 0) {
		// Aggregation group toggle (Story 30.8 AC#5)
		// spec-timeline-agg-nav-fix 观察项1：阈值口径与 handleTimelineKey 导航守卫和
		// renderStepTimeline 的 useAggregation 同源（filteredStepCount = filtered unified
		// 里 EventStep 计数）——渲染层是显示模式真相源，Enter/导航必须匹配它。
		if m.filteredStepCount() > 100 && key == "enter" {
			// stepIdx = cursor 在 step-only 序号空间的位置（复用 handleTimelineKey 的
			// unifiedCursorToStepOrd · 与导航路径同一换算逻辑）。
			filtered := m.filteredUnifiedEvents()
			groupIdx := unifiedCursorToStepOrd(filtered, m.timeline.StepCursor) / timeline.AggGroupSize
			// spec-timeline-agg-nav-fix Finding 7: chunk-group 展开态写入
			// ExpandedChunkGroups（groupIdx 键），与 ToolPath 折叠组的
			// ExpandedAggGroups（StepNums[0] 步号键）彻底分离命名空间，
			// 杜绝低步号（0-3）与组索引（0-3）碰撞污染。
			if m.timeline.ExpandedChunkGroups == nil {
				m.timeline.ExpandedChunkGroups = make(map[int]bool)
			}
			m.timeline.ExpandedChunkGroups[groupIdx] = !m.timeline.ExpandedChunkGroups[groupIdx]
			return m, nil
		}

		// Story 41-3 AC2: Enter context-aware — only toggle fold when cursor
		// is at group StartIdx; otherwise fall through to drill-in (v/enter).
		if key == "enter" && m.filteredStepCount() <= 100 {
			filtered := m.filteredUnifiedEvents()
			if len(filtered) > 0 {
				aggGroups := buildToolAggGroups(filtered)
				cursorPos := min(m.timeline.StepCursor, len(filtered)-1)
				for _, g := range aggGroups {
					if cursorPos == g.StartIdx {
						if m.timeline.ExpandedAggGroups == nil {
							m.timeline.ExpandedAggGroups = make(map[int]bool)
						}
						wasExpanded := m.timeline.ExpandedAggGroups[g.StepNums[0]]
						m.timeline.ExpandedAggGroups[g.StepNums[0]] = !wasExpanded
						start := g.StepNums[0]
						end := g.StepNums[len(g.StepNums)-1]
						if wasExpanded {
							m.statusMsg = fmt.Sprintf("Group [%d-%d] collapsed", start, end)
						} else {
							m.statusMsg = fmt.Sprintf("Group [%d-%d] expanded", start, end)
						}
						m.statusMsgTTL = statusMsgDefaultTTL
						return m, nil
					}
				}
				// Story 43-3 review patch P11 (D2 follow-through): same Enter
				// dispatch but for ScriptAggGroup — independent map namespace.
				scriptGroups := buildScriptAggGroups(filtered)
				for _, g := range scriptGroups {
					if cursorPos == g.StartIdx {
						if m.timeline.ExpandedScriptAggGroups == nil {
							m.timeline.ExpandedScriptAggGroups = make(map[int]bool)
						}
						wasExpanded := m.timeline.ExpandedScriptAggGroups[g.StartIdx]
						m.timeline.ExpandedScriptAggGroups[g.StartIdx] = !wasExpanded
						if wasExpanded {
							m.statusMsg = fmt.Sprintf("Script group L%d-L%d %s collapsed", g.FirstLine, g.LastLine, g.StmtKind)
						} else {
							m.statusMsg = fmt.Sprintf("Script group L%d-L%d %s expanded", g.FirstLine, g.LastLine, g.StmtKind)
						}
						m.statusMsgTTL = statusMsgDefaultTTL
						return m, nil
					}
				}
			}
		}

		idx := m.resolveStepIndex()
		if idx >= 0 && idx < len(m.timeline.StepEntries) {
			// V (Shift+V) → Level 3 debug toggle — MUST be before v check
			if key == "V" || key == "shift+V" || msg.ShiftedCode == 'V' {
				entry := &m.timeline.StepEntries[idx]
				switch entry.Level {
				case levelSummary, levelExpanded:
					entry.Level = levelDebug
					if m.timeline.StepDetailCache[entry.Summary.Step] == nil && !m.timeline.FetchingDetail && m.hasProcessSelected() {
						m.timeline.FetchingDetail = true
						m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
						return m, fetchStepDetailCmd(m.selectedPID, m.selectedUUID, entry.Summary.Step)
					}
				case levelDebug:
					entry.Level = levelExpanded
				}
				m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
				return m, nil
			}
			// v or enter → Level 2 expand toggle
			if key == "v" || key == "enter" {
				entry := &m.timeline.StepEntries[idx]
				if entry.Level == levelSummary {
					// Check if detail is loaded and has no expandable content
					if cached, ok := m.timeline.StepDetailCache[entry.Summary.Step]; ok && !hasExpandableContent(cached, entry.Summary) {
						m.statusMsg = "(no additional detail)"
						m.statusMsgTTL = statusMsgDefaultTTL
						return m, nil
					}
					entry.Level = levelExpanded
					if m.timeline.StepDetailCache[entry.Summary.Step] == nil && !m.timeline.FetchingDetail && m.hasProcessSelected() {
						m.timeline.FetchingDetail = true
						m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
						return m, fetchStepDetailCmd(m.selectedPID, m.selectedUUID, entry.Summary.Step)
					}
				} else {
					entry.Level = levelSummary
				}
				m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
				return m, nil
			}
			if key == "P" || key == "shift+P" || msg.ShiftedCode == 'P' {
				// Story 36-1: P key enters Inspector with System Lens
				m2, cmd := m.enterStepInspector()
				m3 := m2.(dashboardModel)
				m3.inspector.Lens = lensSystem
				return m3, cmd
			}
		}

		// P key fallback: process has 0 completed steps → enter Inspector anyway
		if (key == "P" || key == "shift+P" || msg.ShiftedCode == 'P') && len(m.timeline.StepEntries) == 0 {
			m2, cmd := m.enterStepInspector()
			m3 := m2.(dashboardModel)
			m3.inspector.Lens = lensSystem
			return m3, cmd
		}
	}

	// P key on timeline when no events at all
	if m.activePane == paneTimeline && (key == "P" || key == "shift+P" || msg.ShiftedCode == 'P') && len(m.timeline.StepEntries) == 0 {
		m2, cmd := m.enterStepInspector()
		m3 := m2.(dashboardModel)
		m3.inspector.Lens = lensSystem
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
				m.tree.UserManualSelect = true // AC5
				if newCursor < len(m.tree.Rows) {
					m = selectProcess(m, m.tree.Rows[newCursor])
				}
				if newCursor < m.tree.Offset {
					m.tree.Offset = newCursor
				}
				if visibleLines > 0 && newCursor >= m.tree.Offset+visibleLines {
					m.tree.Offset = newCursor - visibleLines + 1
				}
			},
		}
		if ui.HandleListKey(key, nil, &m.tree.Cursor, len(m.tree.Rows), navOpts) {
			m.tree.UserManualSelect = true // AC5
			// Story 34.8: scrolling to the bottom of the tree loads the next
			// (older) page on the following tick. Most-recent-first paging means
			// the newly loaded page is overwhelmingly historical, progressively
			// filling in the deeper process tree until HasMore is exhausted.
			if m.procPaging.HasMore && m.tree.Cursor >= len(m.tree.Rows)-1 {
				m.procPaging.LoadedPages++
			}
			if m.selectedPID != prevPID {
				m2, cmd := m.handlePIDChange()
				return m2, cmd
			}
			return m, nil
		}
		switch key {
		case "enter", " ":
			m.tree.UserManualSelect = true // AC5
			// AC3: Toggle dead subtree collapse on Enter/Space
			if m.tree.Cursor < len(m.tree.Rows) {
				row := m.tree.Rows[m.tree.Cursor]
				if (row.Proc.State == types.StateDead || row.Proc.State == types.StateZombie) && row.Proc.UUID != "" {
					// Check if this dead node has children in the tree
					hasChildren := false
					roots := buildProcessTree(m.processes, m.tree.SortMode, m.tree.SortAsc)
					var findNode func(nodes []*treeNode) *treeNode
					findNode = func(nodes []*treeNode) *treeNode {
						for _, n := range nodes {
							if n.Proc.UUID == row.Proc.UUID {
								return n
							}
							if found := findNode(n.Children); found != nil {
								return found
							}
						}
						return nil
					}
					if node := findNode(roots); node != nil && len(node.Children) > 0 {
						hasChildren = true
					}
					if hasChildren {
						m.tree.CollapsedDeadTrees[row.Proc.UUID] = !m.tree.CollapsedDeadTrees[row.Proc.UUID]
						m.tree.Rows = flattenTreeWithCollapse(roots, m.tree.CollapsedDeadTrees)
						if m.tree.Cursor >= len(m.tree.Rows) {
							m.tree.Cursor = max(0, len(m.tree.Rows)-1)
						}
						return m, nil
					}
				}
				m = selectProcess(m, m.tree.Rows[m.tree.Cursor])
			}
		case "s":
			// Cycle tree sort mode: Time → PID → State → Time
			m.tree.SortMode = (m.tree.SortMode + 1) % 3
			roots := buildProcessTree(m.processes, m.tree.SortMode, m.tree.SortAsc)
			m.tree.Rows = flattenTreeWithCollapse(roots, m.tree.CollapsedDeadTrees)
			m.tree.Cursor = 0
			m.tree.Offset = 0
			if len(m.tree.Rows) > 0 {
				m = selectProcess(m, m.tree.Rows[0])
			}
			label := "Time"
			if m.tree.SortMode < len(treeSortLabels) {
				label = treeSortLabels[m.tree.SortMode]
			}
			m.statusMsg = fmt.Sprintf("Tree sort: %s", label)
			m.statusMsgTTL = statusMsgDefaultTTL
		case "o", "S", "shift+S":
			// Toggle sort direction: asc ↔ desc
			m.tree.SortAsc = !m.tree.SortAsc
			roots := buildProcessTree(m.processes, m.tree.SortMode, m.tree.SortAsc)
			m.tree.Rows = flattenTreeWithCollapse(roots, m.tree.CollapsedDeadTrees)
			m.tree.Cursor = 0
			m.tree.Offset = 0
			if len(m.tree.Rows) > 0 {
				m = selectProcess(m, m.tree.Rows[0])
			}
			dir := "desc"
			if m.tree.SortAsc {
				dir = "asc"
			}
			m.statusMsg = fmt.Sprintf("Tree sort: %s %s", treeSortLabels[m.tree.SortMode], dir)
			m.statusMsgTTL = statusMsgDefaultTTL
		default:
			if (msg.Code == 'K' || msg.ShiftedCode == 'K') && msg.Mod&tea.ModShift != 0 {
				if len(m.tree.Rows) > 0 && m.tree.Cursor < len(m.tree.Rows) {
					m.confirmKill = true
					m.confirmPID = m.tree.Rows[m.tree.Cursor].Proc.PID
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
		if ui.HandleListKey(key, nil, &m.intent.Cursor, len(m.intent.FlatNodes), navOpts) {
			return m, nil
		}
		switch key {
		case "enter":
			if m.intent.Cursor < len(m.intent.FlatNodes) {
				n := m.intent.FlatNodes[m.intent.Cursor]
				// Story 38-4 AC#3 / patch P1: cursor on a non-terminal
				// tree header toggles the user-collapse flag and
				// re-flattens. Keyed by stable RootIntent (not positional
				// treeIndex) so a tree state change does not silently
				// move the toggle to a different tree.
				if n.IsTreeHeader && n.TreeWire != nil && !isIntentTreeTerminal(n.TreeWire.State) {
					if m.intent.TreeCollapsed == nil {
						m.intent.TreeCollapsed = make(map[string]bool)
					}
					key := n.TreeWire.RootIntent
					m.intent.TreeCollapsed[key] = !m.intent.TreeCollapsed[key]
					// Patch P4: prune stale entries pointing at trees no
					// longer in the IPC list.
					m.intent.TreeCollapsed = pruneIntentCollapse(m.intent.TreeCollapsed, m.intent.Trees)
					m.intent.FlatNodes = flattenIntentTreesWithCollapse(m.intent.Trees, m.intent.TreeCollapsed)
					if m.intent.Cursor >= len(m.intent.FlatNodes) {
						m.intent.Cursor = max(0, len(m.intent.FlatNodes)-1)
					}
					return m, nil
				}
				if n.Node != nil && n.Node.PID > 0 {
					targetPID := types.PID(n.Node.PID)
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
						m.statusMsg = "process no longer exists"
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
				} else if n.Node != nil {
					m.statusMsg = "node has no process assigned yet"
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
			if len(m.security.Alerts) > 0 && m.security.Cursor < len(m.security.Alerts)-1 {
				m.security.Cursor++
				securityAdjustScroll(&m)
			}
			return m, nil
		case "up", "k":
			if m.security.Cursor > 0 {
				m.security.Cursor--
				securityAdjustScroll(&m)
			}
			return m, nil
		case "enter":
			if len(m.security.Alerts) > 0 && m.security.Cursor < len(m.security.Alerts) {
				alert := m.security.Alerts[m.security.Cursor]
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
					m.statusMsg = "process no longer exists"
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

	// 全局進程操作（k/l）— 需排除面板内冲突。
	// 注：`r` 已迁移到 Layer 1 的 resumeHandler（Story 43.x 统一语义），
	// strace 录制 toggle 改由 shift+R / R 触发（recordToggleHandler）。
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
	if m.tree.SearchMode {
		switch key {
		case "esc":
			m.tree.SearchMode = false
			m.tree.SearchQuery = ""
			m.tree.SearchCursor = 0
			m.tree.SearchOffset = 0
			return m, nil, true
		case "enter":
			m.tree.SearchMode = false
			return m, nil, true
		case "backspace":
			runes := []rune(m.tree.SearchQuery)
			if len(runes) > 0 {
				m.tree.SearchQuery = string(runes[:len(runes)-1])
				m.tree.SearchCursor = 0
				m.tree.SearchOffset = 0
			}
			return m, nil, true
		default:
			if len([]rune(key)) == 1 {
				m.tree.SearchQuery += key
				m.tree.SearchCursor = 0
				m.tree.SearchOffset = 0
			}
			return m, nil, true
		}
	}

	// '/' enters search input mode
	if key == "/" {
		m.tree.SearchMode = true
		m.tree.SearchCursor = 0
		m.tree.SearchOffset = 0
		return m, nil, true
	}

	// Without an active query, fall through to the normal tree handler
	if m.tree.SearchQuery == "" {
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
		m.tree.SearchCursor = max(0, min(newCursor, len(filtered)-1))
		// Scroll to keep cursor visible
		if m.tree.SearchCursor < m.tree.SearchOffset {
			m.tree.SearchOffset = m.tree.SearchCursor
		}
		if visibleLines > 0 && m.tree.SearchCursor >= m.tree.SearchOffset+visibleLines {
			m.tree.SearchOffset = m.tree.SearchCursor - visibleLines + 1
		}
		// Sync treeCursor to the same process in treeRows
		row := filtered[m.tree.SearchCursor]
		for i, r := range m.tree.Rows {
			if (row.Proc.UUID != "" && r.Proc.UUID == row.Proc.UUID) ||
				(row.Proc.UUID == "" && r.Proc.PID == row.Proc.PID) {
				m.tree.Cursor = i
				break
			}
		}
		m = selectProcess(m, row)
		m.tree.UserManualSelect = true
	}

	switch key {
	case "up", "k":
		navigate(m.tree.SearchCursor - 1)
	case "down", "j":
		navigate(m.tree.SearchCursor + 1)
	case "pgdown":
		navigate(m.tree.SearchCursor + max(visibleLines-1, 1))
	case "pgup":
		navigate(m.tree.SearchCursor - max(visibleLines-1, 1))
	case "home", "g":
		navigate(0)
	case "end", "G", "shift+G":
		navigate(len(filtered) - 1)
	case "enter", " ":
		navigate(m.tree.SearchCursor)
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
