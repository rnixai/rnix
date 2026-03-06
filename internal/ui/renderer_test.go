package ui

import (
	"bytes"
	"os"
	"testing"
)

func TestDetectProfile_NonTTY(t *testing.T) {
	var buf bytes.Buffer
	p := DetectProfile(&buf)

	if p.IsTTY {
		t.Error("expected IsTTY=false for bytes.Buffer")
	}
	if p.ColorLevel != 0 {
		t.Errorf("expected ColorLevel=0 for non-TTY, got %d", p.ColorLevel)
	}
	if p.Width != 80 {
		t.Errorf("expected default Width=80, got %d", p.Width)
	}
	if !p.IsUnicode {
		t.Error("expected IsUnicode=true by default")
	}
}

func TestDetectProfile_NoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	p := DetectProfile(&buf)

	if p.ColorLevel != 0 {
		t.Errorf("expected ColorLevel=0 with NO_COLOR set, got %d", p.ColorLevel)
	}
}

func TestDetectProfile_RnixASCII(t *testing.T) {
	t.Run("RNIX_ASCII=1", func(t *testing.T) {
		t.Setenv("RNIX_ASCII", "1")
		var buf bytes.Buffer
		p := DetectProfile(&buf)

		if p.IsUnicode {
			t.Error("expected IsUnicode=false with RNIX_ASCII=1")
		}
	})

	t.Run("RNIX_ASCII=true", func(t *testing.T) {
		t.Setenv("RNIX_ASCII", "true")
		var buf bytes.Buffer
		p := DetectProfile(&buf)

		if p.IsUnicode {
			t.Error("expected IsUnicode=false with RNIX_ASCII=true")
		}
	})

	t.Run("RNIX_ASCII=0", func(t *testing.T) {
		t.Setenv("RNIX_ASCII", "0")
		var buf bytes.Buffer
		p := DetectProfile(&buf)

		if !p.IsUnicode {
			t.Error("expected IsUnicode=true with RNIX_ASCII=0")
		}
	})
}

func TestDetectProfile_DevNull(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("cannot open /dev/null: %v", err)
	}
	defer f.Close()

	p := DetectProfile(f)
	// /dev/null is not a TTY
	if p.IsTTY {
		t.Error("expected IsTTY=false for /dev/null")
	}
}

func TestNewRenderer_DefaultMode(t *testing.T) {
	var buf bytes.Buffer
	r := NewRenderer(&buf, ModeDefault)

	if r.OutputMode != ModeDefault {
		t.Errorf("expected ModeDefault, got %d", r.OutputMode)
	}
	if r.Writer != &buf {
		t.Error("Writer mismatch")
	}
}

func TestNewRenderer_JSONMode(t *testing.T) {
	var buf bytes.Buffer
	r := NewRenderer(&buf, ModeJSON)

	if r.Profile.ColorLevel != 0 {
		t.Errorf("expected ColorLevel=0 in JSON mode, got %d", r.Profile.ColorLevel)
	}
}

func TestNewRenderer_Modes(t *testing.T) {
	modes := []OutputMode{ModeDefault, ModeQuiet, ModeVerbose, ModeJSON}
	for _, m := range modes {
		var buf bytes.Buffer
		r := NewRenderer(&buf, m)
		if r.OutputMode != m {
			t.Errorf("expected mode %d, got %d", m, r.OutputMode)
		}
	}
}
