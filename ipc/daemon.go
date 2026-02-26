package ipc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	daemonStartTimeout = 3 * time.Second
	daemonPollInterval = 100 * time.Millisecond
)

// DaemonExe is the executable path used to re-exec the daemon process.
// Defaults to os.Args[0]; overridable for testing.
var DaemonExe = ""

func daemonExe() string {
	if DaemonExe != "" {
		return DaemonExe
	}
	return os.Args[0]
}

// EnsureDaemon ensures a crux daemon is running and returns a connected Client.
// If the daemon is already running, connects to it. Otherwise, starts a new daemon
// and waits for it to become ready.
func EnsureDaemon() (*Client, error) {
	sockPath := SocketPath()

	client, err := tryConnect(sockPath)
	if err == nil {
		return client, nil
	}

	cleanStaleSocket(sockPath)

	if err := startDaemon(); err != nil {
		return nil, fmt.Errorf("ipc: start daemon: %w", err)
	}

	return waitForDaemon(sockPath)
}

// tryConnect attempts to connect and ping the daemon.
func tryConnect(sockPath string) (*Client, error) {
	client, err := DialTimeout(sockPath, time.Second)
	if err != nil {
		return nil, err
	}

	if _, err := client.Ping(); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

// cleanStaleSocket removes a leftover socket file if present.
func cleanStaleSocket(sockPath string) {
	os.Remove(sockPath)
}

// startDaemon launches a new daemon process in the background.
func startDaemon() error {
	sockPath := SocketPath()
	sockDir := filepath.Dir(sockPath)
	if err := os.MkdirAll(sockDir, 0700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}

	cmd := exec.Command(daemonExe(), "daemon", "--internal")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("exec daemon: %w", err)
	}

	writePIDFile(sockDir, cmd.Process.Pid)

	return nil
}

// waitForDaemon polls until the daemon is ready, up to daemonStartTimeout.
func waitForDaemon(sockPath string) (*Client, error) {
	deadline := time.Now().Add(daemonStartTimeout)
	for time.Now().Before(deadline) {
		client, err := tryConnect(sockPath)
		if err == nil {
			return client, nil
		}
		time.Sleep(daemonPollInterval)
	}
	return nil, fmt.Errorf("ipc: daemon did not start within %s", daemonStartTimeout)
}

// writePIDFile writes the daemon's PID to crux.pid for diagnostics.
func writePIDFile(dir string, pid int) {
	pidPath := filepath.Join(dir, "crux.pid")
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0600)
}

// IsDaemonRunning checks if a daemon is reachable at the default socket path.
func IsDaemonRunning() bool {
	client, err := tryConnect(SocketPath())
	if err != nil {
		return false
	}
	client.Close()
	return true
}
