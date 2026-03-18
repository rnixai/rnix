# Rnix

**An Operating System for AI Agents — Powered by Unix Philosophy**

[中文版](README_zh.md) | [Documentation](https://rnix.ai/docs) | [GitHub](https://github.com/rnixai/rnix)

---

Rnix is a runtime that brings Unix operating system abstractions to AI agents. Every agent execution is a **process** with its own PID and state machine. Every resource — LLM, filesystem, shell — is a **file** accessed through a virtual filesystem. Every interaction with the kernel is a **syscall**. If you know Unix, you already know Rnix.

## Key Features

- **Everything is a Process** — Each agent execution gets an independent PID, state machine, FD table, threads, and coroutines. IPC messaging, pipes, signals, and process groups for multi-agent collaboration.

- **Everything is a File** — LLMs, filesystem, shell, and MCP tools are unified as VFS devices. Multi-provider LLM support with `rnix serve` OpenAI-compatible gateway.

- **Autonomous Agents** — Unified reasoning loop with LLM-driven action selection (tool_call/plan/spawn/specialize/complete). Stem cell differentiation auto-specializes generic agents. Declarative intent system with reconciler for goal-driven execution.

- **Deep Debugging Toolkit** — strace, GDB-style interactive debugger (attach/breakpoint/step/inspect), time-travel replay with fork-continue, distributed causal tracing, visual TUI dashboard, and agtest regression testing.

- **Compose & AgentShell** — DAG orchestration via YAML with budget pools and SLA contracts. Full scripting language: pipes, variables, if/else, loops, functions, parallel blocks, and source imports.

- **Token Economy & Security** — Budget pools with priority allocation, contract SLA evaluation, agent reputation system, and Skill synergy emergence. Adaptive immune security with anomaly detection and self-healing.

## Architecture

```
CLI (cmd/rnix) → IPC Client → Unix Socket → IPC Server → Kernel
                                                            │
                                              Process ReasonStep Loop
                                                  │           ▲
                                             VFS Devices    Context
                                            (LLM, FS, Shell, MCP)
```

Rnix runs as a background **daemon** holding the kernel and process table. CLI commands communicate over a Unix domain socket. The daemon auto-starts on first use and auto-exits after 60s idle.

## Installation

### Prerequisites

- **Go 1.26+** — [Download](https://go.dev/dl/)
- **At least one LLM provider** — Claude Code CLI (`npm install -g @anthropic-ai/claude-code`), Cursor CLI, or any OpenAI-compatible API (Ollama, Groq, DeepSeek) configured in `~/.config/rnix/providers.yaml`

### Install

```bash
go install github.com/rnixai/rnix/cmd/rnix@latest
```

Verify:

```bash
$ rnix version
rnix v0.1.0
commit:  cd9c568
built:   2026-03-15T07:23:57Z
```

### Build from Source

```bash
git clone https://github.com/rnixai/rnix.git
cd rnix
make build    # → ./rnix
make all      # lint + vet + test + build
```

## Quick Start

### Run your first agent

```bash
$ rnix -i "Analyze ./README.md"
[kernel] spawning PID 1 (claude/haiku)...
[agent/1] reasoning step 1...
[agent/1] reasoning step 2...
══ Result ══════════════════════════════════════════════════════════════════════
  ## README.md Analysis
  ...
════════════════════════════════════════════════════════════════════════════════
[kernel] PID 1 exited(0) | claude/haiku | tokens: 1024 | elapsed: 5.3s
```

### Use a named Agent

```bash
$ rnix -i "Analyze ./cmd/rnix/main.go" --agent=code-analyst
```

### Trace syscalls

```bash
# Terminal A: run an agent
$ rnix -i "Analyze the project structure"

# Terminal B: attach strace to PID 1
$ rnix strace 1
[strace] attached to PID 1 (state: running)
[  0.013s] Open(path="/dev/llm/claude") → 3    1ms
[  0.014s] Write(fd=3, size=1234) → <nil>    5.20s  ← LLM call
[  5.214s] Read(fd=3, length=1048576) → 892    2ms
...
```

### Manage processes

```bash
$ rnix ps                    # list all processes
$ rnix kill 1                # send SIGTERM to PID 1
$ rnix top                   # real-time TUI process monitor
$ rnix log 1                 # view reasoning logs
$ rnix daemon status         # check daemon status
$ rnix daemon stop           # stop daemon
```

### Interactive debugging (gdb)

```bash
$ rnix gdb 1
(rnix-gdb) break syscall Write
(rnix-gdb) continue
(rnix-gdb) inspect context
(rnix-gdb) step reason
(rnix-gdb) detach
```

### Multi-agent orchestration

```bash
$ rnix compose up            # run rnix-compose.yaml workflow
$ rnix -i 'spawn "Analyze" | spawn "Generate docs"'  # pipe syntax
$ rnix intent apply "Refactor auth to JWT"            # declarative intent
```

### LLM serve gateway

```bash
$ rnix serve                 # OpenAI-compatible API on localhost:8080
$ curl localhost:8080/v1/chat/completions -d '{"model":"claude","messages":[...]}'
```

## CLI Reference

| Command | Description |
|---------|-------------|
| `rnix -i "intent"` | Spawn an agent process with the given intent |
| `rnix -i "intent" --agent=NAME` | Use a named agent definition |
| `rnix -i "intent" --provider=NAME` | Use a specific LLM provider |
| `rnix init` | Initialize configuration environment |
| `rnix ps` | List all processes |
| `rnix kill <pid>` | Terminate a process |
| `rnix strace <pid>` | Trace syscalls of a process |
| `rnix log <pid>` | View categorized reasoning logs |
| `rnix top` | Real-time process monitor (TUI) |
| `rnix gdb <pid>` | Interactive debugger (breakpoints, stepping, inspection) |
| `rnix dashboard` | Visual debugging TUI (multi-pane) |
| `rnix record start/stop/list` | Execution recording management |
| `rnix replay <id>` | Replay recorded execution |
| `rnix trace <id>` | View distributed tracing |
| `rnix trace blame <id>` | Root cause analysis |
| `rnix ctx-profile <pid>` | Context memory profiling |
| `rnix ctx-growth <pid>` | Context growth prediction |
| `rnix compose up/down` | Multi-agent DAG workflow |
| `rnix apply "intent"` | Declarative intent decomposition |
| `rnix intent status/list` | Intent tree management |
| `rnix run <script.ash>` | Execute AgentShell script |
| `rnix serve` | OpenAI-compatible LLM gateway |
| `rnix skill install/search/update/list` | Skill package management |
| `rnix agtest <file>` | Run agent regression tests |
| `rnix reputation [agent]` | Agent reputation scores |
| `rnix lineage <pid>` | Stem cell differentiation path |
| `rnix topology` | Agent collaboration topology |
| `rnix synergy list` | Effective Skill combinations |
| `rnix immune status/resume/similarity` | Adaptive security management |
| `rnix daemon status/stop` | Daemon management |
| `rnix version` | Show version info |

**Global flags:** `--json`, `--verbose` (`-v`), `--quiet` (`-q`)

## VFS Device Paths

| Path | Purpose |
|------|---------|
| `/dev/llm/<provider>` | LLM inference (claude, cursor, ollama, groq, deepseek, ...) |
| `/dev/fs` | Host filesystem access |
| `/dev/shell` | Shell command execution |
| `/dev/mcp/*` | MCP tool servers (auto-mounted per process) |
| `/proc/{pid}/status` | Process status (JSON) |
| `/proc/{pid}/intent` | Process intent (plain text) |
| `/mnt/mcp/{pid}-{server}` | MCP mount points (auto-lifecycle) |

## Project Structure

```
cmd/rnix/          ← CLI entry point (Cobra)
├── kernel/        ← Microkernel: process table, spawn, kill, wait, reaper
│   ├── vfs/       ← VFS file abstraction, device registry, FD table
│   ├── context/   ← Per-process conversation history
│   └── debug/     ← Strace, recording, distributed tracing
├── drivers/       ← VFS device implementations
│   ├── llm/       ← /dev/llm/* (claude, cursor, ollama, groq, ...)
│   ├── fs/        ← /dev/fs — sandboxed host filesystem
│   ├── shell/     ← /dev/shell — subprocess execution
│   └── mcp/       ← /dev/mcp/* — MCP server transport
├── ipc/           ← Client/Server, NDJSON over Unix socket
├── intent/        ← Declarative intent decomposition & reconciliation
├── compose/       ← DAG-based multi-agent orchestration
├── shell/         ← AgentShell scripting language
├── agents/        ← Agent loader (lib/agents/)
├── skills/        ← Skill loader (lib/skills/)
├── skillpkg/      ← Skill package management
└── internal/      ← Shared utilities (types, xsync, ui)
```

## Configuration

| File | Purpose |
|------|---------|
| `rnix-providers.yaml` | LLM provider definitions (driver, model, API key) |
| `rnix-init.yaml` | Bootstrap services and supervisor trees |
| `rnix-compose.yaml` | Multi-agent workflow DAGs |
| `lib/agents/*/agent.yaml` | Agent manifests |
| `lib/skills/*/SKILL.md` | Skill definitions (YAML frontmatter + Markdown) |

## Agent & Skill Model

**Agent** = "Who I am" — identity, model preferences, skill references

```yaml
# lib/agents/code-analyst/agent.yaml
name: code-analyst
description: "Code quality analysis agent"
models:
  provider: claude
  preferred: sonnet
context_budget: 8192
skills:
  - code-analysis
```

**Skill** = "How to do X" — procedural knowledge, tool permissions

```markdown
# lib/skills/code-analysis/SKILL.md
---
name: code-analysis
description: Analyze code quality and identify issues.
allowed-tools: /dev/fs /dev/shell
---
# Code Analysis
## Workflow
1. Read source files via /dev/fs
2. Run linters via /dev/shell
3. Generate report
```

## Development

```bash
make build          # Build binary → ./rnix
make test           # Run all tests with race detection
make lint           # golangci-lint
make vet            # go vet
make all            # lint + vet + test + build
```

Run a single test:

```bash
go test -race -run TestFunctionName ./package/...
```

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License — see [LICENSE](LICENSE) for details.
