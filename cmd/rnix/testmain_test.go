package main

import (
	"fmt"
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
//
// Story 44.2 review P5: if MkdirTemp fails we MUST exit non-zero rather than
// fall through to m.Run() — otherwise the whole suite silently runs against
// whatever live daemon happens to be on the dev box, which is exactly the
// failure mode this TestMain exists to prevent.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "rnix-test-nosock-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testmain: MkdirTemp failed; aborting to avoid running against a live daemon:", err)
		os.Exit(1)
	}
	os.Setenv("XDG_RUNTIME_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
