package shell

import (
	"os"
	"strings"
)

// Environment holds shell variables for script execution.
// Not thread-safe — shell execution is sequential (Story 11.1).
type Environment struct {
	vars map[string]string
}

// NewEnvironment creates an empty environment.
func NewEnvironment() *Environment {
	return &Environment{vars: make(map[string]string)}
}

// NewEnvironmentFromOS creates an environment initialized from os.Environ().
func NewEnvironmentFromOS() *Environment {
	env := NewEnvironment()
	for _, entry := range os.Environ() {
		if k, v, ok := strings.Cut(entry, "="); ok {
			env.vars[k] = v
		}
	}
	return env
}

// Set assigns a value to a variable.
func (e *Environment) Set(key, value string) {
	e.vars[key] = value
}

// Get retrieves a variable's value.
func (e *Environment) Get(key string) (string, bool) {
	v, ok := e.vars[key]
	return v, ok
}

// Delete removes a variable.
func (e *Environment) Delete(key string) {
	delete(e.vars, key)
}

// All returns a snapshot copy of all variables.
func (e *Environment) All() map[string]string {
	cp := make(map[string]string, len(e.vars))
	for k, v := range e.vars {
		cp[k] = v
	}
	return cp
}

// Expand performs variable substitution on input.
// Supports $VAR, ${VAR}, and \$ escape syntax.
// Undefined variables expand to empty string (bash default).
func (e *Environment) Expand(input string) string {
	var buf strings.Builder
	i := 0
	for i < len(input) {
		if input[i] == '\\' && i+1 < len(input) && input[i+1] == '$' {
			buf.WriteByte('$')
			i += 2
			continue
		}
		if input[i] == '$' {
			i++
			if i < len(input) && input[i] == '{' {
				i++
				start := i
				for i < len(input) && input[i] != '}' {
					i++
				}
				if i < len(input) {
					name := input[start:i]
					i++
					val := e.vars[name]
					buf.WriteString(val)
				} else {
					buf.WriteString("${")
					buf.WriteString(input[start:])
				}
			} else if i < len(input) && isVarStart(input[i]) {
				start := i
				for i < len(input) && isVarChar(input[i]) {
					i++
				}
				name := input[start:i]
				val := e.vars[name]
				buf.WriteString(val)
			} else {
				buf.WriteByte('$')
			}
			continue
		}
		buf.WriteByte(input[i])
		i++
	}
	return buf.String()
}

func isVarStart(c byte) bool { return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }
func isVarChar(c byte) bool  { return isVarStart(c) || (c >= '0' && c <= '9') }
