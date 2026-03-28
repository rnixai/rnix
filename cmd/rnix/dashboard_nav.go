package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
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

	// === Layer 1: Prompt Pager 覆盖层 ===
	if m.promptPager {
		if key == "q" || key == "esc" || key == "p" {
			m.promptPager = false
			return m, nil
		}
		if key == "tab" {
			// Cycle tabs: Messages → System → Tools → Messages
			m.promptTab = (m.promptTab + 1) % 3
			detail := m.stepDetailCache[m.promptStep]
			if detail != nil {
				content := formatPromptContent(detail, m.promptStep, m.promptTab)
				m.promptContent = content
				m.promptViewport.SetContent(content)
				m.promptViewport.GotoTop()
			}
			return m, nil
		}
		if key == "home" {
			m.promptViewport.GotoTop()
			return m, nil
		}
		if key == "end" {
			m.promptViewport.GotoBottom()
			return m, nil
		}
		var cmd tea.Cmd
		m.promptViewport, cmd = m.promptViewport.Update(msg)
		return m, cmd
	}

	// === Layer 1.5: Help 覆盖层 ===
	if m.helpOverlay {
		if key == "?" || key == "esc" || key == "q" {
			m.helpOverlay = false
		}
		return m, nil
	}

	// === Layer 2: History 覆盖层 (Story 29-5) ===
	if m.viewMode == viewHistory {
		return m.historyKey(msg)
	}

	// === Layer 2.5: LLM Viewer 覆盖层 (Story 29-6) ===
	if m.viewMode == viewLLM {
		return m.llmViewerKey(msg)
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
	case "L", "shift+L":
		return m.enterLLMViewer()
	case "H":
		return m.enterHistoryView()
	case "esc":
		if m.viewMode == viewExpanded {
			// 让面板先处理内部 Esc（如 Trace 的 tree→list）
			if m.activePane == paneTrace && m.traceViewMode != 0 {
				return m.handleTraceKey(key)
			}
			// z 还原：显示 Tree
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
		// z 键：切换 Tree 侧边栏显隐（类似 VS Code Ctrl+B）
		switch m.viewMode {
		case viewDefault:
			m.viewMode = viewExpanded
			m.expandedPane = m.rightPane
			if m.activePane == paneTree {
				m.activePane = m.rightPane // Tree 隐藏时，焦点移到右侧
			}
		case viewExpanded:
			m.viewMode = viewDefault
		}
		return m, nil
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
	if m.activePane == paneTimeline && len(m.stepEntries) > 0 {
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
			if msg.Code == 'p' {
				entry := m.stepEntries[idx]
				if cached := m.stepDetailCache[entry.summary.Step]; cached != nil {
					m.enterPromptPager(cached, entry.summary.Step)
					return m, nil
				}
				if !m.fetchingDetail && m.selectedPID > 0 {
					m.fetchingDetail = true
					return m, fetchStepDetailForPagerCmd(m.selectedPID, entry.summary.Step)
				}
				return m, nil
			}
		}
	}

	// Tree 面板
	if m.activePane == paneTree {
		prevPID := m.selectedPID
		visibleLines := m.dashboardVisibleLines()
		switch key {
		case "up", "k":
			if m.treeCursor > 0 {
				m.treeCursor--
				if m.treeCursor < len(m.treeRows) {
					m = selectProcess(m, m.treeRows[m.treeCursor])
				}
				if m.treeCursor < m.treeOffset {
					m.treeOffset = m.treeCursor
				}
			}
		case "down", "j":
			if m.treeCursor < len(m.treeRows)-1 {
				m.treeCursor++
				if m.treeCursor < len(m.treeRows) {
					m = selectProcess(m, m.treeRows[m.treeCursor])
				}
				if visibleLines > 0 && m.treeCursor >= m.treeOffset+visibleLines {
					m.treeOffset = m.treeCursor - visibleLines + 1
				}
			}
		case "pgdown":
			jump := max(visibleLines-1, 1)
			m.treeCursor = min(m.treeCursor+jump, len(m.treeRows)-1)
			if m.treeCursor < len(m.treeRows) {
				m = selectProcess(m, m.treeRows[m.treeCursor])
			}
			if visibleLines > 0 && m.treeCursor >= m.treeOffset+visibleLines {
				m.treeOffset = m.treeCursor - visibleLines + 1
			}
		case "pgup":
			jump := max(visibleLines-1, 1)
			m.treeCursor = max(m.treeCursor-jump, 0)
			if m.treeCursor < len(m.treeRows) {
				m = selectProcess(m, m.treeRows[m.treeCursor])
			}
			if m.treeCursor < m.treeOffset {
				m.treeOffset = m.treeCursor
			}
		case "home", "g":
			m.treeCursor = 0
			m.treeOffset = 0
			if len(m.treeRows) > 0 {
				m = selectProcess(m, m.treeRows[0])
			}
		case "end", "G", "shift+G":
			if len(m.treeRows) > 0 {
				m.treeCursor = len(m.treeRows) - 1
				m = selectProcess(m, m.treeRows[m.treeCursor])
				if visibleLines > 0 && m.treeCursor >= m.treeOffset+visibleLines {
					m.treeOffset = m.treeCursor - visibleLines + 1
				}
			}
		case "enter":
			if m.treeCursor < len(m.treeRows) {
				m = selectProcess(m, m.treeRows[m.treeCursor])
			}
		case "s":
			// Cycle tree sort mode: Time → PID → State → Time
			m.treeSortMode = (m.treeSortMode + 1) % 3
			roots := buildProcessTree(m.processes, m.treeSortMode, m.treeSortAsc)
			m.treeRows = flattenTree(roots)
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
		case "S", "shift+S":
			// Toggle sort direction: asc ↔ desc
			m.treeSortAsc = !m.treeSortAsc
			roots := buildProcessTree(m.processes, m.treeSortMode, m.treeSortAsc)
			m.treeRows = flattenTree(roots)
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
		switch key {
		case "down", "j":
			if m.intentCursor < len(m.intentFlatNodes)-1 {
				m.intentCursor++
				intentAdjustScroll(&m)
			}
			return m, nil
		case "up", "k":
			if m.intentCursor > 0 {
				m.intentCursor--
				intentAdjustScroll(&m)
			}
			return m, nil
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

	// 全局进程操作（k/a/l/r）— 需排除面板内冲突
	isPaneNavConflict := (m.activePane == paneTimeline && (key == "l" || key == "h" || key == "k")) ||
		(m.activePane == paneHeatmap && key == "k")
	if !isPaneNavConflict && m.selectedPID > 0 && m.connected {
		switch key {
		case "k":
			m.confirmKill = true
			m.confirmPID = m.selectedPID
			return m, nil
		case "a":
			c := exec.Command(os.Args[0], "gdb", fmt.Sprint(m.selectedPID))
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				return execResultMsg{err: err}
			})
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
