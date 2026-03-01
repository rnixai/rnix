package skillpkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createTestTarGz creates a .tar.gz archive containing a SKILL.md file.
func createTestTarGz(t *testing.T, skillName string) []byte {
	t.Helper()
	skillContent := fmt.Sprintf(`---
name: %s
description: "A test skill"
allowed-tools: /dev/fs
metadata:
  author: test
  version: "1.0.0"
---

# %s

Test skill instructions.
`, skillName, skillName)

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := []byte(skillContent)
	header := &tar.Header{
		Name: "SKILL.md",
		Mode: 0o644,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// checksumOf returns the "sha256:<hex>" checksum of data.
func checksumOf(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", h)
}

// setupMockRegistry creates an httptest.Server that serves a mock skill registry.
func setupMockRegistry(t *testing.T, skillName string) (*httptest.Server, []byte, string) {
	t.Helper()
	tarData := createTestTarGz(t, skillName)
	checksum := checksumOf(tarData)

	indexYAML := fmt.Sprintf(`skills:
  - name: %s
    description: "A test skill"
    latest: "1.0.0"
`, skillName)

	latestYAML := fmt.Sprintf(`version: "1.0.0"
checksum: "%s"
`, checksum)

	mux := http.NewServeMux()
	mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		w.Write([]byte(indexYAML))
	})
	mux.HandleFunc(fmt.Sprintf("/packages/%s/latest.yaml", skillName), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		w.Write([]byte(latestYAML))
	})
	mux.HandleFunc(fmt.Sprintf("/packages/%s/1.0.0.tar.gz", skillName), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(tarData)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, tarData, checksum
}

func TestRegistryClient_FetchIndex(t *testing.T) {
	srv, _, _ := setupMockRegistry(t, "test-skill")
	client := NewRegistryClient(srv.URL, srv.Client())

	index, err := client.FetchIndex()
	if err != nil {
		t.Fatalf("FetchIndex failed: %v", err)
	}
	if len(index.Skills) != 1 {
		t.Fatalf("expected 1 skill in index, got %d", len(index.Skills))
	}
	if index.Skills[0].Name != "test-skill" {
		t.Errorf("expected skill name 'test-skill', got %q", index.Skills[0].Name)
	}
}

func TestRegistryClient_Resolve(t *testing.T) {
	srv, _, checksum := setupMockRegistry(t, "test-skill")
	client := NewRegistryClient(srv.URL, srv.Client())

	ver, err := client.Resolve("test-skill")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if ver.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", ver.Version)
	}
	if ver.Checksum != checksum {
		t.Errorf("expected checksum %q, got %q", checksum, ver.Checksum)
	}
}

func TestRegistryClient_Resolve_NotFound(t *testing.T) {
	srv, _, _ := setupMockRegistry(t, "test-skill")
	client := NewRegistryClient(srv.URL, srv.Client())

	_, err := client.Resolve("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
}

func TestRegistryClient_Fetch(t *testing.T) {
	srv, tarData, checksum := setupMockRegistry(t, "test-skill")
	client := NewRegistryClient(srv.URL, srv.Client())

	ver := &SkillVersion{Version: "1.0.0", Checksum: checksum}
	pkg, err := client.Fetch("test-skill", ver)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if pkg.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", pkg.Name)
	}
	if len(pkg.Data) != len(tarData) {
		t.Errorf("expected %d bytes, got %d", len(tarData), len(pkg.Data))
	}
}

func TestRegistryClient_Verify_Success(t *testing.T) {
	srv, tarData, checksum := setupMockRegistry(t, "test-skill")
	client := NewRegistryClient(srv.URL, srv.Client())

	pkg := &SkillPackage{
		Name:     "test-skill",
		Version:  "1.0.0",
		Checksum: checksum,
		Data:     tarData,
	}
	if err := client.Verify(pkg); err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
}

func TestRegistryClient_Verify_Mismatch(t *testing.T) {
	srv, _, _ := setupMockRegistry(t, "test-skill")
	client := NewRegistryClient(srv.URL, srv.Client())

	pkg := &SkillPackage{
		Name:     "test-skill",
		Version:  "1.0.0",
		Checksum: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Data:     []byte("corrupted data"),
	}
	if err := client.Verify(pkg); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

func TestRegistryClient_Verify_NoChecksum(t *testing.T) {
	srv, _, _ := setupMockRegistry(t, "test-skill")
	client := NewRegistryClient(srv.URL, srv.Client())

	pkg := &SkillPackage{
		Name: "test-skill",
		Data: []byte("some data"),
	}
	if err := client.Verify(pkg); err == nil {
		t.Fatal("expected error for missing checksum")
	}
}

func TestRegistryClient_NetworkError(t *testing.T) {
	// Use a server that is immediately closed
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()

	client := NewRegistryClient(srv.URL, srv.Client())

	_, err := client.FetchIndex()
	if err == nil {
		t.Fatal("expected network error")
	}
}
