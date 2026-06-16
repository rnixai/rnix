package llm

// ATDD coverage for Story 56.3 — CLI driver sink 并发隔离 + argv 脱敏全链路
//
// 关键 AC：
//   - AC #2：跨进程共享 driver + 多 LLMFile 并发，capture 各归各，-race 干净
//   - AC #3 / #7：argv 凭据值脱敏（driver 层 RedactArgv 是唯一防线，
//     kernel 二次脱敏只触 headers）；effort 真实值保真

import (
	"strings"
	"sync"
	"testing"
)

// 56-3-INT-001 — 跨 LLMFile 并发不串味（裁决 1 并发铁律 -race 红线）
//
// 模拟「跨进程共享 driver」场景：单个 CodexCliDriver 实例 + N 个独立 LLMFile
// 并发各自 Call 一次，capture 必须落在自己 LLMFile 的 lastRawCapture 字段，
// 内容必须含本 worker 自己的 sentinel（intent 文本），不能含别 worker 的。
func TestATDD_56_3_INT001_CLI_ConcurrentNoCrossTalk(t *testing.T) {
	const N = 16
	// 单 driver 实例 — 跨 worker 共享，模拟生产环境 driver 是 factory 闭包单例
	sharedDriver := NewCodexCliDriver(
		CodexWithCommandBuilder(codexMockCmdBuilder("codex_call_success")),
	)

	var wg sync.WaitGroup
	errCh := make(chan error, N*2)

	for i := range N {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			f := openLLMFile(t, sharedDriver, ModeCall)
			defer f.Close()

			sentinel := "ATDD-INT001-w" + strings.Repeat("x", idx%4) + "-" + itoa(idx)
			req := `{"intent":"` + sentinel + `"}`
			if err := f.Write(t.Context(), []byte(req)); err != nil {
				errCh <- &workerErr{idx: idx, msg: "Write failed: " + err.Error()}
				return
			}
			cap := f.LastRawCapture()
			if cap == nil {
				errCh <- &workerErr{idx: idx, msg: "LastRawCapture() == nil"}
				return
			}
			argv, ok := cap.Request["argv"].([]string)
			if !ok {
				errCh <- &workerErr{idx: idx, msg: "argv not []string"}
				return
			}
			argvJoined := strings.Join(argv, " ")
			if !strings.Contains(argvJoined, sentinel) {
				errCh <- &workerErr{idx: idx, msg: "missing self sentinel in argv: " + argvJoined}
				return
			}
			// cross-talk 检测：不能含别 worker 的 sentinel 模式（同前缀但不同 idx）
			otherIdx := (idx + 7) % N
			if otherIdx == idx {
				otherIdx = (idx + 1) % N
			}
			otherSentinel := "ATDD-INT001-w" + strings.Repeat("x", otherIdx%4) + "-" + itoa(otherIdx)
			if otherSentinel != sentinel && strings.Contains(argvJoined, otherSentinel) {
				errCh <- &workerErr{
					idx: idx,
					msg: "cross-talk: argv contains another worker's sentinel " + otherSentinel,
				}
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Error(err)
		}
	}
}

// 56-3-INT-002 — argv 凭据值脱敏（driver 层主脱敏唯一防线）
//
// 经 CodexWithExtraArgs 注入 `--api-key sk-LEAK_PLAINTEXT_XYZ` 真实凭据，
// 验证落 capture 的 argv 中：
//  1. 不含原始凭据值（明文零落盘红线）
//  2. 凭据位置已替换为 redacted(...) 指纹
//  3. effort 真实值保真（CAP-1 透传审计）
func TestATDD_56_3_INT002_ArgvCredential_Redacted(t *testing.T) {
	t.Parallel()
	const secretValue = "sk-LEAK_PLAINTEXT_XYZ_42"
	d := NewCodexCliDriver(
		CodexWithCommandBuilder(codexMockCmdBuilder("codex_call_success")),
		CodexWithReasoningEffort("high"),
		CodexWithExtraArgs([]string{"--api-key", secretValue}),
	)
	f := openLLMFile(t, d, ModeCall)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"audit"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil")
	}
	argv, ok := cap.Request["argv"].([]string)
	if !ok {
		t.Fatalf("argv not []string: %T", cap.Request["argv"])
	}

	// (a) 明文凭据零落盘
	for _, v := range argv {
		if strings.Contains(v, secretValue) {
			t.Fatalf("argv leaked plaintext credential at %q", v)
		}
	}

	// (b) 凭据位置必须是 redacted(...) 指纹形态（找 --api-key 的下一个元素）
	foundRedacted := false
	for i, tok := range argv {
		if tok == "--api-key" && i+1 < len(argv) {
			if strings.HasPrefix(argv[i+1], "redacted(") {
				foundRedacted = true
			}
			break
		}
	}
	if !foundRedacted {
		t.Errorf("--api-key value not fingerprinted: %v", argv)
	}

	// (c) effort 真实值保真
	if !strings.Contains(strings.Join(argv, " "), "model_reasoning_effort=high") {
		t.Errorf("effort flag truth lost: %v", argv)
	}
}

// 56-3-INT-003 — drivers/llm 不导入 kernel（Constraint #9 / 文件级保险绳）
//
// 56.3 在 raw_capture.go 增 helper、各 CLI driver 加 import，扫描确认没有
// 反向引入 kernel（lint 也兜，但测试更快）。
func TestATDD_56_3_INT003_DriversLLMNoKernelImport(t *testing.T) {
	files := []string{
		"raw_capture.go",
		"claude_cli.go",
		"codex_cli.go",
		"cursor_cli.go",
		"qwen_cli.go",
		"vfsfile.go",
	}
	for _, path := range files {
		src := mustReadFile(t, path)
		if strings.Contains(src, `"github.com/rnixai/rnix/kernel`) {
			t.Errorf("%s: 出现 kernel 反向 import (Constraint：drivers/llm 不导入 kernel)", path)
		}
	}
}

// --- helpers (file-local) ---------------------------------------------------

type workerErr struct {
	idx int
	msg string
}

func (e *workerErr) Error() string {
	return "worker[" + itoa(e.idx) + "]: " + e.msg
}

// itoa 是局部 strconv.Itoa 简版，避免 test 文件多 import。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
