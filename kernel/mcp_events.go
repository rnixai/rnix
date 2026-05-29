package kernel

import (
	"errors"
	"strings"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 48.5 §决策 1 — drivers/mcp never imports kernel, so the PID-attributed
// events.jsonl entries (mcp.error / mcp.reconnect) are emitted ONLY here, by the
// kernel, at the VFS tool-call boundary. The transport exposes its state via the
// read-only vfs.MCPTransport surface (Status/ReconnectCount/...) and the
// ErrDeviceDisconnected sentinel; the kernel observes both and emits events.
// This mirrors the 48.2 ErrForceKilled → Unmount forced=true pattern.

const mcpPathPrefix = "/mnt/mcp/"

// recordMCPOpenPath remembers the MCP tool path behind an FD so the subsequent
// Write/Read can attribute health events to a server WITHOUT an fd→path syscall.
// No-op for non-MCP paths (AC6 zero-overhead: non-MCP fds never enter the map).
func (p *Process) recordMCPOpenPath(fd types.FD, path string) {
	if !strings.HasPrefix(path, mcpPathPrefix) {
		return
	}
	p.mu.Lock()
	if p.mcpFDPaths == nil {
		p.mcpFDPaths = make(map[types.FD]string)
	}
	p.mcpFDPaths[fd] = path
	p.mu.Unlock()
}

// mcpPathForFD returns the recorded MCP path for an FD ("" if not an MCP fd).
func (p *Process) mcpPathForFD(fd types.FD) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.mcpFDPaths) == 0 {
		return ""
	}
	return p.mcpFDPaths[fd]
}

// clearMCPOpenPath drops the FD→path entry on Close.
func (p *Process) clearMCPOpenPath(fd types.FD) {
	p.mu.Lock()
	if p.mcpFDPaths != nil {
		delete(p.mcpFDPaths, fd)
	}
	p.mu.Unlock()
}

// observeMCPHealth inspects a completed MCP tool-call boundary op and emits
// state-transition events (Story 48.5 AC1/AC3). Called from vfsWriteWithEvent /
// vfsReadWithEvent after the op when the fd maps to an MCP path. Events fire
// only on transitions (disconnect sentinel; reconnect-count delta) so a healthy
// stream of calls produces zero noise (AC6 / §易错点 13).
func (k *KernelImpl) observeMCPHealth(proc *Process, mcpPath string, opErr error) {
	server, base := mcpServerAndBase(mcpPath)

	// mcp.error — the L1/backoff sentinel surfaced through the VFS error chain.
	if opErr != nil && isDeviceDisconnected(opErr) {
		k.emitEvent(proc, "mcp.error", map[string]any{
			"server": server,
			"reason": opErr.Error(),
		}, nil, nil, 0)
	}

	// mcp.reconnect — emitted once per ReconnectCount delta. Requires the live
	// transport; resolved via the mount table (read-only projection).
	tr := k.mcpTransportForPath(base)
	if tr == nil {
		return
	}
	cur := tr.ReconnectCount()
	proc.mu.Lock()
	if proc.mcpReconnectSeen == nil {
		proc.mcpReconnectSeen = make(map[string]int)
	}
	// Implicit baseline 0 on first observation ([Review][Patch] P7). MCP mounts
	// are per-process (path carries <pid>-<server>) so the transport is always
	// freshly created with ReconnectCount 0 at spawn/reattach, and a reconnect
	// only ever fires inside a Call — never before this process's first tool
	// call. Hence any cur>0 at first observation is a reconnect this process
	// drove, including one triggered by its FIRST call. The previous `seen &&`
	// guard swallowed exactly that first-call-triggered reconnect.
	prev := proc.mcpReconnectSeen[server]
	proc.mcpReconnectSeen[server] = cur
	proc.mu.Unlock()
	if cur > prev {
		k.emitEvent(proc, "mcp.reconnect", map[string]any{
			"server":          server,
			"reconnect_count": cur,
			"attempt":         cur - prev,
		}, nil, nil, 0)
	}
}

// mcpTransportForPath resolves the transport for an MCP mount base path via the
// mount table. Returns nil when no mount manager is wired or no mount matches.
func (k *KernelImpl) mcpTransportForPath(base string) vfs.MCPTransport {
	if k.mountMgr == nil {
		return nil
	}
	for _, m := range k.mountMgr.ListMounts() {
		if m.Path == base {
			return m.Transport()
		}
	}
	return nil
}

// MCPTransportByServer resolves the live transport for a mounted MCP server by
// name (Story 48.5 Task 7.4 — backs `rnix mcp logs <name>`). Server names are
// recovered from the `/mnt/mcp/<pid>-<name>` mount path suffix.
func (k *KernelImpl) MCPTransportByServer(name string) (vfs.MCPTransport, bool) {
	if k.mountMgr == nil {
		return nil, false
	}
	for _, m := range k.mountMgr.ListMounts() {
		if mcpServerFromMountPath(m.Path) == name {
			if tr := m.Transport(); tr != nil {
				return tr, true
			}
		}
	}
	return nil, false
}

// mcpServerAndBase splits a full MCP tool path
// (/mnt/mcp/<pid>-<server>/tools/<name>) into the server name and the mount base
// (/mnt/mcp/<pid>-<server>). Tolerant of non-conforming paths (returns the raw
// leading segment as both base leaf and server).
func mcpServerAndBase(full string) (server, base string) {
	rest := strings.TrimPrefix(full, mcpPathPrefix)
	seg := rest
	if before, _, ok := strings.Cut(rest, "/"); ok {
		seg = before
	}
	base = mcpPathPrefix + seg
	server = serverFromMountLeaf(seg)
	return server, base
}

// mcpServerFromMountPath extracts the server name from a mount base path
// (/mnt/mcp/<pid>-<server>).
func mcpServerFromMountPath(path string) string {
	leaf := strings.TrimPrefix(path, mcpPathPrefix)
	if before, _, ok := strings.Cut(leaf, "/"); ok {
		leaf = before
	}
	return serverFromMountLeaf(leaf)
}

// serverFromMountLeaf strips the `<pid>-` prefix from a mount leaf, returning
// the server name. Falls back to the raw leaf when there is no dash.
func serverFromMountLeaf(leaf string) string {
	if _, after, ok := strings.Cut(leaf, "-"); ok && after != "" {
		return after
	}
	return leaf
}

// isDeviceDisconnected reports whether the error chain carries a
// *types.DriverError with Code == ErrDeviceDisconnected. It walks PAST the first
// DriverError match (errors.As stops at the first) so a vfs layer that wrapped
// the L1 sentinel in an outer ErrServiceUnavailable is still detected.
func isDeviceDisconnected(err error) bool {
	for err != nil {
		var de *types.DriverError
		if !errors.As(err, &de) {
			return false
		}
		if de.Code == types.ErrDeviceDisconnected {
			return true
		}
		err = de.Unwrap()
	}
	return false
}
