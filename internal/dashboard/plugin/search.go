// Package plugin — Dashboard 横切关注点的 Plugin 实现（Story 38-5 PR1）。
//
// Plugin 模式用于收纳「跨多个 PaneModel/OverlayModel 的共享逻辑」，避免 god struct 复发：
//   - SearchPlugin: Inspector + Timeline 的 / search 状态（曾散落在 dashboardModel 7 字段）；
//   - HealthPlugin: errorCount/warnCount/heartbeat（曾散落 3 字段）；
//   - HelpPlugin:   自动从 KeyLayers 派生 help overlay 内容。
//
// 解耦原则（spec § 04 风险 3）：
//   - Plugin 通过 interface（如 Searchable）与子 Model 协作；
//   - **严禁** Plugin 内部 import "github.com/rnixai/rnix/cmd/rnix" — go module 边界已防止反向依赖；
//   - Plugin 字段全部 plugin 包内私有或显式公开（无第三方包能直接 mutate）。
package plugin

import (
	"strings"
	"time"
)

// Searchable 是 PaneModel/OverlayModel 接入 SearchPlugin 的契约。
//
// 实现要求：
//   - SearchableLines() 返回当前 pane 中可被 / search 命中的「纯文本行」（lipgloss strip 后）；
//   - 返回 slice 内容必须与 View() 输出顺序一致（搜索匹配的索引 = View 行号）；
//   - 调用频率：每次用户输入 / 命令、按下 n / N、或 SearchPlugin.Apply 被显式调用时；
//   - 性能上界：单次返回 ≤ 1500 行（Inspector 5-lens 最大场景），允许 O(n) 内存分配。
//
// nil 安全：实现方在 receiver 为 nil 时返回 nil（SearchPlugin.Apply 已处理 nil 情况）。
type Searchable interface {
	SearchableLines() []string
}

// SearchPlugin 收纳 Dashboard 跨 pane 搜索状态（曾在 dashboardModel 占 7 字段）。
//
// 状态机：
//   - Mode=false: 未在搜索模式（按 / 进入）；
//   - Mode=true:  搜索模式激活，Query 持续累积，Matches 实时刷新；
//   - NoMatchExpireAt: 当前 Query 无匹配时的 TTL 提示过期时刻（spec § 02 落地）。
//
// 生命周期：SearchPlugin 由 App Model 持有，在多个 Searchable 之间复用（一次只对一个目标活跃）。
type SearchPlugin struct {
	Mode            bool
	Query           string
	Matches         []int
	MatchIdx        int
	Reverse         bool
	CrossLens       bool
	NoMatchExpireAt time.Time
}

// Apply 在 target 上执行当前 Query 的搜索，返回匹配的行索引列表。
//
// 行为约定：
//   - target 为 nil 或 SearchableLines 返回 nil → 返回 nil；
//   - Query 为空字符串 → 返回 nil（视为未激活搜索）；
//   - 不修改 SearchPlugin 自身状态（caller 负责把结果 assign 给 p.Matches）；
//   - 大小写不敏感（与 dashboard inspector 现有行为一致）。
func (p *SearchPlugin) Apply(target Searchable, query string) []int {
	if p == nil || target == nil || query == "" {
		return nil
	}
	lines := target.SearchableLines()
	if len(lines) == 0 {
		return nil
	}
	q := strings.ToLower(query)
	matches := make([]int, 0, 8)
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), q) {
			matches = append(matches, i)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	return matches
}

// EnterSearchMode 把 SearchPlugin 切入 search 输入态，清空 Query 与 Reverse
// 标志（Story 36-5 / forward / Story 36-6 ? backward search 入口的共享逻辑）。
//
// 行为：
//   - Mode=true / Query="" / Reverse=reverse;
//   - 不清空 Matches / MatchIdx（caller 在 enter 时显式重算 · 与历史等价）.
//
// nil 安全：receiver 为 nil 时 noop。
func (p *SearchPlugin) EnterSearchMode(reverse bool) {
	if p == nil {
		return
	}
	p.Mode = true
	p.Query = ""
	p.Reverse = reverse
}

// JumpMatch 在 Matches 列表中循环跳转 dir 步（dir=+1 下一个 / -1 上一个）。
//
// 行为契约（与 cmd/rnix.inspectorJumpSearchMatch / timeline n/N 共享语义）：
//   - 若 Matches 为空 → 返回 false（caller 不应执行后续滚动副作用）;
//   - Reverse=true → dir 翻转（Story 36-6 AC-6 backward 语义）;
//   - MatchIdx 模 n 环绕（双向安全 · ((idx+dir)%n + n) % n）;
//   - 返回 true 表示 MatchIdx 已更新.
//
// nil 安全：receiver 为 nil → 返回 false。
func (p *SearchPlugin) JumpMatch(dir int) bool {
	if p == nil || len(p.Matches) == 0 {
		return false
	}
	if p.Reverse {
		dir = -dir
	}
	n := len(p.Matches)
	p.MatchIdx = ((p.MatchIdx+dir)%n + n) % n
	return true
}

// Reset 清空 SearchPlugin 全部状态字段（Story 36-5 P-1 · 跨 pane / mode 切换
// 时调用 · 与 cmd/rnix.clearSearchState 等价 · 不含 inspector-private 字段）。
//
// 字段重置：Mode=false / Query="" / Matches=nil / MatchIdx=0 / Reverse=false /
// CrossLens=false / NoMatchExpireAt=time.Time{}（零值）。
//
// nil 安全：receiver 为 nil → noop。
func (p *SearchPlugin) Reset() {
	if p == nil {
		return
	}
	p.Mode = false
	p.Query = ""
	p.Matches = nil
	p.MatchIdx = 0
	p.Reverse = false
	p.CrossLens = false
	p.NoMatchExpireAt = time.Time{}
}

// HandleInputKey 处理 search 输入态的非-enter 按键（Story 38-5 PR11 Step 4(c)
// timeline / inspector 共享通用模式）。
//
// 处理的按键：
//   - "esc"：退出 search 模式 + 清空 Query;
//   - "backspace"：删除 Query 最后一个 rune;
//   - " " / "space"：追加空格到 Query;
//   - 单字符按键：追加该字符到 Query;
//   - 其他多字符按键（如 "enter" / "up" / "f1" 等）：noop · 返回 handled=false
//     让 cmd/rnix wrapper 决定后续处理（如 "enter" 触发 Apply）.
//
// 返回：
//   - handled: 当前按键是否被本方法处理（false 时 caller 须自行处理 · 例如 "enter"）.
//
// nil 安全：receiver 为 nil 时返回 false。
func (p *SearchPlugin) HandleInputKey(key string) (handled bool) {
	if p == nil {
		return false
	}
	switch key {
	case "esc":
		p.Mode = false
		p.Query = ""
		return true
	case "backspace":
		runes := []rune(p.Query)
		if len(runes) > 0 {
			p.Query = string(runes[:len(runes)-1])
		}
		return true
	case " ", "space":
		p.Query += " "
		return true
	default:
		if len([]rune(key)) == 1 {
			p.Query += key
			return true
		}
		return false
	}
}
