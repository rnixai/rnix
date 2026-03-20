package vfs

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

func TestDeviceRegistry_Register(t *testing.T) {
	t.Run("register succeeds", func(t *testing.T) {
		reg := NewDeviceRegistry()
		factory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			return &mockFile{}, nil
		}
		if err := reg.Register("/dev/llm/claude", factory); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	})

	t.Run("duplicate register returns error", func(t *testing.T) {
		reg := NewDeviceRegistry()
		factory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			return &mockFile{}, nil
		}
		if err := reg.Register("/dev/llm/claude", factory); err != nil {
			t.Fatalf("first Register failed: %v", err)
		}
		if err := reg.Register("/dev/llm/claude", factory); err == nil {
			t.Fatal("expected error for duplicate register, got nil")
		}
	})
}

func TestDeviceRegistry_Open(t *testing.T) {
	t.Run("open registered path returns VFSFile", func(t *testing.T) {
		reg := NewDeviceRegistry()
		called := false
		factory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			called = true
			if subpath != "" {
				t.Fatalf("expected empty subpath for exact match, got %q", subpath)
			}
			if flags != O_RDWR {
				t.Fatalf("expected O_RDWR, got %d", flags)
			}
			return &mockFile{}, nil
		}
		_ = reg.Register("/dev/llm/claude", factory)

		file, err := reg.Open("/dev/llm/claude", O_RDWR, "")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		if file == nil {
			t.Fatal("expected non-nil VFSFile")
		}
		if !called {
			t.Fatal("factory was not called")
		}
	})

	t.Run("open unregistered path returns error", func(t *testing.T) {
		reg := NewDeviceRegistry()
		_, err := reg.Open("/dev/nonexistent", O_RDONLY, "")
		if err == nil {
			t.Fatal("expected error for unregistered path, got nil")
		}
	})
}

func TestDeviceRegistry_PrefixMatch(t *testing.T) {
	t.Run("prefix match passes subpath", func(t *testing.T) {
		reg := NewDeviceRegistry()
		var capturedSubpath string
		factory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			capturedSubpath = subpath
			return &mockFile{}, nil
		}
		_ = reg.Register("/dev/fs", factory)

		file, err := reg.Open("/dev/fs/path/to/file", O_RDONLY, "")
		if err != nil {
			t.Fatalf("Open with prefix failed: %v", err)
		}
		if file == nil {
			t.Fatal("expected non-nil VFSFile")
		}
		if capturedSubpath != "/path/to/file" {
			t.Fatalf("expected subpath '/path/to/file', got %q", capturedSubpath)
		}
	})

	t.Run("longest prefix wins", func(t *testing.T) {
		reg := NewDeviceRegistry()
		var calledPath string
		shortFactory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			calledPath = "short"
			return &mockFile{}, nil
		}
		longFactory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			calledPath = "long"
			return &mockFile{}, nil
		}
		_ = reg.Register("/dev", shortFactory)
		_ = reg.Register("/dev/fs", longFactory)

		_, err := reg.Open("/dev/fs/myfile", O_RDONLY, "")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		if calledPath != "long" {
			t.Fatalf("expected longest prefix match (long), got %q", calledPath)
		}
	})

	t.Run("exact match preferred over prefix", func(t *testing.T) {
		reg := NewDeviceRegistry()
		var calledPath string
		prefixFactory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			calledPath = "prefix"
			return &mockFile{}, nil
		}
		exactFactory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			calledPath = "exact"
			return &mockFile{}, nil
		}
		_ = reg.Register("/dev/fs", prefixFactory)
		_ = reg.Register("/dev/fs/special", exactFactory)

		_, err := reg.Open("/dev/fs/special", O_RDONLY, "")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		if calledPath != "exact" {
			t.Fatalf("expected exact match, got %q", calledPath)
		}
	})
}

func TestDeviceRegistry_Stat(t *testing.T) {
	t.Run("stat registered device", func(t *testing.T) {
		reg := NewDeviceRegistry()
		factory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			return &mockFile{}, nil
		}
		_ = reg.Register("/dev/llm/claude", factory)

		stat, err := reg.Stat("/dev/llm/claude")
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if !stat.IsDevice {
			t.Fatal("expected IsDevice=true")
		}
		if stat.DevicePath != "/dev/llm/claude" {
			t.Fatalf("expected DevicePath '/dev/llm/claude', got %q", stat.DevicePath)
		}
	})

	t.Run("stat unregistered path returns error", func(t *testing.T) {
		reg := NewDeviceRegistry()
		_, err := reg.Stat("/dev/nonexistent")
		if err == nil {
			t.Fatal("expected error for unregistered path")
		}
	})

	t.Run("stat prefix matched path", func(t *testing.T) {
		reg := NewDeviceRegistry()
		factory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			return &mockFile{}, nil
		}
		_ = reg.Register("/dev/fs", factory)

		stat, err := reg.Stat("/dev/fs/some/file")
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if stat.DevicePath != "/dev/fs" {
			t.Fatalf("expected DevicePath '/dev/fs', got %q", stat.DevicePath)
		}
	})
}

func TestDeviceRegistry_ConcurrentRegister(t *testing.T) {
	reg := NewDeviceRegistry()
	var wg sync.WaitGroup
	const n = 50

	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := fmt.Sprintf("/dev/test/%d", i)
			factory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
				return &mockFile{}, nil
			}
			_ = reg.Register(path, factory)
		}(i)
	}
	wg.Wait()
}

func TestDeviceRegistry_RegisterWithDriver(t *testing.T) {
	t.Run("register with driver and retrieve", func(t *testing.T) {
		reg := NewDeviceRegistry()
		factory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			return &mockFile{}, nil
		}
		driver := "test-driver"
		if err := reg.RegisterWithDriver("/dev/shell", factory, driver); err != nil {
			t.Fatalf("RegisterWithDriver failed: %v", err)
		}

		got, ok := reg.GetDriver("/dev/shell")
		if !ok {
			t.Fatal("expected GetDriver to return true")
		}
		if got != "test-driver" {
			t.Fatalf("expected driver 'test-driver', got %v", got)
		}
	})

	t.Run("get driver returns false for unregistered path", func(t *testing.T) {
		reg := NewDeviceRegistry()
		_, ok := reg.GetDriver("/dev/nonexistent")
		if ok {
			t.Fatal("expected GetDriver to return false for unregistered path")
		}
	})
}

func TestDeviceRegistry_RangeDrivers(t *testing.T) {
	reg := NewDeviceRegistry()
	factory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
		return &mockFile{}, nil
	}
	_ = reg.RegisterWithDriver("/dev/shell", factory, "shell-driver")
	_ = reg.RegisterWithDriver("/dev/fs", factory, "fs-driver")

	paths := make(map[string]bool)
	reg.RangeDrivers(func(path string, driver any) bool {
		paths[path] = true
		return true
	})

	if !paths["/dev/shell"] {
		t.Fatal("expected /dev/shell in RangeDrivers")
	}
	if !paths["/dev/fs"] {
		t.Fatal("expected /dev/fs in RangeDrivers")
	}
}

func TestDeviceRegistry_UnregisterCleansDriverMap(t *testing.T) {
	reg := NewDeviceRegistry()
	factory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
		return &mockFile{}, nil
	}
	_ = reg.RegisterWithDriver("/dev/test", factory, "driver")

	if _, ok := reg.GetDriver("/dev/test"); !ok {
		t.Fatal("expected driver before unregister")
	}

	if err := reg.Unregister("/dev/test"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	if _, ok := reg.GetDriver("/dev/test"); ok {
		t.Fatal("expected driver to be cleaned after unregister")
	}
}

// mockFile implements VFSFile for testing.
type mockFile struct {
	readData  []byte
	readErr   error
	writeData []byte
	writeErr  error
	closed    bool
	closeErr  error
	stat      FileStat
	statErr   error
}

func (m *mockFile) Read(length int) ([]byte, error)            { return m.readData, m.readErr }
func (m *mockFile) Write(_ context.Context, data []byte) error { m.writeData = data; return m.writeErr }
func (m *mockFile) Close() error                               { m.closed = true; return m.closeErr }
func (m *mockFile) Stat() (FileStat, error)                    { return m.stat, m.statErr }

func TestToolDef_JSONRoundTrip(t *testing.T) {
	original := ToolDef{
		Name:        "shell",
		Description: "Execute a shell command",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to execute",
				},
			},
			"required": []any{"command"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ToolDef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Name != original.Name {
		t.Fatalf("Name mismatch: got %q, want %q", decoded.Name, original.Name)
	}
	if decoded.Description != original.Description {
		t.Fatalf("Description mismatch: got %q, want %q", decoded.Description, original.Description)
	}
	if decoded.Parameters["type"] != "object" {
		t.Fatalf("Parameters.type mismatch: got %v", decoded.Parameters["type"])
	}
}
