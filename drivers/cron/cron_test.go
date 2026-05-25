package cron

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// --- CronExpr parser tests ---

func TestParseCron_StandardFields(t *testing.T) {
	tests := []struct {
		expr string
		ok   bool
	}{
		{"* * * * *", true},
		{"0 0 * * *", true},
		{"*/15 * * * *", true},
		{"0 9 * * 1-5", true},
		{"30 4 1,15 * *", true},
		{"0 0 * * 0", true},
		// Invalid
		{"", false},
		{"* * *", false},        // too few fields
		{"60 * * * *", false},   // minute out of range
		{"* 25 * * *", false},   // hour out of range
		{"* * 32 * *", false},   // day out of range
		{"* * * 13 *", false},   // month out of range
		{"* * * * 7", false},    // dow out of range
		{"*/0 * * * *", false},  // step 0
	}

	for _, tt := range tests {
		_, err := ParseCron(tt.expr)
		if tt.ok && err != nil {
			t.Errorf("ParseCron(%q) unexpected error: %v", tt.expr, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("ParseCron(%q) expected error, got nil", tt.expr)
		}
	}
}

func TestParseCron_Macros(t *testing.T) {
	tests := []struct {
		expr string
		ok   bool
	}{
		{"@hourly", true},
		{"@daily", true},
		{"@weekly", true},
		{"@monthly", true},
		{"@every 5m", true},
		{"@every 1h", true},
		{"@every 2h30m", true},
		// Invalid
		{"@every 30s", false},     // less than 1 minute
		{"@every invalid", false},
		{"@unknown", false},
	}

	for _, tt := range tests {
		_, err := ParseCron(tt.expr)
		if tt.ok && err != nil {
			t.Errorf("ParseCron(%q) unexpected error: %v", tt.expr, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("ParseCron(%q) expected error, got nil", tt.expr)
		}
	}
}

func TestCronExpr_NextAfter(t *testing.T) {
	// Every 5 minutes
	expr, err := ParseCron("*/5 * * * *")
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2025, 1, 15, 10, 3, 0, 0, time.UTC)
	next := expr.NextAfter(base)

	if next.Minute() != 5 {
		t.Errorf("expected minute 5, got %d", next.Minute())
	}
	if next.Hour() != 10 {
		t.Errorf("expected hour 10, got %d", next.Hour())
	}
}

func TestCronExpr_NextAfter_Every(t *testing.T) {
	expr, err := ParseCron("@every 10m")
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	next := expr.NextAfter(base)

	expected := base.Add(10 * time.Minute)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

// --- ToolDef metadata tests ---

func TestCronDriver_ToolDefs_Metadata(t *testing.T) {
	d := NewDriver("", nil)
	defs := d.ToolDefs()

	if len(defs) != 3 {
		t.Fatalf("expected 3 ToolDefs, got %d", len(defs))
	}

	tests := []struct {
		name       string
		readOnly   bool
		concurrent bool
		destr      bool
		deferred   bool
		hint       string
	}{
		{"CronCreate", false, false, false, true, "cron schedule timer periodic recurring"},
		{"CronList", true, false, false, true, "cron list scheduled jobs"},
		{"CronDelete", false, false, false, true, "cron delete remove job"},
	}

	for i, tt := range tests {
		def := defs[i]
		if def.Name != tt.name {
			t.Errorf("[%d] expected name %q, got %q", i, tt.name, def.Name)
		}
		if def.IsReadOnly != tt.readOnly {
			t.Errorf("[%s] IsReadOnly: want %v, got %v", tt.name, tt.readOnly, def.IsReadOnly)
		}
		if def.IsConcurrencySafe != tt.concurrent {
			t.Errorf("[%s] IsConcurrencySafe: want %v, got %v", tt.name, tt.concurrent, def.IsConcurrencySafe)
		}
		if def.IsDestructive != tt.destr {
			t.Errorf("[%s] IsDestructive: want %v, got %v", tt.name, tt.destr, def.IsDestructive)
		}
		if def.ShouldDefer != tt.deferred {
			t.Errorf("[%s] ShouldDefer: want %v, got %v", tt.name, tt.deferred, def.ShouldDefer)
		}
		if def.SearchHint != tt.hint {
			t.Errorf("[%s] SearchHint: want %q, got %q", tt.name, tt.hint, def.SearchHint)
		}
	}
}

func TestCronDriver_ToolDescriptorInterface(t *testing.T) {
	var _ vfs.ToolDescriptor = (*CronDriver)(nil)
}

// --- JobStore tests ---

func TestJobStore_AddAndGet(t *testing.T) {
	s := NewJobStore("")
	next := time.Now().Add(time.Hour)

	job, err := s.Add("@hourly", "check status", "stem", false, next)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == "" {
		t.Error("job ID should not be empty")
	}
	if job.Schedule != "@hourly" {
		t.Errorf("unexpected schedule: %q", job.Schedule)
	}

	got := s.Get(job.ID)
	if got == nil {
		t.Fatal("expected to find job")
	}
	if got.ID != job.ID {
		t.Error("ID mismatch")
	}

	missing := s.Get("nonexistent")
	if missing != nil {
		t.Error("expected nil for missing job")
	}
}

func TestJobStore_MaxLimit(t *testing.T) {
	s := NewJobStore("")
	next := time.Now().Add(time.Hour)

	for i := range MaxActiveJobs {
		_, err := s.Add("@hourly", "task", "stem", false, next)
		if err != nil {
			t.Fatalf("unexpected error at job %d: %v", i, err)
		}
	}

	_, err := s.Add("@hourly", "one too many", "stem", false, next)
	if err == nil {
		t.Fatal("expected error when exceeding max jobs")
	}
}

func TestJobStore_Delete(t *testing.T) {
	s := NewJobStore("")
	next := time.Now().Add(time.Hour)
	job, _ := s.Add("@hourly", "task", "stem", false, next)

	if !s.Delete(job.ID) {
		t.Fatal("expected Delete to return true")
	}
	if s.Delete(job.ID) {
		t.Error("expected Delete to return false for already deleted")
	}
	if s.Get(job.ID) != nil {
		t.Error("expected nil after delete")
	}
}

func TestJobStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	s1 := NewJobStore(dir)
	next := time.Now().Add(time.Hour)

	// Add a durable job
	_, err := s1.Add("@daily", "backup", "stem", true, next)
	if err != nil {
		t.Fatal(err)
	}
	// Add a non-durable job (should NOT be persisted)
	_, err = s1.Add("@hourly", "check", "stem", false, next)
	if err != nil {
		t.Fatal(err)
	}

	// Verify file exists
	path := filepath.Join(dir, "jobs.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("jobs.json not found: %v", err)
	}

	// Load in new store
	s2 := NewJobStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}

	jobs := s2.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 durable job, got %d", len(jobs))
	}
	if jobs[0].Prompt != "backup" {
		t.Errorf("unexpected prompt: %q", jobs[0].Prompt)
	}
}

func TestJobStore_PruneExpired(t *testing.T) {
	s := NewJobStore("")
	next := time.Now().Add(time.Hour)

	job, _ := s.Add("@hourly", "old task", "stem", false, next)
	// Manually set created time to past
	j := s.Get(job.ID)
	j.CreatedAt = time.Now().Add(-(NonDurableExpiry + time.Hour))
	s.jobs.Store(job.ID, j)

	pruned := s.PruneExpired()
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}
	if s.Count() != 0 {
		t.Error("expected 0 jobs after prune")
	}
}

// --- CronFile VFS tests ---

func TestCronFile_Create(t *testing.T) {
	spawned := make(chan string, 1)
	d := NewDriver("", func(intent, agent string) (types.PID, error) {
		spawned <- intent
		return 1, nil
	})
	f := &CronFile{driver: d, devicePath: "/dev/cron"}

	data, _ := json.Marshal(map[string]any{
		"schedule": "@every 5m",
		"prompt":   "check health",
		"agent":    "monitor",
	})

	if err := f.Write(context.Background(), data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	resp, err := f.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	var job CronJob
	if err := json.Unmarshal(resp, &job); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if job.Schedule != "@every 5m" {
		t.Errorf("unexpected schedule: %q", job.Schedule)
	}
	if job.Agent != "monitor" {
		t.Errorf("unexpected agent: %q", job.Agent)
	}
	if job.ID == "" {
		t.Error("job should have an ID")
	}

	d.Close()
}

func TestCronFile_List(t *testing.T) {
	d := NewDriver("", nil)
	f := &CronFile{driver: d, devicePath: "/dev/cron"}

	// Create a job
	data, _ := json.Marshal(map[string]any{"schedule": "@hourly", "prompt": "task1"})
	f.Write(context.Background(), data)
	f.Read(0)

	// List
	listData, _ := json.Marshal(map[string]any{})
	f.Write(context.Background(), listData)
	resp, _ := f.Read(0)

	var jobs []*CronJob
	json.Unmarshal(resp, &jobs)
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}

	d.Close()
}

func TestCronFile_Delete(t *testing.T) {
	d := NewDriver("", nil)
	f := &CronFile{driver: d, devicePath: "/dev/cron"}

	// Create
	data, _ := json.Marshal(map[string]any{"schedule": "@hourly", "prompt": "task1"})
	f.Write(context.Background(), data)
	resp, _ := f.Read(0)
	var job CronJob
	json.Unmarshal(resp, &job)

	// Delete
	delData, _ := json.Marshal(map[string]any{"id": job.ID})
	if err := f.Write(context.Background(), delData); err != nil {
		t.Fatalf("delete write failed: %v", err)
	}
	resp, _ = f.Read(0)
	var result map[string]any
	json.Unmarshal(resp, &result)
	if result["deleted"] != job.ID {
		t.Errorf("expected deleted=%q, got %v", job.ID, result["deleted"])
	}

	d.Close()
}

func TestCronFile_Delete_NotFound(t *testing.T) {
	d := NewDriver("", nil)
	f := &CronFile{driver: d, devicePath: "/dev/cron"}

	data, _ := json.Marshal(map[string]any{"id": "nonexistent"})
	err := f.Write(context.Background(), data)
	if err == nil {
		t.Fatal("expected error for missing job")
	}
	var de *types.DriverError
	if !errors.As(err, &de) || de.Code != types.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	d.Close()
}

func TestCronFile_Create_InvalidSchedule(t *testing.T) {
	d := NewDriver("", nil)
	f := &CronFile{driver: d, devicePath: "/dev/cron"}

	data, _ := json.Marshal(map[string]any{"schedule": "bad expr", "prompt": "test"})
	err := f.Write(context.Background(), data)
	if err == nil {
		t.Fatal("expected error for invalid schedule")
	}
	var de *types.DriverError
	if !errors.As(err, &de) || de.Code != types.ErrInvalid {
		t.Errorf("expected ErrInvalid, got %v", err)
	}

	d.Close()
}

func TestCronFile_Create_MissingFields(t *testing.T) {
	d := NewDriver("", nil)
	f := &CronFile{driver: d, devicePath: "/dev/cron"}

	// Missing prompt
	data, _ := json.Marshal(map[string]any{"schedule": "@hourly"})
	err := f.Write(context.Background(), data)
	if err == nil {
		t.Fatal("expected error for missing prompt")
	}

	// Missing schedule
	data, _ = json.Marshal(map[string]any{"prompt": "test"})
	err = f.Write(context.Background(), data)
	// This will be treated as "list" op since no "schedule" or "id"
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d.Close()
}

func TestCronFile_InvalidJSON(t *testing.T) {
	d := NewDriver("", nil)
	f := &CronFile{driver: d, devicePath: "/dev/cron"}

	err := f.Write(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	var de *types.DriverError
	if !errors.As(err, &de) || de.Code != types.ErrInvalid {
		t.Errorf("expected ErrInvalid, got %v", err)
	}

	d.Close()
}

func TestCronFile_Close(t *testing.T) {
	d := NewDriver("", nil)
	f := &CronFile{driver: d, devicePath: "/dev/cron"}

	if err := f.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := f.Close(); err == nil {
		t.Fatal("expected error on double close")
	}
	if err := f.Write(context.Background(), []byte("{}")); err == nil {
		t.Fatal("expected error on write after close")
	}

	d.Close()
}

func TestCronFile_Stat(t *testing.T) {
	d := NewDriver("", nil)
	f := &CronFile{driver: d, devicePath: "/dev/cron"}

	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !stat.IsDevice {
		t.Error("expected IsDevice=true")
	}
	if stat.DevicePath != "/dev/cron" {
		t.Errorf("expected DevicePath /dev/cron, got %q", stat.DevicePath)
	}

	d.Close()
}

// --- Device registration integration test ---

func TestDeviceRegistration_Integration(t *testing.T) {
	devReg := vfs.NewDeviceRegistry()

	d := NewDriver("", nil)
	err := devReg.RegisterWithDriver("/dev/cron", FileFactory(d), d)
	if err != nil {
		t.Fatalf("RegisterWithDriver failed: %v", err)
	}

	driver, ok := devReg.GetDriver("/dev/cron")
	if !ok {
		t.Fatal("driver not found for /dev/cron")
	}

	td, ok := driver.(vfs.ToolDescriptor)
	if !ok {
		t.Fatal("driver does not implement ToolDescriptor")
	}

	defs := td.ToolDefs()
	if len(defs) != 3 {
		t.Fatalf("expected 3 ToolDefs, got %d", len(defs))
	}

	found := false
	devReg.RangeDrivers(func(path string, d any) bool {
		if path == "/dev/cron" {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Error("/dev/cron not found via RangeDrivers")
	}

	d.Close()
}
