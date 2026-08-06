# agtest Tier1 golden suite

Deterministic agent-behavior regression tests. Each case drives a real rnix
process through the real daemon / spawn / VFS / observability stack — only the
LLM output is deterministic, supplied by the **replay** driver (Story 68.1) from
a scripted `*.responses.yaml` file.

## Layout & naming

```
tests/agtest/
├── README.md                 ← this file
├── providers.example.yaml    ← replay provider declaration to copy into .rnix/
└── tier1/
    ├── NN-slug.yaml          ← one case (or a suite of cases)
    └── scripts/
        └── NN-slug.responses.yaml   ← its paired replay script
```

- Case files live directly under `tier1/`; response scripts live under
  `tier1/scripts/`. This separation is **structural, not stylistic**:
  `agtest.ParseDir` scans `*.yaml` non-recursively, so a script sitting beside a
  case would be mis-parsed as a case. The `scripts/` subdirectory is immune.
- Naming: `NN-slug.yaml` ↔ `scripts/NN-slug.responses.yaml`, `NN` a two-digit
  ordinal (keeps `ParseDir`'s lexical read order stable).
- **Suite files** (a single `NN-slug.yaml` with a top-level `tests:` list of
  several cases) pair each member with its own script via a trailing letter:
  `scripts/NN-slug-a.responses.yaml`, `-b`, … in `tests[]` order (e.g.
  `10-suite-multi-case.yaml` → `10-suite-multi-case-a/-b.responses.yaml`).
- A case's `agent.model` is the script path **relative to the case file's own
  directory** (e.g. `scripts/02-single-tool-echo.responses.yaml`). The agtest
  executor absolutizes it against that directory before spawning, so cases stay
  portable across machines — never write an absolute path.
- **Self-hosting case (Story 49.1, NR-1 evidence):** `13-self-hosting-validation.yaml`
  drives the real `code-analyst` agent (code-analysis skill) with the replay driver
  against a planted fixture `tier1/testdata/self-hosting-demo.go` (3 known defects),
  asserting the report names the file + defect keywords + severity labels and that
  the agent really `Read` the fixture — run via `make agtest` / `rnix agtest
  tests/agtest/tier1/ --tier1`. Live-LLM counterpart (advisory): `tier2/02-self-hosting-live.yaml`.

## Tier1 assertion discipline

Tier1 is deterministic-only. Enforced by `agtest.ValidateTier1`, which the
repository guard test (`agtest/tier1_guard_test.go`) runs over this whole
directory inside `make test` / CI — a non-conforming case cannot land on main.

1. **No `assert.quality`** — LLM-judge assertions are nondeterministic.
2. **At least one output/syscalls assertion** — a case with no assertion (only
   exit-code) has no regression value.
3. **`agent.provider` must be `replay`** — Tier1 must run the deterministic
   driver. The provider instance name is fixed to `replay` by convention.
4. **No absolute/machine-dependent paths** in assertion strings (any value
   starting with `/`, or containing `/home/` or `/tmp/`). PID / timestamp values
   are the author's responsibility (README discipline + review) — the machine
   heuristic is intentionally narrow to avoid false positives.

Additional discipline (not machine-enforced, but required):

- **Assertion values must be stable.** For `output`, assert a fixed substring of
  the script's `Complete` result. For `syscalls`, assert only event *type* names
  (`ReasonStep`, `Spawn`, `Open`, `Read`, `Write`, `Close`, ...) — `EvalSyscalls`
  is a set-membership match and does not inspect args, so never assert a path or
  argument.

## Writing a response script

Each `responses` entry is one scripted LLM turn (see Story 68.1 for the full
schema). Key rules:

- **Every case that ends by calling a tool must have a `Complete` tool_call as
  its last response.** Running out of script mid-flight is a fail-loud error
  (drift detection, not a normal path); a tool-ending script with no terminal
  `Complete` runs to the `max_steps` backstop and the step count becomes
  nondeterministic.
- **Exception — pure-text cases terminate without `Complete`.** A content-only
  response (`content` set, no `tool_calls`) is itself a terminal `text` action:
  the reasoning loop ends on it immediately. So a text-class case
  (`03-text-then-complete`, `06-reasoning-step`) is a **single** content-only
  response with no `Complete`, and any response scripted after it is never
  consumed. Do not add a trailing `Complete` to a text case expecting it to run.
- **Keep `usage` values small (or omit them).** Large token counts trip the
  context Compactor, which invokes a real LLM summary and destroys determinism.
- **Tool commands must be safe, deterministic, dependency-free.** `echo` / `true`
  / `false`-level only. No reading/writing outside the repo, no network, no
  env-dependent output. `tool_call.name` uses the Decision 45 semantic names
  (`Bash`, `Read`, ...) — cases omit `agent.name`, so they run a direct intent
  spawn with the default tool set available.

## Make targets (Story 68.3)

The unattended, isolated-daemon runner — this is what CI and most local runs
should use:

```bash
make agtest        # Tier1 (this directory), isolated daemon, PR gate
make agtest-live   # Tier2 (tests/agtest/tier2/), your ambient daemon, advisory
```

`make agtest` runs `tests/agtest/run-tier1.sh`: it starts a daemon in a
throwaway `mktemp` sandbox (own socket / data dir / global config — never your
real `rnix daemon`), writes this directory's `providers.example.yaml` into
that sandbox's config, runs `rnix agtest tests/agtest/tier1/ --tier1`, and
cleans up on any exit path (pass or fail). Not part of `make all` — it drives
a real spawn/daemon/VFS stack, a different failure class than `go test`. Full
walkthrough (including the failure-to-case workflow below): see
[`docs/eval-loop.md`](../../docs/eval-loop.md).

## Running (manual, for iterating on a single case)

Copy the replay provider declaration into your project config (the `.rnix/`
directory is gitignored, so this step is manual):

```bash
mkdir -p .rnix
# copy the `providers:` entry from tests/agtest/providers.example.yaml
$EDITOR .rnix/providers.yaml
```

Restart the daemon so it picks up the new build / config, then run from the
repository root:

```bash
rnix daemon stop        # CLI auto-restarts a fresh daemon on next command
rnix agtest tests/agtest/tier1/ --dry-run           # parse + validate only
rnix agtest tests/agtest/tier1/ --dry-run --tier1   # + enforce Tier1 discipline
rnix agtest tests/agtest/tier1/                     # execute
```

`timeout` in a case is milliseconds (`30000` = 30s). `--tier1` calls the same
`agtest.ValidateTier1` the `tier1_guard_test.go` repository guard and
`make agtest` both use — handy to check a new/edited case before it ever
reaches CI.

## Importing a failure into a case (Story 68.3)

Turn a real (mis)behaving process into a regression case without hand-writing
the response script:

```bash
rnix ps -a --uuid                  # find the UUID (or its last-6 short id)
rnix agtest import <uuid>          # generates tests/agtest/imported/import-<short>.{yaml,...}
```

Reads `steps.jsonl` / `proc-info.json` / `events.jsonl` directly off disk — no
daemon required. The output is a **skeleton, not a ready case**: assertions
are only commented-out suggestions (no live `assert:` key), so
`agtest.ValidateTier1` refuses it on purpose until a human fills in real
assertions. Review the warnings printed in both generated files' headers, then
move the pair into `tier1/` + `tier1/scripts/`, renaming to the next
`NN-slug` ordinal, and confirm with `make agtest`. `tests/agtest/imported/` is
gitignored — nothing there is ever auto-committed. Full step-by-step: see
[`docs/eval-loop.md`](../../docs/eval-loop.md#失败转用例流程).

## Determinism check (two-run equality)

The suite must produce byte-identical output across runs, modulo duration:

```bash
rnix agtest tests/agtest/tier1/ --json | jq 'del(.data.duration_ms, .data.cases[].duration_ms)' > /tmp/run1.json
rnix agtest tests/agtest/tier1/ --json | jq 'del(.data.duration_ms, .data.cases[].duration_ms)' > /tmp/run2.json
diff /tmp/run1.json /tmp/run2.json    # expect no output
```

Case order, names, statuses, and assertion results must all match; only the
duration fields may differ.

## Maintenance note

The guard test reads these YAML files, which live outside any Go package, so the
`go test` cache does not track their changes. After editing any case or script,
re-run with `-count=1`:

```bash
go test -race -count=1 ./agtest/...
```
