package intentdriver

import (
	"context"
	"strings"
	"testing"

	"github.com/rnixai/rnix/intent"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// failingSpawner makes every spawned intent node exit non-zero, so the
// reconciler marks the tree IntentFailed after retries are exhausted.
type failingSpawner struct{ pidAlloc uint64 }

func (s *failingSpawner) SpawnIntent(_ context.Context, _ *intent.IntentNode) (types.PID, error) {
	s.pidAlloc++
	return types.PID(s.pidAlloc), nil
}

func (s *failingSpawner) Wait(_ types.PID) (intent.ExitStatus, error) {
	return intent.ExitStatus{Code: 1, Reason: "child failed (e.g. 401)"}, nil
}

func (s *failingSpawner) Kill(_ types.PID) error { return nil }

// TestIntentFile_AutoStart_FailedTree_ReturnsError verifies F2 (rnix-eval
// upstream finding, P0): when auto_start runs an intent tree whose sub-tasks
// fail, the decompose Write returns an error rather than a successful tree.
// reconciler.Execute returns nil even on IntentFailed, so without this check the
// failed tree surfaced as a successful tool result and the orchestrator reported
// "fake success" with exit 0. The error trips the caller's HasToolError so
// `rnix apply` exits non-zero.
func TestIntentFile_AutoStart_FailedTree_ReturnsError(t *testing.T) {
	nodesJSON := `[{"id":"a","intent":"do task a","depends_on":[]}]`
	mgr := intent.NewManager(
		intent.NewDecomposer(&mockDecomposeCaller{response: nodesJSON}),
		&failingSpawner{},
		intent.DefaultReconcilerConfig(),
	)
	driver := NewDriver(mgr)

	factory := FileFactory(driver)
	file, err := factory("/decompose", vfs.O_RDWR, "")
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	werr := file.Write(context.Background(), []byte(`{"intent":"do a job","auto_start":true}`))
	if werr == nil {
		t.Fatal("expected auto_start to return an error when the intent tree failed, got nil (fake success)")
	}
	if !strings.Contains(werr.Error(), "intent execution failed") {
		t.Errorf("error = %q, want substring 'intent execution failed'", werr.Error())
	}
}

// TestIntentFile_AutoStart_SuccessTree_NoError guards against over-triggering:
// when all sub-tasks succeed, auto_start returns the tree with no error.
func TestIntentFile_AutoStart_SuccessTree_NoError(t *testing.T) {
	nodesJSON := `[{"id":"a","intent":"do task a","depends_on":[]}]`
	mgr := intent.NewManager(
		intent.NewDecomposer(&mockDecomposeCaller{response: nodesJSON}),
		&mockSpawner{}, // Wait returns Code 0
		intent.DefaultReconcilerConfig(),
	)
	driver := NewDriver(mgr)

	factory := FileFactory(driver)
	file, err := factory("/decompose", vfs.O_RDWR, "")
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if werr := file.Write(context.Background(), []byte(`{"intent":"do a job","auto_start":true}`)); werr != nil {
		t.Fatalf("expected success for all-passing tree, got error: %v", werr)
	}
}
