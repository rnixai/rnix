package vfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// credentialHeaderSet enumerates header keys that carry credentials and must
// have their values fingerprinted by RedactHeaders. Match is case-insensitive
// on the key (lookup uses strings.ToLower(key)).
var credentialHeaderSet = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"api-key":             {},
	"x-api-key":           {},
	"x-goog-api-key":      {}, // Gemini SDK (genai) ships API key under this key.
	"x-auth-token":        {},
	"cookie":              {},
	"set-cookie":          {},
}

// sensitiveArgFlagSet enumerates CLI argv flags whose adjacent value carries a
// credential. RedactArgv (Story 56.3) walks argv looking for these flags in
// either the `--flag value` form (token + next token) or the `--flag=value`
// form (single token with `=`). Match is case-insensitive on the flag name.
//
// Reasoning-effort flags (`--effort`, `-c model_reasoning_effort=...`) are
// **not** in this set — they carry transparency-critical real values that the
// raw capture is meant to surface (CAP-1).
var sensitiveArgFlagSet = map[string]struct{}{
	"--api-key":      {},
	"--apikey":       {},
	"--token":        {},
	"--auth-token":   {},
	"--password":     {},
	"--secret":       {},
	"--bearer":       {},
	"--access-key":   {},
	"--access-token": {},
	"-h":             {}, // header flag (curl-style); value form "Authorization: ..."
	"--header":       {},
}

// redactedPrefix marks the start of an already-redacted fingerprint string;
// RedactCredential checks it to stay idempotent (组合矩阵第 5 行).
const redactedPrefix = "redacted("

// RedactHeaders returns a copy of h with credential-bearing values replaced
// by an irreversible fingerprint (Story 56.1 AC#3). Header key match is
// case-insensitive; the set covers Authorization / api-key / x-api-key /
// cookie (extensible). Non-credential headers are passed through unchanged.
//
// Bearer-style values keep their scheme token to preserve operational signal
// ("Bearer ..." stays recognizable) while the secret tail is fingerprinted.
func RedactHeaders(h map[string]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if _, ok := credentialHeaderSet[strings.ToLower(k)]; !ok {
			out[k] = v
			continue
		}
		out[k] = redactHeaderValue(v)
	}
	return out
}

// redactHeaderValue redacts a single credential-bearing header value.
// "Bearer <token>" / "Basic <token>" → "<scheme> <fingerprint>" so the
// auth scheme is still visible; bare credential strings are fingerprinted
// in their entirety.
func redactHeaderValue(v string) string {
	if v == "" {
		return v
	}
	// Detect "<scheme> <secret>" and only redact the secret tail.
	if idx := strings.IndexByte(v, ' '); idx > 0 && idx < len(v)-1 {
		scheme := v[:idx]
		secret := strings.TrimSpace(v[idx+1:])
		if isAuthScheme(scheme) && secret != "" {
			return scheme + " " + RedactCredential(secret)
		}
	}
	return RedactCredential(v)
}

// isAuthScheme returns true for the common HTTP authentication scheme tokens
// that prefix credential headers ("Bearer", "Basic", "Token", "ApiKey").
func isAuthScheme(s string) bool {
	switch strings.ToLower(s) {
	case "bearer", "basic", "token", "apikey", "digest":
		return true
	}
	return false
}

// RedactCredential turns a raw credential string into an irreversible
// fingerprint of the form `redacted(len=N,prefix=...,sha256=...)`. Length
// and a short prefix help identify the credential class without leaking
// the secret; the sha256 prefix lets callers correlate identical secrets
// across log lines without revealing the plaintext.
//
// Idempotent: an already-redacted string (starts with `redacted(`) is
// returned unchanged so kernel纵深防御二次脱敏不会产生嵌套指纹（组合矩阵第 5 行）。
func RedactCredential(s string) string {
	if s == "" {
		return s
	}
	if strings.HasPrefix(s, redactedPrefix) {
		return s
	}
	// Hash full plaintext; expose only the first 12 hex chars so the digest is
	// non-reversible by brute force but still distinguishes different secrets.
	sum := sha256.Sum256([]byte(s))
	hash := hex.EncodeToString(sum[:])[:12]

	// Reveal at most a 3-byte prefix and only when the credential is long
	// enough that the prefix does not approximate the whole secret.
	const prefixBytes = 3
	prefix := ""
	if len(s) >= prefixBytes*3 {
		prefix = s[:prefixBytes]
	}

	return fmt.Sprintf("%slen=%d,prefix=%s,sha256=%s)", redactedPrefix, len(s), prefix, hash)
}

// RedactArgv returns a copy of argv with credential-bearing values fingerprinted
// (Story 56.3 driver-layer primary redaction for CLI raw capture).
//
// Walks argv element-by-element, recognizing two forms:
//
//  1. `--flag value`     — sensitiveArgFlagSet hit ⇒ next element fingerprinted.
//  2. `--flag=value`     — split on first `=`; if the flag head matches the
//     set, the tail (after `=`) is fingerprinted.
//
// Special-case for `-H` / `--header`: the value carries an HTTP header line
// like `Authorization: Bearer sk-xxx`. We delegate to redactHeaderValue so
// the credentialHeaderSet logic stays the single source of truth.
//
// Non-sensitive flags (`--effort`, `-c model_reasoning_effort=…`, `--model`,
// `--output-format`, etc.) pass through unchanged. Bare positional arguments
// (e.g. the prompt text appended by codex/cursor) are also untouched —
// stdin/positional prompt redaction is explicitly out of scope (story裁决 4 stdin 不脱敏).
//
// Idempotent: re-running on already-redacted output is a no-op because
// RedactCredential / redactHeaderValue both short-circuit on the
// `redacted(` prefix.
func RedactArgv(argv []string) []string {
	if len(argv) == 0 {
		return argv
	}
	out := make([]string, len(argv))
	copy(out, argv)
	i := 0
	for i < len(out) {
		tok := out[i]

		// Form 2: --flag=value (single-token).
		if eq := strings.IndexByte(tok, '='); eq > 0 {
			head := tok[:eq]
			tail := tok[eq+1:]
			if isSensitiveArgFlag(head) {
				out[i] = head + "=" + redactArgValue(head, tail)
			}
			i++
			continue
		}

		// Form 1: --flag value (two tokens).
		if isSensitiveArgFlag(tok) && i+1 < len(out) {
			out[i+1] = redactArgValue(tok, out[i+1])
			i += 2
			continue
		}

		i++
	}
	return out
}

// isSensitiveArgFlag reports whether name matches a sensitiveArgFlagSet entry
// case-insensitively.
func isSensitiveArgFlag(name string) bool {
	if name == "" {
		return false
	}
	_, ok := sensitiveArgFlagSet[strings.ToLower(name)]
	return ok
}

// redactArgValue fingerprints the value attached to a sensitive flag. For
// header-style flags (`-H` / `--header`) the value is an HTTP header line, so
// we route through redactHeaderValue; for everything else we fingerprint the
// whole value.
func redactArgValue(flag, value string) string {
	switch strings.ToLower(flag) {
	case "-h", "--header":
		return redactHeaderLine(value)
	}
	return RedactCredential(value)
}

// redactHeaderLine handles the curl-style `-H "Name: Value"` form: split on
// the first colon, look up the name in credentialHeaderSet (so the same rule
// set governs argv `-H` and HTTP headers), and fingerprint just the value
// portion. Non-credential header lines pass through unchanged.
func redactHeaderLine(line string) string {
	if line == "" {
		return line
	}
	idx := strings.IndexByte(line, ':')
	if idx <= 0 || idx >= len(line)-1 {
		// Not a `Name: Value` form — fingerprint conservatively.
		return RedactCredential(line)
	}
	name := strings.TrimSpace(line[:idx])
	value := strings.TrimSpace(line[idx+1:])
	if _, ok := credentialHeaderSet[strings.ToLower(name)]; !ok {
		return line
	}
	return name + ": " + redactHeaderValue(value)
}
