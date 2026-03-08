package agtest

// TestSuiteSpec represents a collection of test cases (single or multi-file).
type TestSuiteSpec struct {
	Version string         `yaml:"version"`
	Name    string         `yaml:"name,omitempty"`
	Tests   []TestCaseSpec `yaml:"tests"`
}

// TestCaseSpec defines a single declarative agent behavior test case.
type TestCaseSpec struct {
	Version string        `yaml:"version,omitempty"`
	Name    string        `yaml:"name,omitempty"`
	Intent  string        `yaml:"intent"`
	Agent   AgentConfig   `yaml:"agent"`
	Timeout int           `yaml:"timeout,omitempty"`
	Skip    bool          `yaml:"skip,omitempty"`
	Assert  *AssertConfig `yaml:"assert,omitempty"`
}

// AgentConfig specifies which agent to use and its configuration overrides.
type AgentConfig struct {
	Name          string   `yaml:"name"`
	Model         string   `yaml:"model,omitempty"`
	Skills        []string `yaml:"skills,omitempty"`
	ContextBudget int      `yaml:"context_budget,omitempty"`
}

// AssertConfig holds all assertion types for a test case (expanded in Story 16-2).
type AssertConfig struct {
	Output   *OutputAssert  `yaml:"output,omitempty"`
	Syscalls *SyscallAssert `yaml:"syscalls,omitempty"`
	Quality  *QualityAssert `yaml:"quality,omitempty"`
}

// OutputAssert checks the agent's final text output.
type OutputAssert struct {
	Contains    []string `yaml:"contains,omitempty"`
	NotContains []string `yaml:"not_contains,omitempty"`
}

// SyscallAssert checks the syscall sequence during execution.
type SyscallAssert struct {
	Includes []string `yaml:"includes,omitempty"`
	Excludes []string `yaml:"excludes,omitempty"`
}

// QualityAssert delegates evaluation to a lightweight LLM judge.
type QualityAssert struct {
	Criteria string `yaml:"criteria,omitempty"`
}
