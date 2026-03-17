package config

import (
	"bufio"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxDotenvSize is the maximum size of a .env file (1MB).
const maxDotenvSize = 1 << 20

// validEnvNameRe matches safe RNIX_ENV values (alphanumeric, underscore, hyphen).
var validEnvNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidateEnvName checks that name is safe for use as an RNIX_ENV value.
// It rejects empty strings and path-traversal attempts.
func ValidateEnvName(name string) bool {
	return name != "" && validEnvNameRe.MatchString(name)
}

// ParseDotenv parses a .env file from the given reader.
// Supported syntax:
//   - KEY=VALUE (unquoted — value trimmed)
//   - KEY="VALUE" (double-quoted — supports \n, \", \\ escapes)
//   - KEY='VALUE' (single-quoted — literal, no escapes)
//   - KEY= (empty value)
//   - # comment lines and inline comments (unquoted values only)
//   - export KEY=VALUE (optional export prefix)
//   - blank lines ignored
//
// Variable interpolation ($VAR) is NOT supported.
// The reader is limited to maxDotenvSize bytes.
func ParseDotenv(r io.Reader) (map[string]string, error) {
	limited := io.LimitReader(r, maxDotenvSize)
	scanner := bufio.NewScanner(limited)
	vars := make(map[string]string)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Strip leading/trailing whitespace
		line = strings.TrimSpace(line)

		// Skip blank lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip optional "export " prefix
		if after, ok := strings.CutPrefix(line, "export "); ok {
			line = strings.TrimSpace(after)
		}

		// Split on first '='
		key, rawVal, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("dotenv line %d: missing '=' in %q", lineNum, line)
		}

		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("dotenv line %d: empty key", lineNum)
		}

		val, err := parseDotenvValue(rawVal)
		if err != nil {
			return nil, fmt.Errorf("dotenv line %d: %w", lineNum, err)
		}

		vars[key] = val
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("dotenv read: %w", err)
	}

	return vars, nil
}

// parseDotenvValue parses the value portion after '='.
func parseDotenvValue(raw string) (string, error) {
	raw = strings.TrimLeft(raw, " \t")

	if raw == "" {
		return "", nil
	}

	switch raw[0] {
	case '"':
		return parseDoubleQuoted(raw)
	case '\'':
		return parseSingleQuoted(raw)
	default:
		return parseUnquoted(raw), nil
	}
}

// parseDoubleQuoted handles "..." with escape sequences.
func parseDoubleQuoted(raw string) (string, error) {
	if len(raw) < 2 || raw[0] != '"' {
		return "", fmt.Errorf("invalid double-quoted value")
	}

	var sb strings.Builder
	i := 1 // skip opening quote
	for i < len(raw) {
		ch := raw[i]
		if ch == '"' {
			// closing quote found — verify only whitespace/comment follows
			tail := strings.TrimSpace(raw[i+1:])
			if tail != "" && !strings.HasPrefix(tail, "#") {
				return "", fmt.Errorf("unexpected content after closing quote: %q", raw[i+1:])
			}
			return sb.String(), nil
		}
		if ch == '\\' && i+1 < len(raw) {
			next := raw[i+1]
			switch next {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			default:
				sb.WriteByte('\\')
				sb.WriteByte(next)
			}
			i += 2
			continue
		}
		sb.WriteByte(ch)
		i++
	}

	return "", fmt.Errorf("unterminated double-quoted value")
}

// parseSingleQuoted handles '...' (literal, no escapes).
func parseSingleQuoted(raw string) (string, error) {
	if len(raw) < 2 || raw[0] != '\'' {
		return "", fmt.Errorf("invalid single-quoted value")
	}

	end := strings.IndexByte(raw[1:], '\'')
	if end < 0 {
		return "", fmt.Errorf("unterminated single-quoted value")
	}

	// Verify only whitespace/comment follows closing quote
	tail := strings.TrimSpace(raw[1+end+1:])
	if tail != "" && !strings.HasPrefix(tail, "#") {
		return "", fmt.Errorf("unexpected content after closing quote: %q", raw[1+end+1:])
	}

	return raw[1 : 1+end], nil
}

// parseUnquoted returns the value with trailing comments and whitespace stripped.
func parseUnquoted(raw string) string {
	// Strip inline comment (space + #)
	if idx := strings.Index(raw, " #"); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimRight(raw, " \t")
}

// LoadDotenvFile reads and parses a .env file.
// Returns (nil, nil) if the file does not exist.
func LoadDotenvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	return ParseDotenv(f)
}

// LoadDotenvDir loads .env files from dir in priority order:
//
//	.env → .env.local → .env.{rnixEnv} → .env.{rnixEnv}.local
//
// Later files override earlier values. rnixEnv defaults to "development" if empty.
// Returns only the variables defined in the .env files (not os.Environ).
func LoadDotenvDir(dir string, rnixEnv string) (map[string]string, error) {
	if rnixEnv == "" {
		rnixEnv = "development"
	}
	if !ValidateEnvName(rnixEnv) {
		return nil, fmt.Errorf("invalid RNIX_ENV value: %q", rnixEnv)
	}

	files := []string{
		filepath.Join(dir, ".env"),
		filepath.Join(dir, ".env.local"),
		filepath.Join(dir, ".env."+rnixEnv),
		filepath.Join(dir, ".env."+rnixEnv+".local"),
	}

	merged := make(map[string]string)
	for _, path := range files {
		vars, err := LoadDotenvFile(path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", path, err)
		}
		maps.Copy(merged, vars)
	}

	if len(merged) == 0 {
		return nil, nil
	}
	return merged, nil
}

// NewEnvLookup returns a lookup function that checks dotenvVars first,
// then falls back to os.Getenv. If dotenvVars is nil, returns os.Getenv directly.
func NewEnvLookup(dotenvVars map[string]string) func(string) string {
	if dotenvVars == nil {
		return os.Getenv
	}
	return func(key string) string {
		if v, ok := dotenvVars[key]; ok {
			return v
		}
		return os.Getenv(key)
	}
}
