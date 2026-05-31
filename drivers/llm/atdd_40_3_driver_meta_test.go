package llm

import (
	"strconv"
	"testing"
)

// ============================================================================
// ATDD Tests for Story 40.3: 集成验证、可观测性与文档
//
// RED PHASE: These tests reference methods/interfaces that do not exist yet
// (DriverMetaProvider, ClaudeCliDriver.DriverMeta()). They will fail to
// compile until the feature is implemented.
//
// AC1: Dashboard 能力展示 — DriverMeta returns correct metadata
// AC1: DriverMetaProvider interface type assertion
// ============================================================================

// ---------------------------------------------------------------------------
// AC #1: DriverMetaProvider 接口 — ClaudeCliDriver 实现可选元数据接口
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC1_DriverMetaProvider_InterfaceSatisfied(t *testing.T) {
	t.Parallel()

	// GIVEN: ClaudeCliDriver 实例
	// WHEN:  对 DriverMetaProvider 接口做类型断言
	// THEN:  断言成功（ClaudeCliDriver 实现了 DriverMetaProvider）

	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("success")))

	var _ DriverMetaProvider = d // compile-time interface check

	if _, ok := any(d).(DriverMetaProvider); !ok {
		t.Fatal("AC1 FAIL: ClaudeCliDriver does not implement DriverMetaProvider interface")
	}
}

func TestATDD_40_3_AC1_DriverMeta_ReturnsCorrectKeys(t *testing.T) {
	t.Parallel()

	// GIVEN: ClaudeCliDriver 配置了 model 和 permissionMode
	// WHEN:  调用 DriverMeta()
	// THEN:  返回的 map 包含 resolved_bin, permission_mode, cap_partial_messages,
	//        cap_add_dir, cap_permission_mode 五个 key

	d := NewClaudeCliDriver(
		WithCommandBuilder(mockCmdBuilder("success")),
		WithModel("claude-sonnet-4-6"),
		WithPermissionMode("bypassPermissions"),
	)

	meta := d.DriverMeta()

	expectedKeys := []string{
		"resolved_bin",
		"permission_mode",
		"cap_partial_messages",
		"cap_add_dir",
		"cap_permission_mode",
	}
	for _, key := range expectedKeys {
		if _, ok := meta[key]; !ok {
			t.Errorf("AC1 FAIL: DriverMeta() missing key %q", key)
		}
	}
}

func TestATDD_40_3_AC1_DriverMeta_PermissionModeValue(t *testing.T) {
	t.Parallel()

	// GIVEN: WithPermissionMode("bypassPermissions")
	// WHEN:  调用 DriverMeta()
	// THEN:  meta["permission_mode"] == "bypassPermissions"

	d := NewClaudeCliDriver(
		WithCommandBuilder(mockCmdBuilder("success")),
		WithPermissionMode("bypassPermissions"),
	)

	meta := d.DriverMeta()

	if got := meta["permission_mode"]; got != "bypassPermissions" {
		t.Errorf("AC1 FAIL: expected permission_mode=%q, got %q", "bypassPermissions", got)
	}
}

func TestATDD_40_3_AC1_DriverMeta_CapabilitiesAreBoolStrings(t *testing.T) {
	t.Parallel()

	// GIVEN: ClaudeCliDriver（caps 由 probeCapabilities 填充或零值）
	// WHEN:  调用 DriverMeta()
	// THEN:  cap_* 值可解析为 bool（"true" 或 "false"）

	d := NewClaudeCliDriver(
		WithCommandBuilder(mockCmdBuilder("success")),
	)

	meta := d.DriverMeta()

	for _, key := range []string{"cap_partial_messages", "cap_add_dir", "cap_permission_mode"} {
		val, ok := meta[key]
		if !ok {
			t.Errorf("AC1 FAIL: DriverMeta() missing key %q", key)
			continue
		}
		if _, err := strconv.ParseBool(val); err != nil {
			t.Errorf("AC1 FAIL: DriverMeta()[%q] = %q is not a valid bool string", key, val)
		}
	}
}

// ---------------------------------------------------------------------------
// AC #1: 非 Claude CLI driver 不实现 DriverMetaProvider
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC1_NonClaudeDriver_NoDriverMetaProvider(t *testing.T) {
	t.Parallel()

	// GIVEN: 一个非 ClaudeCliDriver 的 LLMDriver 实现
	// WHEN:  对 DriverMetaProvider 接口做类型断言
	// THEN:  断言失败（非 Claude driver 不填充 DriverMeta）

	// Use any non-Claude driver; the Anthropic driver is a direct API driver.
	var driver LLMDriver = NewAnthropicDriver("test-anthropic")

	if _, ok := driver.(DriverMetaProvider); ok {
		t.Error("AC1 FAIL: non-Claude driver should NOT implement DriverMetaProvider")
	}
}
