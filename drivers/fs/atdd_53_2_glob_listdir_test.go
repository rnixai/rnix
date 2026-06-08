package fs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ATDD 红灯脚手架 — Story 53.2 / AC4：移除 list_dir，目录枚举能力并入 Glob。
//
// 生成模式: AI generation (backend, Go 标准 testing)。
// TDD 阶段: RED —— 全部以 t.Skip() 标记为未激活脚手架；dev-story 阶段逐个
// 移除 t.Skip 行以"激活"，验证 RED→GREEN（参照仓库 ATDD 生命周期：
// "add scaffolding" 提交 → "activate" 提交）。
//
// 本文件覆盖 AC4 在 **能力拥有层 drivers/fs** 的契约（测试 ID 53.2-UNIT-001..005）：
//   - GUARD（激活后即绿，capability 安全网）: UNIT-001 / UNIT-002
//     —— 锁定 "Glob(pattern=\"*\") 已是 list_dir 超集"，兑现 fold-not-delete：
//        必须先有 Glob 列目录覆盖，才能安全删除 list_dir 测试（story AC4 task 2.8）。
//   - RED（激活后在 list_dir 移除前 FAILS、移除后 PASS，驱动实现）:
//        UNIT-003 / UNIT-004 / UNIT-005。
//
// 注：globResult 是 execGlob 内部局部类型（hostfs.go:542），测试包无法引用，
// 故下方自定义 glob 解码结构体匹配其 json tag（matches / path / is_dir）。

// globEntryAT 解码单条 glob 匹配项（对齐 hostfs.go globEntry 的 json tag）。
type globEntryAT struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

// globResultAT 解码 execGlob 输出（对齐内部 globResult 的 json tag）。
type globResultAT struct {
	Matches []globEntryAT `json:"matches"`
	Notice  string        `json:"notice"`
}

// runGlob 是测试内联 helper：在 workDir 上以命令模式跑一次 glob，返回解码结果。
// （仅做数据提取，不含断言——断言保留在各测试体内，遵循 test-quality DoD。）
func runGlob(t *testing.T, workDir, pattern string) globResultAT {
	t.Helper()
	factory := FileFactory()
	file, err := factory("/", vfs.O_RDWR, workDir)
	if err != nil {
		t.Fatalf("Open command-mode failed: %v", err)
	}
	defer file.Close()

	payload, _ := json.Marshal(map[string]string{"op": "glob", "pattern": pattern})
	if err := file.Write(context.Background(), payload); err != nil {
		t.Fatalf("Glob write failed: %v", err)
	}
	raw, err := file.Read(0)
	if err != nil {
		t.Fatalf("Glob read failed: %v", err)
	}
	var out globResultAT
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal glob result failed: %v (raw=%s)", err, raw)
	}
	return out
}

// 53.2-UNIT-001 [GUARD] Glob(pattern="*") 返回目录的直接子项（文件+子目录混合），
// 且不递归进子目录 —— 即 list_dir 的功能超集。
//
// 性质: GUARD（激活后即绿；Glob 代码不变，本测试锁定其等价 list_dir 的语义，
// 是 AC4 "先加 Glob 覆盖再删 list_dir 测试" 的安全网）。
func TestATDD_53_2_UNIT_001_GlobListsDirectoryDirectChildren(t *testing.T) {
	t.Skip("ATDD RED-PHASE 脚手架 (Story 53.2 / AC4)：dev-story 移除本行激活。性质 GUARD —— 激活后即绿，锁定 Glob 列目录能力。")

	dir := t.TempDir()
	// 直接子项：一个文件 + 一个子目录；子目录内再放一个文件以验证不递归。
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("setup a.txt: %v", err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("setup sub/: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("deep"), 0o644); err != nil {
		t.Fatalf("setup sub/inner.txt: %v", err)
	}

	out := runGlob(t, dir, "*")

	byPath := make(map[string]globEntryAT)
	for _, e := range out.Matches {
		byPath[e.Path] = e
	}

	// 直接子项 a.txt 存在且非目录。
	if e, ok := byPath["a.txt"]; !ok {
		t.Errorf("expected direct child %q in glob result; got %v", "a.txt", out.Matches)
	} else if e.IsDir {
		t.Errorf("a.txt should not be a directory")
	}
	// 直接子项 sub 存在且为目录。
	if e, ok := byPath["sub"]; !ok {
		t.Errorf("expected direct child dir %q in glob result; got %v", "sub", out.Matches)
	} else if !e.IsDir {
		t.Errorf("sub should be a directory (is_dir=true)")
	}
	// 关键：pattern="*" 不跨 "/"，深层项 sub/inner.txt 不应出现（证明等价 list_dir 直接子项语义）。
	if _, ok := byPath[filepath.Join("sub", "inner.txt")]; ok {
		t.Errorf("Glob(\"*\") must NOT recurse: %q should be absent; got %v", filepath.Join("sub", "inner.txt"), out.Matches)
	}
}

// 53.2-UNIT-002 [GUARD] Glob(pattern="*") 在空目录上返回零个匹配。
//
// 性质: GUARD（边界；空目录 matches 为 nil slice → JSON {"matches":null} →
// 解码后 len==0，故断言 len 而非字面串）。
func TestATDD_53_2_UNIT_002_GlobListsEmptyDirectory(t *testing.T) {
	t.Skip("ATDD RED-PHASE 脚手架 (Story 53.2 / AC4)：dev-story 移除本行激活。性质 GUARD（空目录边界）。")

	dir := t.TempDir() // 空目录
	out := runGlob(t, dir, "*")
	if len(out.Matches) != 0 {
		t.Errorf("expected 0 matches for empty dir, got %d: %v", len(out.Matches), out.Matches)
	}
}

// 53.2-UNIT-003 [RED] /dev/fs 的 ToolDefs 不再包含 list_dir —— 工具集降为
// {Read, Write, Edit, Glob, Grep} 五个。
//
// 性质: RED（impl 前 6 个含 list_dir → FAILS；移除 list_dir ToolDef 后 5 个 → PASS）。
// 对应 story AC4：删 hostfs.go:72-88 list_dir ToolDef 块。
func TestATDD_53_2_UNIT_003_ToolDefsExcludeListDir(t *testing.T) {
	t.Skip("ATDD RED-PHASE 脚手架 (Story 53.2 / AC4)：dev-story 移除本行激活。性质 RED —— 移除 list_dir ToolDef 前失败。")

	defs := NewDriver().ToolDefs()

	want := map[string]bool{"Read": false, "Write": false, "Edit": false, "Glob": false, "Grep": false}
	for _, def := range defs {
		if def.Name == "list_dir" {
			t.Errorf("list_dir ToolDef 应已移除（能力并入 Glob），但仍存在于 /dev/fs 工具集")
		}
		if _, ok := want[def.Name]; ok {
			want[def.Name] = true
		}
	}
	if len(defs) != 5 {
		t.Errorf("/dev/fs 工具集应为 5 个 (Read/Write/Edit/Glob/Grep)，got %d: %v", len(defs), toolNames(defs))
	}
	for name, found := range want {
		if !found {
			t.Errorf("缺少预期工具 %q", name)
		}
	}
}

// toolNames 提取 def 名称切片，便于失败信息可读（数据提取 helper，无断言）。
func toolNames(defs []vfs.ToolDef) []string {
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	return names
}

// 53.2-UNIT-004 [RED] 打开目录读取时的错误文案指向 Glob（含 "*"），不再提 list_dir。
//
// 性质: RED（impl 前文案 "is a directory, use list_dir to enumerate" → 含 list_dir、
// 不含 Glob → FAILS；改为 "use Glob with pattern \"*\"" 后 → PASS）。
// 对应 story AC4：hostfs.go:988 错误文案更新。
func TestATDD_53_2_UNIT_004_DirectoryErrorGuidesToGlob(t *testing.T) {
	t.Skip("ATDD RED-PHASE 脚手架 (Story 53.2 / AC4)：dev-story 移除本行激活。性质 RED —— 错误文案改指 Glob 前失败。")

	dir := t.TempDir() // 目录
	factory := FileFactory()
	// "/"+dir = "//<abs>" —— VFS 双斜线转义为显式宿主绝对路径（同 TestFileFactory_DirectoryRejected）。
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
	msg := drvErr.Error()
	if !strings.Contains(msg, "Glob") {
		t.Errorf("目录错误文案应引导使用 Glob，got: %s", msg)
	}
	if strings.Contains(msg, "list_dir") {
		t.Errorf("目录错误文案不应再提 list_dir（已移除），got: %s", msg)
	}
}

// 53.2-UNIT-005 [RED] {"op":"list"} 命令已被移除 —— 应作为未知操作被拒（ErrDriver）。
//
// 性质: RED（impl 前 list op 合法、列目录无错 → "expect error" FAILS；
// 删 hostfs.go:310-311 dispatch case 后 list 落入 default 分支 → 返回 ErrDriver → PASS）。
// 这是 list_dir 能力"确已移除"的行为级证据（不只是 ToolDef 不可见）。
func TestATDD_53_2_UNIT_005_ListOperationRejected(t *testing.T) {
	t.Skip("ATDD RED-PHASE 脚手架 (Story 53.2 / AC4)：dev-story 移除本行激活。性质 RED —— 删除 list dispatch 前失败。")

	dir := t.TempDir()
	factory := FileFactory()
	file, err := factory("/", vfs.O_RDWR, dir)
	if err != nil {
		t.Fatalf("Open command-mode failed: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte(`{"op": "list"}`))
	if err == nil {
		t.Fatal("expected {\"op\":\"list\"} to be rejected as unknown operation after list_dir removal, got nil")
	}
	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrDriver {
		t.Errorf("expected ErrDriver for removed 'list' op, got %s", drvErr.Code)
	}
}
