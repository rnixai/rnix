package ipc

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"
)

func TestBRouteResumeProgressProbe_SpawnVsResumeAndAttachDebug(t *testing.T) {
	t.Run("spawn emits StreamProgress on its stream", func(t *testing.T) {
		client, _, _, _ := setupResumeIPCTest(t)
		var progress []ProgressPayload
		_, final, err := client.SpawnAndWatch(SpawnRequest{
			Intent:   "b-route spawn progress probe",
			Provider: "claude",
			Model:    "claude-4",
		}, func(ev StreamEvent) {
			if ev.Type != StreamProgress {
				return
			}
			var pp ProgressPayload
			if err := json.Unmarshal(ev.Payload, &pp); err == nil {
				progress = append(progress, pp)
			}
		})
		if err != nil {
			t.Fatalf("SpawnAndWatch: %v", err)
		}
		if len(progress) == 0 || progress[0].Event != "spawn" {
			t.Fatalf("spawn stream progress = %#v, want first event spawn", progress)
		}
		if final == nil || final.ExitCode != 0 {
			t.Fatalf("spawn final = %#v, want exit 0", final)
		}
	})

	for _, tc := range []struct {
		name string
		uuid string
		fork bool
	}{
		{name: "resume no fork", uuid: "broute-probe-0000-0000-000000000001", fork: false},
		{name: "resume fork", uuid: "broute-probe-0000-0000-000000000002", fork: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _, baseDir, llmFile := setupResumeIPCTest(t)
			writeIPCTestData(t, baseDir, tc.uuid, 2)
			reached, release := llmFile.parkOnWrite()

			resp, err := client.ResumeWithOpts(tc.uuid, tc.fork)
			if err != nil {
				t.Fatalf("ResumeWithOpts(fork=%v): %v", tc.fork, err)
			}
			if resp.ResumedFromStep != 3 {
				t.Fatalf("ResumedFromStep = %d, want 3", resp.ResumedFromStep)
			}
			if tc.fork && resp.UUID == tc.uuid {
				t.Fatalf("fork resume reused origin UUID %q", tc.uuid)
			}
			if !tc.fork && resp.UUID != tc.uuid {
				t.Fatalf("non-fork resume UUID = %q, want %q", resp.UUID, tc.uuid)
			}

			select {
			case <-reached:
			case <-time.After(3 * time.Second):
				t.Fatal("resume process did not reach LLM Read gate")
			}

			debugClient, err := Dial(client.socketPath)
			if err != nil {
				t.Fatalf("Dial debug: %v", err)
			}
			defer debugClient.Close()

			events := make(chan SyscallEventWire, 8)
			errs := make(chan error, 1)
			// 10-10 S-1 race fix: wait for the attach initial OK (tap registered
			// server-side) BEFORE releasing the gated process — otherwise the
			// resumed process's remaining syscalls can drain before the debug tap
			// registers and the fork case misses live events (probe race, not a
			// product gap).
			attachReady := make(chan struct{})
			go func() {
				errs <- debugClient.AttachDebugWithReady(resp.PID, func() { close(attachReady) }, func(ev SyscallEventWire) {
					events <- ev
				})
			}()

			select {
			case <-attachReady:
			case <-time.After(3 * time.Second):
				t.Fatal("attach_debug initial response not received")
			}

			close(release)

			select {
			case ev := <-events:
				if ev.Syscall == "" {
					t.Fatalf("attach_debug event has empty syscall: %#v", ev)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("attach_debug did not receive a live resume syscall event")
			}

			select {
			case err := <-errs:
				if err != nil {
					t.Fatalf("AttachDebug: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("attach_debug did not finish")
			}
		})
	}
}

func TestBRouteResumeProgressProbe_ResumeIPCDoesNotStreamProgress(t *testing.T) {
	client, _, baseDir, llmFile := setupResumeIPCTest(t)
	uuid := "broute-stream-0000-0000-000000000001"
	writeIPCTestData(t, baseDir, uuid, 2)
	reached, release := llmFile.parkOnRead()
	defer close(release)

	conn, err := net.Dial("unix", client.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	req := Request{Method: MethodResume, Payload: marshalJSON(ResumeRequest{UUID: uuid, Fork: true})}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("encode resume request: %v", err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read resume response: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("decode resume response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resume response not OK: %#v", resp.Error)
	}

	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("resume process did not reach LLM Read gate")
	}

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	line, err = reader.ReadBytes('\n')
	if err == nil {
		var ev StreamEvent
		if json.Unmarshal(line, &ev) == nil && ev.Type == StreamProgress {
			t.Fatalf("resume IPC unexpectedly streamed progress event: %s", line)
		}
		t.Fatalf("resume IPC unexpectedly streamed extra line: %s", line)
	}
}

func TestBRouteResumeProgressProbe_IsolatedCurrentHeadDaemonBlockedWithoutProviderEnv(t *testing.T) {
	if os.Getenv("OPENCODE_ZEN_API_KEY") != "" || os.Getenv("DEEPSEEK_API_KEY") != "" {
		t.Skip("provider env is present")
	}
	t.Log("current shell has no OPENCODE_ZEN_API_KEY or DEEPSEEK_API_KEY; current-HEAD isolated daemon cannot run bmad-prd-visual token probe against configured project provider")
}
