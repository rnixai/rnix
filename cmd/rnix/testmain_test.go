package main

import (
	"os"
	"testing"
)

// TestMain isolates the daemon socket for the entire cmd/rnix test binary.
//
// Pointing XDG_RUNTIME_DIR at an empty temp directory makes ipc.SocketPath()
// resolve to a non-existent socket, so any test that dials the daemon without
// standing up its own mock server fails fast ("daemon not available") instead
// of connecting to whatever real daemon happens to be running on the dev
// machine. This keeps the suite deterministic regardless of host state:
//   - an old daemon that doesn't recognize a new IPC method would otherwise
//     accept the connection but never reply, hanging the test until timeout;
//   - a daemon that does reply returns live process data that pollutes shared
//     state and fails unrelated tests.
//
// Mock-server tests that set ipc.SocketPathOverride to their own socket are
// unaffected; when they reset the override to "", SocketPath() falls back to
// the empty XDG_RUNTIME_DIR set here, so it still resolves to no daemon.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "rnix-test-nosock-")
	if err != nil {
		os.Exit(m.Run())
	}
	os.Setenv("XDG_RUNTIME_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
