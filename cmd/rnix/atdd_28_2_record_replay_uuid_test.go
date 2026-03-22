package main

// =============================================================================
// ATDD Story 28.2: StepRecord 路径迁移到 UUID — CLI record/replay 扩展
// TDD RED PHASE — Tests reference functions that do NOT exist yet
// =============================================================================
//
// Test Strategy:
//   AC-4: `rnix record list` 显示 step 会话（UUID + legacy 目录）
//   AC-5: `rnix replay <id>` 支持 UUID 前缀匹配
//
// NOTE: Tests reference isUUIDDir(), isLegacyPIDDir(), scanStepSessions(),
// matchStepUUIDPrefix() which do NOT exist yet → compile failure = RED phase.
//
// Priority: P0 (CLI observability for UUID-based paths)
// Test Level: Unit (helper functions) + Integration (CLI behavior)

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// ---------------------------------------------------------------------------
// AC-4: isUUIDDir — identifies UUID-format directory names
// ---------------------------------------------------------------------------

func TestATDD28_2_AC4_IsUUIDDir_ValidUUID(t *testing.T) {
	// RED: isUUIDDir does not exist yet
	if !isUUIDDir("019576f2-1234-7def-8abc-def012345678") {
		t.Fatal("AC-4: should recognize valid UUID v7 directory name")
	}
}

func TestATDD28_2_AC4_IsUUIDDir_ValidUUID4(t *testing.T) {
	if !isUUIDDir("550e8400-e29b-41d4-a716-446655440000") {
		t.Fatal("AC-4: should recognize valid UUID v4 directory name")
	}
}

func TestATDD28_2_AC4_IsUUIDDir_NumericPID(t *testing.T) {
	if isUUIDDir("42") {
		t.Fatal("AC-4: numeric PID directory should NOT be identified as UUID")
	}
}

func TestATDD28_2_AC4_IsUUIDDir_InvalidString(t *testing.T) {
	if isUUIDDir("not-a-uuid") {
		t.Fatal("AC-4: arbitrary string should NOT be identified as UUID")
	}
}

func TestATDD28_2_AC4_IsUUIDDir_Empty(t *testing.T) {
	if isUUIDDir("") {
		t.Fatal("AC-4: empty string should NOT be identified as UUID")
	}
}

// ---------------------------------------------------------------------------
// AC-4: isLegacyPIDDir — identifies numeric PID directory names
// ---------------------------------------------------------------------------

func TestATDD28_2_AC4_IsLegacyPIDDir_Numeric(t *testing.T) {
	// RED: isLegacyPIDDir does not exist yet
	if !isLegacyPIDDir("42") {
		t.Fatal("AC-4: should recognize numeric PID directory")
	}
}

func TestATDD28_2_AC4_IsLegacyPIDDir_UUID(t *testing.T) {
	if isLegacyPIDDir("019576f2-1234-7def-8abc-def012345678") {
		t.Fatal("AC-4: UUID should NOT be identified as legacy PID")
	}
}

func TestATDD28_2_AC4_IsLegacyPIDDir_Zero(t *testing.T) {
	if isLegacyPIDDir("0") {
		t.Fatal("AC-4: PID 0 should NOT be considered valid legacy PID")
	}
}

// ---------------------------------------------------------------------------
// AC-4: scanStepSessions — scan data/steps/ for UUID and legacy directories
// ---------------------------------------------------------------------------

func TestATDD28_2_AC4_ScanStepSessions_UUID_Dirs(t *testing.T) {
	baseDir := t.TempDir()
	stepsDir := filepath.Join(baseDir, "data", "steps")

	// Create UUID directory with process-meta.json
	uuidName := "019576f2-1234-7def-8abc-def012345678"
	uuidDir := filepath.Join(stepsDir, uuidName)
	if err := os.MkdirAll(uuidDir, 0o755); err != nil {
		t.Fatalf("AC-4: mkdir: %v", err)
	}

	meta := struct {
		PID          types.PID `json:"pid"`
		SystemPrompt string    `json:"system_prompt"`
	}{
		PID:          types.PID(3),
		SystemPrompt: "test prompt",
	}
	metaJSON, _ := json.Marshal(meta)
	_ = os.WriteFile(filepath.Join(uuidDir, "process-meta.json"), metaJSON, 0o644)
	_ = os.WriteFile(filepath.Join(uuidDir, "steps.jsonl"), []byte(`{"step":1}`+"\n"), 0o644)

	// RED: scanStepSessions does not exist yet
	sessions, err := scanStepSessions(baseDir)
	if err != nil {
		t.Fatalf("AC-4: scanStepSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("AC-4: expected 1 session, got %d", len(sessions))
	}
	if sessions[0].UUID != uuidName {
		t.Errorf("AC-4: UUID = %q, want %q", sessions[0].UUID, uuidName)
	}
	if sessions[0].PID != 3 {
		t.Errorf("AC-4: PID = %d, want 3 (from process-meta.json)", sessions[0].PID)
	}
	if sessions[0].IsLegacy {
		t.Error("AC-4: UUID directory should not be marked as legacy")
	}
}

func TestATDD28_2_AC4_ScanStepSessions_Legacy_Dirs(t *testing.T) {
	baseDir := t.TempDir()
	stepsDir := filepath.Join(baseDir, "data", "steps")

	// Create legacy PID directory
	legacyDir := filepath.Join(stepsDir, "7")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("AC-4: mkdir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(legacyDir, "steps.jsonl"), []byte(`{"step":1}`+"\n"), 0o644)

	sessions, err := scanStepSessions(baseDir)
	if err != nil {
		t.Fatalf("AC-4: scanStepSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("AC-4: expected 1 session, got %d", len(sessions))
	}
	if !sessions[0].IsLegacy {
		t.Error("AC-4: numeric PID directory should be marked as legacy")
	}
	if sessions[0].PID != 7 {
		t.Errorf("AC-4: legacy PID = %d, want 7", sessions[0].PID)
	}
}

func TestATDD28_2_AC4_ScanStepSessions_Mixed(t *testing.T) {
	baseDir := t.TempDir()
	stepsDir := filepath.Join(baseDir, "data", "steps")

	// UUID dir
	uuid1 := "019576f2-1111-7def-8abc-def012345678"
	if err := os.MkdirAll(filepath.Join(stepsDir, uuid1), 0o755); err != nil {
		t.Fatalf("AC-4: mkdir: %v", err)
	}
	meta1, _ := json.Marshal(struct {
		PID types.PID `json:"pid"`
	}{PID: 3})
	_ = os.WriteFile(filepath.Join(stepsDir, uuid1, "process-meta.json"), meta1, 0o644)
	_ = os.WriteFile(filepath.Join(stepsDir, uuid1, "steps.jsonl"), []byte(`{"step":1}`+"\n"), 0o644)

	// Another UUID dir
	uuid2 := "019576f3-2222-7abc-9012-abcdef012345"
	if err := os.MkdirAll(filepath.Join(stepsDir, uuid2), 0o755); err != nil {
		t.Fatalf("AC-4: mkdir: %v", err)
	}
	meta2, _ := json.Marshal(struct {
		PID types.PID `json:"pid"`
	}{PID: 5})
	_ = os.WriteFile(filepath.Join(stepsDir, uuid2, "process-meta.json"), meta2, 0o644)
	_ = os.WriteFile(filepath.Join(stepsDir, uuid2, "steps.jsonl"), []byte(`{"step":1}`+"\n"), 0o644)

	// Legacy PID dir
	if err := os.MkdirAll(filepath.Join(stepsDir, "7"), 0o755); err != nil {
		t.Fatalf("AC-4: mkdir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(stepsDir, "7", "steps.jsonl"), []byte(`{"step":1}`+"\n"), 0o644)

	sessions, err := scanStepSessions(baseDir)
	if err != nil {
		t.Fatalf("AC-4: scanStepSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("AC-4: expected 3 sessions (2 UUID + 1 legacy), got %d", len(sessions))
	}

	uuidCount := 0
	legacyCount := 0
	for _, s := range sessions {
		if s.IsLegacy {
			legacyCount++
		} else {
			uuidCount++
		}
	}
	if uuidCount != 2 {
		t.Errorf("AC-4: expected 2 UUID sessions, got %d", uuidCount)
	}
	if legacyCount != 1 {
		t.Errorf("AC-4: expected 1 legacy session, got %d", legacyCount)
	}
}

func TestATDD28_2_AC4_ScanStepSessions_Empty(t *testing.T) {
	baseDir := t.TempDir()

	sessions, err := scanStepSessions(baseDir)
	if err != nil {
		t.Fatalf("AC-4: scanStepSessions on empty dir: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("AC-4: expected 0 sessions for empty dir, got %d", len(sessions))
	}
}

func TestATDD28_2_AC4_ScanStepSessions_SkipNonStepDirs(t *testing.T) {
	baseDir := t.TempDir()
	stepsDir := filepath.Join(baseDir, "data", "steps")

	// Directory without steps.jsonl should be skipped
	if err := os.MkdirAll(filepath.Join(stepsDir, "019576f2-dead-7def-8abc-def012345678"), 0o755); err != nil {
		t.Fatalf("AC-4: mkdir: %v", err)
	}

	sessions, err := scanStepSessions(baseDir)
	if err != nil {
		t.Fatalf("AC-4: scanStepSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("AC-4: dirs without steps.jsonl should be skipped, got %d sessions", len(sessions))
	}
}

// ---------------------------------------------------------------------------
// AC-5: matchStepUUIDPrefix — find step directory by UUID prefix
// ---------------------------------------------------------------------------

func TestATDD28_2_AC5_MatchStepUUIDPrefix_FullUUID(t *testing.T) {
	baseDir := t.TempDir()
	stepsDir := filepath.Join(baseDir, "data", "steps")

	uuid := "019576f2-1234-7def-8abc-def012345678"
	if err := os.MkdirAll(filepath.Join(stepsDir, uuid), 0o755); err != nil {
		t.Fatalf("AC-5: mkdir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(stepsDir, uuid, "steps.jsonl"), []byte(`{"step":1}`+"\n"), 0o644)

	// RED: matchStepUUIDPrefix does not exist yet
	matched, err := matchStepUUIDPrefix(baseDir, uuid)
	if err != nil {
		t.Fatalf("AC-5: matchStepUUIDPrefix full UUID: %v", err)
	}
	if matched != filepath.Join(stepsDir, uuid) {
		t.Fatalf("AC-5: matched = %q, want %q", matched, filepath.Join(stepsDir, uuid))
	}
}

func TestATDD28_2_AC5_MatchStepUUIDPrefix_Short8Char(t *testing.T) {
	baseDir := t.TempDir()
	stepsDir := filepath.Join(baseDir, "data", "steps")

	uuid := "019576f2-1234-7def-8abc-def012345678"
	if err := os.MkdirAll(filepath.Join(stepsDir, uuid), 0o755); err != nil {
		t.Fatalf("AC-5: mkdir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(stepsDir, uuid, "steps.jsonl"), []byte(`{"step":1}`+"\n"), 0o644)

	// 8-char prefix should match
	matched, err := matchStepUUIDPrefix(baseDir, "019576f2")
	if err != nil {
		t.Fatalf("AC-5: matchStepUUIDPrefix 8-char prefix: %v", err)
	}
	if matched != filepath.Join(stepsDir, uuid) {
		t.Fatalf("AC-5: matched = %q, want %q", matched, filepath.Join(stepsDir, uuid))
	}
}

func TestATDD28_2_AC5_MatchStepUUIDPrefix_NoMatch(t *testing.T) {
	baseDir := t.TempDir()
	stepsDir := filepath.Join(baseDir, "data", "steps")

	uuid := "019576f2-1234-7def-8abc-def012345678"
	if err := os.MkdirAll(filepath.Join(stepsDir, uuid), 0o755); err != nil {
		t.Fatalf("AC-5: mkdir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(stepsDir, uuid, "steps.jsonl"), []byte(`{"step":1}`+"\n"), 0o644)

	_, err := matchStepUUIDPrefix(baseDir, "aaaa1111")
	if err == nil {
		t.Fatal("AC-5: non-matching prefix should return error")
	}
}

func TestATDD28_2_AC5_MatchStepUUIDPrefix_Ambiguous(t *testing.T) {
	baseDir := t.TempDir()
	stepsDir := filepath.Join(baseDir, "data", "steps")

	// Two UUIDs with same 8-char prefix (unlikely but must handle)
	uuid1 := "019576f2-1111-7def-8abc-111111111111"
	uuid2 := "019576f2-2222-7def-8abc-222222222222"
	for _, u := range []string{uuid1, uuid2} {
		if err := os.MkdirAll(filepath.Join(stepsDir, u), 0o755); err != nil {
			t.Fatalf("AC-5: mkdir: %v", err)
		}
		_ = os.WriteFile(filepath.Join(stepsDir, u, "steps.jsonl"), []byte(`{"step":1}`+"\n"), 0o644)
	}

	// Ambiguous prefix should return error
	_, err := matchStepUUIDPrefix(baseDir, "019576f2")
	if err == nil {
		t.Fatal("AC-5: ambiguous prefix matching multiple dirs should return error")
	}
}

func TestATDD28_2_AC5_MatchStepUUIDPrefix_IgnoresLegacyPID(t *testing.T) {
	baseDir := t.TempDir()
	stepsDir := filepath.Join(baseDir, "data", "steps")

	// Legacy PID dir should not be matched by UUID prefix search
	if err := os.MkdirAll(filepath.Join(stepsDir, "42"), 0o755); err != nil {
		t.Fatalf("AC-5: mkdir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(stepsDir, "42", "steps.jsonl"), []byte(`{"step":1}`+"\n"), 0o644)

	_, err := matchStepUUIDPrefix(baseDir, "42")
	if err == nil {
		t.Fatal("AC-5: legacy PID directory should NOT be matched by UUID prefix search")
	}
}

func TestATDD28_2_AC5_MatchStepUUIDPrefix_EmptyDir(t *testing.T) {
	baseDir := t.TempDir()

	_, err := matchStepUUIDPrefix(baseDir, "019576f2")
	if err == nil {
		t.Fatal("AC-5: empty data dir should return error")
	}
}
