package docs_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func docsDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

func readTutorial(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(docsDir(), "tutorials", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("教程文件不存在: %s", path)
	}
	return string(data)
}

func assertContains(t *testing.T, content, substr, msg string) {
	t.Helper()
	if !strings.Contains(content, substr) {
		t.Errorf("%s: 未找到 %q", msg, substr)
	}
}

func assertContainsAny(t *testing.T, content string, substrs []string, msg string) {
	t.Helper()
	for _, s := range substrs {
		if strings.Contains(content, s) {
			return
		}
	}
	t.Errorf("%s: 未找到任何一个 %v", msg, substrs)
}

// --- 文件存在性测试 ---

func TestTutorialFiles_Exist(t *testing.T) {
	dir := docsDir()
	files := []string{
		"tutorials/README.md",
		"tutorials/writing-first-skill.md",
		"tutorials/debugging-first-bug.md",
		"tutorials/composing-multi-agent-workflow.md",
	}
	for _, f := range files {
		path := filepath.Join(dir, f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("文件应存在: %s", f)
		}
	}
}

// --- AC1: 编写第一个 Skill 教程 ---

func TestWritingFirstSkill_HasRequiredSections(t *testing.T) {
	content := readTutorial(t, "writing-first-skill.md")
	for _, s := range []string{"前置条件", "SKILL.md", "Agent", "运行", "常见问题"} {
		assertContains(t, content, s, "教程应包含章节")
	}
}

func TestWritingFirstSkill_HasSkillMDExample(t *testing.T) {
	content := readTutorial(t, "writing-first-skill.md")
	for _, field := range []string{"name:", "version:", "description:", "tags:", "allowed-tools:"} {
		assertContains(t, content, field, "SKILL.md 示例应包含 frontmatter 字段")
	}
	assertContains(t, content, "allowed-tools", "应解释 allowed-tools 与 VFS 路径映射")
}

func TestWritingFirstSkill_HasAgentYamlExample(t *testing.T) {
	content := readTutorial(t, "writing-first-skill.md")
	for _, field := range []string{"name:", "description:", "models:", "skills:"} {
		assertContains(t, content, field, "agent.yaml 示例应包含字段")
	}
	assertContains(t, content, "instructions", "应包含 instructions.md 说明")
}

func TestWritingFirstSkill_HasCLIExamples(t *testing.T) {
	content := readTutorial(t, "writing-first-skill.md")
	assertContains(t, content, "crux -i", "应包含 crux -i 命令示例")
	assertContains(t, content, "crux ps", "应包含 crux ps 命令示例")
	assertContains(t, content, "crux astrace", "应包含 crux astrace 命令示例")
}

// --- AC2: 调试第一个 bug 教程 ---

func TestDebuggingFirstBug_HasRequiredSections(t *testing.T) {
	content := readTutorial(t, "debugging-first-bug.md")
	lower := strings.ToLower(content)
	for _, s := range []string{"bug", "astrace", "修复", "验证"} {
		if !strings.Contains(lower, strings.ToLower(s)) {
			t.Errorf("教程应包含章节关键词: %s", s)
		}
	}
}

func TestDebuggingFirstBug_HasAstraceOutput(t *testing.T) {
	content := readTutorial(t, "debugging-first-bug.md")
	assertContains(t, content, "crux astrace", "应包含 crux astrace 命令")
	lower := strings.ToLower(content)
	for _, field := range []string{"syscall", "pid", "device"} {
		if !strings.Contains(lower, field) {
			t.Errorf("astrace 输出应展示 SyscallEvent 字段: %s", field)
		}
	}
	assertContainsAny(t, content, []string{"PERMISSION", "ErrCode", "错误码"}, "应展示错误码信息")
}

func TestDebuggingFirstBug_ShowsFixWorkflow(t *testing.T) {
	content := readTutorial(t, "debugging-first-bug.md")
	assertContains(t, content, "/dev/fs", "应展示缺失的 VFS 设备路径")
	hasComparison := strings.Contains(content, "修复前") || strings.Contains(content, "修复后") ||
		strings.Count(content, "tools:") >= 2 || strings.Count(content, "allowed-tools") >= 2
	if !hasComparison {
		t.Error("应展示修复前后对比")
	}
}

// --- AC3: 组合多智能体工作流教程 ---

func TestComposingWorkflow_HasRequiredSections(t *testing.T) {
	content := readTutorial(t, "composing-multi-agent-workflow.md")
	for _, s := range []string{"设计", "crux-compose.yaml", "compose up", "crux top", "结果"} {
		assertContains(t, content, s, "教程应包含章节关键词")
	}
}

func TestComposingWorkflow_HasComposeYamlExample(t *testing.T) {
	content := readTutorial(t, "composing-multi-agent-workflow.md")
	for _, field := range []string{"agents:", "intent:", "agent:", "depends_on:"} {
		assertContains(t, content, field, "compose YAML 示例应包含字段")
	}
}

func TestComposingWorkflow_HasExtendedScenarios(t *testing.T) {
	content := readTutorial(t, "composing-multi-agent-workflow.md")
	if !(strings.Contains(content, "spawn") && strings.Contains(content, "|")) {
		t.Error("应包含管道语法示例")
	}
	assertContainsAny(t, content, []string{"export", "environment"}, "应包含变量/环境传递示例")
	if !(strings.Contains(content, "if") && strings.Contains(content, "else")) {
		t.Error("应包含条件分支示例")
	}
}

// --- 交叉引用测试 ---

func TestTutorials_CrossReferences(t *testing.T) {
	tutorials := []string{
		"writing-first-skill.md",
		"debugging-first-bug.md",
		"composing-multi-agent-workflow.md",
	}
	for _, name := range tutorials {
		content := readTutorial(t, name)
		assertContainsAny(t, content, []string{"concepts.md", "核心概念"}, name+" 应引用概念文档")
		assertContainsAny(t, content, []string{"reference.md", "参考手册"}, name+" 应引用参考手册")
	}
}

// ═══════════════════════════════════════════════════════════
// Story 12-2: 架构文档（Architecture Documentation）
// ═══════════════════════════════════════════════════════════

func readArchDoc(t *testing.T) string {
	t.Helper()
	path := filepath.Join(docsDir(), "architecture.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("架构文档不存在: %s", path)
	}
	return string(data)
}

// --- 文件存在性测试 ---

func TestArchitectureDoc_Exists(t *testing.T) {
	path := filepath.Join(docsDir(), "architecture.md")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("架构文档应存在: %s", path)
	}
}

// --- AC1: 微内核设计 ---

func TestArchitecture_MicrokernelDesign_HasSubInterfaces(t *testing.T) {
	content := readArchDoc(t)
	for _, iface := range []string{
		"ProcessManager", "MountManager", "IPCManager",
		"SignalManager", "ProcGroupManager", "SupervisorManager",
	} {
		assertContains(t, content, iface, "微内核设计应包含子接口")
	}
}

func TestArchitecture_MicrokernelDesign_HasCallbacks(t *testing.T) {
	content := readArchDoc(t)
	assertContains(t, content, "KernelCallbacks", "应包含 KernelCallbacks")
	for _, cb := range []string{"OnSpawn", "OnStep", "OnComplete", "OnError"} {
		assertContains(t, content, cb, "KernelCallbacks 应包含回调方法")
	}
}

func TestArchitecture_MicrokernelDesign_HasDataFlow(t *testing.T) {
	content := readArchDoc(t)
	assertContains(t, content, "Spawn", "数据流应包含 Spawn")
	assertContains(t, content, "reasonStep", "数据流应包含 reasonStep")
	assertContainsAny(t, content, []string{"数据流", "流程", "Data Flow"}, "应有数据流说明")
}

// --- AC2: 进程模型 ---

func TestArchitecture_ProcessModel_HasStateMachine(t *testing.T) {
	content := readArchDoc(t)
	for _, state := range []string{"Created", "Running", "Zombie", "Dead"} {
		assertContains(t, content, state, "状态机应包含状态")
	}
	assertContainsAny(t, content, []string{"PID", "pid"}, "应包含 PID 分配策略")
}

func TestArchitecture_ProcessModel_HasReapSequence(t *testing.T) {
	content := readArchDoc(t)
	assertContainsAny(t, content, []string{"reapProcess", "reap", "资源释放"}, "应包含资源释放说明")
	assertContains(t, content, "CtxFree", "资源释放应包含 CtxFree")
	assertContainsAny(t, content, []string{"orphan", "reparent", "孤儿", "重新指定"}, "应包含孤儿进程处理")
}

func TestArchitecture_ProcessModel_HasConcurrencyModel(t *testing.T) {
	content := readArchDoc(t)
	assertContains(t, content, "Thread", "并发模型应包含 Thread")
	assertContains(t, content, "Coroutine", "并发模型应包含 Coroutine")
	assertContainsAny(t, content, []string{"goroutine", "Goroutine"}, "应包含 goroutine 管理")
}

// --- AC3: 驱动层 ---

func TestArchitecture_DriverLayer_HasDeviceRegistry(t *testing.T) {
	content := readArchDoc(t)
	assertContains(t, content, "DeviceRegistry", "应包含 DeviceRegistry")
	assertContains(t, content, "VFSFileFactory", "应包含 VFSFileFactory")
	assertContains(t, content, "VFSFile", "应包含 VFSFile 接口")
}

func TestArchitecture_DriverLayer_HasLLMDriver(t *testing.T) {
	content := readArchDoc(t)
	assertContains(t, content, "LLMDriver", "应包含 LLMDriver 接口")
	assertContainsAny(t, content, []string{"Call(", "Call("}, "LLMDriver 应包含 Call 方法")
	assertContainsAny(t, content, []string{"LLMRequest", "LLMResponse"}, "应包含请求/响应结构")
}

func TestArchitecture_DriverLayer_HasMCPMount(t *testing.T) {
	content := readArchDoc(t)
	assertContains(t, content, "MCPTransport", "应包含 MCPTransport")
	assertContainsAny(t, content, []string{"Mount(", "Mount("}, "应包含 Mount 方法")
	assertContainsAny(t, content, []string{"Unmount", "unmount"}, "应包含 Unmount")
	assertContains(t, content, "/mnt/mcp/", "应包含 MCP 挂载路径")
}

// --- AC4: 上下文管理 ---

func TestArchitecture_ContextMgmt_HasManagerMethods(t *testing.T) {
	content := readArchDoc(t)
	for _, method := range []string{"CtxAlloc", "CtxFree", "BuildPrompt", "AppendMessage"} {
		assertContains(t, content, method, "上下文管理应包含方法")
	}
}

func TestArchitecture_ContextMgmt_HasTokenBudget(t *testing.T) {
	content := readArchDoc(t)
	assertContains(t, content, "ContextBudget", "应包含 ContextBudget")
	assertContains(t, content, "TokensUsed", "应包含 TokensUsed")
	assertContainsAny(t, content, []string{"budget_exceeded", "预算", "Budget"}, "应包含预算超限说明")
}

// --- 架构文档交叉引用 ---

func TestArchitecture_CrossReferences(t *testing.T) {
	content := readArchDoc(t)
	assertContainsAny(t, content, []string{"concepts.md", "核心概念"}, "应引用概念文档")
	assertContainsAny(t, content, []string{"reference.md", "参考手册"}, "应引用参考手册")
	assertContainsAny(t, content, []string{"tutorials/", "教程"}, "应引用教程")
}
