package tty

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// --- ToolDef metadata tests ---

func TestTtyDriver_ToolDefs_Metadata(t *testing.T) {
	d := NewDriver(nil)
	defs := d.ToolDefs()

	if len(defs) != 1 {
		t.Fatalf("expected 1 ToolDef, got %d", len(defs))
	}

	ask := defs[0]
	if ask.Name != "ask" {
		t.Errorf("expected name 'ask', got %q", ask.Name)
	}
	if !ask.IsReadOnly {
		t.Error("ask should be ReadOnly")
	}
	if ask.IsConcurrencySafe {
		t.Error("ask should NOT be ConcurrencySafe")
	}
	if ask.IsDestructive {
		t.Error("ask should NOT be destructive")
	}
	if !ask.ShouldDefer {
		t.Error("ask should have ShouldDefer=true")
	}
	if ask.SearchHint != "tty ask user question interact" {
		t.Errorf("unexpected SearchHint: %q", ask.SearchHint)
	}
	if ask.Description == "" {
		t.Error("ask Description should not be empty")
	}
	if ask.Parameters == nil {
		t.Error("ask Parameters should not be nil")
	}
}

func TestTtyDriver_ToolDescriptorInterface(t *testing.T) {
	var _ vfs.ToolDescriptor = (*TtyDriver)(nil)
}

// --- Write/Read tests ---

func TestTtyFile_Write_SingleQuestion(t *testing.T) {
	mockAsk := func(ctx context.Context, questions []Question) ([]Answer, error) {
		if len(questions) != 1 {
			t.Fatalf("expected 1 question, got %d", len(questions))
		}
		if questions[0].Question != "What color?" {
			t.Errorf("unexpected question: %q", questions[0].Question)
		}
		return []Answer{{Answer: "blue"}}, nil
	}

	d := NewDriver(mockAsk)
	f := &TtyFile{driver: d, devicePath: "/dev/tty"}

	reqJSON, _ := json.Marshal(map[string]any{
		"questions": []Question{{Question: "What color?"}},
	})
	if err := f.Write(context.Background(), reqJSON); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := f.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	var answers []Answer
	if err := json.Unmarshal(data, &answers); err != nil {
		t.Fatalf("unmarshal answers: %v", err)
	}
	if len(answers) != 1 || answers[0].Answer != "blue" {
		t.Errorf("unexpected answers: %v", answers)
	}
}

func TestTtyFile_Write_MultipleQuestions(t *testing.T) {
	mockAsk := func(ctx context.Context, questions []Question) ([]Answer, error) {
		return make([]Answer, len(questions)), nil
	}

	d := NewDriver(mockAsk)
	f := &TtyFile{driver: d, devicePath: "/dev/tty"}

	questions := []Question{
		{Question: "Q1"},
		{Question: "Q2", Options: []string{"A", "B"}},
		{Question: "Q3", Options: []string{"X", "Y"}, MultiSelect: true},
	}
	reqJSON, _ := json.Marshal(map[string]any{"questions": questions})
	if err := f.Write(context.Background(), reqJSON); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := f.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if data == nil {
		t.Fatal("expected non-nil data")
	}
}

func TestTtyFile_Write_WithOptions(t *testing.T) {
	var receivedQuestions []Question
	mockAsk := func(ctx context.Context, questions []Question) ([]Answer, error) {
		receivedQuestions = questions
		return []Answer{{Answer: "Option B"}}, nil
	}

	d := NewDriver(mockAsk)
	f := &TtyFile{driver: d, devicePath: "/dev/tty"}

	reqJSON, _ := json.Marshal(map[string]any{
		"questions": []Question{
			{Question: "Pick one", Options: []string{"Option A", "Option B", "Option C"}},
		},
	})
	if err := f.Write(context.Background(), reqJSON); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if len(receivedQuestions) != 1 {
		t.Fatalf("expected 1 question passed to callback, got %d", len(receivedQuestions))
	}
	if len(receivedQuestions[0].Options) != 3 {
		t.Errorf("expected 3 options, got %d", len(receivedQuestions[0].Options))
	}
}

func TestTtyFile_Write_EmptyQuestions(t *testing.T) {
	d := NewDriver(func(ctx context.Context, q []Question) ([]Answer, error) {
		return nil, nil
	})
	f := &TtyFile{driver: d, devicePath: "/dev/tty"}

	reqJSON, _ := json.Marshal(map[string]any{"questions": []Question{}})
	err := f.Write(context.Background(), reqJSON)
	if err == nil {
		t.Fatal("expected error for empty questions")
	}
	var de *types.DriverError
	if !errors.As(err, &de) || de.Code != types.ErrInvalid {
		t.Errorf("expected ErrInvalid, got %v", err)
	}
}

func TestTtyFile_Write_TooManyQuestions(t *testing.T) {
	d := NewDriver(func(ctx context.Context, q []Question) ([]Answer, error) {
		return nil, nil
	})
	f := &TtyFile{driver: d, devicePath: "/dev/tty"}

	questions := make([]Question, 5)
	for i := range questions {
		questions[i] = Question{Question: "Q"}
	}
	reqJSON, _ := json.Marshal(map[string]any{"questions": questions})
	err := f.Write(context.Background(), reqJSON)
	if err == nil {
		t.Fatal("expected error for too many questions")
	}
	var de *types.DriverError
	if !errors.As(err, &de) || de.Code != types.ErrInvalid {
		t.Errorf("expected ErrInvalid, got %v", err)
	}
}

func TestTtyFile_Write_InvalidJSON(t *testing.T) {
	d := NewDriver(func(ctx context.Context, qs []Question) ([]Answer, error) {
		return nil, nil
	})
	f := &TtyFile{driver: d, devicePath: "/dev/tty"}

	err := f.Write(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	var de *types.DriverError
	if !errors.As(err, &de) || de.Code != types.ErrInvalid {
		t.Errorf("expected ErrInvalid, got %v", err)
	}
}

func TestTtyFile_Write_NoCallback(t *testing.T) {
	d := NewDriver(nil)
	f := &TtyFile{driver: d, devicePath: "/dev/tty"}

	reqJSON, _ := json.Marshal(map[string]any{
		"questions": []Question{{Question: "Hello?"}},
	})
	err := f.Write(context.Background(), reqJSON)
	if err == nil {
		t.Fatal("expected error when no callback")
	}
	var de *types.DriverError
	if !errors.As(err, &de) || de.Code != types.ErrServiceUnavailable {
		t.Errorf("expected ErrServiceUnavailable, got %v", err)
	}
}

func TestTtyFile_Write_Timeout(t *testing.T) {
	mockAsk := func(ctx context.Context, questions []Question) ([]Answer, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	d := NewDriver(mockAsk)
	f := &TtyFile{driver: d, devicePath: "/dev/tty"}

	// Use a short-lived context to simulate timeout without waiting 5 minutes
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	reqJSON, _ := json.Marshal(map[string]any{
		"questions": []Question{{Question: "Hello?"}},
	})
	err := f.Write(ctx, reqJSON)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	var de *types.DriverError
	if !errors.As(err, &de) {
		t.Fatalf("expected DriverError, got %T: %v", err, err)
	}
}

func TestTtyFile_Write_CallbackError(t *testing.T) {
	mockAsk := func(ctx context.Context, questions []Question) ([]Answer, error) {
		return nil, errors.New("ipc disconnected")
	}

	d := NewDriver(mockAsk)
	f := &TtyFile{driver: d, devicePath: "/dev/tty"}

	reqJSON, _ := json.Marshal(map[string]any{
		"questions": []Question{{Question: "Hello?"}},
	})
	err := f.Write(context.Background(), reqJSON)
	if err == nil {
		t.Fatal("expected error from callback")
	}
	var de *types.DriverError
	if !errors.As(err, &de) || de.Code != types.ErrDriver {
		t.Errorf("expected ErrDriver, got %v", err)
	}
}

func TestTtyFile_Read_NoData(t *testing.T) {
	f := &TtyFile{devicePath: "/dev/tty"}
	_, err := f.Read(0)
	if err == nil {
		t.Fatal("expected error when no data")
	}
}

func TestTtyFile_Read_EOF(t *testing.T) {
	f := &TtyFile{devicePath: "/dev/tty", response: []byte("data"), offset: 4}
	data, err := f.Read(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil on EOF, got %v", data)
	}
}

func TestTtyFile_Close(t *testing.T) {
	f := &TtyFile{devicePath: "/dev/tty", response: []byte("data")}

	if err := f.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Double close should error
	if err := f.Close(); err == nil {
		t.Fatal("expected error on double close")
	}

	// Write after close should error
	if err := f.Write(context.Background(), []byte("{}")); err == nil {
		t.Fatal("expected error on write after close")
	}

	// Read after close should error
	_, _ = f.Read(0)
}

func TestTtyFile_Stat(t *testing.T) {
	f := &TtyFile{devicePath: "/dev/tty", response: []byte("hello")}

	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !stat.IsDevice {
		t.Error("expected IsDevice=true")
	}
	if stat.DevicePath != "/dev/tty" {
		t.Errorf("expected DevicePath /dev/tty, got %q", stat.DevicePath)
	}
	if stat.Size != 5 {
		t.Errorf("expected Size=5, got %d", stat.Size)
	}
}

// --- FileFactory test ---

func TestFileFactory(t *testing.T) {
	d := NewDriver(nil)
	factory := FileFactory(d)

	file, err := factory("/ask", vfs.O_RDWR, "/tmp")
	if err != nil {
		t.Fatalf("FileFactory failed: %v", err)
	}

	ttyFile, ok := file.(*TtyFile)
	if !ok {
		t.Fatal("expected *TtyFile")
	}
	if ttyFile.devicePath != "/dev/tty/ask" {
		t.Errorf("unexpected devicePath: %q", ttyFile.devicePath)
	}
}

// --- Device registration integration test ---

func TestDeviceRegistration_Integration(t *testing.T) {
	devReg := vfs.NewDeviceRegistry()

	d := NewDriver(nil)
	err := devReg.RegisterWithDriver("/dev/tty", FileFactory(d), d)
	if err != nil {
		t.Fatalf("RegisterWithDriver failed: %v", err)
	}

	driver, ok := devReg.GetDriver("/dev/tty")
	if !ok {
		t.Fatal("driver not found for /dev/tty")
	}

	td, ok := driver.(vfs.ToolDescriptor)
	if !ok {
		t.Fatal("driver does not implement ToolDescriptor")
	}

	defs := td.ToolDefs()
	if len(defs) != 1 {
		t.Fatalf("expected 1 ToolDef, got %d", len(defs))
	}
	if defs[0].Name != "ask" {
		t.Errorf("expected tool name 'ask', got %q", defs[0].Name)
	}

	// Verify RangeDrivers can discover
	found := false
	devReg.RangeDrivers(func(path string, d any) bool {
		if path == "/dev/tty" {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Error("/dev/tty not found via RangeDrivers")
	}
}
