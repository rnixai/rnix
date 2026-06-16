// Package inspector — raw_lens_test.go (Story 56.4 · CAP-3 路② Raw I/O lens)
//
// 行为测试覆盖 RenderRawLens pure helper（AC#3 / #5）+ LensRaw 枚举守门。
//
// RED 机制（记忆 atdd-code-story-red-mechanism-preference）：骨架 + t.Skip。
//   - LensRaw 顺序守门（LensRaw 紧跟 LensRawJSON）= green-guard（常量已存在即过）；
//   - LensCount==6 守门 = t.Skip（dev 落地时把 state.go LensCount 5→6 后移除 skip）；
//   - RenderRawLens API/CLI 渲染 / 截断标记 / ASCII 降级 / nil 安全 = t.Skip
//     （dev 移除 skip 后填 raw_lens.go 渲染逻辑验 RED→GREEN）。
//
// pure helper 无 dashboardModel 依赖，直接喂 vfs.RawCapture 断言渲染串（参考
// meta_lens_test.go）。JSON roundtrip 后 headers/argv 是 map[string]any / []any
// （deferred #23 预警）——fixture 直接构造 map[string]any 形态以贴合 production。
package inspector

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// rawAPIFixture 构造一条 56.2-shaped API RawCapture（reasoning_effort 在 body）。
func rawAPIFixture() *vfs.RawCapture {
	return &vfs.RawCapture{
		TsMs: 1000,
		Step: 1,
		Kind: "api",
		Request: map[string]any{
			"method":  "POST",
			"url":     "https://api.example.com/v1/messages",
			"headers": map[string]any{"authorization": "redacted(len=40,prefix=sk-,sha256=abcd)"},
			"body":    `{"model":"claude","reasoning_effort":"high"}`,
		},
		Response: map[string]any{
			"status": float64(200),
			"body":   `{"role":"assistant"}`,
		},
	}
}

// rawCLIFixture 构造一条 56.3-shaped CLI RawCapture（--effort 在 argv）。
func rawCLIFixture() *vfs.RawCapture {
	return &vfs.RawCapture{
		TsMs: 2000,
		Step: 2,
		Kind: "cli",
		Request: map[string]any{
			"argv":  []any{"claude", "-p", "--effort", "high"},
			"stdin": "user prompt",
			"env":   map[string]any{"ANTHROPIC_API_KEY": "redacted(len=40,prefix=sk-,sha256=abcd)"},
		},
		Response: map[string]any{
			"stdout":    "assistant reply",
			"stderr":    "",
			"exit_code": float64(0),
		},
	}
}

// ---------------------------------------------------------------------------
// AC#3: LensRaw 枚举顺序守门 (green-guard — 常量已追加在 LensRawJSON 之后即过)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC3_LensRaw_OrderAfterRawJSON(t *testing.T) {
	if LensRaw != LensRawJSON+1 {
		t.Fatalf("AC#3: LensRaw must be appended immediately after LensRawJSON "+
			"(lens 顺序敏感 · state.go:41-42): LensRaw=%d, LensRawJSON=%d", LensRaw, LensRawJSON)
	}
}

// ---------------------------------------------------------------------------
// AC#3: LensCount 同步 5→6 守门 (t.Skip until dev bumps LensCount in state.go)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC3_LensCount_Is6(t *testing.T) {

	if LensCount != 6 {
		t.Fatalf("AC#3: LensCount must sync to 6 when LensRaw is added (否则 viewport/content "+
			"数组越界或漏一个): got %d", LensCount)
	}
}

// ---------------------------------------------------------------------------
// AC#3: RenderRawLens — API 族渲染 (t.Skip until impl)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC3_RenderRawLens_API(t *testing.T) {

	out := RenderRawLens(rawAPIFixture(), 80)
	// API 族须可见 method / url / 以及 body 内的 effort 真实值（CAP-3 核心）
	for _, want := range []string{"POST", "api.example.com", "reasoning_effort", "high"} {
		if !strings.Contains(out, want) {
			t.Errorf("AC#3: API render missing %q\n---\n%s", want, out)
		}
	}
}

// ---------------------------------------------------------------------------
// AC#3: RenderRawLens — CLI 族渲染 (t.Skip until impl)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC3_RenderRawLens_CLI(t *testing.T) {

	out := RenderRawLens(rawCLIFixture(), 80)
	// CLI 族须可见 argv 内的 --effort 真实值 + stdout + exit_code（CAP-3 核心）
	for _, want := range []string{"claude", "--effort", "high", "assistant reply"} {
		if !strings.Contains(out, want) {
			t.Errorf("AC#3: CLI render missing %q\n---\n%s", want, out)
		}
	}
}

// ---------------------------------------------------------------------------
// AC#3 / AC#5: 截断标记可见 (t.Skip until impl)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC3_RenderRawLens_TruncatedMarkerVisible(t *testing.T) {

	rc := rawAPIFixture()
	rc.Truncated = true
	rc.OriginalBytes = 5_000_000
	out := RenderRawLens(rc, 80)
	// 顶部一行须暴露截断状态 + 原始字节数（AC#3 / AC#5 截断可见）
	if !strings.Contains(out, "truncated") {
		t.Errorf("AC#3: truncated marker not visible\n---\n%s", out)
	}
	if !strings.Contains(out, "5000000") && !strings.Contains(out, "5,000,000") {
		t.Errorf("AC#3: OriginalBytes not visible\n---\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// AC#5: 脱敏指纹原样显示 — 不还原 (t.Skip until impl)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC5_RenderRawLens_RedactedFingerprintShown(t *testing.T) {

	out := RenderRawLens(rawAPIFixture(), 80)
	// 落盘已脱敏 → lens 读到即显示指纹，零反脱敏
	if !strings.Contains(out, "redacted(") {
		t.Errorf("AC#5: redacted fingerprint should be shown verbatim\n---\n%s", out)
	}
	// 反向：绝不能出现疑似还原的明文 key 前缀以外的完整密钥（此处仅哨兵断言指纹存在）
}

// ---------------------------------------------------------------------------
// AC#3: nil / 空安全 (green-guard — 骨架已 nil 安全)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC3_RenderRawLens_NilSafe(t *testing.T) {
	// 懒加载未命中 / 该 step 无 raw 记录 → 不 panic，返回非空占位提示
	out := RenderRawLens(nil, 80)
	if out == "" {
		t.Errorf("AC#3: nil capture should yield a non-empty placeholder, got empty")
	}
}

// ---------------------------------------------------------------------------
// AC#3: ASCII 降级 — 零宽度 / 窄宽度不 panic (green-guard)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC3_RenderRawLens_WidthBoundary_NoPanic(t *testing.T) {
	// 边界宽度（0 / 1）不得 panic（骨架与实现均须满足）
	for _, w := range []int{0, 1, 200} {
		_ = RenderRawLens(rawAPIFixture(), w)
		_ = RenderRawLens(rawCLIFixture(), w)
	}
}
