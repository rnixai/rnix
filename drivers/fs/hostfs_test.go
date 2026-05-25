package fs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
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

	file, err := factory("/"+filepath.Join(dir, "sample.txt"), vfs.O_RDONLY, "")
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

	_, err := factory("/nonexistent/path/file.txt", vfs.O_RDONLY, "")
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
	_, err := factory("/"+path, vfs.O_RDONLY, "")
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

	file, err := factory("/"+filepath.Join(dir, "sample.txt"), vfs.O_RDONLY, "")
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

func TestHostFSFile_Write_ReadOnlyMode(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory("/"+filepath.Join(dir, "sample.txt"), vfs.O_RDONLY, "")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte("data"))
	if err == nil {
		t.Fatal("expected error on Write to read-only mode file, got nil")
	}
}

func TestHostFSFile_Close_DoubleClose(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory("/"+filepath.Join(dir, "sample.txt"), vfs.O_RDONLY, "")
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

	file, err := factory("/"+filepath.Join(dir, "sample.txt"), vfs.O_RDONLY, "")
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

	_, err := factory("", vfs.O_RDONLY, "")
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

func TestFileFactory_WriteFlagAccepted(t *testing.T) {
	tmp := t.TempDir()
	factory := FileFactory()

	// O_WRONLY and O_RDWR should now succeed (command mode).
	for _, flag := range []vfs.OpenFlag{vfs.O_WRONLY, vfs.O_RDWR} {
		file, err := factory("/test.txt", flag, tmp)
		if err != nil {
			t.Fatalf("expected Open with flag %d to succeed, got: %v", flag, err)
		}
		file.Close()
	}
}

func TestFileFactory_NestedPath(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory("/"+filepath.Join(dir, "nested", "deep.txt"), vfs.O_RDONLY, "")
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

	// "/"+dir = "//<abs>" — VFS double-slash escape for explicit host-absolute path.
	_, err := factory("/"+dir, vfs.O_RDONLY, "")
	if err == nil {
		t.Fatal("expected error when opening a directory, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrIsDirectory {
		t.Errorf("expected ErrIsDirectory, got %s", drvErr.Code)
	}
	if !strings.Contains(drvErr.Error(), "use list_dir") {
		t.Errorf("expected error message to guide toward list_dir, got: %s", drvErr.Error())
	}
}

func TestHostFSFile_Read_PartialLength(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory("/"+filepath.Join(dir, "sample.txt"), vfs.O_RDONLY, "")
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

func TestHostFSFile_Write_AfterClose(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory("/"+filepath.Join(dir, "sample.txt"), vfs.O_RDONLY, "")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	err = file.Write(context.Background(), []byte("data"))
	if err == nil {
		t.Fatal("expected error on Write after Close, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "closed") {
		t.Errorf("expected closed error, got: %s", got)
	}
}

func TestHostFSFile_Write_ReturnsDriverError(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	// Read-mode file should reject writes with DriverError.
	file, err := factory("/"+filepath.Join(dir, "sample.txt"), vfs.O_RDONLY, "")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte("data"))
	if err == nil {
		t.Fatal("expected error on Write to read-only mode, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrPermission {
		t.Errorf("expected ErrPermission, got %s", drvErr.Code)
	}
}

func TestHostFSFile_Stat_AfterClose(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory("/"+filepath.Join(dir, "sample.txt"), vfs.O_RDONLY, "")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	_, err = file.Stat()
	if err == nil {
		t.Fatal("expected error on Stat after Close, got nil")
	}
}

func TestFileFactory_WorkDir(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	t.Run("relative path resolved with workDir", func(t *testing.T) {
		// subpath from DeviceRegistry always starts with "/" (e.g., "/sample.txt")
		file, err := factory("/sample.txt", vfs.O_RDONLY, dir)
		if err != nil {
			t.Fatalf("Open with workDir failed: %v", err)
		}
		defer file.Close()

		data, err := file.Read(0)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if !strings.Contains(string(data), "HostFS") {
			t.Errorf("unexpected content: %q", data)
		}
	})

	t.Run("absolute path ignores workDir (double-slash escape)", func(t *testing.T) {
		// Double-slash convention: //absolute/path → TrimPrefix "/" → /absolute/path → IsAbs → penetrates
		absPath := filepath.Join(dir, "sample.txt")
		// Construct double-slash subpath: "/" + absPath
		subpath := "/" + absPath

		file, err := factory(subpath, vfs.O_RDONLY, "/nonexistent/workdir")
		if err != nil {
			t.Fatalf("Open absolute path failed: %v", err)
		}
		defer file.Close()

		data, err := file.Read(0)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if !strings.Contains(string(data), "HostFS") {
			t.Errorf("unexpected content: %q", data)
		}
	})

	t.Run("empty workDir backwards compatible", func(t *testing.T) {
		// With empty workDir, subpath is used as-is (original behavior)
		absPath := filepath.Join(dir, "sample.txt")
		file, err := factory("/"+absPath, vfs.O_RDONLY, "")
		if err != nil {
			t.Fatalf("Open with empty workDir failed: %v", err)
		}
		defer file.Close()

		data, err := file.Read(0)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if !strings.Contains(string(data), "HostFS") {
			t.Errorf("unexpected content: %q", data)
		}
	})

	t.Run("nested relative path with workDir", func(t *testing.T) {
		file, err := factory("/nested/deep.txt", vfs.O_RDONLY, dir)
		if err != nil {
			t.Fatalf("Open nested with workDir failed: %v", err)
		}
		defer file.Close()

		data, err := file.Read(0)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if len(data) == 0 {
			t.Error("expected non-empty content")
		}
	})
}

// --- New tests for write and list support ---

func TestHostFSFile_WriteFile_Success(t *testing.T) {
	tmp := t.TempDir()
	factory := FileFactory()

	file, err := factory("/output.txt", vfs.O_RDWR, tmp)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte(`{"content": "hello world"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	result, err := file.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(result) != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}

	// Verify file was actually created on disk.
	content, err := os.ReadFile(filepath.Join(tmp, "output.txt"))
	if err != nil {
		t.Fatalf("os.ReadFile failed: %v", err)
	}
	if string(content) != "hello world" {
		t.Errorf("file content = %q, want %q", content, "hello world")
	}
}

func TestHostFSFile_WriteFile_AutoMkdir(t *testing.T) {
	tmp := t.TempDir()
	factory := FileFactory()

	file, err := factory("/deep/nested/dir/file.txt", vfs.O_RDWR, tmp)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte(`{"content": "nested content"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify nested directory and file were created.
	content, err := os.ReadFile(filepath.Join(tmp, "deep", "nested", "dir", "file.txt"))
	if err != nil {
		t.Fatalf("os.ReadFile failed: %v", err)
	}
	if string(content) != "nested content" {
		t.Errorf("file content = %q, want %q", content, "nested content")
	}
}

func TestHostFSFile_ListDir_Success(t *testing.T) {
	dir := testdataDir(t)
	factory := FileFactory()

	file, err := factory("/", vfs.O_RDWR, dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte(`{"op": "list"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	result, err := file.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	var listResult listDirResult
	if err := json.Unmarshal(result, &listResult); err != nil {
		t.Fatalf("unmarshal list result failed: %v", err)
	}

	// testdata should contain at least sample.txt and nested/
	names := make(map[string]bool)
	for _, e := range listResult.Entries {
		names[e.Name] = true
	}
	if !names["sample.txt"] {
		t.Error("expected sample.txt in listing")
	}
	if !names["nested"] {
		t.Error("expected nested/ in listing")
	}
}

func TestHostFSFile_ListDir_Empty(t *testing.T) {
	tmp := t.TempDir()
	factory := FileFactory()

	file, err := factory("/", vfs.O_RDWR, tmp)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte(`{"op": "list"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	result, err := file.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	expected := `{"entries":[]}`
	if string(result) != expected {
		t.Errorf("expected empty list %q, got %q", expected, result)
	}
}

func TestHostFSFile_ListDir_NotFound(t *testing.T) {
	tmp := t.TempDir()
	factory := FileFactory()

	file, err := factory("/nonexistent", vfs.O_RDWR, tmp)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte(`{"op": "list"}`))
	if err == nil {
		t.Fatal("expected error listing nonexistent dir, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %s", drvErr.Code)
	}
}

func TestHostFSFile_Sandbox_PathEscape(t *testing.T) {
	tmp := t.TempDir()
	factory := FileFactory()

	// Attempt to write outside workDir via path traversal.
	file, err := factory("/../../etc/passwd", vfs.O_RDWR, tmp)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte(`{"content": "hacked"}`))
	if err == nil {
		t.Fatal("expected sandbox error, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrPermission {
		t.Errorf("expected ErrPermission, got %s", drvErr.Code)
	}
}

func TestHostFSFile_CommandMode_ReadWithoutWrite(t *testing.T) {
	tmp := t.TempDir()
	factory := FileFactory()

	file, err := factory("/test.txt", vfs.O_RDWR, tmp)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	_, err = file.Read(0)
	if err == nil {
		t.Fatal("expected error on Read without Write, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrDriver {
		t.Errorf("expected ErrDriver, got %s", drvErr.Code)
	}
}

func TestHostFSFile_WriteFile_EmptyContent(t *testing.T) {
	tmp := t.TempDir()
	factory := FileFactory()

	file, err := factory("/empty.txt", vfs.O_RDWR, tmp)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	// Writing empty content should create an empty file.
	err = file.Write(context.Background(), []byte(`{"content": ""}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmp, "empty.txt"))
	if err != nil {
		t.Fatalf("os.ReadFile failed: %v", err)
	}
	if len(content) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(content))
	}
}

func TestHostFSFile_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	factory := FileFactory()

	file, err := factory("/test.txt", vfs.O_RDWR, tmp)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestHostFSFile_Sandbox_NoWorkDir(t *testing.T) {
	tmp := t.TempDir()
	factory := FileFactory()

	// Without workDir, no sandbox check — path used as-is.
	target := filepath.Join(tmp, "output.txt")
	file, err := factory("/"+target, vfs.O_RDWR, "")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte(`{"content": "no sandbox"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("os.ReadFile failed: %v", err)
	}
	if string(content) != "no sandbox" {
		t.Errorf("expected 'no sandbox', got %q", content)
	}
}

func TestHostFSDriver_ToolDefs(t *testing.T) {
	d := NewDriver()
	defs := d.ToolDefs()

	if len(defs) != 6 {
		t.Fatalf("expected 6 tool defs, got %d", len(defs))
	}

	expected := map[string]bool{"Read": false, "Write": false, "list_dir": false, "Edit": false, "Glob": false, "Grep": false}
	for _, def := range defs {
		if _, ok := expected[def.Name]; !ok {
			t.Fatalf("unexpected tool name: %q", def.Name)
		}
		expected[def.Name] = true
		if def.Description == "" {
			t.Fatalf("expected non-empty description for %q", def.Name)
		}
		if def.Parameters["type"] != "object" {
			t.Fatalf("expected parameters type 'object' for %q, got %v", def.Name, def.Parameters["type"])
		}
	}

	for name, found := range expected {
		if !found {
			t.Fatalf("missing tool def: %q", name)
		}
	}
}

func TestResolvePath(t *testing.T) {
	cases := []struct {
		name    string
		subpath string
		workDir string
		want    string
	}{
		{"relative subpath under workDir", "/.rnix/skills/x", "/proj/echo", "/proj/echo/.rnix/skills/x"},
		{"simple relative under workDir", "/src/main.go", "/proj/echo", "/proj/echo/src/main.go"},
		{"root subpath with workDir", "/", "/proj/echo", "/proj/echo"},
		{"empty subpath with workDir", "", "/proj/echo", "/proj/echo"},
		{"relative subpath without workDir does not become absolute", "/.rnix/skills/x", "", ".rnix/skills/x"},
		{"root subpath without workDir falls back to CWD marker", "/", "", "."},
		{"empty subpath without workDir falls back to CWD marker", "", "", "."},
		{"absolute-looking input (double-slash escape) penetrates workDir", "//etc/passwd", "/proj/echo", "/etc/passwd"},
		{"absolute-looking input without workDir still penetrates", "//etc/passwd", "", "/etc/passwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolvePath(tc.subpath, tc.workDir)
			if got != tc.want {
				t.Fatalf("resolvePath(%q, %q) = %q, want %q", tc.subpath, tc.workDir, got, tc.want)
			}
		})
	}
}

func TestFileFactory_Compat(t *testing.T) {
	// Package-level FileFactory should still work
	factory := FileFactory()
	if factory == nil {
		t.Fatal("FileFactory() returned nil")
	}

	// Also verify NewDriver().FileFactory() works
	d := NewDriver()
	dfactory := d.FileFactory()
	if dfactory == nil {
		t.Fatal("NewDriver().FileFactory() returned nil")
	}
}
