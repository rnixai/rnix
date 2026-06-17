package config

import (
	"fmt"
	"math"
	"os"
	"time"

	"github.com/goccy/go-yaml"
)

// shellConfigFile is the minimal view of config.yaml needed to resolve the
// /dev/shell command-execution timeout (Story 57.1 AC1). The `shell` section
// is optional; a missing file or section yields the zero value, which the
// caller maps to "use the driver default".
type shellConfigFile struct {
	Shell struct {
		// CommandTimeoutSeconds caps a single shell command's wall-clock
		// execution time. snake_case mirrors the existing yaml convention
		// (gc.interval_seconds, checkpoint.interval_seconds).
		CommandTimeoutSeconds int `yaml:"command_timeout_seconds"`
	} `yaml:"shell"`
}

// ResolveShellTimeout reads shell.command_timeout_seconds from configPath and
// returns it as a Duration. It returns 0 (meaning "unset — let the driver fall
// back to its default") when the file is missing, unparseable, or the value is
// absent / non-positive. Any warning is returned so the caller can surface it;
// the function never fails hard, matching ResolveFeatures' lenient contract.
func ResolveShellTimeout(configPath string) (time.Duration, string) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		// A missing file is the common "no config" case — silent. Any other
		// read error (permission denied, EISDIR, I/O) is a real misconfiguration
		// the operator should see, so surface it as a warning rather than
		// silently falling back to the default.
		if os.IsNotExist(err) {
			return 0, ""
		}
		return 0, fmt.Sprintf("shell: failed to read %s: %v", configPath, err)
	}

	var cfg shellConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return 0, fmt.Sprintf("shell: failed to parse %s: %v", configPath, err)
	}

	secs := cfg.Shell.CommandTimeoutSeconds
	if secs <= 0 {
		return 0, ""
	}
	// The config layer trusts the operator and does NOT clamp to the per-call
	// ceiling, but an implausibly large value would overflow int64 nanoseconds
	// in `time.Duration(secs) * time.Second` and wrap to a tiny/negative
	// duration. Refuse such values (use the driver default) with a warning
	// rather than silently producing a sub-second timeout.
	const maxSafeSeconds = math.MaxInt64 / int64(time.Second)
	if int64(secs) > maxSafeSeconds {
		return 0, fmt.Sprintf("shell: command_timeout_seconds=%d is implausibly large (max %d); using the driver default", secs, maxSafeSeconds)
	}
	return time.Duration(secs) * time.Second, ""
}
