package intentdriver

import (
	"context"
	"fmt"
	"testing"

	"github.com/rnixai/rnix/intent"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// newGuardTestDriver builds a driver backed by a real *intent.Manager (manager
// is a concrete type and cannot be mocked) and returns the retained mockSpawner
// so tests can assert SpawnIntent was NOT called when the guard rejects —
// Execute is the only path that spawns, so pidAlloc==0 proves the guard fired
// before Confirm/Execute.
func newGuardTestDriver(nodesJSON string) (*IntentDriver, *mockSpawner) {
	spawner := &mockSpawner{}
	mgr := intent.NewManager(
		intent.NewDecomposer(&mockDecomposeCaller{response: nodesJSON}),
		spawner,
		intent.DefaultReconcilerConfig(),
	)
	return NewDriver(mgr), spawner
}

// Scenario 拒绝-stop: auto_start with a node whose intent contains
// `rnix daemon stop` must be rejected with ErrInvalid before any spawn.
func TestIntentFile_AutoStart_Guard_RejectsDaemonStop(t *testing.T) {
	nodesJSON := `[{"id":"restart","intent":"运行 rnix daemon stop && rnix daemon start 重启 daemon","depends_on":[]}]`
	driver, spawner := newGuardTestDriver(nodesJSON)
	file, _ := FileFactory(driver)("/decompose", vfs.O_RDWR, "")

	err := file.Write(context.Background(), []byte(`{"intent":"restart the daemon","auto_start":true}`))
	if err == nil {
		t.Fatal("expected guard to reject a daemon-stop node under auto_start")
	}
	var de *types.DriverError
	if !isDriverError(err, &de) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if de.Code != types.ErrInvalid {
		t.Errorf("expected Code=%q, got %q", types.ErrInvalid, de.Code)
	}
	if spawner.pidAlloc != 0 {
		t.Errorf("guard must reject before Execute; SpawnIntent called %d time(s)", spawner.pidAlloc)
	}
}

// Scenario 拒绝-restart: `rnix daemon restart` is equally rejected.
func TestIntentFile_AutoStart_Guard_RejectsDaemonRestart(t *testing.T) {
	nodesJSON := `[{"id":"r","intent":"rnix daemon restart","depends_on":[]}]`
	driver, spawner := newGuardTestDriver(nodesJSON)
	file, _ := FileFactory(driver)("/decompose", vfs.O_RDWR, "")

	err := file.Write(context.Background(), []byte(`{"intent":"x","auto_start":true}`))
	var de *types.DriverError
	if !isDriverError(err, &de) || de.Code != types.ErrInvalid {
		t.Fatalf("expected ErrInvalid DriverError, got %v", err)
	}
	if spawner.pidAlloc != 0 {
		t.Errorf("SpawnIntent should not be called, got %d", spawner.pidAlloc)
	}
}

// Scenario 放行-start: a bare `rnix daemon start` does not kill the running
// daemon, so it must NOT be blocked — execution proceeds normally.
func TestIntentFile_AutoStart_Guard_AllowsDaemonStart(t *testing.T) {
	nodesJSON := `[{"id":"s","intent":"rnix daemon start","depends_on":[]}]`
	driver, spawner := newGuardTestDriver(nodesJSON)
	file, _ := FileFactory(driver)("/decompose", vfs.O_RDWR, "")

	err := file.Write(context.Background(), []byte(`{"intent":"start daemon","auto_start":true}`))
	if err != nil {
		t.Fatalf("bare 'rnix daemon start' must be allowed, got: %v", err)
	}
	if spawner.pidAlloc == 0 {
		t.Error("expected Execute to run (SpawnIntent called) for an allowed auto_start node")
	}
}

// Scenario 放行-正常: ordinary nodes pass the guard and execute to terminal.
func TestIntentFile_AutoStart_Guard_AllowsNormalNodes(t *testing.T) {
	nodesJSON := `[{"id":"a","intent":"write a file to disk","depends_on":[]}]`
	driver, spawner := newGuardTestDriver(nodesJSON)
	file, _ := FileFactory(driver)("/decompose", vfs.O_RDWR, "")

	err := file.Write(context.Background(), []byte(`{"intent":"do work","auto_start":true}`))
	if err != nil {
		t.Fatalf("normal node should pass the guard, got: %v", err)
	}
	if spawner.pidAlloc == 0 {
		t.Error("expected Execute to run for a normal auto_start request")
	}
}

// Scenario 不检查-手动: without auto_start the guard must not fire, even when a
// node would restart the daemon — manual confirm/execute is out of scope.
func TestIntentFile_ManualPath_Guard_NotTriggered(t *testing.T) {
	nodesJSON := `[{"id":"r","intent":"rnix daemon stop","depends_on":[]}]`
	driver, spawner := newGuardTestDriver(nodesJSON)
	file, _ := FileFactory(driver)("/decompose", vfs.O_RDWR, "")

	err := file.Write(context.Background(), []byte(`{"intent":"x"}`))
	if err != nil {
		t.Fatalf("manual path (no auto_start) must not trigger the guard, got: %v", err)
	}
	if spawner.pidAlloc != 0 {
		t.Error("manual decompose must not Execute (no spawn)")
	}
}

// TestIntentFile_AutoStart_Guard_KnownBypasses documents the CURRENT, intentionally
// scoped limits of the literal-regex guard: semantically-equivalent daemon-termination
// commands that are NOT matched today and therefore pass through to Execute. Each case
// asserts present behavior (allowed) to lock in the known gap as a red-line baseline.
// When detection is later strengthened (decision 2026-06-05「增强检测机制」— needs spec
// re-negotiation; see deferred-work.md「37-3 code-review defer」复审升级块), move the
// relevant cases to a rejected set expecting ErrInvalid.
func TestIntentFile_AutoStart_Guard_KnownBypasses(t *testing.T) {
	cases := []struct {
		name       string
		nodeIntent string // would terminate the daemon but evades `rnix daemon (stop|restart)`
	}{
		{"kill_by_pgrep", "kill -9 $(pgrep -f rnix)"},
		{"pkill_rnix", "pkill -f rnix"},
		{"socat_shutdown", "echo '{\"method\":\"shutdown\"}' | socat - UNIX-CONNECT:/tmp/rnix.sock"},
		{"flag_form", "rnix daemon --stop"},
		{"word_order_inverted", "stop the rnix daemon now"},
		{"chinese_paraphrase", "停止 rnix 守护进程"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodesJSON := fmt.Sprintf(`[{"id":"n","intent":%q,"depends_on":[]}]`, tc.nodeIntent)
			driver, spawner := newGuardTestDriver(nodesJSON)
			file, _ := FileFactory(driver)("/decompose", vfs.O_RDWR, "")

			err := file.Write(context.Background(), []byte(`{"intent":"x","auto_start":true}`))
			if err != nil {
				t.Fatalf("KNOWN LIMITATION regressed: %q should currently pass the literal-regex guard, got %v — if detection was strengthened, move this case to the rejected set", tc.nodeIntent, err)
			}
			if spawner.pidAlloc == 0 {
				t.Errorf("expected Execute to run (guard passed) for known-bypass intent %q", tc.nodeIntent)
			}
		})
	}
}
