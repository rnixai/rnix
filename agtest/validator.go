package agtest

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// ValidationError represents a single field-level validation failure.
type ValidationError struct {
	Field   string // field path, e.g. "intent" or "tests[0].agent.name"
	Message string
	Line    int // YAML line number (0 = unknown)
}

func (e *ValidationError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s: %s", e.Line, e.Field, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors aggregates multiple validation failures.
type ValidationErrors []ValidationError

func (ve ValidationErrors) Error() string {
	msgs := make([]string, len(ve))
	for i, e := range ve {
		msgs[i] = e.Error()
	}
	return fmt.Sprintf("validation failed (%d errors):\n  %s", len(ve), strings.Join(msgs, "\n  "))
}

// Validate checks a TestSuiteSpec for required fields. rawYAML is used
// to extract line numbers for error reporting (may be nil to skip line info).
func Validate(suite *TestSuiteSpec, rawYAML []byte) error {
	var errs ValidationErrors
	lineMap := buildLineMap(rawYAML)

	version := suite.Version
	if len(suite.Tests) == 1 && suite.Tests[0].Version != "" {
		version = suite.Tests[0].Version
	}
	if version == "" {
		errs = append(errs, ValidationError{
			Field:   "version",
			Message: "required",
			Line:    lineMap.lookup("version"),
		})
	} else if version != "1.0" {
		errs = append(errs, ValidationError{
			Field:   "version",
			Message: fmt.Sprintf("unsupported version %q, only \"1.0\" is supported", version),
			Line:    lineMap.lookup("version"),
		})
	}

	if len(suite.Tests) == 0 {
		errs = append(errs, ValidationError{
			Field:   "tests",
			Message: "at least one test case is required",
			Line:    lineMap.lookup("tests"),
		})
	}

	isSuite := lineMap != nil && lineMap.isSuite

	for i := range suite.Tests {
		tc := &suite.Tests[i]
		prefix := ""
		if isSuite {
			prefix = fmt.Sprintf("tests[%d].", i)
		}

		if tc.Intent == "" {
			errs = append(errs, ValidationError{
				Field:   prefix + "intent",
				Message: "required",
				Line:    lineMap.lookupTest(i, "intent"),
			})
		}
		if tc.Agent.Name == "" {
			errs = append(errs, ValidationError{
				Field:   prefix + "agent.name",
				Message: "required",
				Line:    lineMap.lookupTest(i, "agent"),
			})
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// lineMap caches field-to-line-number mappings extracted from YAML AST.
type lineMap struct {
	topLevel  map[string]int            // top-level key -> line
	testItems []map[string]int          // per-test-item key -> line
	isSuite   bool
}

func (lm *lineMap) lookup(field string) int {
	if lm == nil {
		return 0
	}
	return lm.topLevel[field]
}

func (lm *lineMap) lookupTest(index int, field string) int {
	if lm == nil {
		return 0
	}
	if !lm.isSuite {
		return lm.topLevel[field]
	}
	if index < len(lm.testItems) {
		if line, ok := lm.testItems[index][field]; ok {
			return line
		}
	}
	return 0
}

func buildLineMap(rawYAML []byte) *lineMap {
	if len(rawYAML) == 0 {
		return nil
	}
	lm := &lineMap{
		topLevel: make(map[string]int),
	}

	file, err := parser.ParseBytes(rawYAML, 0)
	if err != nil || len(file.Docs) == 0 {
		return lm
	}

	root := file.Docs[0].Body
	mapping, ok := root.(*ast.MappingNode)
	if !ok {
		return lm
	}

	for _, v := range mapping.Values {
		key := v.Key.String()
		lm.topLevel[key] = v.Key.GetToken().Position.Line

		if key == "tests" {
			lm.isSuite = true
			lm.testItems = extractTestItems(v.Value)
		}
	}

	// For single test case: also store "agent" sub-keys
	if !lm.isSuite {
		extractAgentSubKeys(mapping, lm.topLevel)
	}

	return lm
}

func extractTestItems(node ast.Node) []map[string]int {
	seq, ok := node.(*ast.SequenceNode)
	if !ok {
		return nil
	}
	var items []map[string]int
	for _, item := range seq.Values {
		m := make(map[string]int)
		mapping, ok := item.(*ast.MappingNode)
		if !ok {
			items = append(items, m)
			continue
		}
		for _, v := range mapping.Values {
			m[v.Key.String()] = v.Key.GetToken().Position.Line
		}
		items = append(items, m)
	}
	return items
}

func extractAgentSubKeys(mapping *ast.MappingNode, target map[string]int) {
	for _, v := range mapping.Values {
		if v.Key.String() == "agent" {
			agentMapping, ok := v.Value.(*ast.MappingNode)
			if ok {
				for _, av := range agentMapping.Values {
					target["agent."+av.Key.String()] = av.Key.GetToken().Position.Line
				}
			}
		}
	}
}

