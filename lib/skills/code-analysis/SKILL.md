---
name: code-analysis
description: >
  Analyze code quality, identify bugs, performance issues and security
  vulnerabilities. Use when the user wants to review code files or
  find problems in source code.
allowed-tools: Read Write Edit Glob Grep Bash
metadata:
  author: rnix
  version: "1.0"
---

# Code Analysis

## When to use this skill

Use this skill when the user asks to analyze, review, or audit source code for quality issues, bugs, or security vulnerabilities.

## How to analyze code

1. Use `Read` to load the target file(s)
2. Examine code structure, naming conventions, error handling patterns
3. Use `Grep` to search for code patterns, and `Bash` to run static analysis tools if available
4. If needed, use `Read` to load related files (imports, tests, configs) for context

## Tool usage guide

### Read / Grep / Glob — Source inspection

- `Read` — load target files to get full source code; load related import or config files for context; load test files to evaluate test coverage
- `Grep` — search for specific code patterns across files (prefer this over running `grep` through `Bash`)
- `Glob` — enumerate related files by pattern (e.g. `*.go`); list a directory with `pattern="*"`

### Bash — Shell command execution

Use for auxiliary analysis commands that have no dedicated tool:
- `wc -l` to count file lines
- `find . -name "*.go" -type f` to locate related files when a glob is insufficient

⚠️ **Security constraint**: Always append `| head -N` to limit `Bash` command output and avoid consuming context budget with large outputs.

## Workflow

1. **Read target files** — Use `Read` to load user-specified files
2. **Understand context** — Analyze import dependencies, type definitions, interface contracts
3. **Analyze per dimension** — Systematically check across all analysis dimensions
4. **Summarize findings** — Organize all findings by severity level
5. **Output report** — Output analysis results in structured format

## Severity levels

- **Critical** — Must fix immediately. Includes: security vulnerabilities, data corruption risks, program crashes
- **Warning** — Should fix. Includes: potential bugs, performance issues, incomplete error handling
- **Info** — Suggested improvements. Includes: code style, readability, best practice deviations

## Common patterns to check

- Unchecked error returns
- Resource leaks (file handles, connections)
- Race conditions in concurrent code
- Security vulnerabilities (injection, hardcoded credentials)
- Performance anti-patterns (unnecessary allocations, O(n²) algorithms)
