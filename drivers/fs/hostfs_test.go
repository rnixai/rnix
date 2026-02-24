package fs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gonewx/crux/internal/types"
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

	var sysErr *types.DriverError
	if !errors.As(err, &sysErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
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

	var sysErr2 *types.DriverError
	if !errors.As(err, &sysErr2) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if sysErr2.Code != types.ErrPermission {
		t.Errorf("expected ErrPermission, got %s", sysErr2.Code)
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

	var sysErr3 *types.DriverError
	if !errors.As(err, &sysErr3) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if sysErr3.Code != types.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %s", sysErr3.Code)
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

		var sysErr4 *types.DriverError
		if !errors.As(err, &sysErr4) {
			t.Fatalf("expected *types.DriverError for flag %d, got %T: %v", flag, err, err)
		}
		if sysErr4.Code != types.ErrPermission {
			t.Errorf("flag %d: expected ErrPermission, got %s", flag, sysErr4.Code)
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

func TestFileFactory_DirectoryRejected(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	_, err := factory(dir, vfs.O_RDONLY)
	if err == nil {
		t.Fatal("expected error when opening a directory, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrPermission {
		t.Errorf("expected ErrPermission, got %s", drvErr.Code)
	}
}

func TestHostFSFile_Read_PartialLength(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory(filepath.Join(dir, "sample.txt"), vfs.O_RDONLY)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	// Read only first 5 bytes
	data, err := file.Read(5)
	if err != nil {
		t.Fatalf("Read(5) failed: %v", err)
	}
	if len(data) != 5 {
		t.Errorf("Read(5) returned %d bytes, want 5", len(data))
	}
	if string(data) != "Hello" {
		t.Errorf("Read(5) = %q, want %q", string(data), "Hello")
	}

	// Read next chunk
	data2, err := file.Read(2)
	if err != nil {
		t.Fatalf("Read(2) failed: %v", err)
	}
	if string(data2) != ", " {
		t.Errorf("Read(2) = %q, want %q", string(data2), ", ")
	}
}
