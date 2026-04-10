package shell

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

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
