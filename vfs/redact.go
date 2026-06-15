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
	"x-auth-token":        {},
	"cookie":              {},
	"set-cookie":          {},
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
