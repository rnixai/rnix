// Package heatmap — types.go (Story 38-5 PR3 Step 1)
//
// 定义 Heatmap pane 专用的可视化类型：Segment / SegmentKind / ActivityLevel。
//
// 边界说明：
//   - spec § Project Structure Notes 列出的 segmentKind/activityLevel/heatmapSegment 标注「保留 cmd/rnix」，
//     但实际只有 Heatmap pane 使用这些类型，迁入本包后通过 cmd/rnix 端 alias 保持兼容（与 PR2 处理 flatRow
//     的方式一致：spec 字面与实际偏差时按"必要扩边界"原则迁入子包，cmd/rnix 端保留 alias 让旧测试 grep 字符串
//     兼容）。
//   - segmentKind 数值常量与 cmd/rnix 端原定义保持一致（iota 顺序：System=0, Skill=1, Tool=2, User=3,
//     Assistant=4, Leaked=5），cmd/rnix alias 不变更值语义。
package heatmap

// SegmentKind 是 Heatmap 片段的内容分类（System/Skill/Tool/User/Assistant/Leaked 6 类）。
type SegmentKind int

const (
	SegSystem SegmentKind = iota
	SegSkill
	SegTool
	SegUser
	SegAssistant
	SegLeaked
)

// ActivityLevel 是 Heatmap 片段的活跃度评估（Active/Warm/Cold/Leaked 4 档）。
type ActivityLevel int

const (
	ActActive ActivityLevel = iota
	ActWarm
	ActCold
	ActLeaked
)

// Segment 是 Heatmap pane 渲染的单个可视化片段（来自 BuildSegments 的输出）。
//
// 字段说明：
//   - Label/Tokens/Pct: 文字标签 + token 数 + 百分比（用于显示与排序）；
//   - Kind/Activity:    分类与活跃度（决定 segmentColor 配色）；
//   - Summary:          展开模式下显示的简介文本（可空）。
type Segment struct {
	Label    string
	Tokens   int
	Pct      float64
	Kind     SegmentKind
	Activity ActivityLevel
	Summary  string
}
