# Rnix

<div align="center">

**An Operating System for AI Agents — Built with Unix Philosophy**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE) [![Go Report Card](https://goreportcard.com/badge/github.com/rnixai/rnix)](https://goreportcard.com/report/github.com/rnixai/rnix) [![GitHub Stars](https://img.shields.io/github/stars/rnixai/rnix?style=social)](https://github.com/rnixai/rnix/stargazers)

[Documentation](https://docs.rnix.ai/) | [中文版](README_zh.md) | [Changelog](CHANGELOG.md)

</div>

---

```bash
# You know Unix? You already know Rnix.
go install github.com/rnixai/rnix/cmd/rnix@latest
rnix -i "Analyze the project structure"
rnix strace 1   # See exactly what your agent did
rnix top        # Real-time process monitor
```

## The Problem

You've built a complex multi-agent workflow — orchestrations, tool chains, retry logic. It breaks. The error is somewhere in a tangled chain of callbacks, role definitions, and LLM calls. You can't see what happened, can't trace execution, can't kill a runaway agent.

**Rnix gives your agents the same tools you use to debug operating systems.**

## How Rnix Is Different

| | LangChain | CrewAI | AutoGen | **Rnix** |
|---|---|---|---|---|
| Architecture | Library | Library | Library | **Runtime** |
| Agent Model | Callbacks | Roles | Conversations | **Processes (PID)** |
| Resource Access | APIs | APIs | APIs | **Virtual Filesystem** |
| Debugging | LangSmith | Logs | Manual | **rnix strace** |
| Dashboard | No | No | No | **rnix dashboard** |
| Kill Agent | No | No | No | **rnix kill** |
| Language | Python | Python | Python/.NET | **Go** |

## Quick Start

```bash
# Install
go install github.com/rnixai/rnix/cmd/rnix@latest

# Initialize configuration
rnix init

# Run your first agent
rnix -i "Analyze the project structure"
#
# Trace syscalls
rnix strace 1

# Real-time dashboard
rnix dashboard
```

## v0.7 Highlights

- **Dashboard UX** — Multi-pane TUI: process history, LLM conversation viewer, intent tree, distributed tracing
- **Unified Reasoning Loop** — LLM autonomously selects: tool_call / plan / spawn / specialize / complete
- **UUID v7 Processes** — Time-sortable UUIDs for distributed process tracking
- **Native ToolCalls** — VFS devices self-describe capabilities, LLM discovers tools dynamically

## License

MIT — see [LICENSE](LICENSE).

---

If Rnix helps you debug AI agents, give us a star on GitHub!
