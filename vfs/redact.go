package vfs

import "maps"

// RedactHeaders returns a copy of h with credential-bearing values replaced
// by an irreversible fingerprint (Story 56.1 AC#3). Header key match is
// case-insensitive; the set covers Authorization / api-key / x-api-key /
// cookie (extensible). Non-credential headers are passed through unchanged.
//
// 56.1 RED skeleton: returns a shallow copy with no redaction applied.
// Dev-story fills the actual redaction; the safety-critical assertions in
// vfs/redact_test.go must fail until then.
func RedactHeaders(h map[string]string) map[string]string {
	out := make(map[string]string, len(h))
	maps.Copy(out, h)
	return out
}

// RedactCredential turns a raw credential string into an irreversible
// fingerprint of the form `redacted(len=N,prefix=...,sha256=...)`. Length
// and a short prefix help identify the credential class without leaking
// the secret; the sha256 prefix lets callers correlate identical secrets
// across log lines without revealing the plaintext.
//
// 56.1 RED skeleton: returns the input verbatim. Dev-story implements the
// fingerprint; tests assert plaintext is *not* present in the output.
func RedactCredential(s string) string {
	return s
}
