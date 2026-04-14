package memory

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// ScanResult describes why content was rejected by the security scanner.
type ScanResult struct {
	Rejected bool
	Category string // "prompt_injection", "hidden_unicode", "exfiltration", "role_hijack"
	Reason   string
	Match    string // the matched substring
}

// scanner holds precompiled regex patterns for security scanning.
var scanner = struct {
	promptInjection []*regexp.Regexp
	exfiltration    []*regexp.Regexp
	roleHijack      []*regexp.Regexp
}{
	promptInjection: compileAll(
		`(?i)ignore\s+(all\s+)?previous\s+instructions`,
		`(?i)disregard\s+(all\s+)?(previous|above|prior)`,
		`(?i)system\s+prompt\s+override`,
		`(?i)forget\s+(all\s+)?(previous|your)\s+(instructions|rules)`,
		`(?i)new\s+instructions?\s*:`,
		`(?i)override\s+(system|safety|security)`,
	),
	exfiltration: compileAll(
		`(?i)curl\s+.*\$`,
		`(?i)wget\s+.*\$`,
		`(?i)cat\s+\.env`,
		`(?i)curl\s+.*api[_-]?key`,
		`(?i)wget\s+.*api[_-]?key`,
		`(?i)curl\s+.*secret`,
		`(?i)curl\s+.*token`,
	),
	roleHijack: compileAll(
		`(?i)you\s+are\s+now`,
		`(?i)act\s+as\s+if`,
		`(?i)pretend\s+to\s+be`,
		`(?i)from\s+now\s+on\s+you\s+are`,
		`(?i)assume\s+the\s+role\s+of`,
	),
}

func compileAll(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		out[i] = regexp.MustCompile(p)
	}
	return out
}

// hiddenUnicodeCodepoints that are invisible and can be used for injection.
var hiddenUnicodeCodepoints = map[rune]string{
	'\u200B': "ZERO WIDTH SPACE",
	'\u200C': "ZERO WIDTH NON-JOINER",
	'\u200D': "ZERO WIDTH JOINER",
	'\u2060': "WORD JOINER",
	'\uFEFF': "ZERO WIDTH NO-BREAK SPACE (BOM)",
	'\u200E': "LEFT-TO-RIGHT MARK",
	'\u200F': "RIGHT-TO-LEFT MARK",
	'\u00AD': "SOFT HYPHEN",
	'\u2061': "FUNCTION APPLICATION",
	'\u2062': "INVISIBLE TIMES",
	'\u2063': "INVISIBLE SEPARATOR",
	'\u2064': "INVISIBLE PLUS",
}

// ScanContent checks content for security threats. Returns a ScanResult
// indicating whether the content was rejected and why.
func ScanContent(content string) ScanResult {
	// 1. Check hidden Unicode
	for _, r := range content {
		if name, ok := hiddenUnicodeCodepoints[r]; ok {
			return ScanResult{
				Rejected: true,
				Category: "hidden_unicode",
				Reason:   fmt.Sprintf("security scan rejected: hidden Unicode character %s (U+%04X)", name, r),
				Match:    string(r),
			}
		}
		// Also reject Unicode control characters in Cc category (except normal whitespace)
		if unicode.Is(unicode.Cc, r) && r != '\n' && r != '\r' && r != '\t' {
			return ScanResult{
				Rejected: true,
				Category: "hidden_unicode",
				Reason:   fmt.Sprintf("security scan rejected: control character U+%04X", r),
				Match:    string(r),
			}
		}
	}

	// 2. Check prompt injection
	for _, re := range scanner.promptInjection {
		if loc := re.FindString(content); loc != "" {
			return ScanResult{
				Rejected: true,
				Category: "prompt_injection",
				Reason:   fmt.Sprintf("security scan rejected: prompt injection pattern detected (%q)", truncate(loc, 60)),
				Match:    loc,
			}
		}
	}

	// 3. Check exfiltration
	for _, re := range scanner.exfiltration {
		if loc := re.FindString(content); loc != "" {
			return ScanResult{
				Rejected: true,
				Category: "exfiltration",
				Reason:   fmt.Sprintf("security scan rejected: exfiltration command detected (%q)", truncate(loc, 60)),
				Match:    loc,
			}
		}
	}

	// 4. Check role hijack
	for _, re := range scanner.roleHijack {
		if loc := re.FindString(content); loc != "" {
			return ScanResult{
				Rejected: true,
				Category: "role_hijack",
				Reason:   fmt.Sprintf("security scan rejected: role hijack pattern detected (%q)", truncate(loc, 60)),
				Match:    loc,
			}
		}
	}

	return ScanResult{Rejected: false}
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return strings.TrimSpace(string(runes[:maxLen])) + "..."
}
