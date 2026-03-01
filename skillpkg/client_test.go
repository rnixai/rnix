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

// --- ATDD RED Phase: Story 8.2 — skill search 搜索 ---

// setupMockRegistryMultiSkill creates an httptest.Server serving multiple skills in the index.
func setupMockRegistryMultiSkill(t *testing.T) *httptest.Server {
	t.Helper()

	indexYAML := `skills:
  - name: code-analysis
    description: "Analyze code quality and patterns"
    latest: "1.0.0"
    downloads: 1234
  - name: pr-reviewer
    description: "Review pull requests with AI"
    latest: "2.1.0"
    downloads: 5678
  - name: tech-writer
    description: "Generate technical documentation"
    latest: "1.2.0"
    downloads: 890
  - name: bug-finder
    description: "Find and analyze bugs in code"
    latest: "3.0.0"
    downloads: 2345
`

	mux := http.NewServeMux()
	mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		w.Write([]byte(indexYAML))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestRegistryClient_Search_MatchByName verifies keyword matches against skill names.
// AC #1: search 子命令返回匹配的 Skill 列表
func TestRegistryClient_Search_MatchByName(t *testing.T) {
	srv := setupMockRegistryMultiSkill(t)
	client := NewRegistryClient(srv.URL, srv.Client())

	// Given: registry contains "code-analysis", "pr-reviewer", "tech-writer", "bug-finder"
	// When: searching by keyword "code"
	results, err := client.Search("code")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Then: should match "code-analysis" (name contains "code")
	//       and "bug-finder" (description contains "code")
	if len(results) < 1 {
		t.Fatalf("expected at least 1 result for keyword 'code', got %d", len(results))
	}

	found := false
	for _, r := range results {
		if r.Name == "code-analysis" {
			found = true
			if r.Version != "1.0.0" {
				t.Errorf("expected version '1.0.0', got %q", r.Version)
			}
			if r.Downloads != 1234 {
				t.Errorf("expected downloads 1234, got %d", r.Downloads)
			}
		}
	}
	if !found {
		t.Error("expected 'code-analysis' in search results")
	}
}

// TestRegistryClient_Search_MatchByDescription verifies keyword matches against descriptions.
// AC #1: search returns skills matching description
func TestRegistryClient_Search_MatchByDescription(t *testing.T) {
	srv := setupMockRegistryMultiSkill(t)
	client := NewRegistryClient(srv.URL, srv.Client())

	// Given: "bug-finder" has description "Find and analyze bugs in code"
	// When: searching by keyword "bugs"
	results, err := client.Search("bugs")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Then: should match "bug-finder" (description contains "bugs")
	if len(results) != 1 {
		t.Fatalf("expected 1 result for keyword 'bugs', got %d", len(results))
	}
	if results[0].Name != "bug-finder" {
		t.Errorf("expected 'bug-finder', got %q", results[0].Name)
	}
}

// TestRegistryClient_Search_CaseInsensitive verifies case-insensitive matching.
// AC #1: search is case-insensitive
func TestRegistryClient_Search_CaseInsensitive(t *testing.T) {
	srv := setupMockRegistryMultiSkill(t)
	client := NewRegistryClient(srv.URL, srv.Client())

	// Given: "pr-reviewer" exists in index
	// When: searching with uppercase "PR"
	results, err := client.Search("PR")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Then: should match "pr-reviewer" (case-insensitive)
	if len(results) < 1 {
		t.Fatalf("expected at least 1 result for keyword 'PR', got %d", len(results))
	}
	found := false
	for _, r := range results {
		if r.Name == "pr-reviewer" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'pr-reviewer' in case-insensitive search results")
	}
}

// TestRegistryClient_Search_NoMatch verifies empty results for non-matching keyword.
// AC #2: 搜索无结果时返回空切片
func TestRegistryClient_Search_NoMatch(t *testing.T) {
	srv := setupMockRegistryMultiSkill(t)
	client := NewRegistryClient(srv.URL, srv.Client())

	// Given: no skill matches "nonexistent"
	// When: searching for "nonexistent"
	results, err := client.Search("nonexistent")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Then: should return empty slice (not nil)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// TestRegistryClient_Search_EmptyKeyword verifies that empty keyword returns all skills.
// AC #1: empty keyword = browse all skills
func TestRegistryClient_Search_EmptyKeyword(t *testing.T) {
	srv := setupMockRegistryMultiSkill(t)
	client := NewRegistryClient(srv.URL, srv.Client())

	// Given: registry has 4 skills
	// When: searching with empty keyword
	results, err := client.Search("")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Then: should return all 4 skills
	if len(results) != 4 {
		t.Fatalf("expected 4 results for empty keyword (browse all), got %d", len(results))
	}
}

// TestRegistryClient_Search_ResultFields verifies SearchResult fields are populated correctly.
// AC #1: 每条结果包含：名称、描述、版本、下载量
func TestRegistryClient_Search_ResultFields(t *testing.T) {
	srv := setupMockRegistryMultiSkill(t)
	client := NewRegistryClient(srv.URL, srv.Client())

	// Given: "pr-reviewer" exists with known metadata
	// When: searching for "pr-reviewer"
	results, err := client.Search("pr-reviewer")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Then: result should have all fields populated
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Name != "pr-reviewer" {
		t.Errorf("expected name 'pr-reviewer', got %q", r.Name)
	}
	if r.Description != "Review pull requests with AI" {
		t.Errorf("expected description 'Review pull requests with AI', got %q", r.Description)
	}
	if r.Version != "2.1.0" {
		t.Errorf("expected version '2.1.0', got %q", r.Version)
	}
	if r.Downloads != 5678 {
		t.Errorf("expected downloads 5678, got %d", r.Downloads)
	}
}
