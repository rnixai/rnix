package vfs

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/gonewx/crux/internal/types"
)

func TestVFS_Open(t *testing.T) {
	t.Run("returns incremental FDs starting from 3", func(t *testing.T) {
		reg := NewDeviceRegistry()
		_ = reg.Register("/dev/test", func(subpath string, flags OpenFlag) (VFSFile, error) {
			return &mockFile{}, nil
		})
		v := NewVFS(reg)
		pid := types.PID(1)

		fd1, err := v.Open(pid, "/dev/test", O_RDWR)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		if fd1 != 3 {
			t.Fatalf("expected FD 3, got %d", fd1)
		}

		fd2, err := v.Open(pid, "/dev/test", O_RDWR)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		if fd2 != 4 {
			t.Fatalf("expected FD 4, got %d", fd2)
		}

		fd3, err := v.Open(pid, "/dev/test", O_RDWR)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		if fd3 != 5 {
			t.Fatalf("expected FD 5, got %d", fd3)
		}
	})

	t.Run("unregistered path returns VFSError with ErrNotFound", func(t *testing.T) {
		reg := NewDeviceRegistry()
		v := NewVFS(reg)

		_, err := v.Open(types.PID(1), "/dev/nonexistent", O_RDONLY)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var vfsErr *VFSError
		if !errors.As(err, &vfsErr) {
			t.Fatalf("expected *VFSError, got %T", err)
		}
		if vfsErr.Code != types.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %s", vfsErr.Code)
		}
		if vfsErr.Op != "Open" {
			t.Fatalf("expected Op 'Open', got %q", vfsErr.Op)
		}
	})
}

func TestVFS_ReadWrite(t *testing.T) {
	t.Run("Read delegates to VFSFile", func(t *testing.T) {
		reg := NewDeviceRegistry()
		mf := &mockFile{readData: []byte("hello")}
		_ = reg.Register("/dev/test", func(subpath string, flags OpenFlag) (VFSFile, error) {
			return mf, nil
		})
		v := NewVFS(reg)
		pid := types.PID(1)

		fd, _ := v.Open(pid, "/dev/test", O_RDONLY)
		data, err := v.Read(pid, fd, 5)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if string(data) != "hello" {
			t.Fatalf("expected 'hello', got %q", string(data))
		}
	})

	t.Run("Write delegates to VFSFile", func(t *testing.T) {
		reg := NewDeviceRegistry()
		mf := &mockFile{}
		_ = reg.Register("/dev/test", func(subpath string, flags OpenFlag) (VFSFile, error) {
			return mf, nil
		})
		v := NewVFS(reg)
		pid := types.PID(1)

		fd, _ := v.Open(pid, "/dev/test", O_WRONLY)
		err := v.Write(pid, fd, []byte("world"))
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		if string(mf.writeData) != "world" {
			t.Fatalf("expected 'world', got %q", string(mf.writeData))
		}
	})
}

func TestVFS_Close(t *testing.T) {
	t.Run("Close removes FD from table", func(t *testing.T) {
		reg := NewDeviceRegistry()
		mf := &mockFile{}
		_ = reg.Register("/dev/test", func(subpath string, flags OpenFlag) (VFSFile, error) {
			return mf, nil
		})
		v := NewVFS(reg)
		pid := types.PID(1)

		fd, _ := v.Open(pid, "/dev/test", O_RDWR)
		if err := v.Close(pid, fd); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
		if !mf.closed {
			t.Fatal("expected VFSFile.Close to be called")
		}

		// Read after Close should fail
		_, err := v.Read(pid, fd, 1)
		if err == nil {
			t.Fatal("expected error reading closed FD")
		}

		// Write after Close should fail
		err = v.Write(pid, fd, []byte("x"))
		if err == nil {
			t.Fatal("expected error writing closed FD")
		}
	})

	t.Run("Close already closed FD returns error", func(t *testing.T) {
		reg := NewDeviceRegistry()
		_ = reg.Register("/dev/test", func(subpath string, flags OpenFlag) (VFSFile, error) {
			return &mockFile{}, nil
		})
		v := NewVFS(reg)
		pid := types.PID(1)

		fd, _ := v.Open(pid, "/dev/test", O_RDWR)
		_ = v.Close(pid, fd)

		err := v.Close(pid, fd)
		if err == nil {
			t.Fatal("expected error closing already-closed FD")
		}
	})
}

func TestVFS_InvalidFD(t *testing.T) {
	reg := NewDeviceRegistry()
	v := NewVFS(reg)
	pid := types.PID(1)

	t.Run("Read invalid FD", func(t *testing.T) {
		_, err := v.Read(pid, 99, 1)
		if err == nil {
			t.Fatal("expected error")
		}
		var vfsErr *VFSError
		if !errors.As(err, &vfsErr) {
			t.Fatalf("expected *VFSError, got %T", err)
		}
		if vfsErr.Code != types.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %s", vfsErr.Code)
		}
	})

	t.Run("Write invalid FD", func(t *testing.T) {
		err := v.Write(pid, 99, []byte("x"))
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("Close invalid FD", func(t *testing.T) {
		err := v.Close(pid, 99)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestVFS_MultiProcess(t *testing.T) {
	reg := NewDeviceRegistry()
	_ = reg.Register("/dev/test", func(subpath string, flags OpenFlag) (VFSFile, error) {
		return &mockFile{readData: []byte("data")}, nil
	})
	v := NewVFS(reg)

	pid1 := types.PID(1)
	pid2 := types.PID(2)

	// Both PIDs get FD 3 (independent counters)
	fd1, _ := v.Open(pid1, "/dev/test", O_RDWR)
	fd2, _ := v.Open(pid2, "/dev/test", O_RDWR)

	if fd1 != 3 {
		t.Fatalf("pid1: expected FD 3, got %d", fd1)
	}
	if fd2 != 3 {
		t.Fatalf("pid2: expected FD 3, got %d", fd2)
	}

	// Close PID1's FD should not affect PID2
	_ = v.Close(pid1, fd1)

	data, err := v.Read(pid2, fd2, 4)
	if err != nil {
		t.Fatalf("pid2 Read after pid1 Close should work: %v", err)
	}
	if string(data) != "data" {
		t.Fatalf("expected 'data', got %q", string(data))
	}
}

func TestVFS_Stat(t *testing.T) {
	reg := NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags OpenFlag) (VFSFile, error) {
		return &mockFile{}, nil
	})
	v := NewVFS(reg)

	t.Run("stat registered device", func(t *testing.T) {
		stat, err := v.Stat("/dev/llm/claude")
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if !stat.IsDevice {
			t.Fatal("expected IsDevice=true")
		}
	})

	t.Run("stat unregistered path", func(t *testing.T) {
		_, err := v.Stat("/dev/nonexistent")
		if err == nil {
			t.Fatal("expected error")
		}
		var vfsErr *VFSError
		if !errors.As(err, &vfsErr) {
			t.Fatalf("expected *VFSError, got %T", err)
		}
		if vfsErr.Code != types.ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %s", vfsErr.Code)
		}
	})
}

func TestVFS_CloseAll(t *testing.T) {
	reg := NewDeviceRegistry()
	files := make([]*mockFile, 3)
	callCount := 0
	_ = reg.Register("/dev/test", func(subpath string, flags OpenFlag) (VFSFile, error) {
		mf := &mockFile{}
		files[callCount] = mf
		callCount++
		return mf, nil
	})
	v := NewVFS(reg)
	pid := types.PID(1)

	_, _ = v.Open(pid, "/dev/test", O_RDWR)
	_, _ = v.Open(pid, "/dev/test", O_RDWR)
	_, _ = v.Open(pid, "/dev/test", O_RDWR)

	if err := v.CloseAll(pid); err != nil {
		t.Fatalf("CloseAll failed: %v", err)
	}

	for i, mf := range files {
		if !mf.closed {
			t.Fatalf("file %d not closed", i)
		}
	}

	// After CloseAll, any FD access should fail
	_, err := v.Read(pid, 3, 1)
	if err == nil {
		t.Fatal("expected error after CloseAll")
	}
}

func TestVFS_ConcurrentAccess(t *testing.T) {
	reg := NewDeviceRegistry()
	_ = reg.Register("/dev/test", func(subpath string, flags OpenFlag) (VFSFile, error) {
		return &mockFile{readData: []byte("ok")}, nil
	})
	v := NewVFS(reg)

	const goroutines = 50
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pid := types.PID(uint64(i + 1))

			fd, err := v.Open(pid, "/dev/test", O_RDWR)
			if err != nil {
				t.Errorf("goroutine %d: Open failed: %v", i, err)
				return
			}

			_, err = v.Read(pid, fd, 2)
			if err != nil {
				t.Errorf("goroutine %d: Read failed: %v", i, err)
				return
			}

			err = v.Write(pid, fd, []byte("data"))
			if err != nil {
				t.Errorf("goroutine %d: Write failed: %v", i, err)
				return
			}

			err = v.Close(pid, fd)
			if err != nil {
				t.Errorf("goroutine %d: Close failed: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestVFS_ConcurrentSameProcess(t *testing.T) {
	reg := NewDeviceRegistry()
	_ = reg.Register("/dev/test", func(subpath string, flags OpenFlag) (VFSFile, error) {
		return &mockFile{readData: []byte("ok")}, nil
	})
	v := NewVFS(reg)
	pid := types.PID(1)

	const goroutines = 50
	var wg sync.WaitGroup
	fds := make(chan types.FD, goroutines)

	// Concurrent Open on same PID
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fd, err := v.Open(pid, "/dev/test", O_RDWR)
			if err != nil {
				t.Errorf("Open failed: %v", err)
				return
			}
			fds <- fd
		}()
	}
	wg.Wait()
	close(fds)

	// Verify all FDs are unique
	seen := make(map[types.FD]bool)
	for fd := range fds {
		if seen[fd] {
			t.Fatalf("duplicate FD: %d", fd)
		}
		seen[fd] = true
	}
	if len(seen) != goroutines {
		t.Fatalf("expected %d unique FDs, got %d", goroutines, len(seen))
	}
}

func TestVFSError_Unwrap(t *testing.T) {
	underlying := fmt.Errorf("underlying")
	vfsErr := newVFSError("Open", 1, "/dev/test", underlying, types.ErrNotFound)

	if !errors.Is(vfsErr, underlying) {
		t.Fatal("errors.Is should find underlying error")
	}
}
