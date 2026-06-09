# Rnix Architecture (for driving rnix programmatically)

This file documents rnix's **internals** — the VFS device model, the daemon and
its socket, the IPC wire protocol, the process state machine, and the resume
design. Read it when you need to drive rnix from code (not just a shell) or to
reason about how rnix works under the hood.

> **A note on device paths.** This document describes rnix's architecture, so
> rnix's device paths (`/dev/llm`, `/dev/fs`, `/mnt/mcp/...`, etc.) appear below
> as **the subject being documented** — exactly as an OS manual lists its device
> nodes. This is *not* an instruction to call those paths as tools. To drive
> rnix you always use the `Bash` tool to run `rnix` CLI commands; the device
> paths are what rnix's own agents use internally.

## VFS device model — "Everything is a File"

Rnix exposes every capability behind a uniform file interface (Open / Read /
Write / Close). An agent reads an LLM completion the same way it reads a file.
Adding a capability means mounting a new device. The current device catalog:

| Device path | Capability |
|-------------|------------|
| `/dev/llm/<provider>` | LLM completion (e.g. `/dev/llm/claude`) |
| `/dev/fs` | Sandboxed host filesystem |
| `/dev/shell` | Subprocess / shell execution |
| `/dev/memory/commit`, `/dev/memory/recall`, `/dev/memory/profile` | Persistent agent memory |
| `/dev/web` | Web search |
| `/dev/lsp` | LSP code intelligence |
| `/dev/tasks` | Task/subagent management |
| `/dev/tty` | Interactive terminal I/O |
| `/dev/cron` | Scheduled wake-ups |
| `/proc/<pid>/` | Dynamic per-process information |
| `/mnt/mcp/<pid>-<server>/tools/<tool>` | MCP server tools (Model Context Protocol) |

Each process has its own file-descriptor table. A skill's `allowed-tools`
frontmatter lists the device paths it may use. An agent's permitted devices are
the deduped union of all loaded skill `allowed-tools`; spawn intersects that
union only when a parent process passes an inherited `AllowedDevices` constraint.

## Daemon model

A single background daemon holds the kernel and the process table. The CLI is a
thin client that talks to it over a Unix domain socket. Key facts:

- **Socket location** (first that applies): `$XDG_RUNTIME_DIR/rnix/rnix.sock`,
  else `/tmp/rnix-$UID/rnix.sock`. A `rnix.pid` file sits beside the socket.
- **Auto-start**: commands that create work (`rnix -i`, `rnix apply`,
  `rnix compose up`, `rnix run`) call `EnsureDaemon` and start the daemon on
  first use. Passive query, attach, and some lifecycle commands dial the current
  daemon only; if it is stopped they may show an empty view or "no active
  daemon". Use `rnix daemon start` when you need an explicit running daemon.
- **Shared state**: because one daemon serves all terminals, a process spawned
  in one shell is visible to `rnix ps` in another.
- **Lifecycle**: `rnix daemon status` / `rnix daemon stop`.

## IPC protocol (NDJSON over the Unix socket)

If you drive rnix from code instead of a shell, speak its wire protocol directly.
It is newline-delimited JSON (NDJSON):

```jsonc
// Request
{"method": "spawn", "payload": { /* method-specific */ }}
// Response
{"ok": true, "payload": { /* method-specific */ }}
// On failure
{"ok": false, "error": {"code": "...", "message": "..."}}
```

Spawn additionally streams progress events before the final response.
Representative methods (run against the daemon socket):

- Process: `spawn`, `list_procs`, `kill`, `spawn_pipeline`, `exec_script`
- Debug: `attach_debug`, `attach_log`, `attach_gdb`, `gdb_command`, `record_start`,
  `record_stop`, `replay_load`, `fork_continue`
- Intent: `apply_intent`, `intent_status`, `intent_confirm`,
  `apply_incremental_intent`, `intent_list`
- Token/security/telemetry: `budget_status`, `sla_status`, `reputation_status`,
  `synergy_list`, `immune_status`, `immune_resume`, `lineage`, `topology_query`
- Misc: `ping`, `shutdown`, `provider_status`, `ctx_profile`, `ctx_growth`

For most tasks you should drive rnix through the CLI (run commands with `Bash`),
which wraps this protocol for you. Reach for raw IPC only when embedding rnix in
another program.

## Process state machine

```
Created ──▶ Running ──▶ Zombie ──▶ Dead
               │
               └────▶ Suspended
```

- **Created → Running**: the reasoning loop (`reasonStep`) starts.
- **Running → Suspended**: the user pauses the process, daemon shutdown
  preserves it, or the kernel deliberately checkpoints it for later recovery.
- **Suspended → Running**: `rnix resume <uuid>` restores from checkpoint/history
  into a runnable process.
- **Running → Zombie**: the agent completes, errors, times out, or is killed.
- **Zombie → Dead**: the process is reaped; its observation data is flushed to
  disk.
- **Suspended → Dead**: killing a suspended process reaps it without resuming.

Each agent runs a single reasoning loop in which the LLM autonomously picks an
action per step (tool call, plan, spawn a child, complete, etc.).

## Resume design — "Dead is frozen, not the end"

Rnix departs from the Unix default that Dead is terminal. A finished or crashed
agent keeps its complete history on disk under
`.rnix/data/steps/<uuid>/` — `steps.jsonl`, `events.jsonl`, `ctx-profile.json`,
`process-meta.json`, and a periodic `checkpoint.json`. Until garbage collection
removes that directory, the process can be revived.

| Mode | Command | UUID behavior |
|------|---------|---------------|
| Continue | `rnix resume <uuid>` | Keeps the original UUID |
| Fork (explore) | `rnix resume --fork <uuid>` | New UUID, links back via `origin_uuid` |
| Truncated fork | `rnix resume --fork --from-step <n> <uuid>` | New UUID, replays history up to step n |

Dead/Zombie resume is "spawn a fresh process from history"; Suspended resume
restores from the checkpoint path when one exists, otherwise from history.
Suspended/Running processes are exempt from gc; Dead/Zombie ones are collected
per `gc.retention_days` and `gc.max_entries` (see `rnix gc`).

## LLM providers

Rnix supports multiple LLM driver types — CLI drivers (Claude CLI, Cursor CLI,
Qwen CLI), native SDKs (Anthropic, Gemini, OpenAI), and any OpenAI-compatible
HTTP endpoint (Ollama, Groq, DeepSeek, …). Providers are declared in
`providers.yaml` (generated by `rnix init`); a provider named `foo` is reachable
as the device `/dev/llm/foo`. Select one per run with `--provider` / `-m`, or set
a project default in the config. `rnix doctor` validates provider setup.
