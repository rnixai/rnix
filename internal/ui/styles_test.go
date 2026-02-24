package ui

import (
	"strings"
	"testing"
)

func TestInitStyles_NoColor(t *testing.T) {
	profile := TerminalProfile{Width: 80, IsTTY: false, ColorLevel: 0, IsUnicode: true}
	InitStyles(profile)

	// With no color, styled text should not contain ANSI escape sequences
	text := KernelStyle.Render("test")
	if strings.Contains(text, "\x1b[") {
		t.Error("KernelStyle should not contain ANSI codes with ColorLevel=0")
	}

	text = AgentStyle.Render("test")
	if strings.Contains(text, "\x1b[") {
		t.Error("AgentStyle should not contain ANSI codes with ColorLevel=0")
	}

	text = SuccessStyle.Render("test")
	if strings.Contains(text, "\x1b[") {
		t.Error("SuccessStyle should not contain ANSI codes with ColorLevel=0")
	}

	text = ErrorStyle.Render("test")
	if strings.Contains(text, "\x1b[") {
		t.Error("ErrorStyle should not contain ANSI codes with ColorLevel=0")
	}

	text = WarningStyle.Render("test")
	if strings.Contains(text, "\x1b[") {
		t.Error("WarningStyle should not contain ANSI codes with ColorLevel=0")
	}

	text = MutedStyle.Render("test")
	if strings.Contains(text, "\x1b[") {
		t.Error("MutedStyle should not contain ANSI codes with ColorLevel=0")
	}
}

func TestInitStyles_WithColor(t *testing.T) {
	profile := TerminalProfile{Width: 80, IsTTY: true, ColorLevel: 3, IsUnicode: true}
	InitStyles(profile)

	// With color, styled text should contain color information
	// lipgloss may or may not embed ANSI codes depending on the output writer,
	// but the style should be configured with a foreground color.
	// We verify that InitStyles doesn't panic and the styles are functional.
	text := KernelStyle.Render("kernel")
	if text == "" {
		t.Error("KernelStyle.Render should produce non-empty output")
	}

	text = AgentStyle.Render("agent")
	if text == "" {
		t.Error("AgentStyle.Render should produce non-empty output")
	}
}

func TestInitStyles_ColorConstants(t *testing.T) {
	// Verify color constants match the design spec
	if ColorKernel != "#888888" {
		t.Errorf("ColorKernel: got %s, want #888888", ColorKernel)
	}
	if ColorAgent != "#5B9BD5" {
		t.Errorf("ColorAgent: got %s, want #5B9BD5", ColorAgent)
	}
	if ColorSuccess != "#6BCB77" {
		t.Errorf("ColorSuccess: got %s, want #6BCB77", ColorSuccess)
	}
	if ColorWarning != "#FFD93D" {
		t.Errorf("ColorWarning: got %s, want #FFD93D", ColorWarning)
	}
	if ColorError != "#FF6B6B" {
		t.Errorf("ColorError: got %s, want #FF6B6B", ColorError)
	}
	if ColorMuted != "#666666" {
		t.Errorf("ColorMuted: got %s, want #666666", ColorMuted)
	}
}
