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

// ============================================================================
// ATDD Story 56.3 — vfs.RedactArgv (CLI driver argv 脱敏，AC#3 / AC#7)
//
// CLI 族 raw capture 把完整 argv 落盘（包含 --effort 真实值供审计）。argv 中
// 偶含凭据型 flag 必须 driver 层脱敏（裁决 4：argv 是 []string，逃 kernel
// per-record 截断 + kernel 二次脱敏只触 headers 字段，driver 层是唯一防线）。
//
// 红线：明文凭据零落盘；effort/model 等正常透传 flag 保真；幂等。
// ============================================================================

func TestATDD_56_3_UNIT001_RedactArgv_FlagValueRedacted(t *testing.T) {
	secret := "sk-CLI_SECRET_PLAINTEXT_42"
	argv := []string{
		"/usr/local/bin/claude",
		"--print", "-",
		"--model", "claude-opus-4-7",
		"--api-key", secret,
		"--effort", "high", // CAP-1 透传凭证：必须保真
	}
	out := RedactArgv(argv)
	if len(out) != len(argv) {
		t.Fatalf("len changed: got=%d want=%d", len(out), len(argv))
	}
	// 凭据值必须不可见，且替换为指纹
	for _, v := range out {
		if strings.Contains(v, secret) {
			t.Fatalf("RedactArgv leaked plaintext at element %q", v)
		}
	}
	// --api-key 后续元素必须含 fingerprint 形态
	if got := out[6]; !strings.HasPrefix(got, "redacted(") {
		t.Errorf("api-key value not fingerprinted: %q", got)
	}
	// --effort high 必须保真（CAP-1 真实值审计）
	if out[7] != "--effort" || out[8] != "high" {
		t.Errorf("effort flag mutated: out[7..8]=%q,%q", out[7], out[8])
	}
	// --model 也必须保真
	if out[3] != "--model" || out[4] != "claude-opus-4-7" {
		t.Errorf("model flag mutated: out[3..4]=%q,%q", out[3], out[4])
	}
	// binary 不动
	if out[0] != argv[0] {
		t.Errorf("binary mutated: %q want %q", out[0], argv[0])
	}
}

func TestATDD_56_3_UNIT002_RedactArgv_EqualsFormRedacted(t *testing.T) {
	secret := "tok_LIVE_SECRET_88"
	argv := []string{
		"codex",
		"exec",
		"-c", "model_reasoning_effort=high", // 保真：effort
		"--token=" + secret,                  // 单 token form 须脱敏
		"--secret=ANOTHER_SECRET_VAL",         // 同形式
	}
	out := RedactArgv(argv)
	for _, v := range out {
		if strings.Contains(v, secret) {
			t.Fatalf("equals-form leaked plaintext: %q", v)
		}
		if strings.Contains(v, "ANOTHER_SECRET_VAL") {
			t.Fatalf("equals-form --secret leaked: %q", v)
		}
	}
	// effort kv 必须保真
	if out[3] != "model_reasoning_effort=high" {
		t.Errorf("model_reasoning_effort kv mutated: %q", out[3])
	}
	// 凭据 kv 必须以 head=redacted(...) 形态
	if !strings.HasPrefix(out[4], "--token=redacted(") {
		t.Errorf("--token= form not fingerprinted: %q", out[4])
	}
	if !strings.HasPrefix(out[5], "--secret=redacted(") {
		t.Errorf("--secret= form not fingerprinted: %q", out[5])
	}
}

func TestATDD_56_3_UNIT003_RedactArgv_HeaderFlagInArgv(t *testing.T) {
	// 极少 CLI 用 -H 但既然在白名单内仍需正确处理
	argv := []string{"curl-like", "-H", "Authorization: Bearer sk-LEAK_BEARER_99"}
	out := RedactArgv(argv)
	if strings.Contains(out[2], "sk-LEAK_BEARER_99") {
		t.Fatalf("-H Authorization leaked: %q", out[2])
	}
	// scheme token 应保留可见
	if !strings.Contains(out[2], "Bearer ") {
		t.Errorf("Bearer scheme dropped: %q", out[2])
	}
}

func TestATDD_56_3_UNIT004_RedactArgv_Idempotent(t *testing.T) {
	argv := []string{"claude", "--api-key", "sk-IDEMPOTENT_SECRET_77"}
	first := RedactArgv(argv)
	second := RedactArgv(first)
	if first[2] != second[2] {
		t.Errorf("RedactArgv not idempotent:\n  first  = %q\n  second = %q", first[2], second[2])
	}
}

func TestATDD_56_3_UNIT005_RedactArgv_NoCredentialsPassthrough(t *testing.T) {
	argv := []string{
		"claude", "--print", "-",
		"--output-format", "stream-json",
		"--model", "haiku",
		"--effort", "high",
	}
	out := RedactArgv(argv)
	for i, v := range argv {
		if out[i] != v {
			t.Errorf("non-credential argv mutated at %d: in=%q out=%q", i, v, out[i])
		}
	}
}

func TestATDD_56_3_UNIT006_RedactArgv_EmptyAndShortArgs(t *testing.T) {
	// 空 / 单元素 / 末位是 sensitive flag 但无下一个值都应 graceful
	if got := RedactArgv(nil); len(got) != 0 {
		t.Errorf("nil argv: got=%v", got)
	}
	if got := RedactArgv([]string{}); len(got) != 0 {
		t.Errorf("empty argv: got=%v", got)
	}
	// trailing sensitive flag 无值不应越界
	argv := []string{"claude", "--api-key"}
	out := RedactArgv(argv)
	if len(out) != 2 || out[0] != "claude" || out[1] != "--api-key" {
		t.Errorf("trailing sensitive flag mishandled: %v", out)
	}
}
