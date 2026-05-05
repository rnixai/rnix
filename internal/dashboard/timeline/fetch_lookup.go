// Package timeline — fetch_lookup.go (Story 38-5 PR11 Step 4(c))
//
// FindNextDetailToFetch 实现 fetchNextExpandedDetail 的纯查找逻辑（cmd/rnix
// 端 fetchStepDetailCmd IPC 副作用保留 wrapper 内 · 此处仅返回 step number）。
package timeline

// FindNextDetailToFetch 返回下一个需要拉取 detail 的 step number。
//
// 优先级（与 cmd/rnix.fetchNextExpandedDetail 等价）：
//   - Priority 1: 任何 Level >= LevelExpanded 且 StepDetailCache[Step] == nil
//     的 entry（按 StepEntries 顺序首个）;
//   - Priority 2: 当前可视窗口（visStart..visEnd）内 Level < LevelExpanded
//     且 StepDetailCache[Step] == nil 的 entry（用于 ▸ 折叠指示器预拉）;
//   - 都没找到 → 返回 -1.
//
// 边界：
//   - visStart < 0 → clamp 到 0;
//   - visEnd > len(filtered) → clamp（caller 通常已 clamp · 此处再保险）;
//   - filtered 中 ev.StepEntry == nil → 跳过.
//
// 参数：
//   - state.StepEntries / state.StepDetailCache（直接使用）;
//   - filtered: 当前 filtered UnifiedEvents 的 *StepEntry 切片（解开 ev.StepEntry
//     后传入 · 避免 timeline 包反向 import event 包）;
//   - visStart: filtered 中可视窗口起始索引;
//   - visEnd:   filtered 中可视窗口结束索引（不含）.
//
// 返回 -1 表示无 step 需要拉取。
func FindNextDetailToFetch(state TimelineState, filtered []*StepEntry, visStart, visEnd int) int {
	// Priority 1: expanded steps without cached detail
	for _, entry := range state.StepEntries {
		if entry.Level >= LevelExpanded && state.StepDetailCache[entry.Summary.Step] == nil {
			return entry.Summary.Step
		}
	}
	// Priority 2: visible collapsed steps without cached detail
	if visStart < 0 {
		visStart = 0
	}
	if visEnd > len(filtered) {
		visEnd = len(filtered)
	}
	for i := visStart; i < visEnd; i++ {
		entry := filtered[i]
		if entry == nil {
			continue
		}
		if entry.Level < LevelExpanded && state.StepDetailCache[entry.Summary.Step] == nil {
			return entry.Summary.Step
		}
	}
	return -1
}
