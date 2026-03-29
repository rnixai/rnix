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

## Core Concepts

| Unix | Rnix | Description |
|------|------|-------------|
| `process` | Agent process | Every agent run gets a PID, state machine, and resource table |
| `/dev/*` | VFS devices | LLM, filesystem, shell, MCP — all accessed as files via Open/Read/Write |
| `strace` | `rnix strace` | See every syscall your agent makes, in real time |
| `kill` | `rnix kill` | Terminate any agent from any terminal |
| `top` | `rnix top` | Real-time process monitor with token and time metrics |
| `gdb` | `rnix gdb` | Interactive debugger: breakpoints, single-step, inspect context |
| `init` | `rnix init` | Bootstrap configuration for providers, agents, and skills |

## License

MIT — see [LICENSE](LICENSE).

---

If Rnix helps you debug AI agents, give us a star on GitHub!
