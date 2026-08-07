package kernel

import (
	"fmt"
	"strings"
	"testing"
	"time"

	gocontext "context"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Story 73.4 — 限流在进程账面与 wire 上可见（FR8 + NFR3 通道②收口）
//
// 覆盖：
//   - AC1  types.ErrCode 新增 RATE_LIMIT（存在性 + 与 13 个既有码不重复）
//   - AC2  llmErrCode 表驱动映射（6 行 + nil 兜底）+ 终态事件 code 字段断言
//   - AC4  exit_reason 归因三链路（D5 primary 限流优先 / 66.1 既有语义 /
//          primary+fallback 双限流取 primary）
//   - 回归  atdd_66_1 非限流路径逐字不动（本包既有用例全绿）
//
// 固件 provenance：限流文案全部复用 73.1 实捕的 qwen/Anthropic 样本文案
// （kernelThrottleBody / kernelQuotaBody，见 atdd_73_1_rate_limit_kernel_test.go），
// 不自造。
// ============================================================================

// --- AC1: ErrRateLimit 常量存在 + 与既有码不重复 ---

func TestATDD_73_4_AC1_ErrRateLimit_ExistsAndUnique(t *testing.T) {
	if types.ErrRateLimit != "RATE_LIMIT" {
		t.Errorf("types.ErrRateLimit = %q, want %q", types.ErrRateLimit, "RATE_LIMIT")
	}
	existing := []types.ErrCode{
		types.ErrTimeout, types.ErrNotFound, types.ErrPermission, types.ErrInternal,
		types.ErrDriver, types.ErrInvalid, types.ErrIsDirectory, types.ErrBrokenPipe,
		types.ErrServiceUnavailable, types.ErrAlreadyMounted, types.ErrResourceExhausted,
		types.ErrForceKilled, types.ErrDeviceDisconnected,
	}
	seen := map[types.ErrCode]bool{types.ErrRateLimit: true}
	for _, c := range existing {
		if c == types.ErrRateLimit {
			t.Errorf("ErrRateLimit collides with existing code %q", c)
		}
		if seen[c] {
			t.Errorf("duplicate ErrCode %q among existing codes", c)
		}
		seen[c] = true
	}
}

// --- AC2: llmErrCode 表驱动映射 ---

func TestATDD_73_4_AC2_llmErrCode_Table(t *testing.T) {
	// wrapLikeProduction (atdd_73_1) reproduces the driver→VFS wrapping depth
	// the kernel actually sees; the raw sentinels cover the bare-CLI shapes.
	rateLimitErr := llm.NewLLMError("qwen", 429,
		llm.NewRateLimitErrorWithWait(llm.KindThrottle, kernelThrottleBody, 4*time.Second, time.Time{}, "header"))
	quotaErr := llm.NewLLMError("qwen", 429,
		llm.NewRateLimitError(llm.KindQuota, kernelQuotaBody))

	cases := []struct {
		name string
		err  error
		want types.ErrCode
	}{
		{"rate-limit throttle", rateLimitErr, types.ErrRateLimit},
		{"rate-limit quota", quotaErr, types.ErrRateLimit},
		{"bare rate-limit sentinel", llm.ErrRateLimit, types.ErrRateLimit},
		{"auth", wrapLikeProduction(llm.ErrAuth), types.ErrPermission},
		{"login required", wrapLikeProduction(llm.ErrLoginRequired), types.ErrPermission},
		{"context length", wrapLikeProduction(llm.ErrContextLength), types.ErrResourceExhausted},
		{"model not found", wrapLikeProduction(llm.ErrModelNotFound), types.ErrNotFound},
		{"timeout", wrapLikeProduction(llm.ErrTimeout), types.ErrTimeout},
		{"stream incomplete", wrapLikeProduction(llm.ErrStreamIncomplete), types.ErrTimeout},
		{"bare wrapped error", fmt.Errorf("vfs write: %w", types.NewDriverError("Write", "/dev/llm/mock", fmt.Errorf("boom"), types.ErrDriver)), types.ErrDriver},
		{"nil error", nil, types.ErrDriver},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := llmErrCode(tc.err); got != tc.want {
				t.Errorf("llmErrCode(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// --- 终态事件 code 字段（AC2 消费面断言） ---

// readFailRateLimitLLM: Write 成功、Read 恒返回限流错误 — 单错误路径
// （read-fail 无 primary/fallback 之分）终态事件的 code 判定源。
type readFailRateLimitLLM struct{ readErr error }

func (f *readFailRateLimitLLM) Write(_ gocontext.Context, _ []byte) error { return nil }
func (f *readFailRateLimitLLM) Read(_ int) ([]byte, error)               { return nil, f.readErr }
func (f *readFailRateLimitLLM) Close() error                             { return nil }
func (f *readFailRateLimitLLM) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}
func (f *readFailRateLimitLLM) SupportsToolCalling() bool { return true }

// readErrorCode asserts the terminal action=error ReasonStep event carries the
// wanted code (the LAST such event — the terminal one).
func readErrorCode(t *testing.T, baseDir, uuid string) string {
	t.Helper()
	evs := readDiskEvents(t, baseDir, uuid)
	errEvts := diskEventsWithAction(evs, "ReasonStep", "error")
	if len(errEvts) == 0 {
		t.Fatal("no terminal ReasonStep action=error event found")
	}
	last := errEvts[len(errEvts)-1]
	code, _ := last.Args["code"].(string)
	return code
}

func TestATDD_73_4_AC2_EventCode_ReadFailRateLimit(t *testing.T) {
	readErr := llm.NewLLMError("qwen", 429,
		llm.NewRateLimitErrorWithWait(llm.KindThrottle, kernelThrottleBody, 4*time.Second, time.Time{}, "header"))
	llmFile := &readFailRateLimitLLM{readErr: readErr}
	k, baseDir := newFailureRawKernel(t, llmFile, nil, "claude", "")

	pid, err := k.Spawn("73.4 AC2 read-fail code", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)
	if exit.Code == 0 {
		t.Fatalf("expected non-zero exit, got %+v", exit)
	}
	if got := readErrorCode(t, baseDir, proc.UUID); got != string(types.ErrRateLimit) {
		t.Errorf("terminal event code = %q, want %q (read-fail rate limit)", got, types.ErrRateLimit)
	}
}

func TestATDD_73_4_AC2_EventCode_WriteFailWithFallback(t *testing.T) {
	// primary 限流、fallback 连接错误：fallback_exhausted 事件取 primary 的
	// code（RATE_LIMIT），终态 write-fail 事件取 aggregate 的 code（%w 只包
	// fallback → DRIVER）。
	primaryErr := llm.NewLLMError("qwen", 429,
		llm.NewRateLimitErrorWithWait(llm.KindThrottle, kernelThrottleBody, 4*time.Second, time.Time{}, "header"))
	primary := &flakyRawLLMFile{failures: 999, writeErr: primaryErr}
	fallback := &flakyRawLLMFile{failures: 999, writeErr: fmt.Errorf("connection refused: dial tcp 10.0.0.1:443")}
	k, baseDir := newFailureRawKernel(t, primary, fallback, "primary", "fb")

	agent := fallbackAgentInfo("primary", "sonnet", "haiku", "fb")
	pid, err := k.Spawn("73.4 AC2 write-fail code", agent, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)
	if exit.Code == 0 {
		t.Fatalf("expected non-zero exit, got %+v", exit)
	}

	evs := readDiskEvents(t, baseDir, proc.UUID)
	fbExhausted := diskEventsWithAction(evs, "ReasonStep", "fallback_exhausted")
	if len(fbExhausted) == 0 {
		t.Fatal("no fallback_exhausted events found")
	}
	for i, ev := range fbExhausted {
		if code, _ := ev.Args["code"].(string); code != string(types.ErrRateLimit) {
			t.Errorf("fallback_exhausted[%d] code = %q, want %q (primary's code)", i, code, types.ErrRateLimit)
		}
	}
	if got := readErrorCode(t, baseDir, proc.UUID); got != string(types.ErrDriver) {
		t.Errorf("terminal event code = %q, want %q (aggregate wraps only the fallback connection error)", got, types.ErrDriver)
	}
}

// --- AC4: exit_reason 归因三链路 ---

// 链路 1：primary=限流 + fallback=裸连接错误 → exit_reason 含限流线索
// （D5：primary 限流归因优先于 fallback 明细）。
func TestATDD_73_4_AC4_Chain1_PrimaryRateLimit_FallbackBare(t *testing.T) {
	primaryErr := llm.NewLLMError("qwen", 429,
		llm.NewRateLimitErrorWithWait(llm.KindThrottle, kernelThrottleBody, 4*time.Second, time.Time{}, "header"))
	primary := &flakyRawLLMFile{failures: 999, writeErr: primaryErr}
	fallback := &flakyRawLLMFile{failures: 999, writeErr: fmt.Errorf("connection refused: dial tcp 10.0.0.1:443")}
	k, baseDir := newFailureRawKernel(t, primary, fallback, "primary", "fb")

	agent := fallbackAgentInfo("primary", "sonnet", "haiku", "fb")
	pid, err := k.Spawn("73.4 AC4 chain 1", agent, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)

	if !strings.HasPrefix(exit.Reason, "all providers exhausted: ") {
		t.Fatalf("exit.Reason = %q, want prefix %q", exit.Reason, "all providers exhausted: ")
	}
	if !strings.Contains(exit.Reason, "rate limit") {
		t.Errorf("exit.Reason = %q, want rate-limit clue (primary's attribution must win over the bare fallback error)", exit.Reason)
	}
	if got := readProcInfoExitReason(t, k.ResolveStepBaseDir(proc), proc.UUID); got != exit.Reason {
		t.Errorf("proc-info exit_reason = %q, want %q", got, exit.Reason)
	}
}

// 链路 2：primary=ErrAuth + fallback=限流 → exit_reason 含限流文本
// （66.1 既有语义：detail 取 fbErr；D5 分支不介入——primary 非限流）。
func TestATDD_73_4_AC4_Chain2_PrimaryAuth_FallbackRateLimit(t *testing.T) {
	primary := &flakyRawLLMFile{failures: 999, writeErr: wrapLikeProduction(llm.ErrAuth)}
	fbErr := llm.NewLLMError("fb", 429,
		llm.NewRateLimitErrorWithWait(llm.KindThrottle, kernelThrottleBody, 4*time.Second, time.Time{}, "header"))
	fallback := &flakyRawLLMFile{failures: 999, writeErr: fbErr}
	k, baseDir := newFailureRawKernel(t, primary, fallback, "primary", "fb")

	agent := fallbackAgentInfo("primary", "sonnet", "haiku", "fb")
	pid, err := k.Spawn("73.4 AC4 chain 2", agent, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)

	if !strings.HasPrefix(exit.Reason, "all providers exhausted: ") {
		t.Fatalf("exit.Reason = %q, want prefix %q", exit.Reason, "all providers exhausted: ")
	}
	if !strings.Contains(exit.Reason, "rate limit") {
		t.Errorf("exit.Reason = %q, want fallback's rate-limit detail (66.1 semantics: detail takes fbErr)", exit.Reason)
	}
	// 终态事件 code = aggregate 的 code（%w 包 fallback 限流 → RATE_LIMIT）
	if got := readErrorCode(t, baseDir, proc.UUID); got != string(types.ErrRateLimit) {
		t.Errorf("terminal event code = %q, want %q", got, types.ErrRateLimit)
	}
}

// 链路 3：primary=限流 + fallback=限流 → exit_reason 含 primary 文本
// （D5：primary 限流优先，取 primary 的文案而非 fallback 的）。
func TestATDD_73_4_AC4_Chain3_BothRateLimited(t *testing.T) {
	primaryErr := llm.NewLLMError("qwen", 429,
		llm.NewRateLimitErrorWithWait(llm.KindThrottle, kernelThrottleBody, 4*time.Second, time.Time{}, "header"))
	fbErr := llm.NewLLMError("fb", 429,
		llm.NewRateLimitError(llm.KindQuota, kernelQuotaBody))
	primary := &flakyRawLLMFile{failures: 999, writeErr: primaryErr}
	fallback := &flakyRawLLMFile{failures: 999, writeErr: fbErr}
	k, baseDir := newFailureRawKernel(t, primary, fallback, "primary", "fb")

	agent := fallbackAgentInfo("primary", "sonnet", "haiku", "fb")
	pid, err := k.Spawn("73.4 AC4 chain 3", agent, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)

	if !strings.Contains(exit.Reason, "requests per minute") {
		t.Errorf("exit.Reason = %q, want the PRIMARY's throttle text (D5: primary rate-limit detail wins)", exit.Reason)
	}
	if got := readErrorCode(t, baseDir, proc.UUID); got != string(types.ErrRateLimit) {
		t.Errorf("terminal event code = %q, want %q", got, types.ErrRateLimit)
	}
}
