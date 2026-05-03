// Package plugin — health.go (Story 38-5 PR1)
//
// HealthPlugin 收纳 dashboard 顶部状态指示（errorCount / warnCount / heartbeat），
// 曾在 dashboardModel 占 3 字段（spec § 01 表格行 19/23）。
//
// PR1 阶段仅落地骨架，PR11 在 App Model 瘦身时把 errorCount/warnCount/heartbeatStatus
// 字段从 dashboardModel 迁入。
package plugin

import "time"

// HealthPlugin 持有 dashboard 健康指示状态。
//
// 字段语义：
//   - ErrorCount/WarnCount: 累计错误 / 警告事件数（用于 Title Bar 红/黄角标）；
//   - HeartbeatStatus:      心跳活性指示（"alive" / "stalled" / "down"）；
//   - LastBeatAt:           上次收到心跳事件的时刻（用于 staleness 检测）。
type HealthPlugin struct {
	ErrorCount      int
	WarnCount       int
	HeartbeatStatus string
	LastBeatAt      time.Time
}

// IsHealthy 当心跳活跃且无累计错误时返回 true。
//
// PR1 仅占位实现；具体阈值（如 LastBeatAt 距 now 超过 5s 视为 stalled）在 PR11 落地。
func (h *HealthPlugin) IsHealthy() bool {
	if h == nil {
		return false
	}
	return h.ErrorCount == 0 && h.HeartbeatStatus != "down"
}
