package shell

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// StatementKind identifies the type of a parsed statement.
type StatementKind string

const (
	StmtExport   StatementKind = "export"
	StmtSpawn    StatementKind = "spawn"
	StmtPipeline StatementKind = "pipeline"
	StmtIf       StatementKind = "if"
	StmtFor      StatementKind = "for"
	StmtWhile    StatementKind = "while"
	StmtBuiltin  StatementKind = "builtin"
)

// MaxLoopIterations is the safety limit for while loops to prevent infinite execution.
const MaxLoopIterations = 10000

// ExportStmt holds a parsed export KEY=VALUE.
type ExportStmt struct {
	Key   string
	Value string
}

// Condition represents a binary comparison: $VAR.PROP OP VALUE or $VAR OP VALUE.
type Condition struct {
	VarName  string
	Property string // "exitcode", "result", or "" for plain variable
	Operator string // "==" or "!="
	Value    string
}

// IfBlock holds a parsed if/else/end block with nested statement lists.
type IfBlock struct {
	Condition Condition
	Then      []Statement
	Else      []Statement
}

// ForBlock holds a parsed for/in/end loop with variable binding and iteration list.
type ForBlock struct {
	VarName string
	List    []string
	Body    []Statement
}

// WhileBlock holds a parsed while/end loop with a condition.
type WhileBlock struct {
	Condition Condition
	Body      []Statement
}

// BuiltinStmt holds a parsed builtin command (wait, sleep, exit).
type BuiltinStmt struct {
	Command string
	Args    []string
}

// ErrScriptExit signals a controlled script termination via the exit builtin.
type ErrScriptExit struct {
	Code int
}

func (e *ErrScriptExit) Error() string {
	return fmt.Sprintf("script exit with code %d", e.Code)
}

// SpawnResult captures the outcome of an assignment spawn for condition evaluation.
type SpawnResult struct {
	ExitCode int
	Result   string
	Tokens   int
}

// Statement is a single parsed line of a script.
type Statement struct {
	Kind     StatementKind
	Export   *ExportStmt
	Spawn    *Command
	Pipeline *Pipeline
	If       *IfBlock
	For      *ForBlock
	While    *WhileBlock
	Builtin  *BuiltinStmt
	Assign   string   // variable name for assignment spawn (e.g. "result" in "result = spawn ...")
	OnError  *Command // on-error handler spawn command
	Raw      string
}

// Script is a sequence of parsed statements.
type Script struct {
	Statements []Statement
}

// ParseScript splits input by newlines and parses using a recursive descent parser
// that supports if/else/end blocks, assignment spawn, and on-error handlers.
func ParseScript(input string) (*Script, error) {
	lines := strings.Split(input, "\n")
	stmts, nextIdx, err := parseBlock(lines, 0, false)
	if err != nil {
		return nil, err
	}
	for i := nextIdx; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return nil, fmt.Errorf("line %d: unexpected statement after block end", i+1)
		}
	}
	return &Script{Statements: stmts}, nil
}

func parseBlock(lines []string, startIdx int, insideBlock bool) ([]Statement, int, error) {
	var stmts []Statement
	i := startIdx
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}
		lower := strings.ToLower(trimmed)

		if lower == "else" || lower == "end" {
			if !insideBlock {
				return nil, 0, fmt.Errorf("line %d: unexpected %q outside block", i+1, trimmed)
			}
			return stmts, i, nil
		}

		if strings.HasPrefix(lower, "if ") || strings.HasPrefix(lower, "if\t") {
			ifBlock, nextIdx, err := parseIfBlock(lines, i)
			if err != nil {
				return nil, 0, err
			}
			stmts = append(stmts, Statement{Kind: StmtIf, If: ifBlock, Raw: trimmed})
			i = nextIdx
			continue
		}

		if strings.HasPrefix(lower, "for ") || strings.HasPrefix(lower, "for\t") {
			forBlock, nextIdx, err := parseForBlock(lines, i)
			if err != nil {
				return nil, 0, err
			}
			stmts = append(stmts, Statement{Kind: StmtFor, For: forBlock, Raw: trimmed})
			i = nextIdx
			continue
		}

		if strings.HasPrefix(lower, "while ") || strings.HasPrefix(lower, "while\t") {
			whileBlock, nextIdx, err := parseWhileBlock(lines, i)
			if err != nil {
				return nil, 0, err
			}
			stmts = append(stmts, Statement{Kind: StmtWhile, While: whileBlock, Raw: trimmed})
			i = nextIdx
			continue
		}

		if isBuiltinKeyword(lower) {
			stmt, err := parseBuiltinStatement(trimmed, i)
			if err != nil {
				return nil, 0, err
			}
			stmts = append(stmts, stmt)
			i++
			continue
		}

		stmt, err := parseStatement(trimmed)
		if err != nil {
			return nil, 0, fmt.Errorf("line %d: %w", i+1, err)
		}
		stmts = append(stmts, stmt)
		i++
	}
	return stmts, i, nil
}

func parseIfBlock(lines []string, ifLineIdx int) (*IfBlock, int, error) {
	ifLine := strings.TrimSpace(lines[ifLineIdx])
	condStr := strings.TrimSpace(ifLine[3:])

	cond, err := parseCondition(condStr)
	if err != nil {
		return nil, 0, fmt.Errorf("line %d: %w", ifLineIdx+1, err)
	}

	thenBody, nextIdx, err := parseBlock(lines, ifLineIdx+1, true)
	if err != nil {
		return nil, 0, err
	}
	if nextIdx >= len(lines) {
		return nil, 0, fmt.Errorf("line %d: unclosed if block (missing 'end')", ifLineIdx+1)
	}

	block := &IfBlock{Condition: *cond, Then: thenBody}
	terminator := strings.ToLower(strings.TrimSpace(lines[nextIdx]))

	if terminator == "else" {
		elseBody, endIdx, err := parseBlock(lines, nextIdx+1, true)
		if err != nil {
			return nil, 0, err
		}
		if endIdx >= len(lines) || strings.ToLower(strings.TrimSpace(lines[endIdx])) != "end" {
			return nil, 0, fmt.Errorf("line %d: unclosed else block (missing 'end')", nextIdx+1)
		}
		block.Else = elseBody
		return block, endIdx + 1, nil
	}

	return block, nextIdx + 1, nil
}

func parseCondition(s string) (*Condition, error) {
	s = strings.TrimSpace(s)
	parts := strings.Fields(s)
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid condition: expected 3 parts, got %d in %q", len(parts), s)
	}

	left, op, right := parts[0], parts[1], parts[2]
	if op != "==" && op != "!=" {
		return nil, fmt.Errorf("invalid operator %q: must be '==' or '!='", op)
	}
	if !strings.HasPrefix(left, "$") {
		return nil, fmt.Errorf("condition left operand must start with '$': got %q", left)
	}

	ref := left[1:]
	varName := ref
	property := ""
	if dotIdx := strings.IndexByte(ref, '.'); dotIdx >= 0 {
		varName = ref[:dotIdx]
		property = ref[dotIdx+1:]
	}

	return &Condition{VarName: varName, Property: property, Operator: op, Value: right}, nil
}

func parseForBlock(lines []string, forLineIdx int) (*ForBlock, int, error) {
	forLine := strings.TrimSpace(lines[forLineIdx])
	rest := strings.TrimSpace(forLine[3:]) // skip "for" (case-insensitive, already matched)

	spaceIdx := strings.IndexAny(rest, " \t")
	if spaceIdx < 0 {
		return nil, 0, fmt.Errorf("line %d: invalid for syntax: expected 'for VAR in LIST'", forLineIdx+1)
	}
	varName := rest[:spaceIdx]

	if isReservedKeyword(varName) {
		return nil, 0, fmt.Errorf("line %d: cannot use reserved keyword %q as for loop variable", forLineIdx+1, varName)
	}

	remaining := strings.TrimSpace(rest[spaceIdx:])
	if len(remaining) < 2 || !strings.EqualFold(remaining[:2], "in") {
		return nil, 0, fmt.Errorf("line %d: expected 'in' keyword in for statement", forLineIdx+1)
	}
	if len(remaining) > 2 && remaining[2] != ' ' && remaining[2] != '\t' {
		return nil, 0, fmt.Errorf("line %d: expected 'in' keyword in for statement", forLineIdx+1)
	}

	listStr := strings.TrimSpace(remaining[2:])
	if listStr == "" {
		return nil, 0, fmt.Errorf("line %d: empty list in for statement", forLineIdx+1)
	}

	list, err := parseForList(listStr)
	if err != nil {
		return nil, 0, fmt.Errorf("line %d: %w", forLineIdx+1, err)
	}

	body, nextIdx, err := parseBlock(lines, forLineIdx+1, true)
	if err != nil {
		return nil, 0, err
	}
	if nextIdx >= len(lines) {
		return nil, 0, fmt.Errorf("line %d: unclosed for block (missing 'end')", forLineIdx+1)
	}

	terminator := strings.ToLower(strings.TrimSpace(lines[nextIdx]))
	if terminator == "else" {
		return nil, 0, fmt.Errorf("line %d: unexpected 'else' in for block", nextIdx+1)
	}

	return &ForBlock{VarName: varName, List: list, Body: body}, nextIdx + 1, nil
}

func parseForList(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s[0] == '[' {
		if s[len(s)-1] != ']' {
			return nil, fmt.Errorf("unclosed bracket in for list")
		}
		inner := s[1 : len(s)-1]
		items := strings.Split(inner, ",")
		var result []string
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item != "" {
				result = append(result, item)
			}
		}
		if len(result) == 0 {
			return nil, fmt.Errorf("empty for list")
		}
		return result, nil
	}
	return strings.Fields(s), nil
}

func parseWhileBlock(lines []string, whileLineIdx int) (*WhileBlock, int, error) {
	whileLine := strings.TrimSpace(lines[whileLineIdx])
	condStr := strings.TrimSpace(whileLine[5:]) // skip "while" (5 chars)

	cond, err := parseCondition(condStr)
	if err != nil {
		return nil, 0, fmt.Errorf("line %d: %w", whileLineIdx+1, err)
	}

	body, nextIdx, err := parseBlock(lines, whileLineIdx+1, true)
	if err != nil {
		return nil, 0, err
	}
	if nextIdx >= len(lines) {
		return nil, 0, fmt.Errorf("line %d: unclosed while block (missing 'end')", whileLineIdx+1)
	}

	terminator := strings.ToLower(strings.TrimSpace(lines[nextIdx]))
	if terminator == "else" {
		return nil, 0, fmt.Errorf("line %d: unexpected 'else' in while block", nextIdx+1)
	}

	return &WhileBlock{Condition: *cond, Body: body}, nextIdx + 1, nil
}

func isBuiltinKeyword(lower string) bool {
	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "wait", "sleep", "exit":
		return true
	}
	return false
}

func parseBuiltinStatement(line string, lineIdx int) (Statement, error) {
	fields := strings.Fields(line)
	command := strings.ToLower(fields[0])
	args := fields[1:]

	switch command {
	case "sleep":
		if len(args) != 1 {
			return Statement{}, fmt.Errorf("line %d: sleep requires exactly one duration argument", lineIdx+1)
		}
		if _, err := time.ParseDuration(args[0]); err != nil {
			return Statement{}, fmt.Errorf("line %d: invalid sleep duration %q: expected format like 5s, 500ms, 2m", lineIdx+1, args[0])
		}
	case "exit":
		if len(args) != 1 {
			return Statement{}, fmt.Errorf("line %d: exit requires exactly one integer argument (0-255)", lineIdx+1)
		}
		exitCode, err := strconv.Atoi(args[0])
		if err != nil {
			return Statement{}, fmt.Errorf("line %d: exit code %q is not a valid integer", lineIdx+1, args[0])
		}
		if exitCode < 0 || exitCode > 255 {
			return Statement{}, fmt.Errorf("line %d: exit code %d out of range (0-255)", lineIdx+1, exitCode)
		}
	case "wait":
		if len(args) != 1 {
			return Statement{}, fmt.Errorf("line %d: wait requires exactly one argument (PID or $variable)", lineIdx+1)
		}
	}

	return Statement{
		Kind:    StmtBuiltin,
		Builtin: &BuiltinStmt{Command: command, Args: args},
		Raw:     line,
	}, nil
}

func parseStatement(line string) (Statement, error) {
	lower := strings.ToLower(line)

	// 1. export (highest priority)
	if strings.HasPrefix(lower, "export ") || strings.HasPrefix(lower, "export\t") {
		return parseExport(line)
	}

	// 2. assignment: VAR = spawn "..."
	if varName, rest, ok := isAssignment(line); ok {
		mainLine, handlerLine, hasOnError := splitOnError(rest)
		cmd, err := parseSpawnCommand(mainLine)
		if err != nil {
			return Statement{}, err
		}
		stmt := Statement{Kind: StmtSpawn, Spawn: &cmd, Assign: varName, Raw: line}
		if hasOnError {
			hCmd, hErr := parseSpawnCommand(handlerLine)
			if hErr != nil {
				return Statement{}, hErr
			}
			stmt.OnError = &hCmd
		}
		return stmt, nil
	}

	// 3. on-error split (non-assignment)
	mainLine, handlerLine, hasOnError := splitOnError(line)

	// 4. pipeline detection (on main part)
	if isPipelineStatement(mainLine) {
		p, err := ParsePipeline(mainLine)
		if err != nil {
			return Statement{}, err
		}
		stmt := Statement{Kind: StmtPipeline, Pipeline: p, Raw: line}
		if hasOnError {
			hCmd, hErr := parseSpawnCommand(handlerLine)
			if hErr != nil {
				return Statement{}, hErr
			}
			stmt.OnError = &hCmd
		}
		return stmt, nil
	}

	// 5. spawn (default)
	cmd, err := parseSpawnCommand(mainLine)
	if err != nil {
		return Statement{}, err
	}
	stmt := Statement{Kind: StmtSpawn, Spawn: &cmd, Raw: line}
	if hasOnError {
		hCmd, hErr := parseSpawnCommand(handlerLine)
		if hErr != nil {
			return Statement{}, hErr
		}
		stmt.OnError = &hCmd
	}
	return stmt, nil
}

func isAssignment(line string) (varName, rest string, ok bool) {
	eqIdx := strings.Index(line, "=")
	if eqIdx < 0 {
		return "", "", false
	}

	// "==" is a condition operator, not an assignment
	if eqIdx+1 < len(line) && line[eqIdx+1] == '=' {
		return "", "", false
	}

	varName = strings.TrimSpace(line[:eqIdx])
	if !isValidVarName(varName) {
		return "", "", false
	}

	rest = strings.TrimSpace(line[eqIdx+1:])
	lr := strings.ToLower(rest)
	if !strings.HasPrefix(lr, "spawn ") && !strings.HasPrefix(lr, "spawn\t") &&
		!strings.HasPrefix(lr, "spawn\"") && !strings.HasPrefix(lr, "spawn'") {
		return "", "", false
	}
	return varName, rest, true
}

// SplitOnError splits a line at the first unquoted "on-error" keyword.
// Returns (main, handler, found). Exported for CLI script syntax detection.
func SplitOnError(line string) (string, string, bool) {
	return splitOnError(line)
}

func splitOnError(line string) (string, string, bool) {
	target := "on-error"
	inQuote := false
	var quoteChar byte

	for i := 0; i < len(line); i++ {
		ch := line[i]
		if inQuote {
			if ch == quoteChar {
				inQuote = false
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inQuote = true
			quoteChar = ch
			continue
		}
		if i+len(target) <= len(line) && strings.EqualFold(line[i:i+len(target)], target) {
			before := i == 0 || line[i-1] == ' ' || line[i-1] == '\t'
			after := i+len(target) == len(line) || line[i+len(target)] == ' ' || line[i+len(target)] == '\t'
			if before && after {
				main := strings.TrimSpace(line[:i])
				handler := strings.TrimSpace(line[i+len(target):])
				return main, handler, true
			}
		}
	}
	return line, "", false
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

// reservedKeywords contains keywords that cannot be used as variable names.
// Phase 3 keywords (fn, return, parallel, source) are pre-registered even
// though they are not yet implemented, to prevent user-defined name collisions.
var reservedKeywords = map[string]bool{
	"for": true, "in": true, "while": true, "if": true, "else": true, "end": true,
	"fn": true, "return": true, "parallel": true, "source": true,
	"wait": true, "sleep": true, "exit": true,
	"export": true, "spawn": true,
}

func isReservedKeyword(s string) bool {
	return reservedKeywords[strings.ToLower(s)]
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
	if isReservedKeyword(s) {
		return false
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
	captures     map[string]*SpawnResult
	OnStageStart StageCallback
}

// NewScriptExecutor creates a ScriptExecutor with the given spawner and environment.
func NewScriptExecutor(spawner KernelSpawner, env *Environment) *ScriptExecutor {
	return &ScriptExecutor{spawner: spawner, env: env, captures: make(map[string]*SpawnResult)}
}

// Execute runs each statement in order using recursive block execution.
// Supports export, spawn, pipeline, if/else/end, assignment spawn, and on-error.
func (e *ScriptExecutor) Execute(ctx context.Context, script *Script) (*ScriptResult, error) {
	start := time.Now()
	result := &ScriptResult{}
	stageNum := 0
	totalStages := countExecutableStages(script)

	err := e.executeBlock(ctx, script.Statements, result, &stageNum, totalStages)
	result.Elapsed = time.Since(start)

	var exitErr *ErrScriptExit
	if errors.As(err, &exitErr) {
		result.LastExitCode = exitErr.Code
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (e *ScriptExecutor) executeBlock(ctx context.Context, stmts []Statement,
	result *ScriptResult, stageNum *int, totalStages int) error {

	for _, stmt := range stmts {
		if err := ctx.Err(); err != nil {
			return err
		}

		switch stmt.Kind {
		case StmtExport:
			expandedValue := e.env.Expand(stmt.Export.Value)
			e.env.Set(stmt.Export.Key, expandedValue)

		case StmtSpawn:
			*stageNum++
			expandedIntent := e.env.Expand(stmt.Spawn.Intent)
			if e.OnStageStart != nil {
				e.OnStageStart(*stageNum, totalStages, expandedIntent)
			}
			res, exitCode, tokens, err := e.spawner.SpawnAndWait(ctx, expandedIntent, stmt.Spawn.Agent, stmt.Spawn.Model)
			if err != nil {
				return fmt.Errorf("spawn: %w", err)
			}
			result.LastResult = res
			result.LastExitCode = exitCode
			result.TotalTokens += tokens

			if stmt.Assign != "" {
				e.captures[stmt.Assign] = &SpawnResult{
					ExitCode: exitCode, Result: res, Tokens: tokens,
				}
				e.env.Set(stmt.Assign, res)
			}

			if exitCode != 0 && stmt.OnError != nil {
				*stageNum++
				hIntent := e.env.Expand(stmt.OnError.Intent)
				if e.OnStageStart != nil {
					e.OnStageStart(*stageNum, totalStages, hIntent)
				}
				hRes, hExitCode, hTokens, hErr := e.spawner.SpawnAndWait(
					ctx, hIntent, stmt.OnError.Agent, stmt.OnError.Model)
				if hErr != nil {
					return fmt.Errorf("on-error: %w", hErr)
				}
				result.LastResult = hRes
				result.LastExitCode = hExitCode
				result.TotalTokens += hTokens

				if stmt.Assign != "" {
					e.captures[stmt.Assign] = &SpawnResult{
						ExitCode: hExitCode, Result: hRes, Tokens: hTokens,
					}
					e.env.Set(stmt.Assign, hRes)
				}
			}

			if result.LastExitCode != 0 && stmt.Assign == "" {
				return nil
			}

		case StmtPipeline:
			*stageNum++
			expanded := expandPipelineIntents(e.env, stmt.Pipeline)
			if e.OnStageStart != nil {
				e.OnStageStart(*stageNum, totalStages, "pipeline")
			}
			pExec := NewPipelineExecutor(e.spawner)
			pResult, err := pExec.Execute(ctx, expanded)
			if err != nil {
				return fmt.Errorf("pipeline: %w", err)
			}
			if len(pResult.Stages) > 0 {
				last := pResult.Stages[len(pResult.Stages)-1]
				result.LastResult = last.Result
				result.LastExitCode = last.ExitCode
			}
			result.TotalTokens += pResult.TotalTokens

			if result.LastExitCode != 0 && stmt.OnError != nil {
				*stageNum++
				hIntent := e.env.Expand(stmt.OnError.Intent)
				if e.OnStageStart != nil {
					e.OnStageStart(*stageNum, totalStages, hIntent)
				}
				hRes, hExitCode, hTokens, hErr := e.spawner.SpawnAndWait(
					ctx, hIntent, stmt.OnError.Agent, stmt.OnError.Model)
				if hErr != nil {
					return fmt.Errorf("on-error: %w", hErr)
				}
				result.LastResult = hRes
				result.LastExitCode = hExitCode
				result.TotalTokens += hTokens
			}

			if result.LastExitCode != 0 {
				return nil
			}

		case StmtIf:
			match, err := e.evalCondition(&stmt.If.Condition)
			if err != nil {
				return fmt.Errorf("if condition: %w", err)
			}
			var branch []Statement
			if match {
				branch = stmt.If.Then
			} else {
				branch = stmt.If.Else
			}
			if len(branch) > 0 {
				err = e.executeBlock(ctx, branch, result, stageNum, totalStages)
				if err != nil {
					return err
				}
				if result.LastExitCode != 0 {
					return nil
				}
			}

		case StmtFor:
			for _, item := range stmt.For.List {
				if err := ctx.Err(); err != nil {
					return err
				}
				e.env.Set(stmt.For.VarName, e.env.Expand(item))
				err := e.executeBlock(ctx, stmt.For.Body, result, stageNum, totalStages)
				if err != nil {
					e.env.Delete(stmt.For.VarName)
					return err
				}
				if result.LastExitCode != 0 {
					break
				}
			}
			e.env.Delete(stmt.For.VarName)

		case StmtWhile:
			iterations := 0
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				match, err := e.evalCondition(&stmt.While.Condition)
				if err != nil {
					return fmt.Errorf("while condition: %w", err)
				}
				if !match {
					break
				}
				err = e.executeBlock(ctx, stmt.While.Body, result, stageNum, totalStages)
				if err != nil {
					return err
				}
				if result.LastExitCode != 0 {
					break
				}
				iterations++
				if iterations >= MaxLoopIterations {
					return fmt.Errorf("while loop exceeded maximum iterations (%d), possible infinite loop", MaxLoopIterations)
				}
			}

		case StmtBuiltin:
			if err := e.executeBuiltin(ctx, stmt.Builtin, result); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *ScriptExecutor) executeBuiltin(ctx context.Context, stmt *BuiltinStmt, result *ScriptResult) error {
	switch stmt.Command {
	case "wait":
		expanded := e.env.Expand(stmt.Args[0])
		pid, err := strconv.Atoi(expanded)
		if err != nil {
			return fmt.Errorf("wait: invalid PID %q: %w", expanded, err)
		}
		exitCode, err := e.spawner.Wait(ctx, pid)
		if err != nil {
			return fmt.Errorf("wait: %w", err)
		}
		result.LastExitCode = exitCode
		return nil

	case "sleep":
		d, err := time.ParseDuration(stmt.Args[0])
		if err != nil {
			return fmt.Errorf("sleep: invalid duration: %w", err)
		}
		select {
		case <-time.After(d):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}

	case "exit":
		code, err := strconv.Atoi(stmt.Args[0])
		if err != nil {
			return fmt.Errorf("exit: invalid code %q: %w", stmt.Args[0], err)
		}
		return &ErrScriptExit{Code: code}
	}

	return fmt.Errorf("unknown builtin command: %q", stmt.Command)
}

func (e *ScriptExecutor) evalCondition(cond *Condition) (bool, error) {
	var left string

	if cond.Property != "" {
		capture, ok := e.captures[cond.VarName]
		if !ok {
			return false, fmt.Errorf("undefined result variable: %q", cond.VarName)
		}
		switch cond.Property {
		case "exitcode":
			left = strconv.Itoa(capture.ExitCode)
		case "result":
			left = capture.Result
		default:
			return false, fmt.Errorf("unknown property %q on %q", cond.Property, cond.VarName)
		}
	} else {
		val, ok := e.env.Get(cond.VarName)
		if !ok {
			left = ""
		} else {
			left = val
		}
	}

	switch cond.Operator {
	case "==":
		return left == cond.Value, nil
	case "!=":
		return left != cond.Value, nil
	}
	return false, fmt.Errorf("unknown operator: %q", cond.Operator)
}

func countExecutableStages(script *Script) int {
	return countStagesInBlock(script.Statements)
}

func countStagesInBlock(stmts []Statement) int {
	n := 0
	for _, stmt := range stmts {
		switch stmt.Kind {
		case StmtSpawn:
			n++
			if stmt.OnError != nil {
				n++
			}
		case StmtPipeline:
			n++
			if stmt.OnError != nil {
				n++
			}
		case StmtIf:
			thenCount := countStagesInBlock(stmt.If.Then)
			elseCount := countStagesInBlock(stmt.If.Else)
			n += max(thenCount, elseCount)
		case StmtFor:
			bodyCount := countStagesInBlock(stmt.For.Body)
			n += len(stmt.For.List) * bodyCount
		case StmtWhile:
			bodyCount := countStagesInBlock(stmt.While.Body)
			n += 10 * bodyCount
		case StmtBuiltin:
			if stmt.Builtin.Command == "wait" {
				n++
			}
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
