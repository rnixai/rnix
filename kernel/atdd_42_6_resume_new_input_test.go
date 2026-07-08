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
	t.Cleanup(kern.Shutdown)
	_, projBase := TestSetupDataDir(t, kern)
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

// --- 42.6-UNIT-004: checkpoint 路径 NewInput 注入（快照已含 final output，只追加 user turn） ---

func TestATDD_42_6_UNIT_004_CheckpointResume_NewInput_AppendsUserTurnOnly(t *testing.T) {
	kern, ctxMgr, projBase := setupResumeNewInputTest(t)
	uuid := "newinput-ckpt-0000-0000-000000000001"
	writeHistoryFixture(t, projBase, uuid, 3, "final assistant answer of last round")
	writeCheckpointFixtureForNewInput(t, projBase, uuid)

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
	// checkpoint snapshot already carries the assistant turn — assert we did NOT
	// duplicate it from proc-info.json result.
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

// writeCheckpointFixtureForNewInput writes a minimal checkpoint.json whose
// ContextSnapshot already contains the full conversation including the final
// assistant output (checkpoint = complete ctx snapshot semantics).
func writeCheckpointFixtureForNewInput(t *testing.T, projBase, uuid string) {
	t.Helper()
	stepsDir := filepath.Join(projBase, "steps", uuid)

	ctxSnap, _ := json.Marshal(map[string]any{
		"system_prompt": "test",
		"messages": []map[string]any{
			{"role": "user", "content": "user turn 1"},
			{"role": "assistant", "content": "checkpoint assistant snapshot"},
		},
		"max_size": 256,
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
