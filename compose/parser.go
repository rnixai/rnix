package compose

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

// ParseFile reads and parses a crux-compose.yaml file from the given path.
func ParseFile(path string) (*ComposeSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read compose file: %w", err)
	}
	return ParseBytes(data)
}

// ParseBytes parses crux-compose.yaml content from bytes.
func ParseBytes(data []byte) (*ComposeSpec, error) {
	var spec ComposeSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse compose yaml: %w", err)
	}
	if err := validate(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// validate checks the parsed ComposeSpec for required fields and constraints.
func validate(spec *ComposeSpec) error {
	if spec.Version == "" {
		return fmt.Errorf("compose validation: version is required")
	}
	if spec.Version != "1.0" {
		return fmt.Errorf("compose validation: unsupported version %q, only \"1.0\" is supported", spec.Version)
	}
	if spec.Intent == "" {
		return fmt.Errorf("compose validation: intent is required")
	}
	if len(spec.Agents) == 0 {
		return fmt.Errorf("compose validation: agents must not be empty")
	}
	for name, agent := range spec.Agents {
		if agent.Intent == "" {
			return fmt.Errorf("compose validation: agent %q must have an intent", name)
		}
		for dep, condition := range agent.DependsOn {
			if _, ok := spec.Agents[dep]; !ok {
				return fmt.Errorf("compose validation: agent %q depends_on %q which does not exist", name, dep)
			}
			if condition != "completed" {
				return fmt.Errorf("compose validation: agent %q depends_on %q has unsupported condition %q, only \"completed\" is supported", name, dep, condition)
			}
		}
	}
	return nil
}
