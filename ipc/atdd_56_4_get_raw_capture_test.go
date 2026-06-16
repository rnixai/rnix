package ipc

// =============================================================================
// ATDD Story 56.4: get_raw_capture IPC Method (CAP-3 单一数据后端)
// TDD RED PHASE — behavioral tests t.Skip until implementation lands.
// =============================================================================
//
// 本文件覆盖 CAP-3 的 IPC 后端（AC#1 / #4 / #5 / #8）。三路（strace / dashboard /
// 直接 IPC）共用此唯一 get_raw_capture 方法作数据后端——本文件断言后端契约，
// strace 渲染断言在 cmd/rnix/atdd_56_4_strace_raw_test.go、lens 渲染断言在
// internal/dashboard/inspector/raw_lens_test.go。
//
// RED 机制（记忆 atdd-code-story-red-mechanism-preference）：骨架 + t.Skip。
// 序列化 / 方法常量为 green-guard（类型存在即过，不 skip）；handler round-trip /
// PID→UUID 解析 / malformed 计数为 t.Skip（dev 移除 skip 后填 handler 逻辑验
// RED→GREEN）。
//
// fixture 路径铁律（Story 56.4 Testing Standards + 记忆 rnix-session-data-daemon-version）：
// raw.jsonl 必须落在 production FindBaseDirByUUID 能解析的布局——
// <projBaseDir>/steps/<uuid>/raw.jsonl（用 TestSetupDataDir 拿 projBase）。
//
// Test Level: Integration (IPC server handler 直调 + 读盘 fixture)
// Priority: P0 (CAP-3 唯一后端)

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// fixture helpers
// ---------------------------------------------------------------------------

// writeTestRawJSONL writes the given RawCapture records as NDJSON to
// <projBaseDir>/steps/<uuid>/raw.jsonl, mirroring writeTestStepsUUID's layout
// so FindBaseDirByUUID resolves the fixture exactly like production.
func writeTestRawJSONL(t *testing.T, projBaseDir, procUUID string, records []vfs.RawCapture) {
	t.Helper()
	dir := filepath.Join(projBaseDir, "steps", procUUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeTestRawJSONL mkdir: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, "raw.jsonl"))
	if err != nil {
		t.Fatalf("writeTestRawJSONL create: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			t.Fatalf("writeTestRawJSONL encode: %v", err)
		}
	}
}

// testRawAPIRecord returns a 56.2-shaped API RawCapture for the given step,
// with reasoning_effort embedded in the request body (CAP-1/CAP-3 核心可见点).
func testRawAPIRecord(step int) vfs.RawCapture {
	return vfs.RawCapture{
		TsMs: int64(step) * 1000,
		Step: step,
		Kind: "api",
		Request: map[string]any{
			"method":  "POST",
			"url":     "https://api.example.com/v1/messages",
			"headers": map[string]any{"authorization": "redacted(len=40,prefix=sk-,sha256=abcd)"},
			"body":    `{"model":"claude","reasoning_effort":"high"}`,
		},
		Response: map[string]any{
			"status": float64(200),
			"body":   `{"role":"assistant"}`,
		},
	}
}

// testRawCLIRecord returns a 56.3-shaped CLI RawCapture for the given step,
// with --effort embedded in argv (CAP-1/CAP-3 核心可见点).
func testRawCLIRecord(step int) vfs.RawCapture {
	return vfs.RawCapture{
		TsMs: int64(step) * 1000,
		Step: step,
		Kind: "cli",
		Request: map[string]any{
			"argv":  []any{"claude", "-p", "--effort", "high"},
			"stdin": "user prompt",
			"env":   map[string]any{"ANTHROPIC_API_KEY": "redacted(len=40,prefix=sk-,sha256=abcd)"},
		},
		Response: map[string]any{
			"stdout":    "assistant reply",
			"stderr":    "",
			"exit_code": float64(0),
		},
	}
}

// ---------------------------------------------------------------------------
// AC#1: Protocol — method constant + Request/Response serialization (green-guard)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC1_MethodConstant(t *testing.T) {
	if MethodGetRawCapture != "get_raw_capture" {
		t.Fatalf("AC#1: MethodGetRawCapture = %q, want %q", MethodGetRawCapture, "get_raw_capture")
	}
}

func TestATDD_56_4_AC1_GetRawCaptureRequest_Serialization(t *testing.T) {
	req := GetRawCaptureRequest{PID: types.PID(42), UUID: "abc", Step: 3}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("AC#1: marshal GetRawCaptureRequest: %v", err)
	}
	var decoded GetRawCaptureRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC#1: unmarshal GetRawCaptureRequest: %v", err)
	}
	if decoded.PID != 42 || decoded.UUID != "abc" || decoded.Step != 3 {
		t.Errorf("AC#1: roundtrip mismatch: got %+v", decoded)
	}
	// snake_case + omitempty tags
	if !json.Valid(data) {
		t.Errorf("AC#1: invalid JSON: %s", data)
	}
}

func TestATDD_56_4_AC1_GetRawCaptureResponse_Serialization(t *testing.T) {
	resp := GetRawCaptureResponse{
		Records:     []vfs.RawCapture{testRawAPIRecord(1)},
		ParseErrors: 2,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("AC#1: marshal GetRawCaptureResponse: %v", err)
	}
	var decoded GetRawCaptureResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC#1: unmarshal GetRawCaptureResponse: %v", err)
	}
	if len(decoded.Records) != 1 || decoded.Records[0].Kind != "api" {
		t.Errorf("AC#1: Records roundtrip mismatch: got %+v", decoded.Records)
	}
	if decoded.ParseErrors != 2 {
		t.Errorf("AC#1: ParseErrors = %d, want 2", decoded.ParseErrors)
	}
}

// ---------------------------------------------------------------------------
// AC#1: handler round-trip — Step=0 全部 / Step=N 单条 (t.Skip until impl)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC1_Handler_AllSteps(t *testing.T) {

	srv, sockPath, _ := setupTestServer(t)
	proc := kernel.NewProcess(0, "raw capture test", nil)
	_ = proc.Start()
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)
	writeTestRawJSONL(t, projBase, proc.UUID, []vfs.RawCapture{
		testRawAPIRecord(1),
		testRawCLIRecord(2),
	})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetRawCapture, GetRawCaptureRequest{PID: proc.PID, Step: 0})
	if !resp.OK {
		t.Fatalf("AC#1: request failed: %+v", resp.Error)
	}
	var out GetRawCaptureResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("AC#1: unmarshal: %v", err)
	}
	if len(out.Records) != 2 {
		t.Fatalf("AC#1: Step=0 should return all 2 records, got %d", len(out.Records))
	}
	if out.Records[0].Kind != "api" || out.Records[1].Kind != "cli" {
		t.Errorf("AC#1: record order/kind mismatch: got %q, %q", out.Records[0].Kind, out.Records[1].Kind)
	}
}

func TestATDD_56_4_AC1_Handler_SingleStep(t *testing.T) {

	srv, sockPath, _ := setupTestServer(t)
	proc := kernel.NewProcess(0, "raw capture test", nil)
	_ = proc.Start()
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)
	writeTestRawJSONL(t, projBase, proc.UUID, []vfs.RawCapture{
		testRawAPIRecord(1),
		testRawCLIRecord(2),
	})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetRawCapture, GetRawCaptureRequest{PID: proc.PID, Step: 2})
	if !resp.OK {
		t.Fatalf("AC#1: request failed: %+v", resp.Error)
	}
	var out GetRawCaptureResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("AC#1: unmarshal: %v", err)
	}
	if len(out.Records) != 1 {
		t.Fatalf("AC#1: Step=2 should return 1 record, got %d", len(out.Records))
	}
	if out.Records[0].Step != 2 || out.Records[0].Kind != "cli" {
		t.Errorf("AC#1: wrong record: got step=%d kind=%q", out.Records[0].Step, out.Records[0].Kind)
	}
}

// ---------------------------------------------------------------------------
// AC#1: 不存在的 uuid / 文件 → OK + 空列表 (t.Skip until impl)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC1_Handler_MissingFile_EmptyList(t *testing.T) {

	srv, sockPath, _ := setupTestServer(t)
	kernel.TestSetupDataDir(t, srv.kern)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetRawCapture, GetRawCaptureRequest{
		UUID: "56400000-0000-7000-0000-000000000099", // 不存在
	})
	if !resp.OK {
		t.Fatalf("AC#1: missing uuid should be OK=true (empty list), got error: %+v", resp.Error)
	}
	var out GetRawCaptureResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("AC#1: unmarshal: %v", err)
	}
	if len(out.Records) != 0 {
		t.Errorf("AC#1: missing file should yield empty list, got %d records", len(out.Records))
	}
}

// ---------------------------------------------------------------------------
// AC#1: PID→UUID 解析 — live proc + reaped history 两路 (t.Skip until impl)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC1_Handler_PIDResolution_LiveProc(t *testing.T) {

	srv, sockPath, _ := setupTestServer(t)
	proc := kernel.NewProcess(0, "live proc", nil)
	_ = proc.Start()
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)
	writeTestRawJSONL(t, projBase, proc.UUID, []vfs.RawCapture{testRawAPIRecord(1)})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	// UUID 留空 → handler 须经 GetProcess(PID) 解析 live proc.UUID
	resp := sendRequest(t, conn, MethodGetRawCapture, GetRawCaptureRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("AC#1: live PID resolution failed: %+v", resp.Error)
	}
	var out GetRawCaptureResponse
	_ = json.Unmarshal(resp.Payload, &out)
	if len(out.Records) != 1 {
		t.Errorf("AC#1: live PID→UUID should resolve 1 record, got %d", len(out.Records))
	}
}

func TestATDD_56_4_AC1_Handler_PIDResolution_ReapedHistory(t *testing.T) {

	srv, sockPath, _ := setupTestServer(t)
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	reapedUUID := "56400000-0000-7000-0000-deadbeef0001"
	pid := types.PID(56401)
	procInfo := vfs.ProcInfo{
		PID:   pid,
		UUID:  reapedUUID,
		State: types.StateDead,
	}
	if err := kernel.SaveProcInfo(projBase, procInfo); err != nil {
		t.Fatalf("SaveProcInfo: %v", err)
	}
	writeTestRawJSONL(t, projBase, reapedUUID, []vfs.RawCapture{testRawAPIRecord(1)})
	if err := srv.kern.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}

	conn := dial(t, sockPath)
	// PID 已失活 → handler 须经 FindHistoryByPID(PID) 取历史 UUID 再读盘
	resp := sendRequest(t, conn, MethodGetRawCapture, GetRawCaptureRequest{PID: pid})
	if !resp.OK {
		t.Fatalf("AC#1: reaped PID resolution failed: %+v", resp.Error)
	}
	var out GetRawCaptureResponse
	_ = json.Unmarshal(resp.Payload, &out)
	if len(out.Records) != 1 {
		t.Errorf("AC#1: reaped PID→history UUID should resolve 1 record, got %d", len(out.Records))
	}
}

// ---------------------------------------------------------------------------
// AC#5: 读取为脱敏后内容 — 三路不还原凭据指纹 (t.Skip until impl)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC5_Handler_RedactedFingerprint_NotRestored(t *testing.T) {

	srv, sockPath, _ := setupTestServer(t)
	proc := kernel.NewProcess(0, "redact test", nil)
	_ = proc.Start()
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)
	writeTestRawJSONL(t, projBase, proc.UUID, []vfs.RawCapture{testRawAPIRecord(1)})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetRawCapture, GetRawCaptureRequest{PID: proc.PID, Step: 1})
	var out GetRawCaptureResponse
	_ = json.Unmarshal(resp.Payload, &out)
	if len(out.Records) != 1 {
		t.Fatalf("AC#5: expected 1 record, got %d", len(out.Records))
	}
	headers, _ := out.Records[0].Request["headers"].(map[string]any)
	auth, _ := headers["authorization"].(string)
	// 落盘已是 redacted(...) 指纹，读路径零反脱敏 → 读到即原指纹
	if auth != "redacted(len=40,prefix=sk-,sha256=abcd)" {
		t.Errorf("AC#5: read path must NOT alter the redacted fingerprint, got %q", auth)
	}
}

// ---------------------------------------------------------------------------
// AC#8: malformed JSON 行被计数并暴露 (消化 deferred #17, t.Skip until impl)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC8_Handler_MalformedLines_Counted(t *testing.T) {

	srv, sockPath, _ := setupTestServer(t)
	proc := kernel.NewProcess(0, "malformed test", nil)
	_ = proc.Start()
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	// 手写 raw.jsonl: 1 good line + 1 malformed line + 1 good line
	dir := filepath.Join(projBase, "steps", proc.UUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	good1, _ := json.Marshal(testRawAPIRecord(1))
	good2, _ := json.Marshal(testRawCLIRecord(2))
	content := string(good1) + "\n" + "{ this is not valid json\n" + string(good2) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "raw.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write raw.jsonl: %v", err)
	}
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetRawCapture, GetRawCaptureRequest{PID: proc.PID, Step: 0})
	if !resp.OK {
		t.Fatalf("AC#8: request failed: %+v", resp.Error)
	}
	var out GetRawCaptureResponse
	_ = json.Unmarshal(resp.Payload, &out)
	if len(out.Records) != 2 {
		t.Errorf("AC#8: expected 2 valid records (malformed skipped), got %d", len(out.Records))
	}
	if out.ParseErrors != 1 {
		t.Errorf("AC#8: expected ParseErrors=1 (malformed line counted), got %d", out.ParseErrors)
	}
}

// ---------------------------------------------------------------------------
// AC#4: 三路一致性 — IPC 后端为唯一数据源 (t.Skip until impl)
// ---------------------------------------------------------------------------
//
// 三路（strace --raw / dashboard LensRaw / 直接 IPC）共用 get_raw_capture 作唯一
// 后端 → 对同一 uuid+step 取到同一条 raw.jsonl 记录。本测试断言 IPC 后端返回的
// 记录与磁盘上的源记录逐字段一致（strace/lens 渲染各自的输入即此记录，渲染断言
// 在各自包的测试文件，此处锁定共享后端的收敛点）。
func TestATDD_56_4_AC4_ThreeWay_SharedBackend_Consistency(t *testing.T) {

	srv, sockPath, _ := setupTestServer(t)
	proc := kernel.NewProcess(0, "consistency test", nil)
	_ = proc.Start()
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)
	source := testRawAPIRecord(7)
	writeTestRawJSONL(t, projBase, proc.UUID, []vfs.RawCapture{source})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetRawCapture, GetRawCaptureRequest{PID: proc.PID, Step: 7})
	var out GetRawCaptureResponse
	_ = json.Unmarshal(resp.Payload, &out)
	if len(out.Records) != 1 {
		t.Fatalf("AC#4: expected 1 record, got %d", len(out.Records))
	}
	got := out.Records[0]
	// 后端记录须与磁盘源记录在 step / kind / request.body 上逐字段收敛
	if got.Step != source.Step || got.Kind != source.Kind {
		t.Errorf("AC#4: backend record diverged: got step=%d kind=%q, want step=%d kind=%q",
			got.Step, got.Kind, source.Step, source.Kind)
	}
	gotBody, _ := got.Request["body"].(string)
	wantBody, _ := source.Request["body"].(string)
	if gotBody != wantBody {
		t.Errorf("AC#4: backend request body diverged: got %q, want %q", gotBody, wantBody)
	}
}
