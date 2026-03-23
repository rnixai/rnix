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
//   Layer 2: Kill 确认
//   Layer 3: Replay 模式
//   Layer 4: 主视图全局快捷键 (Esc, Tab, Shift-Tab, digit keys, L, H)
//   Layer 5: 面板内按键分发

func (m dashboardModel) dashboardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// === Layer 0: 全局退出（任何模式） ===
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	// === Layer 1: Prompt Pager 覆盖层 ===
	if m.promptPager {
		if key == "q" || key == "esc" {
			m.promptPager = false
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

	// === Layer 2: Kill 确认 ===
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

	// === Layer 3: Replay 模式 ===
	if m.replayMode {
		return m.handleReplayKey(key)
	}

	// === Layer 4: 主视图全局快捷键 ===
	switch key {
	case "L":
		m.statusMsg = "LLM 查看器尚未实现（Story 29.6）"
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	case "H":
		m.statusMsg = "历史视图尚未实现（Story 29.5）"
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	case "esc":
		if m.viewMode == viewExpanded {
			// 让面板先处理内部 Esc（如 Trace 的 tree→list）
			if m.expandedPane == paneTrace && m.traceViewMode != 0 {
				return m.handleTraceKey(key)
			}
			m.viewMode = viewDefault
			return m, nil
		}
	case "tab":
		m.activePane = (m.activePane + 1) % 8
		m.viewMode = viewExpanded
		m.expandedPane = m.activePane
		return m, nil
	case "shift+tab":
		m.activePane = (m.activePane + 7) % 8
		m.viewMode = viewExpanded
		m.expandedPane = m.activePane
		return m, nil
	case "1", "2", "3", "4", "5", "6", "7", "8":
		n, _ := strconv.Atoi(key)
		// 展开 Eval 面板时 1/2/3 传给面板处理子视图切换
		if m.viewMode == viewExpanded && m.expandedPane == paneEval && n >= 1 && n <= 3 {
			return m.handleEvalKey(key)
		}
		// 展开 Timeline 面板时 1-4 传给面板处理 filter 键
		if m.viewMode == viewExpanded && m.expandedPane == paneTimeline && n >= 1 && n <= 4 {
			m = m.handleTimelineKey(key)
			return m, nil
		}
		return m.jumpToPane(paneType(n - 1))
	}

	// === Layer 5: 面板内按键分发 ===
	return m.dispatchPaneKey(msg)
}

// jumpToPane 跳转到指定面板，若已在该面板则 toggle 回默认视图
func (m dashboardModel) jumpToPane(p paneType) (tea.Model, tea.Cmd) {
	if m.viewMode == viewExpanded && m.expandedPane == p {
		m.viewMode = viewDefault
	} else {
		m.viewMode = viewExpanded
		m.expandedPane = p
	}
	m.activePane = p
	return m, nil
}

// dispatchPaneKey 将按键分发给当前活动面板处理
func (m dashboardModel) dispatchPaneKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Step Timeline 按键（v/V/p）
	if m.activePane == paneTimeline && m.stepTimelineMode && len(m.stepEntries) > 0 && m.stepCursor < len(m.stepEntries) {
		if msg.Code == 'v' {
			entry := &m.stepEntries[m.stepCursor]
			if entry.level == levelSummary {
				entry.level = levelExpanded
				if m.stepDetailCache[entry.summary.Step] == nil && !m.fetchingDetail && m.selectedPID > 0 {
					m.fetchingDetail = true
					return m, fetchStepDetailCmd(m.selectedPID, entry.summary.Step)
				}
			} else {
				entry.level = levelSummary
			}
			return m, nil
		}
		if msg.Code == 'V' {
			entry := &m.stepEntries[m.stepCursor]
			switch entry.level {
			case levelSummary, levelExpanded:
				entry.level = levelDebug
				if m.stepDetailCache[entry.summary.Step] == nil && !m.fetchingDetail && m.selectedPID > 0 {
					m.fetchingDetail = true
					return m, fetchStepDetailCmd(m.selectedPID, entry.summary.Step)
				}
			case levelDebug:
				entry.level = levelExpanded
			}
			return m, nil
		}
		if msg.Code == 'p' {
			entry := m.stepEntries[m.stepCursor]
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
		case "enter":
			if m.treeCursor < len(m.treeRows) {
				m = selectProcess(m, m.treeRows[m.treeCursor])
			}
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
