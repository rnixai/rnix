package memory

import (
	"strings"
	"testing"
)

// =============================================================================
// ATDD Tests for Story 35.2: BuildMemoryBlock (inject.go)
// TDD RED PHASE — Tests for the kernel/memory/inject.go BuildMemoryBlock function.
// =============================================================================

// Note: These tests are in the kernel/memory package to test internal behavior.
// The BuildMemoryBlock function is exported for use by kernel/sections.go.

// 35.2-INJECT-001: BuildMemoryBlock 空记忆返回空字符串
func TestBuildMemoryBlock_Empty(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, projectDir, cfg)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	result := BuildMemoryBlock(store)
	if result != "" {
		t.Errorf("expected empty string for empty memory, got: %q", result)
	}
}

// 35.2-INJECT-002: BuildMemoryBlock 项目记忆存在
func TestBuildMemoryBlock_ProjectOnly(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, projectDir, cfg)
	_ = store.Load()
	_ = store.Add("memory", "test project fact")

	result := BuildMemoryBlock(store)
	if !strings.Contains(result, "test project fact") {
		t.Errorf("missing project entry, got: %q", result)
	}
}

// 35.2-INJECT-003: BuildMemoryBlock 全局+项目，全局在前
func TestBuildMemoryBlock_DualScope_Order(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, projectDir, cfg)
	_ = store.Load()
	_ = store.Add("global_memory", "global knowledge")
	_ = store.Add("memory", "project knowledge")

	result := BuildMemoryBlock(store)
	gIdx := strings.Index(result, "global knowledge")
	pIdx := strings.Index(result, "project knowledge")
	if gIdx < 0 || pIdx < 0 {
		t.Fatalf("missing entries, got: %q", result)
	}
	if gIdx > pIdx {
		t.Error("global memory should appear before project memory")
	}
}

// 35.2-INJECT-004: BuildMemoryBlock 包含 Memory heading
func TestBuildMemoryBlock_HasHeading(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, projectDir, cfg)
	_ = store.Load()
	_ = store.Add("memory", "entry")

	result := BuildMemoryBlock(store)
	if !strings.Contains(result, "Memory") {
		t.Error("should contain Memory heading")
	}
}

// 35.2-INJECT-005: BuildMemoryBlock 全局记忆独立存在
func TestBuildMemoryBlock_GlobalOnly(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, projectDir, cfg)
	_ = store.Load()
	_ = store.Add("global_memory", "only global")

	result := BuildMemoryBlock(store)
	if !strings.Contains(result, "only global") {
		t.Errorf("missing global entry, got: %q", result)
	}
}
