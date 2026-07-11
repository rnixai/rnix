# Rnix

<div align="center">

**The AI-Era Unix — An Operating System for AI Agents**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE) [![Go Report Card](https://goreportcard.com/badge/github.com/rnixai/rnix)](https://goreportcard.com/report/github.com/rnixai/rnix) [![GitHub Stars](https://img.shields.io/github/stars/rnixai/rnix?style=social)](https://github.com/rnixai/rnix/stargazers)

[Documentation](https://docs.rnix.ai/) | [中文版](README_zh.md) | [Changelog](CHANGELOG.md)

</div>

---

```bash
# You know Unix? You already know Rnix.
rnix -i "Analyze the project structure"
rnix strace 1        # See exactly what your agent did
rnix dashboard       # Multi-pane real-time monitoring
rnix gdb 1           # Breakpoints, stepping, context inspection
```

## Design Philosophy

Rnix brings 50 years of proven OS abstractions directly into the world of AI agents. Not a framework. Not a library. A runtime.

**Everything is a Process** — Every agent is a first-class process with its own PID, state machine, and resource table. Use `ps` to list, `kill` to terminate, `strace` to trace — manage agents the same way you manage system processes. Processes can be paused, resumed, suspended, and restarted from checkpoints.

**Everything is a File** — LLMs, filesystem, shell, memory, web search, LSP code intelligence, MCP tools — all unified as VFS devices, accessed through Open/Read/Write/Close. Adding a new capability is just mounting a new device.

**Observability Built In** — Not a logging layer bolted on after the fact. `strace` shows every syscall, `gdb` provides breakpoints and stepping, `dashboard` offers multi-pane monitoring with process tree, timeline, context heatmap, and LLM conversation replay.

**Orchestration is Process Management** — Supervisor trees auto-restart crashed processes. Compose DAGs declare dependencies between agents. Budget Pools manage token allocation. The Intent system decomposes high-level goals into sub-task DAGs. These are OS-level primitives, not application-layer wrappers.

## Quick Start

```bash
# Install
go install github.com/rnixai/rnix/cmd/rnix@latest

# Initialize configuration
rnix init

# Run your first agent
rnix -i "Analyze the project structure"

# Trace syscalls
rnix strace 1

# Real-time dashboard
rnix dashboard
```

## Core Concepts

| Unix | Rnix | Description |
|------|------|-------------|
| `process` | Agent process | Each run gets a PID (UUID v7), state machine, and resource table |
| `/dev/*` | VFS devices | LLM, filesystem, shell, memory, web, LSP, MCP — unified file interface |
| `strace` | `rnix strace` | See every syscall in real time |
| `kill` | `rnix kill` | Terminate any agent from any terminal |
| `top` | `rnix top` | Real-time process monitor with token and time metrics |
| `gdb` | `rnix gdb` | Interactive debugger: breakpoints, stepping, context inspection |
| `ps` | `rnix ps` | List all process states |
| pipe | `rnix compose` | DAG orchestration for multi-agent collaboration |
| suspend/resume | `rnix suspend/resume` | Suspend processes and resume from checkpoint |

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  CLI & AgentShell                                       │
│  rnix -i / strace / gdb / ps / compose / dashboard      │
├─────────────────────────────────────────────────────────┤
│  Agent + Skill Definitions                              │
│  agent.yaml + instructions.md + SKILL.md                │
├─────────────────────────────────────────────────────────┤
│  Microkernel                                            │
│  Process Model / VFS / Context Mgmt / IPC / 50+ Syscalls│
├─────────────────────────────────────────────────────────┤
│  Driver Layer                                           │
│  /dev/llm/* · /dev/fs · /dev/shell · /dev/memory/*      │
│  /dev/web · /dev/lsp · /dev/tasks · /dev/tty · /dev/cron│
├─────────────────────────────────────────────────────────┤
│  Host OS + Multi-LLM Providers                          │
│  Claude · Gemini · Qwen · Anthropic SDK · OpenAI-compat │
└─────────────────────────────────────────────────────────┘
```

## Highlights

- **7 LLM Drivers** — Claude CLI / Cursor CLI / Qwen CLI / Anthropic SDK / Gemini / OpenAI Official / OpenAI-compatible (Ollama, Groq, DeepSeek, etc.)
- **14 VFS Devices** — LLM, filesystem, shell, memory (commit/recall/profile), web, LSP, tasks, TTY, cron, ProcFS, MCP
- **Agent Memory** — Persistent cross-session knowledge with security scanning and async writeback
- **50+ Syscalls** — Stable ABI covering process, context, VFS, IPC, signal, capability, and supervisor
- **Interactive Debugging** — gdb-style debugger + time-travel event navigation + fork-based continuation (not deterministic replay)
- **Multi-Pane Dashboard** — Process tree, timeline, heatmap, LLM conversation replay, debug mode
- **Token Economy** — Budget pools (compose quota engine + per-process runtime budgets), post-hoc SLA contract evaluation feeding agent reputation scoring, skill synergy matrix
- **Adaptive Security** — Behavioral monitoring, anomaly detection, and auto-suspension

## License

MIT — see [LICENSE](LICENSE).

Full documentation at [docs.rnix.ai](https://docs.rnix.ai/).
