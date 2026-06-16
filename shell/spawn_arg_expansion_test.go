package shell

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
)

// ============================================================
// spawn --agent/--model 变量严格展开
// Spec: spec-agentshell-spawn-agent-model-var-expansion.md
//
// 覆盖 I/O 矩阵：变量展开 / 未定义变量报错 / 无变量回归 /
// spawn 结果复用 / pipeline stage / parallel 块 / on-error 分支 /
// ScriptSpawn 事件 payload。
// ============================================================

// --- 变量展开：SpawnAndWait 收到展开后的 agent/model ---

func TestSpawnArgExpansion_AgentModelVars(t *testing.T) {
	spawner := &mockSpawner{results: []mockResult{{result: "ok", exitCode: 0}}}
	script, err := ParseScript(`
export m=foo
spawn "x" --agent=sa-$m --model=$m-bar
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawner.calls))
	}
	if spawner.calls[0].agent != "sa-foo" {
		t.Errorf("agent = %q, want %q", spawner.calls[0].agent, "sa-foo")
	}
	if spawner.calls[0].model != "foo-bar" {
		t.Errorf("model = %q, want %q", spawner.calls[0].model, "foo-bar")
	}
}

// --- 未定义变量：执行报错含行号，spawn 不发起 ---

func TestSpawnArgExpansion_UndefinedModelVar_ErrorsBeforeSpawn(t *testing.T) {
	spawner := &mockSpawner{}
	script, err := ParseScript(`spawn "x" --model=$nope`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	_, execErr := exec.Execute(context.Background(), script)
	if execErr == nil {
		t.Fatal("expected error for undefined $nope, got nil")
	}
	if !strings.Contains(execErr.Error(), "line 1") {
		t.Errorf("error should contain line number, got: %v", execErr)
	}
	if !strings.Contains(execErr.Error(), "--model") {
		t.Errorf("error should mention --model, got: %v", execErr)
	}
	if len(spawner.calls) != 0 {
		t.Errorf("spawn must not fire on expansion error, got %d calls", len(spawner.calls))
	}
}

func TestSpawnArgExpansion_UndefinedAgentVar_ErrorsBeforeSpawn(t *testing.T) {
	spawner := &mockSpawner{}
	script, err := ParseScript(`spawn "x" --agent=$missing`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	_, execErr := exec.Execute(context.Background(), script)
	if execErr == nil {
		t.Fatal("expected error for undefined $missing, got nil")
	}
	if !strings.Contains(execErr.Error(), "--agent") {
		t.Errorf("error should mention --agent, got: %v", execErr)
	}
	if len(spawner.calls) != 0 {
		t.Errorf("spawn must not fire on expansion error, got %d calls", len(spawner.calls))
	}
}

// --- 回归保护：无变量的字面量行为不变 ---

func TestSpawnArgExpansion_LiteralValues_Unchanged(t *testing.T) {
	spawner := &mockSpawner{results: []mockResult{{result: "ok", exitCode: 0}}}
	script, err := ParseScript(`spawn "x" --agent=sa-toolbox --model=deepseek-v4-flash`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if spawner.calls[0].agent != "sa-toolbox" || spawner.calls[0].model != "deepseek-v4-flash" {
		t.Errorf("literal passthrough broken: agent=%q model=%q",
			spawner.calls[0].agent, spawner.calls[0].model)
	}
}

// --- spawn 结果变量复用：--result-last-line 后值可作为 --model ---

func TestSpawnArgExpansion_ResultVarReuse(t *testing.T) {
	spawner := &mockSpawner{results: []mockResult{
		{result: "thinking...\nverbose output\ndeepseek-v4\n", exitCode: 0},
		{result: "done", exitCode: 0},
	}}
	script, err := ParseScript(`
x = spawn "pick model" --result-last-line
spawn "y" --model=$x
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(spawner.calls) != 2 {
		t.Fatalf("expected 2 spawn calls, got %d", len(spawner.calls))
	}
	if spawner.calls[1].model != "deepseek-v4" {
		t.Errorf("model = %q, want clean last line %q", spawner.calls[1].model, "deepseek-v4")
	}
}

// --- pipeline：各 stage 的 agent/model 均展开 ---

func TestSpawnArgExpansion_PipelineStages(t *testing.T) {
	spawner := &mockSpawner{results: []mockResult{
		{result: "a", exitCode: 0},
		{result: "b", exitCode: 0},
	}}
	script, err := ParseScript(`
export m=fast
spawn "s1" --model=$m | spawn "s2" --agent=sa-$m
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(spawner.calls) != 2 {
		t.Fatalf("expected 2 spawn calls, got %d", len(spawner.calls))
	}
	if spawner.calls[0].model != "fast" {
		t.Errorf("stage 1 model = %q, want %q", spawner.calls[0].model, "fast")
	}
	if spawner.calls[1].agent != "sa-fast" {
		t.Errorf("stage 2 agent = %q, want %q", spawner.calls[1].agent, "sa-fast")
	}
}

func TestSpawnArgExpansion_PipelineUndefinedVar_FailsWholePipeline(t *testing.T) {
	spawner := &mockSpawner{}
	script, err := ParseScript(`spawn "s1" | spawn "s2" --agent=$ghost`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	_, execErr := exec.Execute(context.Background(), script)
	if execErr == nil {
		t.Fatal("expected pipeline failure for undefined $ghost")
	}
	if !strings.Contains(execErr.Error(), "--agent") {
		t.Errorf("error should mention --agent, got: %v", execErr)
	}
	if !strings.Contains(execErr.Error(), "stage 2") {
		t.Errorf("error should identify failing stage, got: %v", execErr)
	}
	if len(spawner.calls) != 0 {
		t.Errorf("no stage may spawn when expansion fails, got %d calls", len(spawner.calls))
	}
}

// --- on-error 分支：fallback spawn 使用展开后的 agent/model ---

func TestSpawnArgExpansion_OnErrorAgentModel(t *testing.T) {
	spawner := &mockSpawner{results: []mockResult{
		{result: "boom", exitCode: 1},
		{result: "recovered", exitCode: 0},
	}}
	script, err := ParseScript(`
export fb=rescue
spawn "main task" on-error spawn "fix it" --agent=$fb --model=$fb-model
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(spawner.calls) != 2 {
		t.Fatalf("expected 2 spawn calls (main + on-error), got %d", len(spawner.calls))
	}
	if spawner.calls[1].agent != "rescue" {
		t.Errorf("on-error agent = %q, want %q", spawner.calls[1].agent, "rescue")
	}
	if spawner.calls[1].model != "rescue-model" {
		t.Errorf("on-error model = %q, want %q", spawner.calls[1].model, "rescue-model")
	}
}

// --- ScriptSpawn 事件 payload：agent/model 为展开后的值 ---

func TestSpawnArgExpansion_EventPayloadExpanded(t *testing.T) {
	spawner := &mockSpawner{results: []mockResult{{result: "ok", exitCode: 0}}}
	script, err := ParseScript(`
export m=glm5
spawn "x" --agent=sa-$m --model=$m
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rec := &onEventRecorder{}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	exec.OnEvent = rec.record
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	spawnEvents := rec.filterByKind(ScriptSpawn)
	if len(spawnEvents) != 1 {
		t.Fatalf("expected 1 ScriptSpawn event, got %d", len(spawnEvents))
	}
	meta := spawnEvents[0].Meta
	if meta["agent"] != "sa-glm5" {
		t.Errorf("event agent = %v, want %q", meta["agent"], "sa-glm5")
	}
	if meta["model"] != "glm5" {
		t.Errorf("event model = %v, want %q", meta["model"], "glm5")
	}
}

// --- parallel 块：worker 收到展开值，事件 payload 同步展开 ---

func TestSpawnArgExpansion_ParallelBlock(t *testing.T) {
	spawner := &concurrentMockSpawner{}
	script, err := ParseScript(`
export m=par
parallel
  spawn "p1" --model=$m
  spawn "p2" --agent=sa-$m
end
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	var evAgents, evModels syncSlice
	exec.OnEvent = func(ev ScriptEvent) {
		if ev.Kind == ScriptSpawn {
			evAgents.append(toStr(ev.Meta["agent"]))
			evModels.append(toStr(ev.Meta["model"]))
		}
	}
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	calls := spawner.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 parallel spawn calls, got %d", len(calls))
	}
	seen := map[string]bool{}
	for _, c := range calls {
		if c.model == "par" {
			seen["model"] = true
		}
		if c.agent == "sa-par" {
			seen["agent"] = true
		}
	}
	if !seen["model"] || !seen["agent"] {
		t.Errorf("parallel spawns missing expanded values: %+v", calls)
	}
	// 事件 payload 也必须是展开值（不得出现 $m 字面量）
	for _, v := range append(evAgents.snapshot(), evModels.snapshot()...) {
		if strings.Contains(v, "$") {
			t.Errorf("parallel ScriptSpawn payload contains unexpanded value %q", v)
		}
	}
	if !containsStr(evModels.snapshot(), "par") || !containsStr(evAgents.snapshot(), "sa-par") {
		t.Errorf("parallel ScriptSpawn payload missing expanded values: agents=%v models=%v",
			evAgents.snapshot(), evModels.snapshot())
	}
}

// syncSlice 是 parallel 事件回调用的最小线程安全字符串切片
// （OnEvent 可能从 worker goroutine 并发触发）。
type syncSlice struct {
	mu sync.Mutex
	v  []string
}

func (s *syncSlice) append(x string) {
	s.mu.Lock()
	s.v = append(s.v, x)
	s.mu.Unlock()
}

func (s *syncSlice) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]string, len(s.v))
	copy(cp, s.v)
	return cp
}

func toStr(v any) string {
	s, _ := v.(string)
	return s
}

func containsStr(list []string, want string) bool {
	return slices.Contains(list, want)
}

func TestSpawnArgExpansion_ParallelUndefinedVar_FailsBeforeLaunch(t *testing.T) {
	spawner := &concurrentMockSpawner{}
	script, err := ParseScript(`
parallel
  spawn "p1" --model=$void
end
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	_, execErr := exec.Execute(context.Background(), script)
	if execErr == nil {
		t.Fatal("expected error for undefined $void in parallel block")
	}
	if !strings.Contains(execErr.Error(), "--model") {
		t.Errorf("error should mention --model, got: %v", execErr)
	}
	if len(spawner.getCalls()) != 0 {
		t.Error("no parallel task may spawn when Phase A expansion fails")
	}
}

// ============================================================
// spawn --provider / --fallback-provider / --fallback-model
// Spec: spec-spawn-provider-flags.md
//
// 沿 agent/model 透传链平行加 provider 通道。覆盖 I/O 矩阵：
// 字面量 / 变量展开 / 未定义报错 / 回归 / fallback flags /
// on-error / pipeline / parallel 站点。
// ============================================================

// --- 字面量 provider 三元组透传到 SpawnRequest ---

func TestSpawnProviderFlags_LiteralPassthrough(t *testing.T) {
	spawner := &mockSpawner{results: []mockResult{{result: "ok", exitCode: 0}}}
	script, err := ParseScript(
		`spawn "x" --agent=a --provider=deepseek --model=deepseek-v4-flash --fallback-provider=anthropic --fallback-model=claude-x`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawner.calls))
	}
	c := spawner.calls[0]
	if c.provider != "deepseek" {
		t.Errorf("provider = %q, want %q", c.provider, "deepseek")
	}
	if c.model != "deepseek-v4-flash" {
		t.Errorf("model = %q, want %q", c.model, "deepseek-v4-flash")
	}
	if c.fallbackProvider != "anthropic" {
		t.Errorf("fallbackProvider = %q, want %q", c.fallbackProvider, "anthropic")
	}
	if c.fallbackModel != "claude-x" {
		t.Errorf("fallbackModel = %q, want %q", c.fallbackModel, "claude-x")
	}
}

// --- 变量展开：--provider=$p 等三个 flag 均展开 ---

func TestSpawnProviderFlags_VarExpansion(t *testing.T) {
	spawner := &mockSpawner{results: []mockResult{{result: "ok", exitCode: 0}}}
	script, err := ParseScript(`
export p=deepseek
export fp=anthropic
export fm=claude-x
spawn "x" --provider=$p --fallback-provider=$fp --fallback-model=$fm
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	c := spawner.calls[0]
	if c.provider != "deepseek" || c.fallbackProvider != "anthropic" || c.fallbackModel != "claude-x" {
		t.Errorf("expanded provider triple mismatch: %+v", c)
	}
}

// --- 未定义变量：执行报错含行号 + flag 名，spawn 不发起 ---

func TestSpawnProviderFlags_UndefinedProviderVar_ErrorsBeforeSpawn(t *testing.T) {
	spawner := &mockSpawner{}
	script, err := ParseScript(`spawn "x" --provider=$nope`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	_, execErr := exec.Execute(context.Background(), script)
	if execErr == nil {
		t.Fatal("expected error for undefined $nope, got nil")
	}
	if !strings.Contains(execErr.Error(), "line 1") {
		t.Errorf("error should contain line number, got: %v", execErr)
	}
	if !strings.Contains(execErr.Error(), "--provider") {
		t.Errorf("error should mention --provider, got: %v", execErr)
	}
	if len(spawner.calls) != 0 {
		t.Errorf("spawn must not fire on expansion error, got %d calls", len(spawner.calls))
	}
}

func TestSpawnProviderFlags_UndefinedFallbackModelVar_ErrorsBeforeSpawn(t *testing.T) {
	spawner := &mockSpawner{}
	script, err := ParseScript(`spawn "x" --fallback-model=$gone`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	_, execErr := exec.Execute(context.Background(), script)
	if execErr == nil {
		t.Fatal("expected error for undefined $gone, got nil")
	}
	if !strings.Contains(execErr.Error(), "--fallback-model") {
		t.Errorf("error should mention --fallback-model, got: %v", execErr)
	}
	if len(spawner.calls) != 0 {
		t.Errorf("spawn must not fire on expansion error, got %d calls", len(spawner.calls))
	}
}

// --- 回归：不传 provider flag 时三字段为空，行为不变 ---

func TestSpawnProviderFlags_OmittedFlags_EmptyAndUnchanged(t *testing.T) {
	spawner := &mockSpawner{results: []mockResult{{result: "ok", exitCode: 0}}}
	script, err := ParseScript(`spawn "x" --agent=a --model=m`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	c := spawner.calls[0]
	if c.provider != "" || c.fallbackProvider != "" || c.fallbackModel != "" {
		t.Errorf("omitted provider flags must stay empty, got %+v", c)
	}
	if c.agent != "a" || c.model != "m" {
		t.Errorf("agent/model regression: %+v", c)
	}
}

// --- on-error 站点：fallback spawn 透传 provider ---

func TestSpawnProviderFlags_OnErrorSite(t *testing.T) {
	spawner := &mockSpawner{results: []mockResult{
		{result: "boom", exitCode: 1},
		{result: "recovered", exitCode: 0},
	}}
	script, err := ParseScript(
		`spawn "main" on-error spawn "fix" --provider=anthropic --fallback-provider=deepseek`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(spawner.calls) != 2 {
		t.Fatalf("expected 2 spawn calls, got %d", len(spawner.calls))
	}
	if spawner.calls[1].provider != "anthropic" {
		t.Errorf("on-error provider = %q, want %q", spawner.calls[1].provider, "anthropic")
	}
	if spawner.calls[1].fallbackProvider != "deepseek" {
		t.Errorf("on-error fallbackProvider = %q, want %q", spawner.calls[1].fallbackProvider, "deepseek")
	}
}

// --- pipeline 站点：各 stage 的 provider 均透传 ---

func TestSpawnProviderFlags_PipelineStages(t *testing.T) {
	spawner := &mockSpawner{results: []mockResult{
		{result: "a", exitCode: 0},
		{result: "b", exitCode: 0},
	}}
	script, err := ParseScript(
		`spawn "s1" --provider=deepseek | spawn "s2" --provider=anthropic`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(spawner.calls) != 2 {
		t.Fatalf("expected 2 spawn calls, got %d", len(spawner.calls))
	}
	if spawner.calls[0].provider != "deepseek" {
		t.Errorf("stage 1 provider = %q, want %q", spawner.calls[0].provider, "deepseek")
	}
	if spawner.calls[1].provider != "anthropic" {
		t.Errorf("stage 2 provider = %q, want %q", spawner.calls[1].provider, "anthropic")
	}
}

// --- parallel 站点：worker 收到展开后的 provider，事件 payload 同步 ---

func TestSpawnProviderFlags_ParallelBlock(t *testing.T) {
	spawner := &concurrentMockSpawner{}
	script, err := ParseScript(`
export p=par-prov
parallel
  spawn "p1" --provider=$p
  spawn "p2" --provider=other
end
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	exec := NewScriptExecutor(spawner, NewEnvironment())
	if _, err := exec.Execute(context.Background(), script); err != nil {
		t.Fatalf("execute: %v", err)
	}
	calls := spawner.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 parallel spawn calls, got %d", len(calls))
	}
	seen := map[string]bool{}
	for _, c := range calls {
		seen[c.provider] = true
	}
	if !seen["par-prov"] || !seen["other"] {
		t.Errorf("parallel spawns missing expanded providers: %+v", calls)
	}
}
