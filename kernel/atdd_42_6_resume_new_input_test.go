package kernel

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 42.6: Resume NewInput 携带 + history 路径防重放补全
// （apex 10-11 B 路续接需求，见 _bmad-output/implementation-artifacts/
//  requirement-brief-resume-new-input-2026-07-08.md）
// =============================================================================

// captureLLMFile records every prompt written to the LLM device so tests can
// assert the message tail that the resumed process actually sends.
type captureLLMFile struct {
	writes [][]byte
}

func (f *captureLLMFile) Write(_ gocontext.Context, data []byte) error {
	cp := make([]byte, len(data))
	copy(cp, data)
	f.writes = append(f.writes, cp)
	return nil
}
func (f *captureLLMFile) Read(_ int) ([]byte, error) {
	return []byte(`{"action":"complete","summary":"done","content":"done"}`), nil
}
func (f *captureLLMFile) Close() error                { return nil }
func (f *captureLLMFile) Stat() (vfs.FileStat, error) { return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil }
func (f *captureLLMFile) SupportsToolCalling() bool   { return true }

func setupResumeNewInputTest(t *testing.T) (*KernelImpl, *rnixctx.Manager, string) {
	t.Helper()
	llmFile := &captureLLMFile{}
	devReg := vfs.NewDeviceRegistry()
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()
	kern := NewKernel(vfsInst, ctxMgr, nil)
	_, projBase := TestSetupDataDir(t, kern) // 注册 TempDir 清理（最先注册 → 最后执行）
	t.Cleanup(kern.Shutdown)                 // 后注册 → 先执行：drain 进程 goroutine 后才删 TempDir
	return kern, ctxMgr, projBase
}

// writeHistoryFixture writes steps.jsonl + process-meta.json + proc-info.json
// for a Dead history-resume source. result is proc-info.json's final output
// (empty = omitted, mirroring processes that died without a result).
func writeHistoryFixture(t *testing.T, projBase, uuid string, lastStep int, result string) {
	t.Helper()
	stepsDir := filepath.Join(projBase, "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var stepsContent []byte
	for i := 1; i <= lastStep; i++ {
		record := map[string]any{
			"step":   i,
			"action": "tool_call",
			"messages": []map[string]string{
				{"role": "user", "content": fmt.Sprintf("user turn %d", i)},
			},
		}
		line, _ := json.Marshal(record)
		stepsContent = append(stepsContent, line...)
		stepsContent = append(stepsContent, '\n')
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "steps.jsonl"), stepsContent, 0o600); err != nil {
		t.Fatalf("write steps.jsonl: %v", err)
	}
	meta, _ := json.Marshal(map[string]any{"system_prompt": "test", "tool_defs": []any{}})
	if err := os.WriteFile(filepath.Join(stepsDir, "process-meta.json"), meta, 0o600); err != nil {
		t.Fatalf("write process-meta.json: %v", err)
	}
	info := map[string]any{
		"pid": 99, "uuid": uuid, "state": "dead", "intent": "new-input test",
		"provider": "claude", "model": "claude-4", "tokens_used": 1000,
	}
	if result != "" {
		info["result"] = result
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(stepsDir, "proc-info.json"), data, 0o600); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
}

// ctxMessages snapshots the resumed process's context messages via Serialize.
func ctxMessages(t *testing.T, kern *KernelImpl, ctxMgr *rnixctx.Manager, uuid string) []rnixctx.Message {
	t.Helper()
	proc, ok := kern.GetProcessByUUID(uuid)
	if !ok {
		t.Fatalf("resumed process %s not in procTable", uuid)
	}
	ctx, err := ctxMgr.GetContext(proc.CtxID)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	snap, err := ctx.Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	var decoded struct {
		Messages []rnixctx.Message `json:"messages"`
	}
	if err := json.Unmarshal(snap, &decoded); err != nil {
		t.Fatalf("decode ctx snapshot: %v", err)
	}
	return decoded.Messages
}

// --- 42.6-UNIT-001: history 路径 NewInput 注入 + final assistant output 防重放补全 ---

func TestATDD_42_6_UNIT_001_HistoryResume_NewInput_AppendsFinalOutputThenUserTurn(t *testing.T) {
	kern, ctxMgr, projBase := setupResumeNewInputTest(t)
	uuid := "newinput-hist-0000-0000-000000000001"
	writeHistoryFixture(t, projBase, uuid, 3, "final assistant answer of last round")

	result, err := kern.ResumeWithOpts(uuid, ResumeOpts{Fork: true, NewInput: "next user turn"})
	if err != nil {
		t.Fatalf("ResumeWithOpts: %v", err)
	}

	msgs := ctxMessages(t, kern, ctxMgr, result.UUID)
	if len(msgs) < 3 {
		t.Fatalf("expected >= 3 messages (history + assistant + user), got %d: %#v", len(msgs), msgs)
	}
	last := msgs[len(msgs)-1]
	prev := msgs[len(msgs)-2]
	if last.Role != rnixctx.RoleUser || last.Content != "next user turn" {
		t.Fatalf("tail message = {%s %q}, want user turn with NewInput", last.Role, last.Content)
	}
	if prev.Role != rnixctx.RoleAssistant || !strings.Contains(prev.Content, "final assistant answer of last round") {
		t.Fatalf("second-to-last = {%s %q}, want final assistant output restored before NewInput (anti-replay)", prev.Role, prev.Content)
	}
}

// --- 42.6-UNIT-002: history 路径 NewInput 空值零行为变化 ---

func TestATDD_42_6_UNIT_002_HistoryResume_EmptyNewInput_NoBehaviorChange(t *testing.T) {
	kern, ctxMgr, projBase := setupResumeNewInputTest(t)
	uuid := "newinput-hist-0000-0000-000000000002"
	writeHistoryFixture(t, projBase, uuid, 3, "final assistant answer of last round")

	result, err := kern.ResumeWithOpts(uuid, ResumeOpts{Fork: true})
	if err != nil {
		t.Fatalf("ResumeWithOpts: %v", err)
	}

	msgs := ctxMessages(t, kern, ctxMgr, result.UUID)
	for _, m := range msgs {
		if m.Role == rnixctx.RoleAssistant && strings.Contains(m.Content, "final assistant answer of last round") {
			t.Fatalf("empty NewInput must not append final assistant output (pure resume semantics unchanged): %#v", msgs)
		}
		if m.Content == "next user turn" {
			t.Fatalf("empty NewInput must not append any user turn: %#v", msgs)
		}
	}
}

// --- 42.6-UNIT-003: history 路径 result 缺失时只追加 NewInput（不失败） ---

func TestATDD_42_6_UNIT_003_HistoryResume_NewInput_MissingResultStillAppendsUserTurn(t *testing.T) {
	kern, ctxMgr, projBase := setupResumeNewInputTest(t)
	uuid := "newinput-hist-0000-0000-000000000003"
	writeHistoryFixture(t, projBase, uuid, 2, "")

	result, err := kern.ResumeWithOpts(uuid, ResumeOpts{Fork: true, NewInput: "next user turn"})
	if err != nil {
		t.Fatalf("ResumeWithOpts with missing result: %v", err)
	}

	msgs := ctxMessages(t, kern, ctxMgr, result.UUID)
	last := msgs[len(msgs)-1]
	if last.Role != rnixctx.RoleUser || last.Content != "next user turn" {
		t.Fatalf("tail message = {%s %q}, want NewInput user turn", last.Role, last.Content)
	}
}

// --- 42.6-UNIT-004: checkpoint 快照尾已含 final output（与 result 一致）→ 只追加 user turn、不重复 ---

func TestATDD_42_6_UNIT_004_CheckpointResume_SnapshotHasFinalOutput_AppendsUserTurnOnly(t *testing.T) {
	kern, ctxMgr, projBase := setupResumeNewInputTest(t)
	uuid := "newinput-ckpt-0000-0000-000000000001"
	// proc-info result == 快照尾 assistant content：防重放补全必须识别为「已在」并跳过。
	writeHistoryFixture(t, projBase, uuid, 3, "checkpoint assistant snapshot")
	writeCheckpointFixtureForNewInput(t, projBase, uuid, []map[string]any{
		{"role": "user", "content": "user turn 1"},
		{"role": "assistant", "content": "checkpoint assistant snapshot"},
	})

	result, err := kern.ResumeWithOpts(uuid, ResumeOpts{Fork: true, NewInput: "checkpoint next turn"})
	if err != nil {
		t.Fatalf("ResumeWithOpts(checkpoint): %v", err)
	}
	if result.ResumedFromStep != 3 {
		t.Fatalf("ResumedFromStep = %d, want 3 (checkpoint LastStep+1) — checkpoint path not taken", result.ResumedFromStep)
	}

	msgs := ctxMessages(t, kern, ctxMgr, result.UUID)
	last := msgs[len(msgs)-1]
	if last.Role != rnixctx.RoleUser || last.Content != "checkpoint next turn" {
		t.Fatalf("tail message = {%s %q}, want NewInput user turn on checkpoint path", last.Role, last.Content)
	}
	count := 0
	for _, m := range msgs {
		if m.Role == rnixctx.RoleAssistant && strings.Contains(m.Content, "checkpoint assistant snapshot") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("checkpoint assistant turn duplicated or lost: count=%d msgs=%#v", count, msgs)
	}
}

// --- 42.6-UNIT-005: checkpoint 快照尾停在输入侧（真实 daemon 形态）→ 补 final output 防重放 ---
//
// 真实 checkpoint 写于 step 的输入侧（10-11 e2e 实测：R3 checkpoint last_step
// 尾部是 user+tool，缺该轮最终 assistant 输出），resume 时 LLM 会认为上一轮没
// 发生而重放/从零重来——checkpoint 路径必须与 history 路径一样补全。
func TestATDD_42_6_UNIT_005_CheckpointResume_SnapshotEndsAtInputSide_RestoresFinalOutput(t *testing.T) {
	kern, ctxMgr, projBase := setupResumeNewInputTest(t)
	uuid := "newinput-ckpt-0000-0000-000000000002"
	writeHistoryFixture(t, projBase, uuid, 3, "final assistant answer of last round")
	writeCheckpointFixtureForNewInput(t, projBase, uuid, []map[string]any{
		{"role": "user", "content": "user turn 1"},
		{"role": "assistant", "content": "mid-step assistant tool call"},
		{"role": "user", "content": "上一轮的输入（快照停在输入侧）"},
	})

	result, err := kern.ResumeWithOpts(uuid, ResumeOpts{Fork: true, NewInput: "checkpoint next turn"})
	if err != nil {
		t.Fatalf("ResumeWithOpts(checkpoint): %v", err)
	}

	msgs := ctxMessages(t, kern, ctxMgr, result.UUID)
	if len(msgs) < 2 {
		t.Fatalf("expected >= 2 messages, got %#v", msgs)
	}
	last := msgs[len(msgs)-1]
	prev := msgs[len(msgs)-2]
	if last.Role != rnixctx.RoleUser || last.Content != "checkpoint next turn" {
		t.Fatalf("tail message = {%s %q}, want NewInput user turn", last.Role, last.Content)
	}
	if prev.Role != rnixctx.RoleAssistant || !strings.Contains(prev.Content, "final assistant answer of last round") {
		t.Fatalf("second-to-last = {%s %q}, want final assistant output restored before NewInput (checkpoint anti-replay)", prev.Role, prev.Content)
	}
}

// writeCheckpointFixtureForNewInput writes a minimal checkpoint.json whose
// ContextSnapshot carries the given messages.
func writeCheckpointFixtureForNewInput(t *testing.T, projBase, uuid string, messages []map[string]any) {
	t.Helper()
	stepsDir := filepath.Join(projBase, "steps", uuid)

	ctxSnap, _ := json.Marshal(map[string]any{
		"system_prompt": "test",
		"messages":      messages,
		"max_size":      256,
	})
	cp := map[string]any{
		"version":   CheckpointVersion,
		"uuid":      uuid,
		"last_step": 2,
		"proc_state": map[string]any{
			"intent":   "new-input checkpoint test",
			"provider": "claude",
			"model":    "claude-4",
		},
		"context_snapshot": json.RawMessage(ctxSnap),
	}
	data, _ := json.Marshal(cp)
	if err := os.WriteFile(filepath.Join(stepsDir, "checkpoint.json"), data, 0o600); err != nil {
		t.Fatalf("write checkpoint.json: %v", err)
	}
}
