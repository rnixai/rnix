package shell

import (
	"fmt"
	"strings"
)

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
