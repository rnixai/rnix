package shell

import (
	"fmt"
	"strings"
)

// Command represents a single parsed command in a pipeline.
type Command struct {
	Type           string // "spawn"
	Intent         string
	Agent          string
	Model          string
	ResultLastLine bool // --result-last-line: capture only last non-empty line
}

// Pipeline represents a sequence of commands connected by pipes.
type Pipeline struct {
	Commands []Command
}

// ParsePipeline parses a pipeline expression like: spawn "A" | spawn "B" | spawn "C"
func ParsePipeline(input string) (*Pipeline, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("empty pipeline")
	}

	segments := splitPipeline(input)
	var cmds []Command
	for i, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			return nil, fmt.Errorf("stage %d: empty command", i+1)
		}
		cmd, err := parseSpawnCommand(seg)
		if err != nil {
			return nil, fmt.Errorf("stage %d: %w", i+1, err)
		}
		cmds = append(cmds, cmd)
	}
	if len(cmds) == 0 {
		return nil, fmt.Errorf("empty pipeline")
	}
	return &Pipeline{Commands: cmds}, nil
}

// splitPipeline splits input by unquoted '|' characters.
func splitPipeline(input string) []string {
	var segments []string
	var current strings.Builder
	inQuote := false
	var quoteChar byte

	for i := 0; i < len(input); i++ {
		ch := input[i]
		if inQuote {
			current.WriteByte(ch)
			if ch == quoteChar {
				inQuote = false
			}
		} else if ch == '"' || ch == '\'' {
			inQuote = true
			quoteChar = ch
			current.WriteByte(ch)
		} else if ch == '|' {
			segments = append(segments, current.String())
			current.Reset()
		} else {
			current.WriteByte(ch)
		}
	}
	segments = append(segments, current.String())
	return segments
}

func parseSpawnCommand(seg string) (Command, error) {
	tokens, err := tokenize(seg)
	if err != nil {
		return Command{}, err
	}
	if len(tokens) == 0 {
		return Command{}, fmt.Errorf("empty command")
	}

	if !strings.EqualFold(tokens[0], "spawn") {
		return Command{}, fmt.Errorf("expected 'spawn', got %q", tokens[0])
	}

	cmd := Command{Type: "spawn"}
	intentFound := false
	for _, tok := range tokens[1:] {
		if after, ok := strings.CutPrefix(tok, "--agent="); ok {
			cmd.Agent = after
		} else if after, ok := strings.CutPrefix(tok, "--model="); ok {
			cmd.Model = after
		} else if tok == "--result-last-line" {
			cmd.ResultLastLine = true
		} else if !intentFound {
			cmd.Intent = tok
			intentFound = true
		} else {
			return Command{}, fmt.Errorf("unexpected token: %q", tok)
		}
	}

	if !intentFound {
		return Command{}, fmt.Errorf("missing intent after 'spawn'")
	}
	return cmd, nil
}

// tokenize splits a command string into tokens, respecting quoted strings.
func tokenize(input string) ([]string, error) {
	var tokens []string
	i := 0
	for i < len(input) {
		for i < len(input) && (input[i] == ' ' || input[i] == '\t') {
			i++
		}
		if i >= len(input) {
			break
		}

		ch := input[i]
		if ch == '"' || ch == '\'' {
			quoteChar := ch
			i++
			start := i
			for i < len(input) && input[i] != quoteChar {
				i++
			}
			if i >= len(input) {
				return nil, fmt.Errorf("unclosed %c quote", quoteChar)
			}
			tokens = append(tokens, input[start:i])
			i++
		} else {
			start := i
			for i < len(input) && input[i] != ' ' && input[i] != '\t' {
				i++
			}
			tokens = append(tokens, input[start:i])
		}
	}
	return tokens, nil
}
