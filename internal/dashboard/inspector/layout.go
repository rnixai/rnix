// Package inspector — layout.go (Story 38-5 PR11 Step 4(c) ContentHeight 迁出)
//
// 本文件迁出 cmd/rnix/dashboard_inspector.go::inspectorContentHeight 的纯尺寸
// 计算逻辑（terminal height → inspector content area height）。
//
// **迁移动机**（PR11 Step 4(c) · 2026-05-05）：
//
//   - inspectorContentHeight 在 cmd/rnix 端是 `(m dashboardModel)` receiver
//     方法但仅引用 `m.height` 单字段 · 实质是 pure pipeline termHeight → int；
//   - Story 38-3 AC#6 行为契约（h≥20 → 6 行 chrome 含 thumbnail bar · 否则 4 行
//     chrome 不含 thumbnail）保留 · 与本包内 thumbnail.go::Trim/Compress 同抽象级别；
//   - cmd/rnix wrapper 保留 (m dashboardModel) receiver + 同名让 ATDD 27-4
//     callsite (`m.inspectorContentHeight()`) 零修改通过 + dashboard.go line 261
//     callsite 同模式。
//
// 包边界（spec § 04 风险 3 缓解）：
//   - 不 import cmd/rnix（go module 边界已强制）；
//   - 仅依赖 stdlib 内置 max（Go 1.22+）；
//   - **零** cmd/rnix-private 类型引用。

package inspector

// ContentHeight returns the inspector content area height (in lines), given
// the total terminal height. Pure function · zero state dependency.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05): Migrated from
// cmd/rnix/dashboard_inspector.go::inspectorContentHeight. The cmd/rnix end
// retains a thin wrapper with `(m dashboardModel) receiver + lowercase name`
// so existing callsites (dashboard.go:261 + dashboard_inspector.go:83) and
// ATDD 27-4 grep contract continue to work unchanged.
//
// Behavior contract (Story 38-3 AC#6 · preserved verbatim from cmd/rnix):
//   - termHeight >= 20: tall terminals reserve 6 lines for chrome
//     (stepRail 1 + thumbnailBar 2 + lensTabs 1 + footer 1 + spacing 1)
//   - termHeight < 20: short terminals reserve 4 lines for chrome
//     (stepRail 2 + lensTabs 1 + footer 1) and suppress the thumbnail bar
//   - Result is always at least 1 (max clamp ensures non-zero render area)
func ContentHeight(termHeight int) int {
	if termHeight >= 20 {
		// stepRail(1) + thumbnailBar(2) + lensTabs(1) + footer(1) + spacing(1) = 6
		return max(termHeight-6, 1)
	}
	// stepRail(2) + lensTabs(1) + footer(1) = 4
	return max(termHeight-4, 1)
}
