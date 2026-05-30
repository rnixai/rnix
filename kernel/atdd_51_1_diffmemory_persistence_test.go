package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ATDD 51.1: DiffMemory 持久化（仿 reputation JSONL 落盘 + 启动加载）
//
// 来源 Story：_bmad-output/implementation-artifacts/51-1-diffmemory-persistence.md
// 审计来源：completion-audit-suspects-2026-05-30.md §A2 (AR-3)
// Story Key: 51-1-diffmemory-persistence
//
// 验收标准（Acceptance Criteria，节选 unit 可测项）：
//   AC1（落盘）        : Record 后以 JSONL 追加持久化 DiffMemoryEntry（intent/skills/timestamp/hit_count）
//   AC2（启动加载）    : 带路径新建实例时 replay 文件；缺文件不报错；同签名 last-wins
//   AC3（跨重启存活）  : record → 同路径新实例 → Lookup 命中（核心结果级断言）
//   AC4（HitCount 存活）: replay 恢复 hit_count，且重启后 eviction 仍基于恢复的 hit_count
//   AC5（load 尊重 maxSize）: distinct intent 数 > maxSize → 加载后驱逐至 maxSize（保高 hit/新 ts）
//   AC6（向后兼容）    : NewDiffMemory(maxSize) 保持纯内存、零文件写
//   AC8（启动鲁棒性）  : 空行/损坏行跳过，不阻断加载、不 panic
//   AC9（并发安全）    : 带持久化 Record 并发 + -race 干净
//   （AC7 生产装配 / AC10 质量门为 main.go 接线与 make all，非 unit 范围，不在此文件覆盖）
//
// ============================== RED PHASE ==============================
// 本测试在 Story 51.1 实现前应 FAIL（红灯）。
// 预期失败原因：kernel 包无法编译——构造函数 NewDiffMemoryWithPersistence
// 尚未实现（undefined）。实现后所有用例应转绿（含 go test -race）。
// 这是 Go ATDD 的标准红灯形态（缺失 API → 包编译失败），与
// kernel 既有 atdd_* 持久化测试的红灯模式一致。
//
// 注意：测试严格使用 Story Dev Notes 规定的契约：
//   - 构造函数：NewDiffMemoryWithPersistence(maxSize int, filePath string) (*DiffMemory, error)
//   - filePath 为完整文件路径（约定 .rnix/data/diffmemory/diffmemory.json）
//   - 落盘行格式直接复用既有 DiffMemoryEntry（无新增字段）
//   - NewDiffMemory(maxSize) 签名与纯内存语义保持不变
// 断言只依赖既有公共 API（Record / Lookup）+ 直接文件检查，不臆造访问器。
// ======================================================================

// tempDiffMemoryFile 返回隔离的持久化文件完整路径（父目录可能尚不存在）。
func tempDiffMemoryFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".rnix", "data", "diffmemory", "diffmemory.json")
}

// writeDiffMemoryFixture 把若干 entry 以 JSONL 形式（外加可选原始行）预写到文件，
// 用于构造"加载"场景。rawLines 按原样作为整行写入（用于注入空行 / 损坏行）。
func writeDiffMemoryFixture(t *testing.T, path string, entries []DiffMemoryEntry, rawLines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture dir: %v", err)
	}
	var b strings.Builder
	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal fixture entry: %v", err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	for _, raw := range rawLines {
		b.WriteString(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

// readDiffMemoryEntries 逐行解析持久化文件为 DiffMemoryEntry（跳过空行）。
func readDiffMemoryEntries(t *testing.T, path string) []DiffMemoryEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persistence file: %v", err)
	}
	var out []DiffMemoryEntry
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e DiffMemoryEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("unmarshal persisted line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

// --------------------------------------------------------------------------
// AC1（落盘）：Record 后文件存在，且为合法 JSONL，字段可往返。
// --------------------------------------------------------------------------

func TestATDD_51_1_AC1_RecordPersistsJSONL(t *testing.T) {
	path := tempDiffMemoryFile(t)
	d, err := NewDiffMemoryWithPersistence(128, path)
	if err != nil {
		t.Fatalf("NewDiffMemoryWithPersistence: %v", err)
	}

	d.Record("build rest api", []string{"go-coder", "api-designer"})

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected persistence file at %s: %v", path, err)
	}
	entries := readDiffMemoryEntries(t, path)
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
	if got.HitCount != 1 {
		t.Errorf("expected fresh record HitCount=1, got %d", got.HitCount)
	}
}

// AC1（nil skills 防护）：Record(intent, nil) 持久化的 skills 应为 [] 而非 JSON null。
func TestATDD_51_1_AC1_NilSkillsPersistedAsEmptyArray(t *testing.T) {
	path := tempDiffMemoryFile(t)
	d, err := NewDiffMemoryWithPersistence(128, path)
	if err != nil {
		t.Fatalf("NewDiffMemoryWithPersistence: %v", err)
	}

	d.Record("empty skills intent", nil)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persistence file: %v", err)
	}
	line := strings.TrimSpace(string(data))
	if strings.Contains(line, `"skills":null`) {
		t.Fatalf("nil skills must serialize as [], got JSON null: %s", line)
	}
	if !strings.Contains(line, `"skills":[]`) {
		t.Fatalf("expected skills to serialize as empty array, got: %s", line)
	}
}

// --------------------------------------------------------------------------
// AC2 + AC3（启动加载 / 跨重启存活，核心）：record → 同路径新实例 → Lookup 命中。
// --------------------------------------------------------------------------

func TestATDD_51_1_AC3_PathsSurviveRestart(t *testing.T) {
	path := tempDiffMemoryFile(t)

	d1, err := NewDiffMemoryWithPersistence(128, path)
	if err != nil {
		t.Fatalf("first instance: %v", err)
	}
	d1.Record("analyze system logs", []string{"log-analyst"})
	d1.Record("write integration tests", []string{"tester", "qa"})

	// 模拟 daemon 重启：同路径新建实例
	d2, err := NewDiffMemoryWithPersistence(128, path)
	if err != nil {
		t.Fatalf("second instance (simulated restart): %v", err)
	}
	if skills, ok := d2.Lookup("analyze system logs"); !ok || len(skills) != 1 || skills[0] != "log-analyst" {
		t.Fatalf("path 1 did not survive restart: skills=%v ok=%v", skills, ok)
	}
	if skills, ok := d2.Lookup("write integration tests"); !ok || len(skills) != 2 || skills[0] != "tester" {
		t.Fatalf("path 2 did not survive restart: skills=%v ok=%v", skills, ok)
	}
}

// AC2（缺文件不报错）：路径不存在时构造成功、空 store、可继续 Record。
func TestATDD_51_1_AC2_MissingFileNoError(t *testing.T) {
	path := tempDiffMemoryFile(t) // 父目录与文件均不存在

	d, err := NewDiffMemoryWithPersistence(128, path)
	if err != nil {
		t.Fatalf("missing file must not error, got: %v", err)
	}
	if _, ok := d.Lookup("anything"); ok {
		t.Fatalf("expected empty store on missing file")
	}
	// 仍可正常写入
	d.Record("new path", []string{"s"})
	if _, ok := d.Lookup("new path"); !ok {
		t.Fatalf("record after empty construct failed")
	}
}

// AC2（last-wins）：同 intent 先后 record 两组不同 skills → reload 返回最新组。
func TestATDD_51_1_AC2_LastWinsOnReload(t *testing.T) {
	path := tempDiffMemoryFile(t)

	d1, err := NewDiffMemoryWithPersistence(128, path)
	if err != nil {
		t.Fatalf("first instance: %v", err)
	}
	d1.Record("optimize query", []string{"old-skill"})
	d1.Record("optimize query", []string{"new-skill-a", "new-skill-b"})

	d2, err := NewDiffMemoryWithPersistence(128, path)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	skills, ok := d2.Lookup("optimize query")
	if !ok {
		t.Fatalf("expected hit after reload")
	}
	if len(skills) != 2 || skills[0] != "new-skill-a" || skills[1] != "new-skill-b" {
		t.Fatalf("last-wins violated: expected latest skills, got %v", skills)
	}
}

// --------------------------------------------------------------------------
// AC4（HitCount 存活）：行为级证明——replay 必须恢复 hit_count，否则 eviction 误删高频项。
// 构造：keepme(hit=100, 最旧) + f1(hit=1) + f2(hit=1)，maxSize=2，加载触发一次驱逐。
//   - 若 hit_count 被恢复：驱逐最低 hit（f1/f2 之一），keepme 存活。
//   - 若 hit_count 被重置为 1：三者同 hit，按最旧 ts 驱逐 → keepme（最旧）被删。
// 断言 keepme 存活，即证明 hit_count 在 replay 时被恢复并参与 eviction 排序。
// --------------------------------------------------------------------------

func TestATDD_51_1_AC4_HitCountRestoredAndUsedInEviction(t *testing.T) {
	path := tempDiffMemoryFile(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeDiffMemoryFixture(t, path, []DiffMemoryEntry{
		{Intent: "keepme", Skills: []string{"hot"}, HitCount: 100, Timestamp: base},                   // 最旧但最高频
		{Intent: "f1", Skills: []string{"cold1"}, HitCount: 1, Timestamp: base.Add(10 * time.Minute)},  // 低频
		{Intent: "f2", Skills: []string{"cold2"}, HitCount: 1, Timestamp: base.Add(20 * time.Minute)},  // 低频
	})

	d, err := NewDiffMemoryWithPersistence(2, path) // maxSize=2 → 加载 3 条须驱逐 1 条
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if _, ok := d.Lookup("keepme"); !ok {
		t.Fatalf("high-hit-count entry was evicted on load — hit_count not restored before eviction (AC4 violated)")
	}
}

// --------------------------------------------------------------------------
// AC5（load 尊重 maxSize）：文件 distinct intent 数 > maxSize → 加载后驱逐至 maxSize，
// 保留高 hit_count / 新 timestamp 者，逐出最低 hit + 最旧 timestamp 者。
// --------------------------------------------------------------------------

func TestATDD_51_1_AC5_LoadTrimsToMaxSize(t *testing.T) {
	path := tempDiffMemoryFile(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(name string, hits int, off time.Duration) DiffMemoryEntry {
		return DiffMemoryEntry{Intent: name, Skills: []string{name + "-skill"}, HitCount: hits, Timestamp: base.Add(off)}
	}
	// 5 条 → maxSize=3：应逐出 hit 最低的 alpha、bravo（均 0；alpha 更旧先逐，bravo 次之）。
	writeDiffMemoryFixture(t, path, []DiffMemoryEntry{
		mk("alpha", 0, 0),
		mk("bravo", 0, 10*time.Minute),
		mk("charlie", 5, 5*time.Minute),
		mk("delta", 3, 7*time.Minute),
		mk("echo", 10, 9*time.Minute),
	})

	d, err := NewDiffMemoryWithPersistence(3, path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, kept := range []string{"charlie", "delta", "echo"} {
		if _, ok := d.Lookup(kept); !ok {
			t.Errorf("expected %q to survive maxSize trim, but missing", kept)
		}
	}
	for _, evicted := range []string{"alpha", "bravo"} {
		if _, ok := d.Lookup(evicted); ok {
			t.Errorf("expected %q to be evicted by maxSize trim, but present", evicted)
		}
	}
}

// --------------------------------------------------------------------------
// AC6（向后兼容）：NewDiffMemory(maxSize) 纯内存，不创建任何文件。
// --------------------------------------------------------------------------

func TestATDD_51_1_AC6_PureMemoryWritesNoFile(t *testing.T) {
	tmp := t.TempDir()
	probe := filepath.Join(tmp, ".rnix", "data", "diffmemory", "diffmemory.json")

	d := NewDiffMemory(128) // 纯内存构造
	d.Record("in memory only", []string{"mem"})

	if _, err := os.Stat(probe); !os.IsNotExist(err) {
		t.Fatalf("pure-memory DiffMemory must not create persistence file, stat err=%v", err)
	}
	if skills, ok := d.Lookup("in memory only"); !ok || skills[0] != "mem" {
		t.Fatalf("in-memory lookup failed: skills=%v ok=%v", skills, ok)
	}
}

// --------------------------------------------------------------------------
// AC8（启动鲁棒性）：空行与损坏 JSON 行被跳过，合法条目正常加载，构造不报错、不 panic。
// --------------------------------------------------------------------------

func TestATDD_51_1_AC8_CorruptAndEmptyLinesSkipped(t *testing.T) {
	path := tempDiffMemoryFile(t)
	ts := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	writeDiffMemoryFixture(t, path,
		[]DiffMemoryEntry{
			{Intent: "first valid", Skills: []string{"s1"}, HitCount: 1, Timestamp: ts},
			{Intent: "second valid", Skills: []string{"s2"}, HitCount: 1, Timestamp: ts},
		},
		"",                      // 空行
		"this-is-not-json{{{",   // 损坏行
		"   ",                   // 仅空白
	)

	var d *DiffMemory
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("load must not panic on corrupt/empty lines, panic: %v", r)
			}
		}()
		d, err = NewDiffMemoryWithPersistence(128, path)
	}()
	if err != nil {
		t.Fatalf("corrupt line must not fail load (AC8), got error: %v", err)
	}

	if _, ok := d.Lookup("first valid"); !ok {
		t.Errorf("valid entry before corrupt line not loaded")
	}
	if _, ok := d.Lookup("second valid"); !ok {
		t.Errorf("valid entry not loaded despite corrupt line present")
	}
}

// --------------------------------------------------------------------------
// AC9（并发安全）：带持久化的并发 Record 在 -race 下无竞态；落盘不丢条目。
// 运行：go test -race -run TestATDD_51_1_AC9 ./kernel/
// --------------------------------------------------------------------------

func TestATDD_51_1_AC9_ConcurrentRecordWithPersistence(t *testing.T) {
	path := tempDiffMemoryFile(t)
	d, err := NewDiffMemoryWithPersistence(256, path)
	if err != nil {
		t.Fatalf("NewDiffMemoryWithPersistence: %v", err)
	}

	const n = 32
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			intent := fmt.Sprintf("concurrent intent %d", k)
			d.Record(intent, []string{"skill"})
			d.Lookup(intent)
		}(i)
	}
	wg.Wait()

	// 每条首次 Record 至少追加一行 → 文件不少于 n 行。
	entries := readDiffMemoryEntries(t, path)
	if len(entries) < n {
		t.Fatalf("expected at least %d persisted lines, got %d", n, len(entries))
	}

	// reload 后全部命中，证明并发落盘未丢条目。
	d2, err := NewDiffMemoryWithPersistence(256, path)
	if err != nil {
		t.Fatalf("reload after concurrent writes: %v", err)
	}
	for i := range n {
		intent := fmt.Sprintf("concurrent intent %d", i)
		if _, ok := d2.Lookup(intent); !ok {
			t.Fatalf("intent %q lost after concurrent persist + reload", intent)
		}
	}
}
