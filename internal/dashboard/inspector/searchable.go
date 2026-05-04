// Package inspector — searchable.go (Story 38-5 PR10 Step 4)
//
// InspectorModel 实现 plugin.Searchable interface 让 SearchPlugin 通过 Apply(target, query)
// 接入 Inspector 跨 lens 搜索（spec § 04 风险 3 缓解 · 与 PR4 TimelineModel 同模式）。
//
// 关键点：
//   - SearchableLines 返回当前激活 lens 的 Contents 行（split by "\n"）；
//   - 跨 lens 切换会让搜索结果失效（cmd/rnix 端 switchInspectorLens 会重新计算）；
//   - 不应用 diff 模式过滤（搜索默认覆盖完整 lens 内容）；
//   - 与 38-3 AC#8 byte-position search（SearchPos 字段）协同：SearchableLines 提供行级
//     索引粒度，SearchPos 提供 byte-level 精确高亮（同一搜索的两个粒度）；
//   - 编译期断言 InspectorModel 满足 plugin.Searchable interface（在 model.go）。
package inspector

import (
	"strings"
)

// SearchableLines 实现 plugin.Searchable interface。
//
// 返回当前激活 lens（Lens 字段）的 Contents 行。Contents 是 5 个 lens 的预渲染内容缓存（PR1
// state.go 注释），每个 lens 独立拥有完整文本（含 lipgloss styled string）。
//
// 性能上界：O(n)，n = lens content 行数（典型 ≤ 500 行 · 5-lens 最大场景 · spec § Searchable
// 1500 行上限内）。
//
// nil 安全：receiver 为 nil 或 Lens 越界时返回 nil。
//
// 行级 vs byte-level 协同（38-3 AC#7/AC#8）：
//   - SearchableLines 返回行级 lines，SearchPlugin.Apply 计算行索引匹配；
//   - SearchPos 字段（state.go）独立维护 byte-level 命中（用于词级高亮）；
//   - cmd/rnix 端 rebuildInspectorContents 在搜索激活时同时刷新两者。
//
// 包边界约束：本方法不能 import cmd/rnix（spec § Risk 3 SearchPlugin 解耦）；只读
// InspectorState.Contents 字段，与 KeyLayer.ActiveModesFn 同模式。
func (m *InspectorModel) SearchableLines() []string {
	if m == nil {
		return nil
	}
	idx := int(m.state.Lens)
	if idx < 0 || idx >= LensCount {
		return nil
	}
	content := m.state.Contents[idx]
	if content == "" {
		return nil
	}
	// strings.Split 在内容尾部含 "\n" 时会产生多余空行，但与 cmd/rnix 端
	// inspectorContents 行索引保持一致（SearchPlugin.Apply 行索引 = View 行号 ·
	// spec § 04 plugin/search.go 注释「返回 slice 内容必须与 View() 输出顺序一致」）。
	return strings.Split(content, "\n")
}
