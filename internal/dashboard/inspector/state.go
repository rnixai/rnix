// Package inspector — state.go (Story 38-5 PR10 Step 1)
//
// InspectorState 字段抽离自 cmd/rnix/dashboard.go::dashboardModel 的 25 个 inspector
// 相关字段（含 diff mode 7 + follow live 2 + 38-3 AC#7/#8 lens marks/byte search 2）。
//
// 设计原则与 PR2-PR9 同模式（值类型 · 字段公开 · nil 安全 · 38-1/2/3/4 行为契约保留）。
//
// **38-3 / 36-6 行为契约保留**（关键 · spec § AC6 PR10 验收点）：
//   - Step Inspector 5-lens 视觉规则（38-3 落地 · 4色角色 + Tool I/O box + Step Rail
//     + Thumbnail Bar + JSON 高亮 + 词级搜索）保持在 cmd/rnix 端不变；
//   - Diff mode（base picker overlay / dd double-tap / lens marks / per-lens viewport）
//     38-3 + 36-6 落地行为完全保留；
//   - Follow Live（生成计数器 + 自动跟随最新 step）36-6 落地行为完全保留；
//   - 本 PR Step 1 不改 render 主体，仅做字段位置迁移。
//
// **PrevMode 字段类型决策**：
//   - 原 dashboardModel.inspectorPrevMode 类型为 cmd/rnix.viewMode（私有 enum）；
//   - 若迁入 inspector 包需把 viewMode 一并迁移，会引发广泛 cascade 改动；
//   - 折中方案：InspectorState.PrevMode 用 int 存储，cmd/rnix 端 cast `int(prevMode)`
//     <-> `viewMode(state.PrevMode)`，inspector 包对该 int 语义不做假设；
//   - 该决策在 PR11 App Model 瘦身时可重新评估（若 viewMode 一并迁移则可改回强类型）。
package inspector

import (
	"time"

	"charm.land/bubbles/v2/viewport"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// Lens 是 Step Inspector 五镜头的枚举类型。原 cmd/rnix.inspectorLens 同模式。
//
// PR10 Step 1: 迁出至 inspector 包以让 InspectorState/InspectorModel 自包含。
// cmd/rnix 端通过 `type inspectorLens = inspector.Lens` 保留旧名零行为变化
// （与 PR2 flatRow / PR3 heatmapSegment / PR4 stepEntry / PR6 intentFlatNode
// / PR8 spanFlatNode 同模式）。
type Lens int

// Lens 常量与原 cmd/rnix.lensConversation/lensSystem/lensToolIO/lensMeta/lensRawJSON
// 取值与顺序完全一致（38-3 落地的 5-lens 视觉规则依赖此顺序）。
const (
	LensConversation Lens = iota // ❶ Message flow
	LensSystem                   // ❷ System prompt
	LensToolIO                   // ❸ Tool call details
	LensMeta                     // ❹ Metadata
	LensRawJSON                  // ❺ Raw JSON
	LensRaw                      // ❻ Raw I/O (Story 56.4 · 原始请求/响应)
)

// LensCount 是 Lens 枚举的总数。原 cmd/rnix.inspectorLensCount 等价。
//
// Story 56.4: Raw lens（LensRaw ❻）落地后由 5 → 6——cmd/rnix lensNames /
// 按键 6 / viewport·content 数组（[LensCount]）随之扩容。
const LensCount = 6

// SearchMatchPos identifies the byte-range location of a single search hit
// inside the Inspector's currently active lens content. Story 38-3 AC#8:
// stored alongside the legacy SearchMatches []int (line numbers) so the
// Timeline path is unaffected — this struct is Inspector-only.
//
// PR10 Step 1: 迁出自 cmd/rnix/dashboard_inspector.go::searchMatchPos
// （字段全部公开 LineIdx/ByteStart/ByteEnd · cmd/rnix 端 type alias 兼容）。
type SearchMatchPos struct {
	LineIdx   int
	ByteStart int
	ByteEnd   int
}

// InspectorState 持有 Step Inspector overlay 的完整状态（25 字段）。
//
// 字段语义（与原 dashboardModel 的 inspector* 字段完全等价）：
//
// 核心步进状态（Story 36-1）：
//   - PID/UUID：当前 attached 进程的 PID + UUID（PID 复用感知 · 28-4 落地）；
//   - Step：当前正在查看的 step 序号（1-based）；
//   - StepMax：当前进程已知的最大 step 序号；
//   - Steps：步骤摘要列表（cache · IPC ListSteps 填充）；
//   - Detail：当前 step 的完整详情（IPC GetStepDetail 填充 · 含 LLM IO + tool call）；
//   - PrevDetail：上一个查看的 step 详情（diff mode 用）；
//   - PrevStep：上一个查看的 step 序号；
//   - CurDetailStep：Detail 实际对应的 step 序号（异步加载时与 Step 可能短暂不同）；
//
// 5-lens 视觉状态（Story 38-3）：
//   - Lens：当前激活的镜头（Conversation/System/ToolIO/Meta/RawJSON）；
//   - Viewports：5 个独立 viewport.Model（per-lens scroll position 保留）；
//   - Contents：5 个镜头的预渲染内容缓存；
//   - PrevMode：进入 Inspector 前的 view mode（int 形式存储 cmd/rnix.viewMode 数值）；
//   - Fetching：是否正在异步加载 Detail（防止 UI 闪烁）；
//   - SystemExpanded：System prompt lens 是否展开完整内容（38-3 落地）；
//
// Diff mode（Story 36-6 + 38-3 AC#7）：
//   - DiffMode：是否处于 diff 视觉模式；
//   - DiffBase：被 diff 的基准 step 序号；
//   - DiffDelta：当前 step 相对 base 的偏移（current - base）；
//   - DiffUnfolded：已展开的 fold region 索引集（map[diff-line-index]bool）；
//   - DiffPicker：base picker overlay 是否打开（按 D 进入）；
//   - DiffPickerCursor：picker 内的 cursor 位置（指向 Steps 索引）；
//   - DiffDdDeadline：dd double-tap 截止时间（按 d 后 500ms 内再按 d 退出 diff）；
//
// Follow Live（Story 36-6）：
//   - FollowLive：自动跟随最新 step（新 step 到达时自动跳转）；
//   - FollowGen：generation counter — 防止旧 tick 干扰（用户手动跳转后 +1 失效旧 tick）；
//
// 38-3 AC#7 / AC#8：
//   - DiffLensMarks：per-lens diff-mark cache（refresh on lens / base / current change）；
//   - SearchPos：byte-position search hits（word-level highlight · 与 SearchMatches []int
//     共存：Timeline 路径仍用行索引，Inspector 内部用字节位置）。
//
// Nil 安全：所有指针字段（Detail/PrevDetail/DiffUnfolded）均允许 nil；
// 切片字段（Steps/SearchPos）允许 nil 或空；DiffUnfolded 在 reset 时
// 显式置 nil（cmd/rnix 已有 lazy init pattern）。
type InspectorState struct {
	// Core step state (Story 36-1)
	PID           types.PID
	UUID          string
	Step          int
	StepMax       int
	Steps         []ipc.StepSummaryWire
	Detail        *ipc.GetStepDetailResponse
	PrevDetail    *ipc.GetStepDetailResponse
	PrevStep      int
	CurDetailStep int

	// 5-lens visual state (Story 38-3)
	Lens           Lens
	Viewports      [LensCount]viewport.Model
	Contents       [LensCount]string
	PrevMode       int // cmd/rnix.viewMode 数值（int 存储 · 见包级注释）
	Fetching       bool
	SystemExpanded bool

	// Diff mode (Story 36-6 + 38-3 AC#7)
	DiffMode         bool
	DiffBase         int
	DiffDelta        int
	DiffUnfolded     map[int]bool
	DiffPicker       bool
	DiffPickerCursor int
	DiffDdDeadline   time.Time

	// Follow Live (Story 36-6)
	FollowLive bool
	FollowGen  int

	// 38-3 AC#7 / AC#8
	DiffLensMarks [LensCount]bool
	SearchPos     []SearchMatchPos

	// Raw I/O lens (Story 56.4 · CAP-3 路②懒加载缓存)
	// 当 LensRaw 激活（或在该 lens 下切 step）时经 IPC GetRawCapture 懒加载该
	// step 的 raw 记录并缓存于此（nil 安全 · key=step 序号）。不进 GetStepDetail
	// 以免非 raw lens 的 step 拉取被增重。
	RawByStep map[int]*vfs.RawCapture
	// RawParseErrByStep 缓存每个 step 查询返回的 malformed 行计数（56.4 review
	// decision 1→a）· >0 时 Raw lens 渲染 "N line(s) skipped (malformed)"。
	RawParseErrByStep map[int]int
}
