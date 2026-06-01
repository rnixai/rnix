package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/kernel"
)

// ATDD 51.1 — AC7（生产装配）回归测试。
//
// 来源 Story：_bmad-output/implementation-artifacts/51-1-diffmemory-persistence.md（AC7）
// 追溯缺口：_bmad-output/test-artifacts/traceability/traceability-51-1.md（G-1）
//
// 背景：kernel 包的 atdd_51_1_diffmemory_persistence_test.go 已充分覆盖
// DiffMemory 持久化机制本身（AC1–9），但 daemon 的「生产装配点」——把持久化
// DiffMemory 接到内核、并在加载失败时优雅回退——此前无任何自动化测试守护。
// 该接线若被未来 main.go 重构静默回退为纯内存，整个「跨重启存活」能力会无声
// 失效且不报错（隐蔽性高）。本文件锁定装配契约，把 trace 门禁从 CONCERNS 升 PASS。
//
// 被测对象：assembleDiffMemory(cwd)（main.go），封装
//   resolveDataDir(cwd,"diffmemory")/diffmemory.json 路径约定
//   + NewDiffMemoryWithPersistence + 加载失败回退 NewDiffMemory（不 panic）。

// readPersistedDiffEntries 逐行解析持久化文件为 kernel.DiffMemoryEntry（跳过空行）。
func readPersistedDiffEntries(t *testing.T, path string) []kernel.DiffMemoryEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persistence file %s: %v", path, err)
	}
	var out []kernel.DiffMemoryEntry
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e kernel.DiffMemoryEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("unmarshal persisted line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

// AC7（路径约定 + 落盘）：装配后的 store Record 必须落盘到
// resolveDataDir(cwd,"diffmemory")/diffmemory.json，且为合法 JSONL。
func TestATDD_51_1_AC7_AssembleUsesPersistencePath(t *testing.T) {
	dataDir := t.TempDir()

	dm := assembleDiffMemory(dataDir)
	if dm == nil {
		t.Fatal("assembleDiffMemory returned nil")
	}
	dm.Record("build rest api", []string{"go-coder", "api-designer"})

	wantPath := filepath.Join(dataDir, "diffmemory", "diffmemory.json")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected persistence file at convention path %s: %v", wantPath, err)
	}
	entries := readPersistedDiffEntries(t, wantPath)
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 JSONL line, got %d", len(entries))
	}
	got := entries[0]
	if got.Intent != "build rest api" {
		t.Errorf("intent round-trip mismatch: got %q", got.Intent)
	}
	if len(got.Skills) != 2 || got.Skills[0] != "go-coder" || got.Skills[1] != "api-designer" {
		t.Errorf("skills round-trip mismatch: got %v", got.Skills)
	}
}

// AC7（核心 · 跨重启往返存活，端到端含路径约定）：assemble → Record →
// 同 cwd 再 assemble（模拟 daemon 重启）→ Lookup 命中正确 skills。
// 这是 AC3「跨重启存活」在生产装配层的复现：若装配点退化为纯内存或路径不一致，
// 第二次 assemble 将 Lookup miss。
func TestATDD_51_1_AC7_PathsSurviveSimulatedRestart(t *testing.T) {
	dataDir := t.TempDir()

	d1 := assembleDiffMemory(dataDir)
	d1.Record("analyze system logs", []string{"log-analyst"})
	d1.Record("write integration tests", []string{"tester", "qa"})

	// 模拟 daemon 重启：同一 dataDir 重新装配。
	d2 := assembleDiffMemory(dataDir)
	if skills, ok := d2.Lookup("analyze system logs"); !ok || len(skills) != 1 || skills[0] != "log-analyst" {
		t.Fatalf("path 1 did not survive simulated restart via assembly: skills=%v ok=%v", skills, ok)
	}
	if skills, ok := d2.Lookup("write integration tests"); !ok || len(skills) != 2 || skills[0] != "tester" {
		t.Fatalf("path 2 did not survive simulated restart via assembly: skills=%v ok=%v", skills, ok)
	}
}

// AC7（加载失败 → 回退纯内存，不 panic）：当持久化文件不可打开（非 NotExist
// 错误）时，assembleDiffMemory 必须 warn + 回退到可用的纯内存 store，绝不 panic、
// 不阻断 daemon 启动。
//
// 触发手法：把约定路径中的目录段 ".rnix/data/diffmemory" 预先占成一个**文件**，
// 这样 os.Open(".rnix/data/diffmemory/diffmemory.json") 会返回 ENOTDIR（非
// os.IsNotExist），使 NewDiffMemoryWithPersistence 的 load() 返回错误，进入回退分支。
func TestATDD_51_1_AC7_LoadFailureFallsBackToInMemory(t *testing.T) {
	dataDir := t.TempDir()

	// 让约定路径的父目录段成为文件，制造 ENOTDIR。
	blocker := filepath.Join(dataDir, "diffmemory") // 期望是目录，这里占成文件
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	var dm *kernel.DiffMemory
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("assembleDiffMemory must not panic on load failure, panic: %v", r)
			}
		}()
		dm = assembleDiffMemory(dataDir)
	}()

	if dm == nil {
		t.Fatal("assembleDiffMemory returned nil on load failure (expected in-memory fallback)")
	}
	// 回退后仍是可用的纯内存 store：Record + Lookup 正常工作。
	dm.Record("fallback intent", []string{"mem-skill"})
	if skills, ok := dm.Lookup("fallback intent"); !ok || len(skills) != 1 || skills[0] != "mem-skill" {
		t.Fatalf("in-memory fallback store not usable: skills=%v ok=%v", skills, ok)
	}
}
