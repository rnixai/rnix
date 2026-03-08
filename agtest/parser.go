package agtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// ParseFile reads and parses a single agtest YAML file.
// The file may contain a single TestCaseSpec or a TestSuiteSpec.
func ParseFile(path string) (*TestSuiteSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read test file %s: %w", path, err)
	}
	suite, err := ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return suite, nil
}

// ParseDir scans a directory for *.yaml files and merges them into a single TestSuiteSpec.
func ParseDir(dir string) (*TestSuiteSpec, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read test directory: %w", err)
	}

	merged := &TestSuiteSpec{Version: "1.0"}
	count := 0
	for _, e := range entries {
		if e.IsDir() || !isYAMLFile(e.Name()) {
			continue
		}
		suite, err := ParseFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		merged.Tests = append(merged.Tests, suite.Tests...)
		count++
	}

	if count == 0 {
		return nil, fmt.Errorf("no .yaml test files found in %s", dir)
	}
	return merged, nil
}

// ParseBytes parses agtest YAML content from bytes.
// Auto-detects whether the input is a single TestCaseSpec or a TestSuiteSpec.
func ParseBytes(data []byte) (*TestSuiteSpec, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("empty test file")
	}

	if isSuiteFormat(data) {
		var suite TestSuiteSpec
		if err := yaml.Unmarshal(data, &suite); err != nil {
			return nil, fmt.Errorf("parse test suite yaml: %w", err)
		}
		if err := Validate(&suite, data); err != nil {
			return nil, err
		}
		return &suite, nil
	}

	var spec TestCaseSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse test case yaml: %w", err)
	}
	suite := &TestSuiteSpec{
		Version: spec.Version,
		Name:    spec.Name,
		Tests:   []TestCaseSpec{spec},
	}
	if err := Validate(suite, data); err != nil {
		return nil, err
	}
	return suite, nil
}

// isSuiteFormat detects whether the YAML data has a top-level "tests" key.
func isSuiteFormat(data []byte) bool {
	var probe map[string]any
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false
	}
	_, ok := probe["tests"]
	return ok
}

func isYAMLFile(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".yaml")
}
