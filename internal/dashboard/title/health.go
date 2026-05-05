package title

import (
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ComputeCtxPercent returns the context-budget usage percentage (0-999) for the
// selected process. If no process is selected (selectedPID <= 0) this falls back
// to the average usage across all running/created processes.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05 · 第 9 个会话第 9 个 commit):
// Migrated from cmd/rnix/dashboard_title.go::computeCtxPercent. Pure function ·
// zero dashboardModel dependency · 0 cmd/rnix 反向依赖.
//
// Behavior contract (preserved verbatim from cmd/rnix · ATDD 34.2-UNIT-009 测试覆盖):
//
//   - selectedPID > 0:
//     遍历 processes 找到匹配 PID
//   - 命中且 ContextBudget > 0 → ClampPercent(TokensUsed * 100 / ContextBudget)
//   - 命中且 ContextBudget == 0 → 0
//   - 未命中（PID 已被 reaper 回收）→ 0
//   - selectedPID <= 0:
//     聚合所有 Running/Created 且 ContextBudget > 0 的进程：
//   - sum(TokensUsed) * 100 / sum(ContextBudget) → ClampPercent
//   - sum(ContextBudget) == 0 → 0
//
// 返回值通过 ClampPercent 钳到 [0, 999] · 防御 ContextBudget 计算溢出 / 上游 API
// 异常值（同 ClampPercent godoc 描述的防御边界）.
func ComputeCtxPercent(selectedPID types.PID, processes []vfs.ProcInfo) int {
	if selectedPID > 0 {
		for _, p := range processes {
			if p.PID == selectedPID {
				if p.ContextBudget > 0 {
					return ClampPercent(int(int64(p.TokensUsed) * 100 / int64(p.ContextBudget)))
				}
				return 0
			}
		}
		return 0 // selected PID not found (reaped between ticks)
	}
	var totalUsed, totalBudget int64
	for _, p := range processes {
		if (p.State == types.StateRunning || p.State == types.StateCreated) && p.ContextBudget > 0 {
			totalUsed += int64(p.TokensUsed)
			totalBudget += int64(p.ContextBudget)
		}
	}
	if totalBudget > 0 {
		return ClampPercent(int(totalUsed * 100 / totalBudget))
	}
	return 0
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
func ComputeBudgetPercent(selectedPID types.PID, processes []vfs.ProcInfo) int {
	if selectedPID <= 0 {
		return 0
	}
	for _, p := range processes {
		if p.PID == selectedPID {
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
