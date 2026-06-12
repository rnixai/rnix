package shell

import (
	"fmt"
	"strings"
)

// countExecutableStages computes the progress denominator (Total) for a script.
// It returns 0 ("untrusted", no denominator shown downstream) when the total
// cannot be statically determined: the script contains a source statement, a
// fn call without a resolvable definition, a recursive fn-call cycle, an
// expansion exceeding MaxCallDepth, or a conditional early return.
func countExecutableStages(script *Script) int {
	if containsSourceStmt(script.Statements) {
		return 0 // sourced script stages are unknown at parse time
	}
	n, trusted, _ := countStagesInBlock(script.Statements, script.Functions, map[string]bool{}, 0)
	if !trusted {
		return 0
	}
	return n
}

// countStagesInBlock counts executable stages in a statement block.
// fns is the script's function table used to expand StmtFnCall bodies;
// visiting tracks fn names on the current expansion path for cycle detection;
// depth guards against exponential expansion of diamond-shaped call chains
// (no cycle, but O(2^n) paths) — mirrors the runtime MaxCallDepth limit.
// trusted=false means the count cannot be statically determined (missing fn
// definition, recursive cycle, depth overflow, or conditional early return)
// and must propagate up to the caller.
// returned=true means the block ends in an unconditional top-level return:
// counting stopped there, which is exact for the block itself; callers that
// embed the block conditionally (If/For/While/Parallel) must degrade to
// untrusted, while FnCall expansion treats it as a normal fn exit.
func countStagesInBlock(stmts []Statement, fns map[string]*FnDef, visiting map[string]bool, depth int) (n int, trusted bool, returned bool) {
	if depth > MaxCallDepth {
		return 0, false, false // expansion too deep — statically unknowable
	}
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
			thenCount, ok, ret := countStagesInBlock(stmt.If.Then, fns, visiting, depth)
			if !ok || ret {
				// conditional return: whether later statements run is unknowable
				return 0, false, false
			}
			elseCount, ok, ret := countStagesInBlock(stmt.If.Else, fns, visiting, depth)
			if !ok || ret {
				return 0, false, false
			}
			n += max(thenCount, elseCount)
		case StmtFor:
			bodyCount, ok, ret := countStagesInBlock(stmt.For.Body, fns, visiting, depth)
			if !ok || ret {
				return 0, false, false
			}
			n += len(stmt.For.List) * bodyCount
		case StmtWhile:
			bodyCount, ok, ret := countStagesInBlock(stmt.While.Body, fns, visiting, depth)
			if !ok || ret {
				return 0, false, false
			}
			n += 10 * bodyCount
		case StmtBuiltin:
			if stmt.Builtin.Command == "wait" {
				n++
			}
		case StmtFnDef:
			// 0 — definition doesn't count as a stage
		case StmtFnCall:
			if _, isBuiltin := builtinFunctions[stmt.FnCall.Name]; isBuiltin {
				break // builtin fns (len/append/keys) don't spawn — 0 stages
			}
			fnDef, ok := fns[stmt.FnCall.Name]
			if !ok || visiting[stmt.FnCall.Name] {
				// missing definition (may live in a sourced file) or
				// recursive cycle — expansion count is statically unknown
				return 0, false, false
			}
			visiting[stmt.FnCall.Name] = true
			// body returned=true is a normal fn exit here, not an early-return leak
			bodyCount, bodyOK, _ := countStagesInBlock(fnDef.Body, fns, visiting, depth+1)
			delete(visiting, stmt.FnCall.Name)
			if !bodyOK {
				return 0, false, false
			}
			n += bodyCount
		case StmtReturn:
			// unconditional return at this block level: nothing after it runs,
			// so the count so far is exact — stop here
			return n, true, true
		case StmtParallel:
			bodyCount, ok, ret := countStagesInBlock(stmt.Parallel.Body, fns, visiting, depth)
			if !ok || ret {
				return 0, false, false
			}
			n += bodyCount
		case StmtArrayLit, StmtMapLit, StmtAssignIndex, StmtAssignProp:
			// 0 — pure assignments, no spawn
		case StmtSource:
			// handled by containsSourceStmt in countExecutableStages;
			// defensive here: source makes the total untrusted
			return 0, false, false
		default:
			// fail-safe: a future StatementKind not handled here degrades to
			// untrusted instead of silently undercounting the denominator
			return 0, false, false
		}
	}
	return n, true, false
}

func expandPipelineCommandsStrict(env *Environment, p *Pipeline) (*Pipeline, error) {
	expanded := &Pipeline{Commands: make([]Command, len(p.Commands))}
	for i, cmd := range p.Commands {
		intent, err := env.ExpandStrict(cmd.Intent)
		if err != nil {
			return nil, fmt.Errorf("stage %d: %w", i+1, err)
		}
		agent, err := env.ExpandStrict(cmd.Agent)
		if err != nil {
			return nil, fmt.Errorf("stage %d: --agent: %w", i+1, err)
		}
		model, err := env.ExpandStrict(cmd.Model)
		if err != nil {
			return nil, fmt.Errorf("stage %d: --model: %w", i+1, err)
		}
		expanded.Commands[i] = Command{
			Type:           cmd.Type,
			Intent:         intent,
			Agent:          agent,
			Model:          model,
			ResultLastLine: cmd.ResultLastLine,
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

// extractLastLine returns the last non-empty trimmed line from s.
// Used by --result-last-line to extract a keyword from verbose LLM output.
func extractLastLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return strings.TrimSpace(s)
}
