package fs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/kernel"
	"github.com/gonewx/crux/vfs"
)

// testdataDir returns the absolute path to the testdata directory.
func testdataDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	return filepath.Join(filepath.Dir(filename), "testdata")
}

func TestFileFactory_ReadSuccess(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory(filepath.Join(dir, "sample.txt"), vfs.O_RDONLY)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	data, err := file.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	expected := "Hello, HostFS!\nThis is a test file for reading verification.\n"
	if string(data) != expected {
		t.Errorf("Read content mismatch:\ngot:  %q\nwant: %q", string(data), expected)
	}
}

func TestFileFactory_FileNotFound(t *testing.T) {
	factory := FileFactory()

	_, err := factory("/nonexistent/path/file.txt", vfs.O_RDONLY)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}

	var sysErr *kernel.SyscallError
	if !errors.As(err, &sysErr) {
		t.Fatalf("expected *kernel.SyscallError, got %T: %v", err, err)
	}
	if sysErr.Code != types.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %s", sysErr.Code)
	}
}

func TestFileFactory_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	// Create a file with no read permissions
	tmp := t.TempDir()
	path := filepath.Join(tmp, "noperm.txt")
	if err := os.WriteFile(path, []byte("secret"), 0o000); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer os.Chmod(path, 0o644) // cleanup

	factory := FileFactory()
	_, err := factory(path, vfs.O_RDONLY)
	if err == nil {
		t.Fatal("expected error for permission-denied file, got nil")
	}

	var sysErr *kernel.SyscallError
	if !errors.As(err, &sysErr) {
		t.Fatalf("expected *kernel.SyscallError, got %T: %v", err, err)
	}
	if sysErr.Code != types.ErrPermission {
		t.Errorf("expected ErrPermission, got %s", sysErr.Code)
	}
}

func TestHostFSFile_Stat(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory(filepath.Join(dir, "sample.txt"), vfs.O_RDONLY)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if stat.Name != "sample.txt" {
		t.Errorf("Stat Name = %q, want %q", stat.Name, "sample.txt")
	}
	if stat.Size <= 0 {
		t.Errorf("Stat Size = %d, want > 0", stat.Size)
	}
	if stat.IsDevice {
		t.Error("Stat IsDevice = true, want false")
	}
	if stat.DevicePath != "/dev/fs" {
		t.Errorf("Stat DevicePath = %q, want %q", stat.DevicePath, "/dev/fs")
	}
}

func TestHostFSFile_Write_ReadOnly(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory(filepath.Join(dir, "sample.txt"), vfs.O_RDONLY)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte("data"))
	if err == nil {
		t.Fatal("expected error on Write to read-only device, got nil")
	}

	var sysErr *kernel.SyscallError
	if !errors.As(err, &sysErr) {
		t.Fatalf("expected *kernel.SyscallError, got %T: %v", err, err)
	}
	if sysErr.Code != types.ErrPermission {
		t.Errorf("expected ErrPermission, got %s", sysErr.Code)
	}
}

func TestHostFSFile_Close_DoubleClose(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory(filepath.Join(dir, "sample.txt"), vfs.O_RDONLY)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	err = file.Close()
	if err == nil {
		t.Fatal("expected error on double Close, got nil")
	}
}

func TestHostFSFile_Read_AfterClose(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory(filepath.Join(dir, "sample.txt"), vfs.O_RDONLY)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	_, err = file.Read(0)
	if err == nil {
		t.Fatal("expected error on Read after Close, got nil")
	}
}

func TestFileFactory_EmptySubpath(t *testing.T) {
	factory := FileFactory()

	_, err := factory("", vfs.O_RDONLY)
	if err == nil {
		t.Fatal("expected error for empty subpath, got nil")
	}

	var sysErr *kernel.SyscallError
	if !errors.As(err, &sysErr) {
		t.Fatalf("expected *kernel.SyscallError, got %T: %v", err, err)
	}
	if sysErr.Code != types.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %s", sysErr.Code)
	}
}

func TestFileFactory_WriteFlag_Rejected(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	for _, flag := range []vfs.OpenFlag{vfs.O_WRONLY, vfs.O_RDWR} {
		_, err := factory(filepath.Join(dir, "sample.txt"), flag)
		if err == nil {
			t.Fatalf("expected error for flag %d, got nil", flag)
		}

		var sysErr *kernel.SyscallError
		if !errors.As(err, &sysErr) {
			t.Fatalf("expected *kernel.SyscallError for flag %d, got %T: %v", flag, err, err)
		}
		if sysErr.Code != types.ErrPermission {
			t.Errorf("flag %d: expected ErrPermission, got %s", flag, sysErr.Code)
		}
	}
}

func TestFileFactory_NestedPath(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory(filepath.Join(dir, "nested", "deep.txt"), vfs.O_RDONLY)
	if err != nil {
		t.Fatalf("Open nested file failed: %v", err)
	}
	defer file.Close()

	data, err := file.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	expected := "nested content\n"
	if string(data) != expected {
		t.Errorf("Read content mismatch:\ngot:  %q\nwant: %q", string(data), expected)
	}
}
