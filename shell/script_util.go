package shell

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
