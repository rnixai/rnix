//go:build linux

package kernel

import (
	"bytes"
	"log"
	"os"
	"strconv"
	"syscall"
	"time"
)

// osReconcileSupported gates StartOSReconcileDaemon — Linux exposes
// /proc/<pid>/environ, the only portable-enough source for the RNIX_PROC_UUID
// self-marker scan.
const osReconcileSupported = true

// rnixProcUUIDEnvPrefix is the environ key (with '=') the LLM CLI drivers inject
// via configureCommandRnixParentEnv. Matched exactly — never by process name.
const rnixProcUUIDEnvPrefix = "RNIX_PROC_UUID="

// defaultOSProcScanner walks /proc/<pid> and returns every process whose
// environ carries RNIX_PROC_UUID. Per-process read failures (EPERM for
// foreign-UID processes, ESRCH when a pid exits mid-scan) are skipped silently
// — the scan is best-effort and re-runs every interval.
func defaultOSProcScanner() []osCliProc {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		log.Printf("[os-reconcile] warn: read /proc: %v", err)
		return nil
	}
	var out []osCliProc
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, perr := strconv.Atoi(e.Name())
		if perr != nil || pid <= 0 {
			continue // non-numeric /proc entries (self, sys, ...)
		}
		uuid, ok := readProcUUID(pid)
		if !ok || uuid == "" {
			continue
		}
		out = append(out, osCliProc{
			OSPid: pid,
			UUID:  uuid,
			Argv:  readProcArgv(pid),
		})
	}
	return out
}

// readProcUUID extracts the RNIX_PROC_UUID value from /proc/<pid>/environ
// (NUL-separated KEY=VALUE pairs). ok=false on any read error.
func readProcUUID(pid int) (string, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return "", false
	}
	for kv := range bytes.SplitSeq(data, []byte{0}) {
		if bytes.HasPrefix(kv, []byte(rnixProcUUIDEnvPrefix)) {
			return string(kv[len(rnixProcUUIDEnvPrefix):]), true
		}
	}
	return "", true // readable but not tagged
}

// readProcArgv returns a truncated, space-joined /proc/<pid>/cmdline for logs.
func readProcArgv(pid int) string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return ""
	}
	// cmdline is NUL-separated; join with spaces and trim a trailing NUL.
	data = bytes.TrimRight(data, "\x00")
	joined := bytes.ReplaceAll(data, []byte{0}, []byte{' '})
	if len(joined) > osReconcileArgvMax {
		joined = joined[:osReconcileArgvMax]
	}
	return string(joined)
}

// defaultOSProcKiller reaps the process group of osPid with SIGTERM → grace →
// SIGKILL. The pgid is resolved via syscall.Getpgid; if that fails (process
// already gone) the pid is treated as its own group leader. All signals are
// best-effort — ESRCH means the target is already gone.
func defaultOSProcKiller(osPid int) {
	pgid, err := syscall.Getpgid(osPid)
	if err != nil {
		pgid = osPid
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	time.Sleep(osReconcileGrace)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	log.Printf("[os-reconcile] reaped process group pgid=%d (os_pid=%d)", pgid, osPid)
}
