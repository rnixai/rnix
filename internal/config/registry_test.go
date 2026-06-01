package config

import (
	"path/filepath"
	"testing"
)

func TestProjectRegistry_RegisterAndLookup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	r := NewProjectRegistry(path)

	id, err := r.Register("/home/user/echomatrix")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if id == "" {
		t.Fatal("Register() returned empty ID")
	}

	got, ok := r.Lookup(id)
	if !ok {
		t.Fatalf("Lookup(%q) not found", id)
	}
	if got != "/home/user/echomatrix" {
		t.Errorf("Lookup(%q) = %q, want /home/user/echomatrix", id, got)
	}
}

func TestProjectRegistry_LoadPersisted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")

	r1 := NewProjectRegistry(path)
	id, err := r1.Register("/home/user/myproject")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	r2 := NewProjectRegistry(path)
	if err := r2.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got, ok := r2.Lookup(id)
	if !ok {
		t.Fatalf("Lookup(%q) not found after reload", id)
	}
	if got != "/home/user/myproject" {
		t.Errorf("Lookup(%q) = %q, want /home/user/myproject", id, got)
	}
}

func TestProjectRegistry_LoadMissingFile(t *testing.T) {
	r := NewProjectRegistry(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err := r.Load(); err != nil {
		t.Fatalf("Load() should succeed for missing file, got %v", err)
	}
}

func TestProjectRegistry_RegisterEmptyPath(t *testing.T) {
	r := NewProjectRegistry(filepath.Join(t.TempDir(), "projects.json"))
	id, err := r.Register("")
	if err != nil {
		t.Fatalf("Register(\"\") error = %v", err)
	}
	if id != "" {
		t.Errorf("Register(\"\") = %q, want empty", id)
	}
}

func TestProjectRegistry_List(t *testing.T) {
	dir := t.TempDir()
	r := NewProjectRegistry(filepath.Join(dir, "projects.json"))

	r.Register("/home/user/a")
	r.Register("/home/user/b")

	entries := r.List()
	if len(entries) != 2 {
		t.Errorf("List() len = %d, want 2", len(entries))
	}
}

func TestProjectRegistry_Idempotent(t *testing.T) {
	dir := t.TempDir()
	r := NewProjectRegistry(filepath.Join(dir, "projects.json"))

	id1, _ := r.Register("/home/user/foo")
	id2, _ := r.Register("/home/user/foo")
	if id1 != id2 {
		t.Errorf("idempotent register: %q != %q", id1, id2)
	}
	if len(r.List()) != 1 {
		t.Errorf("duplicate register should not create two entries")
	}
}
