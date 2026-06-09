# Rnix Workflows

Worked, end-to-end recipes for driving rnix. Run every command with the `Bash`
tool. Each recipe is self-contained — copy it, swap in your own intents, and go.

## Where configuration lives

`rnix init` bootstraps the configuration. After that:

- **Global config** lives under `~/.config/rnix/` (e.g. `providers.yaml` for LLM
  providers, the daemon `config.yaml`).
- **Project config** lives in a `.rnix/` directory at the project root — compose
  files (`.rnix/compose.yaml`), and project-scoped agents/skills under
  `.rnix/agents/` and `.rnix/skills/`.

When in doubt about an exact filename or path, run `rnix init` (it prints what it
writes) or `rnix config show` — don't assume a legacy name.

## Recipe 1 — Spawn an agent, watch it, resume it

The bread-and-butter loop.

```bash
# Spawn. Prints the new PID; the daemon auto-starts if it wasn't running.
rnix -i "analyze ./README.md and summarize its structure"

# Watch the whole table (-a includes finished processes).
rnix ps -a

# Trace what a specific agent is doing, syscall by syscall.
rnix strace <pid>

# Need it to keep going after it finished? Find its UUID, then resume.
rnix ps -a --uuid
rnix resume <uuid>          # continue under the same UUID
rnix resume --fork <uuid>   # or branch into a new UUID, leaving the original intact
```

Tip: add `--agent <name>` to spawn a named agent definition, and `-m <model>` /
`--provider <p>` to override the model or provider for that run.

## Recipe 2 — Orchestrate multiple agents with compose

Use compose when several agents must run with dependencies between them. Define
them in `.rnix/compose.yaml`:

```yaml
version: "1.0"
intent: "PR review + analysis + documentation"
agents:
  reviewer:
    intent: "review PR changes"
    agent: "pr-reviewer"      # optional: use a named agent definition
    model: "opus"             # optional: per-agent model override
    skills: [pr-reviewer]
  analyst:
    intent: "analyze code quality"
    model: "haiku"
    skills: [code-analyst]
    depends_on:
      reviewer: completed     # wait until 'reviewer' has completed
  writer:
    intent: "write change documentation"
    skills: [doc-writer]
    depends_on:
      reviewer: completed
      analyst: completed
```

`depends_on` is a map of `<upstream-agent>: <condition>` (e.g. `completed`).
rnix resolves the DAG and runs independent agents in parallel.

```bash
rnix compose up                 # spawn all agents in dependency order
rnix compose up -f my-flow.yaml  # use a non-default compose file
rnix ps -a                       # watch them progress
rnix compose down                # stop all agents from the orchestration
```

Optional per-agent fields include `provider`, `priority` (`high`/`normal`/`low`),
`max_tokens`, `timeout_ms`, `candidates` (for auto-selection), and `sla`. The
top-level spec also accepts `model`, `provider`, and `token_budget`.

## Recipe 3 — Decompose a high-level goal with intent

When you have a broad goal and want rnix to break it into sub-tasks itself, use
the intent system. The method is **decompose → confirm → execute**.

```bash
# Declare an intent. rnix decomposes it into a sub-task DAG and (by default)
# waits for your confirmation before executing.
rnix apply "build a REST API for a todo app with tests"

# Skip the confirmation gate and start executing immediately:
rnix apply "build a REST API for a todo app with tests" -y

# Inspect progress.
rnix intent list
rnix intent status <intent-id>

# Add new requirements to an in-flight intent (incremental update):
rnix apply "also add OpenAPI docs" -u <intent-id>
```

The reconciler executes the DAG with retry, timeout, and drift detection;
failed nodes cascade to their dependents. You don't script the steps — you state
the goal and let rnix plan it.

## Recipe 4 — Suspend, resume, and fork for long or risky runs

```bash
# Pause a running agent (frees it from the active scheduler; state is preserved).
rnix suspend <pid>

# Resume it later, exactly where it left off.
rnix resume <uuid>

# Explore a branch from a finished run without disturbing the original:
rnix resume --fork <uuid>

# Retry from a specific point in history (truncated fork):
rnix resume --fork --from-step <n> <uuid>
```

Because rnix treats Dead as a frozen state (history persists under
`.rnix/data/steps/<uuid>/`), you can revive crashed or completed agents until gc
reclaims them. See `references/architecture.md` for the resume design and gc
policy.

## Choosing the right tool

- **One task, one agent** → `rnix -i "..."`.
- **Several agents with dependencies you define** → `rnix compose up`.
- **A goal you want rnix to break down for you** → `rnix apply "..."`.
- **Continue / branch a previous run** → `rnix resume [--fork] <uuid>`.
