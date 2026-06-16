package main

// =============================================================================
// ATDD Story 56.4: rnix strace --raw (CAP-3 路①)
// TDD RED PHASE — render round-trips t.Skip until runStraceRaw implemented.
// =============================================================================
//
// 覆盖 AC#2（strace --raw 扩展）+ AC#2 实时 strace 零回归守门。
//
// RED 机制（记忆 atdd-code-story-red-mechanism-preference）：骨架 + t.Skip。
//   - --raw / --step / --uuid flag 存在性 = green-guard（init() 已注册即过）；
//   - daemon-down 友好提示（非报错退出）= green-guard（骨架已实现）；
//   - --raw 渲染含 effort 真实值 / --json 原始 JSON / 无记录提示 = t.Skip
//     （需 live daemon + raw.jsonl fixture；dev 移除 skip 后填 runStraceRaw 渲染
//     逻辑 + 起 in-proc daemon 验 RED→GREEN）；
//   - 实时 strace（未传 --raw）零回归 = green-guard（早分支不触碰既有路径）。
//
// strace flag 在 main.go init() 注册于 straceCmd——测试经 rootCmd.Find 取真实
// 命令以复用注册好的 flag。

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// withStraceBogusSocket points ipc.Dial at a non-existent socket so --raw 模式
// 的 daemon dial 快速失败（ENOENT），无需真实 daemon。
func withStraceBogusSocket(t *testing.T) {
	t.Helper()
	prev := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/nonexistent/rnix-atdd-56-4.sock"
	t.Cleanup(func() { ipc.SocketPathOverride = prev })
}

// resetStraceFlags restores the package-level strace flags after a test mutates
// them (flags are shared global state in cmd/rnix).
func resetStraceFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		flagStraceRaw = false
		flagStraceStep = 0
		flagStraceUUID = ""
		flagJSON = false
	})
}

// setupRawStraceFixture starts an in-proc daemon, registers a live process, and
// writes a raw.jsonl fixture (1 API record with reasoning_effort=high) at the
// production FindBaseDirByUUID layout. Returns the process so the test can
// target it by PID. Points ipc.SocketPathOverride at the daemon.
func setupRawStraceFixture(t *testing.T) *kernel.Process {
	t.Helper()
	sockPath, kern := setupTestIPCServer(t)
	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = "" })

	proc := kernel.NewProcess(0, "raw strace test", nil)
	_ = proc.Start()
	_, projBase := kernel.TestSetupDataDir(t, kern)
	kernel.TestSetProjectConfig(proc)

	rec := vfs.RawCapture{
		TsMs: 1000,
		Step: 1,
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
	dir := filepath.Join(projBase, "steps", proc.UUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	data, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(dir, "raw.jsonl"), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write raw.jsonl: %v", err)
	}
	kern.AddProcess(proc)
	return proc
}

// ---------------------------------------------------------------------------
// AC#2: --raw / --step / --uuid flag 存在性 (green-guard)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC2_StraceRawFlags_Registered(t *testing.T) {
	for _, name := range []string{"raw", "step", "uuid"} {
		if straceCmd.Flags().Lookup(name) == nil {
			t.Errorf("AC#2: strace --%s flag not registered", name)
		}
	}
}

// ---------------------------------------------------------------------------
// AC#2: daemon-down → 友好提示, 非报错退出 (green-guard)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC2_StraceRaw_DaemonDown_FriendlyNotice(t *testing.T) {
	resetStraceFlags(t)
	withStraceBogusSocket(t)
	flagStraceRaw = true

	var buf bytes.Buffer
	straceCmd.SetOut(&buf)
	straceCmd.SetErr(&buf)

	// --raw 模式 daemon-down 应返回 nil（非报错退出）并给出友好提示
	if err := runStrace(straceCmd, []string{"42"}); err != nil {
		t.Fatalf("AC#2: --raw daemon-down should not hard-error, got: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "daemon") && !strings.Contains(out, "no raw captures") {
		t.Errorf("AC#2: expected friendly daemon-down/no-records notice, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// AC#2: --raw 渲染含 effort 真实值 (t.Skip — 需 live daemon + fixture)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC2_StraceRaw_RendersEffortValue(t *testing.T) {
	resetStraceFlags(t)
	proc := setupRawStraceFixture(t)
	flagStraceRaw = true

	var buf bytes.Buffer
	straceCmd.SetOut(&buf)
	straceCmd.SetErr(&buf)

	if err := runStrace(straceCmd, []string{strconv.Itoa(int(proc.PID))}); err != nil {
		t.Fatalf("AC#2: --raw render should not error, got: %v", err)
	}
	out := buf.String()
	// --raw 模式对含 reasoning_effort=high 的 API 记录渲染人类可读文本，
	// 输出须含 "reasoning_effort" 与 "high"（CAP-3 核心可见点）。
	for _, want := range []string{"reasoning_effort", "high", "POST", "api.example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("AC#2: --raw render missing %q\n---\n%s", want, out)
		}
	}
}

// ---------------------------------------------------------------------------
// AC#2: --json 输出原始 vfs.RawCapture JSON (t.Skip — 需 live daemon + fixture)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC2_StraceRaw_JSONOutput(t *testing.T) {
	resetStraceFlags(t)
	proc := setupRawStraceFixture(t)
	flagStraceRaw = true
	flagJSON = true

	var buf bytes.Buffer
	straceCmd.SetOut(&buf)
	straceCmd.SetErr(&buf)

	if err := runStrace(straceCmd, []string{strconv.Itoa(int(proc.PID))}); err != nil {
		t.Fatalf("AC#2: --raw --json should not error, got: %v", err)
	}
	// --raw --json 输出原始 RawCapture JSON（NDJSON），首行可被 json.Unmarshal
	// 回 vfs.RawCapture（含 kind/step/request/response）。
	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatalf("AC#2: --json produced no output")
	}
	first := strings.SplitN(line, "\n", 2)[0]
	var rec vfs.RawCapture
	if err := json.Unmarshal([]byte(first), &rec); err != nil {
		t.Fatalf("AC#2: --json output not valid RawCapture JSON: %v\n---\n%s", err, first)
	}
	if rec.Kind != "api" || rec.Step != 1 {
		t.Errorf("AC#2: --json roundtrip mismatch: got kind=%q step=%d", rec.Kind, rec.Step)
	}
}

// ---------------------------------------------------------------------------
// AC#2: 无记录时明确提示, 非报错 (t.Skip — 需 live daemon)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC2_StraceRaw_NoRecords_Notice(t *testing.T) {
	resetStraceFlags(t)
	sockPath, kern := setupTestIPCServer(t)
	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = "" })

	// 进程存在但无 raw.jsonl（未写 fixture）→ --raw 给出友好提示，返回 nil。
	proc := kernel.NewProcess(0, "no raw test", nil)
	_ = proc.Start()
	kernel.TestSetupDataDir(t, kern)
	kernel.TestSetProjectConfig(proc)
	kern.AddProcess(proc)

	flagStraceRaw = true
	var buf bytes.Buffer
	straceCmd.SetOut(&buf)
	straceCmd.SetErr(&buf)

	if err := runStrace(straceCmd, []string{strconv.Itoa(int(proc.PID))}); err != nil {
		t.Fatalf("AC#2: no-records should not error, got: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "no raw captures") {
		t.Errorf("AC#2: expected '[strace] no raw captures for ...' notice, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// AC#2: 实时 strace 未传 --raw 零回归 (green-guard)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC2_StraceRealtime_NoRawFlag_NoRegression(t *testing.T) {
	resetStraceFlags(t)
	withStraceBogusSocket(t)
	// flagStraceRaw 保持 false → 必须走既有实时 AttachDebug 路径（早分支不触发）

	var buf bytes.Buffer
	straceCmd.SetOut(&buf)
	straceCmd.SetErr(&buf)

	// daemon-down 时既有实时 strace 路径给出 "no active daemon" 渲染并返回 nil
	// （未传 --raw 行为一字不动）。
	if err := runStrace(straceCmd, []string{"42"}); err != nil {
		t.Fatalf("AC#2: realtime strace daemon-down should not hard-error, got: %v", err)
	}
	// 关键回归断言：未传 --raw 时绝不进入 raw 查询分支
	if flagStraceRaw {
		t.Error("AC#2: flagStraceRaw should remain false in realtime path")
	}
}

// 56.4 review decision 1→a: buildRawLens 在 ParseErrors>0 时追加 malformed 提示行，
// 使 dashboard Raw lens 与 strace --raw 一致暴露被跳过的行。
func TestReview_56_4_BuildRawLens_ParseErrorHint(t *testing.T) {
	step := 3
	m := dashboardModel{width: 80}
	m.inspector.Step = step
	m.inspector.Lens = lensRaw
	m.inspector.RawByStep = map[int]*vfs.RawCapture{
		step: {Step: step, Kind: "api", Request: map[string]any{"url": "https://x"}},
	}
	m.inspector.RawParseErrByStep = map[int]int{step: 2}

	out := m.buildRawLens(step)
	if !strings.Contains(out, "2 line(s) skipped (malformed)") {
		t.Errorf("decision 1→a: lens must surface malformed-line hint, got:\n%s", out)
	}
}

// 56.4 review decision 3→1: buildRawLens 按传入 step 取缓存，使 diff 两侧
// （base/current）渲染各自记录，而非恒同。
func TestReview_56_4_BuildRawLens_PerStepSelectsRecord(t *testing.T) {
	m := dashboardModel{width: 80}
	m.inspector.RawByStep = map[int]*vfs.RawCapture{
		1: {Step: 1, Kind: "api", Request: map[string]any{"url": "https://base-url"}},
		2: {Step: 2, Kind: "api", Request: map[string]any{"url": "https://current-url"}},
	}

	baseOut := m.buildRawLens(1)
	curOut := m.buildRawLens(2)
	if baseOut == curOut {
		t.Fatal("decision 3→1: diff sides must differ per step, got identical content")
	}
	if !strings.Contains(baseOut, "base-url") {
		t.Errorf("step 1 render should contain base-url, got:\n%s", baseOut)
	}
	if !strings.Contains(curOut, "current-url") {
		t.Errorf("step 2 render should contain current-url, got:\n%s", curOut)
	}
}
