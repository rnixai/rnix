// Package debug — state.go (Story 38-5 PR11 Step 1 + Step 4(a) cascade fix)
//
// DebugState 字段抽离自 cmd/rnix/dashboard.go::dashboardModel 的 debug 字段
// （Story 34.6 落地的 strace fusion debug 模式）。
//
// **设计决策演进**:
//
//   - PR11 Step 1（2026-05-04）：仅抽出 12 个标量字段，**不**抽 Events / StraceEvents
//     两个 `[]UnifiedEvent` slice 字段（UnifiedEvent 类型 cascade 阻塞）；
//
//   - **PR11 Step 4(a) cascade 解除**（2026-05-04 同会话延伸）：UnifiedEvent
//     已迁至 internal/dashboard/event 包（commit a08ae3d）· 本包现可直接引用
//     event.UnifiedEvent · Events / StraceEvents 完整迁出至 DebugState（与 spec
//     § Tasks 11.3 line 187 「14 字段」最终对齐）。
//
// 设计原则与 PR2-PR10 同模式（值类型 · 字段公开 · nil 安全）。
//
// **34.6 strace fusion 行为契约保留**（关键）：
//   - DebugState 持有独立 IPC 连接（Client）+ strace channel（StraceCh）；
//   - OnExit 必须关闭 Client（避免 goroutine leak · Story 34.6 落地）；
//   - 历史 watermark + auto-reload 防止重复加载（Story 34.6 落地）；
//   - 设备延迟统计映射（DeviceLatency）配合 strace events 实时计算；
//   - debugEvents (merged step + strace) / debugStraceEvents (strace ring) 行为不变。
//
// **DeviceLatency 类型决策**（与 inspector PrevMode / PR2-PR10 type alias 同模式）：
//   - 原 cmd/rnix.deviceLatencyStats 类型迁出至 debug.DeviceLatencyStats（公开字段）；
//   - cmd/rnix 端 type alias `type deviceLatencyStats = debug.DeviceLatencyStats`
//     保留旧名零行为变化。
package debug

import (
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// DeviceLatencyStats 收纳设备延迟统计（迁出自 cmd/rnix.deviceLatencyStats）。
//
// 字段语义（与原 cmd/rnix.deviceLatencyStats 完全等价）：
//   - Count：累计调用次数；
//   - TotalMs：累计延迟总和（毫秒）；
//   - ErrorCount：累计错误次数。
//
// AvgMs 返回平均延迟（Count==0 时返回 0 防止除零）。
type DeviceLatencyStats struct {
	Count      int
	TotalMs    float64
	ErrorCount int
}

// AvgMs 返回平均延迟（毫秒）。Count==0 时返回 0。
func (s DeviceLatencyStats) AvgMs() float64 {
	if s.Count == 0 {
		return 0
	}
	return s.TotalMs / float64(s.Count)
}

// DebugState 持有 Debug 模式（Story 34.6 strace fusion）的全部 14 字段。
//
// 字段语义（与原 dashboardModel 的 debug* 字段完全等价）：
//
// 模式 + IPC：
//   - Mode：Debug 模式是否激活；
//   - Client：独立 IPC 连接（用于 strace stream · OnExit 必须 Close 防泄漏）；
//   - StraceCh：strace event channel（goroutine read 端 · debugStraceCh 类型一致）；
//   - ShowStrace：strace 可见性 toggle（默认 true · Story 34.6）；
//
// 数据 + 统计：
//   - CtxProfile：Context Profile 数据（debug.CtxProfileResult · 进程上下文热力图）；
//   - DeviceLatency：设备延迟统计（按设备路径分组 · 实时计算）；
//   - Events：merged debug timeline (steps + strace) · debug 模式下渲染数据源；
//   - StraceEvents：strace event ring buffer · ShowStrace=true 时与 Events 合并。
//
// 进程跟踪：
//   - AttachedPID：当前 attached 用于 strace 的 PID（与 selectedPID 解耦 · 防止 PID 切换中断）；
//   - AutoReloaded：防止重复加载历史事件（stream 结束后只加载一次）；
//   - HistWatermark：历史加载的最大 TimestampMs（stream events ≤ 此值跳过去重）；
//
// 视觉控制：
//   - ScrollTop：debug timeline 滚动偏移；
//   - Cursor：debug timeline cursor 位置；
//   - AutoScroll：是否自动滚动到最新事件（默认 true）。
//
// Nil 安全：所有指针 / channel / map / slice 字段允许 nil；DeviceLatency 推荐用
// make(map[string]*DeviceLatencyStats) 初始化（cmd/rnix 端 newDashboardModel 已正确初始化）；
// Events / StraceEvents nil slice 安全（len(nil) == 0 / range 安全）。
type DebugState struct {
	Mode          bool
	Client        *ipc.Client
	StraceCh      <-chan ipc.SyscallEventWire
	ShowStrace    bool
	CtxProfile    *debug.CtxProfileResult
	DeviceLatency map[string]*DeviceLatencyStats
	Events        []event.UnifiedEvent // merged debug timeline (steps + strace) · PR11 Step 4(a) cascade fix
	StraceEvents  []event.UnifiedEvent // strace event ring buffer · PR11 Step 4(a) cascade fix
	AttachedPID   types.PID
	AutoReloaded  bool
	HistWatermark int64
	ScrollTop     int
	Cursor        int
	AutoScroll    bool
	// ExpandedThinking — Story 60.2: 已展开的 DriverThinking 聚合块集合，键为该块
	// 首事件的 TimestampMs（int64 · 跨 re-render 稳定 · live append 不漂移）。
	// CollapseThinkingGroups 据此决定折叠块投影为 1 行摘要还是「摘要 + 有界正文行」。
	ExpandedThinking map[int64]bool
	// ExpandedToolInput — Story 65.3: 已展开的 DriverToolCall input_delta 聚合块集合，
	// 键为该组首分片的 TimestampMs（int64 · 语义对齐 ExpandedThinking · 独立 map 防与
	// thinking 块 ts 键碰撞）。CollapseToolInputGroups 据此决定折叠组投影为 1 行摘要
	// 还是「摘要 + 有界正文行」。
	ExpandedToolInput map[int64]bool
}
