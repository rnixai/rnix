package kernel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rnixai/rnix/internal/types"
)

// ============================================================================
// ATDD Story 66.1 — exit_reason 并入 driver 错误真因（P1b）
//
//   - AC1: LLM Write 失败（无 fallback）→ exit_reason = "llm write failed: <首行>"
//   - AC1: fallback 双失败 → "all providers exhausted: <fallback 错误>"
//   - AC2: daemon 日志一行含 pid= 与真因
//   - AC4: 正常 completed 路径 exit_reason 零改动
//   - AC6: 截断 UTF-8 安全 / 多行取首行 / proc-info.json 同形态
// ============================================================================

const quotaErrText = "API Error: Server is temporarily limiting requests · carpool 5h quota exhausted"

func quotaDriverError() error {
	return types.NewDriverError("Write", "/dev/llm/mock", errors.New(quotaErrText), types.ErrDriver)
}

func readProcInfoExitReason(t *testing.T, stepBaseDir, uuid string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stepBaseDir, "steps", uuid, "proc-info.json"))
	if err != nil {
		t.Fatalf("read proc-info.json: %v", err)
	}
	var info struct {
		ExitReason string `json:"exit_reason"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("unmarshal proc-info.json: %v", err)
	}
	return info.ExitReason
}

// --- AC1 + AC6: write 失败 exit_reason 带真因，proc-info.json 同形态 ---

func TestATDD_66_1_AC1_WriteFailed_ExitReasonCarriesDriverDetail(t *testing.T) {
	llm := &flakyRawLLMFile{failures: 999, writeErr: quotaDriverError()}
	k, baseDir := newFailureRawKernel(t, llm, nil, "claude", "")

	pid, err := k.Spawn("66.1 AC1 quota failure", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)

	if !strings.HasPrefix(exit.Reason, "llm write failed: ") {
		t.Fatalf("exit.Reason = %q, want prefix %q", exit.Reason, "llm write failed: ")
	}
	if !strings.Contains(exit.Reason, "quota") {
		t.Errorf("exit.Reason = %q, want to contain %q", exit.Reason, "quota")
	}
	// 深 unwrap：不得携带 DriverError 的 PID/device 装饰
	if strings.Contains(exit.Reason, "[DRIVER]") || strings.Contains(exit.Reason, "/dev/llm/mock") {
		t.Errorf("exit.Reason = %q, want deep-unwrapped detail without driver decoration", exit.Reason)
	}
	if got := readProcInfoExitReason(t, k.ResolveStepBaseDir(proc), proc.UUID); got != exit.Reason {
		t.Errorf("proc-info exit_reason = %q, want %q", got, exit.Reason)
	}
}

// --- AC1: fallback 双失败 → all providers exhausted: <真因> ---

func TestATDD_66_1_AC1_FallbackAlsoFails_AllProvidersExhaustedDetail(t *testing.T) {
	primary := &flakyRawLLMFile{failures: 999, writeErr: quotaDriverError()}
	fallback := &flakyRawLLMFile{failures: 999, writeErr: fmt.Errorf("fallback auth token expired")}
	k, baseDir := newFailureRawKernel(t, primary, fallback, "primary", "fb")

	agent := fallbackAgentInfo("primary", "sonnet", "haiku", "fb")
	pid, err := k.Spawn("66.1 fallback double failure", agent, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)

	if !strings.HasPrefix(exit.Reason, "all providers exhausted: ") {
		t.Fatalf("exit.Reason = %q, want prefix %q", exit.Reason, "all providers exhausted: ")
	}
	if !strings.Contains(exit.Reason, "fallback auth token expired") {
		t.Errorf("exit.Reason = %q, want to contain fallback error detail", exit.Reason)
	}
}

// --- AC2: daemon 日志可 grep（pid= + 真因） ---

func TestATDD_66_1_AC2_DaemonLogContainsPIDAndDetail(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	llm := &flakyRawLLMFile{failures: 999, writeErr: quotaDriverError()}
	k, baseDir := newFailureRawKernel(t, llm, nil, "claude", "")

	pid, err := k.Spawn("66.1 AC2 daemon log", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	waitDone(t, proc)

	logs := buf.String()
	if !strings.Contains(logs, fmt.Sprintf("pid=%d", pid)) {
		t.Errorf("daemon log missing pid=%d marker:\n%s", pid, logs)
	}
	if !strings.Contains(logs, "quota") {
		t.Errorf("daemon log missing driver detail (quota):\n%s", logs)
	}
}

// --- AC4: 正常 completed 路径零改动 ---

func TestATDD_66_1_AC4_CompletedPathUnchanged(t *testing.T) {
	llm := &flakyRawLLMFile{readData: makeLLMResponse("done", 5)}
	k, baseDir := newFailureRawKernel(t, llm, nil, "claude", "")

	pid, err := k.Spawn("66.1 AC4 normal completion", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)

	if exit.Code != 0 {
		t.Fatalf("expected clean exit, got %+v", exit)
	}
	if strings.Contains(exit.Reason, "llm write failed") || strings.Contains(exit.Reason, ": ") {
		t.Errorf("exit.Reason = %q, want unchanged base literal on success path", exit.Reason)
	}
}

// --- AC6: driverErrorDetail 单测（截断 / 首行 / 深 unwrap / nil） ---

func TestATDD_66_1_AC6_DriverErrorDetail_Unit(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		if got := driverErrorDetail(nil); got != "" {
			t.Errorf("driverErrorDetail(nil) = %q, want empty", got)
		}
	})

	t.Run("multiline keeps first line only", func(t *testing.T) {
		err := errors.New("first line of failure\nsecond line\nthird line")
		got := driverErrorDetail(err)
		if got != "first line of failure" {
			t.Errorf("got %q, want first line only", got)
		}
	})

	t.Run("deep unwrap to DriverError.Err", func(t *testing.T) {
		inner := errors.New("llm [claude]: 429 too many requests")
		wrapped := fmt.Errorf("vfs write: %w", types.NewDriverError("Write", "/dev/llm/claude", inner, types.ErrDriver))
		got := driverErrorDetail(wrapped)
		if got != inner.Error() {
			t.Errorf("got %q, want deep-unwrapped %q", got, inner.Error())
		}
	})

	t.Run("overlong multibyte truncated on UTF-8 boundary", func(t *testing.T) {
		// 每个 '错' 3 字节；200/3 不整除 → 截断点落在多字节字符中间
		long := strings.Repeat("错", 100)
		got := driverErrorDetail(errors.New(long))
		if len(got) > maxExitReasonDetailBytes {
			t.Errorf("len = %d, want <= %d", len(got), maxExitReasonDetailBytes)
		}
		if !utf8.ValidString(got) {
			t.Errorf("truncated detail is not valid UTF-8: %q", got)
		}
		if !strings.HasPrefix(got, "错") {
			t.Errorf("truncated detail lost content: %q", got)
		}
	})
}
