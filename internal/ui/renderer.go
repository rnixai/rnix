// Package ui provides terminal UI components for Rnix CLI.
package ui

import (
	"io"
	"os"

	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// OutputMode determines the verbosity and format of CLI output.
type OutputMode int

const (
	ModeDefault OutputMode = iota
	ModeQuiet
	ModeVerbose
	ModeJSON
)

// TerminalProfile describes the capabilities of the output terminal.
type TerminalProfile struct {
	Width     int
	IsTTY     bool
	ColorLevel int // 0=none, 1=16-color, 2=256-color, 3=truecolor
	IsUnicode bool
}

// DetectProfile inspects the given writer and environment to build a TerminalProfile.
func DetectProfile(w io.Writer) TerminalProfile {
	p := TerminalProfile{
		Width:     80,
		IsTTY:     false,
		ColorLevel: 3,
		IsUnicode: true,
	}

	// Check if writer is a file (needed for TTY/width detection)
	if f, ok := w.(*os.File); ok {
		fd := int(f.Fd())
		p.IsTTY = isatty.IsTerminal(uintptr(fd)) || isatty.IsCygwinTerminal(uintptr(fd))
		if width, _, err := term.GetSize(fd); err == nil && width > 0 {
			p.Width = width
		}
	}

	// NO_COLOR environment variable disables all colors
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		p.ColorLevel = 0
	}

	// Non-TTY defaults to no color
	if !p.IsTTY {
		p.ColorLevel = 0
	}

	// RNIX_ASCII disables Unicode symbols
	if v := os.Getenv("RNIX_ASCII"); v == "1" || v == "true" {
		p.IsUnicode = false
	}

	return p
}

// Renderer manages styled output to a writer using a detected terminal profile.
type Renderer struct {
	Profile    TerminalProfile
	Writer     io.Writer
	OutputMode OutputMode
}

// NewRenderer creates a Renderer for the given writer and output mode.
func NewRenderer(w io.Writer, mode OutputMode) *Renderer {
	profile := DetectProfile(w)
	// JSON mode forces no color
	if mode == ModeJSON {
		profile.ColorLevel = 0
	}
	return &Renderer{
		Profile:    profile,
		Writer:     w,
		OutputMode: mode,
	}
}
