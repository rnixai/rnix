package vfs

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestDeviceRegistry_Register(t *testing.T) {
	t.Run("register succeeds", func(t *testing.T) {
		reg := NewDeviceRegistry()
		factory := func(subpath string, flags OpenFlag) (VFSFile, error) {
			return &mockFile{}, nil
		}
		if err := reg.Register("/dev/llm/claude", factory); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	})

	t.Run("duplicate register returns error", func(t *testing.T) {
		reg := NewDeviceRegistry()
		factory := func(subpath string, flags OpenFlag) (VFSFile, error) {
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
		factory := func(subpath string, flags OpenFlag) (VFSFile, error) {
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

		file, err := reg.Open("/dev/llm/claude", O_RDWR)
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
		_, err := reg.Open("/dev/nonexistent", O_RDONLY)
		if err == nil {
			t.Fatal("expected error for unregistered path, got nil")
		}
	})
}

func TestDeviceRegistry_PrefixMatch(t *testing.T) {
	t.Run("prefix match passes subpath", func(t *testing.T) {
		reg := NewDeviceRegistry()
		var capturedSubpath string
		factory := func(subpath string, flags OpenFlag) (VFSFile, error) {
			capturedSubpath = subpath
			return &mockFile{}, nil
		}
		_ = reg.Register("/dev/fs", factory)

		file, err := reg.Open("/dev/fs/path/to/file", O_RDONLY)
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
		shortFactory := func(subpath string, flags OpenFlag) (VFSFile, error) {
			calledPath = "short"
			return &mockFile{}, nil
		}
		longFactory := func(subpath string, flags OpenFlag) (VFSFile, error) {
			calledPath = "long"
			return &mockFile{}, nil
		}
		_ = reg.Register("/dev", shortFactory)
		_ = reg.Register("/dev/fs", longFactory)

		_, err := reg.Open("/dev/fs/myfile", O_RDONLY)
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
		prefixFactory := func(subpath string, flags OpenFlag) (VFSFile, error) {
			calledPath = "prefix"
			return &mockFile{}, nil
		}
		exactFactory := func(subpath string, flags OpenFlag) (VFSFile, error) {
			calledPath = "exact"
			return &mockFile{}, nil
		}
		_ = reg.Register("/dev/fs", prefixFactory)
		_ = reg.Register("/dev/fs/special", exactFactory)

		_, err := reg.Open("/dev/fs/special", O_RDONLY)
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
		factory := func(subpath string, flags OpenFlag) (VFSFile, error) {
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
		factory := func(subpath string, flags OpenFlag) (VFSFile, error) {
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
			factory := func(subpath string, flags OpenFlag) (VFSFile, error) {
				return &mockFile{}, nil
			}
			_ = reg.Register(path, factory)
		}(i)
	}
	wg.Wait()
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
