// Package timeline — searchable.go (Story 38-5 PR4 Step 4)
//
// TimelineModel 实现 plugin.Searchable interface 让 SearchPlugin 通过 Apply(target, query)
// 接入 Timeline 跨 pane 搜索（spec § 04 风险 3 缓解）。
//
// 关键点：
//   - SearchableLines 返回当前 StepEntries 中每个 step 的「纯文本行」（lipgloss strip 后）；
//   - 行格式："step%d  %s  %s"（Step + Action + Summary · 与 cmd/rnix 端 Timeline 渲染默认行
//     一致 · ToolPath 优先于 Action）；
//   - 不应用 StepFilter 过滤（搜索默认覆盖所有 step · n/N 跨 filter 边界跳转 ·
//     与 inspector 现有行为对齐）；
//   - System events（compact/budget/spawn/exit/stall/immune）暂不纳入 SearchableLines
//     （这些不属于 step list · 在 cmd/rnix 端通过 unifiedEvents 渲染 · 后续 PR 可扩展接入）。
package timeline

import (
	"fmt"
	"strings"
)

// SearchableLines 实现 plugin.Searchable interface。
//
// 返回 TimelineState.StepEntries 中每个 step 的纯文本行（按 StepEntries 顺序排列）。
//
// 性能上界：O(n)，n = len(StepEntries)（典型 ≤ 500 步 · 一次性扫描 + 字符串拼接）；
// 单次返回内存：~64 byte/行 + 行数组（≤ 50KB · 远低于 spec § Searchable 1500 行上限）。
//
// nil 安全：receiver 为 nil 或 StepEntries 为空时返回 nil。
//
// 行格式（与 cmd/rnix 端 formatDefaultLine 保持兼容 · 大小写敏感 · SearchPlugin.Apply 内部
// 转小写后比较）：
//
//	step%d  <action-or-toolpath>  <summary>
//
// 例：
//
//	step1  /dev/fs  read main.go
//	step2  tool_call  unknown
//	step3  /dev/shell  ls -la
//
// 包边界约束：本方法不能 import cmd/rnix（spec § Risk 3 SearchPlugin 解耦）；只读 TimelineState
// 字段，与 KeyLayer.ActiveModesFn 同模式。
func (m *TimelineModel) SearchableLines() []string {
	if m == nil {
		return nil
	}
	entries := m.state.StepEntries
	if len(entries) == 0 {
		return nil
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		s := entry.Summary
		action := s.Action
		if s.ToolPath != "" {
			action = s.ToolPath
		}
		summary := s.Summary
		// 格式与 cmd/rnix 端 formatDefaultLine fallback 路径一致（不依赖 detail 缓存）。
		// 注意：实际渲染中可能用 detail.Action / detail.Summary 替代，但 SearchableLines
		// 只看 StepSummaryWire 基础字段，避免依赖 cmd/rnix 端 cache 状态。
		line := fmt.Sprintf("step%d  %s  %s", s.Step, action, strings.TrimSpace(summary))
		lines = append(lines, line)
	}
	return lines
}
