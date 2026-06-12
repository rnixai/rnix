package shell

import (
	"fmt"
	"strings"
)

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
