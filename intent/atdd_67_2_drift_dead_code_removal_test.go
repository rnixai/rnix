package intent

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Story 67.2 — Intent 漂移死代码删除与 Reconciler 叙事正名。
//
// 删除型 story 无新增生产符号，不适用「骨架 + t.Skip」模式。RED 机制是
// 源码内容扫描断言（[[atdd-doc-markdown-story-pattern]] 文件合规断言）：
// 测试读取 intent 包非测试 .go 生产源文件（排除 _test.go 后缀，天然含本
// ATDD 文件自身，避免自指命中），断言死符号零命中。删除前 FAIL（红），
// 删除后 GREEN。活体守卫从第一天起绿且保持绿（GREEN-stays-GREEN，不 skip）。
//
// grep 子串陷阱：kernel/os_reconcile.go 的 osReconcileInterval 含子串
// ReconcileInterval，但在 kernel 包不在 intent 包——扫描限定 intent 目录
// （CWD=intent，filepath.Glob("*.go")）天然规避。

// deadIntentDriftSymbols are identifiers whose presence in intent package
// production source proves the dead 漂移检测骨架 (ComputeDrifts 死方法 +
// 两个从未写入的空壳漂移常量 + 两个从未被读的 ticker 配置字段) still exists.
// Story 67.2 deletes all of them; live 漂移事件符号 (DriftNodeFailed/
// DriftNodeTimeout/AddDrift/ClearDrift/...) 不在此列，由下方 GREEN 守卫保护。
var deadIntentDriftSymbols = []string{
	"ComputeDrifts",       // 死方法：唯一调用者是测试（随本 story 一并删除）
	"DriftNewRequirement", // 空壳常量：全仓零写入
	"DriftNodeModified",   // 空壳常量：全仓零写入
	"ReconcileInterval",   // 死配置字段：循环纯事件驱动无 ticker，零读取
	"MaxReconcileDelay",   // 死配置字段：零读取
}

// TestATDD_67_2_DeadIntentDriftSymbolsRemoved scans intent package production
// .go source and fails if any dead symbol from the removed 漂移检测骨架 remains.
func TestATDD_67_2_DeadIntentDriftSymbolsRemoved(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob intent sources: %v", err)
	}
	scanned := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue // exclude tests (and this ATDD file) — avoid self-hit
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		scanned++
		src := string(data)
		for _, sym := range deadIntentDriftSymbols {
			if strings.Contains(src, sym) {
				t.Errorf("dead symbol %q still present in %s — Story 67.2 requires removal", sym, f)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("scanned 0 intent source files — glob or CWD wrong")
	}
}

// TestATDD_67_2_LiveDriftEventChainStillIntact is a GREEN-stays-GREEN guard:
// 保留的活体漂移事件符号 (DriftNodeFailed/DriftNodeTimeout 由 reconciler.go
// 失败/超时路径真实写入) 与漂移生命周期方法 (InitDesired/AddDrift/ClearDrift/
// ActiveDrifts 是 reconciler 写入链) must remain intact after 死骨架 deletion.
func TestATDD_67_2_LiveDriftEventChainStillIntact(t *testing.T) {
	// 活体常量值不变（reconciler.go:155-157 写入 + intent_status wire 暴露集合）
	if DriftNodeFailed != "node_failed" {
		t.Errorf("DriftNodeFailed = %q, want \"node_failed\"", DriftNodeFailed)
	}
	if DriftNodeTimeout != "node_timeout" {
		t.Errorf("DriftNodeTimeout = %q, want \"node_timeout\"", DriftNodeTimeout)
	}

	// 活体方法可调用（InitDesired 三处生产调用；AddDrift/ClearDrift/ActiveDrifts 走 reconciler 写入链）
	tree := &IntentTree{
		ID:         "intent-67-2",
		RootIntent: "green guard",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"a": {ID: "a", Intent: "task a", State: IntentPending},
		},
		CreatedAt: time.Now(),
	}
	tree.InitDesired()
	if tree.DesiredNodes["a"] != IntentCompleted {
		t.Errorf("InitDesired: DesiredNodes[a] = %q, want %q", tree.DesiredNodes["a"], IntentCompleted)
	}
	tree.AddDrift(DriftItem{NodeID: "a", Type: DriftNodeFailed, Message: "boom", DetectedAt: time.Now()})
	if len(tree.ActiveDrifts()) != 1 {
		t.Fatalf("ActiveDrifts after AddDrift = %d, want 1", len(tree.ActiveDrifts()))
	}
	tree.ClearDrift("a")
	if len(tree.ActiveDrifts()) != 0 {
		t.Errorf("ActiveDrifts after ClearDrift = %d, want 0", len(tree.ActiveDrifts()))
	}
}

// TestATDD_67_2_DriftItemWireTagsUnchanged is a GREEN-stays-GREEN guard on the
// intent_status IPC 暴露面：DriftItem 的 json tag 是 wire 不变式 (AC4 零改动)。
// 删除 ComputeDrifts 只删方法体，DriftItem 结构与 tag 逐字节保留。
func TestATDD_67_2_DriftItemWireTagsUnchanged(t *testing.T) {
	wantTags := map[string]string{
		"NodeID":     "node_id",
		"Type":       "type",
		"Message":    "message",
		"DetectedAt": "detected_at",
	}
	typ := reflect.TypeFor[DriftItem]()
	for fieldName, wantTag := range wantTags {
		field, ok := typ.FieldByName(fieldName)
		if !ok {
			t.Errorf("DriftItem.%s field missing", fieldName)
			continue
		}
		if got := field.Tag.Get("json"); got != wantTag {
			t.Errorf("DriftItem.%s json tag = %q, want %q", fieldName, got, wantTag)
		}
	}
}

// TestATDD_67_2_ReconcilerConfigLiveFieldsIntact is a GREEN-stays-GREEN guard:
// 删除死 ticker 字段 (ReconcileInterval/MaxReconcileDelay) 后 ReconcilerConfig
// 的两个活体字段 (DefaultMaxRetries/DefaultTimeout，reconciler.go:99-101 真实
// 读取) 默认值不变。
func TestATDD_67_2_ReconcilerConfigLiveFieldsIntact(t *testing.T) {
	cfg := DefaultReconcilerConfig()
	if cfg.DefaultMaxRetries != 3 {
		t.Errorf("DefaultReconcilerConfig().DefaultMaxRetries = %d, want 3", cfg.DefaultMaxRetries)
	}
	if cfg.DefaultTimeout != 5*time.Minute {
		t.Errorf("DefaultReconcilerConfig().DefaultTimeout = %v, want 5m", cfg.DefaultTimeout)
	}
}
