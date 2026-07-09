// ATDD Story 65.1 — 60.2 折叠吸收 guard（story 裁决 5 / AC #7）。
//
// 65.1 的 kernel 侧将在 thinking 块结束时补发 subtype="aggregate" 的
// DriverThinking 聚合事件。本文件锁定 60.2 既有契约对该新 subtype 的吸收行为：
//   - BuildThinkingAggGroups: subtype 非 started 的 DriverThinking 一律吸收进
//     当前块（不断块）→ started + delta×N + aggregate = 单折叠块；
//   - ReconstructThinkingText: 只拼 subtype=="delta" → aggregate 全文不重复入正文;
//   - DeltaCount/TotalBytes: 只计 delta → aggregate 不污染计数。
//
// GREEN-GUARD（不 skip）：按 60.2 现行实现此测试今日即应通过——它是 65.1 落地后
// 防 dashboard 回归的防护网，不是 65.3 的 input_delta 折叠功能。
package event

import (
	"strings"
	"testing"
)

func TestATDD_65_1_AggregateEventAbsorbedByThinkingGroup(t *testing.T) {
	events := []UnifiedEvent{
		mkThinkEv(t, "started", "started", 1000),
		mkThinkEv(t, "delta", "Hel", 1010),
		mkThinkEv(t, "delta", "lo w", 1020),
		mkThinkEv(t, "delta", "orld", 1030),
		// 65.1 kernel 补发的聚合事件：携全文，须被吸收且不重复正文/不污染计数。
		mkThinkEv(t, "aggregate", "Hello world", 1040),
		mkNonThinkEv("DriverMessage"),
	}

	groups := BuildThinkingAggGroups(events)
	if len(groups) != 1 {
		t.Fatalf("组数 = %d, want 1（aggregate 被吸收，不断块）", len(groups))
	}
	g := groups[0]
	if g.DeltaCount != 3 {
		t.Errorf("DeltaCount = %d, want 3（aggregate 不计入）", g.DeltaCount)
	}
	if want := len("Hel") + len("lo w") + len("orld"); g.TotalBytes != want {
		t.Errorf("TotalBytes = %d, want %d（aggregate 不计入）", g.TotalBytes, want)
	}

	text := ReconstructThinkingText(events, g)
	if text != "Hello world" {
		t.Errorf("ReconstructThinkingText = %q, want %q（只拼 delta）", text, "Hello world")
	}
	if strings.Count(text, "Hello world") != 1 {
		t.Errorf("正文重复：aggregate 全文被二次拼接: %q", text)
	}
}
