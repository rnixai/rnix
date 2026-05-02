package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// SpawnFunc is called when a cron job triggers. It spawns a new agent process.
// Returns the PID of the spawned process.
type SpawnFunc func(intent string, agentName string) (types.PID, error)

// CronDriver manages scheduled recurring jobs.
type CronDriver struct {
	store   *JobStore
	spawn   SpawnFunc
	timers  map[string]*time.Timer
	mu      sync.Mutex
	done    chan struct{}
	closed  bool
}

// Compile-time interface check.
var _ vfs.ToolDescriptor = (*CronDriver)(nil)

// NewDriver creates a new CronDriver.
func NewDriver(dataDir string, spawn SpawnFunc) *CronDriver {
	return &CronDriver{
		store:  NewJobStore(dataDir),
		spawn:  spawn,
		timers: make(map[string]*time.Timer),
		done:   make(chan struct{}),
	}
}

// LoadAndStart loads persisted jobs and starts their timers.
func (d *CronDriver) LoadAndStart() error {
	if err := d.store.Load(); err != nil {
		return err
	}

	d.store.PruneExpired()

	for _, job := range d.store.List() {
		d.scheduleJob(job)
	}
	return nil
}

// Close stops all timers and cleans up.
func (d *CronDriver) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return
	}
	d.closed = true
	close(d.done)

	for id, timer := range d.timers {
		timer.Stop()
		delete(d.timers, id)
	}
}

// ToolDefs returns the VFS tool definitions for the cron device.
func (d *CronDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:              "cron_create",
			Description:       loadPrompt("cron_create"),
			IsReadOnly:        false,
			IsConcurrencySafe: false,
			IsDestructive:     false,
			ShouldDefer:       true,
			SearchHint:        "cron schedule timer periodic recurring",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"schedule": map[string]any{
						"type":        "string",
						"description": "5-field cron expression (e.g. \"*/5 * * * *\")",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "Prompt to enqueue at each fire time",
					},
					"agent": map[string]any{
						"type":        "string",
						"description": "Optional agent name (default: stem)",
					},
					"durable": map[string]any{
						"type":        "boolean",
						"description": "If true, persist across restarts",
					},
				},
				"required": []string{"schedule", "prompt"},
			},
		},
		{
			Name:              "cron_list",
			Description:       loadPrompt("cron_list"),
			IsReadOnly:        true,
			IsConcurrencySafe: false,
			IsDestructive:     false,
			ShouldDefer:       true,
			SearchHint:        "cron list scheduled jobs",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:              "cron_delete",
			Description:       loadPrompt("cron_delete"),
			IsReadOnly:        false,
			IsConcurrencySafe: false,
			IsDestructive:     false,
			ShouldDefer:       true,
			SearchHint:        "cron delete remove job",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "Cron job ID to delete",
					},
				},
				"required": []string{"id"},
			},
		},
	}
}

// FileFactory returns a VFS file factory for the cron device.
func FileFactory(d *CronDriver) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return &CronFile{
			driver:     d,
			devicePath: "/dev/cron" + subpath,
		}, nil
	}
}

// CronFile is a per-open VFS file handle for the cron device.
type CronFile struct {
	driver     *CronDriver
	devicePath string
	buf        []byte
	closed     bool
}

func (f *CronFile) Write(_ context.Context, data []byte) error {
	if f.closed {
		return &types.DriverError{Op: "write", Device: f.devicePath, Err: fmt.Errorf("file closed"), Code: types.ErrInvalid}
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return &types.DriverError{Op: "write", Device: f.devicePath, Err: fmt.Errorf("invalid JSON: %w", err), Code: types.ErrInvalid}
	}

	op := f.detectOp(raw)
	var result any
	var err error

	switch op {
	case "create":
		result, err = f.opCreate(raw)
	case "list":
		result, err = f.opList()
	case "delete":
		result, err = f.opDelete(raw)
	default:
		err = &types.DriverError{Op: "write", Device: f.devicePath, Err: fmt.Errorf("cannot determine operation"), Code: types.ErrInvalid}
	}

	if err != nil {
		return err
	}

	f.buf, _ = json.Marshal(result)
	return nil
}

func (f *CronFile) Read(_ int) ([]byte, error) {
	if f.closed {
		return nil, &types.DriverError{Op: "read", Device: f.devicePath, Err: fmt.Errorf("file closed"), Code: types.ErrInvalid}
	}
	if f.buf == nil {
		return nil, &types.DriverError{Op: "read", Device: f.devicePath, Err: fmt.Errorf("no data available"), Code: types.ErrInvalid}
	}
	data := f.buf
	f.buf = nil
	return data, nil
}

func (f *CronFile) Close() error {
	if f.closed {
		return &types.DriverError{Op: "close", Device: f.devicePath, Err: fmt.Errorf("already closed"), Code: types.ErrInvalid}
	}
	f.closed = true
	return nil
}

func (f *CronFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{
		DevicePath: f.devicePath,
		IsDevice:   true,
	}, nil
}

func (f *CronFile) detectOp(raw map[string]any) string {
	if _, ok := raw["schedule"]; ok {
		return "create"
	}
	if _, ok := raw["id"]; ok {
		return "delete"
	}
	return "list"
}

func (f *CronFile) opCreate(raw map[string]any) (*CronJob, error) {
	schedule, _ := raw["schedule"].(string)
	prompt, _ := raw["prompt"].(string)
	agent, _ := raw["agent"].(string)
	durable, _ := raw["durable"].(bool)

	if schedule == "" {
		return nil, &types.DriverError{Op: "cron_create", Device: f.devicePath, Err: fmt.Errorf("schedule is required"), Code: types.ErrInvalid}
	}
	if prompt == "" {
		return nil, &types.DriverError{Op: "cron_create", Device: f.devicePath, Err: fmt.Errorf("prompt is required"), Code: types.ErrInvalid}
	}
	if agent == "" {
		agent = "stem"
	}

	expr, err := ParseCron(schedule)
	if err != nil {
		return nil, &types.DriverError{Op: "cron_create", Device: f.devicePath, Err: fmt.Errorf("invalid schedule: %w", err), Code: types.ErrInvalid}
	}

	nextRun := expr.NextAfter(time.Now())

	job, err := f.driver.store.Add(schedule, prompt, agent, durable, nextRun)
	if err != nil {
		return nil, &types.DriverError{Op: "cron_create", Device: f.devicePath, Err: err, Code: types.ErrResourceExhausted}
	}

	f.driver.scheduleJob(job)

	return job, nil
}

func (f *CronFile) opList() ([]*CronJob, error) {
	return f.driver.store.List(), nil
}

func (f *CronFile) opDelete(raw map[string]any) (map[string]any, error) {
	id, _ := raw["id"].(string)
	if id == "" {
		return nil, &types.DriverError{Op: "cron_delete", Device: f.devicePath, Err: fmt.Errorf("id is required"), Code: types.ErrInvalid}
	}

	f.driver.mu.Lock()
	if timer, ok := f.driver.timers[id]; ok {
		timer.Stop()
		delete(f.driver.timers, id)
	}
	f.driver.mu.Unlock()

	if !f.driver.store.Delete(id) {
		return nil, &types.DriverError{Op: "cron_delete", Device: f.devicePath, Err: fmt.Errorf("job %q not found", id), Code: types.ErrNotFound}
	}

	return map[string]any{"deleted": id}, nil
}

// scheduleJob sets up a timer for the next run of a cron job.
func (d *CronDriver) scheduleJob(job *CronJob) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return
	}

	nextRun := job.NextRun
	delay := time.Until(nextRun)
	if delay < 0 {
		// If next run is in the past, compute new next run
		expr, err := ParseCron(job.Schedule)
		if err != nil {
			return
		}
		nextRun = expr.NextAfter(time.Now())
		if nextRun.IsZero() {
			log.Printf("[cron] job %s: no valid next run time, disabling", job.ID)
			return
		}
		d.store.UpdateNextRun(job.ID, nextRun)
		delay = time.Until(nextRun)
	}
	if nextRun.IsZero() {
		log.Printf("[cron] job %s: next run is zero, disabling", job.ID)
		return
	}

	timer := time.AfterFunc(delay, func() {
		d.fireJob(job)
	})
	d.timers[job.ID] = timer
}

// fireJob executes a cron job and reschedules it.
func (d *CronDriver) fireJob(job *CronJob) {
	// Check if we're shutting down
	select {
	case <-d.done:
		return
	default:
	}

	// Spawn the agent process
	if d.spawn != nil {
		pid, err := d.spawn(job.Prompt, job.Agent)
		if err != nil {
			log.Printf("[cron] job %s spawn failed (agent=%s): %v", job.ID, job.Agent, err)
		} else {
			log.Printf("[cron] job %s spawned PID %d", job.ID, pid)
		}
	}

	// Compute next run
	expr, err := ParseCron(job.Schedule)
	if err != nil {
		log.Printf("[cron] job %s: invalid schedule on reschedule: %v", job.ID, err)
		return
	}
	nextRun := expr.NextAfter(time.Now())
	if nextRun.IsZero() {
		log.Printf("[cron] job %s: no valid next run time after fire, disabling", job.ID)
		return
	}
	d.store.UpdateNextRun(job.ID, nextRun)

	// Reschedule
	d.scheduleJob(job)
}
