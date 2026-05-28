package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/drivers/mcp"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// AgentLoaderFunc loads an agent definition by name.
type AgentLoaderFunc func(name string) (*agents.AgentInfo, error)

// InitConfig holds the init bootstrap configuration.
type InitConfig struct {
	Services    []ServiceConfig    `yaml:"services"`
	Supervisors []SupervisorConfig `yaml:"supervisors"`
}

// ServiceConfig describes a service to initialize during bootstrap.
type ServiceConfig struct {
	Name     string         `yaml:"name"`
	Type     string         `yaml:"type"`
	Required bool           `yaml:"required"`
	Config   map[string]any `yaml:"config"`
}

// SupervisorConfig describes a Supervisor tree to build during bootstrap.
type SupervisorConfig struct {
	Name        string        `yaml:"name"`
	Strategy    string        `yaml:"strategy"`
	MaxRestarts int           `yaml:"max_restarts"`
	MaxWindow   time.Duration `yaml:"max_window"`
	Required    bool          `yaml:"required"`
	Children    []ChildConfig `yaml:"children"`
}

// ChildConfig describes a child process within a SupervisorConfig.
type ChildConfig struct {
	Name          string `yaml:"name"`
	Intent        string `yaml:"intent"`
	Agent         string `yaml:"agent"`
	Model         string `yaml:"model"`
	Provider      string `yaml:"provider,omitempty"`
	ContextBudget int    `yaml:"context_budget"`
	Restart       string `yaml:"restart"`
}

// ServiceInitializer defines the interface for init-time service setup.
type ServiceInitializer interface {
	Name() string
	Init(cfg map[string]any) error
}

// InitResult holds the outcome of a bootstrap sequence.
type InitResult struct {
	Started  []string
	Warnings []string
	Failed   []ServiceError
}

// ServiceError records a service initialization failure.
type ServiceError struct {
	Service  string
	Err      error
	Recovery string
}

// serviceRegistry maps service type strings to factory functions.
var serviceRegistry = map[string]func() ServiceInitializer{
	"skill_registry": func() ServiceInitializer { return &skillRegistryService{} },
	"mcp_manager":    func() ServiceInitializer { return &mcpManagerService{} },
	"log_aggregator": func() ServiceInitializer { return &logAggregatorService{} },
}

// LoadInitConfig reads and parses a rnix-init.yaml file.
// If the file does not exist, returns DefaultInitConfig().
func LoadInitConfig(path string) (*InitConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultInitConfig(), nil
		}
		return nil, fmt.Errorf("read init config: %w", err)
	}

	var cfg InitConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse init config: %w", err)
	}
	return &cfg, nil
}

// DefaultInitConfig returns an empty config for when no rnix-init.yaml exists.
func DefaultInitConfig() *InitConfig {
	return &InitConfig{}
}

// Bootstrap executes the init sequence: services first, then supervisors.
// Returns InitResult on success (may contain warnings for optional failures).
// Returns error if any required service or supervisor fails.
func Bootstrap(k *KernelImpl, cfg *InitConfig, agentLoader AgentLoaderFunc) (*InitResult, error) {
	result := &InitResult{}

	// Phase 1: Service initialization (serial, ordered)
	for _, svcCfg := range cfg.Services {
		factory, ok := serviceRegistry[svcCfg.Type]
		if !ok {
			se := ServiceError{
				Service:  svcCfg.Name,
				Err:      fmt.Errorf("unknown service type: %s", svcCfg.Type),
				Recovery: fmt.Sprintf("Check rnix-init.yaml: service %q has invalid type %q", svcCfg.Name, svcCfg.Type),
			}
			if svcCfg.Required {
				result.Failed = append(result.Failed, se)
				return result, fmt.Errorf("required service %q failed: %w\nRecovery: %s", svcCfg.Name, se.Err, se.Recovery)
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("optional service %q: %v", svcCfg.Name, se.Err))
			continue
		}

		svc := factory()
		if err := svc.Init(svcCfg.Config); err != nil {
			se := ServiceError{
				Service:  svcCfg.Name,
				Err:      err,
				Recovery: fmt.Sprintf("Check configuration for service %q (type: %s)", svcCfg.Name, svcCfg.Type),
			}
			if svcCfg.Required {
				result.Failed = append(result.Failed, se)
				return result, fmt.Errorf("required service %q failed: %w\nRecovery: %s", svcCfg.Name, se.Err, se.Recovery)
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("optional service %q: %v", svcCfg.Name, err))
			result.Failed = append(result.Failed, se)
			continue
		}

		// Story 48.3 — type-switch on the just-initialized service to wire
		// registries that kernel needs to expose via its public surface.
		// Avoids broadening ServiceInitializer (which would force
		// skill_registry / log_aggregator to grow no-op kernel injection
		// methods).
		if mm, ok := svc.(*mcpManagerService); ok && mm != nil {
			k.SetMCPRegistry(mm.Servers())
		}

		result.Started = append(result.Started, svcCfg.Name)
	}

	// Phase 2: Supervisor tree construction (serial, ordered)
	var startedSupervisorPIDs []types.PID
	for _, supCfg := range cfg.Supervisors {
		spec, err := supCfg.toSupervisorSpec(agentLoader)
		if err != nil {
			if supCfg.Required {
				rollbackSupervisors(k, startedSupervisorPIDs)
				return result, fmt.Errorf("required supervisor %q failed: %w", supCfg.Name, err)
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("optional supervisor %q: %v", supCfg.Name, err))
			continue
		}

		pid, err := k.SpawnSupervisor(spec)
		if err != nil {
			if supCfg.Required {
				rollbackSupervisors(k, startedSupervisorPIDs)
				return result, fmt.Errorf("required supervisor %q failed: %w", supCfg.Name, err)
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("optional supervisor %q: %v", supCfg.Name, err))
			continue
		}

		// SpawnSupervisor is async — child startup happens in a goroutine.
		// Wait briefly to detect immediate startup failures (e.g., LLM device unavailable).
		if failed, reason := checkSupervisorStartup(k, pid); failed {
			if supCfg.Required {
				rollbackSupervisors(k, startedSupervisorPIDs)
				return result, fmt.Errorf("required supervisor %q failed: %s", supCfg.Name, reason)
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("optional supervisor %q: %s", supCfg.Name, reason))
			continue
		}

		startedSupervisorPIDs = append(startedSupervisorPIDs, pid)
		result.Started = append(result.Started, fmt.Sprintf("supervisor:%s", supCfg.Name))
	}

	return result, nil
}

// checkSupervisorStartup waits briefly for the supervisor goroutine to detect
// immediate startup failures. Returns (true, reason) if the supervisor failed.
func checkSupervisorStartup(k *KernelImpl, pid types.PID) (bool, string) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return true, "supervisor process not found"
	}

	wgDone := make(chan struct{})
	go func() {
		proc.wg.Wait()
		close(wgDone)
	}()

	select {
	case <-wgDone:
		// Goroutine exited — read exit status from Done channel.
		var exit ExitStatus
		select {
		case exit = <-proc.Done:
		default:
			exit = ExitStatus{Code: 1, Reason: "supervisor exited without status"}
		}
		k.Reap(pid)
		if exit.Code != 0 {
			return true, exit.Reason
		}
		// exit.Code == 0: supervisor completed normally (all children finished)
		return false, ""
	case <-time.After(200 * time.Millisecond):
		// Supervisor still running — startup succeeded
		return false, ""
	}
}

// rollbackSupervisors kills and reaps all previously started supervisors.
func rollbackSupervisors(k *KernelImpl, pids []types.PID) {
	for i := len(pids) - 1; i >= 0; i-- {
		if err := k.Kill(pids[i], types.SIGKILL); err != nil {
			fmt.Fprintf(os.Stderr, "[init] rollback: kill PID %d failed: %v\n", pids[i], err)
		}
		proc, ok := k.GetProcess(pids[i])
		if ok {
			proc.wg.Wait()
		}
		k.Reap(pids[i])
	}
}

// toSupervisorSpec converts a SupervisorConfig to a SupervisorSpec.
func (sc *SupervisorConfig) toSupervisorSpec(agentLoader AgentLoaderFunc) (SupervisorSpec, error) {
	spec := SupervisorSpec{
		Strategy:    RestartStrategy(sc.Strategy),
		MaxRestarts: sc.MaxRestarts,
		MaxWindow:   sc.MaxWindow,
	}
	for _, cc := range sc.Children {
		cs := ChildSpec{
			Name:          cc.Name,
			Intent:        cc.Intent,
			Model:         cc.Model,
			Provider:      cc.Provider,
			ContextBudget: cc.ContextBudget,
			Restart:       ChildRestart(cc.Restart),
		}
		if cc.Agent != "" {
			agent, err := agentLoader(cc.Agent)
			if err != nil {
				return SupervisorSpec{}, fmt.Errorf("load agent %q for child %q: %w", cc.Agent, cc.Name, err)
			}
			cs.Agent = agent
		}
		spec.Children = append(spec.Children, cs)
	}
	return spec, nil
}

// === Built-in service implementations ===

// skillRegistryService scans a directory for skill metadata.
type skillRegistryService struct{}

func (s *skillRegistryService) Name() string { return "skill-registry" }

func (s *skillRegistryService) Init(cfg map[string]any) error {
	scanPath := "lib/skills"
	if p, ok := cfg["scan_path"]; ok {
		if sp, ok := p.(string); ok {
			scanPath = sp
		}
	}

	entries, err := os.ReadDir(scanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // empty registry, not an error
		}
		return fmt.Errorf("scan skills directory %q: %w", scanPath, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile := filepath.Join(scanPath, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			continue // skip directories without SKILL.md
		}
	}
	return nil
}

// mcpManagerService loads the global MCP configuration into an in-memory
// map[serverName]→vfs.MCPConfig. Story 48.3 — rewritten from the 28-line
// schema-only stub. Bootstrap type-switches the service post-Init and feeds
// Servers() into kernel.SetMCPRegistry so IPC handleMCPList / handleMCPTest
// can resolve server names without re-parsing mcp.yaml.
//
// The service deliberately does NOT pre-connect any server (avoids slow
// daemon startup); spawn-path Mount continues to be the only place a real
// transport is created in steady-state. The `mcp_test` IPC path uses
// kernel.RunMCPProbe to spin up a one-shot transport using this registry as
// the lookup table.
//
// Naming note: Story Dev Notes §决策 #2 acknowledges this struct is
// effectively `mcp_validator` now, but renaming is epic-out-of-scope.
type mcpManagerService struct {
	// servers is nil before Init succeeds and after Init failures so callers
	// can distinguish "not loaded" from "loaded empty". Bootstrap type-asserts
	// post-Init; if Init failed and Required=false the service stays
	// installed with nil servers, which propagates as a nil registry into
	// kernel — IPC handlers must tolerate that (AC4 §易错点 10).
	servers map[string]vfs.MCPConfig
}

func (s *mcpManagerService) Name() string { return "mcp-manager" }

func (s *mcpManagerService) Init(cfg map[string]any) error {
	// Code review patch P1: clear stale state so a re-Init after failure does
	// not surface a previous run's servers via Servers() / kernel.MCPRegistry().
	s.servers = nil

	// Code review patch P5: default to the global config path (consistent with
	// `cmd/rnix/check.go::resolveMCPConfigPath`) rather than a CWD-relative
	// "mcp.yaml" — daemon CWD is not guaranteed to be the user's project root.
	configPath := defaultMCPConfigPath()
	if p, ok := cfg["config_path"]; ok {
		if cp, ok := p.(string); ok && cp != "" {
			configPath = cp
		}
	}
	if configPath == "" {
		return nil // no resolvable config path, no MCP servers — same shape as IsNotExist
	}

	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			return nil // no MCP config, not an error — servers stays nil
		}
		return fmt.Errorf("stat mcp config %q: %w", configPath, err)
	}

	global, err := mcp.LoadMCPConfig(configPath)
	if err != nil {
		return fmt.Errorf("load mcp config %q: %w", configPath, err)
	}

	// Build the canonical name→vfs.MCPConfig map. ServerName is populated
	// from the yaml key (MCPServerConfig itself has no name field). This
	// matches agentLoader's behaviour at cmd/rnix/main.go:1423.
	registry := make(map[string]vfs.MCPConfig, len(global.Servers))
	for name, server := range global.Servers {
		if server.Command == "" {
			return fmt.Errorf("mcp server %q: command is required", name)
		}
		if server.TransportType != "" && server.TransportType != "stdio" {
			return fmt.Errorf("mcp server %q: transport_type=%q unsupported (only stdio in 48.3)", name, server.TransportType)
		}
		registry[name] = server.ToMCPConfig(name)
	}
	s.servers = registry
	return nil
}

// Servers returns the parsed registry. Bootstrap uses this to inject into
// kernel.SetMCPRegistry; ATDD tests use it to verify Init populated the map.
// Returns nil before Init runs, on Init failure, or when mcp.yaml is absent.
func (s *mcpManagerService) Servers() map[string]vfs.MCPConfig {
	return s.servers
}

// defaultMCPConfigPath returns the canonical mcp.yaml location (mirrors
// `cmd/rnix/check.go::resolveMCPConfigPath`). Returns "" when GlobalDir is
// not resolvable so Init can short-circuit instead of stat()'ing CWD.
func defaultMCPConfigPath() string {
	globalDir, err := config.GlobalDir()
	if err != nil {
		return ""
	}
	return filepath.Join(globalDir, "mcp.yaml")
}

// logAggregatorService initializes the log aggregation channel (no-op placeholder).
type logAggregatorService struct{}

func (s *logAggregatorService) Name() string { return "log-aggregator" }

func (s *logAggregatorService) Init(cfg map[string]any) error {
	return nil // MVP: always succeeds
}
