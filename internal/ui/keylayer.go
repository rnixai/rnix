// Package ui — keylayer.go (Story 38.1)
//
// KeyLayer 提供 Dashboard 三层 explicit dispatcher 的基础抽象：
//
//	Layer 0: Global  — 所有 view/pane 都可触发的键 (q, ctrl+c, ?, esc, ...)
//	Layer 1: View    — 按 viewMode 路由（viewDefault / viewExpanded /
//	                   viewStepInspector / viewDebug 各自一个 KeyLayer）
//	Layer 2: Pane    — 按 activePane 路由（paneTree / paneTimeline / ... 各一个）
//
// 调度规则：Dispatcher.Handle 按 0 → 1 → 2 顺序调用对应层的 KeyHandler。
// 第一层返回 consumed=true 时停止，剩余层不再尝试。
//
// 依赖方向：本文件位于 internal/ui，**禁止反向依赖** cmd/rnix。
// KeyHandler 通过 any 类型接受 dashboardModel（cmd/rnix 端注册时类型断言），
// ViewID / PaneID 为 int 别名（cmd/rnix 通过 int(viewMode) / int(paneType) 转入）。
package ui

import (
	"sort"

	tea "charm.land/bubbletea/v2"
)

// ViewID 标识一个 view 模式（cmd/rnix 通过 int(viewMode) 转入）。
type ViewID int

// PaneID 标识一个面板（cmd/rnix 通过 int(paneType) 转入）。
type PaneID int

// KeyContext 是 KeyHandler 接收的不透明模型上下文。cmd/rnix 端传入
// dashboardModel（值类型），KeyHandler 内部做类型断言。
type KeyContext = any

// KeyHandler 处理一次键按下。
//
// 返回值约定：
//   - consumed=true  : 调度链停止，后续层不再尝试
//   - consumed=false : 视为"未匹配"，调度继续；返回的 newCtx 仍会向后传递
//   - newCtx         : KeyHandler 修改后的模型；调用方应用此值（值语义）
//   - cmd            : 要调度给 Bubble Tea 主循环的命令（仅当 consumed=true 才会被使用）
type KeyHandler func(msg tea.KeyPressMsg, ctx KeyContext) (consumed bool, newCtx KeyContext, cmd tea.Cmd)

// KeyDoc 是一个键的文档元数据，用于自动生成 ? help overlay。
type KeyDoc struct {
	Key         string // 显示的键名，如 "q"、"ctrl+c"、"shift+L"
	Description string // 一行说明，如 "Quit"
	ContextNote string // 可选的上下文 hint，如 "Step Inspector (L)"
}

// Mode 是 Mode Strip 的一项，由 ActiveModesFn 返回（如 filter:tool / sort:time）。
type Mode struct {
	Name  string // 例如 "filter"、"sort"、"follow"
	Value string // 例如 "tool"、"time"、"on"
}

// KeyLayer 收集一个层级的全部键位绑定与文档。
//
// 调度顺序（同一层内）：先尝试 Bindings 中精确匹配的 KeyHandler；如无匹配
// 或匹配的 handler 返回 consumed=false，则尝试 Fallback（可选）。
// Fallback 用于"接管整个 pane 的所有键"——例如 paneTimeline 的 Layer 2
// Fallback 委托给 handleTimelineKey，从而把 dispatchPaneKey 的逻辑全部
// 迁移到 Layer 2，便于 dashboard_nav.go 削减至 ≤ 50 行。
type KeyLayer struct {
	Name          string                 // 层名，用于 help overlay 分组标题
	Bindings      map[string]KeyHandler  // key.String() → handler（精确匹配）
	Fallback      KeyHandler             // 任意键的兜底 handler（可选）
	Docs          map[string]KeyDoc      // key.String() → 文档
	ActiveModesFn func(ctx KeyContext) []Mode
}

// ActiveModes 安全调用 ActiveModesFn；nil 时返回空切片。
func (l *KeyLayer) ActiveModes(ctx KeyContext) []Mode {
	if l == nil || l.ActiveModesFn == nil {
		return nil
	}
	return l.ActiveModesFn(ctx)
}

// Dispatcher 由 cmd/rnix 在启动时构造并注册三层 KeyLayer。
type Dispatcher struct {
	Layer0 *KeyLayer              // Global — 所有 view/pane 共用
	Layer1 map[ViewID]*KeyLayer   // View   — 按 viewMode
	Layer2 map[PaneID]*KeyLayer   // Pane   — 按 activePane
}

// NewDispatcher 创建一个空 Dispatcher。
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		Layer1: make(map[ViewID]*KeyLayer),
		Layer2: make(map[PaneID]*KeyLayer),
	}
}

// tryLayer 在一层 KeyLayer 中尝试调度：先 Bindings，再 Fallback。
// 返回值与 Handle 的语义一致：consumed=true 时调用方应停止；newCtx 始终生效。
func tryLayer(l *KeyLayer, msg tea.KeyPressMsg, ctx KeyContext) (KeyContext, tea.Cmd, bool) {
	if l == nil {
		return ctx, nil, false
	}
	key := msg.String()
	if h, ok := l.Bindings[key]; ok && h != nil {
		consumed, newCtx, cmd := h(msg, ctx)
		if consumed {
			return newCtx, cmd, true
		}
		ctx = newCtx
	}
	if l.Fallback != nil {
		consumed, newCtx, cmd := l.Fallback(msg, ctx)
		if consumed {
			return newCtx, cmd, true
		}
		ctx = newCtx
	}
	return ctx, nil, false
}

// Handle 按 Layer 0 → Layer 1[view] → Layer 2[pane] 顺序调用 KeyHandler。
//
// 返回值：
//   - newCtx  : 任一层 handler 修改后的 ctx（即使 consumed=false，handler 的副作用仍生效）
//   - cmd     : 仅当某层消费时返回该层 handler 的 cmd；否则为 nil
//   - consumed: 至少有一层 consumed=true 时为 true
func (d *Dispatcher) Handle(msg tea.KeyPressMsg, view ViewID, pane PaneID, ctx KeyContext) (KeyContext, tea.Cmd, bool) {
	if d == nil {
		return ctx, nil, false
	}

	// Layer 0: Global
	if d.Layer0 != nil {
		newCtx, cmd, consumed := tryLayer(d.Layer0, msg, ctx)
		if consumed {
			return newCtx, cmd, true
		}
		ctx = newCtx
	}

	// Layer 1: View
	if l, ok := d.Layer1[view]; ok && l != nil {
		newCtx, cmd, consumed := tryLayer(l, msg, ctx)
		if consumed {
			return newCtx, cmd, true
		}
		ctx = newCtx
	}

	// Layer 2: Pane
	if l, ok := d.Layer2[pane]; ok && l != nil {
		newCtx, cmd, consumed := tryLayer(l, msg, ctx)
		if consumed {
			return newCtx, cmd, true
		}
		ctx = newCtx
	}

	return ctx, nil, false
}

// HelpFor 按层平铺返回当前 (view, pane) 实际可用的全部 KeyDoc。
// 每层内按键名字典序排列。
func (d *Dispatcher) HelpFor(view ViewID, pane PaneID) []KeyDoc {
	if d == nil {
		return nil
	}
	var out []KeyDoc
	if d.Layer0 != nil {
		out = append(out, sortedDocs(d.Layer0.Docs)...)
	}
	if l, ok := d.Layer1[view]; ok && l != nil {
		out = append(out, sortedDocs(l.Docs)...)
	}
	if l, ok := d.Layer2[pane]; ok && l != nil {
		out = append(out, sortedDocs(l.Docs)...)
	}
	return out
}

// HelpGroup 是分组后的 help 项，用于 help overlay 三段渲染。
type HelpGroup struct {
	Layer string   // "Global" / "View" / "Pane"
	Name  string   // 当前 view/pane 的名字（Layer 1/2 才有）
	Docs  []KeyDoc // 已排序
}

// HelpGroupedFor 按层分组返回 KeyDoc，便于 help overlay 三段渲染。
// 仅返回非空层。
func (d *Dispatcher) HelpGroupedFor(view ViewID, pane PaneID) []HelpGroup {
	if d == nil {
		return nil
	}
	var out []HelpGroup
	if d.Layer0 != nil && len(d.Layer0.Docs) > 0 {
		out = append(out, HelpGroup{Layer: "Global", Name: d.Layer0.Name, Docs: sortedDocs(d.Layer0.Docs)})
	}
	if l, ok := d.Layer1[view]; ok && l != nil && len(l.Docs) > 0 {
		out = append(out, HelpGroup{Layer: "View", Name: l.Name, Docs: sortedDocs(l.Docs)})
	}
	if l, ok := d.Layer2[pane]; ok && l != nil && len(l.Docs) > 0 {
		out = append(out, HelpGroup{Layer: "Pane", Name: l.Name, Docs: sortedDocs(l.Docs)})
	}
	return out
}

// ActiveModesFor 返回 Layer 2 当前 pane 的 ActiveModes，供 Mode Strip 使用。
func (d *Dispatcher) ActiveModesFor(pane PaneID, ctx KeyContext) []Mode {
	if d == nil {
		return nil
	}
	if l, ok := d.Layer2[pane]; ok && l != nil {
		return l.ActiveModes(ctx)
	}
	return nil
}

func sortedDocs(m map[string]KeyDoc) []KeyDoc {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]KeyDoc, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}
