// Package inspector — thumbnail.go (Story 38-5 PR11 Step 4(a-2))
//
// Inspector Step Thumbnail Bar window helpers (Story 38-3 AC#6):
//
//   - TrimThumbnailToWidth     （原 trimThumbnailToWidth · 窄屏 cap maxSlots）
//   - CompressThumbnailWindow  （原 compressThumbnailWindow · 长 step list 压缩）
//
// 两个函数都纯：仅依赖 ipc.StepSummaryWire 切片 + 当前 step 编号 + 窗口大小，
// 不引用 dashboardModel / InspectorState。原 Story 38-3 review P10/P14 的边界
// 处理（current step 缺失时返回最近 tail + 前置 sentinel）完整保留。
//
// `…` 用 ipc.StepSummaryWire{Step: -1} 作为 sentinel；renderStepThumbnailBar
// （仍在 cmd/rnix）通过判断 Step == -1 渲染省略号字形。
package inspector

import (
	"github.com/rnixai/rnix/ipc"
)

// TrimThumbnailToWidth 把 steps 切片缩到 maxSlots 个条目，保留 cur 步在可见窗口内。
//
// 行为（Story 38-3 review P10）：
//   - len(steps) ≤ maxSlots → 原样返回（无切片操作）
//   - cur 不在 steps 中（async load race）→ 中央对齐
//   - 切边时左 / 右两侧分别插入 ipc.StepSummaryWire{Step: -1} sentinel 让截断可见
//
// 输出长度 = min(len(steps), maxSlots) + 0/1/2 个 sentinel。
func TrimThumbnailToWidth(steps []ipc.StepSummaryWire, cur, maxSlots int) []ipc.StepSummaryWire {
	if len(steps) <= maxSlots {
		return steps
	}
	curIdx := -1
	for i, s := range steps {
		if s.Step == cur {
			curIdx = i
			break
		}
	}
	if curIdx < 0 {
		curIdx = len(steps) / 2
	}
	side := max(maxSlots/2, 1)
	start := max(curIdx-side, 0)
	end := min(start+maxSlots, len(steps))
	start = max(end-maxSlots, 0)

	out := make([]ipc.StepSummaryWire, 0, maxSlots+2)
	if start > 0 {
		out = append(out, ipc.StepSummaryWire{Step: -1})
	}
	out = append(out, steps[start:end]...)
	if end < len(steps) {
		out = append(out, ipc.StepSummaryWire{Step: -1})
	}
	return out
}

// CompressThumbnailWindow 当 step list 过长（> 50）时返回围绕 cur 的子切片，
// `side` 个前 + 当前 + `side` 个后，两侧插入 sentinel `…` (Step == -1)。
//
// Story 38-3 review P14：当 cur 不在 all 中（async load race），返回最近 tail
// + 前置 sentinel —— 用户在 step navigation 中几乎总是想看最新 context，而非
// 最旧 50 条历史。
func CompressThumbnailWindow(all []ipc.StepSummaryWire, cur, side int) []ipc.StepSummaryWire {
	curIdx := -1
	for i, s := range all {
		if s.Step == cur {
			curIdx = i
			break
		}
	}
	if curIdx < 0 {
		windowLen := 2*side + 1
		if len(all) <= windowLen {
			return all
		}
		out := make([]ipc.StepSummaryWire, 0, windowLen+1)
		out = append(out, ipc.StepSummaryWire{Step: -1}) // sentinel
		out = append(out, all[len(all)-windowLen:]...)
		return out
	}

	start := max(curIdx-side, 0)
	end := min(curIdx+side+1, len(all))

	out := make([]ipc.StepSummaryWire, 0, end-start+2)
	if start > 0 {
		out = append(out, ipc.StepSummaryWire{Step: -1}) // sentinel
	}
	out = append(out, all[start:end]...)
	if end < len(all) {
		out = append(out, ipc.StepSummaryWire{Step: -1}) // sentinel
	}
	return out
}
