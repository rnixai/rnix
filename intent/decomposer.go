package intent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// LLMCaller abstracts LLM invocation for intent decomposition.
type LLMCaller interface {
	Call(ctx context.Context, prompt string, model string) (string, error)
}

// Decomposer breaks a high-level intent into sub-intents using an LLM.
type Decomposer struct {
	llmDriver LLMCaller
}

// NewDecomposer creates a Decomposer with the given LLM caller.
func NewDecomposer(caller LLMCaller) *Decomposer {
	return &Decomposer{llmDriver: caller}
}

const decomposePromptTemplate = `你是一个任务规划系统。请将以下高层意图分解为具体子任务。

意图: %s

要求:
1. 每个子任务用 JSON 对象表示，包含 id（短标识符）、intent（具体任务描述）、depends_on（依赖的子任务 id 列表，无依赖则为空数组）
2. 子任务粒度适中——每个子任务应能由单个智能体独立完成
3. 正确声明依赖关系——有数据流依赖的任务必须声明 depends_on
4. 返回纯 JSON 数组，不要包含其他文本

示例输出:
[
  {"id": "design", "intent": "设计数据模型和 API 接口", "depends_on": []},
  {"id": "backend", "intent": "实现后端 API 服务", "depends_on": ["design"]},
  {"id": "frontend", "intent": "实现前端界面", "depends_on": ["design"]},
  {"id": "test", "intent": "编写集成测试", "depends_on": ["backend", "frontend"]}
]`

type llmDecomposeNode struct {
	ID        string   `json:"id"`
	Intent    string   `json:"intent"`
	Agent     string   `json:"agent,omitempty"`
	DependsOn []string `json:"depends_on"`
}

// Decompose calls the LLM to break an intent into an IntentTree.
func (d *Decomposer) Decompose(ctx context.Context, intent string, model string) (*IntentTree, error) {
	prompt := fmt.Sprintf(decomposePromptTemplate, intent)

	response, err := d.llmDriver.Call(ctx, prompt, model)
	if err != nil {
		return nil, fmt.Errorf("decompose: LLM call failed: %w", err)
	}

	var nodes []llmDecomposeNode
	if err := json.Unmarshal([]byte(response), &nodes); err != nil {
		return nil, fmt.Errorf("decompose: invalid JSON from LLM: %w", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("decompose: LLM returned empty task list")
	}

	tree := &IntentTree{
		RootIntent: intent,
		State:      IntentAwaitConfirm,
		Nodes:      make(map[string]*IntentNode, len(nodes)),
		CreatedAt:  time.Now(),
	}

	for _, n := range nodes {
		deps := n.DependsOn
		if deps == nil {
			deps = []string{}
		}
		tree.Nodes[n.ID] = &IntentNode{
			ID:        n.ID,
			Intent:    n.Intent,
			Agent:     n.Agent,
			DependsOn: deps,
			State:     IntentPending,
		}
	}

	if _, err := BuildIntentDAG(tree); err != nil {
		return nil, fmt.Errorf("decompose: %w", err)
	}

	tree.InitDesired()

	return tree, nil
}
