package intentdriver

import (
	"strings"
	"testing"
)

// ATDD Story 54.2 — intent 4 工具 ToolDef.Name 改 PascalCase（AC1）+ prompt/description 内
// LLM 可见的工具名引用同步（AC4）。RED 断言新名（改名前运行时失败），t.Skip 标记；
// green-guard（不 skip、立即绿）守护 Subpath（设备路由锚点）与 intent_id（JSON 参数名、
// 异命名空间）不被改名误伤。ToolDefs() 不依赖 manager，故测试用 NewDriver(nil) 隔离构造。

// --- AC1：intent 4 工具呈现名改 PascalCase（RED 脚手架）---

// TestATDD_54_2_100 断言 4 个 intent 工具的 ToolDef.Name 改为 PascalCase，无 snake_case 残留。
func TestATDD_54_2_100_IntentTools_PascalCaseNames(t *testing.T) {
	t.Skip("RED: 待 54.2 实现——drivers/intent/driver.go:33/66/87/108 Name 改 PascalCase")

	defs := NewDriver(nil).ToolDefs()

	want := map[string]bool{
		"IntentDecompose": false, "IntentStatus": false,
		"IntentConfirm": false, "IntentExecute": false,
	}
	forbidden := map[string]bool{
		"intent_decompose": true, "intent_status": true,
		"intent_confirm": true, "intent_execute": true,
	}
	for _, d := range defs {
		if _, ok := want[d.Name]; ok {
			want[d.Name] = true
		}
		if forbidden[d.Name] {
			t.Errorf("snake_case 旧名 %q 仍存在", d.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("缺少 PascalCase 工具名 %q", name)
		}
	}
}

// --- AC4：LLM 可见的工具名引用同步（RED 脚手架）---

// TestATDD_54_2_110 断言 decompose prompt 内的工具名引用改为 PascalCase。
// intent_decompose.txt:14 "Use intent_confirm to approve" → "Use IntentConfirm to approve"。
// ⚠️ 该文件 line 16 的 auto_start 是【参数名】，不在改名范围（本测试只查工具名 intent_confirm）。
func TestATDD_54_2_110_DecomposePrompt_ReferencesPascalCase(t *testing.T) {
	t.Skip("RED: 待 54.2 实现——drivers/intent/prompts/intent_decompose.txt:14 工具名引用")

	prompt := loadPrompt("intent_decompose")
	if !strings.Contains(prompt, "IntentConfirm") {
		t.Errorf("decompose prompt 应引用 PascalCase 工具名 %q", "IntentConfirm")
	}
	if strings.Contains(prompt, "intent_confirm") {
		t.Errorf("decompose prompt 仍含 snake_case 工具名 %q", "intent_confirm")
	}
}

// TestATDD_54_2_111 断言 auto_start 参数 description（driver.go:59 硬编码）内的工具名引用
// 改为 PascalCase。⚠️ description 含工具名 intent_confirm/intent_execute（须改）与参数名
// auto_start（不改）——本测试只断言工具名部分。
func TestATDD_54_2_111_AutoStartDescription_ReferencesPascalCase(t *testing.T) {
	t.Skip("RED: 待 54.2 实现——drivers/intent/driver.go:59 description 内工具名引用")

	desc := autoStartParamDescription(t)
	for _, newName := range []string{"IntentConfirm", "IntentExecute"} {
		if !strings.Contains(desc, newName) {
			t.Errorf("auto_start description 应引用工具名 %q", newName)
		}
	}
	for _, oldName := range []string{"intent_confirm", "intent_execute"} {
		if strings.Contains(desc, oldName) {
			t.Errorf("auto_start description 仍含 snake_case 工具名 %q", oldName)
		}
	}
}

// --- green-guard（不 skip、立即绿）---

// TestATDD_54_2_900 守护 Subpath（设备路由锚点，AC5）不变。本 story 只改 Name 不动 Subpath，
// 故用 Subpath 集合（不依赖 Name）作稳定锚点——改名前后都应绿。
func TestATDD_54_2_900_GreenGuard_SubpathsUnchanged(t *testing.T) {
	defs := NewDriver(nil).ToolDefs()
	got := make(map[string]bool, len(defs))
	for _, d := range defs {
		got[d.Subpath] = true
	}
	for _, sp := range []string{"/decompose", "/status", "/confirm", "/execute"} {
		if !got[sp] {
			t.Errorf("缺少 Subpath %q（路由锚点不可随改名变动）", sp)
		}
	}
}

// TestATDD_54_2_901 守护 intent_id 是 JSON 参数名（项目约定 snake_case），与工具名异命名空间，
// 禁止被 `sed s/intent_/Intent/` 式宽匹配误伤（AC4 参数名陷阱）。
func TestATDD_54_2_901_GreenGuard_IntentIDParamUnchanged(t *testing.T) {
	defs := NewDriver(nil).ToolDefs()
	checked := 0
	for _, d := range defs {
		switch d.Subpath {
		case "/status", "/confirm", "/execute":
			props, _ := d.Parameters["properties"].(map[string]any)
			if _, ok := props["intent_id"]; !ok {
				t.Errorf("%s: 缺少参数 intent_id（参数名不可被改名误伤）", d.Subpath)
			}
			checked++
		}
	}
	if checked != 3 {
		t.Errorf("期望检查 3 个带 intent_id 的工具，实际 %d", checked)
	}
}

// autoStartParamDescription 提取 decompose 工具 auto_start 参数的 description 文本。
// 用 Subpath=="/decompose" 定位（稳定锚点，不随 Name 改名变动）。
func autoStartParamDescription(t *testing.T) string {
	t.Helper()
	for _, d := range NewDriver(nil).ToolDefs() {
		if d.Subpath != "/decompose" {
			continue
		}
		props, ok := d.Parameters["properties"].(map[string]any)
		if !ok {
			t.Fatal("decompose: properties 非 map")
		}
		autoStart, ok := props["auto_start"].(map[string]any)
		if !ok {
			t.Fatal("decompose: 缺少 auto_start 参数")
		}
		desc, _ := autoStart["description"].(string)
		return desc
	}
	t.Fatal("未找到 decompose 工具（Subpath=/decompose）")
	return ""
}
