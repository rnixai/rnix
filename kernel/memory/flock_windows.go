//go:build windows

package memory

// Windows: no-op; intra-process safety is provided by the provider's sync.Mutex.
func flockExclusive(_ uintptr) error { return nil }
func flockUnlock(_ uintptr) error    { return nil }
