package ipc

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 45.4 — Story 30.6 AC#10 IPC heartbeat_status wire schema 不变量
//
// 本文件迁移自 kernel/atdd_45_4_30_6_continuity_test.go（原 AC2.003 用例），
// 用 spec line 147-150 prescribed 的"instance 化 + json.Marshal"路径直接验证
// HeartbeatStatusResponse / StalledProcWire 两个 wire 类型的 JSON 字段集精确
// 匹配 spec 列出的 4+6 snake_case key（含负向断言：无意外字段）。
//
// 为何迁移：kernel 包不能 import ipc（依赖方向 ipc→kernel）。kernel 包内的
// 原实现走 source-grep ipc/protocol.go 旁路，证据强度不足以覆盖"且只有这些
// 字段"的负向断言。本文件在 ipc 包内直接 instance 化 wire 类型 + marshal +
// 解析 key 集合，提供真正的"不多不少"证据。
//
// 决策来源: code review 2026-05-24 — Decision-needed 选项 A 解决
//
// 引用：
//   - _bmad-output/implementation-artifacts/45-4-atdd-rewrite-for-warn-only-semantics.md
//     §AC2 用例 003 + §Review Findings (D1→Patch 迁移)
//   - _bmad-output/implementation-artifacts/30-6-supervisor-heartbeat-monitor-and-auto-recovery.md
//     §Acceptance-Criteria AC#10 (IPC wire schema)
//   - ipc/protocol.go#1327-1343 HeartbeatStatusResponse + StalledProcWire
// =============================================================================

// jsonTopLevelKeys returns the sorted list of top-level keys in a JSON object.
// Used to assert "exactly these keys, no more, no less" against a wire schema.
func jsonTopLevelKeys(t *testing.T, data []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("jsonTopLevelKeys: unmarshal: %v (payload=%s)", err, string(data))
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedCopy(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}

// TestATDD_45_4_30_6_003_HeartbeatStatusResponseWireSchemaInvariant
//
// 守护 Story 30.6 AC#10 IPC heartbeat_status response wire schema：
// 4 个 snake_case 字段 running / check_interval_ms / total_stalled_detected /
// current_stalled，且 marshal 结果**不多不少**正好这 4 个 key。
//
// 在 P4 daemon-passive 框架下，下游消费者（dashboard / `rnix heartbeat status`
// CLI / EchoMatrix 等）字段消费零风险 —— passive mode 仅改 case body，不动 wire
// schema。
func TestATDD_45_4_30_6_003_HeartbeatStatusResponseWireSchemaInvariant(t *testing.T) {
	resp := HeartbeatStatusResponse{
		Running:              true,
		CheckIntervalMs:      30000,
		TotalStalledDetected: 0,
		CurrentStalled:       nil,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Story 45.4 AC2.003: json.Marshal(HeartbeatStatusResponse): %v", err)
	}

	gotKeys := jsonTopLevelKeys(t, data)
	wantKeys := sortedCopy([]string{
		"running",
		"check_interval_ms",
		"total_stalled_detected",
		"current_stalled",
	})

	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("Story 45.4 AC2.003: HeartbeatStatusResponse wire keys count = %d, want %d\n  got:  %v\n  want: %v\n  payload: %s",
			len(gotKeys), len(wantKeys), gotKeys, wantKeys, string(data))
	}
	for i, want := range wantKeys {
		if gotKeys[i] != want {
			t.Errorf("Story 45.4 AC2.003: HeartbeatStatusResponse wire key[%d] = %q, want %q\n  got:  %v\n  want: %v\n  payload: %s",
				i, gotKeys[i], want, gotKeys, wantKeys, string(data))
		}
	}
}

// TestATDD_45_4_30_6_003_StalledProcWireSchemaInvariant
//
// 守护 Story 30.6 AC#10 IPC StalledProcWire schema：6 个 snake_case 字段
// pid / uuid / consecutive_stalls / stalled_duration_ms / heartbeat_gap_ms /
// last_action，且 marshal 结果**不多不少**正好这 6 个 key。
func TestATDD_45_4_30_6_003_StalledProcWireSchemaInvariant(t *testing.T) {
	wire := StalledProcWire{
		PID:               types.PID(42),
		UUID:              "01234567-89ab-7cde-89ab-0123456789ab",
		ConsecutiveStalls: 3,
		StalledDurationMs: 1500,
		HeartbeatGapMs:    900,
		LastAction:        "warn",
	}

	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("Story 45.4 AC2.003: json.Marshal(StalledProcWire): %v", err)
	}

	gotKeys := jsonTopLevelKeys(t, data)
	wantKeys := sortedCopy([]string{
		"pid",
		"uuid",
		"consecutive_stalls",
		"stalled_duration_ms",
		"heartbeat_gap_ms",
		"last_action",
	})

	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("Story 45.4 AC2.003: StalledProcWire wire keys count = %d, want %d\n  got:  %v\n  want: %v\n  payload: %s",
			len(gotKeys), len(wantKeys), gotKeys, wantKeys, string(data))
	}
	for i, want := range wantKeys {
		if gotKeys[i] != want {
			t.Errorf("Story 45.4 AC2.003: StalledProcWire wire key[%d] = %q, want %q\n  got:  %v\n  want: %v\n  payload: %s",
				i, gotKeys[i], want, gotKeys, wantKeys, string(data))
		}
	}
}

// TestATDD_45_4_30_6_003_NestedWireRoundTrip
//
// 守护 nested wire 完整 round trip：嵌套 StalledProcWire 在 CurrentStalled
// slice 里 marshal/unmarshal 后字段集仍精确匹配（防止 nested 类型 marshal
// 路径与顶层路径 schema 漂移）。
func TestATDD_45_4_30_6_003_NestedWireRoundTrip(t *testing.T) {
	original := HeartbeatStatusResponse{
		Running:              true,
		CheckIntervalMs:      5000,
		TotalStalledDetected: 1,
		CurrentStalled: []StalledProcWire{
			{
				PID:               types.PID(7),
				UUID:              "01234567-89ab-7cde-89ab-fedcba987654",
				ConsecutiveStalls: 2,
				StalledDurationMs: 800,
				HeartbeatGapMs:    400,
				LastAction:        "cancel_step",
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Story 45.4 AC2.003: nested marshal: %v", err)
	}

	var roundTrip HeartbeatStatusResponse
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("Story 45.4 AC2.003: nested unmarshal: %v\n  payload: %s", err, string(data))
	}

	if roundTrip.Running != original.Running {
		t.Errorf("Running mismatch after round-trip")
	}
	if roundTrip.CheckIntervalMs != original.CheckIntervalMs {
		t.Errorf("CheckIntervalMs mismatch after round-trip")
	}
	if roundTrip.TotalStalledDetected != original.TotalStalledDetected {
		t.Errorf("TotalStalledDetected mismatch after round-trip")
	}
	if len(roundTrip.CurrentStalled) != 1 {
		t.Fatalf("len(CurrentStalled) = %d, want 1", len(roundTrip.CurrentStalled))
	}
	if roundTrip.CurrentStalled[0] != original.CurrentStalled[0] {
		t.Errorf("CurrentStalled[0] mismatch: got=%+v want=%+v", roundTrip.CurrentStalled[0], original.CurrentStalled[0])
	}
}
