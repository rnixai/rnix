package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================
// ATDD — Story 34-7: Orchestration Relationship Visualization
// Tests for compose/pipeline annotations in tree and detail card.
// ============================================================

// --- AC1: Compose DAG edge rendering in tree ---

func TestOrchestrationAnnotation_ComposeWithDeps(t *testing.T) {
	p := vfs.ProcInfo{
		ComposeNode: "summarizer",
		ComposeDeps: []string{"researcher", "analyst"},
	}
	ann := orchestrationAnnotation(p)
	if ann == "" {
		t.Fatal("expected non-empty annotation for compose node with deps")
	}
	if !strings.Contains(ann, "researcher") || !strings.Contains(ann, "analyst") {
		t.Errorf("annotation %q should contain dep names", ann)
	}
	if !strings.Contains(ann, "╌╌►") {
		t.Errorf("annotation %q should contain Unicode arrow '╌╌►'", ann)
	}
}

func TestOrchestrationAnnotation_ComposeNoDeps(t *testing.T) {
	p := vfs.ProcInfo{
		ComposeNode: "root",
	}
	ann := orchestrationAnnotation(p)
	if ann == "" {
		t.Fatal("expected non-empty annotation for compose node without deps")
	}
	if !strings.Contains(ann, "root") {
		t.Errorf("annotation %q should contain node name", ann)
	}
}

func TestOrchestrationAnnotation_Pipeline(t *testing.T) {
	p := vfs.ProcInfo{
		PipelineIndex: 1,
		PipelineTotal: 3,
	}
	ann := orchestrationAnnotation(p)
	if ann == "" {
		t.Fatal("expected non-empty annotation for pipeline process")
	}
	if !strings.Contains(ann, "2/3") {
		t.Errorf("annotation %q should contain 1-based index '2/3'", ann)
	}
}

func TestOrchestrationAnnotation_NotOrchestrated(t *testing.T) {
	p := vfs.ProcInfo{
		PID:    1,
		Intent: "hello",
	}
	ann := orchestrationAnnotation(p)
	if ann != "" {
		t.Errorf("expected empty annotation for non-orchestrated process, got %q", ann)
	}
}

// --- AC2: ASCII mode support ---

func TestOrchestrationAnnotation_ComposeASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")

	p := vfs.ProcInfo{
		ComposeNode: "summarizer",
		ComposeDeps: []string{"researcher"},
	}
	ann := orchestrationAnnotation(p)
	if strings.ContainsAny(ann, "◄╌│►◆") {
		t.Errorf("ASCII mode annotation %q should not contain Unicode glyphs", ann)
	}
	if !strings.Contains(ann, "-->") {
		t.Errorf("ASCII mode annotation %q should contain '-->'", ann)
	}
}

func TestOrchestrationAnnotation_PipelineASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")

	p := vfs.ProcInfo{
		PipelineIndex: 0,
		PipelineTotal: 2,
	}
	ann := orchestrationAnnotation(p)
	if strings.ContainsAny(ann, "│►") {
		t.Errorf("ASCII mode annotation %q should not contain Unicode glyphs", ann)
	}
	if !strings.Contains(ann, "|>") {
		t.Errorf("ASCII mode annotation %q should contain '|>'", ann)
	}
}

// --- AC3: Tree row includes orchestration suffix ---

func TestTreeRow_ComposeAnnotationInSuffix(t *testing.T) {
	now := time.Now()
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, UUID: "uuid-orch-001", State: types.StateRunning, Intent: "supervisor", CreatedAt: now},
		{PID: 2, PPID: 1, UUID: "uuid-orch-002", State: types.StateRunning, Intent: "research", CreatedAt: now,
			ComposeNode: "researcher", ComposeDeps: []string{}},
		{PID: 3, PPID: 1, UUID: "uuid-orch-003", State: types.StateRunning, Intent: "summarize", CreatedAt: now,
			ComposeNode: "summarizer", ComposeDeps: []string{"researcher"}},
	}

	m := newTestDashboardModel(procs)
	roots := buildProcessTree(procs, 0, false)
	m.tree.Rows = flattenTreeWithCollapse(roots, nil)
	output := m.renderDashboardTreePane(100, 20)
	if !strings.Contains(output, "researcher") {
		t.Error("tree output should contain compose dep 'researcher'")
	}
}

func TestTreeRow_PipelineAnnotationInSuffix(t *testing.T) {
	now := time.Now()
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, UUID: "uuid-pipe-001", State: types.StateRunning, Intent: "stage1", CreatedAt: now,
			PipelineIndex: 0, PipelineTotal: 3},
		{PID: 2, PPID: 0, UUID: "uuid-pipe-002", State: types.StateRunning, Intent: "stage2", CreatedAt: now,
			PipelineIndex: 1, PipelineTotal: 3},
	}

	m := newTestDashboardModel(procs)
	roots := buildProcessTree(procs, 0, false)
	m.tree.Rows = flattenTreeWithCollapse(roots, nil)
	output := m.renderDashboardTreePane(100, 20)
	if !strings.Contains(output, "1/3") {
		t.Error("tree output should contain pipeline marker '1/3'")
	}
	if !strings.Contains(output, "2/3") {
		t.Error("tree output should contain pipeline marker '2/3'")
	}
}

// --- AC4: Detail card orchestration info ---

func TestDetailOrchestrationInfo_Compose(t *testing.T) {
	d := &ipc.GetProcDetailResponse{
		PPID:        10,
		ComposeNode: "analyst",
		ComposeDeps: []string{"researcher"},
	}
	procs := []vfs.ProcInfo{
		{PID: 10, PPID: 0, ComposeNode: ""},
		{PID: 20, PPID: 10, ComposeNode: "researcher"},
		{PID: 21, PPID: 10, ComposeNode: "analyst", ComposeDeps: []string{"researcher"}},
	}
	info := detailOrchestrationInfo(d, procs)
	if info == "" {
		t.Fatal("expected non-empty orchestration info for compose node")
	}
	if !strings.Contains(info, "Compose:analyst") {
		t.Errorf("info %q should contain 'Compose:analyst'", info)
	}
	if !strings.Contains(info, "PID 20") {
		t.Errorf("info %q should contain PID-mapped dep 'PID 20'", info)
	}
	if !strings.Contains(info, "depends_on:") {
		t.Errorf("info %q should contain 'depends_on:'", info)
	}
	if !strings.Contains(info, "stage") {
		t.Errorf("info %q should contain DAG stage info", info)
	}
}

func TestDetailOrchestrationInfo_Pipeline(t *testing.T) {
	d := &ipc.GetProcDetailResponse{
		PipelineIndex: 2,
		PipelineTotal: 5,
	}
	info := detailOrchestrationInfo(d, nil)
	if info == "" {
		t.Fatal("expected non-empty orchestration info for pipeline")
	}
	if !strings.Contains(info, "Pipeline[3/5]") {
		t.Errorf("info %q should contain 'Pipeline[3/5]'", info)
	}
}

func TestDetailOrchestrationInfo_None(t *testing.T) {
	d := &ipc.GetProcDetailResponse{
		PID:    1,
		Intent: "hello",
	}
	info := detailOrchestrationInfo(d, nil)
	if info != "" {
		t.Errorf("expected empty orchestration info, got %q", info)
	}
}

// --- AC5: Omitempty backward compat ---

func TestOrchestrationAnnotation_ZeroValues(t *testing.T) {
	p := vfs.ProcInfo{
		PID:           1,
		ComposeNode:   "",
		ComposeDeps:   nil,
		PipelineIndex: 0,
		PipelineTotal: 0,
	}
	ann := orchestrationAnnotation(p)
	if ann != "" {
		t.Errorf("zero-value orchestration fields should produce empty annotation, got %q", ann)
	}
}
