package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 21.5: Skill 组合矩阵
//
// SynergyComboKey、SynergyRecord、SynergyMatrix、ComboSummary
// 类型和存储引擎的单元测试。
// 测试引用的类型和方法尚不存在，测试将无法编译直到实现完成。
//
// RED → GREEN: 在 kernel/synergy_matrix.go 中实现所有类型和方法。
// ============================================================

// --- 21.5-UNIT-001: [P0] NewComboKey sorted deterministic (AC1) ---

func TestNewComboKey_SortedDeterministic(t *testing.T) {
	// Given: two skill lists in different order
	keyAB := NewComboKey([]string{"review", "analysis"})
	keyBA := NewComboKey([]string{"analysis", "review"})

	// Then: both produce the same key (sorted)
	if keyAB != keyBA {
		t.Errorf("expected same key for {review,analysis} and {analysis,review}, got %q vs %q", keyAB, keyBA)
	}
	// And: key is in alphabetical order
	if string(keyAB) != "analysis,review" {
		t.Errorf("expected key %q, got %q", "analysis,review", keyAB)
	}
}

// --- 21.5-UNIT-002: [P0] NewComboKey single skill (AC1) ---

func TestNewComboKey_SingleSkill(t *testing.T) {
	// Given: a single skill
	key := NewComboKey([]string{"analysis"})

	// Then: key is the skill name itself
	if string(key) != "analysis" {
		t.Errorf("expected key %q, got %q", "analysis", key)
	}
}

// --- 21.5-UNIT-003: [P1] NewComboKey empty returns empty string (AC1) ---

func TestNewComboKey_Empty(t *testing.T) {
	// Given: an empty skill list
	key := NewComboKey([]string{})

	// Then: key is empty string
	if string(key) != "" {
		t.Errorf("expected empty key, got %q", key)
	}
}

// --- 21.5-UNIT-004: [P0] SynergyMatrix RecordCombo and GetAllRecords (AC1) ---

func TestSynergyMatrix_RecordAndRead(t *testing.T) {
	// Given: a temporary directory for synergy matrix
	dir := t.TempDir()
	matrix := NewSynergyMatrix(dir)

	// When: writing 3 records
	records := []SynergyRecord{
		{
			ComboKey:   NewComboKey([]string{"analysis", "review"}),
			Skills:     []string{"analysis", "review"},
			Passed:     true,
			TokensUsed: 1000,
			DurationMs: 5000,
			Timestamp:  time.Now(),
		},
		{
			ComboKey:   NewComboKey([]string{"analysis", "review"}),
			Skills:     []string{"analysis", "review"},
			Passed:     false,
			TokensUsed: 1500,
			DurationMs: 8000,
			Timestamp:  time.Now(),
		},
		{
			ComboKey:   NewComboKey([]string{"security"}),
			Skills:     []string{"security"},
			Passed:     true,
			TokensUsed: 800,
			DurationMs: 3000,
			Timestamp:  time.Now(),
		},
	}

	for _, rec := range records {
		if err := matrix.RecordCombo(rec); err != nil {
			t.Fatalf("RecordCombo failed: %v", err)
		}
	}

	// Then: reading all records returns 3
	all, err := matrix.GetAllRecords()
	if err != nil {
		t.Fatalf("GetAllRecords failed: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 records, got %d", len(all))
	}

	// And: first record has correct combo key
	if all[0].ComboKey != NewComboKey([]string{"analysis", "review"}) {
		t.Errorf("first record combo_key = %q, expected analysis,review", all[0].ComboKey)
	}
	if !all[0].Passed {
		t.Error("first record should be passed=true")
	}
	if all[0].TokensUsed != 1000 {
		t.Errorf("first record tokens_used = %d, expected 1000", all[0].TokensUsed)
	}
}

// --- 21.5-UNIT-005: [P0] SynergyMatrix empty file returns empty slice (AC6) ---

func TestSynergyMatrix_EmptyFile(t *testing.T) {
	// Given: a temp directory with no synergy file
	dir := t.TempDir()
	matrix := NewSynergyMatrix(dir)

	// When: reading all records
	all, err := matrix.GetAllRecords()

	// Then: no error, empty slice
	if err != nil {
		t.Fatalf("GetAllRecords on empty should not error: %v", err)
	}
	if all == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(all) != 0 {
		t.Fatalf("expected 0 records, got %d", len(all))
	}
}

// --- 21.5-UNIT-006: [P1] SynergyMatrix concurrent writes (AC1) ---

func TestSynergyMatrix_ConcurrentWrites(t *testing.T) {
	// Given: a synergy matrix
	dir := t.TempDir()
	matrix := NewSynergyMatrix(dir)

	// When: 10 goroutines each write 5 records concurrently
	const goroutines = 10
	const recordsPerGoroutine = 5
	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := range recordsPerGoroutine {
				rec := SynergyRecord{
					ComboKey:   NewComboKey([]string{"skill-a", "skill-b"}),
					Skills:     []string{"skill-a", "skill-b"},
					Passed:     (idx+j)%2 == 0,
					TokensUsed: 1000 + idx*100 + j*10,
					DurationMs: int64(5000 + idx*100),
					Timestamp:  time.Now(),
				}
				if err := matrix.RecordCombo(rec); err != nil {
					t.Errorf("concurrent RecordCombo failed: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()

	// Then: all records are present
	all, err := matrix.GetAllRecords()
	if err != nil {
		t.Fatalf("GetAllRecords failed: %v", err)
	}
	expected := goroutines * recordsPerGoroutine
	if len(all) != expected {
		t.Fatalf("expected %d records after concurrent writes, got %d", expected, len(all))
	}
}

// --- 21.5-UNIT-007: [P0] GetComboSummaries basic stats (AC2, AC3) ---

func TestGetComboSummaries_BasicStats(t *testing.T) {
	// Given: records with known values
	dir := t.TempDir()
	matrix := NewSynergyMatrix(dir)

	// Solo records for baseline
	for _, rec := range []SynergyRecord{
		{ComboKey: NewComboKey([]string{"analysis"}), Skills: []string{"analysis"}, Passed: true, TokensUsed: 1000, DurationMs: 5000, Timestamp: time.Now()},
		{ComboKey: NewComboKey([]string{"analysis"}), Skills: []string{"analysis"}, Passed: true, TokensUsed: 1200, DurationMs: 6000, Timestamp: time.Now()},
		{ComboKey: NewComboKey([]string{"review"}), Skills: []string{"review"}, Passed: true, TokensUsed: 800, DurationMs: 4000, Timestamp: time.Now()},
		{ComboKey: NewComboKey([]string{"review"}), Skills: []string{"review"}, Passed: false, TokensUsed: 900, DurationMs: 5000, Timestamp: time.Now()},
	} {
		if err := matrix.RecordCombo(rec); err != nil {
			t.Fatalf("RecordCombo failed: %v", err)
		}
	}

	// Combo records
	for _, rec := range []SynergyRecord{
		{ComboKey: NewComboKey([]string{"analysis", "review"}), Skills: []string{"analysis", "review"}, Passed: true, TokensUsed: 900, DurationMs: 4500, Timestamp: time.Now()},
		{ComboKey: NewComboKey([]string{"analysis", "review"}), Skills: []string{"analysis", "review"}, Passed: true, TokensUsed: 1100, DurationMs: 5500, Timestamp: time.Now()},
	} {
		if err := matrix.RecordCombo(rec); err != nil {
			t.Fatalf("RecordCombo failed: %v", err)
		}
	}

	// When: computing summaries
	summaries, err := matrix.GetComboSummaries()
	if err != nil {
		t.Fatalf("GetComboSummaries failed: %v", err)
	}

	// Then: expect 3 combo groups (analysis, review, analysis+review)
	if len(summaries) < 3 {
		t.Fatalf("expected at least 3 summaries, got %d", len(summaries))
	}

	// Find the combo summary
	var comboSummary *ComboSummary
	for i := range summaries {
		if summaries[i].ComboKey == NewComboKey([]string{"analysis", "review"}) && len(summaries[i].Skills) == 2 {
			comboSummary = &summaries[i]
			break
		}
	}
	if comboSummary == nil {
		t.Fatal("combo summary for analysis+review not found")
	}

	// Verify: success rate = 2/2 = 1.0
	if comboSummary.SuccessRate != 1.0 {
		t.Errorf("combo success_rate = %.2f, expected 1.0", comboSummary.SuccessRate)
	}

	// Verify: avg tokens = (900+1100)/2 = 1000
	if comboSummary.AvgTokens != 1000 {
		t.Errorf("combo avg_tokens = %d, expected 1000", comboSummary.AvgTokens)
	}

	// Verify: total executions = 2
	if comboSummary.TotalExecutions != 2 {
		t.Errorf("combo total_executions = %d, expected 2", comboSummary.TotalExecutions)
	}
}

// --- 21.5-UNIT-008: [P0] GetComboSummaries recommended (AC4) ---

func TestGetComboSummaries_Recommended(t *testing.T) {
	// Given: combo that performs >10% better than solo average
	dir := t.TempDir()
	matrix := NewSynergyMatrix(dir)

	// Solo: analysis 50% success, review 50% success -> avg solo = 50%
	records := []SynergyRecord{
		{ComboKey: NewComboKey([]string{"analysis"}), Skills: []string{"analysis"}, Passed: true, TokensUsed: 1000, DurationMs: 5000, Timestamp: time.Now()},
		{ComboKey: NewComboKey([]string{"analysis"}), Skills: []string{"analysis"}, Passed: false, TokensUsed: 1200, DurationMs: 6000, Timestamp: time.Now()},
		{ComboKey: NewComboKey([]string{"review"}), Skills: []string{"review"}, Passed: true, TokensUsed: 800, DurationMs: 4000, Timestamp: time.Now()},
		{ComboKey: NewComboKey([]string{"review"}), Skills: []string{"review"}, Passed: false, TokensUsed: 900, DurationMs: 5000, Timestamp: time.Now()},
		// Combo: 90% success (9 out of 10) -> avg solo 50% + 10% = 60%, combo 90% > 60%
	}
	for _, rec := range records {
		_ = matrix.RecordCombo(rec)
	}
	for i := range 10 {
		_ = matrix.RecordCombo(SynergyRecord{
			ComboKey:   NewComboKey([]string{"analysis", "review"}),
			Skills:     []string{"analysis", "review"},
			Passed:     i < 9, // 9 passed, 1 failed
			TokensUsed: 800,
			DurationMs: 4000,
			Timestamp:  time.Now(),
		})
	}

	// When: computing summaries
	summaries, err := matrix.GetComboSummaries()
	if err != nil {
		t.Fatalf("GetComboSummaries failed: %v", err)
	}

	// Then: combo should be marked recommended
	var comboSummary *ComboSummary
	for i := range summaries {
		if len(summaries[i].Skills) == 2 {
			comboSummary = &summaries[i]
			break
		}
	}
	if comboSummary == nil {
		t.Fatal("combo summary not found")
	}

	if !comboSummary.Recommended {
		t.Errorf("expected combo to be recommended (success_rate=%.2f vs avg_solo=%.2f)",
			comboSummary.SuccessRate, comboSummary.AvgSoloRate)
	}
}

// --- 21.5-UNIT-009: [P0] GetComboSummaries not recommended (AC4) ---

func TestGetComboSummaries_NotRecommended(t *testing.T) {
	// Given: combo that performs only slightly better than solo
	dir := t.TempDir()
	matrix := NewSynergyMatrix(dir)

	// Solo: analysis 70%, review 70% -> avg solo = 70%
	for i := range 10 {
		_ = matrix.RecordCombo(SynergyRecord{
			ComboKey: NewComboKey([]string{"analysis"}), Skills: []string{"analysis"},
			Passed: i < 7, TokensUsed: 1000, DurationMs: 5000, Timestamp: time.Now(),
		})
		_ = matrix.RecordCombo(SynergyRecord{
			ComboKey: NewComboKey([]string{"review"}), Skills: []string{"review"},
			Passed: i < 7, TokensUsed: 900, DurationMs: 4000, Timestamp: time.Now(),
		})
	}
	// Combo: 75% success -> only 5% above solo avg, below 10% threshold
	for i := range 20 {
		_ = matrix.RecordCombo(SynergyRecord{
			ComboKey: NewComboKey([]string{"analysis", "review"}), Skills: []string{"analysis", "review"},
			Passed: i < 15, TokensUsed: 850, DurationMs: 4500, Timestamp: time.Now(),
		})
	}

	// When: computing summaries
	summaries, err := matrix.GetComboSummaries()
	if err != nil {
		t.Fatalf("GetComboSummaries failed: %v", err)
	}

	// Then: combo should NOT be recommended (75% - 70% = 5% < 10%)
	var comboSummary *ComboSummary
	for i := range summaries {
		if len(summaries[i].Skills) == 2 {
			comboSummary = &summaries[i]
			break
		}
	}
	if comboSummary == nil {
		t.Fatal("combo summary not found")
	}

	if comboSummary.Recommended {
		t.Errorf("expected combo NOT to be recommended (success_rate=%.2f vs avg_solo=%.2f, diff=%.2f < 0.10)",
			comboSummary.SuccessRate, comboSummary.AvgSoloRate, comboSummary.SuccessRate-comboSummary.AvgSoloRate)
	}
}

// --- 21.5-UNIT-010: [P1] GetComboSummaries no solo data (AC4) ---

func TestGetComboSummaries_NoSoloData(t *testing.T) {
	// Given: only combo records, no solo baselines
	dir := t.TempDir()
	matrix := NewSynergyMatrix(dir)

	for i := range 5 {
		_ = matrix.RecordCombo(SynergyRecord{
			ComboKey: NewComboKey([]string{"analysis", "review"}), Skills: []string{"analysis", "review"},
			Passed: i < 4, TokensUsed: 1000, DurationMs: 5000, Timestamp: time.Now(),
		})
	}

	// When: computing summaries
	summaries, err := matrix.GetComboSummaries()
	if err != nil {
		t.Fatalf("GetComboSummaries failed: %v", err)
	}

	// Then: AvgSoloRate = 0, Recommended = false (no baseline data)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].AvgSoloRate != 0 {
		t.Errorf("expected AvgSoloRate=0 without solo data, got %.2f", summaries[0].AvgSoloRate)
	}
	if summaries[0].Recommended {
		t.Error("expected Recommended=false without solo baseline data")
	}
}

// --- 21.5-UNIT-011: [P0] GetComboSummaries sort order (AC2) ---

func TestGetComboSummaries_SortOrder(t *testing.T) {
	// Given: multiple combos with different success rates
	dir := t.TempDir()
	matrix := NewSynergyMatrix(dir)

	// Solo baselines (low rate so combos can be recommended)
	for i := range 10 {
		_ = matrix.RecordCombo(SynergyRecord{
			ComboKey: NewComboKey([]string{"a"}), Skills: []string{"a"},
			Passed: i < 2, TokensUsed: 1000, DurationMs: 5000, Timestamp: time.Now(),
		})
		_ = matrix.RecordCombo(SynergyRecord{
			ComboKey: NewComboKey([]string{"b"}), Skills: []string{"b"},
			Passed: i < 2, TokensUsed: 1000, DurationMs: 5000, Timestamp: time.Now(),
		})
		_ = matrix.RecordCombo(SynergyRecord{
			ComboKey: NewComboKey([]string{"c"}), Skills: []string{"c"},
			Passed: i < 2, TokensUsed: 1000, DurationMs: 5000, Timestamp: time.Now(),
		})
	}

	// Combo a+b: 90% success (recommended)
	for i := range 10 {
		_ = matrix.RecordCombo(SynergyRecord{
			ComboKey: NewComboKey([]string{"a", "b"}), Skills: []string{"a", "b"},
			Passed: i < 9, TokensUsed: 800, DurationMs: 4000, Timestamp: time.Now(),
		})
	}
	// Combo a+c: 50% success (not recommended, but higher than a+b+c)
	for i := range 10 {
		_ = matrix.RecordCombo(SynergyRecord{
			ComboKey: NewComboKey([]string{"a", "c"}), Skills: []string{"a", "c"},
			Passed: i < 5, TokensUsed: 900, DurationMs: 4500, Timestamp: time.Now(),
		})
	}
	// Combo b+c: 70% success (recommended)
	for i := range 10 {
		_ = matrix.RecordCombo(SynergyRecord{
			ComboKey: NewComboKey([]string{"b", "c"}), Skills: []string{"b", "c"},
			Passed: i < 7, TokensUsed: 850, DurationMs: 4200, Timestamp: time.Now(),
		})
	}

	// When: computing summaries
	summaries, err := matrix.GetComboSummaries()
	if err != nil {
		t.Fatalf("GetComboSummaries failed: %v", err)
	}

	// Then: recommended combos come first, then sorted by success rate descending
	// Expected order: recommended first (a+b 90%, b+c 70%), then non-recommended (a+c 50%)
	// among solos: a, b, c at 20% each (not recommended, same rate)
	foundRecommended := false
	foundNonRecommended := false
	for _, s := range summaries {
		if len(s.Skills) >= 2 {
			if s.Recommended {
				if foundNonRecommended {
					t.Error("recommended combo appeared after non-recommended combo in sort order")
				}
				foundRecommended = true
			} else {
				foundNonRecommended = true
			}
		}
	}
	if !foundRecommended {
		t.Error("expected at least one recommended combo")
	}
}

// --- 21.5-UNIT-012: [P0] GetComboSummaries empty data (AC6) ---

func TestGetComboSummaries_Empty(t *testing.T) {
	// Given: no records
	dir := t.TempDir()
	matrix := NewSynergyMatrix(dir)

	// When: computing summaries
	summaries, err := matrix.GetComboSummaries()

	// Then: no error, empty slice
	if err != nil {
		t.Fatalf("GetComboSummaries on empty should not error: %v", err)
	}
	if summaries == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries, got %d", len(summaries))
	}
}

// --- 21.5-UNIT-013: [P1] SynergyRecord JSON serialization (AC5) ---

func TestSynergyRecord_JSONSerialization(t *testing.T) {
	// Given: a SynergyRecord
	rec := SynergyRecord{
		ComboKey:   NewComboKey([]string{"analysis", "review"}),
		Skills:     []string{"analysis", "review"},
		Passed:     true,
		TokensUsed: 1000,
		DurationMs: 5000,
		Timestamp:  time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC),
	}

	// When: serializing to JSON
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON fields are snake_case
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	requiredFields := []string{"combo_key", "skills", "passed", "tokens_used", "duration_ms", "timestamp"}
	for _, field := range requiredFields {
		if _, ok := m[field]; !ok {
			t.Errorf("missing JSON field %q", field)
		}
	}
}

// --- 21.5-UNIT-014: [P1] ComboSummary JSON serialization (AC5) ---

func TestComboSummary_JSONSerialization(t *testing.T) {
	// Given: a ComboSummary
	summary := ComboSummary{
		ComboKey:         NewComboKey([]string{"analysis", "review"}),
		Skills:           []string{"analysis", "review"},
		SuccessRate:      0.85,
		AvgTokens:        1200,
		TotalExecutions:  20,
		AvgSoloRate:      0.70,
		TokenImprovement: 12.3,
		Recommended:      true,
	}

	// When: serializing to JSON
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON fields are snake_case
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	requiredFields := []string{"combo_key", "skills", "success_rate", "avg_tokens",
		"total_executions", "avg_solo_rate", "token_improvement", "recommended"}
	for _, field := range requiredFields {
		if _, ok := m[field]; !ok {
			t.Errorf("missing JSON field %q", field)
		}
	}
}

// --- 21.5-UNIT-015: [P1] SynergyMatrix file persistence (AC7) ---

func TestSynergyMatrix_FilePersistence(t *testing.T) {
	// Given: records written by one SynergyMatrix instance
	dir := t.TempDir()
	matrix1 := NewSynergyMatrix(dir)
	_ = matrix1.RecordCombo(SynergyRecord{
		ComboKey: NewComboKey([]string{"a", "b"}), Skills: []string{"a", "b"},
		Passed: true, TokensUsed: 1000, DurationMs: 5000, Timestamp: time.Now(),
	})

	// When: creating a second SynergyMatrix instance on the same directory
	matrix2 := NewSynergyMatrix(dir)
	all, err := matrix2.GetAllRecords()

	// Then: records persist across instances (simulates daemon restart)
	if err != nil {
		t.Fatalf("GetAllRecords from second instance failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 record after re-open, got %d", len(all))
	}
}

// --- 21.5-UNIT-016: [P1] SynergyMatrix file path is in reputation directory (AC7) ---

func TestSynergyMatrix_FilePathInReputationDir(t *testing.T) {
	// Given: a SynergyMatrix with known reputation dir
	dir := t.TempDir()
	matrix := NewSynergyMatrix(dir)

	// When: writing a record
	_ = matrix.RecordCombo(SynergyRecord{
		ComboKey: NewComboKey([]string{"a"}), Skills: []string{"a"},
		Passed: true, TokensUsed: 500, DurationMs: 2000, Timestamp: time.Now(),
	})

	// Then: file is at $dir/synergy-matrix.json
	expected := filepath.Join(dir, "synergy-matrix.json")
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Errorf("expected file at %s but it does not exist", expected)
	}
}

// --- 21.5-UNIT-017: [P1] GetComboSummaries token improvement calculation (AC3) ---

func TestGetComboSummaries_TokenImprovement(t *testing.T) {
	// Given: solo avg tokens = 1000, combo avg tokens = 800
	// Expected: improvement = (1000-800)/1000 * 100 = 20%
	dir := t.TempDir()
	matrix := NewSynergyMatrix(dir)

	// Solo: analysis avg_tokens = 1000
	_ = matrix.RecordCombo(SynergyRecord{
		ComboKey: NewComboKey([]string{"analysis"}), Skills: []string{"analysis"},
		Passed: true, TokensUsed: 1000, DurationMs: 5000, Timestamp: time.Now(),
	})
	// Solo: review avg_tokens = 1000
	_ = matrix.RecordCombo(SynergyRecord{
		ComboKey: NewComboKey([]string{"review"}), Skills: []string{"review"},
		Passed: true, TokensUsed: 1000, DurationMs: 5000, Timestamp: time.Now(),
	})
	// Combo: avg_tokens = 800
	_ = matrix.RecordCombo(SynergyRecord{
		ComboKey: NewComboKey([]string{"analysis", "review"}), Skills: []string{"analysis", "review"},
		Passed: true, TokensUsed: 800, DurationMs: 4000, Timestamp: time.Now(),
	})

	// When: computing summaries
	summaries, err := matrix.GetComboSummaries()
	if err != nil {
		t.Fatalf("GetComboSummaries failed: %v", err)
	}

	// Then: combo's TokenImprovement should be ~20%
	var comboSummary *ComboSummary
	for i := range summaries {
		if len(summaries[i].Skills) == 2 {
			comboSummary = &summaries[i]
			break
		}
	}
	if comboSummary == nil {
		t.Fatal("combo summary not found")
	}

	// TokenImprovement = (soloAvg - comboAvg) / soloAvg * 100 = (1000-800)/1000 * 100 = 20
	if comboSummary.TokenImprovement < 19.9 || comboSummary.TokenImprovement > 20.1 {
		t.Errorf("expected TokenImprovement ~20%%, got %.1f%%", comboSummary.TokenImprovement)
	}
}
