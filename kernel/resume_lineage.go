package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// Story 42.3: Resume Lineage（进程间 fork 关系图）
//
// 与 Stem Cell Lineage（kernel/lineage*.go）完全独立 — 后者记录单进程内技能分化
// 历史；本文件构建跨进程的 fork 关系图（基于 OriginUUID 字段）。
//
// 数据源：
//   - procHistory（in-memory ring buffer，daemon 启动时通过 LoadHistory 填充）
//   - 磁盘 .rnix/data/steps/<uuid>/proc-info.json（procHistory evicted 后的兜底）
//
// 算法：
//   - 沿 OriginUUID 链向上递归，深度上限 ResumeLineageMaxDepth=32，超出或检测到
//     循环时返回 Truncated=true（但不抛错）。
//   - 直接后代：扫描 procHistory + 磁盘 entries 的 OriginUUID 字段，筛选等于
//     query UUID 的条目（不递归 → O(N)）。
// =============================================================================

// ResumeLineageMaxDepth 是 BuildResumeLineage 沿 OriginUUID 链向上递归的深度
// 上限。超出时返回当前链 + Truncated=true 标志（不抛错），让 UI 可以提示用户
// "lineage chain too deep"。
const ResumeLineageMaxDepth = 32

// ResumeLineageMaxDiskScan 是 findDescendantsByOrigin 磁盘 fallback 扫描的目录
// 上限。超出后停止扫描；Truncated 标志由调用方根据需要标记。该上限防止累积
// 数万历史 UUID 后 Dashboard detail focus 退化到 O(N) 磁盘 + JSON 解析。
const ResumeLineageMaxDiskScan = 1024

// ResumeLineageData 是 BuildResumeLineage 的返回类型，承载当前节点 + 祖先链 +
// 直接后代列表。Truncated 标志用于循环检测或深度上限触发。
type ResumeLineageData struct {
	Current     vfs.ProcInfo
	Ancestors   []vfs.ProcInfo // 沿 OriginUUID 向上，直接父节点在 [0]，最远祖先在末尾
	Descendants []vfs.ProcInfo // OriginUUID==Current.UUID 的所有直接后代
	Truncated   bool           // 深度上限或循环检测触发
}

// BuildResumeLineage 沿 OriginUUID 字段在 procHistory + 磁盘双源中构建 lineage
// 图。深度上限 ResumeLineageMaxDepth；遇循环 / 深度超限设置 Truncated=true。
//
// 当查询的 UUID 在 procHistory 和磁盘均不存在时返回 *SyscallError{Code:ErrNotFound}。
func BuildResumeLineage(uuid string, history *ProcessHistory, stepDataDir string) (*ResumeLineageData, error) {
	if uuid == "" {
		return nil, NewSyscallError("GetResumeLineage", 0, "",
			fmt.Errorf("empty UUID"), types.ErrInvalid)
	}
	// Reject anything that doesn't match the safe UUID alphabet (alphanumeric +
	// dashes). Without this guard, a malicious OriginUUID on disk or in an IPC
	// request could traverse out of the steps dir via "../..". Mirrors the
	// kernel/resume.go entry validation.
	if !isValidUUIDFormat(uuid) {
		return nil, NewSyscallError("GetResumeLineage", 0, "",
			fmt.Errorf("invalid UUID format: %s", uuid), types.ErrInvalid)
	}

	// 优先用 procHistory，磁盘兜底。
	cur := lookupAnyHistory(uuid, history, stepDataDir)
	if cur == nil {
		return nil, NewSyscallError("GetResumeLineage", 0, "",
			fmt.Errorf("UUID %s not found", uuid), types.ErrNotFound)
	}

	visited := make(map[string]bool)
	visited[uuid] = true
	truncated := false

	// 沿 OriginUUID 链向上。
	var ancestors []vfs.ProcInfo
	parent := cur.OriginUUID
	for depth := 0; parent != ""; depth++ {
		if depth >= ResumeLineageMaxDepth {
			truncated = true
			break
		}
		// Defensive: a corrupted proc-info.json could carry an OriginUUID with
		// path separators. Stop the walk rather than fall into readProcInfoFromDisk
		// where the validation is duplicated; this keeps the loop bounded.
		if !isValidUUIDFormat(parent) {
			truncated = true
			break
		}
		if visited[parent] {
			truncated = true // 循环检测
			break
		}
		visited[parent] = true
		node := lookupAnyHistory(parent, history, stepDataDir)
		if node == nil {
			// 祖先数据已被 gc / 丢失 — 链截断到此节点，但不算 truncated（与循环检测/深度上限语义不同）
			break
		}
		// Cycle-detection guard: when the lookup returned a node whose UUID
		// field does not match the directory name we walked into, treat it as
		// corrupted and stop. Otherwise a forged proc-info.json (uuid=Y inside
		// dir X) would escape the visited set and allow re-entry via Y.
		if node.UUID != parent {
			truncated = true
			break
		}
		ancestors = append(ancestors, *node)
		parent = node.OriginUUID
	}

	// 直接后代：扫描所有 entries 筛 OriginUUID == uuid。
	descendants := findDescendantsByOrigin(uuid, history, stepDataDir)

	return &ResumeLineageData{
		Current:     *cur,
		Ancestors:   ancestors,
		Descendants: descendants,
		Truncated:   truncated,
	}, nil
}

// lookupAnyHistory 先查 procHistory（in-memory），命中即返回；未命中时读磁盘
// proc-info.json。两源均无返回 nil。
func lookupAnyHistory(uuid string, history *ProcessHistory, stepDataDir string) *vfs.ProcInfo {
	if uuid == "" {
		return nil
	}
	if history != nil {
		if info := history.FindByUUID(uuid); info != nil {
			return info
		}
	}
	return readProcInfoFromDisk(stepDataDir, uuid)
}

// readProcInfoFromDisk reads proc-info.json for a UUID. The dataDir parameter
// is the global data directory; the function searches all project subdirectories.
func readProcInfoFromDisk(dataDir, uuid string) *vfs.ProcInfo {
	if dataDir == "" || uuid == "" {
		return nil
	}
	if !isValidUUIDFormat(uuid) {
		return nil
	}
	baseDir := FindBaseDirByUUID(dataDir, uuid)
	if baseDir == "" {
		return nil
	}
	path := filepath.Join(baseDir, "steps", uuid, procInfoFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var d procInfoDisk
	if err := json.Unmarshal(data, &d); err != nil {
		return nil
	}
	if d.UUID == "" {
		return nil
	}
	info := procInfoFromDisk(d)
	return &info
}

// findDescendantsByOrigin 扫描 procHistory + 磁盘 entries，返回 OriginUUID==uuid
// 的所有条目（直接后代，不递归）。procHistory 命中的 UUID 不再读磁盘以避免重复。
//
// 磁盘扫描有 ResumeLineageMaxDiskScan 上限，防止累积数万历史 UUID 后退化为
// O(N) 磁盘 + JSON 解析（Dev Notes 第 285 行 MVP 备忘录强调过该风险）。
func findDescendantsByOrigin(uuid string, history *ProcessHistory, stepDataDir string) []vfs.ProcInfo {
	if uuid == "" {
		return nil
	}
	seen := make(map[string]bool)
	var descendants []vfs.ProcInfo

	// procHistory 全表扫描（O(N), N <= 1000 通常 < 5ms）。
	if history != nil {
		for _, entry := range history.List() {
			if entry.OriginUUID == uuid && entry.UUID != uuid {
				descendants = append(descendants, entry)
				seen[entry.UUID] = true
			}
		}
	}

	// 磁盘扫描兜底（procHistory evicted 的旧 fork）。
	if stepDataDir != "" {
		scanned := 0
		for _, baseDir := range AllProjectBaseDirs(stepDataDir) {
			stepsDir := filepath.Join(baseDir, "steps")
			entries, err := os.ReadDir(stepsDir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				if seen[e.Name()] {
					continue
				}
				if scanned >= ResumeLineageMaxDiskScan {
					break
				}
				scanned++
				info := readProcInfoFromDisk(stepDataDir, e.Name())
				if info == nil {
					continue
				}
				if info.OriginUUID == uuid && info.UUID != uuid {
					descendants = append(descendants, *info)
					seen[info.UUID] = true
				}
			}
		}
	}

	return descendants
}

// GetResumeLineage is the kernel entry point for the IPC get_resume_lineage
// method. It delegates to BuildResumeLineage using the kernel's procHistory and
// stepDataDir.
func (k *KernelImpl) GetResumeLineage(uuid string) (*ResumeLineageData, error) {
	return BuildResumeLineage(uuid, k.procHistory, k.dataDir)
}

// ProcHistory exposes the kernel's in-memory procHistory for test injection
// (Story 42.3). External callers (ipc package tests) need to seed lineage
// fixtures without going through the full spawn lifecycle.
func (k *KernelImpl) ProcHistory() *ProcessHistory {
	return k.procHistory
}
