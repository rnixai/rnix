package kernel

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// =============================================================================
// ATDD 44.1 — Static / structural verification tests for the unified-resume
// statemachine. Covers acceptance criteria that hinge on *what is no longer in
// the codebase* rather than on runtime behaviour.
//
// Covers:
//   - AC#4: `reactivateCliDisconnectedAncestors` and its caller in
//           `restoreParentLinkage` are removed. `SuspendReasonCLIDisconnected`
//           constant is preserved (44.2 still consumes the string value).
//   - AC#6: No business code (anything in kernel/ ipc/ cmd/ excluding the
//           method definition, the coroutine internal user, and _test.go
//           files) calls `proc.Pause()` or `proc.Resume()`.
//
// These tests intentionally inspect source files on disk. They are stable
// because the kernel package always builds from a working tree where these
// paths exist. If the repository is shipped as a tar without sources the
// `grep`-style assertions skip themselves with t.Skip rather than fail.
// =============================================================================

// repoRootFromCaller returns the repository root by walking up from this test
// file's location until it finds a go.mod. Falls back to "" if not found.
func repoRootFromCaller(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller failed; cannot locate repo root")
	}
	dir := filepath.Dir(here)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Skip("go.mod not found above test file; running from an unusual layout")
	return ""
}

// readFileOrSkip reads a file relative to repoRoot; skips the test if missing.
func readFileOrSkip(t *testing.T, repoRoot, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Skipf("cannot read %s: %v", rel, err)
	}
	return string(b)
}

// --- AC#4: reactivateCliDisconnectedAncestors function body is removed ---

func TestATDD_44_1_030_ReactivateCliDisconnectedAncestors_FunctionRemoved(t *testing.T) {
	root := repoRootFromCaller(t)
	resume := readFileOrSkip(t, root, "kernel/resume.go")

	// The function definition must be gone from kernel/resume.go.
	needle := "func (k *KernelImpl) reactivateCliDisconnectedAncestors"
	if strings.Contains(resume, needle) {
		t.Errorf(
			"kernel/resume.go still defines %q; story 44.1 task 3 requires the function body to be deleted",
			needle,
		)
	}

	// The call site in restoreParentLinkage must also be gone.
	if strings.Contains(resume, "reactivateCliDisconnectedAncestors(") {
		t.Errorf("kernel/resume.go still references reactivateCliDisconnectedAncestors; the caller in restoreParentLinkage must be removed")
	}
}

// --- AC#4: SuspendReasonCLIDisconnected constant is preserved (44.2 needs it) ---

func TestATDD_44_1_031_SuspendReasonCLIDisconnected_ConstantPreserved(t *testing.T) {
	// Compile-time reference: if the constant is renamed or removed, this test
	// fails to build.
	got := SuspendReasonCLIDisconnected
	const want = "cli_disconnected"
	if got != want {
		t.Errorf("SuspendReasonCLIDisconnected = %q, want %q (44.2 still relies on this value)", got, want)
	}
}

// --- AC#6: No business call sites of proc.Pause() / proc.Resume() ---
//
// "Business" = anything under kernel/, ipc/, cmd/ except:
//   - kernel/process.go (the method definitions themselves)
//   - kernel/coroutine.go (resumeCh internal sync primitive, different type)
//   - _test.go files (tests may exercise the methods directly)
//
// The check walks the tree and asserts no matching line exists.

func TestATDD_44_1_052_NoBusinessCallSiteFor_ProcPause_ProcResume(t *testing.T) {
	root := repoRootFromCaller(t)

	dirs := []string{"kernel", "ipc", "cmd"}
	excludeFiles := map[string]bool{
		filepath.Join(root, "kernel", "process.go"):   true,
		filepath.Join(root, "kernel", "coroutine.go"): true,
	}

	var offenders []string
	for _, dir := range dirs {
		absDir := filepath.Join(root, dir)
		err := filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if excludeFiles[path] {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil // unreadable file — skip silently
			}
			body := string(data)
			for _, lit := range []string{".Pause()", ".Resume()"} {
				if strings.Contains(body, "proc"+lit) || strings.Contains(body, "p"+lit) {
					rel, _ := filepath.Rel(root, path)
					offenders = append(offenders, rel+" → proc"+lit)
				}
			}
			return nil
		})
		if err != nil {
			t.Logf("walk %s: %v", absDir, err)
		}
	}

	if len(offenders) > 0 {
		t.Errorf(
			"AC#6 violation — business code must not call proc.Pause()/proc.Resume() directly.\n"+
				"Use k.Suspend(pid) / k.ResumeSubtree(pid) or SIGPAUSE/SIGRESUME instead.\n"+
				"Offending call sites:\n  %s",
			strings.Join(offenders, "\n  "),
		)
	}
}

// --- AC#6: defaultSignalAction must not route SIGPAUSE/SIGRESUME through
// proc.Pause()/proc.Resume(). Source-level check complements the behavioural
// tests in atdd_44_1_signal_test.go. ---

func TestATDD_44_1_053_DefaultSignalAction_NoLegacySoftPauseDispatch(t *testing.T) {
	root := repoRootFromCaller(t)
	sig := readFileOrSkip(t, root, "kernel/signal.go")

	// Locate the SIGPAUSE / SIGRESUME cases inside defaultSignalAction and
	// verify they do NOT contain the legacy SoftPause method calls. We use a
	// crude string check rather than a parser — false positives are tolerable
	// because the test will be re-run after dev-story fixes.
	for _, sig := range []string{"types.SIGPAUSE", "types.SIGRESUME"} {
		_ = sig
	}
	for _, legacy := range []string{"proc.Pause()", "proc.Resume()"} {
		if strings.Contains(sig, "case sig == types.SIGPAUSE:") &&
			strings.Contains(sig, legacy) {
			// Both the case label and the legacy call exist; the legacy call
			// might be unrelated, but the AC requires dev-story to rewrite
			// these cases. Flag it for human review.
			t.Errorf(
				"kernel/signal.go still contains %q while defining the SIGPAUSE/SIGRESUME cases — task 2.1 requires routing through k.suspendSubtree / k.ResumeSubtree",
				legacy,
			)
		}
	}
}
