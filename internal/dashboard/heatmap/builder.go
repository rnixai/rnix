// Package heatmap — builder.go (Story 38-5 PR3 Step 1)
//
// 构造 Heatmap pane 的可视化 Segment 列表。
//
// 主入口：BuildSegments(profile) — 把 debug.CtxProfileResult.TopConsumers 聚合归并为
// 按分类（System/Skill/Tool/User/Assistant/Leaked）合并的 Segment 列表，小于 3% 的合并到 Other 桶。
//
// 内部 helper（包私有）：
//   - mapConsumerKind: TopConsumer.Kind 字符串 → SegmentKind 枚举；
//   - estimateActivity: 根据 Classification.Active.Pct vs Cold.Pct + Rank 推断 ActivityLevel。
//
// 公开 helper：
//   - SegmentKindLabel / ActivityLabel: 枚举 → 显示文本（render.go + cmd/rnix 端共用）；
//   - SegmentColor: (kind, activity) → hex color string（lipgloss 渲染用）；
//   - Dim: 颜色暗化（cold 状态用）。
package heatmap

import (
	"sort"
	"strings"

	"github.com/rnixai/rnix/debug"
)

// SegmentKindLabel 返回 SegmentKind 对应的显示文本（"System Prompt" / "Skill" / "Tool Results" / 等）。
func SegmentKindLabel(kind SegmentKind) string {
	switch kind {
	case SegSystem:
		return "System Prompt"
	case SegSkill:
		return "Skill"
	case SegTool:
		return "Tool Results"
	case SegUser:
		return "User Messages"
	case SegAssistant:
		return "Assistant"
	case SegLeaked:
		return "Leaked"
	default:
		return "Unknown"
	}
}

// ActivityLabel 返回 ActivityLevel 对应的显示文本（"Active" / "Warm" / "Cold" / "Leaked"）。
func ActivityLabel(a ActivityLevel) string {
	switch a {
	case ActActive:
		return "Active"
	case ActWarm:
		return "Warm"
	case ActCold:
		return "Cold"
	case ActLeaked:
		return "Leaked"
	default:
		return "Unknown"
	}
}

// MapConsumerKind 把 TopConsumer.Kind 字符串映射为 SegmentKind 枚举。
//
// 公开供 cmd/rnix 端 thin wrapper + dashboard_test.go::17.3-UNIT-012 / CR-FIX-004 测试使用。
func MapConsumerKind(kind string) SegmentKind {
	switch {
	case kind == "system_prompt":
		return SegSystem
	case kind == "user":
		return SegUser
	case kind == "assistant":
		return SegAssistant
	case strings.HasPrefix(kind, "tool:"):
		return SegTool
	case kind == "skill" || strings.HasPrefix(kind, "skill:"):
		return SegSkill
	default:
		return SegAssistant
	}
}

// mapConsumerKind — 包内便捷 alias 调用 MapConsumerKind。
func mapConsumerKind(kind string) SegmentKind {
	return MapConsumerKind(kind)
}

// Dim 返回输入 hex 颜色的暗化版本（cold 状态用 · 6 个预设颜色 + 默认 fallback）。
//
// 注意：colorIPC（"#9B59B6"）的暗化版本在 cmd/rnix 端原 dim 函数中也是 "#6A3D7E"，本函数兼容。
func Dim(hexColor string) string {
	switch hexColor {
	case "#5B9BD5":
		return "#3A6B94"
	case "#9B59B6":
		return "#6A3D7E"
	case "#6BCB77":
		return "#4A8C52"
	case "#FFD93D":
		return "#B3982B"
	case "#888888":
		return "#5E5E5E"
	default:
		return "#666666"
	}
}

// SegmentColor 返回 (kind, activity) 对应的 hex 颜色字符串。
//
// 调用方：render.go::Render 计算每个 segment 的渲染颜色 + cmd/rnix 端 dashboard_heatmap.go thin wrapper。
//
// 颜色映射：
//   - Leaked (任意 kind 或 actLeaked): "#FF6B6B"（warning red）
//   - System: "#5B9BD5"（蓝色 system_prompt）
//   - Skill:  "#9B59B6"（colorIPC 紫色 · 与 cmd/rnix colorIPC 常量一致）
//   - Tool:   "#6BCB77"（绿色 tool results）
//   - User:   "#FFD93D"（黄色 user messages）
//   - Assistant: "#888888"（中灰）
//   - Cold 状态: 调 Dim 暗化。
func SegmentColor(kind SegmentKind, activity ActivityLevel) string {
	if activity == ActLeaked || kind == SegLeaked {
		return "#FF6B6B"
	}
	var base string
	switch kind {
	case SegSystem:
		base = "#5B9BD5"
	case SegSkill:
		base = "#9B59B6" // 与 cmd/rnix colorIPC 一致
	case SegTool:
		base = "#6BCB77"
	case SegUser:
		base = "#FFD93D"
	case SegAssistant:
		base = "#888888"
	default:
		base = "#888888"
	}
	if activity == ActCold {
		return Dim(base)
	}
	return base
}

// estimateActivity 根据 Classification.Active.Pct vs Cold.Pct + Rank 推断 ActivityLevel。
//
// 规则：
//   - System 永远 Active；Leaked 永远 Leaked；
//   - Active.Pct > Cold.Pct: Rank ≤ 2 → Active；其他 → Warm；
//   - Cold.Pct > Active.Pct: Cold；
//   - 否则: Warm。
func estimateActivity(profile *debug.CtxProfileResult, kind SegmentKind, rank int) ActivityLevel {
	if kind == SegSystem {
		return ActActive
	}
	if kind == SegLeaked {
		return ActLeaked
	}
	cl := profile.Classification
	if cl.Active.Pct > cl.Cold.Pct {
		if rank <= 2 {
			return ActActive
		}
		return ActWarm
	}
	if cl.Cold.Pct > cl.Active.Pct {
		return ActCold
	}
	return ActWarm
}

// BuildSegments 把 debug.CtxProfileResult.TopConsumers 聚合归并为按分类合并的 Segment 列表。
//
// 算法：
//  1. 对每个 TopConsumer 按 Kind 归并到 kindBucket（保留 token/pct 之和 + 最佳 rank 对应的 activity）；
//  2. pct < 3% 的桶合并到 Other；
//  3. Leaked 单独处理（< 3% 也合并到 Other，否则独立 segment）；
//  4. 按 Tokens 降序排序。
//
// 返回 nil 当 profile 为 nil 或 TopConsumers 为空。
//
// 调用方：cmd/rnix dashboard.go::Update（heatmapProfileMsg + replay）+ render.go::Render（PR3 Step 3）。
func BuildSegments(profile *debug.CtxProfileResult) []Segment {
	if profile == nil || len(profile.TopConsumers) == 0 {
		return nil
	}

	type kindBucket struct {
		tokens   int
		pct      float64
		kind     SegmentKind
		activity ActivityLevel
		bestRank int
	}
	merged := make(map[SegmentKind]*kindBucket)

	for _, c := range profile.TopConsumers {
		kind := mapConsumerKind(c.Kind)
		activity := estimateActivity(profile, kind, c.Rank)
		if b, ok := merged[kind]; ok {
			b.tokens += c.Tokens
			b.pct += c.Pct
			if c.Rank < b.bestRank {
				b.bestRank = c.Rank
				b.activity = activity
			}
		} else {
			merged[kind] = &kindBucket{
				tokens: c.Tokens, pct: c.Pct,
				kind: kind, activity: activity, bestRank: c.Rank,
			}
		}
	}

	var segments []Segment
	var otherTokens int
	var otherPct float64

	for _, b := range merged {
		if b.pct < 3.0 {
			otherTokens += b.tokens
			otherPct += b.pct
			continue
		}
		segments = append(segments, Segment{
			Label:    SegmentKindLabel(b.kind),
			Tokens:   b.tokens,
			Pct:      b.pct,
			Kind:     b.kind,
			Activity: b.activity,
		})
	}

	if profile.Classification.Leaked.Tokens > 0 {
		if profile.Classification.Leaked.Pct < 3.0 {
			otherTokens += profile.Classification.Leaked.Tokens
			otherPct += profile.Classification.Leaked.Pct
		} else {
			segments = append(segments, Segment{
				Label:    "Leaked",
				Tokens:   profile.Classification.Leaked.Tokens,
				Pct:      profile.Classification.Leaked.Pct,
				Kind:     SegLeaked,
				Activity: ActLeaked,
			})
		}
	}

	if otherTokens > 0 {
		segments = append(segments, Segment{
			Label:    "Other",
			Tokens:   otherTokens,
			Pct:      otherPct,
			Kind:     SegAssistant,
			Activity: ActCold,
		})
	}

	// Sort by tokens desc, with SegmentKind asc as a deterministic tie-breaker.
	// Without the tie-break, equal-token segments (e.g. a 400-token system prompt
	// vs 400 tokens of merged tool calls) keep the randomized order of the
	// kindBucket map iteration, which made TestBuildSegments_BasicAggregation
	// flaky (~5% of runs the tools segment sorted ahead of System).
	sort.Slice(segments, func(i, j int) bool {
		if segments[i].Tokens != segments[j].Tokens {
			return segments[i].Tokens > segments[j].Tokens
		}
		return segments[i].Kind < segments[j].Kind
	})

	return segments
}
