package shell

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// StatementKind identifies the type of a parsed statement.
type StatementKind string

const (
	StmtExport      StatementKind = "export"
	StmtSpawn       StatementKind = "spawn"
	StmtPipeline    StatementKind = "pipeline"
	StmtIf          StatementKind = "if"
	StmtFor         StatementKind = "for"
	StmtWhile       StatementKind = "while"
	StmtBuiltin     StatementKind = "builtin"
	StmtFnDef       StatementKind = "fn-def"
	StmtFnCall      StatementKind = "fn-call"
	StmtReturn      StatementKind = "return"
	StmtArrayLit    StatementKind = "array-lit"
	StmtMapLit      StatementKind = "map-lit"
	StmtAssignIndex StatementKind = "assign-index"
	StmtAssignProp  StatementKind = "assign-prop"
	StmtParallel    StatementKind = "parallel"
	StmtSource      StatementKind = "source"
)

// MaxLoopIterations is the safety limit for while loops to prevent infinite execution.
const MaxLoopIterations = 10000

// MaxCallDepth is the safety limit for nested function calls to prevent infinite recursion.
const MaxCallDepth = 100

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

// ParallelBlock holds a parsed parallel/end block. Body is restricted to
// StmtSpawn and StmtPipeline statements only.
type ParallelBlock struct {
	Body []Statement
}

// SourceStmt holds a parsed source statement: source <path>
type SourceStmt struct {
	Path string // source target file path (raw value, may contain variables)
}

// BuiltinStmt holds a parsed builtin command (wait, sleep, exit).
type BuiltinStmt struct {
	Command string
	Args    []string
}

// FnDef holds a parsed function definition: fn NAME(PARAMS) ... end
type FnDef struct {
	Name   string
	Params []string
	Body   []Statement
}

// FnCallStmt holds a parsed function call: NAME(ARGS) or VAR = NAME(ARGS)
type FnCallStmt struct {
	Name string
	Args []string
}

// ReturnStmt holds a parsed return statement inside a function body.
type ReturnStmt struct {
	Value string
}

// ArrayLitStmt holds a parsed array literal: VAR = ["a", "b", "c"]
type ArrayLitStmt struct {
	Items []string
}

// MapLitStmt holds a parsed map literal: VAR = {key: "value", key2: "value2"}
type MapLitStmt struct {
	Entries []MapEntry
}

// MapEntry is a single key-value pair in a map literal.
type MapEntry struct {
	Key   string
	Value string
}

// IndexAssign holds a parsed index assignment: VAR[N] = VALUE
type IndexAssign struct {
	VarName string
	Index   string
	Value   string
}

// PropAssign holds a parsed property assignment: VAR.KEY = VALUE
type PropAssign struct {
	VarName  string
	Property string
	Value    string
}

// ErrScriptExit signals a controlled script termination via the exit builtin.
type ErrScriptExit struct {
	Code int
}

func (e *ErrScriptExit) Error() string {
	return fmt.Sprintf("script exit with code %d", e.Code)
}

// ErrFnReturn signals a function return for flow control (not a real error).
type ErrFnReturn struct {
	Value string
}

func (e *ErrFnReturn) Error() string {
	return fmt.Sprintf("function return: %s", e.Value)
}

// SpawnResult captures the outcome of an assignment spawn for condition evaluation.
type SpawnResult struct {
	ExitCode int
	Result   string
	Tokens   int
}

// Statement is a single parsed line of a script.
type Statement struct {
	Kind        StatementKind
	Export      *ExportStmt
	Spawn       *Command
	Pipeline    *Pipeline
	If          *IfBlock
	For         *ForBlock
	While       *WhileBlock
	Parallel    *ParallelBlock
	Source      *SourceStmt
	Builtin     *BuiltinStmt
	FnDef       *FnDef
	FnCall      *FnCallStmt
	Return      *ReturnStmt
	ArrayLit    *ArrayLitStmt
	MapLit      *MapLitStmt
	IndexAssign *IndexAssign
	PropAssign  *PropAssign
	Assign      string   // variable name for assignment spawn/fn-call/array-lit/map-lit
	OnError     *Command // on-error handler spawn command
	Raw         string
	Line        int // 1-based line number from source
}

// Script is a sequence of parsed statements.
type Script struct {
	Statements []Statement
	Functions  map[string]*FnDef
}

// StripShebang removes a leading shebang line (#!...) from script content.
// Exported for use by `rnix run` command.
func StripShebang(content string) string {
	return stripShebang(content)
}

func stripShebang(content string) string {
	if strings.HasPrefix(content, "#!") {
		if _, after, ok := strings.Cut(content, "\n"); ok {
			return after
		}
		return ""
	}
	return content
}

// ParseScript splits input by newlines and parses using a recursive descent parser
// that supports if/else/end blocks, assignment spawn, and on-error handlers.
func ParseScript(input string) (*Script, error) {
	input = stripShebang(input)
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

	script := &Script{Statements: stmts, Functions: make(map[string]*FnDef)}
	for _, stmt := range stmts {
		if stmt.Kind == StmtFnDef {
			if _, exists := script.Functions[stmt.FnDef.Name]; exists {
				return nil, fmt.Errorf("duplicate function name %q", stmt.FnDef.Name)
			}
			script.Functions[stmt.FnDef.Name] = stmt.FnDef
		}
	}
	hasSource := containsSourceStmt(script.Statements)
	if err := validateFnCalls(script.Statements, script.Functions, hasSource); err != nil {
		return nil, err
	}
	return script, nil
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

		if strings.HasPrefix(lower, "fn ") || strings.HasPrefix(lower, "fn\t") {
			if insideBlock {
				return nil, 0, fmt.Errorf("line %d: function definition not allowed inside block", i+1)
			}
			fnDef, nextIdx, err := parseFnDef(lines, i)
			if err != nil {
				return nil, 0, err
			}
			stmts = append(stmts, Statement{Kind: StmtFnDef, FnDef: fnDef, Raw: trimmed, Line: i + 1})
			i = nextIdx
			continue
		}

		if lower == "return" || strings.HasPrefix(lower, "return ") || strings.HasPrefix(lower, "return\t") {
			if !insideBlock {
				return nil, 0, fmt.Errorf("line %d: return not allowed at top level", i+1)
			}
			stmt, err := parseReturnStatement(trimmed, i)
			if err != nil {
				return nil, 0, err
			}
			stmt.Line = i + 1
			stmts = append(stmts, stmt)
			i++
			continue
		}

		if strings.HasPrefix(lower, "if ") || strings.HasPrefix(lower, "if\t") {
			ifBlock, nextIdx, err := parseIfBlock(lines, i)
			if err != nil {
				return nil, 0, err
			}
			stmts = append(stmts, Statement{Kind: StmtIf, If: ifBlock, Raw: trimmed, Line: i + 1})
			i = nextIdx
			continue
		}

		if lower == "parallel" {
			parallelBlock, nextIdx, err := parseParallelBlock(lines, i)
			if err != nil {
				return nil, 0, err
			}
			stmts = append(stmts, Statement{Kind: StmtParallel, Parallel: parallelBlock, Raw: trimmed, Line: i + 1})
			i = nextIdx
			continue
		}

		if strings.HasPrefix(lower, "for ") || strings.HasPrefix(lower, "for\t") {
			forBlock, nextIdx, err := parseForBlock(lines, i)
			if err != nil {
				return nil, 0, err
			}
			stmts = append(stmts, Statement{Kind: StmtFor, For: forBlock, Raw: trimmed, Line: i + 1})
			i = nextIdx
			continue
		}

		if strings.HasPrefix(lower, "while ") || strings.HasPrefix(lower, "while\t") {
			whileBlock, nextIdx, err := parseWhileBlock(lines, i)
			if err != nil {
				return nil, 0, err
			}
			stmts = append(stmts, Statement{Kind: StmtWhile, While: whileBlock, Raw: trimmed, Line: i + 1})
			i = nextIdx
			continue
		}

		if strings.HasPrefix(lower, "source ") || strings.HasPrefix(lower, "source\t") || lower == "source" {
			stmt, err := parseSourceStatement(trimmed, i)
			if err != nil {
				return nil, 0, err
			}
			stmts = append(stmts, stmt)
			i++
			continue
		}

		if isBuiltinKeyword(lower) {
			stmt, err := parseBuiltinStatement(trimmed, i)
			if err != nil {
				return nil, 0, err
			}
			stmt.Line = i + 1
			stmts = append(stmts, stmt)
			i++
			continue
		}

		stmt, err := parseStatement(trimmed)
		if err != nil {
			return nil, 0, fmt.Errorf("line %d: %w", i+1, err)
		}
		stmt.Line = i + 1
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

	// Handle ${VAR.KEY} or ${VAR[N]} brace syntax — store entire braced expr
	// for eval-time expansion
	if strings.HasPrefix(ref, "{") && strings.HasSuffix(ref, "}") {
		varName = ref // e.g. "{config.model}" — evaluated at runtime
		property = ""
	} else if before, after, ok := strings.Cut(ref, "."); ok {
		varName = before
		property = after
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

func parseSourceStatement(line string, lineIdx int) (Statement, error) {
	rest := strings.TrimSpace(line[6:]) // skip "source" (6 chars, case-insensitive match already done)
	if rest == "" {
		return Statement{}, fmt.Errorf("line %d: source requires a file path", lineIdx+1)
	}
	path := unquote(rest)
	return Statement{
		Kind:   StmtSource,
		Source: &SourceStmt{Path: path},
		Raw:    line,
		Line:   lineIdx + 1,
	}, nil
}

func parseParallelBlock(lines []string, parallelLineIdx int) (*ParallelBlock, int, error) {
	body, nextIdx, err := parseBlock(lines, parallelLineIdx+1, true)
	if err != nil {
		return nil, 0, err
	}
	if nextIdx >= len(lines) {
		return nil, 0, fmt.Errorf("line %d: unclosed parallel block (missing 'end')", parallelLineIdx+1)
	}

	terminator := strings.ToLower(strings.TrimSpace(lines[nextIdx]))
	if terminator == "else" {
		return nil, 0, fmt.Errorf("line %d: unexpected 'else' in parallel block", nextIdx+1)
	}

	for _, stmt := range body {
		switch stmt.Kind {
		case StmtSpawn, StmtPipeline:
			// allowed
		default:
			return nil, 0, fmt.Errorf("line %d: only spawn and pipeline statements allowed inside parallel block, got %s",
				stmt.Line, stmt.Kind)
		}
	}

	return &ParallelBlock{Body: body}, nextIdx + 1, nil
}

func parseFnDef(lines []string, fnLineIdx int) (*FnDef, int, error) {
	fnLine := strings.TrimSpace(lines[fnLineIdx])
	rest := strings.TrimSpace(fnLine[2:]) // skip "fn" keyword (2 chars)

	parenIdx := strings.IndexByte(rest, '(')
	if parenIdx < 0 {
		return nil, 0, fmt.Errorf("line %d: invalid fn syntax: missing '('", fnLineIdx+1)
	}
	name := strings.TrimSpace(rest[:parenIdx])
	if name == "" || !isValidIdentifier(name) {
		return nil, 0, fmt.Errorf("line %d: invalid function name %q", fnLineIdx+1, name)
	}
	if isReservedKeyword(name) {
		return nil, 0, fmt.Errorf("line %d: function name %q is a reserved keyword", fnLineIdx+1, name)
	}

	closeParenIdx := strings.IndexByte(rest[parenIdx:], ')')
	if closeParenIdx < 0 {
		return nil, 0, fmt.Errorf("line %d: invalid fn syntax: missing ')'", fnLineIdx+1)
	}
	closeParenIdx += parenIdx

	paramsStr := strings.TrimSpace(rest[parenIdx+1 : closeParenIdx])
	var params []string
	if paramsStr != "" {
		seen := make(map[string]bool)
		for p := range strings.SplitSeq(paramsStr, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if !isValidIdentifier(p) {
				return nil, 0, fmt.Errorf("line %d: invalid parameter name %q", fnLineIdx+1, p)
			}
			if isReservedKeyword(p) {
				return nil, 0, fmt.Errorf("line %d: parameter name %q is a reserved keyword", fnLineIdx+1, p)
			}
			if seen[p] {
				return nil, 0, fmt.Errorf("line %d: duplicate parameter name %q", fnLineIdx+1, p)
			}
			seen[p] = true
			params = append(params, p)
		}
	}

	body, nextIdx, err := parseBlock(lines, fnLineIdx+1, true)
	if err != nil {
		return nil, 0, err
	}
	if nextIdx >= len(lines) {
		return nil, 0, fmt.Errorf("line %d: unclosed fn block (missing 'end')", fnLineIdx+1)
	}
	terminator := strings.ToLower(strings.TrimSpace(lines[nextIdx]))
	if terminator == "else" {
		return nil, 0, fmt.Errorf("line %d: unexpected 'else' in fn block", nextIdx+1)
	}

	return &FnDef{Name: name, Params: params, Body: body}, nextIdx + 1, nil
}

func parseReturnStatement(line string, lineIdx int) (Statement, error) {
	value := ""
	if len(line) > 6 {
		value = strings.TrimSpace(line[6:])
		value = unquote(value)
	}
	return Statement{
		Kind:   StmtReturn,
		Return: &ReturnStmt{Value: value},
		Raw:    line,
	}, nil
}

func isValidIdentifier(s string) bool {
	if len(s) == 0 || !isVarStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isVarChar(s[i]) {
			return false
		}
	}
	return true
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

	// 2. array literal: VAR = [...]
	if varName, rest, ok := isArrayAssignment(line); ok {
		items, err := parseArrayLiteral(rest)
		if err != nil {
			return Statement{}, err
		}
		return Statement{
			Kind:     StmtArrayLit,
			ArrayLit: &ArrayLitStmt{Items: items},
			Assign:   varName,
			Raw:      line,
		}, nil
	}

	// 3. map literal: VAR = {...}
	if varName, rest, ok := isMapAssignment(line); ok {
		entries, err := parseMapLiteral(rest)
		if err != nil {
			return Statement{}, err
		}
		return Statement{
			Kind:   StmtMapLit,
			MapLit: &MapLitStmt{Entries: entries},
			Assign: varName,
			Raw:    line,
		}, nil
	}

	// 4. index assignment: VAR[N] = VALUE
	if varName, idx, val, ok := isIndexAssignment(line); ok {
		return Statement{
			Kind:        StmtAssignIndex,
			IndexAssign: &IndexAssign{VarName: varName, Index: idx, Value: val},
			Raw:         line,
		}, nil
	}

	// 5. property assignment: VAR.KEY = VALUE
	if varName, prop, val, ok := isPropAssignment(line); ok {
		return Statement{
			Kind:       StmtAssignProp,
			PropAssign: &PropAssign{VarName: varName, Property: prop, Value: val},
			Raw:        line,
		}, nil
	}

	// 6. spawn assignment: VAR = spawn "..."
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

	// 7. assignment fn call: VAR = NAME(ARGS)
	if stmt, ok := parseAssignmentFnCall(line); ok {
		return stmt, nil
	}

	// 8. fn call: NAME(ARGS)
	if fnName, args, ok := isFnCallExpr(line); ok {
		return Statement{
			Kind:   StmtFnCall,
			FnCall: &FnCallStmt{Name: fnName, Args: args},
			Raw:    line,
		}, nil
	}

	// 9. on-error split (non-assignment)
	mainLine, handlerLine, hasOnError := splitOnError(line)

	// 10. pipeline detection (on main part)
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

	// 11. spawn (default)
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

func parseAssignmentFnCall(line string) (Statement, bool) {
	eqIdx := strings.Index(line, "=")
	if eqIdx < 0 {
		return Statement{}, false
	}
	if eqIdx+1 < len(line) && line[eqIdx+1] == '=' {
		return Statement{}, false
	}
	varName := strings.TrimSpace(line[:eqIdx])
	if !isValidVarName(varName) {
		return Statement{}, false
	}
	rest := strings.TrimSpace(line[eqIdx+1:])
	fnName, args, ok := isFnCallExpr(rest)
	if !ok {
		return Statement{}, false
	}
	return Statement{
		Kind:   StmtFnCall,
		FnCall: &FnCallStmt{Name: fnName, Args: args},
		Assign: varName,
		Raw:    line,
	}, true
}

func isFnCallExpr(s string) (name string, args []string, ok bool) {
	s = strings.TrimSpace(s)
	parenIdx := strings.IndexByte(s, '(')
	if parenIdx <= 0 {
		return "", nil, false
	}
	name = s[:parenIdx]
	if !isValidIdentifier(name) || isReservedKeyword(name) {
		return "", nil, false
	}
	if s[len(s)-1] != ')' {
		return "", nil, false
	}
	argsStr := strings.TrimSpace(s[parenIdx+1 : len(s)-1])
	if argsStr == "" {
		return name, nil, true
	}
	args = parseFnCallArgs(argsStr)
	return name, args, true
}

func parseFnCallArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	var quoteChar byte

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuote {
			if ch == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(ch)
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inQuote = true
			quoteChar = ch
			continue
		}
		if ch == ',' {
			arg := strings.TrimSpace(current.String())
			if arg != "" {
				args = append(args, arg)
			}
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	arg := strings.TrimSpace(current.String())
	if arg != "" {
		args = append(args, arg)
	}
	return args
}

// containsSourceStmt checks if any source statement exists in the block (recursively).
func containsSourceStmt(stmts []Statement) bool {
	for _, stmt := range stmts {
		if stmt.Kind == StmtSource {
			return true
		}
		switch stmt.Kind {
		case StmtIf:
			if containsSourceStmt(stmt.If.Then) || containsSourceStmt(stmt.If.Else) {
				return true
			}
		case StmtFor:
			if containsSourceStmt(stmt.For.Body) {
				return true
			}
		case StmtWhile:
			if containsSourceStmt(stmt.While.Body) {
				return true
			}
		case StmtFnDef:
			if containsSourceStmt(stmt.FnDef.Body) {
				return true
			}
		case StmtParallel:
			if containsSourceStmt(stmt.Parallel.Body) {
				return true
			}
		}
	}
	return false
}

func validateFnCalls(stmts []Statement, functions map[string]*FnDef, hasSource bool) error {
	for _, stmt := range stmts {
		switch stmt.Kind {
		case StmtFnCall:
			if expectedArgs, isBuiltin := builtinFunctions[stmt.FnCall.Name]; isBuiltin {
				if len(stmt.FnCall.Args) != expectedArgs {
					if stmt.Line > 0 {
						return fmt.Errorf("line %d: builtin %q expects %d args, got %d",
							stmt.Line, stmt.FnCall.Name, expectedArgs, len(stmt.FnCall.Args))
					}
					return fmt.Errorf("builtin %q expects %d args, got %d",
						stmt.FnCall.Name, expectedArgs, len(stmt.FnCall.Args))
				}
				continue
			}
			fn, ok := functions[stmt.FnCall.Name]
			if !ok {
				if hasSource {
					continue // function may come from a sourced file at runtime
				}
				if stmt.Line > 0 {
					return fmt.Errorf("line %d: undefined function %q", stmt.Line, stmt.FnCall.Name)
				}
				return fmt.Errorf("undefined function %q", stmt.FnCall.Name)
			}
			if len(stmt.FnCall.Args) != len(fn.Params) {
				if stmt.Line > 0 {
					return fmt.Errorf("line %d: function %q expects %d args, got %d",
						stmt.Line, stmt.FnCall.Name, len(fn.Params), len(stmt.FnCall.Args))
				}
				return fmt.Errorf("function %q expects %d args, got %d",
					stmt.FnCall.Name, len(fn.Params), len(stmt.FnCall.Args))
			}
		case StmtIf:
			if err := validateFnCalls(stmt.If.Then, functions, hasSource); err != nil {
				return err
			}
			if err := validateFnCalls(stmt.If.Else, functions, hasSource); err != nil {
				return err
			}
		case StmtFor:
			if err := validateFnCalls(stmt.For.Body, functions, hasSource); err != nil {
				return err
			}
		case StmtWhile:
			if err := validateFnCalls(stmt.While.Body, functions, hasSource); err != nil {
				return err
			}
		case StmtFnDef:
			if err := validateFnCalls(stmt.FnDef.Body, functions, hasSource); err != nil {
				return err
			}
		case StmtParallel:
			if err := validateFnCalls(stmt.Parallel.Body, functions, hasSource); err != nil {
				return err
			}
		case StmtSource:
			// source introduces functions at runtime; skip validation
		}
	}
	return nil
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

	before, after, ok := strings.Cut(rest, "=")
	if !ok {
		return Statement{}, fmt.Errorf("invalid export: missing '=' in %q", line)
	}

	key := before
	if key == "" || !isValidVarName(key) {
		return Statement{}, fmt.Errorf("invalid export: invalid key %q in %q", key, line)
	}

	// Spaces before '=' are invalid (e.g., "KEY = VALUE")
	if strings.ContainsAny(key, " \t") {
		return Statement{}, fmt.Errorf("invalid export: spaces around '=' in %q", line)
	}

	value := after
	value = unquote(value)

	return Statement{
		Kind:   StmtExport,
		Export: &ExportStmt{Key: key, Value: value},
		Raw:    line,
	}, nil
}

// reservedKeywords contains keywords that cannot be used as variable names.
var reservedKeywords = map[string]bool{
	"for": true, "in": true, "while": true, "if": true, "else": true, "end": true,
	"fn": true, "return": true, "parallel": true, "source": true,
	"wait": true, "sleep": true, "exit": true,
	"export": true, "spawn": true,
}

var builtinFunctions = map[string]int{
	"len": 1, "append": 2, "keys": 1,
}

func isReservedKeyword(s string) bool {
	return reservedKeywords[strings.ToLower(s)]
}

func isValidVarName(s string) bool {
	if len(s) == 0 || !isVarStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isVarChar(s[i]) {
			return false
		}
	}
	return !isReservedKeyword(s)
}

func isArrayAssignment(line string) (varName, rest string, ok bool) {
	eqIdx := strings.Index(line, "=")
	if eqIdx < 0 {
		return "", "", false
	}
	if eqIdx+1 < len(line) && line[eqIdx+1] == '=' {
		return "", "", false
	}
	varName = strings.TrimSpace(line[:eqIdx])
	if !isValidVarName(varName) {
		return "", "", false
	}
	rest = strings.TrimSpace(line[eqIdx+1:])
	if len(rest) == 0 || rest[0] != '[' {
		return "", "", false
	}
	return varName, rest, true
}

func parseArrayLiteral(s string) ([]string, error) {
	if len(s) == 0 || s[0] != '[' {
		return nil, fmt.Errorf("expected '[' at start of array literal")
	}
	if s[len(s)-1] != ']' {
		return nil, fmt.Errorf("unclosed array literal: missing ']'")
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return []string{}, nil
	}

	var items []string
	var current strings.Builder
	inQuote := false
	var quoteChar byte

	for i := 0; i < len(inner); i++ {
		ch := inner[i]
		if inQuote {
			if ch == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(ch)
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inQuote = true
			quoteChar = ch
			continue
		}
		if ch == ',' {
			item := strings.TrimSpace(current.String())
			if item != "" {
				items = append(items, item)
			}
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	item := strings.TrimSpace(current.String())
	if item != "" {
		items = append(items, item)
	}
	return items, nil
}

func isMapAssignment(line string) (varName, rest string, ok bool) {
	eqIdx := strings.Index(line, "=")
	if eqIdx < 0 {
		return "", "", false
	}
	if eqIdx+1 < len(line) && line[eqIdx+1] == '=' {
		return "", "", false
	}
	varName = strings.TrimSpace(line[:eqIdx])
	if !isValidVarName(varName) {
		return "", "", false
	}
	rest = strings.TrimSpace(line[eqIdx+1:])
	if len(rest) == 0 || rest[0] != '{' {
		return "", "", false
	}
	return varName, rest, true
}

func parseMapLiteral(s string) ([]MapEntry, error) {
	if len(s) == 0 || s[0] != '{' {
		return nil, fmt.Errorf("expected '{' at start of map literal")
	}
	if s[len(s)-1] != '}' {
		return nil, fmt.Errorf("unclosed map literal: missing '}'")
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return []MapEntry{}, nil
	}

	// Split by comma (respecting quotes)
	var parts []string
	var current strings.Builder
	inQuote := false
	var quoteChar byte

	for i := 0; i < len(inner); i++ {
		ch := inner[i]
		if inQuote {
			if ch == quoteChar {
				inQuote = false
			}
			current.WriteByte(ch)
			continue
		}
		if ch == '"' || ch == '\'' {
			inQuote = true
			quoteChar = ch
			current.WriteByte(ch)
			continue
		}
		if ch == ',' {
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	part := strings.TrimSpace(current.String())
	if part != "" {
		parts = append(parts, part)
	}

	seen := make(map[string]bool)
	var entries []MapEntry
	for _, p := range parts {
		before, after, ok := strings.Cut(p, ":")
		if !ok {
			return nil, fmt.Errorf("map entry missing ':' separator in %q", p)
		}
		key := strings.TrimSpace(before)
		value := strings.TrimSpace(after)
		if key == "" || !isValidIdentifier(key) {
			return nil, fmt.Errorf("invalid map key %q: must be a valid identifier", key)
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate key %q in map literal", key)
		}
		seen[key] = true
		value = unquote(value)
		entries = append(entries, MapEntry{Key: key, Value: value})
	}
	return entries, nil
}

func isIndexAssignment(line string) (varName, index, value string, ok bool) {
	bracketIdx := strings.IndexByte(line, '[')
	if bracketIdx <= 0 {
		return "", "", "", false
	}
	closeBracket := strings.IndexByte(line[bracketIdx:], ']')
	if closeBracket < 0 {
		return "", "", "", false
	}
	closeBracket += bracketIdx

	varName = strings.TrimSpace(line[:bracketIdx])
	if !isValidVarName(varName) {
		return "", "", "", false
	}

	after := strings.TrimSpace(line[closeBracket+1:])
	if len(after) == 0 || after[0] != '=' {
		return "", "", "", false
	}
	if len(after) > 1 && after[1] == '=' {
		return "", "", "", false
	}

	index = strings.TrimSpace(line[bracketIdx+1 : closeBracket])
	value = strings.TrimSpace(after[1:])
	value = unquote(value)
	return varName, index, value, true
}

func isPropAssignment(line string) (varName, prop, value string, ok bool) {
	dotIdx := strings.IndexByte(line, '.')
	if dotIdx <= 0 {
		return "", "", "", false
	}

	varName = strings.TrimSpace(line[:dotIdx])
	if !isValidVarName(varName) {
		return "", "", "", false
	}

	rest := line[dotIdx+1:]
	eqIdx := strings.Index(rest, "=")
	if eqIdx < 0 {
		return "", "", "", false
	}
	if eqIdx+1 < len(rest) && rest[eqIdx+1] == '=' {
		return "", "", "", false
	}

	prop = strings.TrimSpace(rest[:eqIdx])
	if !isValidIdentifier(prop) {
		return "", "", "", false
	}

	value = strings.TrimSpace(rest[eqIdx+1:])
	value = unquote(value)
	return varName, prop, value, true
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
	functions    map[string]*FnDef
	callDepth    int
	OnStageStart StageCallback
	fileReader   FileReader
	sourceStack  map[string]bool
	scriptDir    string
}

// NewScriptExecutor creates a ScriptExecutor with the given spawner and environment.
func NewScriptExecutor(spawner KernelSpawner, env *Environment) *ScriptExecutor {
	return &ScriptExecutor{
		spawner:     spawner,
		env:         env,
		captures:    make(map[string]*SpawnResult),
		fileReader:  &OSFileReader{},
		sourceStack: make(map[string]bool),
	}
}

// NewScriptExecutorWithReader creates a ScriptExecutor with an injected FileReader.
func NewScriptExecutorWithReader(spawner KernelSpawner, env *Environment, reader FileReader) *ScriptExecutor {
	return &ScriptExecutor{
		spawner:     spawner,
		env:         env,
		captures:    make(map[string]*SpawnResult),
		fileReader:  reader,
		sourceStack: make(map[string]bool),
	}
}

// SetScriptDir sets the base directory for resolving relative source paths.
func (e *ScriptExecutor) SetScriptDir(dir string) {
	e.scriptDir = dir
}

// SetFileReader sets the file reader implementation.
func (e *ScriptExecutor) SetFileReader(r FileReader) {
	e.fileReader = r
}

// Execute runs each statement in order using recursive block execution.
// Supports export, spawn, pipeline, if/else/end, assignment spawn, and on-error.
func (e *ScriptExecutor) Execute(ctx context.Context, script *Script) (*ScriptResult, error) {
	start := time.Now()
	result := &ScriptResult{}
	stageNum := 0
	totalStages := countExecutableStages(script)

	e.functions = script.Functions

	err := e.executeBlock(ctx, script.Statements, result, &stageNum, totalStages)
	result.Elapsed = time.Since(start)

	var exitErr *ErrScriptExit
	if errors.As(err, &exitErr) {
		result.LastExitCode = exitErr.Code
		return result, nil
	}
	var returnErr *ErrFnReturn
	if errors.As(err, &returnErr) {
		return nil, fmt.Errorf("return outside function")
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

		case StmtArrayLit:
			items := make([]string, len(stmt.ArrayLit.Items))
			for i, item := range stmt.ArrayLit.Items {
				items[i] = e.env.Expand(item)
			}
			e.env.SetArray(stmt.Assign, items)

		case StmtMapLit:
			m := make(map[string]string, len(stmt.MapLit.Entries))
			for _, entry := range stmt.MapLit.Entries {
				m[entry.Key] = e.env.Expand(entry.Value)
			}
			e.env.SetMap(stmt.Assign, m)

		case StmtAssignIndex:
			ia := stmt.IndexAssign
			arr, ok := e.env.GetArray(ia.VarName)
			if !ok {
				return fmt.Errorf("line %d: variable %q is not an array", stmt.Line, ia.VarName)
			}
			expandedIdx := e.env.Expand(ia.Index)
			idx, err := strconv.Atoi(expandedIdx)
			if err != nil {
				return fmt.Errorf("line %d: invalid array index %q", stmt.Line, ia.Index)
			}
			if idx < 0 || idx >= len(arr) {
				return fmt.Errorf("line %d: array %q index %d out of range (length %d)", stmt.Line, ia.VarName, idx, len(arr))
			}
			arr[idx] = e.env.Expand(ia.Value)
			e.env.SetArray(ia.VarName, arr)

		case StmtAssignProp:
			pa := stmt.PropAssign
			m, ok := e.env.GetMap(pa.VarName)
			if !ok {
				return fmt.Errorf("line %d: variable %q is not a map", stmt.Line, pa.VarName)
			}
			m[pa.Property] = e.env.Expand(pa.Value)
			e.env.SetMap(pa.VarName, m)

		case StmtSpawn:
			*stageNum++
			expandedIntent, err := e.env.ExpandStrict(stmt.Spawn.Intent)
			if err != nil {
				return fmt.Errorf("line %d: %w", stmt.Line, err)
			}
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
				hIntent, hExpandErr := e.env.ExpandStrict(stmt.OnError.Intent)
				if hExpandErr != nil {
					return fmt.Errorf("line %d: on-error: %w", stmt.Line, hExpandErr)
				}
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
			expanded, err := expandPipelineIntentsStrict(e.env, stmt.Pipeline)
			if err != nil {
				return fmt.Errorf("line %d: %w", stmt.Line, err)
			}
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
				hIntent, hExpandErr := e.env.ExpandStrict(stmt.OnError.Intent)
				if hExpandErr != nil {
					return fmt.Errorf("line %d: on-error: %w", stmt.Line, hExpandErr)
				}
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

		case StmtParallel:
			if err := e.executeParallel(ctx, stmt, result, stageNum, totalStages); err != nil {
				return err
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
			list := stmt.For.List
			if len(list) == 1 && strings.HasPrefix(list[0], "$") {
				varName := list[0][1:]
				if arr, ok := e.env.GetArray(varName); ok {
					list = arr
				}
			}
			for _, item := range list {
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

		case StmtFnDef:
			// definitions are registered at parse time, not executed

		case StmtFnCall:
			if _, isBuiltin := builtinFunctions[stmt.FnCall.Name]; isBuiltin {
				returnVal, err := e.executeBuiltinFn(ctx, stmt)
				if err != nil {
					return fmt.Errorf("line %d: %w", stmt.Line, err)
				}
				if stmt.Assign != "" && stmt.FnCall.Name != "keys" {
					e.env.Set(stmt.Assign, returnVal)
				}
				break
			}

			fnDef, ok := e.functions[stmt.FnCall.Name]
			if !ok {
				return fmt.Errorf("undefined function %q", stmt.FnCall.Name)
			}
			if e.callDepth >= MaxCallDepth {
				return fmt.Errorf("maximum call depth (%d) exceeded, possible infinite recursion", MaxCallDepth)
			}

			expandedArgs := make([]string, len(stmt.FnCall.Args))
			for i, arg := range stmt.FnCall.Args {
				expandedArgs[i] = e.env.Expand(arg)
			}

			type saveEntry struct {
				value   string
				existed bool
			}
			saved := make(map[string]saveEntry)
			for i, paramName := range fnDef.Params {
				if old, ok := e.env.Get(paramName); ok {
					saved[paramName] = saveEntry{value: old, existed: true}
				} else {
					saved[paramName] = saveEntry{existed: false}
				}
				e.env.Set(paramName, expandedArgs[i])
			}

			e.callDepth++
			fnErr := e.executeBlock(ctx, fnDef.Body, result, stageNum, totalStages)
			e.callDepth--

			for paramName, entry := range saved {
				if entry.existed {
					e.env.Set(paramName, entry.value)
				} else {
					e.env.Delete(paramName)
				}
			}

			returnValue := ""
			var returnErr *ErrFnReturn
			if errors.As(fnErr, &returnErr) {
				returnValue = returnErr.Value
				fnErr = nil
			}
			if fnErr != nil {
				return fnErr
			}
			if stmt.Assign != "" {
				e.env.Set(stmt.Assign, returnValue)
			}

		case StmtReturn:
			value := ""
			if stmt.Return.Value != "" {
				value = e.expandReturnValue(stmt.Return.Value)
			}
			return &ErrFnReturn{Value: value}

		case StmtSource:
			if err := e.executeSource(ctx, stmt, result, stageNum, totalStages); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *ScriptExecutor) executeSource(ctx context.Context, stmt Statement, result *ScriptResult, stageNum *int, totalStages int) error {
	expandedPath, err := e.env.ExpandStrict(stmt.Source.Path)
	if err != nil {
		return fmt.Errorf("line %d: source: %w", stmt.Line, err)
	}

	// Resolve relative paths based on scriptDir
	if !filepath.IsAbs(expandedPath) {
		base := e.scriptDir
		if base == "" {
			base, _ = filepath.Abs(".")
		}
		expandedPath = filepath.Join(base, expandedPath)
	}
	absPath, err := filepath.Abs(expandedPath)
	if err != nil {
		return fmt.Errorf("line %d: source %q: %w", stmt.Line, expandedPath, err)
	}

	// Circular reference detection
	if e.sourceStack[absPath] {
		return fmt.Errorf("line %d: source %q: circular reference detected", stmt.Line, stmt.Source.Path)
	}

	content, err := e.fileReader.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("line %d: source %q: %w", stmt.Line, stmt.Source.Path, err)
	}

	content = stripShebang(content)

	sourcedScript, err := ParseScript(content)
	if err != nil {
		return fmt.Errorf("line %d: source %q: %w", stmt.Line, stmt.Source.Path, err)
	}

	// Register sourced script's functions into current function table
	maps.Copy(e.functions, sourcedScript.Functions)

	// Push source stack, save and set scriptDir
	e.sourceStack[absPath] = true
	prevDir := e.scriptDir
	e.scriptDir = filepath.Dir(absPath)

	err = e.executeBlock(ctx, sourcedScript.Statements, result, stageNum, totalStages)

	// Restore scriptDir and pop source stack
	e.scriptDir = prevDir
	delete(e.sourceStack, absPath)

	return err
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

type parallelTask struct {
	idx              int
	stmt             Statement
	expandedIntent   string
	expandedOnError  string
	expandedPipeline *Pipeline
}

type parallelResult struct {
	result   string
	exitCode int
	tokens   int
	err      error
}

func (e *ScriptExecutor) executeParallel(ctx context.Context, stmt Statement, result *ScriptResult, stageNum *int, totalStages int) error {
	body := stmt.Parallel.Body
	if len(body) == 0 {
		return nil
	}

	// Phase A: sequentially expand all intents on the main goroutine
	tasks := make([]parallelTask, 0, len(body))
	for idx, s := range body {
		task := parallelTask{idx: idx, stmt: s}
		switch s.Kind {
		case StmtSpawn:
			expanded, err := e.env.ExpandStrict(s.Spawn.Intent)
			if err != nil {
				return fmt.Errorf("line %d: %w", s.Line, err)
			}
			task.expandedIntent = expanded
			if s.OnError != nil {
				expandedOnErr, err := e.env.ExpandStrict(s.OnError.Intent)
				if err != nil {
					return fmt.Errorf("line %d: on-error: %w", s.Line, err)
				}
				task.expandedOnError = expandedOnErr
			}
		case StmtPipeline:
			expanded, err := expandPipelineIntentsStrict(e.env, s.Pipeline)
			if err != nil {
				return fmt.Errorf("line %d: %w", s.Line, err)
			}
			task.expandedPipeline = expanded
		}
		tasks = append(tasks, task)
	}

	// Phase B: execute all tasks in parallel
	results := make([]parallelResult, len(tasks))
	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for i, task := range tasks {
		go func(idx int, t parallelTask) {
			defer wg.Done()
			switch t.stmt.Kind {
			case StmtSpawn:
				res, exitCode, tokens, err := e.spawner.SpawnAndWait(ctx, t.expandedIntent, t.stmt.Spawn.Agent, t.stmt.Spawn.Model)
				if exitCode != 0 && t.stmt.OnError != nil && err == nil {
					hRes, hExitCode, hTokens, hErr := e.spawner.SpawnAndWait(ctx, t.expandedOnError, t.stmt.OnError.Agent, t.stmt.OnError.Model)
					if hErr == nil {
						res, exitCode, tokens = hRes, hExitCode, tokens+hTokens
					} else {
						err = hErr
					}
				}
				results[idx] = parallelResult{result: res, exitCode: exitCode, tokens: tokens, err: err}
			case StmtPipeline:
				pExec := NewPipelineExecutor(e.spawner)
				pResult, err := pExec.Execute(ctx, t.expandedPipeline)
				if err != nil {
					results[idx] = parallelResult{err: err}
				} else {
					pr := parallelResult{tokens: pResult.TotalTokens}
					if len(pResult.Stages) > 0 {
						last := pResult.Stages[len(pResult.Stages)-1]
						pr.result = last.Result
						pr.exitCode = last.ExitCode
					}
					results[idx] = pr
				}
			}
		}(i, task)
	}
	wg.Wait()

	// Phase C: sequentially collect results on the main goroutine
	var firstErr error
	for i, pr := range results {
		if pr.err != nil {
			if firstErr == nil {
				firstErr = pr.err
			}
			continue
		}
		result.TotalTokens += pr.tokens
		result.LastResult = pr.result
		result.LastExitCode = pr.exitCode
		*stageNum++
		if e.OnStageStart != nil {
			intent := tasks[i].expandedIntent
			if tasks[i].stmt.Kind == StmtPipeline {
				intent = "pipeline"
			}
			e.OnStageStart(*stageNum, totalStages, intent)
		}

		s := tasks[i].stmt
		if s.Assign != "" {
			e.captures[s.Assign] = &SpawnResult{
				ExitCode: pr.exitCode, Result: pr.result, Tokens: pr.tokens,
			}
			e.env.Set(s.Assign, pr.result)
		}
	}

	return firstErr
}

func (e *ScriptExecutor) expandReturnValue(val string) string {
	if strings.HasPrefix(val, "$") {
		ref := val[1:]
		if before, after, ok := strings.Cut(ref, "."); ok {
			varName := before
			property := after
			if capture, ok := e.captures[varName]; ok {
				switch property {
				case "result":
					return capture.Result
				case "exitcode":
					return strconv.Itoa(capture.ExitCode)
				}
			}
		}
	}
	return e.env.Expand(val)
}

func (e *ScriptExecutor) executeBuiltinFn(_ context.Context, stmt Statement) (string, error) {
	name := stmt.FnCall.Name
	args := stmt.FnCall.Args

	switch name {
	case "len":
		arg := args[0]
		varName := strings.TrimPrefix(arg, "$")
		n, err := e.env.LenOf(varName)
		if err != nil {
			return "", err
		}
		return strconv.Itoa(n), nil

	case "append":
		arrName := strings.TrimPrefix(args[0], "$")
		arr, ok := e.env.GetArray(arrName)
		if !ok {
			return "", fmt.Errorf("append: variable %q is not an array", arrName)
		}
		val := e.env.Expand(args[1])
		arr = append(arr, val)
		e.env.SetArray(arrName, arr)
		return "", nil

	case "keys":
		mapName := strings.TrimPrefix(args[0], "$")
		m, ok := e.env.GetMap(mapName)
		if !ok {
			return "", fmt.Errorf("keys: variable %q is not a map", mapName)
		}
		var keyList []string
		for k := range m {
			keyList = append(keyList, k)
		}
		sortStrings(keyList)
		if stmt.Assign != "" {
			e.env.SetArray(stmt.Assign, keyList)
		}
		return "", nil
	}

	return "", fmt.Errorf("unknown builtin function %q", name)
}

func (e *ScriptExecutor) evalCondition(cond *Condition) (bool, error) {
	var left string

	// Handle ${...} brace expression (expanded at eval time)
	if strings.HasPrefix(cond.VarName, "{") && strings.HasSuffix(cond.VarName, "}") {
		expr := "$" + cond.VarName
		left = e.env.Expand(expr)
	} else if cond.Property != "" {
		// First check captures (spawn result properties)
		capture, ok := e.captures[cond.VarName]
		if ok {
			switch cond.Property {
			case "exitcode":
				left = strconv.Itoa(capture.ExitCode)
			case "result":
				left = capture.Result
			default:
				return false, fmt.Errorf("unknown property %q on %q", cond.Property, cond.VarName)
			}
		} else {
			// Try map property access
			m, mOk := e.env.GetMap(cond.VarName)
			if mOk {
				val, valOk := m[cond.Property]
				if valOk {
					left = val
				}
			} else {
				return false, fmt.Errorf("undefined result variable: %q", cond.VarName)
			}
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
		case StmtFnDef:
			// 0 — definition doesn't count as a stage
		case StmtFnCall:
			n++
		case StmtReturn:
			// 0
		case StmtParallel:
			n += countStagesInBlock(stmt.Parallel.Body)
		case StmtArrayLit, StmtMapLit, StmtAssignIndex, StmtAssignProp:
			// 0 — pure assignments, no spawn
		case StmtSource:
			// 0 — sourced script stages are unknown at parse time
		}
	}
	return n
}

func expandPipelineIntentsStrict(env *Environment, p *Pipeline) (*Pipeline, error) {
	expanded := &Pipeline{Commands: make([]Command, len(p.Commands))}
	for i, cmd := range p.Commands {
		intent, err := env.ExpandStrict(cmd.Intent)
		if err != nil {
			return nil, err
		}
		expanded.Commands[i] = Command{
			Type:   cmd.Type,
			Intent: intent,
			Agent:  cmd.Agent,
			Model:  cmd.Model,
		}
	}
	return expanded, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
