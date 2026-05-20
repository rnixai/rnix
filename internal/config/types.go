package config

import "github.com/rnixai/rnix/internal/types"

// Scope represents a configuration scope layer.
type Scope int

const (
	// ScopeGlobal represents the global (user-level) configuration scope.
	// Paths resolve under $XDG_CONFIG_HOME/rnix/ or ~/.config/rnix/.
	ScopeGlobal Scope = iota

	// ScopeProject represents the project-level configuration scope.
	// Paths resolve under <project-root>/.rnix/.
	ScopeProject
)

// GlobalConfig holds resolved global configuration state.
// All pointer fields point to newly allocated copies; they never share
// underlying data with other config instances.
type GlobalConfig struct {
	Dir        string // Absolute path to global config directory
	Providers  any    // *llm.ProvidersConfig (typed as any to avoid circular imports)
	Config     any    // Parsed global rnix config
	MCP        any    // MCP configuration
	AgentsDir  string // Global agents directory path
	SkillsDir  string // Global skills directory path
}

// ProjectConfig holds resolved project-level configuration state.
// Value-type fields are used where possible; pointer fields point to
// newly allocated copies to ensure immutability.
type ProjectConfig struct {
	ProjectDir  string   // Absolute path to the project root (parent of .rnix/)
	Providers   any      // *llm.ProvidersConfig (typed as any to avoid circular imports)
	Config      any      // Parsed project rnix config
	MCP         any      // MCP configuration
	AgentDirs   []string // Agent search directories in priority order (project first)
	SkillDirs   []string // Skill search directories in priority order (project first)
	InitConfig  any      // Parsed rnix-init.yaml
	ComposeSpec any      // Parsed rnix-compose.yaml

	EnvSnapshot     map[string]string   // .env file vars only (not full os.Environ)
	LLMFileOpener   types.LLMFileOpener // nil = use global VFS Open
	DefaultProvider string              // Merged providers' resolved default; "" = use global

	// AgentLoader, when non-nil, supersedes the daemon's global agent loader
	// for sub-agent Spawn actions emitted by processes in this project. Wired
	// up by ipc/server_spawn.go:resolveProjectContext so that `.rnix/agents/`
	// overrides are honored by ActionSpawn — without this, kernel.tool_exec
	// would fall back to ~/.config/rnix/agents/ and silently ignore the
	// project-level override. Returns any (actually *agents.AgentInfo).
	AgentLoader types.AgentLoaderFunc

	// SkillLoader, when non-nil, supersedes the daemon's global skill loader
	// for ActionSpecialize. Mirrors AgentLoader for the skill lookup path.
	// Returns any (actually *skills.SkillInfo).
	SkillLoader types.SkillLoaderFunc
}
