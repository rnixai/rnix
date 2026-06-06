package memory

import (
	"strings"
	"testing"
)

// =============================================================================
// ATDD Tests for Story 35.6: BuildUserProfileBlock (inject.go)
// TDD RED PHASE — Tests for the kernel/memory/inject.go BuildUserProfileBlock function.
// =============================================================================

// ---------------------------------------------------------------------------
// AC2: BuildPrompt 用户画像独立 Section
// ---------------------------------------------------------------------------

// 35.6-INJECT-001: BuildUserProfileBlock 空用户画像返回空字符串
func TestBuildUserProfileBlock_Empty(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, projectDir, cfg)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	result := BuildUserProfileBlock(store, "")
	if result != "" {
		t.Errorf("expected empty string for empty user profile, got: %q", result)
	}
}

// 35.6-INJECT-002: BuildUserProfileBlock 有用户画像时返回非空 section
func TestBuildUserProfileBlock_WithProfile(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, projectDir, cfg)
	_ = store.Load()
	_ = store.Add("user", "User is a senior Go developer", "")

	result := BuildUserProfileBlock(store, "")
	if result == "" {
		t.Fatal("expected non-empty user profile block")
	}
	if !strings.Contains(result, "User is a senior Go developer") {
		t.Errorf("user profile block missing entry, got: %q", result)
	}
}

// 35.6-INJECT-003: BuildUserProfileBlock 包含 User Profile heading
func TestBuildUserProfileBlock_HasHeading(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, projectDir, cfg)
	_ = store.Load()
	_ = store.Add("user", "prefers concise explanations", "")

	result := BuildUserProfileBlock(store, "")
	if !strings.Contains(result, "User Profile") {
		t.Error("user profile block should contain 'User Profile' heading")
	}
}

// 35.6-INJECT-004: BuildUserProfileBlock 与 BuildMemoryBlock 独立（不互相包含）
func TestBuildUserProfileBlock_IndependentFromMemoryBlock(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, projectDir, cfg)
	_ = store.Load()
	_ = store.Add("memory", "project uses goccy/go-yaml", "")
	_ = store.Add("user", "User prefers Chinese communication", "")

	profileBlock := BuildUserProfileBlock(store, "")
	memoryBlock := BuildMemoryBlock(store, "")

	// Profile block should NOT contain memory entries
	if strings.Contains(profileBlock, "goccy/go-yaml") {
		t.Error("profile block should not contain memory entries")
	}

	// Memory block should NOT contain user entries
	if strings.Contains(memoryBlock, "Chinese communication") {
		t.Error("memory block should not contain user profile entries")
	}

	// Both should contain their own entries
	if !strings.Contains(profileBlock, "Chinese communication") {
		t.Error("profile block missing user entry")
	}
	if !strings.Contains(memoryBlock, "goccy/go-yaml") {
		t.Error("memory block missing memory entry")
	}
}

// 35.6-INJECT-005: BuildUserProfileBlock 多条目全部包含
func TestBuildUserProfileBlock_MultipleEntries(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, projectDir, cfg)
	_ = store.Load()
	_ = store.Add("user", "Senior Go developer", "")
	_ = store.Add("user", "Prefers concise explanations", "")
	_ = store.Add("user", "Expert in distributed systems", "")

	result := BuildUserProfileBlock(store, "")
	for _, entry := range []string{"Senior Go developer", "Prefers concise explanations", "Expert in distributed systems"} {
		if !strings.Contains(result, entry) {
			t.Errorf("profile block missing entry %q", entry)
		}
	}
}

// 35.6-INJECT-006: BuildUserProfileBlock 冻结快照语义
func TestBuildUserProfileBlock_FrozenSemantics(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, projectDir, cfg)
	_ = store.Load()
	_ = store.Add("user", "before-freeze-entry", "")

	// Simulate spawn: capture snapshot
	frozen := BuildUserProfileBlock(store, "")

	// Simulate runtime write
	_ = store.Add("user", "after-freeze-entry", "")

	// Live snapshot should contain new entry
	live := BuildUserProfileBlock(store, "")
	if !strings.Contains(live, "after-freeze-entry") {
		t.Error("live profile block should contain post-freeze entry")
	}

	// Frozen snapshot was taken before the write
	if strings.Contains(frozen, "after-freeze-entry") {
		t.Error("frozen profile block should not contain post-freeze entry")
	}
}
