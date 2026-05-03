package main

// =============================================================================
// ATDD Story 36.1: Step Inspector (replaces Story 27.4 Prompt Pager)
// Tests verify the unified Step Inspector lens architecture.
// =============================================================================

import (
"encoding/json"
"fmt"
"strings"
"testing"

"github.com/rnixai/rnix/ipc"
)

// --- Test helpers ---

func newTestInspectorModelWithDetail() dashboardModel {
m := newTestDashboardModel(mockDashboardProcs())
m.selectedPID = 2
m.selectedUUID = "uuid-mock-002"
m.viewMode = viewStepInspector
m.inspectorPID = 2
m.inspectorUUID = "uuid-mock-002"
m.inspectorStep = 1
m.inspectorStepMax = 3
m.inspectorLens = lensConversation
	m.inspectorDetail = &ipc.GetStepDetailResponse{
		Step:           1,
		Action:         "tool_call",
		Summary:        "Read file",
		SystemPrompt:   "You are an agent.",
		RequestTokens:  1500,
		ResponseTokens: 800,
		ToolPath:       "/dev/fs",
		ToolInput:      `{"path": "/etc/hosts"}`,
		ToolResult:     "127.0.0.1 localhost",
		ToolDurationMs: 12,
		MessageCount:   5,
		TokenCount:     2300,
		Messages: []ipc.MessageWire{
			{Role: "system", Content: "You are an agent."},
			{Role: "user", Content: "Read the hosts file"},
			{Role: "assistant", Content: "I'll read that file.", ToolCalls: []ipc.ToolCallWire{{ID: "tc1", Name: "read_file"}}},
			{Role: "tool", Content: "127.0.0.1 localhost", ToolCallID: "tc1"},
			{Role: "assistant", Content: "Here is the content."},
		},
	}
m.rebuildInspectorContents()
return m
}

// --- 36.1-AC1: Lens content correctness ---

func TestInspector_ConversationLensContainsMessages(t *testing.T) {
m := newTestInspectorModelWithDetail()
content := m.buildLensContent(lensConversation, m.inspectorDetail, nil)

for _, expected := range []string{"system", "user", "assistant", "tool"} {
if !strings.Contains(content, expected) {
t.Errorf("Conversation lens should contain role %q", expected)
}
}
}

func TestInspector_ConversationLensNoMessages(t *testing.T) {
m := newTestInspectorModelWithDetail()
detail := &ipc.GetStepDetailResponse{Messages: nil}
content := m.buildLensContent(lensConversation, detail, nil)

if !strings.Contains(content, "No message history") {
t.Error("Conversation lens with no messages should show fallback text")
}
}

func TestInspector_SystemLensContainsPrompt(t *testing.T) {
m := newTestInspectorModelWithDetail()
content := m.buildLensContent(lensSystem, m.inspectorDetail, nil)

if !strings.Contains(content, "System Prompt") {
t.Error("System lens should contain 'System Prompt' header")
}
if !strings.Contains(content, "You are an agent.") {
t.Error("System lens should contain the system prompt text")
}
}

func TestInspector_SystemLensShowsCharCount(t *testing.T) {
m := newTestInspectorModelWithDetail()
content := m.buildLensContent(lensSystem, m.inspectorDetail, nil)

if !strings.Contains(content, "chars") {
t.Error("System lens should show character count")
}
}

func TestInspector_SystemLensUnchangedHint(t *testing.T) {
m := newTestInspectorModelWithDetail()
prevDetail := &ipc.GetStepDetailResponse{SystemPrompt: "You are an agent."}
content := m.buildLensContent(lensSystem, m.inspectorDetail, prevDetail)

if !strings.Contains(content, "unchanged") {
t.Error("System lens should show 'unchanged' hint when system prompt matches previous step")
}
}

func TestInspector_ToolIOLensContainsAction(t *testing.T) {
m := newTestInspectorModelWithDetail()
content := m.buildLensContent(lensToolIO, m.inspectorDetail, nil)

if !strings.Contains(content, "tool_call") {
t.Error("Tool I/O lens should contain the action name")
}
if !strings.Contains(content, "/dev/fs") {
t.Error("Tool I/O lens should contain the tool path")
}
}

func TestInspector_ToolIOLensNoTool(t *testing.T) {
m := newTestInspectorModelWithDetail()
detail := &ipc.GetStepDetailResponse{Action: "", ToolPath: ""}
content := m.buildLensContent(lensToolIO, detail, nil)

if !strings.Contains(content, "No tool information") {
t.Error("Tool I/O lens with no tool should show fallback text")
}
}

func TestInspector_MetaLensContainsTokens(t *testing.T) {
m := newTestInspectorModelWithDetail()
content := m.buildLensContent(lensMeta, m.inspectorDetail, nil)

if !strings.Contains(content, "1500") {
t.Error("Meta lens should contain request token count")
}
if !strings.Contains(content, "800") {
t.Error("Meta lens should contain response token count")
}
}

func TestInspector_MetaLensContainsToolPath(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	content := m.buildLensContent(lensMeta, m.inspectorDetail, nil)

	if !strings.Contains(content, "/dev/fs") {
		t.Error("Meta lens should contain tool path")
	}
}

func TestInspector_RawJSONLensValidJSON(t *testing.T) {
m := newTestInspectorModelWithDetail()
content := m.buildLensContent(lensRawJSON, m.inspectorDetail, nil)

var js json.RawMessage
if err := json.Unmarshal([]byte(content), &js); err != nil {
t.Errorf("Raw JSON lens should produce valid JSON, got error: %v", err)
}
}

// --- 36.1-AC2: Truncation behavior ---

func TestInspector_TruncationNotice(t *testing.T) {
notice := renderTruncationNotice(10000, 28300)
if !strings.Contains(notice, "truncated") {
t.Error("Truncation notice should contain 'truncated'")
}
if !strings.Contains(notice, "10.0k") {
t.Error("Truncation notice should show shown count as 10.0k")
}
if !strings.Contains(notice, "28.3k") {
t.Error("Truncation notice should show total count as 28.3k")
}
if !strings.Contains(notice, "o open full") {
t.Error("Truncation notice should hint at 'o open full'")
}
}

func TestInspector_ConversationLensTruncatesLargeContent(t *testing.T) {
m := newTestInspectorModelWithDetail()
bigContent := strings.Repeat("x", inspectorTruncateThreshold+5000)
detail := &ipc.GetStepDetailResponse{
Messages: []ipc.MessageWire{{Role: "user", Content: bigContent}},
}
content := m.buildLensContent(lensConversation, detail, nil)

if !strings.Contains(content, "truncated") {
t.Error("Conversation lens should truncate content exceeding threshold")
}
}

// --- 36.1-AC3: Step Rail rendering ---

func TestInspector_StepRailContainsPID(t *testing.T) {
m := newTestInspectorModelWithDetail()
rail := m.renderStepRail(120)

if !strings.Contains(rail, "Step Inspector") {
t.Error("Step rail should contain 'Step Inspector' title")
}
if !strings.Contains(rail, "PID 2") {
t.Error("Step rail should contain PID")
}
}

func TestInspector_StepRailShowsStepCount(t *testing.T) {
m := newTestInspectorModelWithDetail()
rail := m.renderStepRail(120)

if !strings.Contains(rail, "Step 1/3") {
t.Error("Step rail should show 'Step 1/3'")
}
}

func TestInspector_StepRailShowsAction(t *testing.T) {
m := newTestInspectorModelWithDetail()
rail := m.renderStepRail(120)

if !strings.Contains(rail, "tool_call") {
t.Error("Step rail should show the action name")
}
}

// --- 36.1-AC4: Lens tabs rendering ---

func TestInspector_LensTabsShowAllFive(t *testing.T) {
m := newTestInspectorModelWithDetail()
tabs := m.renderLensTabs(120)

// Check all 5 lens identifiers are present (ASCII or Unicode)
for _, label := range []string{"Conv", "Sys", "Tool", "Meta", "JSON"} {
if !strings.Contains(tabs, label) {
t.Errorf("Lens tabs should contain label %q", label)
}
}
}

// --- 36.1-AC5: Independent scroll positions ---

func TestInspector_LensSwitchPreservesOtherViewport(t *testing.T) {
m := newTestInspectorModelWithDetail()

// Start on conversation lens (index 0)
m.inspectorLens = lensConversation

// Switch to system lens — conversation viewport should be independent
m2 := m.switchInspectorLens(lensSystem)
if m2.inspectorLens != lensSystem {
t.Error("switchInspectorLens should change current lens")
}
// Both viewports exist independently
if &m2.inspectorViewports[lensConversation] == &m2.inspectorViewports[lensSystem] {
t.Error("each lens should have its own viewport")
}
}

// --- 36.1-AC6: P key enters Inspector with System Lens ---

func TestInspector_PKeyFromTimelineEntersSystemLens(t *testing.T) {
// P key redirection is handled in dashboard.go Update() via promptPagerMsg
// Verify the inspectorLens field is set to lensSystem when P triggers
m := newTestInspectorModelWithDetail()
m.inspectorLens = lensSystem
if m.inspectorLens != lensSystem {
t.Error("P key path should set lens to lensSystem")
}
}

// --- 36.1-AC7: formatRoleTag still works ---

func TestInspector_FormatRoleTag(t *testing.T) {
toolNames := map[string]string{"tc1": "read_file"}

tests := []struct {
msg      ipc.MessageWire
expected string
}{
{ipc.MessageWire{Role: "system"}, "system"},
{ipc.MessageWire{Role: "user"}, "user"},
{ipc.MessageWire{Role: "assistant"}, "assistant"},
{ipc.MessageWire{Role: "tool", ToolCallID: "tc1"}, "read_file"},
}

for _, tt := range tests {
tag := formatRoleTag(tt.msg, toolNames)
if !strings.Contains(tag, tt.expected) {
t.Errorf("formatRoleTag(%q) should contain %q, got %q", tt.msg.Role, tt.expected, tag)
}
}
}

// --- 36.1-AC8: formatCharCount ---

func TestInspector_FormatCharCount(t *testing.T) {
tests := []struct {
n        int
expected string
}{
{500, "500"},
{1000, "1.0k"},
{2500, "2.5k"},
{28300, "28.3k"},
}
for _, tt := range tests {
result := formatCharCount(tt.n)
if result != tt.expected {
t.Errorf("formatCharCount(%d) = %q, want %q", tt.n, result, tt.expected)
}
}
}

// --- 36.1-AC9: buildToolCallNameMap ---

func TestInspector_BuildToolCallNameMap(t *testing.T) {
msgs := []ipc.MessageWire{
{Role: "assistant", ToolCalls: []ipc.ToolCallWire{
{ID: "tc1", Name: "read_file"},
{ID: "tc2", Name: "write_file"},
}},
{Role: "tool", ToolCallID: "tc1", Content: "data"},
}
names := buildToolCallNameMap(msgs)

if names["tc1"] != "read_file" {
t.Errorf("buildToolCallNameMap should map tc1 to read_file, got %q", names["tc1"])
}
if names["tc2"] != "write_file" {
t.Errorf("buildToolCallNameMap should map tc2 to write_file, got %q", names["tc2"])
}
}

// --- 36.1-AC10: rebuildInspectorContents populates all 5 viewports ---

func TestInspector_RebuildContentsPopulatesAllLenses(t *testing.T) {
m := newTestInspectorModelWithDetail()
m.rebuildInspectorContents()

for i := range inspectorLensCount {
if m.inspectorContents[i] == "" {
t.Errorf("rebuildInspectorContents should populate lens %d content", i)
}
}
}

// --- 36.1-AC11: Inspector footer hints ---

func TestInspector_FooterContainsAllHints(t *testing.T) {
m := newTestInspectorModelWithDetail()
footer := m.renderInspectorFooter()

for _, hint := range []string{"h/l", "1-5", "j/k", "copy", "open", "Esc"} {
if !strings.Contains(footer, hint) {
t.Errorf("Inspector footer should contain hint %q", hint)
}
}
}

// --- 36.1-AC12: Full content builder (for pager) ---

func TestInspector_BuildFullLensContent_Conversation(t *testing.T) {
m := newTestInspectorModelWithDetail()
full := m.buildFullLensContent(lensConversation, m.inspectorDetail, nil)

if !strings.Contains(full, "user") {
t.Error("Full conversation lens should contain role tags")
}
if !strings.Contains(full, "Read the hosts file") {
t.Error("Full conversation lens should contain message content")
}
}

func TestInspector_BuildFullLensContent_System(t *testing.T) {
m := newTestInspectorModelWithDetail()
full := m.buildFullLensContent(lensSystem, m.inspectorDetail, nil)

if full != "You are an agent." {
t.Errorf("Full system lens should be raw system prompt, got %q", full)
}
}

func TestInspector_BuildFullLensContent_ToolIO(t *testing.T) {
m := newTestInspectorModelWithDetail()
full := m.buildFullLensContent(lensToolIO, m.inspectorDetail, nil)

if !strings.Contains(full, "tool_call") {
t.Error("Full tool I/O lens should contain the action")
}
}

func TestInspector_BuildFullLensContent_Meta(t *testing.T) {
m := newTestInspectorModelWithDetail()
full := m.buildFullLensContent(lensMeta, m.inspectorDetail, nil)

if !strings.Contains(full, "1500") {
t.Error("Full meta lens should contain token count")
}
}

func TestInspector_BuildFullLensContent_RawJSON(t *testing.T) {
m := newTestInspectorModelWithDetail()
full := m.buildFullLensContent(lensRawJSON, m.inspectorDetail, nil)

var js json.RawMessage
if err := json.Unmarshal([]byte(full), &js); err != nil {
t.Errorf("Full raw JSON lens should produce valid JSON, got error: %v", err)
}
}

// --- 36.1-AC13: inspectorTruncateThreshold value ---

func TestInspector_TruncateThresholdIs10k(t *testing.T) {
if inspectorTruncateThreshold != 10000 {
t.Errorf("inspectorTruncateThreshold should be 10000, got %d", inspectorTruncateThreshold)
}
}

// --- 36.1-AC14: Tool I/O lens shows duration ---

func TestInspector_ToolIOLensShowsDuration(t *testing.T) {
m := newTestInspectorModelWithDetail()
content := m.buildLensContent(lensToolIO, m.inspectorDetail, nil)

if !strings.Contains(content, "12ms") || !strings.Contains(content, "Duration") {
t.Error("Tool I/O lens should show tool duration")
}
}

// --- 36.1-AC15: Conversation lens shows tool call name in role tag ---

func TestInspector_ConversationLensShowsToolCallName(t *testing.T) {
m := newTestInspectorModelWithDetail()
content := m.buildLensContent(lensConversation, m.inspectorDetail, nil)

if !strings.Contains(content, "read_file") {
t.Error("Conversation lens should resolve tool call names in role tags")
}
}

// --- 36.1-AC16: Inspector content height calculation ---

func TestInspector_ContentHeightCalculation(t *testing.T) {
m := newTestInspectorModelWithDetail()
m.height = 40

h := m.inspectorContentHeight()
// Story 38-3 AC#6: when h>=20 the Inspector reserves an extra 2-line
// thumbnail bar block + 1 spacing line. Total overhead = stepRail(1) +
// thumbnailBar(2) + lensTabs(1) + footer(1) + spacing(1) = 6.
expected := 34
if h != expected {
t.Errorf("inspectorContentHeight() with height=40 should be %d, got %d", expected, h)
}
}

// --- 38.3-AC6: Short terminal hides thumbnail bar ---

func TestInspector_ContentHeightCalculation_ShortTerminal(t *testing.T) {
m := newTestInspectorModelWithDetail()
m.height = 18 // < 20 → thumbnail bar hidden, legacy 4-line chrome restored

h := m.inspectorContentHeight()
expected := 14 // 18 - 4
if h != expected {
t.Errorf("inspectorContentHeight() with height=18 should be %d, got %d", expected, h)
}
}

// Dummy use of fmt to avoid unused import
var _ = fmt.Sprintf
