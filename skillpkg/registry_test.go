package skillpkg

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalRegistry_AddAndGet(t *testing.T) {
	dir := t.TempDir()
	reg := NewLocalRegistry(dir)

	entry := RegistryEntry{
		Name:        "test-skill",
		Version:     "1.0.0",
		InstalledAt: time.Now().UTC(),
		Source:      "community",
		Checksum:    "sha256:abc123",
	}

	if err := reg.Add(entry); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := reg.Get("test-skill")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected entry, got nil")
	}
	if got.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", got.Version)
	}
	if got.Source != "community" {
		t.Errorf("expected source 'community', got %q", got.Source)
	}
	if got.Checksum != "sha256:abc123" {
		t.Errorf("expected checksum 'sha256:abc123', got %q", got.Checksum)
	}
}

func TestLocalRegistry_Get_NotFound(t *testing.T) {
	dir := t.TempDir()
	reg := NewLocalRegistry(dir)

	got, err := reg.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for nonexistent skill, got %+v", got)
	}
}

func TestLocalRegistry_List(t *testing.T) {
	dir := t.TempDir()
	reg := NewLocalRegistry(dir)

	entries := []RegistryEntry{
		{Name: "skill-a", Version: "1.0.0", InstalledAt: time.Now().UTC(), Source: "community", Checksum: "sha256:aaa"},
		{Name: "skill-b", Version: "2.0.0", InstalledAt: time.Now().UTC(), Source: "community", Checksum: "sha256:bbb"},
	}

	for _, e := range entries {
		if err := reg.Add(e); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	list, err := reg.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
}

func TestLocalRegistry_Remove(t *testing.T) {
	dir := t.TempDir()
	reg := NewLocalRegistry(dir)

	entry := RegistryEntry{
		Name:        "test-skill",
		Version:     "1.0.0",
		InstalledAt: time.Now().UTC(),
		Source:      "community",
		Checksum:    "sha256:abc",
	}
	if err := reg.Add(entry); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if err := reg.Remove("test-skill"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	got, err := reg.Get("test-skill")
	if err != nil {
		t.Fatalf("Get after remove failed: %v", err)
	}
	if got != nil {
		t.Error("expected nil after removal")
	}
}

func TestLocalRegistry_Remove_NotFound(t *testing.T) {
	dir := t.TempDir()
	reg := NewLocalRegistry(dir)

	err := reg.Remove("nonexistent")
	if err == nil {
		t.Fatal("expected error for removing nonexistent skill")
	}
}

func TestLocalRegistry_Update(t *testing.T) {
	dir := t.TempDir()
	reg := NewLocalRegistry(dir)

	entry := RegistryEntry{
		Name:        "test-skill",
		Version:     "1.0.0",
		InstalledAt: time.Now().UTC(),
		Source:      "community",
		Checksum:    "sha256:old",
	}
	if err := reg.Add(entry); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	updated := RegistryEntry{
		Name:        "test-skill",
		Version:     "2.0.0",
		InstalledAt: time.Now().UTC(),
		Source:      "community",
		Checksum:    "sha256:new",
	}
	if err := reg.Add(updated); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := reg.Get("test-skill")
	if err != nil {
		t.Fatalf("Get after update failed: %v", err)
	}
	if got.Version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got %q", got.Version)
	}
	if got.Checksum != "sha256:new" {
		t.Errorf("expected checksum 'sha256:new', got %q", got.Checksum)
	}
}

func TestLocalRegistry_PersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	reg := NewLocalRegistry(dir)

	entry := RegistryEntry{
		Name:        "test-skill",
		Version:     "1.0.0",
		InstalledAt: time.Now().UTC(),
		Source:      "community",
		Checksum:    "sha256:abc",
	}
	if err := reg.Add(entry); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Verify file exists on disk
	regPath := filepath.Join(dir, ".registry.yaml")
	if _, err := os.Stat(regPath); err != nil {
		t.Fatalf("registry file not found: %v", err)
	}

	// Create new registry instance and verify data persists
	reg2 := NewLocalRegistry(dir)
	got, err := reg2.Get("test-skill")
	if err != nil {
		t.Fatalf("Get from new instance failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected persisted entry, got nil")
	}
	if got.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", got.Version)
	}
}
