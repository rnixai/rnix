package kernel

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 42.3: 可观测层 — BuildResumeLineage 单元测试（AC#7）
//
// Covers 6 unit scenarios:
//   - UNIT-009: 单进程（无祖先无后代）
//   - UNIT-010: 3 级祖先链（C.Origin=B, B.Origin=A）
//   - UNIT-011: 5 个直接后代（5 个 procInfo.OriginUUID==current）
//   - UNIT-012: 循环链 A.Origin=B + B.Origin=A → Truncated=true
//   - UNIT-013: 35 级链 → 第 32 级截断 Truncated=true
//   - UNIT-014: UUID 不存在（procHistory + 磁盘均无）→ ErrNotFound
//
// RED PHASE:
//   - kernel.BuildResumeLineage 函数尚不存在 → 编译失败
//   - kernel.ResumeLineageData 类型尚不存在 → 编译失败
// =============================================================================

// writeProcInfoLineage writes a minimal proc-info.json with optional OriginUUID,
// used to set up lineage chain fixtures on disk.
func writeProcInfoLineage(t *testing.T, baseDir, uuid, originUUID string, resumedFromStep int) {
	t.Helper()
	dir := filepath.Join(baseDir, "steps", uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	info := map[string]any{
		"pid":      1,
		"uuid":     uuid,
		"state":    "dead",
		"intent":   "lineage chain node",
		"provider": "claude",
		"model":    "claude-4",
	}
	if originUUID != "" {
		info["origin_uuid"] = originUUID
	}
	if resumedFromStep > 0 {
		info["resumed_from_step"] = resumedFromStep
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(dir, "proc-info.json"), data, 0o600); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
}

// --- 42.3-UNIT-009: 单进程，无祖先无后代 (AC#7) ---

func TestATDD_42_3_009_BuildLineage_SoloProcess(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuid := "solo-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeProcInfoLineage(t, baseDir, uuid, "", 0)
	k.procHistory.Add(vfs.ProcInfo{PID: 1, UUID: uuid, State: types.StateDead, Intent: "solo"})

	data, err := callBuildResumeLineage(t, uuid, k.procHistory, k.GetDataDir())
	if err != nil {
		t.Fatalf("BuildResumeLineage: %v", err)
	}
	if data.Current.UUID != uuid {
		t.Errorf("Current.UUID = %q, want %q", data.Current.UUID, uuid)
	}
	if len(data.Ancestors) != 0 {
		t.Errorf("Ancestors len = %d, want 0", len(data.Ancestors))
	}
	if len(data.Descendants) != 0 {
		t.Errorf("Descendants len = %d, want 0", len(data.Descendants))
	}
	if data.Truncated {
		t.Error("Truncated = true, want false")
	}
}

// --- 42.3-UNIT-010: 3 级祖先链 (AC#7) ---

func TestATDD_42_3_010_BuildLineage_ThreeLevelAncestors(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuidA := "lvlA-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	uuidB := "lvlB-aaaaaaaa-bbbb-cccc-dddd-000000000002"
	uuidC := "lvlC-aaaaaaaa-bbbb-cccc-dddd-000000000003"
	// A — root；B → A；C → B
	writeProcInfoLineage(t, baseDir, uuidA, "", 0)
	writeProcInfoLineage(t, baseDir, uuidB, uuidA, 5)
	writeProcInfoLineage(t, baseDir, uuidC, uuidB, 8)
	k.procHistory.Add(vfs.ProcInfo{PID: 1, UUID: uuidA, State: types.StateDead, Intent: "A"})
	k.procHistory.Add(vfs.ProcInfo{PID: 2, UUID: uuidB, State: types.StateDead, Intent: "B", OriginUUID: uuidA, ResumedFromStep: 5})
	k.procHistory.Add(vfs.ProcInfo{PID: 3, UUID: uuidC, State: types.StateDead, Intent: "C", OriginUUID: uuidB, ResumedFromStep: 8})

	data, err := callBuildResumeLineage(t, uuidC, k.procHistory, k.GetDataDir())
	if err != nil {
		t.Fatalf("BuildResumeLineage: %v", err)
	}
	if data.Current.UUID != uuidC {
		t.Errorf("Current.UUID = %q, want %q", data.Current.UUID, uuidC)
	}
	if len(data.Ancestors) != 2 {
		t.Fatalf("Ancestors len = %d, want 2 (B, A)", len(data.Ancestors))
	}
	if data.Ancestors[0].UUID != uuidB {
		t.Errorf("Ancestors[0].UUID = %q, want %q (immediate parent)", data.Ancestors[0].UUID, uuidB)
	}
	if data.Ancestors[1].UUID != uuidA {
		t.Errorf("Ancestors[1].UUID = %q, want %q (root)", data.Ancestors[1].UUID, uuidA)
	}
	if data.Truncated {
		t.Error("Truncated = true, want false for 3-level chain")
	}
}

// --- 42.3-UNIT-011: 5 个直接后代 (AC#7) ---

func TestATDD_42_3_011_BuildLineage_FiveDescendants(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	parent := "parent-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	writeProcInfoLineage(t, baseDir, parent, "", 0)
	k.procHistory.Add(vfs.ProcInfo{PID: 1, UUID: parent, State: types.StateDead, Intent: "parent"})

	descs := []string{
		"child1-aaaaaaaa-bbbb-cccc-dddd-000000000001",
		"child2-aaaaaaaa-bbbb-cccc-dddd-000000000002",
		"child3-aaaaaaaa-bbbb-cccc-dddd-000000000003",
		"child4-aaaaaaaa-bbbb-cccc-dddd-000000000004",
		"child5-aaaaaaaa-bbbb-cccc-dddd-000000000005",
	}
	for i, d := range descs {
		writeProcInfoLineage(t, baseDir, d, parent, 10+i)
		k.procHistory.Add(vfs.ProcInfo{
			PID: types.PID(2 + i), UUID: d, State: types.StateDead,
			Intent: "child", OriginUUID: parent, ResumedFromStep: 10 + i,
		})
	}

	data, err := callBuildResumeLineage(t, parent, k.procHistory, k.GetDataDir())
	if err != nil {
		t.Fatalf("BuildResumeLineage: %v", err)
	}
	if len(data.Descendants) != 5 {
		t.Fatalf("Descendants len = %d, want 5", len(data.Descendants))
	}
	// 后代不要求严格顺序；用 set 校验。
	seen := make(map[string]bool)
	for _, d := range data.Descendants {
		seen[d.UUID] = true
	}
	for _, d := range descs {
		if !seen[d] {
			t.Errorf("missing descendant %q", d)
		}
	}
}

// --- 42.3-UNIT-012: 循环链 A.Origin=B + B.Origin=A → Truncated (AC#7) ---

func TestATDD_42_3_012_BuildLineage_CycleDetection(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	uuidA := "cyc-A-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	uuidB := "cyc-B-aaaaaaaa-bbbb-cccc-dddd-000000000002"
	// 恶意构造：A → B → A 循环
	writeProcInfoLineage(t, baseDir, uuidA, uuidB, 5)
	writeProcInfoLineage(t, baseDir, uuidB, uuidA, 7)
	k.procHistory.Add(vfs.ProcInfo{PID: 1, UUID: uuidA, State: types.StateDead, Intent: "A", OriginUUID: uuidB})
	k.procHistory.Add(vfs.ProcInfo{PID: 2, UUID: uuidB, State: types.StateDead, Intent: "B", OriginUUID: uuidA})

	data, err := callBuildResumeLineage(t, uuidA, k.procHistory, k.GetDataDir())
	if err != nil {
		t.Fatalf("BuildResumeLineage on cycle should not error: %v", err)
	}
	if !data.Truncated {
		t.Error("Truncated = false, want true (cycle detected)")
	}
}

// --- 42.3-UNIT-013: 35 级链 → 第 32 级截断 (AC#7) ---

func TestATDD_42_3_013_BuildLineage_DeepChainTruncation(t *testing.T) {
	k, baseDir := setupResumeKernel(t)
	// 构造 35 级链：lvl0 → lvl1 → ... → lvl34（lvl34 是根）
	var uuids []string
	for i := range 35 {
		uuid := generate35LevelUUID(i)
		uuids = append(uuids, uuid)
	}
	for i := range 35 {
		var origin string
		if i+1 < 35 {
			origin = uuids[i+1]
		}
		writeProcInfoLineage(t, baseDir, uuids[i], origin, i*2)
		k.procHistory.Add(vfs.ProcInfo{
			PID: types.PID(1 + i), UUID: uuids[i], State: types.StateDead,
			Intent: "deep", OriginUUID: origin, ResumedFromStep: i * 2,
		})
	}

	data, err := callBuildResumeLineage(t, uuids[0], k.procHistory, k.GetDataDir())
	if err != nil {
		t.Fatalf("BuildResumeLineage: %v", err)
	}
	// 深度上限 32 → ancestors 最多 32 级，Truncated=true
	if len(data.Ancestors) > 32 {
		t.Errorf("Ancestors len = %d, want <= 32 (depth cap)", len(data.Ancestors))
	}
	if !data.Truncated {
		t.Error("Truncated = false, want true (35-level chain exceeds maxDepth=32)")
	}
}

// --- 42.3-UNIT-014: UUID 不存在 → ErrNotFound (AC#7) ---

func TestATDD_42_3_014_BuildLineage_UUIDNotFound(t *testing.T) {
	k, _ := setupResumeKernel(t)
	missingUUID := "missing-aaaaaaaa-bbbb-cccc-dddd-000000000001"

	_, err := callBuildResumeLineage(t, missingUUID, k.procHistory, k.GetDataDir())
	if err == nil {
		t.Fatal("BuildResumeLineage should return error for missing UUID")
	}
	// 期望 ErrNotFound（包级 helper 或 *SyscallError{Code: ErrNotFound}）
	if !isErrNotFound(err) {
		t.Errorf("err = %v, want ErrNotFound semantics", err)
	}
}

// =============================================================================
// GREEN PHASE: replace t.Fatal stubs with real kernel.BuildResumeLineage calls.
// =============================================================================

// resumeLineageDataStub mirrors the expected kernel.ResumeLineageData shape so
// tests can assert on Current / Ancestors / Descendants / Truncated. We map
// from the real ResumeLineageData here to keep the test surface stable.
type resumeLineageDataStub struct {
	Current     vfs.ProcInfo
	Ancestors   []vfs.ProcInfo
	Descendants []vfs.ProcInfo
	Truncated   bool
}

// callBuildResumeLineage delegates to kernel.BuildResumeLineage and rewraps
// the result into resumeLineageDataStub (a copy with same shape) so the
// existing test assertions continue to work.
func callBuildResumeLineage(t *testing.T, uuid string, history *ProcessHistory, baseDir string) (*resumeLineageDataStub, error) {
	t.Helper()
	data, err := BuildResumeLineage(uuid, history, baseDir)
	if err != nil {
		return nil, err
	}
	return &resumeLineageDataStub{
		Current:     data.Current,
		Ancestors:   data.Ancestors,
		Descendants: data.Descendants,
		Truncated:   data.Truncated,
	}, nil
}

// isErrNotFound returns true when err carries an ErrNotFound semantics.
// Replace by errors.Is(err, types.ErrNotFound-equivalent) when SyscallError
// Unwrap chain is in place.
func isErrNotFound(err error) bool {
	if err == nil {
		return false
	}
	var se *SyscallError
	if errors.As(err, &se) {
		return se.Code == types.ErrNotFound
	}
	return false
}

// generate35LevelUUID builds a deterministic 36-char UUID-like string indexed
// by `i` so the cycle/truncation tests have unique nodes without external deps.
func generate35LevelUUID(i int) string {
	const base = "deep00-aaaaaaaa-bbbb-cccc-dddd-"
	hex := "0123456789abcdef"
	tail := []byte("000000000000")
	tail[len(tail)-1] = hex[i%16]
	tail[len(tail)-2] = hex[(i/16)%16]
	tail[len(tail)-3] = hex[(i/256)%16]
	return base + string(tail)
}
