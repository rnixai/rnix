// Package inspector — search.go (Story 38-5 PR11 Step 4(a-2))
//
// Inspector 搜索 helpers，迁出自 cmd/rnix/dashboard_inspector.go：
//
//   - IsBackScrollKey         （原 isBackScrollKey · Follow Live 触发判定）
//   - FindInspectorMatchesByPos （原 findInspectorMatchesByPos · 词级搜索）
//   - ApplyWordLevelHighlight （原 applyWordLevelHighlight · 词级高亮）
//
// 全部纯函数（不依赖 dashboardModel / InspectorState），仅消费 string 输入和
// SearchMatchPos 类型（已在 state.go 中定义为公开类型）。Story 36-5/36-6/38-3
// AC#8 落地的搜索行为完整保留。
package inspector

import (
	"regexp"
	"slices"

	"github.com/charmbracelet/lipgloss"
)

// IsBackScrollKey 判断按键是否触发 viewport 向后（向上 / 早期内容）滚动，从而
// 触发 Story 36-6 AC-13 的 Follow Live 自动关闭。
//
// 列表与原 cmd/rnix.isBackScrollKey 等价：k / up / pgup / pageup / ctrl+u /
// ctrl+b / g。其它键（包括 j / down / pgdn / G）返回 false。
func IsBackScrollKey(key string) bool {
	switch key {
	case "k", "up", "pgup", "pageup", "ctrl+u", "ctrl+b", "g":
		return true
	}
	return false
}

// FindInspectorMatchesByPos 在 content 中查找 query 全部子串匹配（大小写不敏感），
// 返回每条匹配的精确位置（line / byte 范围）。Story 38-3 AC#8 词级搜索。
//
// 行为：
//   - 空 query → 返回 nil（不报错）
//   - 用 (?i) 大小写不敏感正则匹配，QuoteMeta 转义元字符；正则编译失败 → 返回 nil
//   - 按行扫描，每行内 FindAllStringIndex 报告所有匹配；ByteStart/End 是 content 全局偏移
//   - LineIdx 从 0 开始递增（按 '\n' 分行）
//
// 性能：O(n) 单次扫描；不分配额外缓冲区（regexp 内部由 stdlib 处理）。
func FindInspectorMatchesByPos(content, query string) []SearchMatchPos {
	if query == "" {
		return nil
	}
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(query))
	if err != nil {
		return nil
	}
	var out []SearchMatchPos
	lineStart := 0
	lineIdx := 0
	for i := 0; i <= len(content); i++ {
		if i == len(content) || (i < len(content) && content[i] == '\n') {
			line := content[lineStart:i]
			for _, m := range re.FindAllStringIndex(line, -1) {
				out = append(out, SearchMatchPos{
					LineIdx:   lineIdx,
					ByteStart: lineStart + m[0],
					ByteEnd:   lineStart + m[1],
				})
			}
			lineStart = i + 1
			lineIdx++
		}
	}
	return out
}

// ApplyWordLevelHighlight 把 positions 列表中的每条 hit 包成对应的 reverse-video
// 样式：与 searchMatches[matchIdx] 同行 → curStyle；其他 → otherStyle。Story 38-3 AC#8。
//
// 实现要点：
//   - 倒序处理（i := len(positions)-1; i >= 0; i--），让早插入的样式不影响后续偏移
//   - 边界 case：ByteStart < 0 / ByteEnd > len(content) / ByteStart >= ByteEnd → 跳过
//   - 输入 positions 必须按 ascending byteStart 排序（FindInspectorMatchesByPos 已保证）
//
// matchIdx 越界（< 0 或 ≥ len(searchMatches)） → currentLine = -1 → 全部用 otherStyle。
func ApplyWordLevelHighlight(content string, positions []SearchMatchPos, searchMatches []int, matchIdx int, curStyle, otherStyle lipgloss.Style) string {
	currentLine := -1
	if matchIdx >= 0 && matchIdx < len(searchMatches) {
		currentLine = searchMatches[matchIdx]
	}
	out := []byte(content)
	for i := range slices.Backward(positions) {
		p := positions[i]
		if p.ByteStart < 0 || p.ByteEnd > len(out) || p.ByteStart >= p.ByteEnd {
			continue
		}
		matched := string(out[p.ByteStart:p.ByteEnd])
		var styled string
		if p.LineIdx == currentLine {
			styled = curStyle.Render(matched)
		} else {
			styled = otherStyle.Render(matched)
		}
		out = append(out[:p.ByteStart], append([]byte(styled), out[p.ByteEnd:]...)...)
	}
	return string(out)
}
