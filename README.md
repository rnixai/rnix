# Rnix

**An Operating System for AI Agents — Powered by Unix Philosophy**

[中文版](README_zh.md) | [Documentation](https://rnix.ai/docs) | [GitHub](https://github.com/rnixai/rnix)

---

Rnix brings Unix operating system abstractions to AI agents. Every agent execution is a **process** with its own PID and state machine. Every resource — LLM, filesystem, shell — is a **file** accessed through a virtual filesystem. If you know Unix, you already know Rnix.

## Why Rnix

Most AI agent frameworks are libraries — you import them and hope for the best. Rnix is different: it's a **runtime** with the same battle-tested abstractions that power operating systems.

- **Processes, not callbacks** — Each agent gets a PID, state machine, FD table, and signal handling. Kill a runaway agent with `rnix kill`. Debug one with `rnix gdb`.
- **Files, not APIs** — LLMs, filesystem, shell, and MCP tools are unified as VFS devices. Read from `/dev/llm/claude` like reading from a file.
- **Compose, not code** — Define multi-agent workflows in YAML. Pipe agent outputs. Use AgentShell for scripting.
- **Observe, not guess** — `rnix strace` traces every syscall. `rnix dashboard` gives a real-time TUI. `rnix replay` rewinds execution.

## Quick Start

```bash
# Install
go install github.com/rnixai/rnix/cmd/rnix@latest

# Run your first agent
rnix -i "Analyze the project structure"

# Trace what it actually did
rnix strace 1

# Real-time process monitor
rnix top
```

## v0.7 Highlights

- **Dashboard UX** — Redesigned TUI with multi-pane views: process history, LLM conversation viewer, intent tree, distributed tracing, and evaluation panel.
- **Unified Reasoning Loop** — Single loop where the LLM autonomously selects actions (tool_call / plan / spawn / specialize / complete). No more dual-loop confusion.
- **UUID v7 Processes** — Processes now use time-sortable UUIDs, enabling distributed process tracking across machines.
- **Native ToolCalls** — VFS devices self-describe their capabilities. The LLM discovers tools dynamically.

## Architecture

```
CLI → Unix Socket → Daemon (Kernel + Process Table)
                         │
                    ReasonStep Loop ←→ VFS Devices
                    (LLM, FS, Shell, MCP)
```

## Documentation

Full documentation — CLI reference, configuration, agent/skill model, VFS device paths, and development guide — lives at **[rnix.ai/docs](https://rnix.ai/docs)**.

## License

MIT — see [LICENSE](LICENSE).
