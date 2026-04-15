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
			if stmt.Spawn.ResultLastLine {
				res = extractLastLine(res)
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
				if stmt.OnError.ResultLastLine {
					hRes = extractLastLine(hRes)
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

			// When result is assigned to a variable, the script handles errors
			// via the variable — don't let LastExitCode leak to loop break checks.
			if stmt.Assign != "" {
				result.LastExitCode = 0
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
				if stmt.OnError.ResultLastLine {
					hRes = extractLastLine(hRes)
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
				if err == nil && t.stmt.Spawn.ResultLastLine {
					res = extractLastLine(res)
				}
				if exitCode != 0 && t.stmt.OnError != nil && err == nil {
					hRes, hExitCode, hTokens, hErr := e.spawner.SpawnAndWait(ctx, t.expandedOnError, t.stmt.OnError.Agent, t.stmt.OnError.Model)
					if hErr == nil {
						if t.stmt.OnError.ResultLastLine {
							hRes = extractLastLine(hRes)
						}
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
