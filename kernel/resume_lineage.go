package kernel

import (
	"fmt"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// Story 42.3: Resume Lineage（进程间 fork 关系图）— ATDD red-phase stub
//
// 与 Stem Cell Lineage（kernel/lineage*.go）完全独立 — 后者记录单进程内技能分化
// 历史；本文件构建跨进程的 fork 关系图（基于 OriginUUID 字段）。
//
// 完整实现在 dev-story 阶段补齐：沿 procHistory + 磁盘 proc-info.json 的
// OriginUUID 链向上递归，深度上限 32 防循环；扫描全表找直接后代。
// =============================================================================

// ResumeLineageMaxDepth 是 BuildResumeLineage 沿 OriginUUID 链向上递归的深度
// 上限。超出时返回当前链 + Truncated=true 标志（不抛错），让 UI 可以提示用户
// "lineage chain too deep"。
const ResumeLineageMaxDepth = 32

// ResumeLineageData 是 BuildResumeLineage 的返回类型，承载当前节点 + 祖先链 +
// 直接后代列表。Truncated 标志用于循环检测或深度上限触发。
type ResumeLineageData struct {
	Current     vfs.ProcInfo
	Ancestors   []vfs.ProcInfo // 沿 OriginUUID 向上，最远祖先在末尾
	Descendants []vfs.ProcInfo // OriginUUID==Current.UUID 的所有直接后代
	Truncated   bool           // 深度上限或循环检测触发
}

// BuildResumeLineage 沿 OriginUUID 字段在 procHistory + 磁盘双源中构建 lineage
// 图。深度上限 ResumeLineageMaxDepth；遇循环 / 深度超限设置 Truncated=true。
//
// 当查询的 UUID 在 procHistory 和磁盘均不存在时返回 *SyscallError{Code:ErrNotFound}。
//
// ATDD red-phase: 当前为 stub，dev-story 阶段填充真实逻辑。返回 ErrNotFound 让
// 测试断言模式生效（t.Skip 暂时阻止断言运行）。
func BuildResumeLineage(uuid string, history *ProcessHistory, stepDataDir string) (*ResumeLineageData, error) {
	_ = history
	_ = stepDataDir
	return nil, NewSyscallError("GetResumeLineage", 0, "",
		fmt.Errorf("BuildResumeLineage not yet implemented (uuid=%s)", uuid),
		types.ErrNotFound)
}

// GetResumeLineage is the kernel entry point for the IPC get_resume_lineage
// method. It delegates to BuildResumeLineage using the kernel's procHistory and
// stepDataDir.
//
// ATDD red-phase stub: dev-story replaces with proper delegation.
func (k *KernelImpl) GetResumeLineage(uuid string) (*ResumeLineageData, error) {
	return BuildResumeLineage(uuid, k.procHistory, k.stepDataDir)
}

// ProcHistory exposes the kernel's in-memory procHistory for test injection
// (Story 42.3). External callers (ipc package tests) need to seed lineage
// fixtures without going through the full spawn lifecycle.
//
// ATDD red-phase introduction: kept public so ATDD tests in ipc package can
// drive lineage queries against synthetic procHistory entries.
func (k *KernelImpl) ProcHistory() *ProcessHistory {
	return k.procHistory
}
