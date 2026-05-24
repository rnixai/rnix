// Package detail — atdd_45_5_stall_intensity_test.go
//
// Story 45.5: Dashboard stall intensity heatmap (epic-45 §AC-EA5)
//
// Red-phase ATDD scaffolds — guards the Detail pane stall section behavior
// under P4 daemon-passive supervision. Borrows the fade-to-red intensity
// concept from cc-src `Spinner/useStalledAnimation.ts` (3s/2s thresholds →
// rnix ConsecutiveStalls 4-level mapping warn / cancel_step / suspend).
//
// AC coverage:
//   - AC1 → TestATDD_45_5_001_RenderStallSummary_Level3
//   - AC2 → TestATDD_45_5_002_IntensityBarFillRatio_ConsecutiveStalls (5 sub)
//          + TestATDD_45_5_003_IntensityBar_ASCIIMode
//   - AC3 → TestATDD_45_5_004_NoStallSection_WhenNotStalled (3 sub)
//   - AC4 → TestATDD_45_5_005_UUIDMismatchSkipsStallSection (2 sub)
//   - AC6 → TestATDD_45_5_007_StallSectionFitsInnerW (4 sub)
//
// (AC5 wire-injection covered in cmd/rnix/atdd_45_5_detail_wire_test.go;
//  AC7 make-all regression covered in dev-story Task 5.)
//
// Pre-impl red-phase signal: this file references the new field
//   detail.RenderContext.HeartbeatStatus *ipc.HeartbeatStatusResponse
// which Story 45.5 introduces in dev-story Task 1.1. Until that field
// exists the file fails to compile — the strongest red signal possible for
// a renderer change. After Task 1 the file compiles, and the runtime
// assertions then catch missing renderStallSection behavior (Task 2).
package detail

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// Force lipgloss into a colorful profile when tests run without a TTY (the
// default `go test` environment sets the profile to Ascii, which would strip
// every ANSI escape that the renderer emits). The Stall intensity bar's
// gradient assertions in 002 require ANSI escapes to be present, and the
// ASCII-mode path (003) is governed by our own RNIX_ASCII fallback inside
// renderStallSection rather than by the lipgloss profile — so flipping the
// global profile to TrueColor is safe for the entire detail test package.
func init() {
	lipgloss.DefaultRenderer().SetColorProfile(termenv.TrueColor)
}

// --- Fixtures / helpers ---

// stalledFixture builds a single StalledProcWire matching the supplied PID
// and UUID. Defaults to a level-3 (cancel_step) scenario; callers override
// individual fields for parameterized cases.
func stalledFixture(pid types.PID, uuid string) ipc.StalledProcWire {
	return ipc.StalledProcWire{
		PID:               pid,
		UUID:              uuid,
		ConsecutiveStalls: 3,
		StalledDurationMs: 185_000, // 3m5s
		HeartbeatGapMs:    185_000, // 3m5s
		LastAction:        "cancel_step",
	}
}

// stallTestState builds a minimal DetailState whose Detail.PID/UUID/State
// match SelectedPID/SelectedUUID so the Render() Loading-guard does not
// short-circuit (Story 28-4 AC-4 contract). Without this match Render
// would emit "Loading..." and skip every downstream section, masking the
// behavior under test here.
func stallTestState(pid types.PID, uuid string) DetailState {
	return DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:      pid,
			UUID:     uuid,
			State:    "running",
			Provider: "claude",
			Model:    "sonnet",
		},
	}
}

// stallHeartbeat wraps one StalledProcWire into a HeartbeatStatusResponse.
func stallHeartbeat(wires ...ipc.StalledProcWire) *ipc.HeartbeatStatusResponse {
	return &ipc.HeartbeatStatusResponse{
		Running:              true,
		CheckIntervalMs:      30_000,
		TotalStalledDetected: len(wires),
		CurrentStalled:       wires,
	}
}

// stallSection extracts the substring from `out` between the Stall divider
// and the next divider (or end-of-string). Used by width tests so we only
// measure lines that belong to the new section. Returns empty string when
// no Stall section is present.
//
// The header line itself ends with "----" / "────" (the divider closing
// dashes). Naïvely searching from `len(marker)` would terminate the
// section on those dashes, so we first advance past the header's newline
// before scanning for the next divider.
func stallSection(out string) string {
	const marker = "Stall"
	idx := strings.Index(out, marker)
	if idx < 0 {
		return ""
	}
	rest := out[idx:]
	nl := strings.Index(rest, "\n")
	if nl < 0 {
		return rest
	}
	bodyStart := nl + 1
	body := rest[bodyStart:]
	if next := strings.Index(body, "────"); next >= 0 {
		return rest[:bodyStart+next]
	}
	if next := strings.Index(body, "----"); next >= 0 {
		return rest[:bodyStart+next]
	}
	return rest
}

// --- AC1 (#001): summary line content ---

// TestATDD_45_5_001_RenderStallSummary_Level3 asserts the header + summary
// line content for a level-3 stall: idle / gap durations are formatted via
// ui.FormatDuration; `level N/4` uses min(ConsecutiveStalls,4); action is
// prefixed with "would ".
func TestATDD_45_5_001_RenderStallSummary_Level3(t *testing.T) {
	pid := types.PID(42)
	uuid := "abc12345-xxxx"
	state := stallTestState(pid, uuid)
	ctx := RenderContext{
		SelectedPID:     pid,
		SelectedUUID:    uuid,
		HeartbeatStatus: stallHeartbeat(stalledFixture(pid, uuid)),
	}

	out := Render(state, ctx, 80)

	cases := []string{
		"Stall",            // section header
		"PID 42",           // selected PID in summary
		"3m5s",             // 185000ms via ui.FormatDuration
		"level 3/4",        // ConsecutiveStalls=3 → "3/4"
		"would cancel_step", // LastAction prefixed with "would "
	}
	for _, want := range cases {
		if !strings.Contains(out, want) {
			t.Errorf("expected stall summary to contain %q; got:\n%s", want, out)
		}
	}

	// Both StalledDurationMs and HeartbeatGapMs render the same value here
	// (185000ms each) — the summary must surface both. Sanity-check the
	// duration string appears at least twice in the output (idle + gap).
	if got := strings.Count(out, "3m5s"); got < 2 {
		t.Errorf("expected duration '3m5s' to appear ≥2 times (idle + gap), got %d in:\n%s", got, out)
	}
}

// --- AC2 (#002): intensity bar fill ratio + N/4 suffix + color ---

// TestATDD_45_5_002_IntensityBarFillRatio_ConsecutiveStalls parameterizes
// 5 stall levels (1/2/3/4/7). filled char count must equal
//
//	int(min(level, 4) / 4.0 * barWidth) ± 1
//
// where barWidth = max(innerW-10, 10) and innerW = 80 here. The suffix
// N/4 must use min(level, 4). ConsecutiveStalls=7 is the clamp probe.
//
// Color assertion is a loose ANSI-escape presence check (matches the
// established internal/ui/*_test.go pattern) — guarantees the renderer
// emits styled output without depending on a specific RGB hex.
func TestATDD_45_5_002_IntensityBarFillRatio_ConsecutiveStalls(t *testing.T) {
	pid := types.PID(42)
	uuid := "abc12345-xxxx"
	innerW := 80
	barWidth := innerW - 10 // max(innerW-10, 10) = 70 when innerW=80

	cases := []struct {
		stalls       int
		wantSuffixN  int // N/4 suffix value
		wantClampLvl int // value used in fill-ratio calc (post-clamp)
	}{
		{stalls: 1, wantSuffixN: 1, wantClampLvl: 1},
		{stalls: 2, wantSuffixN: 2, wantClampLvl: 2},
		{stalls: 3, wantSuffixN: 3, wantClampLvl: 3},
		{stalls: 4, wantSuffixN: 4, wantClampLvl: 4},
		{stalls: 7, wantSuffixN: 4, wantClampLvl: 4}, // clamp probe
	}

	for _, tc := range cases {
		wire := stalledFixture(pid, uuid)
		wire.ConsecutiveStalls = tc.stalls

		state := stallTestState(pid, uuid)
		ctx := RenderContext{
			SelectedPID:     pid,
			SelectedUUID:    uuid,
			HeartbeatStatus: stallHeartbeat(wire),
		}

		out := Render(state, ctx, innerW)

		// Suffix N/4 must use clamped value.
		wantSuffix := "] " + itoaSmall(tc.wantSuffixN) + "/4"
		if !strings.Contains(out, wantSuffix) {
			t.Errorf("stalls=%d: expected suffix %q in output:\n%s", tc.stalls, wantSuffix, out)
		}

		// Filled char count = int(level / 4.0 * barWidth) with ±1 tolerance
		// for floating-point truncation. We strip ANSI escapes before
		// counting so style wrappers don't inflate the column.
		clean := stripANSI(out)
		filled := strings.Count(clean, "█")
		expected := tc.wantClampLvl * barWidth / 4
		if filled < expected-1 || filled > expected+1 {
			t.Errorf("stalls=%d: expected filled='█' count %d±1; got %d in:\n%s",
				tc.stalls, expected, filled, clean)
		}

		// Loose ANSI escape presence — assert renderer emits styled output.
		// Pre-impl this fails because no Stall section is emitted at all.
		if !strings.Contains(out, "\x1b[") {
			t.Errorf("stalls=%d: expected ANSI color escape in stall section output, got plain text:\n%s",
				tc.stalls, out)
		}
	}
}

// --- AC2 (#003): RNIX_ASCII=1 fallback ---

// TestATDD_45_5_003_IntensityBar_ASCIIMode confirms:
//   - filled char is '#' (not '█')
//   - unfilled char is '-' (not '░')
//   - no ANSI escape in the stall section
func TestATDD_45_5_003_IntensityBar_ASCIIMode(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")

	pid := types.PID(42)
	uuid := "abc12345-xxxx"
	wire := stalledFixture(pid, uuid)
	wire.ConsecutiveStalls = 4

	state := stallTestState(pid, uuid)
	ctx := RenderContext{
		SelectedPID:     pid,
		SelectedUUID:    uuid,
		HeartbeatStatus: stallHeartbeat(wire),
	}

	out := Render(state, ctx, 80)

	// Confirm header present (so we know the section was emitted).
	if !strings.Contains(out, "Stall") {
		t.Fatalf("expected Stall section header even in ASCII mode, got:\n%s", out)
	}

	section := stallSection(out)
	if section == "" {
		t.Fatalf("could not isolate stall section from:\n%s", out)
	}

	if !strings.Contains(section, "#") {
		t.Errorf("ASCII mode: expected filled char '#', got section:\n%s", section)
	}
	if strings.Contains(section, "█") {
		t.Errorf("ASCII mode: must NOT contain Unicode '█', got section:\n%s", section)
	}
	if strings.Contains(section, "░") {
		t.Errorf("ASCII mode: must NOT contain Unicode '░', got section:\n%s", section)
	}
	if strings.Contains(section, "\x1b[") {
		t.Errorf("ASCII mode: must NOT contain ANSI escape, got section:\n%s", section)
	}
}

// --- AC3 (#004): skip-if-empty / no stall section ---

// TestATDD_45_5_004_NoStallSection_WhenNotStalled covers 3 skip paths:
//  1. HeartbeatStatus == nil
//  2. CurrentStalled empty slice
//  3. CurrentStalled holds different PID
//
// Note: pre-impl this trivially passes (no stall code emits anything). The
// test's role is regression-guard once renderStallSection lands — it must
// not leak section content for non-stalled selections.
func TestATDD_45_5_004_NoStallSection_WhenNotStalled(t *testing.T) {
	pid := types.PID(42)
	uuid := "abc12345-xxxx"
	state := stallTestState(pid, uuid)

	subcases := []struct {
		name string
		hb   *ipc.HeartbeatStatusResponse
	}{
		{
			name: "HeartbeatStatus is nil",
			hb:   nil,
		},
		{
			name: "CurrentStalled is empty",
			hb:   &ipc.HeartbeatStatusResponse{Running: true, CurrentStalled: nil},
		},
		{
			name: "PID mismatch (stalled PID 99, selected PID 42)",
			hb:   stallHeartbeat(stalledFixture(types.PID(99), "other-uuid")),
		},
	}

	for _, sc := range subcases {
		t.Run(sc.name, func(t *testing.T) {
			ctx := RenderContext{
				SelectedPID:     pid,
				SelectedUUID:    uuid,
				HeartbeatStatus: sc.hb,
			}
			out := Render(state, ctx, 80)

			forbidden := []string{"Stall", "level", "would"}
			for _, bad := range forbidden {
				if strings.Contains(out, bad) {
					t.Errorf("%s: must NOT contain %q; got:\n%s", sc.name, bad, out)
				}
			}
		})
	}
}

// --- AC4 (#005): UUID-first matching with PID fallback ---

// TestATDD_45_5_005_UUIDMismatchSkipsStallSection covers Story 28-4 AC-4
// equivalent stale-data guard for the new stall section.
func TestATDD_45_5_005_UUIDMismatchSkipsStallSection(t *testing.T) {
	pid := types.PID(42)
	selectedUUID := "abc12345-xxxx"

	t.Run("UUID mismatch → skip", func(t *testing.T) {
		// Selected PID=42 / UUID=abc...; CurrentStalled has same PID=42 but
		// a different UUID (e.g. old process whose PID was reused).
		state := stallTestState(pid, selectedUUID)
		stale := stalledFixture(pid, "different-uuid-xxxx")
		ctx := RenderContext{
			SelectedPID:     pid,
			SelectedUUID:    selectedUUID,
			HeartbeatStatus: stallHeartbeat(stale),
		}
		out := Render(state, ctx, 80)
		if strings.Contains(out, "Stall") {
			t.Errorf("UUID mismatch must skip stall section, but got:\n%s", out)
		}
	})

	t.Run("UUID empty → PID fallback renders", func(t *testing.T) {
		// Backward compatibility with old daemons that may not populate
		// SelectedUUID. When SelectedUUID == "" the matcher falls back to
		// PID-only equality. Detail.UUID can be anything; Render's loading
		// guard only enforces UUID match when SelectedUUID != "".
		state := stallTestState(pid, "any-uuid-ok")
		wire := stalledFixture(pid, "stalled-uuid-yyy")
		ctx := RenderContext{
			SelectedPID:     pid,
			SelectedUUID:    "", // fallback path
			HeartbeatStatus: stallHeartbeat(wire),
		}
		out := Render(state, ctx, 80)
		if !strings.Contains(out, "Stall") {
			t.Errorf("UUID empty fallback should render stall section; got:\n%s", out)
		}
	})
}

// --- AC6 (#007): inner-width fit / no overflow ---

// TestATDD_45_5_007_StallSectionFitsInnerW asserts every emitted stall
// section line satisfies lipgloss.Width(line) <= innerW for 4 widths.
//
// Uses ASCII mode to keep the assertion deterministic (no ANSI escapes
// to confuse lipgloss.Width on some terminal profiles) — the width
// contract is independent of color profile, so testing under ASCII is
// sufficient. Color path is covered by 002.
func TestATDD_45_5_007_StallSectionFitsInnerW(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")

	pid := types.PID(42)
	uuid := "abc12345-xxxx"
	wire := stalledFixture(pid, uuid)
	wire.ConsecutiveStalls = 4 // worst case — bar fully filled

	for _, innerW := range []int{40, 60, 80, 120} {
		t.Run("innerW="+itoaSmall(innerW), func(t *testing.T) {
			state := stallTestState(pid, uuid)
			ctx := RenderContext{
				SelectedPID:     pid,
				SelectedUUID:    uuid,
				HeartbeatStatus: stallHeartbeat(wire),
			}
			out := Render(state, ctx, innerW)

			// Sanity: stall section must be present so we are actually
			// exercising the width constraint (not vacuously passing).
			if !strings.Contains(out, "Stall") {
				t.Fatalf("innerW=%d: expected Stall section in output:\n%s", innerW, out)
			}

			section := stallSection(out)
			if section == "" {
				t.Fatalf("innerW=%d: could not isolate stall section from:\n%s", innerW, out)
			}
			for line := range strings.SplitSeq(section, "\n") {
				if line == "" {
					continue
				}
				if w := visibleWidth(line); w > innerW {
					t.Errorf("innerW=%d: stall line width %d > innerW; line=%q",
						innerW, w, line)
				}
			}
		})
	}
}

// --- helpers ---

// itoaSmall is a minimal int → string converter for small positive
// integers (suffix labels and table loop labels). Avoids pulling in
// strconv just for one digit values.
func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [12]byte
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

// stripANSI removes CSI escape sequences from s. Tolerant of incomplete
// sequences (treats them as literal). Used to make `█` counting independent
// of color profile in 002.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			// Skip until we hit a letter (terminator of CSI sequence).
			j := i + 2
			for j < len(s) {
				c := s[j]
				j++
				if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
					break
				}
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// visibleWidth returns the displayed column width of a line after
// stripping ANSI escapes. Renderer code uses lipgloss.Width, but tests
// only need column count on ASCII output (007 runs in ASCII mode), and
// avoiding the lipgloss import here keeps the test self-contained.
func visibleWidth(line string) int {
	clean := stripANSI(line)
	// All chars emitted by renderStallSection in ASCII mode are ASCII
	// (space / digits / letters / '#' / '-' / '[' / ']' / '|' etc.), so
	// rune count == column count. CJK width adjustments only matter in
	// Unicode mode (covered loosely by visual inspection of header).
	return len([]rune(clean))
}
