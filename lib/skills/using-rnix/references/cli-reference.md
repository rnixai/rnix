# Rnix CLI Reference

Complete catalog of `rnix` subcommands, grouped by area. Every command here is
verified against the current `cmd/rnix` implementation. Arguments in `<>` are
required, `[]` optional. When a flag isn't listed, run `rnix <command> --help` —
the binary is always the source of truth.

> Run every command with the `Bash` tool. Commands that create work auto-start
> the daemon; passive query/attach commands may require an already-running
> daemon or degrade to an empty/no-daemon view.

## Global flags

These persistent flags work across most commands:

| Flag | Meaning |
|------|---------|
| `--json` | Machine-readable JSON output (use when parsing) |
| `-v`, `--verbose` | Verbose output |
| `-q`, `--quiet` | Quiet output (e.g. `ps -q` prints PIDs only) |
| `-m`, `--model <m>` | LLM model override (e.g. `sonnet`, `opus`, `haiku`) |
| `--provider <p>` | LLM provider override |
| `--fallback-model <m>` | Override the agent's fallback model |
| `--fallback-provider <p>` | Override the agent's fallback provider |

## Spawning agents

The root command spawns one agent from an intent. This is the most common entry
point.

```bash
rnix -i "<intent>"
```

Root-command flags (in addition to the global ones):

| Flag | Meaning |
|------|---------|
| `-i`, `--intent <s>` | The intent to spawn an agent on (required to spawn) |
| `--agent <name>` | Use a named agent definition (e.g. `code-analyst`) |
| `--max-steps <n>` | Max reasoning steps; `0` = unlimited (default `0`) |
| `--dashboard` | Open the dashboard after spawning |

```bash
rnix -i "refactor error handling in main.go"
rnix -i "analyze project structure" --json
rnix -i "review this PR" --agent code-analyst -m opus
```

## Process management

| Command | Purpose |
|---------|---------|
| `rnix ps` | List active processes |
| `rnix ps -a` / `--all` | Include finished (Dead/Zombie) processes |
| `rnix ps --uuid` | Show the UUID column |
| `rnix kill <pid>` | Terminate a process |
| `rnix strace <pid>` | Stream a process's syscalls live (`--verbose`, `--json`) |
| `rnix suspend <pid>` | Suspend a running process |
| `rnix resume <uuid>` | Resume a suspended/exited process, keeping its UUID |
| `rnix resume --fork <uuid>` | Resume into a new UUID (branch/explore) |
| `rnix resume --fork --from-step <n> <uuid>` | Fork from a specific history step |
| `rnix gc` | Garbage-collect old process history (`--dry-run`, `--force`, `--json`) |

`ps` shows PID, state, skills, tokens, and elapsed time. `strace` is the fastest
way to understand what an agent actually did.

There is no separate public wait subcommand in the current CLI. Waiting and
reaping are kernel/compose internals: watch completion with `rnix ps -a` or
`rnix strace <pid>`, and let `compose` dependency conditions wait between DAG
nodes.

## Multi-agent orchestration

Driven by a compose file (default `.rnix/compose.yaml`). See
`references/workflows.md` for a full file example.

| Command | Purpose |
|---------|---------|
| `rnix compose up` | Parse the compose file, resolve the DAG, spawn all agents in order |
| `rnix compose up -f <file>` | Use a specific compose file |
| `rnix compose down` | Stop all agents from the compose orchestration |
| `rnix compose resume` | Resume a compose DAG (see `--help` for node selection) |

## Declarative intent

Give rnix a high-level goal; it decomposes it into a sub-task DAG, you confirm,
it executes.

| Command | Purpose |
|---------|---------|
| `rnix apply "<intent>"` | Declare an intent and auto-decompose into sub-tasks |
| `rnix apply "<intent>" -y` | Skip the confirmation step and start immediately |
| `rnix apply "<intent>" -u <intent-id>` | Incrementally update an existing intent |
| `rnix intent list` | List all intents |
| `rnix intent status [intent-id]` | Show an intent tree's status |

## Skills & agents

| Command | Purpose |
|---------|---------|
| `rnix skill list` | List all installed/discovered skills |
| `rnix skill search <keyword>` | Search the community registry |
| `rnix skill install <name> [name...]` | Install skills from the registry |
| `rnix skill update [name...]` | Update installed skills to the latest version |

Skills are discovered from several scopes including the cross-tool
`.agents/skills/` shared path, so `rnix skill list` reflects skills installed by
other agentskills.io-compatible clients without extra configuration.

## MCP (Model Context Protocol) servers

| Command | Purpose |
|---------|---------|
| `rnix mcp list` | List active MCP server mounts on the daemon |
| `rnix mcp test <name>` | Probe a configured MCP server (connect → tools/list → …) |
| `rnix mcp logs <name>` | Show captured stderr of a mounted MCP server |
| `rnix check mcp` | Verify MCP runtime prerequisites (node, npx, optional Chromium) |

## Observability & debugging

| Command | Purpose |
|---------|---------|
| `rnix top` | Live process monitor with token and time metrics |
| `rnix dashboard` | Multi-pane real-time monitor (`--pid <n>` to focus, `--load <path>` to replay a recording) |
| `rnix gdb <pid>` | Interactive debugger: breakpoints, stepping, context inspection |
| `rnix log <pid>` | Attach to a process's log stream |

## Daemon & runtime

| Command | Purpose |
|---------|---------|
| `rnix daemon status` | Show daemon status |
| `rnix daemon stop` | Stop the running daemon |
| `rnix daemon start` | Start the daemon if not already running |
| `rnix heartbeat status` | Show daemon heartbeat status |
| `rnix serve` | Start the OpenAI-compatible HTTP gateway |

Spawning/execution commands auto-start the daemon. Passive query/attach commands
such as `ps`, `mcp list`, `top`, `log`, `gdb`, `suspend`, and `resume` dial the
current daemon; if it is stopped, start it explicitly with `rnix daemon start`.

## Bootstrap, config & diagnostics

| Command | Purpose |
|---------|---------|
| `rnix init` | Initialize the rnix configuration environment |
| `rnix init --with-mcp-examples` | Also write example MCP server config |
| `rnix config show` | Show the active daemon configuration |
| `rnix doctor` | Diagnose provider/runtime setup (`--probe` runs a live LLM hello; `--provider <p>` limits the check) |
| `rnix version` | Show version and build info |

## AgentShell scripting

| Command | Purpose |
|---------|---------|
| `rnix run <script.ash> [args...]` | Execute an AgentShell script file |

AgentShell scripts also support a shebang: `#!/usr/bin/env -S rnix run`.

## Advanced commands

Rnix exposes additional commands for token economy, adaptive security,
distributed tracing, and lineage. These are specialized; run `rnix <command>
--help` for their exact flags before using them rather than guessing:

- **Token economy** — `rnix reputation`, `rnix synergy list`
- **Adaptive security** — `rnix immune status` / `rnix immune resume`
- **Tracing & replay** — `rnix trace`, `rnix record start|stop|list`, `rnix replay`
- **Lineage & topology** — `rnix lineage`, `rnix topology`
- **Context profiling** — `rnix ctx-profile`, `rnix ctx-growth`
- **Agent testing** — `rnix agtest` (declarative YAML agent tests)
