package main

// dashboard-model-info：标题栏与详情卡的 model 显示行为测试。
//
// 覆盖 spec I/O 矩阵在 cmd/rnix 渲染层的断言：
//   - 标题栏 provider 段后追加实际 model（leftPart/leftPlain 双轨）——宽屏显示、
//     窄屏随 provider 段一起 trim、各宽度下首行可见宽度不溢出（真实验证 plain 同步）；
//   - 详情卡 Provider 行追加 Model，空值回退 "—"；
//   - buildMetaLens 对历史(已 reap, PID=0)进程按 UUID 命中正确进程（review HIGH 修复）。

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// 宽屏：标题栏在 provider 之后显示实际 model。
func TestRenderDashboardTitle_ShowsModel(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 7, UUID: "u7", State: types.StateRunning, Provider: "claude", Model: "claude-opus-4-8", ContextBudget: 100000, TokensUsed: 1000},
	}
	m := newTestDashboardModel(procs)
	m.width = 120
	m.selectedPID = 7
	m.selectedUUID = "u7"

	title := m.renderDashboardTitle()
	if !strings.Contains(title, "claude-opus-4-8") {
		t.Errorf("title should show actual model after provider, got: %s", title)
	}
	if !strings.Contains(title, "claude") {
		t.Errorf("title should still show provider, got: %s", title)
	}
}

// 窄屏：model 随 provider 段一起被 trim（Level 1 候选不含 provider 故也不含
// model）。plain/styled 宽度同步由 TestRenderDashboardTitle_ModelWidthSync 验证。
func TestRenderDashboardTitle_ModelTrimsWithProviderOnNarrow(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 7, UUID: "u7", State: types.StateRunning, Provider: "claude", Model: "claude-opus-4-8", ContextBudget: 100000, TokensUsed: 1000},
	}
	m := newTestDashboardModel(procs)
	m.width = 30
	m.selectedPID = 7
	m.selectedUUID = "u7"

	title := m.renderDashboardTitle()
	if strings.Contains(title, "claude-opus-4-8") {
		t.Errorf("narrow title should trim model along with provider segment, got: %s", title)
	}
}

// 真实验证 leftPart/leftPlain 宽度同步：标题首行的可见宽度（lipgloss.Width 忽略
// ANSI）在各终端宽度下都不得超过该宽度。若 leftPlain 漏掉 model（仅 leftPart 含
// 带色 model），trim 会基于偏短的 plain 误判，使含 model 的 rendered 首行超宽——
// 此断言即可捕获（Blind Hunter 审查关切）。
func TestRenderDashboardTitle_ModelWidthSync(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 7, UUID: "u7", State: types.StateRunning, Provider: "claude", Model: "claude-opus-4-8", ContextBudget: 100000, TokensUsed: 1000},
	}
	m := newTestDashboardModel(procs)
	m.selectedPID = 7
	m.selectedUUID = "u7"
	for _, w := range []int{50, 70, 90, 120} {
		m.width = w
		firstLine := strings.SplitN(m.renderDashboardTitle(), "\n", 2)[0]
		if got := lipgloss.Width(firstLine); got > w {
			t.Errorf("width=%d: title first line visible width %d exceeds terminal width (leftPlain/model 未同步?): %q", w, got, firstLine)
		}
	}
}

// 详情卡：Provider 行追加实际 model。
func TestRenderDetailCardLeft_ShowsModel(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.selectedUUID = "uuid-mock-002"
	m.detail.Detail = &ipc.GetProcDetailResponse{
		PID:            2,
		UUID:           "uuid-mock-002",
		Provider:       "claude",
		Model:          "claude-opus-4-8",
		AllowedDevices: []string{"/dev/fs"},
	}

	result := renderDetailCardLeft(&m, 80, 2)
	if !strings.Contains(result, "Model: claude-opus-4-8") {
		t.Errorf("detail card should show model, got %q", result)
	}
}

// 详情卡：Model 为空 → "—" 占位（历史/placeholder 进程）。
func TestRenderDetailCardLeft_EmptyModelDash(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.selectedPID = 2
	m.selectedUUID = "uuid-mock-002"
	m.detail.Detail = &ipc.GetProcDetailResponse{
		PID:            2,
		UUID:           "uuid-mock-002",
		Provider:       "claude",
		Model:          "",
		AllowedDevices: []string{"/dev/fs"},
	}

	result := renderDetailCardLeft(&m, 80, 2)
	if !strings.Contains(result, "Model: —") {
		t.Errorf("empty model should render dash placeholder, got %q", result)
	}
}

// 历史(已 reap)进程 PID 被置 0；buildMetaLens 必须按 selectedUUID 命中正确进程，
// 而非用 0==0 误匹配 m.processes 中第一个 PID=0 的历史进程（review HIGH 修复）。
func TestBuildMetaLens_HistoricalProcessMatchesByUUID(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 0, UUID: "hist-A", State: types.StateDead, Provider: "claude", Model: "model-AAA"},
		{PID: 0, UUID: "hist-B", State: types.StateDead, Provider: "deepseek", Model: "model-BBB"},
	}
	m := newTestDashboardModel(procs)
	m.width = 100
	m.selectedPID = 0
	m.selectedUUID = "hist-B"

	out := m.buildMetaLens(&ipc.GetStepDetailResponse{DriverType: "openai"})
	if !strings.Contains(out, "model-BBB") {
		t.Errorf("historical process should match by UUID (model-BBB), got:\n%s", out)
	}
	if strings.Contains(out, "model-AAA") {
		t.Errorf("should NOT mismatch first PID=0 process (model-AAA):\n%s", out)
	}
}
