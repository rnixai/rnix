package llm

import (
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// Story 55.2 — DriverInfo.ReasoningEffort surfaces each driver's configured
// reasoning effort verbatim so the kernel can snapshot it onto the process for
// display (dashboard / CLI summary / proc-info.json). cursor-cli / qwen-cli
// have no effort concept (55.1 no-op) → always empty.

// Compile-time guard: LLMFile must satisfy vfs.ReasoningEffortProvider, the
// interface the kernel spawn path type-asserts to backfill proc.ReasoningEffort.
var _ vfs.ReasoningEffortProvider = (*LLMFile)(nil)

func TestDriverInfo_ReasoningEffort_Anthropic(t *testing.T) {
	d := NewAnthropicDriver("test", WithAnthropicEffort("high"))
	if got := d.Info().ReasoningEffort; got != "high" {
		t.Errorf("Info().ReasoningEffort = %q, want %q", got, "high")
	}
}

func TestDriverInfo_ReasoningEffort_Empty_NoEffort(t *testing.T) {
	d := NewAnthropicDriver("test")
	if got := d.Info().ReasoningEffort; got != "" {
		t.Errorf("Info().ReasoningEffort = %q, want empty", got)
	}
}

func TestDriverInfo_ReasoningEffort_CursorQwen_AlwaysEmpty(t *testing.T) {
	// cursor-cli / qwen-cli expose no effort option → Info() must report empty
	// so the display layer renders "—" rather than a stale or fabricated value.
	if got := NewCursorCliDriver().Info().ReasoningEffort; got != "" {
		t.Errorf("cursor Info().ReasoningEffort = %q, want empty", got)
	}
	if got := NewQwenCliDriver().Info().ReasoningEffort; got != "" {
		t.Errorf("qwen Info().ReasoningEffort = %q, want empty", got)
	}
}

func TestLLMFile_ReasoningEffort_DelegatesToDriver(t *testing.T) {
	// LLMFile implements vfs.ReasoningEffortProvider by delegating to the
	// driver's Info(); the kernel uses this at spawn to backfill the process.
	f := &LLMFile{driver: NewAnthropicDriver("test", WithAnthropicEffort("max")), devicePath: "/dev/llm/test"}
	if got := f.ReasoningEffort(); got != "max" {
		t.Errorf("LLMFile.ReasoningEffort() = %q, want %q", got, "max")
	}
	f2 := &LLMFile{driver: NewAnthropicDriver("test"), devicePath: "/dev/llm/test"}
	if got := f2.ReasoningEffort(); got != "" {
		t.Errorf("LLMFile.ReasoningEffort() = %q, want empty", got)
	}
}
