package kernel

// Story 72.1 — 观测读路径单行无界化。
//
// RED 依据（实测存量数据）：
//   - meetup-30a8ac79/019fbdd5-…/steps.jsonl = 147,023,623 B / 128 行，
//     82 行 >1 MB，最长 1,664,248 B，第 46 行起连续超限。
//   - wicket-f5947827/019f3f64-…/events.jsonl 第 87 行 = 262,363 B > 256 KB。
//
// 每条读路径**两个**用例（AC8 / F6）：
//   - ~1.5 MB：对齐存量实测最大值，判死 1 MB 上限。
//   - 8 MB：唯一能判死「把 scanner.Buffer 抬到 4 MB」这种伪修复的用例。
//     只写 1.5 MB = 真空 PASS。

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

const (
	// lineSize15MB 对齐存量实测最长行 1,664,248 B。
	lineSize15MB = 1500 * 1024
	// lineSize8MB 超过仓内最大既有 scanner buffer（raw_writer 的 4 MB），
	// 是唯一判死「抬上限」方案的量纲。
	lineSize8MB = 8 * 1024 * 1024
)

// writeStepsFixture 写一个 steps.jsonl，其中 stepPayload[i] 指定第 i 行
// messages 字段的填充字节数（0 = 不填充）。返回文件路径。
//
// 用 strings.Repeat 构造，不拷贝 147 MB 真实文件（AC8-2 红线）。
func writeStepsFixture(t *testing.T, payloadBytes []int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "steps.jsonl")

	var sb strings.Builder
	for i, n := range payloadBytes {
		rec := types.StepRecord{
			Step:    i + 1,
			Action:  "tool_call",
			Summary: "fixture step",
		}
		if n > 0 {
			// messages 是 json.RawMessage —— 构造一个合法 JSON 数组，
			// 其单个字符串元素的长度决定整行长度。
			blob, err := json.Marshal([]map[string]string{
				{"role": "user", "content": strings.Repeat("x", n)},
			})
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			rec.Messages = blob
		}
		data, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("marshal record %d: %v", i+1, err)
		}
		sb.Write(data)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// --- ReadAllSteps ---------------------------------------------------------

func TestATDD_72_1_AC1_ReadAllSteps_Line15MB(t *testing.T) {
	// 3 行，中间一行 1.5 MB。修复前 scanner 在第 2 行 ErrTooLong → 0 条记录 + error。
	path := writeStepsFixture(t, []int{0, lineSize15MB, 0})

	records, total, err := ReadAllSteps(path, 0)
	if err != nil {
		t.Fatalf("ReadAllSteps: unexpected error = %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(records) != 3 {
		t.Errorf("len(records) = %d, want 3", len(records))
	}
}

func TestATDD_72_1_AC1_ReadAllSteps_Line8MB(t *testing.T) {
	// 8 MB 行：判死「把 scanner.Buffer 抬到 4 MB」的伪修复。
	path := writeStepsFixture(t, []int{0, lineSize8MB, 0})

	records, total, err := ReadAllSteps(path, 0)
	if err != nil {
		t.Fatalf("ReadAllSteps: unexpected error = %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(records) != 3 {
		t.Errorf("len(records) = %d, want 3", len(records))
	}
	// 超长行的正文必须完整返回，不得被截断（读路径零截断，AC5 只管写侧）。
	if got := len(records[1].Messages); got < lineSize8MB {
		t.Errorf("records[1].Messages len = %d, want >= %d (payload must not be truncated on read)", got, lineSize8MB)
	}
}

// --- ReadStep -------------------------------------------------------------

func TestATDD_72_1_AC1_ReadStep_Line15MB(t *testing.T) {
	// 目标 step 在超长行**之后** —— 修复前 scanner.Err() 使整个读取失败，
	// 于是 get_step_detail 谎报「step 3 not yet recorded」。
	path := writeStepsFixture(t, []int{0, lineSize15MB, 0})

	rec, err := ReadStep(path, 3)
	if err != nil {
		t.Fatalf("ReadStep(3): unexpected error = %v", err)
	}
	if rec.Step != 3 {
		t.Errorf("rec.Step = %d, want 3", rec.Step)
	}
}

func TestATDD_72_1_AC1_ReadStep_Line8MB(t *testing.T) {
	path := writeStepsFixture(t, []int{0, lineSize8MB, 0})

	rec, err := ReadStep(path, 3)
	if err != nil {
		t.Fatalf("ReadStep(3): unexpected error = %v", err)
	}
	if rec.Step != 3 {
		t.Errorf("rec.Step = %d, want 3", rec.Step)
	}
}

// TestATDD_72_1_AC3_ReadStep_NotFoundSentinel 守 AC3 的哨兵：IPC 侧要靠
// errors.Is 分辨「该 step 不存在」与「读失败」，故 ReadStep 必须返回可辨识
// 的导出哨兵，而不是匿名 fmt.Errorf。
func TestATDD_72_1_AC3_ReadStep_NotFoundSentinel(t *testing.T) {
	path := writeStepsFixture(t, []int{0, 0})

	_, err := ReadStep(path, 99)
	if err == nil {
		t.Fatal("ReadStep(99) on 2-line fixture: want error, got nil")
	}
	if !errors.Is(err, ErrStepNotFound) {
		t.Errorf("ReadStep(99) error = %v, want to match ErrStepNotFound sentinel", err)
	}
}

// --- parseStepsJSONL (resume) --------------------------------------------

func TestATDD_72_1_AC7_ParseStepsJSONL_Line15MB(t *testing.T) {
	// 修复前第 2 行超限 → scanner.Err() → ErrInternal → 该进程永久不可 resume。
	path := writeStepsFixture(t, []int{0, lineSize15MB, 0})
	k := &KernelImpl{}

	lastStep, _, totalSteps, err := k.parseStepsJSONL(path, 0)
	if err != nil {
		t.Fatalf("parseStepsJSONL: unexpected error = %v", err)
	}
	if lastStep != 3 {
		t.Errorf("lastStep = %d, want 3", lastStep)
	}
	if totalSteps != 3 {
		t.Errorf("totalSteps = %d, want 3", totalSteps)
	}
}

func TestATDD_72_1_AC7_ParseStepsJSONL_Line8MB(t *testing.T) {
	path := writeStepsFixture(t, []int{0, lineSize8MB, 0})
	k := &KernelImpl{}

	lastStep, messages, totalSteps, err := k.parseStepsJSONL(path, 0)
	if err != nil {
		t.Fatalf("parseStepsJSONL: unexpected error = %v", err)
	}
	if lastStep != 3 || totalSteps != 3 {
		t.Errorf("lastStep/totalSteps = %d/%d, want 3/3", lastStep, totalSteps)
	}
	// 最后一行无 payload，故 messages 取自它（last-record-wins 语义不变）。
	_ = messages
}

// TestATDD_72_1_AC7_ParseStepsJSONL_MalformedStillHardFails 是护栏：
// resume 的「单行 unmarshal 失败即硬失败」语义是刻意的（用错误上下文重启比
// 不重启更危险），无界化**不得**顺手把它改成「跳过坏行」。
func TestATDD_72_1_AC7_ParseStepsJSONL_MalformedStillHardFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "steps.jsonl")
	if err := os.WriteFile(path, []byte("{\"step\":1}\n{not json}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	k := &KernelImpl{}

	if _, _, _, err := k.parseStepsJSONL(path, 0); err == nil {
		t.Fatal("parseStepsJSONL on malformed line: want hard failure, got nil error")
	}
}

// --- ReadAllEvents --------------------------------------------------------

// writeEventsFixture 写一个 events.jsonl，payloadBytes[i] 指定第 i 行 args
// 里填充字符串的字节数（0 = 不填充）。
func writeEventsFixture(t *testing.T, payloadBytes []int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	var sb strings.Builder
	for i, n := range payloadBytes {
		ev := SyscallEventDisk{
			TimestampMs: float64(i),
			PID:         42,
			Syscall:     "Write",
		}
		if n > 0 {
			ev.Args = map[string]any{"blob": strings.Repeat("y", n)}
		}
		data, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal event %d: %v", i, err)
		}
		sb.Write(data)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// F3：events 的 256 KB 上限**已在存量数据上撞线**
// （wicket-f5947827/019f3f64-… events.jsonl 第 87 行 = 262,363 B），
// 且后果最隐蔽 —— handleListEvents 把错误吞成 OK=true + 空列表。
func TestATDD_72_1_AC1_ReadAllEvents_Line300KB(t *testing.T) {
	const justOverOldLimit = 300 * 1024 // > 256 KB，复现存量撞线
	path := writeEventsFixture(t, []int{0, justOverOldLimit, 0})

	events, err := ReadAllEvents(path)
	if err != nil {
		t.Fatalf("ReadAllEvents: unexpected error = %v", err)
	}
	if len(events) != 3 {
		t.Errorf("len(events) = %d, want 3", len(events))
	}
}

func TestATDD_72_1_AC1_ReadAllEvents_Line8MB(t *testing.T) {
	path := writeEventsFixture(t, []int{0, lineSize8MB, 0})

	events, err := ReadAllEvents(path)
	if err != nil {
		t.Fatalf("ReadAllEvents: unexpected error = %v", err)
	}
	if len(events) != 3 {
		t.Errorf("len(events) = %d, want 3", len(events))
	}
}

// --- ParseErrors 三件套 (AC4) ---------------------------------------------

// 照抄 raw 路径先例：坏行跳过 + 累计计数 + 薄 wrapper 保旧签名。
func TestATDD_72_1_AC4_ReadAllStepsWithErrors_CountsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "steps.jsonl")
	content := "{\"step\":1}\n{not json}\n{\"step\":2}\n{also bad\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	records, total, parseErrors, err := ReadAllStepsWithErrors(path, 0)
	if err != nil {
		t.Fatalf("ReadAllStepsWithErrors: unexpected error = %v", err)
	}
	if parseErrors != 2 {
		t.Errorf("parseErrors = %d, want 2", parseErrors)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2 (malformed lines excluded from total)", total)
	}
	if len(records) != 2 {
		t.Errorf("len(records) = %d, want 2", len(records))
	}
}

// 空行是正常写盘产物（trailing newline），不得计入 parseErrors —— 否则
// 每个文件都会报 1 个假 parse error。照抄 raw_writer.go:187-189。
func TestATDD_72_1_AC4_ReadAllStepsWithErrors_BlankLinesNotCounted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "steps.jsonl")
	content := "{\"step\":1}\n\n{\"step\":2}\n   \n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, _, parseErrors, err := ReadAllStepsWithErrors(path, 0)
	if err != nil {
		t.Fatalf("ReadAllStepsWithErrors: unexpected error = %v", err)
	}
	if parseErrors != 0 {
		t.Errorf("parseErrors = %d, want 0 (blank lines are normal)", parseErrors)
	}
}

func TestATDD_72_1_AC4_ReadAllEventsWithErrors_CountsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	content := "{\"syscall\":\"Write\"}\n{bad}\n{\"syscall\":\"Read\"}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	events, parseErrors, err := ReadAllEventsWithErrors(path)
	if err != nil {
		t.Fatalf("ReadAllEventsWithErrors: unexpected error = %v", err)
	}
	if parseErrors != 1 {
		t.Errorf("parseErrors = %d, want 1", parseErrors)
	}
	if len(events) != 2 {
		t.Errorf("len(events) = %d, want 2", len(events))
	}
}

// --- 最后一行无 trailing newline（AC1 实现要点 1）-------------------------

func TestATDD_72_1_AC1_ReadAllSteps_NoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "steps.jsonl")
	// 末行**不带** \n —— ReadBytes 在 EOF 时仍返回该段数据，实现必须先处理
	// len(line) > 0 再判 EOF 退出，否则静默丢掉最后一行。
	content := "{\"step\":1}\n{\"step\":2}"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	records, total, err := ReadAllSteps(path, 0)
	if err != nil {
		t.Fatalf("ReadAllSteps: unexpected error = %v", err)
	}
	if total != 2 || len(records) != 2 {
		t.Errorf("total/len = %d/%d, want 2/2 (last line without newline must not be dropped)", total, len(records))
	}
}

// --- WriteStep 写侧截断 (AC5 / AC8-4) ------------------------------------

// writeAndReadBack 通过真实 StepWriter 写一条记录，读回原始行。
func writeAndReadBack(t *testing.T, rec types.StepRecord) string {
	t.Helper()
	dir := t.TempDir()
	sw, err := NewStepWriter(dir, "proc-uuid")
	if err != nil {
		t.Fatalf("NewStepWriter: %v", err)
	}
	if err := sw.WriteStep(rec); err != nil {
		t.Fatalf("WriteStep: %v", err)
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "steps", "proc-uuid", "steps.jsonl"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	return strings.TrimRight(string(data), "\n")
}

func TestATDD_72_1_AC5_WriteStep_TruncatesToolResult(t *testing.T) {
	big := strings.Repeat("r", 200*1024) // > 64 KB
	rec := types.StepRecord{Step: 1, ToolResult: big}

	line := writeAndReadBack(t, rec)

	var got types.StepRecord
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// 截断后必须带标记，且长度 ≤ 64 KB + 标记长度。
	if !strings.Contains(got.ToolResult, "[truncated:") {
		t.Errorf("ToolResult lacks truncation marker: %q...", got.ToolResult[:40])
	}
	if len(got.ToolResult) > maxDriverToolResultBytes+64 {
		t.Errorf("ToolResult len = %d, want <= %d + marker", len(got.ToolResult), maxDriverToolResultBytes)
	}
}

// 🔴 code-review 2026-08-02（Decker 裁决）：input 字段的上限是 4 MB 而非 64 KB，
// 两个理由都是承重的：
//   - 结构化 vs 文本：ToolInput 存工具参数的 JSON 串，截断后不再是合法 JSON，
//     agtest_import 的 decodeToolInput 会失败并静默降级为原始串（写盘那刻即
//     不可复原）；ToolResult 是自由文本，截断只丢尾部。
//   - fidelity layer：kernel/observe.go 把 events.jsonl 的 aggregate input 截到
//     64 KB，其依据正是「steps.jsonl remains the fidelity layer for the full
//     input」（Story 65.1 裁决）。两侧同取 64 KB 会在同一位置截断，那句承诺
//     恰在需要它的时刻失效。
//
// 本测试是「不得统一两个量纲」的护栏：把 input 上限改回 64 KB 即转红。
func TestATDD_72_1_AC5_WriteStep_InputBoundIsRawQuantumNot64KB(t *testing.T) {
	// 200 KB：远超 64 KB，但远低于 4 MB —— 必须逐字节保留。
	input := `{"cmd":"` + strings.Repeat("a", 200*1024) + `"}`
	rec := types.StepRecord{
		Step:      1,
		ToolInput: input,
		ToolCalls: []types.ToolCallRecord{{Name: "Bash", Input: input}},
	}

	line := writeAndReadBack(t, rec)

	var got types.StepRecord
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ToolInput != input {
		t.Errorf("ToolInput was truncated at %d bytes (len %d → %d); the input bound must be the 4 MB raw quantum, not 64 KB",
			maxDriverToolResultBytes, len(input), len(got.ToolInput))
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Input != input {
		t.Errorf("ToolCalls[0].Input was truncated (len %d → %d); same 4 MB bound applies",
			len(input), len(got.ToolCalls[0].Input))
	}
	// 存量实测最大 ToolInput 仅 13,093 B，故 4 MB 是纯防御性上界：
	// 200 KB 的输入在正确实现下必须完整落盘，且仍是合法 JSON。
	var probe map[string]string
	if err := json.Unmarshal([]byte(got.ToolInput), &probe); err != nil {
		t.Errorf("persisted ToolInput is no longer valid JSON: %v", err)
	}
}

// 4 MB 上界本身仍然生效（纯防御，非无界）。
func TestATDD_72_1_AC5_WriteStep_InputStillBoundedAt4MB(t *testing.T) {
	huge := strings.Repeat("b", 5*1024*1024) // > 4 MB
	rec := types.StepRecord{Step: 1, ToolInput: huge}

	line := writeAndReadBack(t, rec)

	var got types.StepRecord
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(got.ToolInput, "[truncated:") {
		t.Error("ToolInput above 4 MB lacks truncation marker — the defensive bound is gone")
	}
	if len(got.ToolInput) > int(defaultRawCaptureMaxOutputBytes)+64 {
		t.Errorf("ToolInput len = %d, want <= 4 MB + marker", len(got.ToolInput))
	}
}

func TestATDD_72_1_AC5_WriteStep_TruncatesToolCallResult(t *testing.T) {
	rec := types.StepRecord{
		Step: 1,
		ToolCalls: []types.ToolCallRecord{
			{Name: "Bash", Result: strings.Repeat("z", 200*1024)},
		},
	}

	line := writeAndReadBack(t, rec)

	var got types.StepRecord
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(got.ToolCalls))
	}
	if !strings.Contains(got.ToolCalls[0].Result, "[truncated:") {
		t.Errorf("ToolCalls[0].Result lacks truncation marker")
	}
	if len(got.ToolCalls[0].Result) > maxDriverToolResultBytes+64 {
		t.Errorf("ToolCalls[0].Result len = %d, want <= 64KB + marker", len(got.ToolCalls[0].Result))
	}
}

// Messages 是 resume 数据源，AC5 明文排除 —— 这条是排除项的护栏。
func TestATDD_72_1_AC5_WriteStep_MessagesUntouched(t *testing.T) {
	blob, _ := json.Marshal([]map[string]string{
		{"role": "user", "content": strings.Repeat("m", 200*1024)},
	})
	rec := types.StepRecord{Step: 1, Messages: blob}

	line := writeAndReadBack(t, rec)

	var got types.StepRecord
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(got.Messages) != string(blob) {
		t.Errorf("Messages was mutated by WriteStep (must be byte-identical)")
	}
}

// 幂等：已截断过的串二次进入不产生双重标记。
//
// ⚠️ 本测试直接调 truncateToBytes，不经 WriteStep——故名为 TruncateToBytes_*。
// 原名 WriteStep_Idempotent 名不副实（WriteStep 从未被调用），会让人误以为
// 写盘路径的幂等性已被覆盖。WriteStep 层的幂等由紧随其后的
// TestATDD_72_1_AC5_WriteStep_IdempotentAcrossWrites 用 writeAndReadBack 验。
func TestATDD_72_1_AC5_TruncateToBytes_Idempotent(t *testing.T) {
	once := truncateToBytes(strings.Repeat("a", 200*1024), maxDriverToolResultBytes)
	twice := truncateToBytes(once, maxDriverToolResultBytes)
	if once != twice {
		t.Errorf("truncateToBytes is not idempotent:\n once=%q\n twice=%q", once[len(once)-40:], twice[len(twice)-40:])
	}
	if strings.Count(twice, "[truncated:") != 1 {
		t.Errorf("double truncation marker detected")
	}
}

// WriteStep 层的真幂等：把第一次写盘后已带截断标记的正文再喂回 WriteStep，
// 落盘结果必须与第一次逐字节相同（标记不叠加、长度不再缩水）。
//
// 这条覆盖的是 truncateToBytes 单元测试到不了的组合面——WriteStep 内部对
// ToolResult / ToolInput / ToolCalls[] 各字段分别调截断，任一字段漏掉「已截断
// 则跳过」判断都会在这里显形（真实触发场景：resume 重放已落盘的 StepRecord）。
func TestATDD_72_1_AC5_WriteStep_IdempotentAcrossWrites(t *testing.T) {
	big := strings.Repeat("r", 200*1024) // > 64 KB，必被截断

	first := writeAndReadBack(t, types.StepRecord{Step: 1, ToolResult: big})

	var firstRec types.StepRecord
	if err := json.Unmarshal([]byte(first), &firstRec); err != nil {
		t.Fatalf("unmarshal first write: %v", err)
	}

	// 已截断的正文二次入写盘路径。
	second := writeAndReadBack(t, types.StepRecord{Step: 1, ToolResult: firstRec.ToolResult})

	if first != second {
		t.Errorf("WriteStep is not idempotent across writes:\n first len=%d\n second len=%d",
			len(first), len(second))
	}
	if n := strings.Count(second, "[truncated:"); n != 1 {
		t.Errorf("expected exactly 1 truncation marker after re-write, got %d", n)
	}
}

// 🔴 不得回写调用方的切片元素：WriteStep 收值拷贝，但 ToolCalls 共享 backing
// array。截断必须发生在 clone 上，否则污染调用方（context never-mutated 红线）。
func TestATDD_72_1_AC5_WriteStep_DoesNotMutateCallerSlice(t *testing.T) {
	callerResult := strings.Repeat("c", 200*1024)
	rec := types.StepRecord{
		Step:      1,
		ToolCalls: []types.ToolCallRecord{{Name: "Bash", Result: callerResult}},
	}

	_ = writeAndReadBack(t, rec)

	// 调用方持有的原始切片元素必须逐字节不变。
	if rec.ToolCalls[0].Result != callerResult {
		t.Errorf("WriteStep mutated caller's ToolCalls[0].Result (len %d → %d)",
			len(callerResult), len(rec.ToolCalls[0].Result))
	}
}
