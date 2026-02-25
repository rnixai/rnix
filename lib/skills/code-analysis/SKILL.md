---
name: code-analysis
description: >
  Analyze code quality, identify bugs, performance issues and security
  vulnerabilities. Use when the user wants to review code files or
  find problems in source code.
allowed-tools: /dev/fs /dev/shell
metadata:
  author: crux
  version: "1.0"
---

# Code Analysis

## When to use this skill

Use this skill when the user asks to analyze, review, or audit source code for quality issues, bugs, or security vulnerabilities.

## How to analyze code

1. Read the target file(s) via /dev/fs
2. Examine code structure, naming conventions, error handling patterns
3. Run static analysis tools via /dev/shell if available
4. If needed, read related files (imports, tests, configs) for context

## Tool usage guide

### /dev/fs — File system access

Use for reading target source code files:
- Read target files to get full source code
- Read related import files or config files for context
- Read test files to evaluate test coverage

### /dev/shell — Shell command execution

Use for auxiliary analysis commands:
- `wc -l` to count file lines
- `grep -rn "pattern" path | head -50` to search for specific patterns (always limit output lines)
- `find . -name "*.go" -type f` to find related files

⚠️ **Security constraint**: Always use `| head -N` to limit shell command output and avoid consuming context budget with large outputs.

## Workflow

1. **Read target files** — Use /dev/fs to read user-specified files
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
