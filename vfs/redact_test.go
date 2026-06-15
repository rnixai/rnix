package vfs

import (
	"strings"
	"testing"
)

// ============================================================================
// ATDD Story 56.1 — vfs.RedactHeaders / vfs.RedactCredential (AC#3)
//
// 56-1-UNIT-001..004. P0 安全红线：明文凭据零落盘是 Epic 56 CAP-4 "默认开"
// 的对偶约束 — 默认开启意味着默认会写到磁盘 → 脱敏必须默认且可靠生效。
//
// RED 机制：源码 `vfs/redact.go` 留有透传骨架（返回入参不变），单测直接断言
// 明文不在输出/指纹存在/幂等/非凭据透传，骨架未实现脱敏时会真实失败。
// dev-story 阶段填充指纹算法即转 GREEN。无 t.Skip — 这是 P0 安全断言。
// ============================================================================

// 56-1-UNIT-001 (P0): credential header 明文必须零泄漏，匹配大小写不敏感。
func TestATDD_56_1_001_RedactHeaders_NoPlaintextLeak(t *testing.T) {
	const (
		authSecret   = "sk-prod-A1B2C3D4E5F6"
		apiKeySecret = "ak_live_XYZ_TOKEN_99887766"
		cookieSecret = "session=eyJhbGciOiJIUzI1NiJ9.payload.sig"
	)
	in := map[string]string{
		// 不同大小写形式都要被识别为凭据
		"Authorization": "Bearer " + authSecret,
		"api-key":       apiKeySecret,
		"X-API-Key":     apiKeySecret + "-second",
		"Cookie":        cookieSecret,
		// 非凭据 header — 不脱敏
		"Content-Type": "application/json",
		"User-Agent":   "rnix/test",
	}

	out := RedactHeaders(in)

	// 凭据明文绝不能出现在任何 value 中
	for outKey, outVal := range out {
		for _, secret := range []string{authSecret, apiKeySecret, cookieSecret} {
			if strings.Contains(outVal, secret) {
				t.Errorf("plaintext credential leaked: header=%q value=%q contains %q",
					outKey, outVal, secret)
			}
		}
	}
}

// 56-1-UNIT-002 (P0): 凭据脱敏后必须保留指纹 (len + prefix + sha256 前缀)，
// 既能识别凭据形态又不可逆推原值。
func TestATDD_56_1_002_RedactCredential_FingerprintPreserved(t *testing.T) {
	const secret = "sk-prod-A1B2C3D4E5F6G7H8" // len=23
	out := RedactCredential(secret)

	if strings.Contains(out, secret) {
		t.Fatalf("RedactCredential leaked plaintext: %q", out)
	}
	// 指纹形态：至少包含 len/prefix/sha256 三种线索之一（具体格式由 dev-story 决定，
	// 但任何"无指纹"的实现都应当失败这条断言）
	hasLen := strings.Contains(out, "len=")
	hasPrefix := strings.Contains(out, "prefix=")
	hasHash := strings.Contains(out, "sha256=")
	if !hasLen || !hasPrefix || !hasHash {
		t.Errorf("fingerprint missing len/prefix/sha256: out=%q (len=%v prefix=%v hash=%v)",
			out, hasLen, hasPrefix, hasHash)
	}

	// 不可逆 — 同样长度的不同凭据应当产生不同输出
	other := RedactCredential("sk-prod-DIFFERENT_VAL_77")
	if out == other && out != "" {
		t.Errorf("different credentials produce identical fingerprint: %q", out)
	}
}

// 56-1-UNIT-003: 脱敏幂等 — Redact(Redact(x)) == Redact(x)。
// 组合矩阵第 5 行：driver 已脱敏 + kernel 纵深防御再脱敏不能产生新的"redacted 嵌套"。
func TestATDD_56_1_003_RedactCredential_Idempotent(t *testing.T) {
	const secret = "sk-prod-IDEMPOTENT_TEST_KEY_42"
	first := RedactCredential(secret)
	second := RedactCredential(first)
	if first != second {
		t.Errorf("RedactCredential not idempotent:\n  first  = %q\n  second = %q", first, second)
	}
}

// 56-1-UNIT-004: 非凭据 header 应原样透传。
func TestATDD_56_1_004_RedactHeaders_PassthroughNonCredential(t *testing.T) {
	in := map[string]string{
		"Content-Type":   "application/json",
		"User-Agent":     "rnix/test",
		"X-Request-Id":   "req-12345",
		"Accept":         "*/*",
		"Content-Length": "1024",
	}
	out := RedactHeaders(in)

	if len(out) != len(in) {
		t.Fatalf("header count changed: in=%d out=%d", len(in), len(out))
	}
	for k, v := range in {
		if out[k] != v {
			t.Errorf("non-credential header mutated: key=%q in=%q out=%q", k, v, out[k])
		}
	}
}
