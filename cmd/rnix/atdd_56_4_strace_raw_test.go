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
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
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
	t.Skip("Story 56.4 RED: runStraceRaw render not implemented — dev removes skip, starts in-proc daemon + raw.jsonl fixture, verifies effort 真实值可见")

	// 期望（dev 实现后）：--raw 模式对含 reasoning_effort=high 的 API 记录
	// 渲染人类可读文本，输出须含 "reasoning_effort" 与 "high"（CAP-3 核心）。
}

// ---------------------------------------------------------------------------
// AC#2: --json 输出原始 vfs.RawCapture JSON (t.Skip — 需 live daemon + fixture)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC2_StraceRaw_JSONOutput(t *testing.T) {
	t.Skip("Story 56.4 RED: runStraceRaw --json not implemented — dev removes skip, verifies raw RawCapture JSON (NDJSON/array, 与既有 strace --json 风格一致)")

	// 期望（dev 实现后）：--raw --json 输出原始 RawCapture JSON（含 kind/step/
	// request/response），每条一行 NDJSON 或数组，可被 json.Unmarshal 回 vfs.RawCapture。
}

// ---------------------------------------------------------------------------
// AC#2: 无记录时明确提示, 非报错 (t.Skip — 需 live daemon)
// ---------------------------------------------------------------------------

func TestATDD_56_4_AC2_StraceRaw_NoRecords_Notice(t *testing.T) {
	t.Skip("Story 56.4 RED: runStraceRaw no-records notice not implemented — dev removes skip, verifies '[strace] no raw captures for <uuid>' 非报错退出")

	// 期望（dev 实现后）：对一个存在但无 raw.jsonl 的进程，--raw 给出形如
	// "[strace] no raw captures for <uuid>" 的提示，返回 nil。
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
