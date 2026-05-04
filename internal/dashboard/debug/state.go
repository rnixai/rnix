// Package debug — state.go (Story 38-5 PR11 Step 1)
//
// DebugState 字段抽离自 cmd/rnix/dashboard.go::dashboardModel 的 debug 字段
// （Story 34.6 落地的 strace fusion debug 模式）。
//
// **设计决策**：本 PR Step 1 抽出 12 个标量字段，**不**抽 Events / StraceEvents 两个
// `[]UnifiedEvent` slice 字段。原因：
//   - UnifiedEvent 类型定义在 cmd/rnix.dashboard_types.go（cmd/rnix 私有）；
//   - 若把 Events 迁入 DebugState，需要先把 UnifiedEvent 类型迁到 internal/dashboard 共享
//     位置（cascade 很大 · alerts/timeline/eventstream 都在用）；
//   - cascade 改动超出本 PR Step 1 的合理边界。
//
// Events / StraceEvents 保留在 dashboardModel，PR11 后续 commits（App Model 瘦身）评估
// 是否一并迁出 UnifiedEvent。
//
// 设计原则与 PR2-PR10 同模式（值类型 · 字段公开 · nil 安全）。
//
// **34.6 strace fusion 行为契约保留**（关键）：
//   - DebugState 持有独立 IPC 连接（Client）+ strace channel（StraceCh）；
//   - OnExit 必须关闭 Client（避免 goroutine leak · Story 34.6 落地）；
//   - 历史 watermark + auto-reload 防止重复加载（Story 34.6 落地）；
//   - 设备延迟统计映射（DeviceLatency）配合 strace events 实时计算。
//
// **DeviceLatency 类型决策**（与 inspector PrevMode / PR2-PR10 type alias 同模式）：
//   - 原 cmd/rnix.deviceLatencyStats 类型迁出至 debug.DeviceLatencyStats（公开字段）；
//   - cmd/rnix 端 type alias `type deviceLatencyStats = debug.DeviceLatencyStats`
//     保留旧名零行为变化。
package debug

import (
	"github.com/rnixai/rnix/debug"
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

// DebugState 持有 Debug 模式（Story 34.6 strace fusion）的标量字段（12 字段）。
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
// **未抽出**字段（仍在 dashboardModel · 见包级注释）：
//   - debugEvents []UnifiedEvent（cmd/rnix 私有类型 cascade）；
//   - debugStraceEvents []UnifiedEvent（同上）。
//
// Nil 安全：所有指针 / channel / map 字段允许 nil；DeviceLatency 推荐用
// make(map[string]*DeviceLatencyStats) 初始化（cmd/rnix 端 newDashboardModel 已正确初始化）。
type DebugState struct {
	Mode          bool
	Client        *ipc.Client
	StraceCh      <-chan ipc.SyscallEventWire
	ShowStrace    bool
	CtxProfile    *debug.CtxProfileResult
	DeviceLatency map[string]*DeviceLatencyStats
	AttachedPID   types.PID
	AutoReloaded  bool
	HistWatermark int64
	ScrollTop     int
	Cursor        int
	AutoScroll    bool
}
