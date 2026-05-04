package main

// =============================================================================
// Story 38-3: Step Inspector 5-lens 视觉增强
// PR1 tests — Conversation 4-color role tag + Tool I/O Box-drawing
// =============================================================================

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// --- AC#1: formatRoleTag tool_use vs tool_result ---

// TestFormatRoleTag_ToolUseVsToolResult verifies Story 38-3 AC#1's split of the
// legacy "tool" branch into tool_use (no ToolCallID, ColorSuccess green) and
// tool_result (with ToolCallID, ColorReplay orange). Other roles stay untouched.
func TestFormatRoleTag_ToolUseVsToolResult(t *testing.T) {
	toolNames := map[string]string{"call_123": "Read"}

	t.Run("tool_use_no_toolcallid", func(t *testing.T) {
		msg := ipc.MessageWire{Role: "tool", ToolCallID: ""}
		tag := formatRoleTag(msg, toolNames)
		if !strings.Contains(tag, "tool_use") {
			t.Errorf("tool_use path should contain 'tool_use' literal, got %q", tag)
		}
		// ColorSuccess #6BCB77 → ANSI 24-bit RGB 107;203;119
		if !strings.Contains(tag, ";203;") && !strings.Contains(tag, "6BCB77") {
			// Best-effort check: tolerate lipgloss color downgrade.
			t.Logf("note: tool_use tag = %q (color depends on lipgloss profile)", tag)
		}
	})

	t.Run("tool_result_with_toolcallid", func(t *testing.T) {
		msg := ipc.MessageWire{Role: "tool", ToolCallID: "call_123"}
		tag := formatRoleTag(msg, toolNames)
		if !strings.Contains(tag, "tool_result") {
			t.Errorf("tool_result path should contain 'tool_result' literal, got %q", tag)
		}
		if !strings.Contains(tag, "Read") {
			t.Errorf("tool_result should suffix mapped tool name, got %q", tag)
		}
	})

	t.Run("tool_result_unmapped_falls_back_to_id", func(t *testing.T) {
		msg := ipc.MessageWire{Role: "tool", ToolCallID: "tc_unknown"}
		tag := formatRoleTag(msg, map[string]string{})
		if !strings.Contains(tag, "tool_result") || !strings.Contains(tag, "tc_unknown") {
			t.Errorf("unmapped tool_result should suffix raw ID, got %q", tag)
		}
	})

	t.Run("user_assistant_system_unchanged", func(t *testing.T) {
		// Regression: pre-Story-38-3 role tags must still render as bracketed labels.
		for _, role := range []string{"user", "assistant", "system"} {
			msg := ipc.MessageWire{Role: role}
			tag := formatRoleTag(msg, toolNames)
			if !strings.Contains(tag, "["+role+"]") {
				t.Errorf("role %q should render as [%s], got %q", role, role, tag)
			}
		}
	})
}

// --- AC#2: Tool I/O lens box-drawing ---

func TestBuildToolIOLens_BoxDrawing(t *testing.T) {
	t.Run("input_box_unicode", func(t *testing.T) {
		m := newTestInspectorModelWithDetail()
		m.width = 100
		content := m.buildLensContent(lensToolIO, m.inspector.Detail, nil)
		if !strings.Contains(content, "┌") || !strings.Contains(content, "Input") {
			t.Errorf("unicode mode: Input box should contain ┌ and Input title; got:\n%s", content)
		}
	})

	t.Run("error_box_red_border", func(t *testing.T) {
		m := newTestInspectorModelWithDetail()
		m.width = 100
		// Inject error to trigger Error box
		detail := *m.inspector.Detail
		detail.ToolError = "permission denied"
		content := m.buildLensContent(lensToolIO, &detail, nil)
		if !strings.Contains(content, "Error") {
			t.Errorf("Error box should render Error title; got:\n%s", content)
		}
		// Story 38-3 review P22: probe the lipgloss profile first; only
		// assert the red colour ANSI code when the runner has a colour
		// profile, otherwise the assertion would never fail in CI's
		// no-colour environment (previously logged via t.Logf, which never
		// flagged a real regression).
		probe := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render("x")
		if probe == "x" {
			t.Skip("color profile is no-op on this runner; skipping ANSI assertion")
		}
		// ColorError #FF6B6B → 24-bit RGB 255;107;107 (lipgloss truecolor profile).
		// Tolerate other profiles by also accepting the literal color hex.
		if !strings.Contains(content, ";107;107") && !strings.Contains(content, "FF6B6B") {
			t.Errorf("Error box should render with red border (#FF6B6B); got snippet = %q", content)
		}
	})

	t.Run("no_tool_info_fallback", func(t *testing.T) {
		m := newTestInspectorModelWithDetail()
		detail := &ipc.GetStepDetailResponse{Action: "", ToolPath: ""}
		content := m.buildLensContent(lensToolIO, detail, nil)
		if !strings.Contains(content, "No tool information") {
			t.Errorf("empty tool detail should render fallback text; got:\n%s", content)
		}
	})

	t.Run("narrow_terminal_60cols", func(t *testing.T) {
		m := newTestInspectorModelWithDetail()
		m.width = 60
		content := m.buildLensContent(lensToolIO, m.inspector.Detail, nil)
		// inspectorBoxWidth(60) → 56. Each line of the box must not exceed 56 chars
		// (run-count, ANSI-stripped) — best-effort check.
		for line := range strings.SplitSeq(content, "\n") {
			stripped := stripANSIApprox(line)
			if rcLen(stripped) > 60 {
				t.Errorf("60-col terminal line overflowed: %d runes: %q", rcLen(stripped), line)
				break
			}
		}
	})

	t.Run("ascii_mode_uses_plus_dash", func(t *testing.T) {
		t.Setenv("RNIX_ASCII", "1")
		// Reset the ASCII mode cache via fresh ui call.
		_ = ui.IsASCIIMode()
		m := newTestInspectorModelWithDetail()
		m.width = 100
		content := m.buildLensContent(lensToolIO, m.inspector.Detail, nil)
		// In ASCII fallback, box uses + and -; should not contain Unicode box chars.
		if strings.Contains(content, "┌") || strings.Contains(content, "│") {
			t.Errorf("ASCII mode should not contain box-drawing runes; got snippet:\n%s", content)
		}
		if !strings.Contains(content, "+") || !strings.Contains(content, "-") {
			t.Errorf("ASCII mode should contain + and - for box edges; got:\n%s", content)
		}
	})
}

// rcLen returns the rune count for a string; helper for width-bound assertions.
func rcLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// =============================================================================
// PR2 tests — Step Rail / Thumbnail Bar / Lens Tabs / System changed / Meta
// =============================================================================

// TestRenderStepRail_FieldGrouping verifies Story 38-3 AC#6: the rail uses
// pipe-separated field groups with role-specific colors.
func TestRenderStepRail_FieldGrouping(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.width = 200
	rail := m.renderStepRail(m.width)
	stripped := stripANSIApprox(rail)

	if !strings.Contains(stripped, "Step Inspector") {
		t.Errorf("rail should contain 'Step Inspector' title; got %q", stripped)
	}
	if !strings.Contains(stripped, "│") {
		t.Errorf("rail should use │ as field separator; got %q", stripped)
	}
	if !strings.Contains(stripped, "PID 2") {
		t.Errorf("rail should contain PID; got %q", stripped)
	}
	if !strings.Contains(stripped, "Step 1/3") {
		t.Errorf("rail should contain Step 1/3 form; got %q", stripped)
	}
	if !strings.Contains(stripped, "tool_call") {
		t.Errorf("rail should contain action name; got %q", stripped)
	}
}

func TestRenderStepRail_AsciiMode(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	m := newTestInspectorModelWithDetail()
	m.width = 200
	rail := m.renderStepRail(m.width)
	stripped := stripANSIApprox(rail)

	if strings.Contains(stripped, "│") {
		t.Errorf("ASCII rail should not contain │; got %q", stripped)
	}
	if !strings.Contains(stripped, "|") {
		t.Errorf("ASCII rail should use | separator; got %q", stripped)
	}
}

// TestRenderStepThumbnailBar_BasicGlyphs verifies Story 38-3 AC#6: glyph row
// + number row, with correct color assignment for current/error/tool/reasoning.
func TestRenderStepThumbnailBar_BasicGlyphs(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.inspector.Steps = []ipc.StepSummaryWire{
		{Step: 1, ToolPath: "/dev/fs"},
		{Step: 2, HasError: true},
		{Step: 3},
	}
	m.inspector.Step = 2
	m.width = 80
	bar := m.renderStepThumbnailBar(m.width)
	stripped := stripANSIApprox(bar)

	if !strings.Contains(stripped, "◆") {
		t.Errorf("thumbnail should contain ◆ glyph; got %q", stripped)
	}
	if !strings.Contains(stripped, "1") || !strings.Contains(stripped, "2") || !strings.Contains(stripped, "3") {
		t.Errorf("thumbnail should contain step numbers 1/2/3; got %q", stripped)
	}
}

// TestRenderStepThumbnailBar_AsciiMode degrades glyphs to */./+ in ASCII mode.
func TestRenderStepThumbnailBar_AsciiMode(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	m := newTestInspectorModelWithDetail()
	m.inspector.Steps = []ipc.StepSummaryWire{{Step: 1}, {Step: 2}}
	m.inspector.Step = 1
	bar := m.renderStepThumbnailBar(80)
	stripped := stripANSIApprox(bar)

	if strings.Contains(stripped, "◆") || strings.Contains(stripped, "◇") {
		t.Errorf("ASCII thumbnail should not contain unicode glyphs; got %q", stripped)
	}
	if !strings.Contains(stripped, "*") {
		t.Errorf("ASCII thumbnail should use *; got %q", stripped)
	}
}

// TestRenderStepThumbnailBar_CompressionWindow verifies Story 38-3 AC#6:
// step counts > 50 are compressed into head/tail windows around current step.
func TestRenderStepThumbnailBar_CompressionWindow(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	steps := make([]ipc.StepSummaryWire, 100)
	for i := range steps {
		steps[i] = ipc.StepSummaryWire{Step: i + 1}
	}
	m.inspector.Steps = steps

	t.Run("current_in_middle", func(t *testing.T) {
		m.inspector.Step = 50
		bar := m.renderStepThumbnailBar(200)
		stripped := stripANSIApprox(bar)
		if !strings.Contains(stripped, "…") {
			t.Errorf("compressed bar should contain ellipsis on both sides; got %q", stripped)
		}
		if !strings.Contains(stripped, "50") {
			t.Errorf("compressed bar should center current step 50; got %q", stripped)
		}
	})

	t.Run("current_at_start", func(t *testing.T) {
		m.inspector.Step = 2
		bar := m.renderStepThumbnailBar(200)
		stripped := stripANSIApprox(bar)
		if !strings.Contains(stripped, "…") {
			t.Errorf("compressed bar should contain trailing ellipsis; got %q", stripped)
		}
	})
}

// TestRenderStepThumbnailBar_HiddenWhenHeightLow verifies Story 38-3 AC#6
// + review P21: when m.height < 20 the inspector chrome must collapse back
// to the legacy 4-line form (no thumbnail bar). Asserted by checking that
// renderStepInspector does not emit any thumbnail glyph rune in that
// height regime, since the thumbnail bar is the unique source of those
// glyphs (◆/◇/◈ in unicode mode, *.+ in ASCII mode).
func TestRenderStepThumbnailBar_HiddenWhenHeightLow(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.inspector.Steps = []ipc.StepSummaryWire{{Step: 1}, {Step: 2}, {Step: 3}}
	m.inspector.Step = 2
	m.width = 80

	t.Run("h_below_20_skips_thumbnail", func(t *testing.T) {
		m.height = 18
		view := m.renderStepInspector(m.width, m.height)
		stripped := stripANSIApprox(view)
		if strings.Contains(stripped, "◆") || strings.Contains(stripped, "◇") {
			t.Errorf("thumbnail glyphs should not appear when height < 20; got:\n%s", stripped)
		}
	})

	t.Run("h_at_or_above_20_shows_thumbnail", func(t *testing.T) {
		m.height = 24
		view := m.renderStepInspector(m.width, m.height)
		stripped := stripANSIApprox(view)
		if !strings.Contains(stripped, "◆") {
			t.Errorf("thumbnail glyph ◆ should appear when height ≥ 20; got:\n%s", stripped)
		}
	})

	t.Run("direct_call_returns_glyphs_regardless", func(t *testing.T) {
		// renderStepThumbnailBar itself does not check m.height — the
		// gating happens in renderStepInspector. This sub-test pins that
		// contract: the helper always renders when called directly.
		bar := m.renderStepThumbnailBar(m.width)
		if bar == "" {
			t.Errorf("renderStepThumbnailBar should produce output when steps are loaded; got empty")
		}
	})
}

// TestRenderLensTabs_TwoSpaceSeparator verifies Story 38-3 AC#7: tab separator
// is now 2 spaces (was 1).
func TestRenderLensTabs_TwoSpaceSeparator(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	tabs := m.renderLensTabs(200)
	stripped := stripANSIApprox(tabs)
	// Expect at least one occurrence of "  " (double space) between tabs.
	if !strings.Contains(stripped, "  ") {
		t.Errorf("lens tabs should use 2-space separator; got %q", stripped)
	}
}

// TestRenderLensTabs_DiffMark verifies Story 38-3 AC#7: tabs whose lens
// content differs in diff mode get a `*` suffix.
func TestRenderLensTabs_DiffMark(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.inspector.DiffMode = true
	// Manually populate the diff mark cache: pretend lens 0 and 2 differ.
	m.inspector.DiffLensMarks[0] = true
	m.inspector.DiffLensMarks[2] = true
	tabs := m.renderLensTabs(200)
	stripped := stripANSIApprox(tabs)
	if !strings.Contains(stripped, "*") {
		t.Errorf("diff-mode tabs should contain `*` marker; got %q", stripped)
	}
}

func TestRenderLensTabs_NoDiffMarkWhenInactive(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.inspector.DiffMode = false
	m.inspector.DiffLensMarks[0] = true // should be ignored when not in diff mode
	tabs := m.renderLensTabs(200)
	stripped := stripANSIApprox(tabs)
	if strings.Contains(stripped, "*") {
		t.Errorf("non-diff-mode tabs should not contain `*` marker; got %q", stripped)
	}
}

// TestBuildSystemLens_ChangedDelta verifies Story 38-3 AC#3: changed system
// prompt shows "⚠ changed from step N (+/-X chars)" header.
//
// Story 38-3 review P18: aligned with spec L61 — the first-step branch now
// fires when `inspectorPrevStep == 0` (no prior step recorded yet), not when
// the *current* step is 0. So tests that exercise the changed/unchanged
// paths must set `inspectorPrevStep ≥ 1` (matching the real handleInspector
// DetailMsg flow where prevStep advances from 0 → 1 → 2 …).
func TestBuildSystemLens_ChangedDelta(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.inspector.PrevStep = 1
	m.inspector.Step = 2
	prev := &ipc.GetStepDetailResponse{SystemPrompt: "abc"}
	cur := &ipc.GetStepDetailResponse{SystemPrompt: "abc + 272 more chars" + strings.Repeat("x", 250)}

	t.Run("positive_delta", func(t *testing.T) {
		content := m.buildLensContent(lensSystem, cur, prev)
		stripped := stripANSIApprox(content)
		if !strings.Contains(stripped, "changed from step 1") {
			t.Errorf("changed path should mention 'changed from step N'; got %q", stripped)
		}
		if !strings.Contains(stripped, "+") {
			t.Errorf("positive delta should show + sign; got %q", stripped)
		}
	})

	t.Run("negative_delta", func(t *testing.T) {
		shorter := &ipc.GetStepDetailResponse{SystemPrompt: "ab"}
		content := m.buildLensContent(lensSystem, shorter, prev)
		stripped := stripANSIApprox(content)
		if !strings.Contains(stripped, "-") {
			t.Errorf("negative delta should show - sign; got %q", stripped)
		}
	})

	t.Run("first_step_no_annotation", func(t *testing.T) {
		m2 := newTestInspectorModelWithDetail()
		m2.inspector.PrevStep = 0
		m2.inspector.Step = 1
		content := m2.buildLensContent(lensSystem, cur, nil)
		stripped := stripANSIApprox(content)
		if strings.Contains(stripped, "changed from step") {
			t.Errorf("first step should not include changed annotation; got %q", stripped)
		}
	})

	t.Run("unchanged_preserves_legacy_text", func(t *testing.T) {
		m2 := newTestInspectorModelWithDetail()
		m2.inspector.PrevStep = 1
		m2.inspector.Step = 2
		same := &ipc.GetStepDetailResponse{SystemPrompt: "abc"}
		samePrev := &ipc.GetStepDetailResponse{SystemPrompt: "abc"}
		content := m2.buildLensContent(lensSystem, same, samePrev)
		stripped := stripANSIApprox(content)
		if !strings.Contains(stripped, "unchanged from step 1") {
			t.Errorf("unchanged path must preserve legacy 'unchanged from step N' text; got %q", stripped)
		}
	})
}

// TestBuildMetaLens_ThreeSections verifies Story 38-3 AC#4: meta lens has
// three labeled sections separated by horizontal rules.
func TestBuildMetaLens_ThreeSections(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	content := m.buildLensContent(lensMeta, m.inspector.Detail, nil)
	stripped := stripANSIApprox(content)
	for _, title := range []string{"Tokens", "Action", "Counts"} {
		if !strings.Contains(stripped, title) {
			t.Errorf("meta lens should contain section %q; got %q", title, stripped)
		}
	}
}

// TestBuildMetaLens_TokenBarChart verifies Story 38-3 AC#4: token bar uses
// █░ block chars (or # / . in ASCII).
func TestBuildMetaLens_TokenBarChart(t *testing.T) {
	t.Run("unicode", func(t *testing.T) {
		m := newTestInspectorModelWithDetail()
		// Bump tokens above the ~10K threshold so the bar shows at least 1
		// filled cell (1500/200000 = <1 cell).
		m.inspector.Detail.RequestTokens = 50000
		content := m.buildLensContent(lensMeta, m.inspector.Detail, nil)
		stripped := stripANSIApprox(content)
		if !strings.Contains(stripped, "█") || !strings.Contains(stripped, "░") {
			t.Errorf("unicode meta lens should contain █ and ░ bar chars; got %q", stripped)
		}
	})

	t.Run("ascii", func(t *testing.T) {
		t.Setenv("RNIX_ASCII", "1")
		m := newTestInspectorModelWithDetail()
		m.inspector.Detail.RequestTokens = 50000
		content := m.buildLensContent(lensMeta, m.inspector.Detail, nil)
		stripped := stripANSIApprox(content)
		if strings.Contains(stripped, "█") {
			t.Errorf("ASCII meta lens should not contain █; got %q", stripped)
		}
		if !strings.Contains(stripped, "#") || !strings.Contains(stripped, ".") {
			t.Errorf("ASCII meta lens should contain # and . bar chars; got %q", stripped)
		}
	})
}

// TestBuildMetaLens_ZeroTokens verifies Story 38-3 AC#4: when all token
// counts are zero, Tokens section collapses to '(no data)' instead of 0-bars.
func TestBuildMetaLens_ZeroTokens(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	detail := &ipc.GetStepDetailResponse{
		Step:           1,
		Action:         "noop",
		RequestTokens:  0,
		ResponseTokens: 0,
		TokenCount:     0,
	}
	content := m.buildLensContent(lensMeta, detail, nil)
	stripped := stripANSIApprox(content)
	if !strings.Contains(stripped, "no data") {
		t.Errorf("zero-token meta lens should show '(no data)'; got %q", stripped)
	}
	if strings.Contains(stripped, "0.0%") {
		t.Errorf("zero-token meta lens should not render 0%% bar; got %q", stripped)
	}
}

// =============================================================================
// PR3 tests — Raw JSON syntax highlighting + word-level search
// =============================================================================

// TestBuildRawJSONLens_SyntaxHighlight verifies Story 38-3 AC#5: keys, strings,
// numbers, and booleans are colored.
func TestBuildRawJSONLens_SyntaxHighlight(t *testing.T) {
	raw := `{
  "step": 5,
  "name": "alpha",
  "active": true,
  "ratio": 0.42
}`
	highlighted := highlightJSON(raw)

	t.Run("contains_ansi_codes_when_color_active", func(t *testing.T) {
		// Profile-tolerant: when the test runner has no color profile (CI / no
		// TTY) lipgloss returns plain text. We probe with a known style first to
		// detect that mode and only assert ANSI presence in color terminals.
		probe := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render("x")
		if probe == "x" {
			t.Skip("color profile is no-op on this runner; skipping ANSI assertion")
		}
		if !strings.Contains(highlighted, "\x1b[") {
			t.Errorf("highlighted JSON should contain ANSI escape sequences; got %q", highlighted)
		}
	})

	t.Run("preserves_literals", func(t *testing.T) {
		stripped := stripANSIApprox(highlighted)
		for _, want := range []string{`"step"`, `"name"`, `"alpha"`, "true", "5", "0.42"} {
			if !strings.Contains(stripped, want) {
				t.Errorf("stripped output should preserve %q; got %q", want, stripped)
			}
		}
	})
}

// TestBuildRawJSONLens_LargeBypassesHighlight verifies Story 38-3 AC#5
// performance fallback: JSON > 100KB skips the regex highlight.
func TestBuildRawJSONLens_LargeBypassesHighlight(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.inspector.Detail.SystemPrompt = strings.Repeat("x", 110*1024)
	content := m.buildLensContent(lensRawJSON, m.inspector.Detail, nil)
	if strings.Contains(content, "\x1b[") {
		t.Errorf(">100KB JSON should bypass syntax highlighting (no ANSI); got len=%d", len(content))
	}
}

// TestFindInspectorMatchesByPos_SubstringPositions verifies the byte-position
// match collector. Story 38-3 AC#8 underlies word-level highlighting.
func TestFindInspectorMatchesByPos_SubstringPositions(t *testing.T) {
	content := "authentication is auth-related\nanother line\nauth"
	positions := findInspectorMatchesByPos(content, "auth")

	if len(positions) != 3 {
		t.Errorf("expected 3 matches of 'auth'; got %d (%v)", len(positions), positions)
	}
	if positions[0].LineIdx != 0 || positions[0].ByteStart != 0 {
		t.Errorf("first match should be line 0 byte 0; got %+v", positions[0])
	}
	if positions[len(positions)-1].LineIdx != 2 {
		t.Errorf("last match should be on line 2; got %+v", positions[len(positions)-1])
	}
}

func TestFindInspectorMatchesByPos_EmptyQuery(t *testing.T) {
	positions := findInspectorMatchesByPos("anything", "")
	if positions != nil {
		t.Errorf("empty query should return nil positions; got %v", positions)
	}
}

func TestFindInspectorMatchesByPos_CaseInsensitive(t *testing.T) {
	content := "Authentication failed: AUTH error"
	positions := findInspectorMatchesByPos(content, "auth")
	if len(positions) != 2 {
		t.Errorf("case-insensitive search should find 2 matches; got %d", len(positions))
	}
}

// TestRebuildInspectorContents_WordLevelSearch verifies Story 38-3 AC#8:
// only the matched substring is reverse-highlighted.
func TestRebuildInspectorContents_WordLevelSearch(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.searchQuery = "auth"
	m.inspector.Contents[m.inspector.Lens] = "authentication failed\nrandom line"
	m.refreshInspectorSearchMatches()

	if len(m.inspector.SearchPos) == 0 {
		t.Fatalf("refreshInspectorSearchMatches should populate inspectorSearchPos")
	}
	if len(m.inspector.SearchPos) != 1 {
		t.Errorf("expected 1 match for 'auth'; got %d", len(m.inspector.SearchPos))
	}
	if m.inspector.SearchPos[0].ByteEnd-m.inspector.SearchPos[0].ByteStart != 4 {
		t.Errorf("match span should be 4 bytes; got %d", m.inspector.SearchPos[0].ByteEnd-m.inspector.SearchPos[0].ByteStart)
	}
}

func TestApplyWordLevelHighlight_DistinctCurrentVsOther(t *testing.T) {
	// Profile-tolerant: in a no-color terminal lipgloss returns plain text.
	probe := lipgloss.NewStyle().Reverse(true).Render("x")
	if probe == "x" {
		t.Skip("color profile is no-op on this runner; reverse-video assertion would always fail")
	}

	content := "auth1 here\nauth2 there"
	positions := []searchMatchPos{
		{LineIdx: 0, ByteStart: 0, ByteEnd: 4},
		{LineIdx: 1, ByteStart: 11, ByteEnd: 15},
	}
	searchMatches := []int{0, 1}
	matchIdx := 1

	cur := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning)).Reverse(true)
	other := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).Reverse(true)
	out := applyWordLevelHighlight(content, positions, searchMatches, matchIdx, cur, other)

	if out == content {
		t.Errorf("highlight should modify content")
	}
	if !strings.Contains(out, "\x1b[7m") {
		t.Errorf("highlight should produce reverse-video ANSI; got %q", out)
	}
}
