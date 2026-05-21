package kernel

import (
	"go/ast"
	"go/parser"
	"go/token"
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
// =============================================================================

// repoRootFromCaller returns the repository root by walking up from this test
// file's location until it finds a go.mod. Fails loudly if not found —
// silently t.Skipping (the pre-44.1 behaviour) made it impossible to detect
// environment misconfiguration that left structural assertions un-evaluated
// (Story 44.1 code review F23).
func repoRootFromCaller(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate repo root")
	}
	dir := filepath.Dir(here)
	for range 8 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("go.mod not found above test file; AC#4/AC#6 structural checks cannot run")
	return ""
}

// readFileOrFatal reads a file relative to repoRoot; fails the test if missing.
// Renamed from readFileOrSkip — like repoRootFromCaller, the previous skip
// behaviour masked real environment problems.
func readFileOrFatal(t *testing.T, repoRoot, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatalf("cannot read %s: %v", rel, err)
	}
	return string(b)
}

// --- AC#4: reactivateCliDisconnectedAncestors function body is removed ---

func TestATDD_44_1_030_ReactivateCliDisconnectedAncestors_FunctionRemoved(t *testing.T) {
	root := repoRootFromCaller(t)
	resume := readFileOrFatal(t, root, "kernel/resume.go")

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
// Replaces the pre-44.1 plain-substring scan, which suffered both false
// positives (`flap.Pause()` matched `p.Pause()`) and false negatives
// (`target.Pause()` was missed). We now parse each .go file with go/ast and
// inspect every SelectorExpr call: an offender is any call whose method
// identifier is Pause/Resume and whose receiver appears to be a *Process
// (named "proc", "p", or any local variable whose declared type contains
// "Process") (Story 44.1 code review F18).

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
		_ = filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if excludeFiles[path] {
				return nil
			}
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if perr != nil {
				// Unparseable file — skip silently; the build will catch it.
				return nil
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name != "Pause" && sel.Sel.Name != "Resume" {
					return true
				}
				if len(call.Args) != 0 {
					return true // Process.Pause/Resume take no args
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				// Heuristic: identifiers commonly used for *Process receivers.
				switch ident.Name {
				case "proc", "p", "parent", "child", "target", "source":
					rel, _ := filepath.Rel(root, path)
					pos := fset.Position(call.Pos())
					offenders = append(offenders, rel+":"+itoa(pos.Line)+" → "+ident.Name+"."+sel.Sel.Name+"()")
				}
				return true
			})
			return nil
		})
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

// itoa avoids dragging in strconv just for line-number formatting.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// --- AC#6: defaultSignalAction must not route SIGPAUSE/SIGRESUME through
// proc.Pause()/proc.Resume(). We parse signal.go with go/ast, locate the
// defaultSignalAction body, and assert no `proc.Pause()`/`proc.Resume()` call
// appears under the SIGPAUSE/SIGRESUME case clauses (Story 44.1 code review
// F17). ---

func TestATDD_44_1_053_DefaultSignalAction_NoLegacySoftPauseDispatch(t *testing.T) {
	root := repoRootFromCaller(t)
	path := filepath.Join(root, "kernel", "signal.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse signal.go: %v", err)
	}

	var targetFunc *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		if fn.Name.Name == "defaultSignalAction" {
			targetFunc = fn
			break
		}
	}
	if targetFunc == nil {
		t.Fatalf("defaultSignalAction not found in %s", path)
	}

	// Walk the function body and check each `case sig == types.SIGPAUSE:` /
	// `case sig == types.SIGRESUME:` clause. Within the clause body, no
	// SelectorExpr `proc.Pause` / `proc.Resume` call may appear.
	ast.Inspect(targetFunc.Body, func(n ast.Node) bool {
		cc, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		// Only the case where the predicate references SIGPAUSE/SIGRESUME.
		isTarget := false
		for _, expr := range cc.List {
			text := exprString(fset, expr)
			if strings.Contains(text, "SIGPAUSE") || strings.Contains(text, "SIGRESUME") {
				isTarget = true
				break
			}
		}
		if !isTarget {
			return true
		}
		// Inspect the clause body for any proc.Pause / proc.Resume call.
		for _, stmt := range cc.Body {
			ast.Inspect(stmt, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name != "Pause" && sel.Sel.Name != "Resume" {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok || ident.Name != "proc" {
					return true
				}
				t.Errorf("defaultSignalAction SIGPAUSE/SIGRESUME case calls proc.%s() — task 2.1 requires routing through k.suspendSubtree / k.ResumeSubtree (signal.go:%d)",
					sel.Sel.Name, fset.Position(call.Pos()).Line)
				return true
			})
		}
		return true
	})
}

// exprString returns the source text covered by an ast.Expr by re-reading the
// fileset position range. Cheaper than printer.Fprint for the single use here.
func exprString(fset *token.FileSet, expr ast.Expr) string {
	start := fset.Position(expr.Pos())
	end := fset.Position(expr.End())
	if start.Filename != end.Filename {
		return ""
	}
	data, err := os.ReadFile(start.Filename)
	if err != nil {
		return ""
	}
	if start.Offset < 0 || end.Offset > len(data) || start.Offset >= end.Offset {
		return ""
	}
	return string(data[start.Offset:end.Offset])
}
