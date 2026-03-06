package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/usecrux/crux/internal/types"
	"github.com/usecrux/crux/vfs"
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

	RenderProcessTable(r, sampleProcs(), false)
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
	RenderProcessTable(r, procs, false)
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

	RenderProcessTable(r, nil, false)
	out := strings.TrimSpace(buf.String())
	if out != "No active processes." {
		t.Fatalf("expected 'No active processes.', got: %q", out)
	}
}

func TestRenderProcessTable_EmptySlice(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	RenderProcessTable(r, []vfs.ProcInfo{}, false)
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
	RenderProcessTable(r, procs, false)
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
	RenderProcessTable(r, procs, false)
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
	RenderProcessTable(r, procs, false)
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
	RenderProcessTable(r, procs, false)
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
	RenderProcessTable(r, procs, false)
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
	RenderProcessTable(r, procs, true)
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
	RenderProcessTable(r, procs, false)
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
	RenderProcessTable(r, procs, false)
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
	RenderProcessTable(r, procs, false)
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
	RenderProcessTable(r, procs, false)
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
	RenderProcessTable(r, procs, false)
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
	RenderProcessTable(r, procs, false)
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
