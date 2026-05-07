// Tests for RenderHistoryStats (Story 38-5 PR11 Step 4(c)).
//
// 行为契约覆盖（与 cmd/rnix/atdd_29_5 + dashboard_history_test 等价）：
//   1. 空 procs → 全 0 + Avg "—"
//   2. State routing：StateRunning/StateCreated → running · StateDead+failed →
//      failed · StateDead 非 failed → done · StateZombie → running
//   3. Token 累加：所有进程 TokensUsed 之和
//   4. 平均时长：仅在有 deadCount>0 时计算（DeadAt - CreatedAt）
//   5. ASCII 字符 ●/✓/✕ 出现在输出
//   6. 输出格式：换行前缀 + 段间双空格 + Total/Avg 标签

package tree

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

func TestRenderHistoryStats_Empty(t *testing.T) {
	out := RenderHistoryStats(nil)
	if !strings.Contains(out, "Running: 0") {
		t.Errorf("empty procs should report Running: 0, got %q", out)
	}
	if !strings.Contains(out, "Done: 0") {
		t.Errorf("empty procs should report Done: 0, got %q", out)
	}
	if !strings.Contains(out, "Failed: 0") {
		t.Errorf("empty procs should report Failed: 0, got %q", out)
	}
	if !strings.Contains(out, "Total: 0 tok") {
		t.Errorf("empty procs should report Total: 0 tok, got %q", out)
	}
	if !strings.Contains(out, "Avg: —") {
		t.Errorf("empty procs should report Avg: — (no dead), got %q", out)
	}
}

func TestRenderHistoryStats_StateRouting(t *testing.T) {
	now := time.Now()
	// IsFailedResult: result == "" 或包含 error/fail/timeout (case-insensitive) → true.
	// 因此 "ok" 表示成功 done · "error: ..." 或 "" 表示失败 failed.
	procs := []vfs.ProcInfo{
		{State: types.StateRunning},
		{State: types.StateCreated},
		{State: types.StateZombie},
		{State: types.StateDead, Result: "ok", DeadAt: now, CreatedAt: now.Add(-2 * time.Second)},                  // done
		{State: types.StateDead, Result: "error: timeout", DeadAt: now, CreatedAt: now.Add(-1 * time.Second)},      // failed
	}
	out := RenderHistoryStats(procs)

	// Running/Created/Zombie 三者计入 running = 3
	if !strings.Contains(out, "Running: 3") {
		t.Errorf("expected Running: 3 (Running+Created+Zombie), got %q", out)
	}
	// Dead "ok" → done (1)
	if !strings.Contains(out, "Done: 1") {
		t.Errorf("expected Done: 1, got %q", out)
	}
	// Dead "error: timeout" → failed (1)
	if !strings.Contains(out, "Failed: 1") {
		t.Errorf("expected Failed: 1, got %q", out)
	}
}

func TestRenderHistoryStats_TokensSum(t *testing.T) {
	procs := []vfs.ProcInfo{
		{State: types.StateRunning, TokensUsed: 100},
		{State: types.StateRunning, TokensUsed: 250},
		{State: types.StateDead, TokensUsed: 50},
	}
	out := RenderHistoryStats(procs)
	if !strings.Contains(out, "Total: 400 tok") {
		t.Errorf("tokens should sum to 400, got %q", out)
	}
}

func TestRenderHistoryStats_AverageElapsed(t *testing.T) {
	now := time.Now()
	procs := []vfs.ProcInfo{
		{State: types.StateDead, Result: "ok", DeadAt: now, CreatedAt: now.Add(-2 * time.Second)},
		{State: types.StateDead, Result: "ok", DeadAt: now, CreatedAt: now.Add(-4 * time.Second)},
		// avg = (2s + 4s) / 2 = 3s
	}
	out := RenderHistoryStats(procs)
	// Avg should not be "—" when deadCount > 0
	if strings.Contains(out, "Avg: —") {
		t.Errorf("avg should be computed when deadCount>0, got %q", out)
	}
	// Avg should reference duration formatted by ui.FormatDuration (human readable)
	if !strings.Contains(out, "Avg:") {
		t.Errorf("Avg label missing: %q", out)
	}
}

func TestRenderHistoryStats_ZeroDeadAtSkipped(t *testing.T) {
	// Dead 进程没有 DeadAt 不参与 avg 计算
	procs := []vfs.ProcInfo{
		{State: types.StateDead, Result: "ok"}, // DeadAt 零值
	}
	out := RenderHistoryStats(procs)
	if !strings.Contains(out, "Avg: —") {
		t.Errorf("zero DeadAt should not contribute to avg (Avg: —), got %q", out)
	}
	if !strings.Contains(out, "Done: 1") {
		t.Errorf("Done should still count: %q", out)
	}
}

func TestRenderHistoryStats_OutputFormat(t *testing.T) {
	out := RenderHistoryStats(nil)

	// 前导换行 + 空格
	if !strings.HasPrefix(out, "\n ") {
		t.Errorf("output should start with newline+space, got %q", out[:min(8, len(out))])
	}

	// 段间分隔 "  |  "（双空格 pipe 双空格）
	if !strings.Contains(out, "  |  Total:") {
		t.Errorf("expected '  |  Total:' separator, got %q", out)
	}
	if !strings.Contains(out, "  |  Avg:") {
		t.Errorf("expected '  |  Avg:' separator, got %q", out)
	}
}

func TestRenderHistoryStats_GlyphCharsPresent(t *testing.T) {
	// ●/✓/✕ 字符应在输出中（基础 lipgloss 渲染前 fmt.Sprintf 已包含 · TermProfile 影响 ANSI 不影响这些字符）
	procs := []vfs.ProcInfo{
		{State: types.StateRunning},
		{State: types.StateDead, Result: "ok", DeadAt: time.Now(), CreatedAt: time.Now().Add(-1 * time.Second)},
		{State: types.StateDead, Result: "error: timeout", DeadAt: time.Now(), CreatedAt: time.Now().Add(-1 * time.Second)},
	}
	out := RenderHistoryStats(procs)
	if !strings.Contains(out, "●") {
		t.Errorf("Running glyph ● missing in %q", out)
	}
	if !strings.Contains(out, "✓") {
		t.Errorf("Done glyph ✓ missing in %q", out)
	}
	if !strings.Contains(out, "✕") {
		t.Errorf("Failed glyph ✕ missing in %q", out)
	}
}

func TestRenderHistoryStats_MixedScenario(t *testing.T) {
	now := time.Now()
	procs := []vfs.ProcInfo{
		{State: types.StateRunning, TokensUsed: 100},
		{State: types.StateDead, Result: "ok", TokensUsed: 200, DeadAt: now, CreatedAt: now.Add(-1 * time.Second)},
		{State: types.StateDead, Result: "fail: panic", TokensUsed: 50, DeadAt: now, CreatedAt: now.Add(-3 * time.Second)},
		{State: types.StateZombie, TokensUsed: 20},
	}
	out := RenderHistoryStats(procs)

	if !strings.Contains(out, "Running: 2") { // Running + Zombie
		t.Errorf("running count: %q", out)
	}
	if !strings.Contains(out, "Done: 1") {
		t.Errorf("done count: %q", out)
	}
	if !strings.Contains(out, "Failed: 1") {
		t.Errorf("failed count: %q", out)
	}
	if !strings.Contains(out, "Total: 370 tok") {
		t.Errorf("total tokens: %q", out)
	}
	// Avg should be present (non-—)
	if strings.Contains(out, "Avg: —") {
		t.Errorf("avg should be computed: %q", out)
	}
}
