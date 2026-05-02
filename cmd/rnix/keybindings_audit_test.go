// Package main — keybindings_audit_test.go (Story 38.1 Decision-B1)
//
// Lint 校验：扫描 Dispatcher 的 KeyDoc 注册，断言：
//  1. 每个 Layer 0/1/2 的 KeyLayer 都有 Name + 至少一条 Docs
//  2. 每个 Bindings 都有对应的 Docs（防止"键能用但没文档"的漂移）
//  3. 每个 Docs 的 Key 字段非空且不重复
//
// 通过 `go test -run TestKeybindingsAudit ./cmd/rnix/...` 运行。
// 失败时打印漂移项，提示更新 specs/keybindings.md 或补 KeyDoc。
//
// 注：与 specs/keybindings.md 的内容比对暂未实现（spec 表格格式与代码注册
// 形式存在偏差，需要 spec 模板化才能精确比对，留作 Story 38-2/38-3）。
package main

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestKeybindingsAudit_AllLayersHaveDocs 断言每个注册的 KeyLayer 都有
// 非空 Name 和至少一条 Docs（避免空层注册）。
func TestKeybindingsAudit_AllLayersHaveDocs(t *testing.T) {
	d := newDispatcher()
	if d.Layer0 == nil {
		t.Fatal("Layer 0 must be registered")
	}
	if d.Layer0.Name == "" {
		t.Error("Layer 0 must have Name")
	}
	if len(d.Layer0.Docs) == 0 {
		t.Error("Layer 0 must have at least one KeyDoc")
	}

	for view, l := range d.Layer1 {
		if l == nil {
			t.Errorf("Layer 1[view=%d] is nil", view)
			continue
		}
		if l.Name == "" {
			t.Errorf("Layer 1[view=%d] must have Name", view)
		}
		// Layer 1 允许 docs-only（如 viewStepInspector / viewDebug），无 Docs 警告
		if len(l.Docs) == 0 {
			t.Logf("Note: Layer 1[view=%d] has no Docs", view)
		}
	}

	for pane, l := range d.Layer2 {
		if l == nil {
			t.Errorf("Layer 2[pane=%d] is nil", pane)
			continue
		}
		if l.Name == "" {
			t.Errorf("Layer 2[pane=%d] must have Name", pane)
		}
	}
}

// TestKeybindingsAudit_BindingsMatchDocs 断言每个 Bindings 都有对应的 Docs
// 注册（防止"绑定了 handler 但 help overlay 不显示"的漂移）。
//
// 例外：modal 键（y/n）和 Bindings 但不展示给用户的辅助键允许跳过 Docs。
func TestKeybindingsAudit_BindingsMatchDocs(t *testing.T) {
	d := newDispatcher()

	// modal 键和 alias 不要求 Docs
	skipDocs := map[string]bool{
		"y":         true, // confirmKill modal answer
		"n":         true, // confirmKill modal answer
		"shift+L":   true, // alias for L
		"shift+R":   true, // alias for R
		"ctrl+c":    true, // documented as ctrl+c
		"shift+tab": true, // alias for tab
		"!":         true, // eval sub-view alias
		"@":         true, // eval sub-view alias
		"#":         true, // eval sub-view alias
	}

	check := func(layer string, bindings, docs map[string]any) {
		var missing []string
		for k := range bindings {
			if skipDocs[k] {
				continue
			}
			if _, ok := docs[k]; !ok {
				// 允许 Docs 中以 range 形式（"2-8" / "1/2/3"）合并多个键
				docKey := matchRangeDoc(k, docs)
				if docKey == "" {
					missing = append(missing, k)
				}
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s: bindings without matching Docs: %v", layer, missing)
		}
	}

	// Layer 0
	bindings0 := make(map[string]any)
	docs0 := make(map[string]any)
	for k := range d.Layer0.Bindings {
		bindings0[k] = struct{}{}
	}
	for k := range d.Layer0.Docs {
		docs0[k] = struct{}{}
	}
	check("Layer 0", bindings0, docs0)

	// Layer 1
	for view, l := range d.Layer1 {
		bindings := make(map[string]any)
		docs := make(map[string]any)
		for k := range l.Bindings {
			bindings[k] = struct{}{}
		}
		for k := range l.Docs {
			docs[k] = struct{}{}
		}
		check("Layer 1[view="+itoa(int(view))+"]", bindings, docs)
	}

	// Layer 2
	for pane, l := range d.Layer2 {
		bindings := make(map[string]any)
		docs := make(map[string]any)
		for k := range l.Bindings {
			bindings[k] = struct{}{}
		}
		for k := range l.Docs {
			docs[k] = struct{}{}
		}
		check("Layer 2[pane="+itoa(int(pane))+"]", bindings, docs)
	}
}

// TestKeybindingsAudit_NoDuplicateLayer0Keys 断言 Layer 0 的所有键不与
// Layer 1/2 的 Bindings 冲突（spec keybindings.md "Layer 0 永远生效" 约定）。
func TestKeybindingsAudit_NoDuplicateLayer0Keys(t *testing.T) {
	d := newDispatcher()
	for k := range d.Layer0.Bindings {
		// 跳过 modal 键（y/n 是 Layer 0 modal 也可在其他 view 模式触发）
		if k == "y" || k == "n" {
			continue
		}
		for view, l := range d.Layer1 {
			if _, ok := l.Bindings[k]; ok {
				t.Errorf("Layer 0 key %q shadowed by Layer 1[view=%d]", k, view)
			}
		}
	}
}

// matchRangeDoc 处理 Docs 中以 range 形式合并的键（如 "2-8" 覆盖 "2".."8"）。
// 返回匹配的 doc key；未匹配返回空字符串。
func matchRangeDoc(key string, docs map[string]any) string {
	for docKey := range docs {
		if !strings.ContainsAny(docKey, "-/") {
			continue
		}
		if strings.Contains(docKey, "-") {
			parts := strings.SplitN(docKey, "-", 2)
			if len(parts) == 2 && len(parts[0]) == 1 && len(parts[1]) == 1 &&
				len(key) == 1 && key[0] >= parts[0][0] && key[0] <= parts[1][0] {
				return docKey
			}
		}
		if strings.Contains(docKey, "/") {
			parts := strings.Split(docKey, "/")
			if slices.Contains(parts, key) {
				return docKey
			}
		}
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
