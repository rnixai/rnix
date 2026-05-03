// Package timeline — types.go (Story 38-5 PR4 Step 1)
//
// 定义 Timeline pane 专用类型：StepEntry / StepDetailLevel / TimelineExpandMode。
//
// 边界说明（与 PR2/PR3 模式一致）：
//   - spec § Project Structure Notes 标注 stepEntry「保留 cmd/rnix」，但实际只有 Timeline pane
//     与 Inspector（间接使用 ipc.StepSummaryWire）使用 stepEntry 容器。按"必要扩边界"原则
//     迁入本包 + cmd/rnix 端用 type alias 保留旧名（type stepEntry = timeline.StepEntry），
//     让现有测试 grep 字符串与字面契约兼容。
//   - StepDetailLevel 的值域（Summary=0, Expanded=1, Debug=2）与 cmd/rnix 端原定义保持一致；
//   - TimelineExpandMode 的值域（Collapsed=0, Expanded=1, ErrorsOnly=2）保持 Story 36-4 落地的语义。
package timeline

import (
	"github.com/rnixai/rnix/ipc"
)

// StepDetailLevel 是 Timeline 中单个 step 的展开层级（影响 v / V 键行为）。
//
// 取值：
//   - LevelSummary   (0): 默认折叠，仅显示 1 行摘要；
//   - LevelExpanded  (1): v 键展开，显示完整步骤详情；
//   - LevelDebug     (2): V 键展开，显示 raw JSON / debug 信息。
type StepDetailLevel int

const (
	LevelSummary  StepDetailLevel = 0
	LevelExpanded StepDetailLevel = 1
	LevelDebug    StepDetailLevel = 2
)

// StepEntry 是 Timeline pane 的 step 容器，承载从 IPC 拉取的 StepSummaryWire
// + 当前展开层级 + autoExpand 标记。
//
// 字段说明：
//   - Summary:    IPC 拉取的步骤摘要（来自 list_steps / get_step_detail 协议）；
//   - Level:      当前展开层级（v/V 键改变）；
//   - AutoExpand: 错误步骤自动展开标记（Story 36-4 expandModeErrorsOnly 触发）。
type StepEntry struct {
	Summary    ipc.StepSummaryWire
	Level      StepDetailLevel
	AutoExpand bool
}

// TimelineExpandMode 是 Timeline 三态展开模式（Story 36-4 落地）。
//
// 取值：
//   - ExpandModeCollapsed   (0): 默认全部折叠（每行 1 项）；
//   - ExpandModeExpanded    (1): 全部展开（s 键切换）；
//   - ExpandModeErrorsOnly  (2): 仅错误展开（Story 36-4 三态切换的中间态）。
//
// 跨 PID 行为：切 PID 时由 dashboardModel.handleTimelinePIDChange 重置为 Collapsed
// （session 内不持久 · spec § AC4 行为契约保留）。
type TimelineExpandMode int

const (
	ExpandModeCollapsed  TimelineExpandMode = 0
	ExpandModeExpanded   TimelineExpandMode = 1
	ExpandModeErrorsOnly TimelineExpandMode = 2
)
