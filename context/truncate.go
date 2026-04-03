package context

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TruncateResult truncates content if it exceeds maxTokens.
// Returns the (possibly truncated) content and whether truncation occurred.
// If maxTokens <= 0, no truncation is performed.
func TruncateResult(content string, maxTokens int) (string, bool) {
	if maxTokens <= 0 || len(content) == 0 {
		return content, false
	}

	tokens := EstimateTokens(content)
	if tokens <= maxTokens {
		return content, false
	}

	// Binary search for the cut point that fits within maxTokens.
	// Start with a rough estimate based on the ratio.
	runes := []rune(content)
	ratio := float64(maxTokens) / float64(tokens)
	cutIdx := int(float64(len(runes)) * ratio)
	cutIdx = min(cutIdx, len(runes))

	// Refine: ensure we don't exceed maxTokens
	truncated := string(runes[:cutIdx])
	for EstimateTokens(truncated) > maxTokens && cutIdx > 0 {
		cutIdx -= max(cutIdx/10, 1)
		if cutIdx < 0 {
			cutIdx = 0
		}
		truncated = string(runes[:cutIdx])
	}

	return truncated, true
}

// FormatTruncationNotice returns a truncation notice message.
func FormatTruncationNotice(originalTokens, shownTokens int, overflowPath string) string {
	if overflowPath != "" {
		return fmt.Sprintf("\n[Truncated: original %d tokens, showing first %d tokens. Full content saved to %s]",
			originalTokens, shownTokens, overflowPath)
	}
	return fmt.Sprintf("\n[Truncated: original %d tokens, showing first %d tokens]",
		originalTokens, shownTokens)
}

// EndTruncatingAccumulator truncates long output by keeping the first headLines
// and last tailLines, inserting a truncation notice in between.
// If the content has fewer lines than headLines+tailLines, it is returned as-is.
func EndTruncatingAccumulator(content string, maxChars int, headLines int, tailLines int) (string, bool) {
	if maxChars <= 0 || len(content) <= maxChars {
		return content, false
	}

	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	if totalLines <= headLines+tailLines {
		return content, false
	}

	head := lines[:headLines]
	tail := lines[totalLines-tailLines:]
	truncatedCount := totalLines - headLines - tailLines

	notice := fmt.Sprintf("\n... [%d lines truncated, showing first %d + last %d lines] ...\n",
		truncatedCount, headLines, tailLines)

	result := strings.Join(head, "\n") + notice + strings.Join(tail, "\n")
	return result, true
}

// WriteOverflow writes content to an overflow file under the given base directory.
// Returns the path to the overflow file.
// The filename is derived from the SHA256 hash of the content (first 16 hex chars).
func WriteOverflow(content string, baseDir string) (string, error) {
	overflowDir := filepath.Join(baseDir, ".rnix", "data", "overflow")
	if err := os.MkdirAll(overflowDir, 0o755); err != nil {
		return "", fmt.Errorf("create overflow dir: %w", err)
	}

	hash := sha256.Sum256([]byte(content))
	filename := fmt.Sprintf("%x", hash[:8]) // first 16 hex chars (8 bytes)
	path := filepath.Join(overflowDir, filename)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write overflow file: %w", err)
	}

	return path, nil
}
