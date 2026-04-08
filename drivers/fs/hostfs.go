// Package fs implements the host filesystem driver for /dev/fs.
package fs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// HostFSDriver is the driver object for the host filesystem device.
// Implements vfs.ToolDescriptor to describe read_file, write_file, list_dir tools.
type HostFSDriver struct{}

// compile-time interface check
var _ vfs.ToolDescriptor = (*HostFSDriver)(nil)

// NewDriver creates a new HostFSDriver instance.
func NewDriver() *HostFSDriver {
	return &HostFSDriver{}
}

// ToolDefs returns tool definitions for the host filesystem device.
func (d *HostFSDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:              "read_file",
			Description:       loadPrompt("read_file"),
			MaxResultTokens:   25000,
			IsReadOnly:        true,
			IsConcurrencySafe: true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "File path relative to the working directory",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:              "write_file",
			Description:       loadPrompt("write_file"),
			IsConcurrencySafe: true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "File path relative to the working directory",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Content to write to the file",
					},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:              "list_dir",
			Description:       loadPrompt("list_dir"),
			MaxResultTokens:   5000,
			IsReadOnly:        true,
			IsConcurrencySafe: true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Directory path relative to the working directory",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "edit_file",
			Description: loadPrompt("edit_file"),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "File path relative to the working directory",
					},
					"old_string": map[string]any{
						"type":        "string",
						"description": "The exact text to find in the file (must match uniquely)",
					},
					"new_string": map[string]any{
						"type":        "string",
						"description": "The replacement text",
					},
					"replace_all": map[string]any{
						"type":        "boolean",
						"description": "Replace all occurrences (default: false, requires unique match)",
					},
				},
				"required": []string{"path", "old_string", "new_string"},
			},
		},
		{
			Name:              "glob",
			Description:       loadPrompt("glob"),
			MaxResultTokens:   5000,
			IsReadOnly:        true,
			IsConcurrencySafe: true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "Glob pattern to match (e.g., \"**/*.go\", \"src/**/*.ts\")",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Directory to search in (default: working directory)",
					},
					"head_limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results (default: 100)",
					},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:              "grep",
			Description:       loadPrompt("grep"),
			MaxResultTokens:   10000,
			IsReadOnly:        true,
			IsConcurrencySafe: true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "Regular expression pattern to search for",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "File or directory to search in (default: working directory)",
					},
					"output_mode": map[string]any{
						"type":        "string",
						"enum":        []string{"content", "files_with_matches", "count"},
						"description": "Output format (default: content)",
					},
					"case_insensitive": map[string]any{
						"type":        "boolean",
						"description": "Ignore case when matching",
					},
					"glob": map[string]any{
						"type":        "string",
						"description": "File name filter (e.g., \"*.go\")",
					},
					"context": map[string]any{
						"type":        "integer",
						"description": "Number of context lines around matches (only with output_mode: content)",
					},
					"head_limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results",
					},
				},
				"required": []string{"pattern"},
			},
		},
	}
}
func (d *HostFSDriver) FileFactory() vfs.VFSFileFactory {
	return FileFactory()
}

const (
	// maxReadFileTokens is the maximum token count for read_file results.
	maxReadFileTokens = 25000
	// maxListDirEntries is the maximum number of entries returned by list_dir.
	maxListDirEntries = 100
)

// HostFSFile implements vfs.VFSFile for host filesystem file access.
// Supports two modes:
//   - Read mode (O_RDONLY): wraps an os.File for direct reading.
//   - Command mode (O_WRONLY/O_RDWR): Write→Read pattern for write-file and list-directory operations.
type HostFSFile struct {
	file     *os.File // non-nil in read mode
	path     string   // resolved host path
	workDir  string   // sandbox root for command mode
	mode     vfs.OpenFlag
	response []byte // buffered result in command mode
	closed   bool
}

// writeRequest is the JSON payload for /dev/fs command-mode writes.
type writeRequest struct {
	Content *string `json:"content"` // pointer to distinguish absent from empty string
	Op      string  `json:"op"`      // "list", "edit", "glob", "grep"

	// edit_file fields
	OldString  *string `json:"old_string"`  // exact match to find
	NewString  *string `json:"new_string"`  // replacement text
	ReplaceAll bool    `json:"replace_all"` // replace all occurrences (default: first only)

	// glob fields
	Pattern   string `json:"pattern"`    // glob pattern (e.g. "**/*.go")
	HeadLimit int    `json:"head_limit"` // max results (0 = default)

	// grep fields
	// Pattern is shared with glob
	Path            string `json:"path"`             // search scope (file or dir)
	OutputMode      string `json:"output_mode"`      // "files_with_matches", "content", "count"
	CaseInsensitive bool   `json:"case_insensitive"` // ignore case
	Context         int    `json:"context"`           // context lines around match
	Glob            string `json:"glob"`              // file name filter
}

// listEntry is a single entry returned by the list operation.
type listEntry struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"is_dir"`
}

// Read reads up to length bytes from the file (read mode) or returns
// buffered command results (command mode).
func (f *HostFSFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, fmt.Errorf("read from closed hostfs file: %s", f.path)
	}

	// Command mode: return buffered response from Write.
	if f.mode != vfs.O_RDONLY {
		if f.response == nil {
			return nil, &types.DriverError{
				Op: "Read", Device: "/dev/fs", Err: fmt.Errorf("no result: write a command first"), Code: types.ErrDriver,
			}
		}
		data := f.response
		f.response = nil // one-shot read
		return data, nil
	}

	// Read mode: delegate to underlying os.File.
	var data []byte
	var readErr error
	if length <= 0 {
		data, readErr = io.ReadAll(f.file)
	} else {
		buf := make([]byte, length)
		n, err := io.ReadAtLeast(f.file, buf, 1)
		if err != nil && err != io.ErrUnexpectedEOF {
			return nil, err
		}
		data = buf[:n]
		readErr = nil
	}
	if readErr != nil {
		return nil, readErr
	}

	// Apply token-based truncation for read_file results.
	content := string(data)
	truncated, didTruncate := rnixctx.TruncateResult(content, maxReadFileTokens)
	if didTruncate {
		originalTokens := rnixctx.EstimateTokens(content)
		shownTokens := rnixctx.EstimateTokens(truncated)

		var overflowPath string
		if f.workDir != "" {
			overflowPath, _ = rnixctx.WriteOverflow(content, f.workDir)
		}

		truncated += rnixctx.FormatTruncationNotice(originalTokens, shownTokens, overflowPath)
		return []byte(truncated), nil
	}

	return data, nil
}

// Write executes a filesystem command in command mode.
// Accepts JSON: {"content": "..."} to write a file, or {"op": "list"} to list a directory.
func (f *HostFSFile) Write(_ context.Context, data []byte) error {
	if f.closed {
		return fmt.Errorf("write to closed hostfs file: %s", f.path)
	}
	if f.mode == vfs.O_RDONLY {
		return &types.DriverError{Op: "Write", Device: "/dev/fs" + f.path, Err: fmt.Errorf("read-only mode"), Code: types.ErrPermission}
	}

	var req writeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return &types.DriverError{Op: "Write", Device: "/dev/fs", Err: fmt.Errorf("invalid JSON payload: %w", err), Code: types.ErrDriver}
	}

	switch {
	case req.Op == "list":
		return f.execList()
	case req.Op == "edit":
		return f.execEdit(req)
	case req.Op == "glob":
		return f.execGlob(req)
	case req.Op == "grep":
		return f.execGrep(req)
	case req.Content != nil:
		return f.execWrite(*req.Content)
	default:
		return &types.DriverError{Op: "Write", Device: "/dev/fs", Err: fmt.Errorf("unknown operation: payload must contain \"content\", or \"op\" must be one of: list, edit, glob, grep"), Code: types.ErrDriver}
	}
}

// execWrite creates/overwrites a file at f.path with the given content.
func (f *HostFSFile) execWrite(content string) error {
	device := "/dev/fs"

	// Sandbox check: path must stay within workDir.
	if err := f.checkSandbox(); err != nil {
		return err
	}

	// Auto-create parent directories.
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return mapOSError("Write", device, err)
	}

	if err := os.WriteFile(f.path, []byte(content), 0o644); err != nil {
		return mapOSError("Write", device, err)
	}

	f.response = []byte("ok")
	return nil
}

// execEdit performs a string replacement edit on a file.
func (f *HostFSFile) execEdit(req writeRequest) error {
	device := "/dev/fs"

	if req.OldString == nil || req.NewString == nil {
		return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("edit requires \"old_string\" and \"new_string\" fields"), Code: types.ErrDriver}
	}

	if err := f.checkSandbox(); err != nil {
		return err
	}

	data, err := os.ReadFile(f.path)
	if err != nil {
		return mapOSError("Write", device, err)
	}

	content := string(data)
	oldStr := *req.OldString
	newStr := *req.NewString

	if oldStr == "" {
		if newStr != "" {
			// Create new file — equivalent to write_file (AC1: skip read-before-write)
			return f.execWrite(newStr)
		}
		return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("old_string and new_string must not both be empty"), Code: types.ErrDriver}
	}

	count := strings.Count(content, oldStr)
	if count == 0 {
		return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("old_string not found in file"), Code: types.ErrNotFound}
	}

	if !req.ReplaceAll && count > 1 {
		return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("old_string matches %d locations; must be unique (or set replace_all: true)", count), Code: types.ErrDriver}
	}

	var result string
	if req.ReplaceAll {
		result = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		result = strings.Replace(content, oldStr, newStr, 1)
	}

	info, _ := os.Stat(f.path)
	perm := os.FileMode(0o644)
	if info != nil {
		perm = info.Mode().Perm()
	}

	if err := os.WriteFile(f.path, []byte(result), perm); err != nil {
		return mapOSError("Write", device, err)
	}

	replacedCount := 1
	if req.ReplaceAll {
		replacedCount = count
	}
	f.response = fmt.Appendf(nil, "ok: replaced %d occurrence(s)", replacedCount)
	return nil
}
type listDirResult struct {
	Entries []listEntry `json:"entries"`
	Notice  string      `json:"notice,omitempty"`
}

// execList reads the directory at f.path and buffers the result as JSON.
func (f *HostFSFile) execList() error {
	device := "/dev/fs"

	entries, err := os.ReadDir(f.path)
	if err != nil {
		return mapOSError("Write", device, err)
	}

	listed := make([]listEntry, 0, min(len(entries), maxListDirEntries))
	for _, e := range entries {
		if len(listed) >= maxListDirEntries {
			break
		}
		info, err := e.Info()
		if err != nil {
			continue // skip entries we can't stat
		}
		listed = append(listed, listEntry{
			Name:  e.Name(),
			Size:  info.Size(),
			IsDir: e.IsDir(),
		})
	}

	out := listDirResult{Entries: listed}
	if len(entries) > maxListDirEntries {
		out.Notice = fmt.Sprintf("Showing %d of %d entries. Use glob pattern for targeted search.", maxListDirEntries, len(entries))
	}

	b, err := json.Marshal(out)
	if err != nil {
		return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("marshal list result: %w", err), Code: types.ErrDriver}
	}

	f.response = b
	return nil
}

const (
	// maxGlobResults is the default maximum number of glob results.
	maxGlobResults = 100
	// maxGrepFileResults is the default max files returned by grep.
	maxGrepFileResults = 50
	// maxGrepContentLines is the max matching lines returned by grep content mode.
	maxGrepContentLines = 250
)

// globEntry represents a single glob match result.
type globEntry struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir,omitempty"`
	Size  int64  `json:"size,omitempty"`
	Mtime int64  `json:"mtime,omitempty"` // Unix millis
}

// execGlob finds files matching a glob pattern within the sandbox.
func (f *HostFSFile) execGlob(req writeRequest) error {
	device := "/dev/fs"

	if req.Pattern == "" {
		return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("glob requires \"pattern\" field"), Code: types.ErrDriver}
	}

	searchRoot := f.workDir
	if searchRoot == "" {
		searchRoot = f.path
	}
	if req.Path != "" {
		searchRoot = resolvePath("/"+req.Path, f.workDir)
	}

	// Sandbox check on the search root
	if f.workDir != "" {
		absRoot, _ := filepath.Abs(searchRoot)
		absWork, _ := filepath.Abs(f.workDir)
		if !strings.HasPrefix(absRoot, absWork+string(filepath.Separator)) && absRoot != absWork {
			return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("search root outside sandbox"), Code: types.ErrPermission}
		}
	}

	limit := req.HeadLimit
	if limit <= 0 {
		limit = maxGlobResults
	}

	var matches []globEntry
	pattern := req.Pattern

	err := filepath.WalkDir(searchRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible dirs
		}
		if len(matches) >= limit {
			return filepath.SkipAll
		}

		rel, _ := filepath.Rel(searchRoot, p)
		if rel == "." {
			return nil
		}

		// Skip common noise directories
		name := d.Name()
		if d.IsDir() && (name == ".git" || name == "node_modules" || name == "__pycache__" || name == ".rnix") {
			return filepath.SkipDir
		}

		if matchGlob(pattern, rel) {
			entry := globEntry{Path: rel, IsDir: d.IsDir()}
			if info, err := d.Info(); err == nil {
				entry.Size = info.Size()
				entry.Mtime = info.ModTime().UnixMilli()
			}
			matches = append(matches, entry)
		}
		return nil
	})
	if err != nil {
		return mapOSError("Write", device, err)
	}

	// Sort by mtime descending (newest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Mtime > matches[j].Mtime
	})

	type globResult struct {
		Matches []globEntry `json:"matches"`
		Notice  string      `json:"notice,omitempty"`
	}
	out := globResult{Matches: matches}
	if len(matches) >= limit {
		out.Notice = fmt.Sprintf("Results limited to %d entries. Use a more specific pattern.", limit)
	}

	b, err := json.Marshal(out)
	if err != nil {
		return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("marshal glob result: %w", err), Code: types.ErrDriver}
	}
	f.response = b
	return nil
}

// matchGlob matches a path against a glob pattern with ** support.
func matchGlob(pattern, name string) bool {
	// Handle ** (match across directory boundaries)
	if strings.Contains(pattern, "**") {
		parts := strings.SplitN(pattern, "**", 2)
		prefix := parts[0]
		suffix := strings.TrimPrefix(parts[1], "/")
		suffix = strings.TrimPrefix(suffix, string(filepath.Separator))

		if prefix != "" {
			prefix = strings.TrimSuffix(prefix, "/")
			prefix = strings.TrimSuffix(prefix, string(filepath.Separator))
			if !strings.HasPrefix(name, prefix+string(filepath.Separator)) && name != prefix {
				return false
			}
		}

		if suffix == "" {
			return true
		}

		// Check if any suffix of the path matches the remaining pattern
		segments := strings.Split(name, string(filepath.Separator))
		for i := range segments {
			tail := strings.Join(segments[i:], string(filepath.Separator))
			// Recurse to handle chained ** in suffix
			if matchGlob(suffix, tail) {
				return true
			}
			// Also try matching just the filename for patterns like **/*.go
			if i == len(segments)-1 {
				if matched, _ := filepath.Match(suffix, segments[i]); matched {
					return true
				}
			}
		}
		return false
	}

	matched, _ := filepath.Match(pattern, name)
	return matched
}

// grepMatch represents a single grep match.
type grepMatch struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Content string `json:"content,omitempty"`
	Count   int    `json:"count,omitempty"`
}

// execGrep searches file contents for a regex pattern.
func (f *HostFSFile) execGrep(req writeRequest) error {
	device := "/dev/fs"

	if req.Pattern == "" {
		return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("grep requires \"pattern\" field"), Code: types.ErrDriver}
	}

	flags := ""
	if req.CaseInsensitive {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + req.Pattern)
	if err != nil {
		return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("invalid regex pattern: %w", err), Code: types.ErrDriver}
	}

	searchRoot := f.workDir
	if searchRoot == "" {
		searchRoot = f.path
	}

	// If a specific path was provided in the request, resolve it
	if req.Path != "" {
		searchRoot = resolvePath("/"+req.Path, f.workDir)
	}

	// Sandbox check
	if f.workDir != "" {
		absRoot, _ := filepath.Abs(searchRoot)
		absWork, _ := filepath.Abs(f.workDir)
		if !strings.HasPrefix(absRoot, absWork+string(filepath.Separator)) && absRoot != absWork {
			return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("search path outside sandbox"), Code: types.ErrPermission}
		}
	}

	outputMode := req.OutputMode
	if outputMode == "" {
		outputMode = "content"
	}
	switch outputMode {
	case "content", "files_with_matches", "count":
		// valid
	default:
		return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("unrecognized output_mode: %q", outputMode), Code: types.ErrDriver}
	}

	limit := req.HeadLimit
	if limit <= 0 {
		switch outputMode {
		case "content":
			limit = maxGrepContentLines
		default:
			limit = maxGrepFileResults
		}
	}

	var globFilter func(string) bool
	if req.Glob != "" {
		globFilter = func(name string) bool {
			matched, _ := filepath.Match(req.Glob, filepath.Base(name))
			return matched
		}
	}

	var results []grepMatch
	totalMatches := 0

	walkErr := filepath.WalkDir(searchRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "__pycache__" || name == ".rnix" {
				return filepath.SkipDir
			}
			return nil
		}
		if totalMatches >= limit {
			return filepath.SkipAll
		}

		rel, _ := filepath.Rel(searchRoot, p)
		if globFilter != nil && !globFilter(rel) {
			return nil
		}

		// Skip binary files (simple heuristic: check first 512 bytes)
		if isBinaryFile(p) {
			return nil
		}

		switch outputMode {
		case "files_with_matches":
			if fileContainsPattern(p, re) {
				results = append(results, grepMatch{File: rel})
				totalMatches++
			}
		case "count":
			count := countMatches(p, re)
			if count > 0 {
				results = append(results, grepMatch{File: rel, Count: count})
				totalMatches++
			}
		case "content":
			lines := matchLines(p, re, req.Context)
			for _, l := range lines {
				if totalMatches >= limit {
					break
				}
				results = append(results, grepMatch{File: rel, Line: l.Line, Content: l.Content})
				totalMatches++
			}
		}
		return nil
	})
	if walkErr != nil {
		return mapOSError("Write", device, walkErr)
	}

	type grepResult struct {
		Matches []grepMatch `json:"matches"`
		Notice  string      `json:"notice,omitempty"`
	}
	out := grepResult{Matches: results}
	if totalMatches >= limit {
		out.Notice = fmt.Sprintf("Results limited to %d. Use glob filter or more specific pattern.", limit)
	}

	b, err := json.Marshal(out)
	if err != nil {
		return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("marshal grep result: %w", err), Code: types.ErrDriver}
	}
	f.response = b
	return nil
}

// isBinaryFile checks if a file appears to be binary by scanning first 512 bytes for null bytes.
func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true // can't open → skip
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	for i := range n {
		if buf[i] == 0 {
			return true
		}
	}
	return false
}

// fileContainsPattern checks if any line in the file matches the regex.
func fileContainsPattern(path string, re *regexp.Regexp) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)
	for scanner.Scan() {
		if re.MatchString(scanner.Text()) {
			return true
		}
	}
	return false
}

// countMatches returns the number of matching lines in a file.
func countMatches(path string, re *regexp.Regexp) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)
	for scanner.Scan() {
		if re.MatchString(scanner.Text()) {
			count++
		}
	}
	return count
}

// matchedLine represents a matched line for content output mode.
type matchedLine struct {
	Line    int
	Content string
}

// matchLines returns matching lines with optional context from a file.
func matchLines(path string, re *regexp.Regexp, contextLines int) []matchedLine {
	// Clamp negative context
	if contextLines < 0 {
		contextLines = 0
	}

	// Skip files larger than 50MB
	info, err := os.Stat(path)
	if err != nil || info.Size() > 50*1024*1024 {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	const maxLineLen = 500
	var allLines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}

	var results []matchedLine
	included := make(map[int]bool) // track which lines are already included

	for i, line := range allLines {
		if re.MatchString(line) {
			start := max(i-contextLines, 0)
			end := i + contextLines
			if end >= len(allLines) {
				end = len(allLines) - 1
			}
			for j := start; j <= end; j++ {
				if !included[j] {
					included[j] = true
					content := allLines[j]
					if len(content) > maxLineLen {
						content = content[:maxLineLen] + "…"
					}
					results = append(results, matchedLine{Line: j + 1, Content: content})
				}
			}
		}
	}
	return results
}

// checkSandbox ensures f.path does not escape the workDir sandbox.
func (f *HostFSFile) checkSandbox() error {
	if f.workDir == "" {
		return nil // no sandbox when workDir is unset
	}
	absPath, err := filepath.Abs(f.path)
	if err != nil {
		return &types.DriverError{Op: "Write", Device: "/dev/fs", Err: fmt.Errorf("cannot resolve path: %w", err), Code: types.ErrDriver}
	}
	absWorkDir, err := filepath.Abs(f.workDir)
	if err != nil {
		return &types.DriverError{Op: "Write", Device: "/dev/fs", Err: fmt.Errorf("cannot resolve workDir: %w", err), Code: types.ErrDriver}
	}
	if !strings.HasPrefix(absPath, absWorkDir+string(filepath.Separator)) && absPath != absWorkDir {
		return &types.DriverError{
			Op: "Write", Device: "/dev/fs",
			Err:  fmt.Errorf("path outside sandbox: %s is not within %s", absPath, absWorkDir),
			Code: types.ErrPermission,
		}
	}
	return nil
}

// Close closes the underlying os.File (read mode) or releases buffers (command mode).
func (f *HostFSFile) Close() error {
	if f.closed {
		return fmt.Errorf("hostfs file already closed: %s", f.path)
	}
	f.closed = true
	f.response = nil
	if f.file != nil {
		return f.file.Close()
	}
	return nil
}

// Stat returns metadata about this host filesystem file.
func (f *HostFSFile) Stat() (vfs.FileStat, error) {
	if f.closed {
		return vfs.FileStat{}, fmt.Errorf("stat on closed hostfs file: %s", f.path)
	}
	if f.file != nil {
		info, err := f.file.Stat()
		if err != nil {
			return vfs.FileStat{}, err
		}
		return vfs.FileStat{
			Name:       info.Name(),
			Size:       info.Size(),
			IsDevice:   false,
			DevicePath: "/dev/fs",
		}, nil
	}
	// Command mode: return basic info.
	return vfs.FileStat{
		Name:       filepath.Base(f.path),
		Size:       int64(len(f.response)),
		IsDevice:   false,
		DevicePath: "/dev/fs",
	}, nil
}

// resolvePath resolves a VFS subpath to a host filesystem path using workDir.
func resolvePath(subpath, workDir string) string {
	trimmed := strings.TrimPrefix(subpath, "/")
	if workDir != "" {
		if trimmed == "" {
			return workDir // subpath "/" → workDir root
		}
		if !filepath.IsAbs(trimmed) {
			return filepath.Join(workDir, trimmed)
		}
	}
	return subpath
}

// FileFactory returns a VFSFileFactory that opens host filesystem files.
func FileFactory() vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		device := "/dev/fs" + subpath

		if subpath == "" {
			return nil, types.NewDriverError("Open", "/dev/fs", fmt.Errorf("missing file path: use /dev/fs/<path> (e.g. /dev/fs/src/main.go), not /dev/fs alone"), types.ErrNotFound)
		}

		resolved := resolvePath(subpath, workDir)

		// Command mode: O_WRONLY or O_RDWR — defer actual I/O to Write.
		if flags != vfs.O_RDONLY {
			return &HostFSFile{
				path:    resolved,
				workDir: workDir,
				mode:    flags,
			}, nil
		}

		// Read mode: open the file immediately.
		f, err := os.Open(resolved)
		if err != nil {
			return nil, mapOSError("Open", device, err)
		}

		// Reject directories — only regular files allowed in read mode.
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, mapOSError("Open", device, err)
		}
		if info.IsDir() {
			f.Close()
			return nil, types.NewDriverError("Open", device, fmt.Errorf("is a directory"), types.ErrPermission)
		}

		return &HostFSFile{
			file:    f,
			path:    resolved,
			workDir: workDir,
			mode:    vfs.O_RDONLY,
		}, nil
	}
}

// mapOSError converts an os-level error to a *types.DriverError.
func mapOSError(op, device string, err error) *types.DriverError {
	switch {
	case os.IsNotExist(err):
		return types.NewDriverError(op, device, err, types.ErrNotFound)
	case os.IsPermission(err):
		return types.NewDriverError(op, device, err, types.ErrPermission)
	default:
		return types.NewDriverError(op, device, err, types.ErrDriver)
	}
}
