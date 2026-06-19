package tree

// ATDD red-phase scaffolds for Story 56.6 — 合成节点在 Agent Tree 的可见性 + 视觉标记
// (AC7). 树构建挂载是零改动复用（builder.go:300-305 PID==0 && ParentUUID!="" 已支持）；
// 视觉标记（图标/标签 + RNIX_ASCII 降级）是新增。
//
// 红灯机制（记忆 atdd-code-story-red-mechanism-preference）:
//   🟢 GREEN-guard（UNIT-018，不 skip）: tree builder 经 ParentUUID 把 PID=0 合成
//      节点挂在宿主下——零改动应已支持，护栏防回归（对齐 spec-agent-tree-uuid-build）。
//   🔴 RED（UNIT-019/020）: 合成节点行渲染须有视觉标记区分（Unicode + RNIX_ASCII
//      降级）；现状渲染不读 Synthetic → 与普通节点无差异。dev-story 移 skip 验 RED→GREEN。
//
// 测试级别: UNIT（internal/dashboard/tree 纯构建 + 渲染）。Synthetic 字段经
// TreeNode/FlatRow 内嵌的 vfs.ProcInfo 自动携带（types.go），无需额外骨架。

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// renderTree566 builds + flattens + renders a process tree to its string form,
// using a minimal RenderContext. Used to assert synthetic-node visual markers.
func renderTree566(procs []vfs.ProcInfo) string {
	roots := BuildProcessTree(procs, 1, true) // PID sort asc for stable order
	rows := FlattenTree(roots)
	state := TreeState{Rows: rows}
	ctx := RenderContext{Now: time.Now()}
	return Render(state, ctx, 80, 24)
}

// hostAndSynthChild566 returns a host process and a synthetic child differing only
// in the Synthetic flag (plainChild has it false) so render diffs isolate the marker.
//
// ⚠️ ATDD 发现（实测 builder.go）: 合成子节点必须设 PPID = 宿主 PID（非 0）。
// Story 称「PID=0 + ParentUUID 已支持，tree builder 零改动」不精确——若 PPID==0，
// 节点先命中 builder.go:294 的 `p.PID == p.PPID`（0==0）根短路，被当作根浮起，
// 永远到不了 :300/:314 的 ParentUUID 挂载分支。设 PPID=宿主PID（非 0）后经
// :314 `ParentUUID != ""` 分支正确挂载，确为零改动。合成侧须落实 PPID=host.PID。
func hostAndSynthChild566() (host, synthChild, plainChild vfs.ProcInfo) {
	now := time.Now()
	host = vfs.ProcInfo{PID: 1, UUID: "u1", State: types.StateRunning, Intent: "host agent", CreatedAt: now}
	synthChild = vfs.ProcInfo{
		PID: 0, PPID: 1, UUID: "synth1", ParentUUID: "u1", Synthetic: true,
		State: types.StateDead, Intent: "subagent task", CreatedAt: now,
	}
	plainChild = synthChild
	plainChild.Synthetic = false
	return host, synthChild, plainChild
}

// ---------------------------------------------------------------------------
// 🟢 56-6-UNIT-018 (P0, AC7 · GREEN-guard 不 skip): 合成节点（PID==0 + PPID=宿主PID
// + 非空 ParentUUID）经既有 ParentUUID 链路挂在宿主下——tree builder 零改动。
// 对齐 spec-agent-tree-uuid-build。⚠️ 见 hostAndSynthChild566 godoc：零改动的前提
// 是合成侧设 PPID=host.PID（非 0），否则命中 PID==PPID 根短路——本护栏锁定该前提。
// ---------------------------------------------------------------------------
func TestATDD_56_6_UNIT_018_SyntheticChildHangsUnderHostByParentUUID(t *testing.T) {
	host, synthChild, _ := hostAndSynthChild566()
	roots := BuildProcessTree([]vfs.ProcInfo{host, synthChild}, 1, true)

	if len(roots) != 1 {
		t.Fatalf("AC7 FAIL: 期望单根（宿主），got %d 个根（合成节点未挂在宿主下，浮为根——检查 PPID=host.PID 前提）", len(roots))
	}
	if roots[0].Proc.UUID != "u1" {
		t.Fatalf("AC7 FAIL: 根应为宿主 u1, got %q", roots[0].Proc.UUID)
	}
	if len(roots[0].Children) != 1 || roots[0].Children[0].Proc.UUID != "synth1" {
		t.Errorf("AC7 FAIL: 合成节点未经 ParentUUID 挂在宿主下（PID=0+PPID=host.PID+ParentUUID 链路）")
	}
}

// ---------------------------------------------------------------------------
// 🔴 56-6-UNIT-019 (P2, AC7): 合成节点行渲染含视觉标记（与普通节点区分）。
// RED: render 不读 Synthetic → 合成与普通节点渲染相同。
// ---------------------------------------------------------------------------
func TestATDD_56_6_UNIT_019_SyntheticNodeRowHasVisualMarker(t *testing.T) {
	t.Skip("RED: 56.6: 树渲染未对合成节点加视觉标记；dev-story 移 skip 验 RED→GREEN")

	host, synthChild, plainChild := hostAndSynthChild566()
	synthOut := renderTree566([]vfs.ProcInfo{host, synthChild})
	plainOut := renderTree566([]vfs.ProcInfo{host, plainChild})

	if synthOut == plainOut {
		t.Error("AC7 FAIL: 合成节点行渲染与普通节点完全相同（缺视觉标记区分图标/标签）")
	}
}

// ---------------------------------------------------------------------------
// 🔴 56-6-UNIT-020 (P2, AC7): 视觉标记在 RNIX_ASCII=1 下降级（仍存在，ASCII 形态）。
// RED: 无标记 → ASCII 模式下合成与普通节点同样无区分。
// ---------------------------------------------------------------------------
func TestATDD_56_6_UNIT_020_SyntheticMarkerASCIIDegrade(t *testing.T) {
	t.Skip("RED: 56.6: 合成节点视觉标记未实现（含 ASCII 降级）；dev-story 移 skip 验 RED→GREEN")

	t.Setenv("RNIX_ASCII", "1")
	host, synthChild, plainChild := hostAndSynthChild566()
	synthOut := renderTree566([]vfs.ProcInfo{host, synthChild})
	plainOut := renderTree566([]vfs.ProcInfo{host, plainChild})

	if synthOut == plainOut {
		t.Error("AC7 FAIL: RNIX_ASCII=1 下合成节点缺视觉标记（ASCII 降级标记应仍区分于普通节点）")
	}
}
