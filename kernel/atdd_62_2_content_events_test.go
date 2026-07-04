package kernel

// Story 62.2 green-guards（事后补记，非红灯 ATDD）— codex --json 流可观察性
// 对齐（调查 codex-cli-observability-parity R3/R2 的 kernel 侧分流契约）:
//
//   - content 事件（driver 经 vfsfile.go 转发）刷新 heartbeat；token 级 delta
//     （无 subtype）不落 events.jsonl（防 API driver 每 token 一行的洪泛），
//     消息级（subtype=agent_message，CLI driver 整段输出）落盘为 DriverMessage。
//   - "item" 事件（driver 对未知 item 类型的 pass-through）落盘为 DriverItem，
//     不再静默消失（如 codex error item 承载的 multi_agent_v2 警告）。
//
// 注入机制复用 40.4 的 streamHarness（stub StreamObserver 捕获 handler 闭包）。

import (
	"testing"
	"time"
)

// feedAndReadEvents feeds events through the captured handler, closes the
// eventWriter so its buffer flushes, and reads back events.jsonl.
func feedAndReadEvents(t *testing.T, h *streamHarness, evts ...map[string]any) []SyscallEventDisk {
	t.Helper()
	for _, e := range evts {
		h.feed(e)
	}
	if h.tk.proc.eventWriter != nil {
		_ = h.tk.proc.eventWriter.Close()
		h.tk.proc.eventWriter = nil
	}
	return h.tk.readEvents(t)
}

// countSyscall returns how many persisted events carry the given syscall name.
func countSyscall(events []SyscallEventDisk, syscall string) int {
	n := 0
	for _, e := range events {
		if e.Syscall == syscall {
			n++
		}
	}
	return n
}

// 62.2-G1: token 级 content delta（无 subtype）必须刷新 heartbeat 且不落
// events.jsonl —— heartbeat 是 R3 的核心修复（长内容流期间 STALL 误报），
// 不落盘是防洪泛约束（API driver 每 token 一个 delta）。
func TestATDD_62_2_G1_ContentDeltaTouchesHeartbeatWithoutPersisting(t *testing.T) {
	h := newStreamHarness(t)

	stale := time.Now().Add(-1 * time.Hour)
	h.tk.proc.LastHeartbeat = stale

	events := feedAndReadEvents(t,
		h,
		map[string]any{"type": "content", "content": "tok"},
		map[string]any{"type": "content", "content": "en-by-"},
		map[string]any{"type": "content", "content": "token"},
	)

	if !h.tk.proc.LastHeartbeat.After(stale) {
		t.Errorf("content delta did not refresh heartbeat: still %v", h.tk.proc.LastHeartbeat)
	}
	if n := countSyscall(events, "DriverMessage"); n != 0 {
		t.Errorf("token-level content deltas must not persist DriverMessage events, got %d", n)
	}
}

// 62.2-G2: 消息级 content（subtype=agent_message，codex/cursor/qwen 的整段
// 输出）落盘为 DriverMessage —— 此前 codex 主线程 wait 循环期的 35 条
// agent_message 在 events.jsonl 零痕迹。
func TestATDD_62_2_G2_AgentMessagePersistedAsDriverMessage(t *testing.T) {
	h := newStreamHarness(t)

	events := feedAndReadEvents(t,
		h,
		map[string]any{"type": "content", "subtype": "agent_message", "content": "Worker finished."},
	)

	if n := countSyscall(events, "DriverMessage"); n != 1 {
		t.Errorf("expected exactly 1 DriverMessage event, got %d (events: %+v)", n, events)
	}
}

// 62.2-G3: driver 对未知 item 类型的 pass-through（type="item"）落盘为
// DriverItem —— codex error item（multi_agent_v2 警告）等不再静默消失。
func TestATDD_62_2_G3_UnknownItemPassthroughPersisted(t *testing.T) {
	h := newStreamHarness(t)

	events := feedAndReadEvents(t,
		h,
		map[string]any{
			"type":      "item",
			"content":   "error",
			"item_type": "error",
			"call_id":   "item_0",
			"message":   "Under-development features enabled: multi_agent_v2.",
		},
	)

	if n := countSyscall(events, "DriverItem"); n != 1 {
		t.Errorf("expected exactly 1 DriverItem event, got %d (events: %+v)", n, events)
	}
}
