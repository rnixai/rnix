package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Story 53.4 AC2: using-rnix is an outward-facing capability contract. Keep the
// documented CLI reference tied to the real Cobra command tree so invented or
// stale subcommands cannot ship unnoticed.
func TestATDD_53_4_CLIReferenceCommandsExist(t *testing.T) {
	data, err := os.ReadFile("../../lib/skills/using-rnix/references/cli-reference.md")
	if err != nil {
		t.Fatalf("read cli-reference.md: %v", err)
	}

	refs := extractRnixCommandRefs53_4(string(data))
	if len(refs) < 20 {
		t.Fatalf("extracted only %d rnix command references; parser likely missed the document", len(refs))
	}

	seen := map[string]string{}
	for _, ref := range refs {
		for _, path := range rnixCommandPathVariants53_4(ref) {
			if len(path) == 0 {
				continue
			}
			key := strings.Join(path, " ")
			seen[key] = ref
			if !cobraPathExists53_4(rootCmd, path) {
				t.Errorf("cli-reference documents %q from %q, but Cobra command path does not exist", key, ref)
			}
		}
	}

	for _, required := range []string{
		"ps", "kill", "strace", "suspend", "resume",
		"compose up", "compose down", "compose resume",
		"apply", "intent list", "intent status",
		"skill list", "mcp list", "daemon start", "run",
	} {
		if _, ok := seen[required]; !ok {
			t.Errorf("cli-reference no longer documents required command path %q", required)
		}
	}
}

func extractRnixCommandRefs53_4(markdown string) []string {
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		seen[s] = struct{}{}
	}

	inline := regexp.MustCompile("`(rnix(?:\\s+[^`]+)?)`")
	for _, match := range inline.FindAllStringSubmatch(markdown, -1) {
		add(match[1])
	}

	for line := range strings.Lines(markdown) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "rnix ") || line == "rnix" {
			add(line)
		}
	}

	refs := make([]string, 0, len(seen))
	for ref := range seen {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

func rnixCommandPathVariants53_4(ref string) [][]string {
	ref = strings.TrimSpace(strings.TrimPrefix(ref, "rnix"))
	if ref == "" {
		return nil
	}
	if idx := strings.Index(ref, "#"); idx >= 0 {
		ref = ref[:idx]
	}

	fields := strings.Fields(ref)
	variants := [][]string{{}}
	for _, field := range fields {
		field = strings.Trim(field, "`.,;)")
		if field == "" {
			continue
		}
		if strings.HasPrefix(field, "-") ||
			strings.HasPrefix(field, "<") ||
			strings.HasPrefix(field, "[") ||
			strings.HasPrefix(field, "\"") ||
			strings.HasPrefix(field, "'") {
			break
		}

		parts := strings.Split(field, "|")
		next := make([][]string, 0, len(variants)*len(parts))
		for _, variant := range variants {
			for _, part := range parts {
				part = strings.Trim(part, "[]<>.,;()")
				if part == "" {
					continue
				}
				copyVariant := append([]string(nil), variant...)
				copyVariant = append(copyVariant, part)
				next = append(next, copyVariant)
			}
		}
		if len(next) == 0 {
			break
		}
		variants = next
	}

	out := variants[:0]
	for _, variant := range variants {
		if len(variant) > 0 {
			out = append(out, variant)
		}
	}
	return out
}

func cobraPathExists53_4(root *cobra.Command, path []string) bool {
	current := root
	for _, part := range path {
		var next *cobra.Command
		for _, child := range current.Commands() {
			if child.Name() == part {
				next = child
				break
			}
		}
		if next == nil {
			return false
		}
		current = next
	}
	return true
}
