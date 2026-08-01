package context

import (
	"strings"
	"testing"
)

// =============================================================================
// ATDD 71.1 AC4/AC5 — 槽位上限取消（`0` = 无上限），原子准入职责保留。
//
// 槽位量的是 STRUCTURE（消息条数），token 量的是 CAPACITY（体积）。822 份实测
// 样本下二者无稳定换算率（205 槽 → 36.7k…146.2k tokens，4.0x 跨度），故槽位上限
// 是量纲错误而非校准问题——换任何阈值都不能修，只能取消。
//
// 路线 A（保留 MaxSize 字段但 `0` = 无上限）而非路线 B（删字段）的决定性理由就在
// 本文件下半部分：ErrContextFull 的原子准入路径必须**保持可达可测**，否则 epic
// 的回归红线会退化成一句无法验证的注释。
// =============================================================================

// --- AC4: 无上限时四处准入校验一律放行 ---

func TestATDD_71_1_AC4_UnlimitedContextAcceptsFarBeyondOldCeiling(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(0)
	if err != nil {
		t.Fatalf("CtxAlloc(0): %v", err)
	}

	// 300 > the retired 256 default, so a surviving ceiling fails here.
	const n = 300
	for i := range n {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		if err := m.AppendMessage(cid, role, "x"); err != nil {
			t.Fatalf("AppendMessage #%d rejected: %v", i, err)
		}
	}
	if err := m.AppendToolResult(cid, "tc-1", "result"); err != nil {
		t.Fatalf("AppendToolResult rejected: %v", err)
	}
	if err := m.CtxWrite(cid, 0, []byte(`{"role":"user","content":"raw"}`)); err != nil {
		t.Fatalf("CtxWrite rejected: %v", err)
	}
	if err := m.AppendAssistantWithToolCalls(cid, "a", "", nil, []ToolCall{
		{ID: "t1"}, {ID: "t2"}, {ID: "t3"},
	}); err != nil {
		t.Fatalf("AppendAssistantWithToolCalls rejected: %v", err)
	}

	used, max, err := m.SlotUsage(cid)
	if err != nil {
		t.Fatalf("SlotUsage: %v", err)
	}
	if used != n+3 {
		t.Errorf("used = %d, want %d (every write must have landed)", used, n+3)
	}
	if max != 0 {
		t.Errorf("max = %d, want 0 (no ceiling configured)", max)
	}
}

// TestATDD_71_1_AC4_AvailableSlotsSentinel pins the sentinel rather than the
// arithmetic result. `MaxSize - len(Messages)` would be NEGATIVE with MaxSize 0,
// flipping every `available >= required` test into its opposite — the twelve
// call sites would then all take their "out of room" branch instead of their
// "plenty of room" one.
func TestATDD_71_1_AC4_AvailableSlotsSentinel(t *testing.T) {
	m := NewManager()
	cid, _ := m.CtxAlloc(0)
	for range 50 {
		_ = m.AppendMessage(cid, RoleUser, "x")
	}

	avail, err := m.AvailableSlots(cid)
	if err != nil {
		t.Fatalf("AvailableSlots: %v", err)
	}
	if avail != unlimitedSlots {
		t.Fatalf("AvailableSlots = %d, want the unlimitedSlots sentinel %d", avail, unlimitedSlots)
	}
	if avail <= 0 {
		t.Errorf("AvailableSlots = %d — a non-positive value inverts every caller's capacity test", avail)
	}
}

// TestATDD_71_1_AC4_SentinelSurvivesCallerSubtraction covers the reason the
// sentinel is MaxInt32 and not MaxInt: four call sites in kernel/compact.go
// subtract from this value. All four sit inside a guard that the sentinel makes
// false, so they are unreachable today — but MaxInt32+MaxInt32 still fits in an
// int64, so an unguarded future consumer does not silently wrap.
func TestATDD_71_1_AC4_SentinelSurvivesCallerSubtraction(t *testing.T) {
	m := NewManager()
	cid, _ := m.CtxAlloc(0)
	avail, _ := m.AvailableSlots(cid)

	if got := avail + avail; got <= 0 {
		t.Errorf("sentinel + sentinel = %d, want a positive value (overflow headroom is the point of MaxInt32)", got)
	}
	if got := 3 - avail; got >= 0 {
		t.Errorf("required(3) - sentinel = %d, want negative (a wrapped positive would request a huge drop)", got)
	}
}

// TestATDD_71_1_AC4_SlotUsageStaysHonest: `used` is real history length and
// remains a meaningful observability figure; only `max` (and the percentage
// derived from it) go to zero.
func TestATDD_71_1_AC4_SlotUsageStaysHonest(t *testing.T) {
	m := NewManager()
	cid, _ := m.CtxAlloc(0)
	for range 7 {
		_ = m.AppendMessage(cid, RoleUser, "x")
	}

	stats, err := m.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if stats.SlotUsed != 7 {
		t.Errorf("SlotUsed = %d, want 7 (history length must stay accurate)", stats.SlotUsed)
	}
	if stats.SlotMax != 0 {
		t.Errorf("SlotMax = %d, want 0", stats.SlotMax)
	}
	if stats.SlotPercentage != 0 {
		t.Errorf("SlotPercentage = %.1f, want 0 (no denominator exists)", stats.SlotPercentage)
	}
}

// --- AC5: 原子准入职责保持可达可测 ---

// TestATDD_71_1_AC5_AtomicAdmissionStillReachableWithCeiling is the decisive
// argument for route A over "delete the field". With an explicit ceiling the
// admission check is byte-for-byte what it was, so ErrContextFull →
// kernel/tool_exec.go errors.Is → selfSuspend("context_full") stays a live,
// testable path instead of becoming unreachable dead code.
func TestATDD_71_1_AC5_AtomicAdmissionStillReachableWithCeiling(t *testing.T) {
	m := NewManager()
	cid, err := m.CtxAlloc(2)
	if err != nil {
		t.Fatalf("CtxAlloc(2): %v", err)
	}

	// 1 assistant + 2 tool results = 3 slots > 2.
	err = m.AppendAssistantWithToolCalls(cid, "a", "", nil, []ToolCall{{ID: "t1"}, {ID: "t2"}})
	if err == nil {
		t.Fatal("expected ErrContextFull with an explicit ceiling of 2")
	}
	if !strings.Contains(err.Error(), "context buffer full") {
		t.Errorf("error = %v, want the ErrContextFull sentinel wrapped in", err)
	}

	// And nothing was written — that partial write is exactly the protocol-illegal
	// state the guarantee exists to prevent (providers reject an assistant turn
	// whose tool_calls have no matching tool results).
	used, _, _ := m.SlotUsage(cid)
	if used != 0 {
		t.Errorf("used = %d, want 0 — a rejected group must not write the assistant message", used)
	}
}

// TestATDD_71_1_AC5_UnlimitedSatisfiesTheGuaranteeMoreStrongly: with no ceiling
// the atomicity promise holds in its strongest form (there is always room), so
// the check is SKIPPED rather than weakened.
func TestATDD_71_1_AC5_UnlimitedSatisfiesTheGuaranteeMoreStrongly(t *testing.T) {
	m := NewManager()
	cid, _ := m.CtxAlloc(0)

	toolCalls := make([]ToolCall, 40)
	for i := range toolCalls {
		toolCalls[i] = ToolCall{ID: "t" + string(rune('a'+i%26))}
	}
	if err := m.AppendAssistantWithToolCalls(cid, "a", "", nil, toolCalls); err != nil {
		t.Fatalf("unlimited context rejected a 41-slot group: %v", err)
	}

	used, _, _ := m.SlotUsage(cid)
	if used != 1 {
		t.Errorf("used = %d, want 1 (one assistant message; the tool results follow separately)", used)
	}
}
