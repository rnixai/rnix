package shell

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Environment holds shell variables for script execution.
// Not thread-safe — shell execution is sequential (Story 11.1).
type Environment struct {
	vars   map[string]string
	arrays map[string][]string
	maps   map[string]map[string]string
}

// NewEnvironment creates an empty environment.
func NewEnvironment() *Environment {
	return &Environment{
		vars:   make(map[string]string),
		arrays: make(map[string][]string),
		maps:   make(map[string]map[string]string),
	}
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

// Set assigns a string value to a variable, clearing any array/map with the same name.
func (e *Environment) Set(key, value string) {
	e.vars[key] = value
	delete(e.arrays, key)
	delete(e.maps, key)
}

// Get retrieves a variable's string value.
func (e *Environment) Get(key string) (string, bool) {
	v, ok := e.vars[key]
	return v, ok
}

// SetArray assigns an array value, clearing any string/map with the same name.
func (e *Environment) SetArray(key string, arr []string) {
	cp := make([]string, len(arr))
	copy(cp, arr)
	e.arrays[key] = cp
	delete(e.vars, key)
	delete(e.maps, key)
}

// GetArray retrieves an array value.
func (e *Environment) GetArray(key string) ([]string, bool) {
	arr, ok := e.arrays[key]
	if !ok {
		return nil, false
	}
	cp := make([]string, len(arr))
	copy(cp, arr)
	return cp, true
}

// SetMap assigns a map value, clearing any string/array with the same name.
func (e *Environment) SetMap(key string, m map[string]string) {
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	e.maps[key] = cp
	delete(e.vars, key)
	delete(e.arrays, key)
}

// GetMap retrieves a map value.
func (e *Environment) GetMap(key string) (map[string]string, bool) {
	m, ok := e.maps[key]
	if !ok {
		return nil, false
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp, true
}

// GetValueKind returns the storage kind for a key: "string", "array", "map", or "".
func (e *Environment) GetValueKind(key string) string {
	if _, ok := e.vars[key]; ok {
		return "string"
	}
	if _, ok := e.arrays[key]; ok {
		return "array"
	}
	if _, ok := e.maps[key]; ok {
		return "map"
	}
	return ""
}

// Delete removes a variable from all storage maps.
func (e *Environment) Delete(key string) {
	delete(e.vars, key)
	delete(e.arrays, key)
	delete(e.maps, key)
}

// All returns a snapshot copy of string variables only (backward compatible).
func (e *Environment) All() map[string]string {
	cp := make(map[string]string, len(e.vars))
	for k, v := range e.vars {
		cp[k] = v
	}
	return cp
}

// Expand performs variable substitution on input.
// Supports $VAR, ${VAR}, ${VAR[N]} (array index), ${VAR.KEY} (map property),
// and \$ escape syntax. Undefined variables expand to empty string.
func (e *Environment) Expand(input string) string {
	result, _ := e.expand(input, false)
	return result
}

// ExpandStrict performs variable substitution like Expand, but returns an error
// when an undefined variable, out-of-bounds array index, or missing map key is referenced.
func (e *Environment) ExpandStrict(input string) (string, error) {
	return e.expand(input, true)
}

func (e *Environment) expand(input string, strict bool) (string, error) {
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
				depth := 1
				for i < len(input) && depth > 0 {
					if input[i] == '{' {
						depth++
					} else if input[i] == '}' {
						depth--
					}
					if depth > 0 {
						i++
					}
				}
				if depth == 0 {
					expr := input[start:i]
					i++ // skip closing '}'
					val, err := e.resolveExpr(expr, strict)
					if err != nil {
						return "", err
					}
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
				if strict {
					if _, ok := e.vars[name]; !ok {
						if _, ok := e.arrays[name]; !ok {
							if _, ok := e.maps[name]; !ok {
								return "", fmt.Errorf("undefined variable %q", name)
							}
						}
					}
				}
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
	return buf.String(), nil
}

// resolveExpr resolves an expression inside ${...}.
// Supports: VAR, VAR[N] (array index), VAR.KEY (map property).
func (e *Environment) resolveExpr(expr string, strict bool) (string, error) {
	// Check for array index: VAR[N]
	if bracketIdx := strings.IndexByte(expr, '['); bracketIdx >= 0 {
		varName := expr[:bracketIdx]
		rest := expr[bracketIdx:]
		if !strings.HasSuffix(rest, "]") {
			if strict {
				return "", fmt.Errorf("invalid array access syntax in %q", expr)
			}
			return e.vars[expr], nil
		}
		indexStr := rest[1 : len(rest)-1]

		arr, ok := e.arrays[varName]
		if !ok {
			if strict {
				return "", fmt.Errorf("undefined variable %q", varName)
			}
			return e.vars[expr], nil
		}

		// Expand the index if it's a variable reference
		expandedIdx := e.Expand(indexStr)
		idx, err := strconv.Atoi(expandedIdx)
		if err != nil {
			if strict {
				return "", fmt.Errorf("invalid array index %q for %q", indexStr, varName)
			}
			return "", nil
		}
		if idx < 0 || idx >= len(arr) {
			if strict {
				return "", fmt.Errorf("array %q index %d out of range (length %d)", varName, idx, len(arr))
			}
			return "", nil
		}
		return arr[idx], nil
	}

	// Check for map property: VAR.KEY
	if dotIdx := strings.IndexByte(expr, '.'); dotIdx >= 0 {
		varName := expr[:dotIdx]
		key := expr[dotIdx+1:]

		m, ok := e.maps[varName]
		if !ok {
			if strict {
				if e.GetValueKind(varName) == "" {
					return "", fmt.Errorf("undefined variable %q", varName)
				}
				return "", fmt.Errorf("variable %q is not a map", varName)
			}
			return e.vars[expr], nil
		}

		val, ok := m[key]
		if !ok {
			if strict {
				return "", fmt.Errorf("map %q has no key %q", varName, key)
			}
			return "", nil
		}
		return val, nil
	}

	// Simple variable
	if strict {
		if _, ok := e.vars[expr]; !ok {
			if _, ok := e.arrays[expr]; !ok {
				if _, ok := e.maps[expr]; !ok {
					return "", fmt.Errorf("undefined variable %q", expr)
				}
			}
		}
	}
	return e.vars[expr], nil
}

// LenOf returns the length of a variable (array length, map size, or string rune count).
func (e *Environment) LenOf(name string) (int, error) {
	if arr, ok := e.arrays[name]; ok {
		return len(arr), nil
	}
	if m, ok := e.maps[name]; ok {
		return len(m), nil
	}
	if s, ok := e.vars[name]; ok {
		return utf8.RuneCountInString(s), nil
	}
	return 0, fmt.Errorf("undefined variable %q", name)
}

func isVarStart(c byte) bool { return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }
func isVarChar(c byte) bool  { return isVarStart(c) || (c >= '0' && c <= '9') }
