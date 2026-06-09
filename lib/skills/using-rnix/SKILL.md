---
name: using-rnix
description: >-
  Drive Rnix — the Agent OS — from the command line: spawn AI agents as OS-style
  processes, orchestrate multi-agent workflows, decompose high-level intents into
  sub-task DAGs, and inspect, trace, suspend or resume agent processes. Use this
  whenever you need to run the `rnix` CLI, launch or manage agents, compose several
  agents together, apply a declarative intent, query/debug/resume a running agent,
  or use rnix as the runtime that executes other agents — even if the request only
  says "spawn an agent", "orchestrate agents", "run rnix", or names a `rnix`
  subcommand. Gives you the rnix capability map and exact, verified CLI usage so
  you produce correct commands without reading rnix's source.
allowed-tools: /dev/shell
metadata:
  author: rnix
  version: "1.0"
---

# Using Rnix

Rnix is **an operating system for AI agents** — "the AI-Era Unix". You drive it
with a single CLI, `rnix`, the way you drive Unix with `ps`, `kill`, and
`strace`. This skill gives you rnix's capability map and the exact commands to
run, so you can accomplish a task with rnix without reading its source.

If you already know Unix, you already know most of rnix. The novelty is *what*
the processes are: each one is an AI agent reasoning in a loop.

## Mental model

Three ideas explain almost everything:

- **Everything is a Process.** Every agent run is a first-class process with a
  PID, a state machine (Created → Running → Zombie → Dead), and a resource table.
  You list them with `ps`, end them with `kill`, watch them with `strace` — the
  same verbs you already know. Processes can be suspended, resumed, and forked
  from checkpoints.
- **Everything is a File.** LLMs, the filesystem, the shell, agent memory, web
  search, code intelligence (LSP), and MCP tools are all exposed as virtual
  devices behind a uniform file interface. Adding a capability is mounting a
  device. (The concrete device catalog lives in `references/architecture.md` —
  you rarely need it just to *drive* rnix.)
- **Orchestration is process management.** Multi-agent workflows are DAGs of
  processes (`compose`); high-level goals are decomposed into sub-task DAGs
  (`intent`/`apply`); supervisor trees restart crashed agents. These are
  OS primitives, not application glue.

**Daemon model — read this once.** A background daemon holds the kernel and the
process table; the CLI talks to it over a Unix domain socket. You do **not**
start the daemon yourself: any `rnix` command that needs it auto-starts it on
first use. Because the daemon is shared, a process you spawn in one terminal is
visible to `rnix ps` in another. Manage the daemon explicitly only when you need
to: `rnix daemon status` / `rnix daemon stop`.

## How to drive rnix

Run every `rnix` command with the **`Bash`** tool — rnix is a normal CLI binary.
For example, use `Bash` to run `rnix -i "analyze ./README.md"`.

Two habits keep you accurate:

- **When unsure of a flag, ask the binary, not your memory.** `rnix --help` lists
  all commands; `rnix <command> --help` shows a command's flags and examples.
  This skill lists the verified, common surface; the binary is the final word.
- **Prefer `--json` when you need to parse output.** Most commands accept it and
  emit a stable, scriptable shape instead of the human table.

Global flags that work on most commands: `--json`, `-v/--verbose`, `-q/--quiet`,
`-m/--model`, `--provider`.

## Capability map

Each capability below is summarized here and expanded in a reference file. Read
the reference only when the task needs that depth.

- **Process lifecycle** — spawn an agent from an intent, list/inspect, kill,
  suspend, and resume. Resume is unusually powerful: in rnix, **Dead is a frozen
  state, not the end** — a finished or crashed agent keeps its full history on
  disk and can be revived. See *Core commands* below and `references/workflows.md`.
- **VFS device model** — the uniform file interface over LLMs, filesystem, shell,
  memory, web, LSP, and MCP. You generally don't touch devices directly when
  driving rnix from the CLI; agents use them internally. Catalog and paths:
  `references/architecture.md`.
- **Multi-agent orchestration (compose)** — declare a set of agents and their
  dependencies in a YAML file; rnix runs them in DAG order with token budgets and
  optional SLAs. See `references/workflows.md`.
- **Declarative intent** — give rnix a high-level goal; it decomposes the goal
  into a sub-task DAG, you confirm, and it executes with retry and drift
  detection. The method is *decompose → confirm → execute*, not a fixed script.
  See `references/workflows.md`.
- **Skills & agents** — agents are defined by a manifest plus instructions;
  skills are reusable capability bundles (a `SKILL.md` like this one). rnix
  discovers skills from several scopes, including the cross-tool `.agents/skills/`
  shared path. Manage them with `rnix skill ...`.
- **Observability** — `strace` streams an agent's syscalls live; `gdb` gives
  breakpoints and stepping; `dashboard` is a multi-pane monitor; `top` is a live
  process monitor. This is built in, not bolted on.
- **IPC (for programmatic drivers)** — if you're driving rnix from code rather
  than a shell, the daemon speaks NDJSON over its Unix socket. Protocol shape and
  methods: `references/architecture.md`.

## Core commands

These are verified against the current rnix CLI. Arguments in `<>` are required,
`[]` optional.

| Goal | Command |
|------|---------|
| Spawn an agent from an intent | `rnix -i "<intent>"` (optional `--agent <name>`, `-m <model>`, `--provider <p>`, `--max-steps <n>`) |
| List processes | `rnix ps` (`-a/--all` includes finished; `--uuid` shows UUIDs) |
| Trace an agent's syscalls live | `rnix strace <pid>` |
| Stop an agent | `rnix kill <pid>` |
| Suspend / resume | `rnix suspend <pid>` · `rnix resume <uuid>` |
| Fork from history (explore) | `rnix resume --fork <uuid>` |
| Apply a high-level intent | `rnix apply "<intent>"` (add `-y` to skip the confirm step) |
| Inspect an intent | `rnix intent status [id]` · `rnix intent list` |
| Run a multi-agent workflow | `rnix compose up` · `rnix compose down` |
| Manage skills | `rnix skill list` · `rnix skill search <kw>` · `rnix skill install <name>` |
| Live monitors / debug | `rnix top` · `rnix dashboard` · `rnix gdb <pid>` |
| Bootstrap / diagnose config | `rnix init` · `rnix doctor` · `rnix config show` |
| Daemon control | `rnix daemon status` · `rnix daemon stop` |

`-i/--intent` on the root command is the primary way to spawn one agent. Spawning
auto-starts the daemon, prints the new PID, and streams the agent's progress.

Full subcommand catalog, every flag, and more examples: `references/cli-reference.md`.

## End-to-end example: spawn, watch, resume

A complete, runnable flow for "have an agent analyze a file, then inspect it":

```bash
# 1. Spawn an agent on an intent. Prints a PID; the daemon starts if needed.
rnix -i "analyze ./README.md and summarize its structure"

# 2. See it running (and everything else). -a also shows finished processes.
rnix ps -a

# 3. Watch what the agent is actually doing, syscall by syscall.
rnix strace 1            # use the PID from step 2

# 4. If it finished (Dead) but you want it to keep going, resume by UUID.
rnix ps -a --uuid        # find the UUID
rnix resume <uuid>       # continue with the same UUID
#   or branch off without touching the original:
rnix resume --fork <uuid>
```

Run each command with `Bash`. For multi-agent and intent-driven flows, see
`references/workflows.md`.

## Going deeper

Load a reference file only when the task calls for it:

- `references/cli-reference.md` — every subcommand and flag, grouped by area,
  with examples. Start here when you need a command not in *Core commands*.
- `references/architecture.md` — the VFS device model and its device paths, the
  daemon and its socket locations, the IPC NDJSON protocol, the process state
  machine, and the resume design ("Dead is frozen"). Read this to drive rnix
  *programmatically* or to reason about its internals.
- `references/workflows.md` — worked end-to-end recipes: compose DAGs, declarative
  intent, suspend/resume/fork, and multi-agent patterns.

## Keeping this skill current

This skill is rnix's **outward-facing capability contract**: other applications
and agents load it to learn how to drive rnix. If it drifts from the real CLI,
it will make those agents emit commands that fail. So treat it as part of the
public interface:

> Whenever rnix adds or changes an outward-facing CLI command, flag, IPC method,
> or core capability, update this skill in the same change. Put command/flag
> updates in `references/cli-reference.md`, capability or architecture changes in
> `references/architecture.md` or this body, and new recipes in
> `references/workflows.md`. Verify command syntax against `cmd/rnix` (or
> `rnix <cmd> --help`) — never document a command from memory.
