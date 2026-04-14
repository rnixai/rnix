package memory

import (
	"strings"
	"testing"
)

// =============================================================================
// ATDD Tests for Story 35.6: User Profile & MemoryProvider Interface
// TDD RED PHASE — Tests for FileMemoryProvider per-target capacity limits
// =============================================================================

// ---------------------------------------------------------------------------
// AC1 + AC8: FileMemoryProvider per-target 容量独立化
// ---------------------------------------------------------------------------

// 35.6-PROV-001: FileMemoryProvider "user" target uses independent capacity limit
func TestFileMemoryProvider_UserTargetIndependentLimit(t *testing.T) {
	store, _, _ := setupTestStore(t, 4096, 100) // memory=4096, user=100

	// Add a large entry to "user" target that fits within memory limit but exceeds user limit
	bigEntry := strings.Repeat("u", 90)
	err := store.Add("user", bigEntry)
	if err != nil {
		t.Fatalf("first user Add should succeed within user limit: %v", err)
	}

	// This should fail because user limit is 100, not 4096
	err = store.Add("user", "overflow entry that exceeds the small user limit")
	if err == nil {
		t.Error("expected capacity error for user target with small limit, but Add succeeded")
	}
}

// 35.6-PROV-002: FileMemoryProvider "memory" target still uses MemoryCharLimit
func TestFileMemoryProvider_MemoryTargetUsesOwnLimit(t *testing.T) {
	store, _, _ := setupTestStore(t, 200, 50) // memory=200, user=50

	// Memory target should have its own larger limit
	entry := strings.Repeat("m", 150)
	err := store.Add("memory", entry)
	if err != nil {
		t.Errorf("memory Add within memory limit should succeed: %v", err)
	}
}

// 35.6-PROV-003: Capacity() returns per-target limit for "user"
func TestFileMemoryProvider_CapacityReturnsUserLimit(t *testing.T) {
	store, _, _ := setupTestStore(t, 4096, 2048)

	_, userLimit := store.Capacity("user")
	_, memLimit := store.Capacity("memory")

	if userLimit == memLimit {
		t.Errorf("user limit (%d) should differ from memory limit (%d)", userLimit, memLimit)
	}
	if userLimit != 2048 {
		t.Errorf("expected user limit=2048, got %d", userLimit)
	}
	if memLimit != 4096 {
		t.Errorf("expected memory limit=4096, got %d", memLimit)
	}
}

// 35.6-PROV-004: Replace on "user" target respects user capacity limit
func TestFileMemoryProvider_ReplaceRespectsUserLimit(t *testing.T) {
	store, _, _ := setupTestStore(t, 4096, 100)

	_ = store.Add("user", "short")
	// Replace with an entry that exceeds user limit (100) after capacity accounting
	bigReplacement := strings.Repeat("x", 100)
	err := store.Replace("user", "short", bigReplacement)
	if err == nil {
		t.Error("expected capacity error when Replace exceeds user limit")
	}
}

// 35.6-PROV-005: "user" and "memory" targets have fully independent capacity tracking
func TestFileMemoryProvider_IndependentCapacityTracking(t *testing.T) {
	store, _, _ := setupTestStore(t, 200, 200)

	// Fill user target near capacity
	_ = store.Add("user", strings.Repeat("u", 180))

	// Memory target should still have full capacity
	err := store.Add("memory", strings.Repeat("m", 180))
	if err != nil {
		t.Errorf("memory Add should succeed independently of user capacity: %v", err)
	}
}

// 35.6-PROV-006: Backward compatibility — unknown target falls back to memory limit
func TestFileMemoryProvider_UnknownTargetFallback(t *testing.T) {
	// This tests the limitFor() fallback behavior
	store, _, _ := setupTestStore(t, 4096, 2048)

	// "memory" target should work with MemoryCharLimit
	_, limit := store.Capacity("memory")
	if limit != 4096 {
		t.Errorf("expected memory limit=4096, got %d", limit)
	}
}

// 35.6-PROV-007: DefaultMemoryConfig has correct default UserCharLimit
func TestDefaultMemoryConfig_UserCharLimitDefault(t *testing.T) {
	cfg := DefaultMemoryConfig()
	if cfg.Store.UserCharLimit != 2048 {
		t.Errorf("expected default UserCharLimit=2048, got %d", cfg.Store.UserCharLimit)
	}
	if cfg.Store.MemoryCharLimit != 4096 {
		t.Errorf("expected default MemoryCharLimit=4096, got %d", cfg.Store.MemoryCharLimit)
	}
}

// 35.6-PROV-008: USER.md file is managed independently from MEMORY.md
func TestFileMemoryProvider_UserMdIndependentFromMemoryMd(t *testing.T) {
	store, _, _ := setupTestStore(t, 4096, 2048)

	_ = store.Add("user", "user profile entry")
	_ = store.Add("memory", "memory knowledge entry")

	userSnap := store.Snapshot("user")
	memSnap := store.Snapshot("memory")

	if !strings.Contains(userSnap, "user profile entry") {
		t.Error("user snapshot missing user entry")
	}
	if strings.Contains(userSnap, "memory knowledge entry") {
		t.Error("user snapshot should not contain memory entries")
	}
	if !strings.Contains(memSnap, "memory knowledge entry") {
		t.Error("memory snapshot missing memory entry")
	}
	if strings.Contains(memSnap, "user profile entry") {
		t.Error("memory snapshot should not contain user entries")
	}
}

// 35.6-PROV-009: ParseMemoryConfig parses user_char_limit from YAML
func TestParseMemoryConfig_UserCharLimit(t *testing.T) {
	yamlData := []byte(`
enabled: true
store:
  memory_char_limit: 8192
  user_char_limit: 1024
`)
	cfg, err := ParseMemoryConfig(yamlData)
	if err != nil {
		t.Fatalf("ParseMemoryConfig failed: %v", err)
	}
	if cfg.Store.UserCharLimit != 1024 {
		t.Errorf("expected UserCharLimit=1024, got %d", cfg.Store.UserCharLimit)
	}
	if cfg.Store.MemoryCharLimit != 8192 {
		t.Errorf("expected MemoryCharLimit=8192, got %d", cfg.Store.MemoryCharLimit)
	}
}

// ---------------------------------------------------------------------------
// AC7: MemoryProvider 接口完整性
// ---------------------------------------------------------------------------

// 35.6-PROV-010: MemoryProvider interface has all required methods
func TestMemoryProvider_InterfaceCompleteness(t *testing.T) {
	// Compile-time interface check — FileMemoryProvider implements MemoryProvider
	var _ MemoryProvider = (*FileMemoryProvider)(nil)

	// The interface must expose: Load, Add, Replace, Remove, Snapshot, Capacity
	store, _, _ := setupTestStore(t, 4096, 2048)

	// Exercise all interface methods on "user" target
	if err := store.Add("user", "test entry"); err != nil {
		t.Fatalf("Add user failed: %v", err)
	}
	if err := store.Replace("user", "test entry", "replaced entry"); err != nil {
		t.Fatalf("Replace user failed: %v", err)
	}
	snap := store.Snapshot("user")
	if !strings.Contains(snap, "replaced entry") {
		t.Error("Snapshot user missing replaced entry")
	}
	used, limit := store.Capacity("user")
	if limit == 0 || used == 0 {
		t.Errorf("Capacity user: used=%d, limit=%d", used, limit)
	}
	if err := store.Remove("user", "replaced entry"); err != nil {
		t.Fatalf("Remove user failed: %v", err)
	}
	snap = store.Snapshot("user")
	if snap != "" {
		t.Errorf("expected empty snapshot after Remove, got %q", snap)
	}
}

// ---------------------------------------------------------------------------
// AC5 + AC6: 禁用状态与默认启用
// ---------------------------------------------------------------------------

// 35.6-PROV-011: DefaultMemoryConfig has UserProfile.Enabled=true
func TestDefaultMemoryConfig_UserProfileEnabledDefault(t *testing.T) {
	cfg := DefaultMemoryConfig()
	if !cfg.UserProfile.Enabled {
		t.Error("expected default UserProfile.Enabled=true")
	}
}

// 35.6-PROV-012: ParseMemoryConfig parses user_profile.enabled=false
func TestParseMemoryConfig_UserProfileDisabled(t *testing.T) {
	yamlData := []byte(`
enabled: true
user_profile:
  enabled: false
`)
	cfg, err := ParseMemoryConfig(yamlData)
	if err != nil {
		t.Fatalf("ParseMemoryConfig failed: %v", err)
	}
	if cfg.UserProfile.Enabled {
		t.Error("expected UserProfile.Enabled=false after parsing")
	}
}
