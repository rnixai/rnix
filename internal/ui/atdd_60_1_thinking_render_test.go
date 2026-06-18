package ui

import (
	"bytes"
	"strings"
	"testing"
)

// ATDD Story 60.1 — AC3 (internal/ui 层): 节流与呈现权衡 + 输出模式契约。
//
// AC3 要求前台思考反馈做节流/聚合(不逐 delta 刷屏),并尊重 ui.ModeQuiet
// (不打思考指示) 与 ui.ModeJSON(不混入非结构化思考文本)。具体节流策略与默认
// 呈现形态由 dev 决策(story Dev Notes 权衡 #2/#3),故:
//   - 固定契约(Quiet/JSON 守卫) → green-guard,不 skip,no-op 骨架下即 PASS,
//     dev 加渲染后须保持(GREEN-stays-GREEN,实时拦 Mode 红线)。
//   - 可见性 + 节流(dev 决策面) → RED,t.Skip,no-op 骨架下断言必 FAIL,
//     dev 移 skip 填 AgentThinking 节流 + 渲染验 RED→GREEN。
//
// 唯一生产骨架: internal/ui/progress.go 新增 ProgressReporter.AgentThinking
// 空方法(no-op),使本文件可编译,make all 保持绿。
// 构造范式参考既有 progress_test.go(Renderer{Writer,OutputMode,Profile})。

// ---------------------------------------------------------------------------
// 60.1-UNIT-005 (AC3, green-guard): ModeQuiet 下零思考输出
// ---------------------------------------------------------------------------

func TestATDD_60_1_AC3_ThinkingQuietMode_NoOutput(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeQuiet, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	p.AgentThinking(1, 1, "internal deliberation that must stay silent in quiet mode")

	if buf.Len() != 0 {
		t.Errorf("AC3: ModeQuiet 必须零思考输出,实得 %q", buf.String())
	}
}

// ---------------------------------------------------------------------------
// 60.1-UNIT-006 (AC3, green-guard): ModeJSON 下不混入非结构化思考文本
// ---------------------------------------------------------------------------

func TestATDD_60_1_AC3_ThinkingJSONMode_NoUnstructuredText(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	const sentinel = "RAW_THINKING_SENTINEL_should_not_leak"
	p.AgentThinking(1, 1, sentinel)

	if strings.Contains(buf.String(), sentinel) {
		t.Errorf("AC3: ModeJSON 不得混入非结构化思考文本(如需则作结构化字段),实得 %q", buf.String())
	}
}

// ---------------------------------------------------------------------------
// 60.1-UNIT-007 (AC3, RED): 默认前台(ModeDefault)长思考期间有可见反馈
// ---------------------------------------------------------------------------

func TestATDD_60_1_AC3_ThinkingVisibleInForeground(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	// 模拟一段持续的长思考(多个增量),默认前台不应再"无变化"。
	for range 5 {
		p.AgentThinking(7, 2, "long reasoning chunk in progress ... still working")
	}

	if buf.Len() == 0 {
		t.Fatal("AC3: 默认前台(ModeDefault)长思考期间应出现可见思考指示,实得空输出")
	}
}

// ---------------------------------------------------------------------------
// 60.1-UNIT-008 (AC3, RED): 高频思考增量被节流/聚合(不逐 delta 刷屏)
// ---------------------------------------------------------------------------

func TestATDD_60_1_AC3_ThinkingThrottled(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{ColorLevel: 0}}
	p := NewProgressReporter(r)

	const n = 50
	for range n {
		p.AgentThinking(1, 1, "delta")
	}

	lines := strings.Count(buf.String(), "\n")
	if lines == 0 {
		t.Fatal("AC3: 持续思考应产生至少一条可见反馈(节流下限)")
	}
	if lines >= n {
		t.Errorf("AC3: 思考反馈必须节流 —— %d 次增量不应产生 %d 行(逐 delta 刷屏),实得 %d 行",
			n, n, lines)
	}
}
