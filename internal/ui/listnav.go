// Package ui — listnav.go (Story 36-5)
//
// HandleListKey 提供统一的"可滚动列表"导航键集合，供所有仪表板面板
// （Tree / Timeline / Heatmap / Intent / Inspector 等）接入，替代分散
// 在各面板 switch-case 中的 up/k · down/j · pgdown · pgup · home/g ·
// end/G 六方向键处理分支。
//
// 设计要点：
//   - vp 非 nil 时操作 viewport 滚动（PageDown/PageUp/ScrollDown/ScrollUp…）。
//   - cursor 非 nil 时同步 cursor 索引（范围 [0, itemCount-1]）。
//   - 两者独立：三种合法用法
//     1. vp + cursor nil  → 仅 viewport 滚动（Inspector 当前 Lens）
//     2. vp nil + cursor  → 仅 cursor 操作（Tree/Timeline/Intent）
//     3. vp + cursor      → 同步操作（未来可选）
//   - 空列表安全：itemCount <= 0 时，所有识别的导航键返回 handled=true 但无副作用。
//   - 通过 ListNavOpts.OnCursorChange 暴露 cursor 变化钩子，调用方在此
//     处理面板特定副作用（如 Tree 的 selectProcess、Timeline 的
//     ensureStepCursorVisible、Intent 的 intentAdjustScroll）。
package ui

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
)

// ListNavOpts carries configuration and callbacks for HandleListKey.
type ListNavOpts struct {
	// PageSize is the page unit used when vp is nil. Default 10 when unset.
	PageSize int
	// OnCursorChange is invoked after cursor moves (even via page jumps).
	OnCursorChange func(newCursor int)
}

// HandleListKey handles a standard list-navigation key. Returns handled=false
// when the key is NOT in the recognised set so the caller can continue with
// panel-specific behaviour.
//
// Recognised keys: j/down, k/up, pgdown/ctrl+f, pgup/ctrl+b, ctrl+d, ctrl+u,
// g/home, G/shift+G/end.
func HandleListKey(key string, vp *viewport.Model, cursor *int, itemCount int, opts ListNavOpts) bool {
	pageSize := pageSizeFor(vp, opts.PageSize)
	// Story 36-5 AC-1 fix: half-page is computed from vp.Height() (not pageSize=h-1),
	// so halfPage = max(vp.Height()/2, 1) per spec. Without vp, fall back to opts.PageSize/2.
	halfPage := halfPageFor(vp, opts.PageSize)

	// Empty-list safety: consume the key but take no action.
	if itemCount <= 0 && cursor != nil {
		switch key {
		case "j", "down", "k", "up",
			"pgdown", "ctrl+f", "pgup", "ctrl+b",
			"ctrl+d", "ctrl+u",
			"g", "home", "G", "shift+G", "end":
			return true
		}
		return false
	}

	prev := -1
	if cursor != nil {
		prev = *cursor
	}

	switch key {
	case "j", "down":
		if cursor != nil {
			*cursor = clampCursor(*cursor+1, itemCount)
		}
		if vp != nil {
			vp.ScrollDown(1)
		}
	case "k", "up":
		if cursor != nil {
			*cursor = clampCursor(*cursor-1, itemCount)
		}
		if vp != nil {
			vp.ScrollUp(1)
		}
	case "pgdown", "ctrl+f":
		if cursor != nil {
			*cursor = clampCursor(*cursor+pageSize, itemCount)
		}
		if vp != nil {
			vp.PageDown()
		}
	case "pgup", "ctrl+b":
		if cursor != nil {
			*cursor = clampCursor(*cursor-pageSize, itemCount)
		}
		if vp != nil {
			vp.PageUp()
		}
	case "ctrl+d":
		if cursor != nil {
			*cursor = clampCursor(*cursor+halfPage, itemCount)
		}
		if vp != nil {
			vp.ScrollDown(halfPage)
		}
	case "ctrl+u":
		if cursor != nil {
			*cursor = clampCursor(*cursor-halfPage, itemCount)
		}
		if vp != nil {
			vp.ScrollUp(halfPage)
		}
	case "g", "home":
		if cursor != nil {
			*cursor = 0
		}
		if vp != nil {
			vp.GotoTop()
		}
	case "G", "shift+G", "end":
		if cursor != nil {
			*cursor = clampCursor(itemCount-1, itemCount)
		}
		if vp != nil {
			vp.GotoBottom()
		}
	default:
		return false
	}

	if cursor != nil && *cursor != prev && opts.OnCursorChange != nil {
		opts.OnCursorChange(*cursor)
	}
	return true
}

// clampCursor returns c bounded to [0, n-1]; when n <= 0 returns 0.
func clampCursor(c, n int) int {
	if n <= 0 {
		return 0
	}
	if c < 0 {
		return 0
	}
	if c >= n {
		return n - 1
	}
	return c
}

func pageSizeFor(vp *viewport.Model, fallback int) int {
	if vp != nil {
		h := vp.Height()
		if h > 1 {
			return h - 1
		}
		return 1
	}
	if fallback > 0 {
		return fallback
	}
	return 10
}

// halfPageFor returns the half-page step. Per Story 36-5 AC-1, this is
// max(vp.Height()/2, 1) when a viewport is supplied, independent of the integer
// page size used by PgUp/PgDn (which is vp.Height()-1).
func halfPageFor(vp *viewport.Model, fallback int) int {
	if vp != nil {
		return max(vp.Height()/2, 1)
	}
	if fallback > 0 {
		return max(fallback/2, 1)
	}
	return 5
}

// FindMatches returns the 0-based line numbers in content that contain query
// (case-insensitive). Empty query returns nil.
func FindMatches(content, query string) []int {
	if query == "" {
		return nil
	}
	q := strings.ToLower(query)
	var out []int
	for i, line := range strings.Split(content, "\n") {
		if strings.Contains(strings.ToLower(line), q) {
			out = append(out, i)
		}
	}
	return out
}

// HighlightMatches returns line with all case-insensitive occurrences of query
// wrapped by before/after. before/after are ANSI reverse-video markers or
// user-supplied sentinel strings; returning plain strings keeps this pure/testable.
func HighlightMatches(line, query, before, after string) string {
	if query == "" {
		return line
	}
	lower := strings.ToLower(line)
	q := strings.ToLower(query)
	var b strings.Builder
	i := 0
	for i < len(line) {
		idx := strings.Index(lower[i:], q)
		if idx < 0 {
			b.WriteString(line[i:])
			break
		}
		b.WriteString(line[i : i+idx])
		b.WriteString(before)
		b.WriteString(line[i+idx : i+idx+len(q)])
		b.WriteString(after)
		i += idx + len(q)
	}
	return b.String()
}
