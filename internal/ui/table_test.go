package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// testRenderer creates a Renderer writing to a buffer with the given profile.
func testRenderer(profile TerminalProfile, mode OutputMode) (*Renderer, *bytes.Buffer) {
	var buf bytes.Buffer
	r := &Renderer{
		Profile:    profile,
		Writer:     &buf,
		OutputMode: mode,
	}
	return r, &buf
}

// defaultTestProfile returns a profile with no color for deterministic test output.
func defaultTestProfile() TerminalProfile {
	return TerminalProfile{
		Width:      120,
		IsTTY:      false,
		ColorLevel: 0,
		IsUnicode:  true,
	}
}

func sampleProcs() []vfs.ProcInfo {
	base := time.Now().Add(-6200 * time.Millisecond)
	return []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning, Intent: "分析代码", Skills: []string{"code-analyst"}, TokensUsed: 1847, CreatedAt: base},
		{PID: 2, PPID: 1, State: types.StateZombie, Intent: "审查 PR #42", Skills: nil, TokensUsed: 423, CreatedAt: base.Add(-500 * time.Millisecond)},
		{PID: 3, PPID: 0, State: types.StateDead, Intent: "重构 main.go", Skills: []string{"pr-reviewer"}, TokensUsed: 3201, CreatedAt: base.Add(-10 * time.Second)},
	}
}

func TestRenderProcessTable_BasicOutput(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	RenderProcessTable(r, sampleProcs(), false, false)
	out := buf.String()

	// Header should contain column names
	if !strings.Contains(out, "PID") {
		t.Fatal("expected PID column header")
	}
	if !strings.Contains(out, "STATE") {
		t.Fatal("expected STATE column header")
	}
	if !strings.Contains(out, "SKILL") {
		t.Fatal("expected SKILL column header")
	}
	if !strings.Contains(out, "TOKENS") {
		t.Fatal("expected TOKENS column header")
	}
	if !strings.Contains(out, "ELAPSED") {
		t.Fatal("expected ELAPSED column header")
	}

	// Data rows
	if !strings.Contains(out, "running") {
		t.Fatal("expected 'running' state in output")
	}
	if !strings.Contains(out, "zombie") {
		t.Fatal("expected 'zombie' state in output")
	}
	if !strings.Contains(out, "dead") {
		t.Fatal("expected 'dead' state in output")
	}

	// Footer
	if !strings.Contains(out, "1 active") {
		t.Fatal("expected '1 active' in footer")
	}
	if !strings.Contains(out, "1 zombie") {
		t.Fatal("expected '1 zombie' in footer")
	}
	if !strings.Contains(out, "3 total") {
		t.Fatal("expected '3 total' in footer")
	}
}

func TestRenderProcessTable_ColumnAlignment(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning, Skills: []string{"a"}, TokensUsed: 100, CreatedAt: time.Now()},
		{PID: 99, PPID: 0, State: types.StateDead, Skills: []string{"b"}, TokensUsed: 99999, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, false, false)
	lines := strings.Split(buf.String(), "\n")

	// PID column should be right-aligned (5 chars wide)
	// Line 0 = header, Line 1 = separator, Line 2 = first data, Line 3 = second data
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d", len(lines))
	}

	// PID "1" should be right-aligned: "    1"
	if !strings.HasPrefix(lines[2], "    1") {
		t.Fatalf("PID 1 should be right-aligned, got: %q", lines[2][:10])
	}
	// PID "99" should be right-aligned: "   99"
	if !strings.HasPrefix(lines[3], "   99") {
		t.Fatalf("PID 99 should be right-aligned, got: %q", lines[3][:10])
	}

	// TOKENS should be right-aligned
	if !strings.Contains(lines[2], "     100") {
		t.Fatalf("expected TOKENS right-aligned '     100' in line: %q", lines[2])
	}
	if !strings.Contains(lines[3], "  99,999") {
		t.Fatalf("expected TOKENS right-aligned '  99,999' in line: %q", lines[3])
	}
}

func TestRenderProcessTable_StateColorCoding(t *testing.T) {
	// Test that renderState returns different styled strings for each state when color is enabled.
	profile := TerminalProfile{
		Width:      120,
		IsTTY:      true,
		ColorLevel: 3,
		IsUnicode:  true,
	}
	InitStyles(profile)
	r, _ := testRenderer(profile, ModeDefault)

	states := []types.ProcessState{
		types.StateRunning,
		types.StateZombie,
		types.StateDead,
		types.StateCreated,
	}

	results := make(map[types.ProcessState]string)
	for _, s := range states {
		result := renderState(r, s, colWidthState)
		plain := StripAnsi(result)
		if !strings.Contains(plain, s.String()) {
			t.Errorf("renderState(%s) should contain state name, got: %q", s, plain)
		}
		results[s] = result
	}

	// Verify different states produce different styled output
	if results[types.StateRunning] == results[types.StateZombie] {
		t.Error("running and zombie should have different styles")
	}
	if results[types.StateRunning] == results[types.StateDead] {
		t.Error("running and dead should have different styles")
	}
	if results[types.StateZombie] == results[types.StateDead] {
		t.Error("zombie and dead should have different styles")
	}
}

func TestRenderProcessTable_EmptyList(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	RenderProcessTable(r, nil, false, false)
	out := strings.TrimSpace(buf.String())
	if out != "No active processes." {
		t.Fatalf("expected 'No active processes.', got: %q", out)
	}
}

func TestRenderProcessTable_EmptySlice(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	RenderProcessTable(r, []vfs.ProcInfo{}, false, false)
	out := strings.TrimSpace(buf.String())
	if out != "No active processes." {
		t.Fatalf("expected 'No active processes.', got: %q", out)
	}
}

func TestRenderProcessTable_WidthAdaptation_Narrow(t *testing.T) {
	profile := TerminalProfile{Width: 40, ColorLevel: 0, IsUnicode: true}
	InitStyles(profile)
	r, buf := testRenderer(profile, ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, Skills: []string{"x"}, TokensUsed: 100, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, false, false)
	header := strings.Split(buf.String(), "\n")[0]

	// Width 40: only PID + STATE
	if !strings.Contains(header, "PID") {
		t.Fatal("PID should always show")
	}
	if !strings.Contains(header, "STATE") {
		t.Fatal("STATE should always show")
	}
	if strings.Contains(header, "SKILL") {
		t.Fatal("SKILL should not show at width 40")
	}
	if strings.Contains(header, "TOKENS") {
		t.Fatal("TOKENS should not show at width 40")
	}
}

func TestRenderProcessTable_WidthAdaptation_60(t *testing.T) {
	profile := TerminalProfile{Width: 60, ColorLevel: 0, IsUnicode: true}
	InitStyles(profile)
	r, buf := testRenderer(profile, ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, Skills: []string{"x"}, TokensUsed: 100, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, false, false)
	header := strings.Split(buf.String(), "\n")[0]

	if !strings.Contains(header, "SKILL") {
		t.Fatal("SKILL should show at width 60")
	}
	if strings.Contains(header, "TOKENS") {
		t.Fatal("TOKENS should not show at width 60")
	}
}

func TestRenderProcessTable_WidthAdaptation_80(t *testing.T) {
	profile := TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true}
	InitStyles(profile)
	r, buf := testRenderer(profile, ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, Skills: []string{"x"}, TokensUsed: 100, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, false, false)
	header := strings.Split(buf.String(), "\n")[0]

	if !strings.Contains(header, "SKILL") {
		t.Fatal("SKILL should show at width 80")
	}
	if !strings.Contains(header, "TOKENS") {
		t.Fatal("TOKENS should show at width 80")
	}
	if !strings.Contains(header, "ELAPSED") {
		t.Fatal("ELAPSED should show at width 80")
	}
}

func TestRenderProcessTable_WidthAdaptation_120(t *testing.T) {
	profile := TerminalProfile{Width: 120, ColorLevel: 0, IsUnicode: true}
	InitStyles(profile)
	r, buf := testRenderer(profile, ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, Intent: "分析代码", Skills: []string{"x"}, TokensUsed: 100, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, false, false)
	header := strings.Split(buf.String(), "\n")[0]

	if !strings.Contains(header, "INTENT") {
		t.Fatal("INTENT should show at width 120")
	}
}

func TestRenderProcessTable_NoColor(t *testing.T) {
	profile := TerminalProfile{Width: 120, ColorLevel: 0, IsUnicode: true}
	InitStyles(profile)
	r, buf := testRenderer(profile, ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, false, false)
	out := buf.String()

	// Should not contain ANSI escape sequences
	if strings.Contains(out, "\033[") {
		t.Fatal("expected no ANSI escape sequences in NO_COLOR mode")
	}
}

func TestRenderProcessTable_VerboseMode(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning, Intent: "分析代码", Skills: []string{"x"}, TokensUsed: 100, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, true, false)
	header := strings.Split(buf.String(), "\n")[0]

	if !strings.Contains(header, "PPID") {
		t.Fatal("PPID should show in verbose mode")
	}
	if !strings.Contains(header, "INTENT") {
		t.Fatal("INTENT should show in verbose mode")
	}
}

func TestRenderProcessTable_UnicodeSeparator(t *testing.T) {
	profile := TerminalProfile{Width: 120, ColorLevel: 0, IsUnicode: true}
	InitStyles(profile)
	r, buf := testRenderer(profile, ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, false, false)
	lines := strings.Split(buf.String(), "\n")

	if !strings.Contains(lines[1], "─") {
		t.Fatalf("expected Unicode separator ─, got: %q", lines[1])
	}
}

func TestRenderProcessTable_ASCIISeparator(t *testing.T) {
	profile := TerminalProfile{Width: 120, ColorLevel: 0, IsUnicode: false}
	InitStyles(profile)
	r, buf := testRenderer(profile, ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, false, false)
	lines := strings.Split(buf.String(), "\n")

	if strings.Contains(lines[1], "─") {
		t.Fatal("expected ASCII separator, not Unicode")
	}
	if !strings.Contains(lines[1], "-") {
		t.Fatalf("expected ASCII separator -, got: %q", lines[1])
	}
}

func TestRenderProcessTable_NoSkillDash(t *testing.T) {
	profile := TerminalProfile{Width: 120, ColorLevel: 0, IsUnicode: true}
	InitStyles(profile)
	r, buf := testRenderer(profile, ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, Skills: nil, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, false, false)
	out := buf.String()

	// With Unicode, no-skill shows "—"
	if !strings.Contains(out, "—") {
		t.Fatal("expected Unicode dash — for no skills")
	}
}

func TestRenderProcessTable_NoSkillASCII(t *testing.T) {
	profile := TerminalProfile{Width: 120, ColorLevel: 0, IsUnicode: false}
	InitStyles(profile)
	r, buf := testRenderer(profile, ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, Skills: nil, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, false, false)
	lines := strings.Split(buf.String(), "\n")

	// Data row should contain "-" for no skills (ASCII mode)
	found := false
	for _, line := range lines[2:] {
		if strings.Contains(line, "running") && strings.Contains(line, "-") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected ASCII dash - for no skills in ASCII mode")
	}
}

func TestRenderProcessTable_Footer(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, CreatedAt: time.Now()},
		{PID: 2, State: types.StateCreated, CreatedAt: time.Now()},
		{PID: 3, State: types.StateZombie, CreatedAt: time.Now()},
		{PID: 4, State: types.StateDead, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, false, false)
	out := buf.String()

	// active includes Running + Created
	if !strings.Contains(out, "2 active") {
		t.Fatalf("expected '2 active' (running+created), got: %s", out)
	}
	if !strings.Contains(out, "1 zombie") {
		t.Fatalf("expected '1 zombie' in footer")
	}
	if !strings.Contains(out, "4 total") {
		t.Fatalf("expected '4 total' in footer")
	}
}

func TestFormatDuration_Boundaries(t *testing.T) {
	tests := []struct {
		dur    time.Duration
		expect string
	}{
		{0, "0ms"},
		{500 * time.Millisecond, "500ms"},
		{999 * time.Millisecond, "999ms"},
		{1 * time.Second, "1.0s"},
		{1500 * time.Millisecond, "1.5s"},
		{59900 * time.Millisecond, "59.9s"},
		{60 * time.Second, "1.0m"},
		{600 * time.Second, "10.0m"},
	}
	for _, tc := range tests {
		got := FormatDuration(tc.dur)
		if got != tc.expect {
			t.Errorf("FormatDuration(%v) = %q, want %q", tc.dur, got, tc.expect)
		}
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		n      int
		expect string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1847, "1,847"},
		{99999, "99,999"},
		{1000000, "1,000,000"},
	}
	for _, tc := range tests {
		got := FormatTokens(tc.n)
		if got != tc.expect {
			t.Errorf("FormatTokens(%d) = %q, want %q", tc.n, got, tc.expect)
		}
	}
}

func TestStripAnsi(t *testing.T) {
	// Plain text unchanged
	if got := StripAnsi("hello"); got != "hello" {
		t.Errorf("StripAnsi plain = %q", got)
	}
	// ANSI escape removed
	if got := StripAnsi("\033[31mhello\033[0m"); got != "hello" {
		t.Errorf("StripAnsi colored = %q", got)
	}
}

func TestFormatSkills_Truncation(t *testing.T) {
	long := []string{"very-long-skill-name", "another-one"}
	got := FormatSkills(long, 15, "—")
	if len(got) > 15 {
		t.Errorf("FormatSkills should truncate to maxWidth, got len=%d: %q", len(got), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected truncated string to end with ..., got: %q", got)
	}
}

func TestFormatSkills_Empty(t *testing.T) {
	got := FormatSkills(nil, 15, "—")
	if got != "—" {
		t.Errorf("FormatSkills(nil) = %q, want '—'", got)
	}
	got = FormatSkills([]string{}, 15, "—")
	if got != "—" {
		t.Errorf("FormatSkills([]) = %q, want '—'", got)
	}
}

func TestRenderProcessTable_PIDSortVerification(t *testing.T) {
	// Verify output respects the order of input procs (caller is responsible for sorting)
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	procs := []vfs.ProcInfo{
		{PID: 5, State: types.StateRunning, CreatedAt: time.Now()},
		{PID: 1, State: types.StateDead, CreatedAt: time.Now()},
		{PID: 10, State: types.StateZombie, CreatedAt: time.Now()},
	}
	RenderProcessTable(r, procs, false, false)
	lines := strings.Split(buf.String(), "\n")

	// Data rows start at line 2 (after header and separator)
	if len(lines) < 5 {
		t.Fatalf("expected at least 5 lines, got %d", len(lines))
	}
	// First data row should have PID 5 (input order preserved)
	if !strings.Contains(lines[2], "5") {
		t.Errorf("first data row should contain PID 5, got: %q", lines[2])
	}
	// Second data row should have PID 1
	if !strings.Contains(lines[3], "1") {
		t.Errorf("second data row should contain PID 1, got: %q", lines[3])
	}
}

// TestRenderProcessTable_ExitColumn verifies the verbose-only EXIT column
// surfaces a dead/zombie process's ExitReason (the async driver-error reason,
// e.g. an HTTP 429 rate limit, that the resume CLI never showed). The default
// (non-verbose) table must NOT show it.
func TestRenderProcessTable_ExitColumn(t *testing.T) {
	InitStyles(defaultTestProfile())
	base := time.Now().Add(-30 * time.Second)
	procs := []vfs.ProcInfo{
		{PID: 855, PPID: 0, State: types.StateZombie, Intent: "MODE=resume", TokensUsed: 209000,
			CreatedAt: base, DeadAt: time.Now(),
			ExitReason: "llm write failed: 429 rate limit exceeded", ExitCode: 1, ExitCodeSet: true},
	}

	t.Run("verbose shows EXIT column and reason", func(t *testing.T) {
		r, buf := testRenderer(defaultTestProfile(), ModeDefault)
		RenderProcessTable(r, procs, true, false)
		out := buf.String()
		if !strings.Contains(out, "EXIT") {
			t.Errorf("verbose table missing EXIT header:\n%s", out)
		}
		// Reason is display-width truncated; its head must be present.
		if !strings.Contains(out, "llm write failed") {
			t.Errorf("verbose table missing exit reason head:\n%s", out)
		}
	})

	t.Run("default table omits exit reason", func(t *testing.T) {
		r, buf := testRenderer(defaultTestProfile(), ModeDefault)
		RenderProcessTable(r, procs, false, false)
		out := buf.String()
		if strings.Contains(out, "EXIT") || strings.Contains(out, "llm write failed") {
			t.Errorf("default (non-verbose) table should not show exit reason:\n%s", out)
		}
	})
}

// --- ShortUUID (suffix short identifier) ---

func TestShortUUID(t *testing.T) {
	uuid := "019534a1-7c6b-7000-8abc-123456789012"
	// The prefix depends on the runewidth environment: "…" (1-col) in
	// non-EastAsian locales, "~" (1-col) in EastAsian locales where "…"
	// renders as 2 columns. Tests must be portable across both.
	pfx := "…"
	if runewidth.RuneWidth('…') != 1 {
		pfx = "~"
	}

	t.Run("normal UUID → prefix + last 6", func(t *testing.T) {
		want := pfx + "789012"
		if got := ShortUUID(uuid); got != want {
			t.Errorf("ShortUUID(%q) = %q, want %q", uuid, got, want)
		}
	})

	t.Run("empty string returned unchanged", func(t *testing.T) {
		if got := ShortUUID(""); got != "" {
			t.Errorf("ShortUUID(\"\") = %q, want \"\"", got)
		}
	})

	t.Run("shorter than 6 runes returned unchanged", func(t *testing.T) {
		if got := ShortUUID("u1"); got != "u1" {
			t.Errorf("ShortUUID(\"u1\") = %q, want \"u1\"", got)
		}
	})

	t.Run("exactly 6 runes gets prefix", func(t *testing.T) {
		want := pfx + "abcdef"
		if got := ShortUUID("abcdef"); got != want {
			t.Errorf("ShortUUID(\"abcdef\") = %q, want %q", got, want)
		}
	})

	t.Run("multi-byte input is rune-safe", func(t *testing.T) {
		want := pfx + "字漢字漢字漢"
		if got := ShortUUID("漢字漢字漢字漢"); got != want {
			t.Errorf("ShortUUID CJK = %q, want %q", got, want)
		}
	})

	t.Run("ASCII mode uses tilde prefix", func(t *testing.T) {
		t.Setenv("RNIX_ASCII", "1")
		if got := ShortUUID(uuid); got != "~789012" {
			t.Errorf("ShortUUID ASCII = %q, want %q", got, "~789012")
		}
	})
}

// --- Date-aware wall-clock formatting (nowFunc injection) ---

// withFixedNow installs a fixed "now" for the duration of the test.
func withFixedNow(t *testing.T, now time.Time) {
	t.Helper()
	old := nowFunc
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = old })
}

func TestFormatWallClock_DateAware(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.Local)
	withFixedNow(t, now)

	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero time", time.Time{}, "—"},
		{"today → time only (byte-identical to before)",
			time.Date(2026, 7, 4, 9, 5, 7, 0, time.Local), "09:05:07"},
		{"cross-day same year → MM-DD prefix",
			time.Date(2026, 7, 3, 15, 4, 5, 0, time.Local), "07-03 15:04:05"},
		{"cross-year → full date prefix",
			time.Date(2025, 12, 31, 15, 4, 5, 0, time.Local), "2025-12-31 15:04:05"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatWallClock(tc.t); got != tc.want {
				t.Errorf("FormatWallClock(%v) = %q, want %q", tc.t, got, tc.want)
			}
		})
	}
}

func TestFormatWallClockShort_DateAware(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.Local)
	withFixedNow(t, now)

	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero time", time.Time{}, "—"},
		{"today → time only (byte-identical to before)",
			time.Date(2026, 7, 4, 9, 5, 7, 0, time.Local), "09:05"},
		{"cross-day same year → MM-DD prefix",
			time.Date(2026, 7, 3, 15, 4, 5, 0, time.Local), "07-03 15:04"},
		{"cross-year → full date prefix",
			time.Date(2025, 12, 31, 15, 4, 5, 0, time.Local), "2025-12-31 15:04"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatWallClockShort(tc.t); got != tc.want {
				t.Errorf("FormatWallClockShort(%v) = %q, want %q", tc.t, got, tc.want)
			}
		})
	}
}

func TestFormatWallClock_MidnightBoundary(t *testing.T) {
	// 23:59 vs next-day 00:01 — natural-day comparison, not a 24h window.
	now := time.Date(2026, 7, 4, 0, 1, 0, 0, time.Local)
	withFixedNow(t, now)

	created := time.Date(2026, 7, 3, 23, 59, 0, 0, time.Local)
	if got := FormatWallClock(created); got != "07-03 23:59:00" {
		t.Errorf("FormatWallClock 2min ago across midnight = %q, want %q", got, "07-03 23:59:00")
	}
	if got := FormatWallClockShort(created); got != "07-03 23:59" {
		t.Errorf("FormatWallClockShort 2min ago across midnight = %q, want %q", got, "07-03 23:59")
	}

	// Same day, even at the day's edge → time only.
	sameDay := time.Date(2026, 7, 4, 0, 0, 30, 0, time.Local)
	if got := FormatWallClock(sameDay); got != "00:00:30" {
		t.Errorf("FormatWallClock same-day edge = %q, want %q", got, "00:00:30")
	}
}

func TestFormatRelativeTime_DayUnits(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.Local)
	withFixedNow(t, now)

	tests := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"23h stays hour unit", 23 * time.Hour, "23h ago"},
		{"49h → 2d ago", 49 * time.Hour, "2d ago"},
		{"25h → 1d ago", 25 * time.Hour, "1d ago"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatRelativeTime(now.Add(-tc.ago)); got != tc.want {
				t.Errorf("FormatRelativeTime(-%v) = %q, want %q", tc.ago, got, tc.want)
			}
		})
	}
}
