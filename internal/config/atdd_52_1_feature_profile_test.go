package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ATDD 52.1: Feature Profile 配置与加载
//
// 来源 Story：_bmad-output/implementation-artifacts/52-1-feature-profile-config-and-loading.md
// Story Key: 52-1-feature-profile-config-and-loading
//
// 验收标准（Acceptance Criteria）：
//   AC1: FeatureFlags struct 包含 9 个 bool 字段（Planning/Replan/Specialize/DiscoverSkill/Spawn/DiffMemory/StemMatcher/Immune/Compaction）
//   AC2: FeatureProfile 类型 (string)，合法值 baseline|core|adaptive|full|custom
//   AC3: ResolveFeatures(configPath) → (FeatureFlags, FeatureProfile, []string)：读 config.yaml features 段 → ENV override → 展开
//   AC4: 无配置时默认 profile: full（零破坏性）
//   AC5: RNIX_FEATURE_PROFILE 环境变量支持 5 个值，非法值 warning + fallback full
//   AC6: custom 模式读 features.custom 段，未列出 flag 默认 true
//   AC7: 单元测试覆盖全部场景
//
// ============================== RED PHASE ==============================
// 本测试在 Story 52.1 实现前应 FAIL（红灯）。
// 预期失败原因：config 包无法编译——FeatureFlags / FeatureProfile /
// ResolveFeatures / ProfileBaseline 等尚未定义（undefined）。
// 实现后所有用例应转绿（含 go test -race）。
// 这是 Go ATDD 的标准红灯形态，与 kernel 既有 atdd_* 测试一致。
//
// 测试严格使用 Story Dev Notes 规定的契约：
//   - 类型：FeatureFlags (9 bool) / FeatureProfile (string)
//   - 常量：ProfileBaseline / ProfileCore / ProfileAdaptive / ProfileFull / ProfileCustom
//   - 函数：ResolveFeatures(configPath string) (FeatureFlags, FeatureProfile, []string)
//   - 预设矩阵：baseline 全 false; core = Planning+Spawn+Compaction; adaptive = core + 6 项; full = 全 true
//   - custom 未列出 flag 默认 true（非 false）
//   - 诊断：FeatureFlags.String() 返回紧凑一行
// ======================================================================

// writeConfigYAML 把 YAML 内容写到 tmpDir/.rnix/config.yaml 并返回完整路径。
func writeConfigYAML(t *testing.T, tmpDir, content string) string {
	t.Helper()
	dir := filepath.Join(tmpDir, ".rnix")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return p
}

// ---------- AC1: FeatureFlags struct 有 9 个 bool 字段 ----------

func TestATDD_52_1_AC1_FeatureFlags_StructShape(t *testing.T) {
	rt := reflect.TypeFor[FeatureFlags]()

	expectedFields := []string{
		"Planning", "Replan", "Specialize", "DiscoverSkill", "Spawn",
		"DiffMemory", "StemMatcher", "Immune", "Compaction",
	}

	if rt.NumField() != len(expectedFields) {
		t.Fatalf("FeatureFlags has %d fields, want %d", rt.NumField(), len(expectedFields))
	}

	for _, name := range expectedFields {
		f, ok := rt.FieldByName(name)
		if !ok {
			t.Errorf("FeatureFlags missing field %q", name)
			continue
		}
		if f.Type.Kind() != reflect.Bool {
			t.Errorf("FeatureFlags.%s is %v, want bool", name, f.Type.Kind())
		}
	}
}

// ---------- AC2: FeatureProfile 5 个常量 ----------

func TestATDD_52_1_AC2_FeatureProfile_Constants(t *testing.T) {
	profiles := map[string]FeatureProfile{
		"baseline": ProfileBaseline,
		"core":     ProfileCore,
		"adaptive": ProfileAdaptive,
		"full":     ProfileFull,
		"custom":   ProfileCustom,
	}
	for expected, constant := range profiles {
		if string(constant) != expected {
			t.Errorf("Profile constant %q = %q, want %q", expected, constant, expected)
		}
	}
}

// ---------- AC7.1: 4 个预设展开正确 ----------

func TestATDD_52_1_AC7_Presets_Baseline(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: baseline
`)
	flags, profile, warnings := ResolveFeatures(configPath)
	if profile != ProfileBaseline {
		t.Fatalf("profile = %q, want %q", profile, ProfileBaseline)
	}
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	// baseline: 全部 false
	if flags.Planning || flags.Replan || flags.Specialize || flags.DiscoverSkill ||
		flags.Spawn || flags.DiffMemory || flags.StemMatcher || flags.Immune || flags.Compaction {
		t.Errorf("baseline should have all flags false, got %s", flags.String())
	}
}

func TestATDD_52_1_AC7_Presets_Core(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: core
`)
	flags, profile, _ := ResolveFeatures(configPath)
	if profile != ProfileCore {
		t.Fatalf("profile = %q, want %q", profile, ProfileCore)
	}
	// core: Planning=true Spawn=true Compaction=true, 其余 false
	if !flags.Planning {
		t.Error("core: Planning should be true")
	}
	if !flags.Spawn {
		t.Error("core: Spawn should be true")
	}
	if !flags.Compaction {
		t.Error("core: Compaction should be true")
	}
	if flags.Replan || flags.Specialize || flags.DiscoverSkill || flags.DiffMemory || flags.StemMatcher || flags.Immune {
		t.Errorf("core: non-core flags should be false, got %s", flags.String())
	}
}

func TestATDD_52_1_AC7_Presets_Adaptive(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: adaptive
`)
	flags, profile, _ := ResolveFeatures(configPath)
	if profile != ProfileAdaptive {
		t.Fatalf("profile = %q, want %q", profile, ProfileAdaptive)
	}
	// adaptive: 除 Immune 外全 true
	if !flags.Planning || !flags.Replan || !flags.Specialize || !flags.DiscoverSkill ||
		!flags.Spawn || !flags.DiffMemory || !flags.StemMatcher || !flags.Compaction {
		t.Errorf("adaptive: all except Immune should be true, got %s", flags.String())
	}
	if flags.Immune {
		t.Error("adaptive: Immune should be false")
	}
}

func TestATDD_52_1_AC7_Presets_Full(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: full
`)
	flags, profile, _ := ResolveFeatures(configPath)
	if profile != ProfileFull {
		t.Fatalf("profile = %q, want %q", profile, ProfileFull)
	}
	// full: 全部 true
	if !flags.Planning || !flags.Replan || !flags.Specialize || !flags.DiscoverSkill ||
		!flags.Spawn || !flags.DiffMemory || !flags.StemMatcher || !flags.Immune || !flags.Compaction {
		t.Errorf("full: all flags should be true, got %s", flags.String())
	}
}

// ---------- AC4: 无配置文件 → 默认 full ----------

func TestATDD_52_1_AC4_DefaultFull_NoFile(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist", "config.yaml")
	flags, profile, warnings := ResolveFeatures(nonExistent)
	if profile != ProfileFull {
		t.Fatalf("profile = %q, want %q (default full)", profile, ProfileFull)
	}
	if len(warnings) > 0 {
		t.Errorf("no-file should not produce warnings, got %v", warnings)
	}
	// full 的全部 true
	if !flags.Planning || !flags.Replan || !flags.Specialize || !flags.DiscoverSkill ||
		!flags.Spawn || !flags.DiffMemory || !flags.StemMatcher || !flags.Immune || !flags.Compaction {
		t.Errorf("default full should have all flags true, got %s", flags.String())
	}
}

func TestATDD_52_1_AC4_DefaultFull_EmptyFile(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), "")
	flags, profile, _ := ResolveFeatures(configPath)
	if profile != ProfileFull {
		t.Fatalf("empty file: profile = %q, want %q", profile, ProfileFull)
	}
	if !flags.Planning || !flags.Immune || !flags.Compaction {
		t.Errorf("empty file should resolve to full, got %s", flags.String())
	}
}

func TestATDD_52_1_AC4_DefaultFull_NoFeaturesSection(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
immune:
  enabled: true
`)
	flags, profile, _ := ResolveFeatures(configPath)
	if profile != ProfileFull {
		t.Fatalf("no features section: profile = %q, want %q", profile, ProfileFull)
	}
	if !flags.Planning || !flags.Immune {
		t.Errorf("no features section should resolve to full, got %s", flags.String())
	}
}

// ---------- AC3 + AC5: ENV override ----------

func TestATDD_52_1_AC5_ENVOverride_ValidProfile(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: full
`)
	t.Setenv("RNIX_FEATURE_PROFILE", "baseline")
	flags, profile, warnings := ResolveFeatures(configPath)
	if profile != ProfileBaseline {
		t.Fatalf("ENV override: profile = %q, want %q", profile, ProfileBaseline)
	}
	if len(warnings) > 0 {
		t.Errorf("valid ENV should not produce warnings, got %v", warnings)
	}
	// baseline 全 false
	if flags.Planning || flags.Spawn {
		t.Errorf("ENV baseline should override config full, got %s", flags.String())
	}
}

func TestATDD_52_1_AC5_ENVOverride_InvalidProfile(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: core
`)
	t.Setenv("RNIX_FEATURE_PROFILE", "turbo")
	flags, profile, warnings := ResolveFeatures(configPath)
	if profile != ProfileFull {
		t.Fatalf("invalid ENV: profile = %q, want %q (fallback full)", profile, ProfileFull)
	}
	if len(warnings) == 0 {
		t.Fatal("invalid ENV should produce at least one warning")
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "turbo") {
			found = true
		}
	}
	if !found {
		t.Errorf("warning should mention invalid value 'turbo', got %v", warnings)
	}
	// fallback full
	if !flags.Planning || !flags.Immune {
		t.Errorf("invalid ENV should fallback to full, got %s", flags.String())
	}
}

func TestATDD_52_1_AC5_ENVOverride_EmptyString(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: baseline
`)
	t.Setenv("RNIX_FEATURE_PROFILE", "")
	_, profile, _ := ResolveFeatures(configPath)
	if profile != ProfileBaseline {
		t.Fatalf("empty ENV should not override, profile = %q, want %q", profile, ProfileBaseline)
	}
}

// ---------- AC6: custom profile ----------

func TestATDD_52_1_AC6_CustomProfile_PartialFlags(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: custom
  custom:
    planning: true
    replan: false
    diff_memory: false
    stem_matcher: false
`)
	flags, profile, _ := ResolveFeatures(configPath)
	if profile != ProfileCustom {
		t.Fatalf("profile = %q, want %q", profile, ProfileCustom)
	}
	// 显式设置的值
	if !flags.Planning {
		t.Error("custom: planning explicitly true")
	}
	if flags.Replan {
		t.Error("custom: replan explicitly false")
	}
	if flags.DiffMemory {
		t.Error("custom: diff_memory explicitly false")
	}
	if flags.StemMatcher {
		t.Error("custom: stem_matcher explicitly false")
	}
	// 未列出的 flag 默认 true (AC6 关键行为)
	if !flags.Specialize {
		t.Error("custom: specialize not listed → should default true")
	}
	if !flags.DiscoverSkill {
		t.Error("custom: discover_skill not listed → should default true")
	}
	if !flags.Spawn {
		t.Error("custom: spawn not listed → should default true")
	}
	if !flags.Immune {
		t.Error("custom: immune not listed → should default true")
	}
	if !flags.Compaction {
		t.Error("custom: compaction not listed → should default true")
	}
}

func TestATDD_52_1_AC6_CustomProfile_AllFalse(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: custom
  custom:
    planning: false
    replan: false
    specialize: false
    discover_skill: false
    spawn: false
    diff_memory: false
    stem_matcher: false
    immune: false
    compaction: false
`)
	flags, profile, _ := ResolveFeatures(configPath)
	if profile != ProfileCustom {
		t.Fatalf("profile = %q, want %q", profile, ProfileCustom)
	}
	if flags.Planning || flags.Replan || flags.Specialize || flags.DiscoverSkill ||
		flags.Spawn || flags.DiffMemory || flags.StemMatcher || flags.Immune || flags.Compaction {
		t.Errorf("custom all-false: should have all flags false, got %s", flags.String())
	}
}

func TestATDD_52_1_AC6_CustomProfile_EmptyCustomSection(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: custom
`)
	flags, profile, _ := ResolveFeatures(configPath)
	if profile != ProfileCustom {
		t.Fatalf("profile = %q, want %q", profile, ProfileCustom)
	}
	// custom 段为空 → 所有 flag 默认 true
	if !flags.Planning || !flags.Replan || !flags.Specialize || !flags.DiscoverSkill ||
		!flags.Spawn || !flags.DiffMemory || !flags.StemMatcher || !flags.Immune || !flags.Compaction {
		t.Errorf("empty custom section: all flags should default true, got %s", flags.String())
	}
}

// ---------- 非 custom profile 忽略 custom 段 ----------

func TestATDD_52_1_NonCustomProfile_IgnoresCustomSection(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: baseline
  custom:
    planning: true
    immune: true
`)
	flags, profile, _ := ResolveFeatures(configPath)
	if profile != ProfileBaseline {
		t.Fatalf("profile = %q, want %q", profile, ProfileBaseline)
	}
	// baseline 全 false，custom 段应被忽略
	if flags.Planning || flags.Immune {
		t.Errorf("baseline should ignore custom section, got %s", flags.String())
	}
}

// ---------- 配置文件解析失败 → warning + full ----------

func TestATDD_52_1_MalformedYAML_FallbackFull(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: [invalid yaml structure
`)
	flags, profile, warnings := ResolveFeatures(configPath)
	if profile != ProfileFull {
		t.Fatalf("malformed YAML: profile = %q, want %q", profile, ProfileFull)
	}
	if len(warnings) == 0 {
		t.Error("malformed YAML should produce warnings")
	}
	if !flags.Planning || !flags.Immune {
		t.Errorf("malformed YAML should fallback to full, got %s", flags.String())
	}
}

// ---------- 组合矩阵：features 段与 immune 段共存 ----------

func TestATDD_52_1_CombinationMatrix_FeaturesAndImmuneCoexist(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
immune:
  enabled: true
  warn_only: false
features:
  profile: core
checkpoint:
  interval_steps: 10
`)
	flags, profile, warnings := ResolveFeatures(configPath)
	if profile != ProfileCore {
		t.Fatalf("coexistence: profile = %q, want %q", profile, ProfileCore)
	}
	if len(warnings) > 0 {
		t.Errorf("coexistence should not produce warnings, got %v", warnings)
	}
	// core 预设
	if !flags.Planning || !flags.Spawn || !flags.Compaction {
		t.Errorf("coexistence: core flags incorrect, got %s", flags.String())
	}
	if flags.Immune || flags.DiffMemory {
		t.Errorf("coexistence: non-core flags should be false, got %s", flags.String())
	}
}

// ---------- AC7.2: FeatureFlags.String() ----------

func TestATDD_52_1_AC7_FeatureFlags_String(t *testing.T) {
	ff := FeatureFlags{
		Planning: true, Replan: false, Specialize: true,
		DiscoverSkill: false, Spawn: true, DiffMemory: false,
		StemMatcher: false, Immune: true, Compaction: true,
	}
	s := ff.String()
	if !strings.HasPrefix(s, "features[") {
		t.Errorf("String() should start with 'features[', got %q", s)
	}
	if !strings.Contains(s, "planning=true") {
		t.Errorf("String() should contain 'planning=true', got %q", s)
	}
	if !strings.Contains(s, "replan=false") {
		t.Errorf("String() should contain 'replan=false', got %q", s)
	}
	if !strings.Contains(s, "immune=true") {
		t.Errorf("String() should contain 'immune=true', got %q", s)
	}
}

// ---------- ENV custom via env ----------

func TestATDD_52_1_ENVOverride_Custom(t *testing.T) {
	configPath := writeConfigYAML(t, t.TempDir(), `
features:
  profile: full
  custom:
    planning: false
    immune: false
`)
	t.Setenv("RNIX_FEATURE_PROFILE", "custom")
	flags, profile, _ := ResolveFeatures(configPath)
	if profile != ProfileCustom {
		t.Fatalf("ENV custom: profile = %q, want %q", profile, ProfileCustom)
	}
	// ENV 设为 custom → 应读取 config 中的 custom 段
	if flags.Planning {
		t.Error("ENV custom: planning explicitly false in config custom section")
	}
	if flags.Immune {
		t.Error("ENV custom: immune explicitly false in config custom section")
	}
	// 未列出的默认 true
	if !flags.Spawn || !flags.Compaction {
		t.Errorf("ENV custom: unlisted flags should be true, got %s", flags.String())
	}
}
