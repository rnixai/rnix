package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 47.2: SkillLoader lenient validation
//
// 覆盖 AC6 / AC7（loader 层）：
//   - AC6: name 不匹配父目录 / name 超 64 字符 → 不阻塞加载，
//          通过 LenientWarning 暴露（warn-but-load 语义）
//   - AC7: description 缺失 / YAML 完全不可解析 / name 缺失
//          → 返回 *LenientSkipError，调用方可 errors.As 判别
//
// 本文件引用尚不存在的：
//   * LenientSkipError 类型（在 skills 包）
//   * LoadMetadataWithDiag / LoadFullWithDiag 旁路方法
//   * 返回的 []LenientWarning（dev 决策方案 A）
//
// RED → GREEN: 完成 AC6+AC7 loader 改造后，本文件应编译且全部测试通过。
// ============================================================

// writeSKILLMD writes a SKILL.md under <searchDir>/<dirName>/SKILL.md with the
// given raw frontmatter+body content (no template substitution).
func writeSKILLMD(t *testing.T, searchDir, dirName, content string) string {
	t.Helper()
	dir := filepath.Join(searchDir, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	return path
}

// --- 47.2-UNIT-007: [P1] Name mismatch with parent dir → warn but load (AC6) ---

func TestLoader_Lenient_NameMismatch(t *testing.T) {
	root := t.TempDir()

	// Parent dir is "foo", but frontmatter declares name: bar
	writeSKILLMD(t, root, "foo", `---
name: bar
description: "valid skill but parent dir differs"
allowed-tools: /dev/fs
---

body
`)

	loader := NewSkillLoader([]string{root})

	// When: LoadMetadataWithDiag
	info, warnings, err := loader.LoadMetadataWithDiag("foo")

	// Then: success — skill IS loaded
	if err != nil {
		t.Fatalf("LoadMetadataWithDiag: name mismatch must warn-but-load, got err: %v", err)
	}
	if info == nil {
		t.Fatal("info is nil; name mismatch must still return SkillInfo")
	}
	if info.Manifest.Name != "bar" {
		t.Errorf("info.Manifest.Name = %q, want %q", info.Manifest.Name, "bar")
	}

	// And: a LenientWarning is surfaced
	if len(warnings) == 0 {
		t.Fatal("expected at least one LenientWarning for name mismatch")
	}
	var found bool
	for _, w := range warnings {
		if w.Field == "name" && strings.Contains(strings.ToLower(w.Reason), "mismatch") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected LenientWarning with Field=name and Reason mentioning 'mismatch', got %+v", warnings)
	}
}

// --- 47.2-UNIT-008: [P2] Name exceeds 64 chars → warn but load (AC6) ---

func TestLoader_Lenient_NameOverlong(t *testing.T) {
	root := t.TempDir()
	overlong := strings.Repeat("a", 65) // 65 > 64 → must warn

	writeSKILLMD(t, root, "long", fmt.Sprintf(`---
name: %s
description: "name field too long"
allowed-tools: /dev/fs
---

body
`, overlong))

	loader := NewSkillLoader([]string{root})

	info, warnings, err := loader.LoadMetadataWithDiag("long")
	if err != nil {
		t.Fatalf("LoadMetadataWithDiag: overlong name must warn-but-load, got err: %v", err)
	}
	if info == nil {
		t.Fatal("info is nil; overlong name must still return SkillInfo")
	}
	if info.Manifest.Name != overlong {
		t.Errorf("info.Manifest.Name not preserved as-is; got len=%d, want len=%d", len(info.Manifest.Name), len(overlong))
	}

	var found bool
	for _, w := range warnings {
		if w.Field == "name" && strings.Contains(w.Reason, "64") {
			found = true
			if !strings.Contains(w.Detail, "...") {
				t.Errorf("expected Detail to include truncated form, got %q", w.Detail)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected LenientWarning for name length > 64, got %+v", warnings)
	}
}

// --- 47.2-UNIT-009: [P0] Empty description → *LenientSkipError (AC7) ---

func TestLoader_Lenient_DescriptionEmpty(t *testing.T) {
	root := t.TempDir()
	path := writeSKILLMD(t, root, "no-desc", `---
name: no-desc
description: ""
allowed-tools: /dev/fs
---

body
`)

	loader := NewSkillLoader([]string{root})

	info, _, err := loader.LoadMetadataWithDiag("no-desc")
	if err == nil {
		t.Fatal("expected error for empty description")
	}
	if info != nil {
		t.Errorf("info should be nil for lenient-skip; got non-nil")
	}

	var lerr *LenientSkipError
	if !errors.As(err, &lerr) {
		t.Fatalf("expected *LenientSkipError, got %T: %v", err, err)
	}
	if !strings.Contains(lerr.Reason, "description") {
		t.Errorf("LenientSkipError.Reason = %q, want substring 'description'", lerr.Reason)
	}
	if !strings.HasPrefix(lerr.Path, root) {
		t.Errorf("LenientSkipError.Path = %q, want prefix %q", lerr.Path, root)
	}
	_ = path
}

// --- 47.2-UNIT-010: [P0] YAML unparsable → *LenientSkipError (AC7) ---

func TestLoader_Lenient_YAMLParseError(t *testing.T) {
	root := t.TempDir()
	writeSKILLMD(t, root, "bad-yaml", `---
name: : :
description: lol
this is not yaml
---

body
`)

	loader := NewSkillLoader([]string{root})

	_, _, err := loader.LoadMetadataWithDiag("bad-yaml")
	if err == nil {
		t.Fatal("expected error for unparsable YAML")
	}

	var lerr *LenientSkipError
	if !errors.As(err, &lerr) {
		t.Fatalf("expected *LenientSkipError, got %T: %v", err, err)
	}
	if !strings.Contains(strings.ToLower(lerr.Reason), "yaml") {
		t.Errorf("LenientSkipError.Reason = %q, want substring 'yaml'", lerr.Reason)
	}
}

// --- 47.2-UNIT-011: [P1] Missing name → *LenientSkipError (AC7 unified skip) ---

func TestLoader_Lenient_NameMissing(t *testing.T) {
	root := t.TempDir()
	writeSKILLMD(t, root, "no-name", `---
description: "has description but no name"
allowed-tools: /dev/fs
---

body
`)

	loader := NewSkillLoader([]string{root})

	_, _, err := loader.LoadMetadataWithDiag("no-name")
	if err == nil {
		t.Fatal("expected error for missing name")
	}

	var lerr *LenientSkipError
	if !errors.As(err, &lerr) {
		t.Fatalf("expected *LenientSkipError, got %T: %v", err, err)
	}
	if !strings.Contains(strings.ToLower(lerr.Reason), "name") {
		t.Errorf("LenientSkipError.Reason = %q, want substring 'name'", lerr.Reason)
	}
}

// --- 47.2-UNIT-012: [P1] LoadMetadata backward-compat path still returns plain error (AC6/AC7 contract) ---

// AC6 / AC7 / dev-notes §4 mandate that legacy LoadMetadata (without -WithDiag)
// keeps its 2-value return and that lenient-skip cases manifest there as a
// regular error (callers like Epic 2 still see "skill not loadable"). This
// guarantees zero breakage for Epic 2 callers while Installer migrates to the
// new -WithDiag entry point.
func TestLoader_Lenient_LegacyLoadMetadata_BackwardCompat(t *testing.T) {
	root := t.TempDir()
	writeSKILLMD(t, root, "no-desc", `---
name: no-desc
description: ""
allowed-tools: /dev/fs
---

body
`)

	loader := NewSkillLoader([]string{root})

	// Legacy 2-value signature kept intact.
	_, err := loader.LoadMetadata("no-desc")
	if err == nil {
		t.Fatal("expected error for empty description via legacy LoadMetadata")
	}
	// Whether wrapped *LenientSkipError or plain error — callers like Epic 2
	// must see "non-nil error" and skip the skill.
}
