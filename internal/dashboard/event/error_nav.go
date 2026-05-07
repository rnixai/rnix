// Package event — error_nav.go (Story 38-5 PR11 Step 4(c))
//
// FindNextErrorIdx / FindPrevErrorIdx 实现 Story 36-5 AC-5 「n/N 跨 timeline
// 跳转下一个/上一个错误」语义的核心 lookup（cmd/rnix.handleTimelineKey
// 的 n/N 分支主体迁出 · 不含 StepCursor 写入与 ensureStepCursorVisible 副作用）。
package event

// IsError 判定 UnifiedEvent 是否被视为「错误」。
//
// 判定规则（与 cmd/rnix.handleTimelineKey n/N 分支等价）：
//   - StepEntry != nil → ev.StepEntry.Summary.HasError 为真;
//   - StepEntry == nil → ev.Severity >= SevError （system event 严重度门槛）.
func IsError(ev UnifiedEvent) bool {
	if ev.StepEntry != nil {
		return ev.StepEntry.Summary.HasError
	}
	return ev.Severity >= SevError
}

// FindNextErrorIdx 从 events[startIdx+1] 开始向后扫描，返回首个错误事件的索引。
//
// 边界：startIdx 越界（< -1 或 >= len(events)）→ 返回 (-1, false)；
// 无匹配返回 (-1, false)。startIdx 入参可以是 -1（从首位开始扫描）。
func FindNextErrorIdx(events []UnifiedEvent, startIdx int) (idx int, found bool) {
	for i := startIdx + 1; i < len(events); i++ {
		if IsError(events[i]) {
			return i, true
		}
	}
	return -1, false
}

// FindPrevErrorIdx 从 events[startIdx-1] 开始向前扫描，返回首个错误事件的索引。
//
// 边界：startIdx <= 0 → 返回 (-1, false)（无前位可看）；startIdx > len(events)
// → clamp 到 len(events)（防御越界 access · cmd/rnix 入参可能源自陈旧 cursor）；
// 无匹配返回 (-1, false)。
func FindPrevErrorIdx(events []UnifiedEvent, startIdx int) (idx int, found bool) {
	if startIdx > len(events) {
		startIdx = len(events)
	}
	for i := startIdx - 1; i >= 0; i-- {
		if IsError(events[i]) {
			return i, true
		}
	}
	return -1, false
}
