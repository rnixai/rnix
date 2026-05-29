package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"maps"
	"strings"
	"time"

	"github.com/rnixai/rnix/vfs"
)

// 路线 B — 将 MCP server 的工具暴露为 native ToolDef。
//
// MCP 的执行链早已可用（Open /mnt/mcp/<pid>-<server>/tools/<tool> → Write(args)
// → Read(result) 即一次 tools/call），但 buildToolDefs 显式跳过 MCP 设备，所以
// LLM 在系统工具集中看不到这些工具。本文件补上缺失的一步：在工具集组装点为进程
// 的每个 MCP 挂载发一次 tools/list，把返回的工具生成 `mcp__<server>__<tool>` 形式
// 的 native ToolDef，并在 toolMap 注册「清洗后可见名 → 原始名 VFSPath」映射，复用
// 既有的 executeVFSTool default 分支驱动 tools/call。不新增执行/路由/权限代码。

const (
	// mcpToolNameMaxLen bounds the LLM-visible MCP tool name. Anthropic/OpenAI
	// function names must match ^[a-zA-Z0-9_-]{1,64}$; we target the 64 ceiling.
	mcpToolNameMaxLen = 64
	// mcpToolListTimeout caps the per-server tools/list call done at assembly
	// time so a slow/hung server cannot stall spawn or resume.
	mcpToolListTimeout = 10 * time.Second
	// mcpToolMaxResultTokens caps a single MCP tool result to avoid context
	// overflow (mirrors /dev/fs Read). Large payloads (e.g. playwright DOM
	// snapshots) are truncated downstream rather than blowing the window.
	mcpToolMaxResultTokens = 25000
)

// mcpToolListResult is the JSON-RPC result shape of an MCP `tools/list` call.
// inputSchema is optional (some servers / mocks omit it) and tolerated as nil.
type mcpToolListResult struct {
	Tools []struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"inputSchema"`
	} `json:"tools"`
}

// buildMCPToolDefs discovers the tools of every MCP server mounted on proc and
// returns native ToolDefs plus a toolMap fragment that routes the LLM-visible
// `mcp__<server>__<tool>` name back to the original tool's VFS path.
//
// It is purely additive: callers append the returned defs to the process's
// nativeToolDefs after the base/meta defs and merge the map into proc.toolMap.
// Any per-server failure (no live transport, tools/list error, unparseable
// response) is logged and skipped — it must never block spawn/resume, and the
// process keeps its base devices and other servers' tools.
//
// MUST be called WITHOUT holding proc.mu: it snapshots proc.MCPMounts under the
// lock, then does blocking tools/list I/O outside it.
func (k *KernelImpl) buildMCPToolDefs(proc *Process) ([]vfs.ToolDef, map[string]toolMapping) {
	proc.mu.Lock()
	mounts := append([]string(nil), proc.MCPMounts...)
	pid := proc.PID
	proc.mu.Unlock()

	if len(mounts) == 0 {
		return nil, nil
	}

	var defs []vfs.ToolDef
	toolMap := make(map[string]toolMapping)
	seen := make(map[string]bool) // LLM-visible names already used (uniqueness)

	for _, base := range mounts {
		server := mcpServerFromMountPath(base)
		transport := k.mcpTransportForPath(base)
		if transport == nil {
			log.Printf("[mcp-toolgen] pid=%d server=%s: no live transport for mount %s — skipping tool discovery", pid, server, base)
			continue
		}

		result, err := mcpListTools(transport)
		if err != nil {
			log.Printf("[mcp-toolgen] pid=%d server=%s: tools/list failed: %v — skipping (process keeps base devices)", pid, server, err)
			continue
		}

		for _, tool := range result.Tools {
			if tool.Name == "" {
				continue
			}
			visible := sanitizeMCPToolName(fmt.Sprintf("mcp__%s__%s", server, tool.Name), seen)
			params := tool.InputSchema
			if params == nil {
				// A tool with no declared schema still needs a valid object
				// schema so downstream driver conversion (Anthropic/OpenAI/
				// Gemini) accepts it.
				params = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			defs = append(defs, vfs.ToolDef{
				Name:            visible,
				Description:     tool.Description,
				Parameters:      params,
				MaxResultTokens: mcpToolMaxResultTokens,
			})
			// VFSPath uses the ORIGINAL tool name: mcpFile.parseToolName reads
			// the `/tools/<name>` subpath to build the tools/call. The visible
			// (sanitized/prefixed) name and the original name are decoupled —
			// the toolMap entry IS the reverse lookup, no separate table needed.
			toolMap[visible] = toolMapping{
				Type:    "vfs",
				VFSPath: base + "/tools/" + tool.Name,
			}
		}
	}

	if len(defs) == 0 {
		return nil, nil
	}
	return defs, toolMap
}

// attachMCPToolDefs discovers MCP tools and appends them to proc's already-built
// nativeToolDefs / toolMap. It MUST be called AFTER the process's MCP servers are
// mounted (spawn auto-mount) or reattached (resume), because tool discovery needs
// live transports — and the base/meta tool set is assembled earlier, before mounts
// exist. Idempotent for the no-MCP case (returns immediately). No-op on discovery
// failure (logged inside buildMCPToolDefs); never blocks the caller.
func (k *KernelImpl) attachMCPToolDefs(proc *Process) {
	if proc == nil {
		return
	}
	mcpDefs, mcpMap := k.buildMCPToolDefs(proc)
	if len(mcpDefs) == 0 {
		return
	}
	proc.mu.Lock()
	proc.nativeToolDefs = append(proc.nativeToolDefs, mcpDefs...)
	if proc.toolMap == nil {
		proc.toolMap = make(map[string]toolMapping, len(mcpMap))
	}
	maps.Copy(proc.toolMap, mcpMap)
	proc.mu.Unlock()
}

// mcpListTools issues a single tools/list call against the transport and parses
// the result. Timeout-bounded so a hung server cannot stall the caller.
func mcpListTools(transport vfs.MCPTransport) (mcpToolListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpToolListTimeout)
	defer cancel()
	resp, err := transport.Call(ctx, "tools/list", nil)
	if err != nil {
		return mcpToolListResult{}, err
	}
	var parsed mcpToolListResult
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return mcpToolListResult{}, fmt.Errorf("parse tools/list response: %w", err)
	}
	return parsed, nil
}

// sanitizeMCPToolName maps a raw `mcp__server__tool` candidate to a unique,
// API-legal function name: charset restricted to ^[a-zA-Z0-9_-]+$, length
// capped at mcpToolNameMaxLen, and disambiguated against names already in seen.
// A short FNV hash of the raw input is appended when truncation or a collision
// would otherwise lose uniqueness. The chosen name is recorded in seen.
func sanitizeMCPToolName(raw string, seen map[string]bool) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if name == "" {
		name = "mcp_tool"
	}

	suffix := mcpNameHash(raw) // 8 hex chars, stable per raw input

	// Truncate over-long names, preserving uniqueness via the hash suffix.
	if len(name) > mcpToolNameMaxLen {
		keep := max(mcpToolNameMaxLen-1-len(suffix), 1)
		name = name[:keep] + "_" + suffix
	}

	// Disambiguate collisions (distinct raw inputs that sanitized to the same
	// string). Append the hash; if that still collides, fall back to a counter.
	// Every candidate is re-trimmed to stay within mcpToolNameMaxLen — the
	// counter suffix grows with i, so the cap MUST be re-checked each iteration,
	// not just once for the hash form.
	if seen[name] {
		base := name
		if len(base)+1+len(suffix) > mcpToolNameMaxLen {
			base = base[:mcpToolNameMaxLen-1-len(suffix)]
		}
		name = base + "_" + suffix
		for i := 1; seen[name]; i++ {
			ctr := fmt.Sprintf("_%d", i)
			b := base
			if len(b)+len(ctr) > mcpToolNameMaxLen {
				b = b[:mcpToolNameMaxLen-len(ctr)]
			}
			name = b + ctr
		}
	}

	seen[name] = true
	return name
}

// mcpNameHash returns an 8-char hex FNV-1a hash. Non-cryptographic (no gosec
// concern) — used only to make truncated/colliding tool names unique.
func mcpNameHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}
