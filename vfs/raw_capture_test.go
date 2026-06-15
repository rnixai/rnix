package vfs

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// ============================================================================
// ATDD Story 56.1 — vfs.RawCapture 信封 + RawCaptureProvider 接口 (AC#1)
//
// 56-1-UNIT-005. 锁定信封字段集 + JSON tag 全 snake_case + 接口签名。
// 56.2/56.3 必须遵循同一信封 — 这条单测在改动 RawCapture 字段时会立刻
// 红线，作为后续 story 的合约 guard。
// ============================================================================

// 56-1-UNIT-005: 信封字段 + JSON snake_case tag + RawCaptureProvider 签名。
func TestATDD_56_1_005_RawCapture_Envelope_Shape(t *testing.T) {
	// --- (a) 必备字段集 ---
	rt := reflect.TypeFor[RawCapture]()
	wantFields := map[string]bool{
		"TsMs":          false,
		"Step":          false,
		"Kind":          false,
		"Request":       false,
		"Response":      false,
		"Truncated":     false,
		"OriginalBytes": false,
	}
	for f := range rt.Fields() {
		if _, ok := wantFields[f.Name]; ok {
			wantFields[f.Name] = true
		}
	}
	for name, found := range wantFields {
		if !found {
			t.Errorf("RawCapture missing required field %q", name)
		}
	}

	// --- (b) JSON tag 全 snake_case，无 PascalCase 漏出 ---
	wantTags := map[string]string{
		"TsMs":          "ts_ms",
		"Step":          "step",
		"Kind":          "kind",
		"Request":       "request",
		"Response":      "response",
		"Truncated":     "truncated",
		"OriginalBytes": "original_bytes",
	}
	for f := range rt.Fields() {
		want, tracked := wantTags[f.Name]
		if !tracked {
			continue
		}
		tag := f.Tag.Get("json")
		// 允许 ",omitempty" 后缀
		got := strings.SplitN(tag, ",", 2)[0]
		if got != want {
			t.Errorf("field %s: json tag = %q, want %q", f.Name, got, want)
		}
	}

	// --- (c) round-trip JSON 不变形（tag 实际生效）---
	sample := RawCapture{
		TsMs:          1234,
		Step:          5,
		Kind:          "api",
		Request:       map[string]any{"url": "https://example/v1"},
		Response:      map[string]any{"status": float64(200)},
		Truncated:     true,
		OriginalBytes: 8192,
	}
	data, err := json.Marshal(sample)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(data)
	for _, tag := range []string{`"ts_ms"`, `"step"`, `"kind"`, `"truncated"`, `"original_bytes"`} {
		if !strings.Contains(s, tag) {
			t.Errorf("marshaled JSON missing %s tag: %s", tag, s)
		}
	}

	var back RawCapture
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back, sample) {
		t.Errorf("round-trip mismatch:\n  got  = %+v\n  want = %+v", back, sample)
	}

	// --- (d) RawCaptureProvider 接口签名（编译期断言）---
	var _ RawCaptureProvider = (*fakeRawCaptureProvider)(nil)
}

type fakeRawCaptureProvider struct{ rc *RawCapture }

func (f *fakeRawCaptureProvider) LastRawCapture() *RawCapture { return f.rc }
