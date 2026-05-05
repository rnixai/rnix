// Package event — search.go (Story 38-5 PR11 Step 4(c))
//
// FindMatches 实现 Story 36-5 P-7 模态 search 模式下「收集所有匹配索引」的核心
// 检索逻辑（cmd/rnix.handleTimelineSearchKey enter 分支主体迁出）。
package event

import "strings"

// FindMatches 在 events 列表中收集所有命中 query 的索引（大小写不敏感）。
//
// 命中规则（与 cmd/rnix 历史 byte-for-byte 等价）：
//   - haystack 由 ev.Summary + " " + ev.Detail 拼接（lowercase）;
//   - 若 ev.StepEntry != nil，追加 ev.StepEntry.Summary 的 Action / Summary /
//     ToolPath 三字段（lowercase）到 haystack;
//   - haystack 子串匹配 query.ToLower → 索引计入返回切片.
//
// 边界：
//   - query == "" → 返回 nil（不进行任何匹配 · 与 enter handler 的早返回语义对齐）;
//   - events == nil → 返回 nil;
//   - 无匹配 → 返回 nil（cmd/rnix wrapper 据此显示 "No matches for %q" status msg）.
//
// 性能：O(n × len(haystack))；matches 切片预 nil · 命中时 append 增长。
func FindMatches(events []UnifiedEvent, query string) []int {
	if query == "" {
		return nil
	}
	q := strings.ToLower(query)
	var matches []int
	for i, ev := range events {
		hay := strings.ToLower(ev.Summary + " " + ev.Detail)
		if ev.StepEntry != nil {
			hay += " " + strings.ToLower(ev.StepEntry.Summary.Action+" "+ev.StepEntry.Summary.Summary+" "+ev.StepEntry.Summary.ToolPath)
		}
		if strings.Contains(hay, q) {
			matches = append(matches, i)
		}
	}
	return matches
}
