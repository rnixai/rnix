package shell

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// StatementKind identifies the type of a parsed statement.
type StatementKind string

const (
	StmtExport   StatementKind = "export"
	StmtSpawn    StatementKind = "spawn"
	StmtPipeline StatementKind = "pipeline"
)

// ExportStmt holds a parsed export KEY=VALUE.
type ExportStmt struct {
	Key   string
	Value string
}

// Statement is a single parsed line of a script.
type Statement struct {
	Kind     StatementKind
	Export   *ExportStmt
	Spawn    *Command
	Pipeline *Pipeline
	Raw      string
}

// Script is a sequence of parsed statements.
type Script struct {
	Statements []Statement
}

// ParseScript splits input by newlines and parses each non-empty, non-comment line.
func ParseScript(input string) (*Script, error) {
	lines := strings.Split(input, "\n")
	var stmts []Statement
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		stmt, err := parseStatement(trimmed)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		stmts = append(stmts, stmt)
	}
	return &Script{Statements: stmts}, nil
}

func parseStatement(line string) (Statement, error) {
	lower := strings.ToLower(line)
	if strings.HasPrefix(lower, "export ") || strings.HasPrefix(lower, "export\t") {
		return parseExport(line)
	}

	if isPipelineStatement(line) {
		p, err := ParsePipeline(line)
		if err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StmtPipeline, Pipeline: p, Raw: line}, nil
	}

	cmd, err := parseSpawnCommand(line)
	if err != nil {
		return Statement{}, err
	}
	return Statement{Kind: StmtSpawn, Spawn: &cmd, Raw: line}, nil
}

func isPipelineStatement(line string) bool {
	segments := splitPipeline(line)
	if len(segments) < 2 {
		return false
	}
	spawnCount := 0
	for _, seg := range segments {
		trimmed := strings.TrimSpace(strings.ToLower(seg))
		if strings.HasPrefix(trimmed, "spawn") &&
			(len(trimmed) == 5 || trimmed[5] == ' ' || trimmed[5] == '\t' || trimmed[5] == '"' || trimmed[5] == '\'') {
			spawnCount++
		}
	}
	return spawnCount >= 2
}

func parseExport(line string) (Statement, error) {
	// Strip the "export" keyword (case-insensitive)
	rest := strings.TrimSpace(line[len("export"):])

	eqIdx := strings.IndexByte(rest, '=')
	if eqIdx < 0 {
		return Statement{}, fmt.Errorf("invalid export: missing '=' in %q", line)
	}

	key := rest[:eqIdx]
	if key == "" || !isValidVarName(key) {
		return Statement{}, fmt.Errorf("invalid export: invalid key %q in %q", key, line)
	}

	// Spaces before '=' are invalid (e.g., "KEY = VALUE")
	if strings.ContainsAny(key, " \t") {
		return Statement{}, fmt.Errorf("invalid export: spaces around '=' in %q", line)
	}

	value := rest[eqIdx+1:]
	value = unquote(value)

	return Statement{
		Kind:   StmtExport,
		Export: &ExportStmt{Key: key, Value: value},
		Raw:    line,
	}, nil
}

func isValidVarName(s string) bool {
	if len(s) == 0 {
		return false
	}
	if !isVarStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isVarChar(s[i]) {
			return false
		}
	}
	return true
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ScriptResult holds the outcome of a script execution.
type ScriptResult struct {
	LastResult   string
	LastExitCode int
	TotalTokens  int
	Elapsed      time.Duration
}

// ScriptExecutor runs a parsed Script sequentially.
type ScriptExecutor struct {
	spawner      KernelSpawner
	env          *Environment
	OnStageStart StageCallback
}

// NewScriptExecutor creates a ScriptExecutor with the given spawner and environment.
func NewScriptExecutor(spawner KernelSpawner, env *Environment) *ScriptExecutor {
	return &ScriptExecutor{spawner: spawner, env: env}
}

// Execute runs each statement in order. Export statements set variables in env;
// spawn/pipeline statements expand variables before execution.
// Non-zero ExitCode breaks execution. Context cancellation returns an error.
func (e *ScriptExecutor) Execute(ctx context.Context, script *Script) (*ScriptResult, error) {
	start := time.Now()
	result := &ScriptResult{}
	stageNum := 0
	totalStages := countExecutableStages(script)

	for _, stmt := range script.Statements {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		switch stmt.Kind {
		case StmtExport:
			expandedValue := e.env.Expand(stmt.Export.Value)
			e.env.Set(stmt.Export.Key, expandedValue)

		case StmtSpawn:
			stageNum++
			expandedIntent := e.env.Expand(stmt.Spawn.Intent)
			if e.OnStageStart != nil {
				e.OnStageStart(stageNum, totalStages, expandedIntent)
			}
			res, exitCode, tokens, err := e.spawner.SpawnAndWait(ctx, expandedIntent, stmt.Spawn.Agent, stmt.Spawn.Model)
			if err != nil {
				return nil, fmt.Errorf("spawn: %w", err)
			}
			result.LastResult = res
			result.LastExitCode = exitCode
			result.TotalTokens += tokens
			if exitCode != 0 {
				result.Elapsed = time.Since(start)
				return result, nil
			}

		case StmtPipeline:
			stageNum++
			expanded := expandPipelineIntents(e.env, stmt.Pipeline)
			if e.OnStageStart != nil {
				e.OnStageStart(stageNum, totalStages, "pipeline")
			}
			pExec := NewPipelineExecutor(e.spawner)
			pResult, err := pExec.Execute(ctx, expanded)
			if err != nil {
				return nil, fmt.Errorf("pipeline: %w", err)
			}
			if len(pResult.Stages) > 0 {
				last := pResult.Stages[len(pResult.Stages)-1]
				result.LastResult = last.Result
				result.LastExitCode = last.ExitCode
			}
			result.TotalTokens += pResult.TotalTokens
			if result.LastExitCode != 0 {
				result.Elapsed = time.Since(start)
				return result, nil
			}
		}
	}

	result.Elapsed = time.Since(start)
	return result, nil
}

func countExecutableStages(script *Script) int {
	n := 0
	for _, stmt := range script.Statements {
		if stmt.Kind == StmtSpawn || stmt.Kind == StmtPipeline {
			n++
		}
	}
	return n
}

func expandPipelineIntents(env *Environment, p *Pipeline) *Pipeline {
	expanded := &Pipeline{Commands: make([]Command, len(p.Commands))}
	for i, cmd := range p.Commands {
		expanded.Commands[i] = Command{
			Type:   cmd.Type,
			Intent: env.Expand(cmd.Intent),
			Agent:  cmd.Agent,
			Model:  cmd.Model,
		}
	}
	return expanded
}
