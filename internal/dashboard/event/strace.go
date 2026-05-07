// Package event — strace.go (Story 38-5 PR11 Step 4(c) · straceToUnifiedEvent
// 迁出 · 续 PR11 Step 4(a-2)/Step 4(c) helper 迁出 · 同 ClampCursor /
// AppendStraceEvent / FilterDebugEvents pattern)
//
// 本文件把 cmd/rnix/dashboard_debug.go::straceToUnifiedEvent 的纯函数转换体迁
// 至 event 包：把 ipc.SyscallEventWire 转换为 UnifiedEvent（Type=EventSyscall），
// 并通过 debug.FormatEvent 生成单行 summary。
//
// **迁移动机**（PR11 Step 4(c) · 2026-05-05）：
//
//   - straceToUnifiedEvent 是无 dashboardModel 状态依赖的纯转换 · 与 PR11
//     Step 4(a-2) 已迁的 timeline event helpers + Step 4(c) 已迁的 debug
//     state mutators 同模式（pure code motion）；
//   - 该函数产出 UnifiedEvent · 已经在 event 包内 · 把转换函数也放 event 包让
//     "事件类型 + 事件来源转换" 同住一个包 · 减少 cmd/rnix 端跨包跳转；
//   - cmd/rnix 端保留 straceToUnifiedEvent thin wrapper 让 7 处 callsite
//     （3 处主代码 + 4 处测试）零修改通过 · 与已迁出 helper 同 wrapper 模式。
//
// 包边界（spec § 04 风险 3 缓解）：
//   - 不 import cmd/rnix（go module 边界已强制）；
//   - 直接依赖 ipc.SyscallEventWire（已是 event 包入口类型 · UnifiedEvent.RawEvent
//     已经引用 *ipc.SyscallEventWire）；
//   - 直接依赖 debug.FormatEvent + types.SyscallEvent（kernel 层 strace 格式化
//     工具 · 与本包 ipc.SyscallEventWire 同抽象级别）。
//
// 行为契约（cmd/rnix.straceToUnifiedEvent 等价 · 0-行为变更纯重构）：
//   - 时间戳：time.UnixMilli(sew.TimestampMs)（与原版逐字段对齐）；
//   - 严重度：sew.Error == "" → SevInfo · 否则 SevError；
//   - 内联 wireToSyscallEvent 字段映射（避免跨 cmd/rnix 反向依赖）；
//   - Summary：debug.FormatEvent(se, debug.Options{ColorEnabled: false})；
//   - RawEvent：sew 副本指针（避免共享底层数组 · 与原实现一致）；
//   - PID：sew.PID（保持 strace event 的进程归属）；
//   - Type：固定为 EventSyscall。

package event

import (
	"errors"
	"time"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// StraceToUnifiedEvent converts an ipc.SyscallEventWire (received over the
// daemon Unix socket) into a UnifiedEvent for the dashboard's unified event
// stream. Pure function · no dashboard state dependency.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05): Migrated from
// cmd/rnix/dashboard_debug.go::straceToUnifiedEvent. The cmd/rnix wrapper
// retains the lowercase name as a thin pass-through, so existing callsites
// (dashboard_debug.go × 3 + dashboard_debug_test.go × 4) continue to work
// unchanged.
//
// The wireToSyscallEvent helper (cmd/rnix/main.go:1215) is inlined here to
// keep this package self-contained without reverse dependency on cmd/rnix.
// cmd/rnix retains its own copy of wireToSyscallEvent for use by gdb.go +
// main.go (non-dashboard callers).
func StraceToUnifiedEvent(sew ipc.SyscallEventWire) UnifiedEvent {
	ts := time.UnixMilli(sew.TimestampMs)

	// Inline wireToSyscallEvent field mapping (cmd/rnix/main.go:1215) to avoid
	// reverse dep on cmd/rnix. Behaviour preserved verbatim.
	se := types.SyscallEvent{
		Timestamp: time.Duration(sew.TimestampMs) * time.Millisecond,
		PID:       sew.PID,
		Syscall:   sew.Syscall,
		Args:      sew.Args,
		Result:    sew.Result,
		Duration:  time.Duration(sew.DurationMs * float64(time.Millisecond)),
		TraceID:   types.TraceID(sew.TraceID),
		SpanID:    types.SpanID(sew.SpanID),
	}
	if sew.Error != "" {
		se.Err = errors.New(sew.Error)
	}

	summary := debug.FormatEvent(se, debug.Options{ColorEnabled: false})

	sev := SevInfo
	if sew.Error != "" {
		sev = SevError
	}

	rawCopy := sew
	return UnifiedEvent{
		Type:      EventSyscall,
		Severity:  sev,
		Timestamp: ts,
		PID:       sew.PID,
		Summary:   summary,
		RawEvent:  &rawCopy,
	}
}
