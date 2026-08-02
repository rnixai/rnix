package title

import (
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ComputeCtxPercent returns the context-budget usage percentage (0-999) for the
// selected process. If no process is selected (selectedPID <= 0) this falls back
// to the max usage across all running/created processes.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05 · 第 9 个会话第 9 个 commit):
// Migrated from cmd/rnix/dashboard_title.go::computeCtxPercent. Pure function ·
// zero dashboardModel dependency · 0 cmd/rnix 反向依赖.
//
// 🔴 分子是 LastInputTokens（上下文占用量，provider 上报的最近一请求 prompt
// tokens），不是累计 TokensUsed——累计量 ÷ 单请求容量会恒显满值
// （2026-08-03 meetup epic-14 dashboard 事故同族缺陷）。
//
// Behavior contract (preserved verbatim from cmd/rnix · ATDD 34.2-UNIT-009 测试覆盖):
//
//   - selectedPID > 0:
//     遍历 processes 找到匹配 PID
//   - 命中且 ContextBudget > 0 → ClampPercent(LastInputTokens * 100 / ContextBudget)
//   - 命中且 ContextBudget == 0 → 0
//   - 未命中（PID 已被 reaper 回收）→ 0
//   - selectedPID <= 0:
//     聚合所有 Running/Created 且 ContextBudget > 0 且 LastInputTokens > 0 的进程：
//   - max(LastInputTokens * 100 / ContextBudget) → ClampPercent
//   - 无候选进程 → 0
//
// 返回值通过 ClampPercent 钳到 [0, 999] · 防御 ContextBudget 计算溢出 / 上游 API
// 异常值（同 ClampPercent godoc 描述的防御边界）.
func ComputeCtxPercent(selectedPID types.PID, selectedUUID string, processes []vfs.ProcInfo) int {
	if selectedPID > 0 || selectedUUID != "" {
		for _, p := range processes {
			if (selectedPID > 0 && p.PID == selectedPID) || (selectedUUID != "" && p.UUID == selectedUUID) {
				if p.ContextBudget > 0 && p.LastInputTokens > 0 {
					return ClampPercent(int(int64(p.LastInputTokens) * 100 / int64(p.ContextBudget)))
				}
				return 0
			}
		}
		return 0
	}
	var maxPct int
	for _, p := range processes {
		if (p.State == types.StateRunning || p.State == types.StateCreated) && p.ContextBudget > 0 && p.LastInputTokens > 0 {
			pct := int(int64(p.LastInputTokens) * 100 / int64(p.ContextBudget))
			if pct > maxPct {
				maxPct = pct
			}
		}
	}
	return ClampPercent(maxPct)
}

// ComputeBudgetPercent returns the cost / token budget usage percentage (0-999)
// for the selected process. Cost-based (UsedCost/MaxCost) takes priority over
// token-based (TokensUsed/MaxTokens) when both are configured.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05 · 第 9 个会话第 9 个 commit):
// Migrated from cmd/rnix/dashboard_title.go::computeBudgetPercent. Pure
// function · zero dashboardModel dependency · 0 cmd/rnix 反向依赖.
//
// Behavior contract (preserved verbatim from cmd/rnix · ATDD 34.2-UNIT-010 测试覆盖):
//
//   - selectedPID <= 0 → 0 (Title Bar 上无选中进程时不显示 budget %)
//   - 遍历 processes 找到匹配 PID:
//   - MaxCost > 0      → ClampPercent(UsedCost * 100 / MaxCost)（cost 优先）
//   - MaxCost == 0 且 MaxTokens > 0 → ClampPercent(TokensUsed * 100 / MaxTokens)
//   - 两者都 == 0 → 0
//   - 未命中（PID 已被 reaper 回收）→ 0
//
// 返回值通过 ClampPercent 钳到 [0, 999].
func ComputeBudgetPercent(selectedPID types.PID, selectedUUID string, processes []vfs.ProcInfo) int {
	if selectedPID == 0 && selectedUUID == "" {
		return 0
	}
	for _, p := range processes {
		if (selectedPID > 0 && p.PID == selectedPID) || (selectedUUID != "" && p.UUID == selectedUUID) {
			if p.MaxCost > 0 {
				return ClampPercent(int(p.UsedCost * 100 / p.MaxCost))
			}
			if p.MaxTokens > 0 {
				return ClampPercent(int(int64(p.TokensUsed) * 100 / p.MaxTokens))
			}
			return 0
		}
	}
	return 0
}
