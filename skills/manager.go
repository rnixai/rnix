package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/rnixai/rnix/kernel/memory"
)

// SkillManager handles runtime skill create/patch/delete operations.
// Operations are scoped to the project-level .rnix/skills/ directory.
type SkillManager struct {
	projectSkillDir string // absolute path to .rnix/skills/
}

// NewSkillManager creates a new SkillManager.
func NewSkillManager(projectSkillDir string) *SkillManager {
	return &SkillManager{projectSkillDir: projectSkillDir}
}

// Create writes a new SKILL.md file after triple validation.
func (m *SkillManager) Create(name, description, allowedTools, body string, callerDevices []string) error {
	if err := validateSkillName(name); err != nil {
		return err
	}

	skillDir := filepath.Join(m.projectSkillDir, name)
	if _, err := os.Stat(skillDir); err == nil {
		return fmt.Errorf("skill %q already exists", name)
	}

	if err := m.tripleValidation(name, description, allowedTools, body, callerDevices); err != nil {
		return err
	}

	content := renderSkillFile(name, description, allowedTools, body)
	return m.writeSkillFile(name, content)
}

// Patch updates the body of an existing SKILL.md, preserving frontmatter.
func (m *SkillManager) Patch(name, newBody string, callerDevices []string) error {
	if err := validateSkillName(name); err != nil {
		return err
	}

	skillPath := filepath.Join(m.projectSkillDir, name, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("skill %q not found", name)
		}
		return fmt.Errorf("read skill %q: %w", name, err)
	}

	frontmatter, _, err := parseSKILLMD(string(data), false)
	if err != nil {
		return fmt.Errorf("parse existing skill: %w", err)
	}

	var manifest SkillManifest
	if err := yaml.Unmarshal([]byte(frontmatter), &manifest); err != nil {
		return fmt.Errorf("parse frontmatter: %w", err)
	}

	if err := m.tripleValidation(manifest.Name, manifest.Description, manifest.AllowedToolsRaw, newBody, callerDevices); err != nil {
		return err
	}

	content := renderSkillFile(manifest.Name, manifest.Description, manifest.AllowedToolsRaw, newBody)
	return m.writeSkillFile(name, content)
}

// Delete removes a skill directory. Only project-level skills can be deleted.
func (m *SkillManager) Delete(name string) error {
	if err := validateSkillName(name); err != nil {
		return err
	}

	skillDir := filepath.Join(m.projectSkillDir, name)
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return fmt.Errorf("skill %q not found", name)
	}

	return os.RemoveAll(skillDir)
}

// CreateSkill implements the SkillWriter interface for writeback integration.
// It creates a skill without permission boundary checks (system-level operation).
func (m *SkillManager) CreateSkill(name, description, allowedTools, body string) error {
	return m.Create(name, description, allowedTools, body, nil)
}

// tripleValidation performs security scan + frontmatter validation + permission boundary check.
func (m *SkillManager) tripleValidation(name, description, allowedTools, body string, callerDevices []string) error {
	// 1. Security scan (reuse kernel/memory scanner)
	fullContent := body
	if description != "" {
		fullContent = description + "\n" + body
	}
	if result := memory.ScanContent(fullContent); result.Rejected {
		return fmt.Errorf("security scan rejected: %s", result.Reason)
	}

	// 2. Frontmatter validation
	if err := validateFrontmatter(name, description, allowedTools); err != nil {
		return fmt.Errorf("frontmatter validation: %w", err)
	}

	// 3. Permission boundary check
	if err := validatePermissions(allowedTools, callerDevices); err != nil {
		return fmt.Errorf("permission boundary: %w", err)
	}

	return nil
}

// validateSkillName rejects names with path traversal characters.
func validateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("invalid skill name %q: path traversal not allowed", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid skill name %q: path separator not allowed", name)
	}
	return nil
}

// knownToolNames is the canonical semantic tool-name vocabulary accepted by
// validateFrontmatter (Story 54.1, Decision 45 revision ②). allowed-tools may
// now name a tool directly (e.g. `Read`) rather than a device path, so
// enforcement can be tool-level (allowed-tools:Read permits Read but denies
// Write though both live under /dev/fs).
//
// The skills package cannot import kernel (kernel imports skills — cycle), so
// this set is a curated mirror of the tool names registered across the VFS
// device drivers + kernel meta actions. It must track those registries; new
// tools should be added here. Story 54.2 will rename snake_case tool names
// (intent_*, memory_*, skill_*) to PascalCase — add the new names here then.
//
// Device paths (/dev/*, /mnt/mcp/*) remain accepted during the compatibility
// period (see validateFrontmatter); this set only governs the tool-name form.
var knownToolNames = map[string]struct{}{
	// /dev/fs
	"Read": {}, "Write": {}, "Edit": {}, "Glob": {}, "Grep": {},
	// /dev/shell
	"Bash": {},
	// /dev/lsp, /dev/web, /dev/tty
	"LSP": {}, "WebFetch": {}, "WebSearch": {}, "AskUserQuestion": {},
	// /dev/tasks
	"TaskCreate": {}, "TaskGet": {}, "TaskList": {}, "TaskUpdate": {},
	// /dev/cron
	"CronCreate": {}, "CronDelete": {}, "CronList": {},
	// /dev/intent (snake_case until Story 54.2)
	"intent_decompose": {}, "intent_confirm": {}, "intent_execute": {}, "intent_status": {},
	// /dev/memory (snake_case until Story 54.2)
	"memory_commit": {}, "memory_recall": {}, "memory_profile": {},
	// /dev/skills (snake_case until Story 54.2)
	"skill_manage": {}, "skill_registry": {}, "skill_score": {},
	// kernel meta actions
	"Agent": {}, "Skill": {}, "ToolSearch": {}, "EnterPlanMode": {}, "replan": {}, "complete": {},
}

// isKnownToolName reports whether v is a recognized semantic tool name.
func isKnownToolName(v string) bool {
	_, ok := knownToolNames[v]
	return ok
}

// isDevicePath reports whether v is a legacy device-path form of allowed-tools
// (accepted during the Story 54.1 compatibility period).
func isDevicePath(v string) bool {
	return strings.HasPrefix(v, "/dev/") || strings.HasPrefix(v, "/mnt/mcp/")
}

// validateFrontmatter checks name is non-empty, description is non-empty, and
// each allowed_tools entry is either a known semantic tool name (Story 54.1) or
// a legacy device path (compatibility period). Values that are neither are
// rejected.
func validateFrontmatter(name, description, allowedTools string) error {
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	if description == "" {
		return fmt.Errorf("skill description is required")
	}
	for tool := range strings.FieldsSeq(allowedTools) {
		if !isKnownToolName(tool) && !isDevicePath(tool) {
			return fmt.Errorf("invalid allowed-tools entry %q: must be a known tool name (e.g. Read, Bash) or a device path (/dev/ or /mnt/mcp/)", tool)
		}
	}
	return nil
}

// validatePermissions checks that requested tools don't exceed caller's AllowedDevices.
func validatePermissions(allowedTools string, callerDevices []string) error {
	if len(callerDevices) == 0 {
		return nil // unrestricted mode, skip check
	}
	for tool := range strings.FieldsSeq(allowedTools) {
		found := false
		for _, d := range callerDevices {
			if tool == d || strings.HasPrefix(tool, d+"/") || strings.HasPrefix(d, tool+"/") {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("tool %q not in caller's allowed devices %v", tool, callerDevices)
		}
	}
	return nil
}

// renderSkillFile generates the complete SKILL.md content.
func renderSkillFile(name, description, allowedTools, body string) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "name: %s\n", yamlQuoteIfNeeded(name))
	fmt.Fprintf(&sb, "description: %s\n", yamlQuoteIfNeeded(description))
	if allowedTools != "" {
		fmt.Fprintf(&sb, "allowed-tools: %s\n", allowedTools)
	}
	sb.WriteString("---\n\n")
	sb.WriteString(body)
	sb.WriteString("\n")
	return sb.String()
}

// yamlQuoteIfNeeded wraps a string in double quotes if it contains characters
// that would cause YAML parsing issues (colons, braces, brackets, etc.).
func yamlQuoteIfNeeded(s string) string {
	if strings.ContainsAny(s, `:{}[]#&*!|>'"` + "\n") {
		// Escape backslashes and double quotes, then wrap in double quotes
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return s
}

// writeSkillFile creates the skill directory and writes the SKILL.md file.
func (m *SkillManager) writeSkillFile(name, content string) error {
	skillDir := filepath.Join(m.projectSkillDir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write SKILL.md: %w", err)
	}
	return nil
}
