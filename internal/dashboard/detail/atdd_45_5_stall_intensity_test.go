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
	"github.com/rnixai/rnix/internal/ui"
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
// Boundary scan is LINE-BASED: we look for the next line whose leading
// whitespace is followed by a divider sequence ("  ----" / "  ────").
// A substring search over the body would be unsafe because the intensity
// bar's unfilled portion (`barWidth - filled` repeats of '-' in ASCII
// mode) can include a 4+ '-' run that collides with the divider pattern,
// truncating the section mid-bar and silently weakening downstream
// assertions like width checks.
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
	// Scan body line by line; a divider always starts a line (after the
	// two-space indent), so leading-whitespace check rules out the bar.
	offset := 0
	for {
		lineEnd := strings.Index(body[offset:], "\n")
		var line string
		if lineEnd < 0 {
			line = body[offset:]
		} else {
			line = body[offset : offset+lineEnd]
		}
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "────") || strings.HasPrefix(trimmed, "----") {
			return rest[:bodyStart+offset]
		}
		if lineEnd < 0 {
			break
		}
		offset += lineEnd + 1
	}
	return rest
}

// --- AC1 (#001): summary line content ---

// TestATDD_45_5_001_RenderStallSummary_Level3 asserts the header + summary
// line content for a level-3 stall: idle / gap durations are formatted via
// time.Duration.String() ("3m5s" form); `level N/4` uses min(ConsecutiveStalls,4);
// action is prefixed with "would ".
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
		"3m5s",             // 185000ms via time.Duration.String()
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
// Per-level color gradient assertion (Decision D3): levelN < 3 → ColorWarning
// only; levelN == 3 → ColorWarning + Bold (transition stage); levelN >= 4 →
// ColorError. We probe by rendering the same filled glyph through the
// expected lipgloss style and checking the resulting ANSI-wrapped substring
// is present in the stall section. This guards D3 against regressions that
// would swap colors or drop the Bold transition.
func TestATDD_45_5_002_IntensityBarFillRatio_ConsecutiveStalls(t *testing.T) {
	pid := types.PID(42)
	uuid := "abc12345-xxxx"
	innerW := 80
	barWidth := innerW - 10 // max(innerW-10, 10) = 70 when innerW=80

	cases := []struct {
		stalls       int
		wantSuffixN  int // N/4 suffix value
		wantClampLvl int // value used in fill-ratio calc (post-clamp)
		wantColor    string
		wantBold     bool
		forbidColor  string // a color that must NOT appear at this level
	}{
		{stalls: 1, wantSuffixN: 1, wantClampLvl: 1, wantColor: ui.ColorWarning, wantBold: false, forbidColor: ui.ColorError},
		{stalls: 2, wantSuffixN: 2, wantClampLvl: 2, wantColor: ui.ColorWarning, wantBold: false, forbidColor: ui.ColorError},
		{stalls: 3, wantSuffixN: 3, wantClampLvl: 3, wantColor: ui.ColorWarning, wantBold: true, forbidColor: ui.ColorError},
		{stalls: 4, wantSuffixN: 4, wantClampLvl: 4, wantColor: ui.ColorError, wantBold: false, forbidColor: ui.ColorWarning},
		{stalls: 7, wantSuffixN: 4, wantClampLvl: 4, wantColor: ui.ColorError, wantBold: false, forbidColor: ui.ColorWarning}, // clamp probe
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

		// Per-level color gradient (D3 guard). Build the ANSI-open prefix
		// (escape + glyph, without closing reset) that the renderer emits
		// when wrapping the entire filled run, and assert it is present.
		// We can't match the full `style.Render("█")` because that string
		// ends in a reset escape, while production wraps N consecutive '█'
		// in a single open/close pair — the closing reset comes after the
		// run, not after each glyph.
		wantStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tc.wantColor))
		if tc.wantBold {
			wantStyle = wantStyle.Bold(true)
		}
		if !strings.Contains(out, styledOpenPrefix(wantStyle, "█")) {
			t.Errorf("stalls=%d: expected %s%s styled fill glyph in output; got:\n%s",
				tc.stalls, tc.wantColor, boldLabel(tc.wantBold), out)
		}

		// The wrong palette color must NOT appear at this level.
		forbidStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tc.forbidColor))
		if strings.Contains(out, styledOpenPrefix(forbidStyle, "█")) {
			t.Errorf("stalls=%d: must NOT contain %s styled fill glyph; got:\n%s",
				tc.stalls, tc.forbidColor, out)
		}

		// Bold-only differentiator at level 3: when Bold is expected the
		// non-bold ColorWarning prefix must NOT be the one emitted; when
		// Bold is NOT expected the Bold ColorWarning prefix must NOT appear.
		// Rules out the regression where the Bold transition silently drops.
		if tc.wantColor == ui.ColorWarning {
			plainPrefix := styledOpenPrefix(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning)), "█")
			boldPrefix := styledOpenPrefix(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning)).Bold(true), "█")
			if tc.wantBold && !strings.Contains(out, boldPrefix) {
				t.Errorf("stalls=%d: expected Bold ColorWarning glyph at level 3 transition; output missing bold escape", tc.stalls)
			}
			if !tc.wantBold && plainPrefix != boldPrefix && strings.Contains(out, boldPrefix) {
				t.Errorf("stalls=%d: must NOT emit Bold style below level 3; got bold warning glyph", tc.stalls)
			}
		}
	}
}

func boldLabel(bold bool) string {
	if bold {
		return "+Bold"
	}
	return ""
}

// styledOpenPrefix returns the ANSI open-escape + glyph portion of a
// lipgloss render — i.e. everything before the trailing reset. Used to
// substring-match into production output where N consecutive glyphs are
// wrapped in a single open/close pair (e.g. "\x1b[33m█████\x1b[0m").
func styledOpenPrefix(style lipgloss.Style, glyph string) string {
	rendered := style.Render(glyph)
	idx := strings.Index(rendered, glyph)
	if idx < 0 {
		return glyph
	}
	return rendered[:idx+len(glyph)]
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

// TestATDD_45_5_004_NoStallSection_WhenNotStalled covers 4 skip paths:
//  1. HeartbeatStatus == nil
//  2. CurrentStalled empty slice
//  3. UUID mismatch (selectedUUID non-empty branch — stale-data guard)
//  4. PID mismatch (selectedUUID empty branch — true PID-fallback skip)
//
// Subcase 4 was missing before review (the previous "PID mismatch" actually
// exercised the UUID branch because selectedUUID was non-empty). It now
// explicitly drives the PID-fallback matcher and asserts skip on PID
// disagreement.
//
// Note: pre-impl this trivially passes (no stall code emits anything). The
// test's role is regression-guard once renderStallSection lands — it must
// not leak section content for non-stalled selections.
func TestATDD_45_5_004_NoStallSection_WhenNotStalled(t *testing.T) {
	pid := types.PID(42)
	uuid := "abc12345-xxxx"

	subcases := []struct {
		name         string
		selectedUUID string
		hb           *ipc.HeartbeatStatusResponse
	}{
		{
			name:         "HeartbeatStatus is nil",
			selectedUUID: uuid,
			hb:           nil,
		},
		{
			name:         "CurrentStalled is empty",
			selectedUUID: uuid,
			hb:           &ipc.HeartbeatStatusResponse{Running: true, CurrentStalled: nil},
		},
		{
			name:         "UUID mismatch (selectedUUID set, wire UUID different)",
			selectedUUID: uuid,
			hb:           stallHeartbeat(stalledFixture(pid, "other-uuid")),
		},
		{
			name:         "PID fallback PID mismatch (selectedUUID empty, wire PID=99)",
			selectedUUID: "",
			hb:           stallHeartbeat(stalledFixture(types.PID(99), "irrelevant-uuid")),
		},
	}

	for _, sc := range subcases {
		t.Run(sc.name, func(t *testing.T) {
			state := stallTestState(pid, sc.selectedUUID)
			ctx := RenderContext{
				SelectedPID:     pid,
				SelectedUUID:    sc.selectedUUID,
				HeartbeatStatus: sc.hb,
			}
			out := Render(state, ctx, 80)

			// Only check for the Stall section header — the most specific
			// substring. The previous `level` / `would` forbids were too
			// generic and would false-positive against any future Detail
			// section that legitimately contains those English words.
			if strings.Contains(out, "Stall") {
				t.Errorf("%s: must NOT contain Stall section header; got:\n%s", sc.name, out)
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
// Runs across BOTH render paths:
//   - ASCII mode (RNIX_ASCII=1): bar uses '#'/'-' glyphs, no ANSI escapes;
//     visibleWidth = rune-count after stripping any incidental escapes.
//   - Unicode + colored mode: bar uses '█'/'░' glyphs wrapped in lipgloss
//     foreground styles; width must be computed via lipgloss.Width which
//     accounts for ANSI escape bytes and runewidth correctly (the spec
//     line 180 explicitly requires lipgloss.Width).
func TestATDD_45_5_007_StallSectionFitsInnerW(t *testing.T) {
	pid := types.PID(42)
	uuid := "abc12345-xxxx"
	wire := stalledFixture(pid, uuid)
	wire.ConsecutiveStalls = 4 // worst case — bar fully filled

	modes := []struct {
		name       string
		asciiOn    bool
		widthOfFn  func(string) int
	}{
		{name: "ascii", asciiOn: true, widthOfFn: visibleWidth},
		{name: "unicode-colored", asciiOn: false, widthOfFn: lipgloss.Width},
	}

	for _, mode := range modes {
		for _, innerW := range []int{40, 60, 80, 120} {
			t.Run(mode.name+"/innerW="+itoaSmall(innerW), func(t *testing.T) {
				if mode.asciiOn {
					t.Setenv("RNIX_ASCII", "1")
				}

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
					t.Fatalf("mode=%s innerW=%d: expected Stall section in output:\n%s", mode.name, innerW, out)
				}

				section := stallSection(out)
				if section == "" {
					t.Fatalf("mode=%s innerW=%d: could not isolate stall section from:\n%s", mode.name, innerW, out)
				}
				for line := range strings.SplitSeq(section, "\n") {
					if line == "" {
						continue
					}
					if w := mode.widthOfFn(line); w > innerW {
						t.Errorf("mode=%s innerW=%d: stall line width %d > innerW; line=%q",
							mode.name, innerW, w, line)
					}
				}
			})
		}
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
