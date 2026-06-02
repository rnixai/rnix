package kernel

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// ATDD 52.3: 可观测性（CLI + strace + proc-info 持久化）
//
// 来源 Story：_bmad-output/implementation-artifacts/52-3-observability-cli-strace-procinfo.md
// Story Key: 52-3-observability-cli-strace-procinfo
//
// 验收标准（Acceptance Criteria）：
//   AC1: `rnix config show` 输出当前 daemon 活跃的 feature profile 名称 + 各 flag 状态
//   AC2: 进程 spawn 时 emit `feature_profile` strace 事件（含 profile 名 + flags 快照）
//   AC3: `proc-info.json` 增加 `feature_profile` 字段（profile 名称 string）
//   AC4: Dashboard Detail 面板展示进程的 feature profile
//
// ============================== RED PHASE ==============================
// 本测试在 Story 52.3 实现前应 FAIL（红灯）。
// 预期失败原因：
//   1. kernel.FeatureFlags 无 ProfileName string 字段（编译错误）
//   2. FullFeatureFlags() 不设 ProfileName（断言失败）
//   3. vfs.ProcInfo 无 FeatureProfile string 字段（编译错误）
//   4. procInfoDisk 无 FeatureProfile 字段（编译错误）
//   5. GetProcInfo 不填充 FeatureProfile（断言失败）
//   6. spawn 后无 feature_profile strace 事件（断言失败）
//
// 实现后所有用例应转绿（含 go test -race）。
// ======================================================================

// ---------- AC2 前提: FeatureFlags.ProfileName 字段存在 ----------

func TestATDD_52_3_AC2_FeatureFlags_HasProfileName(t *testing.T) {
	rt := reflect.TypeFor[FeatureFlags]()

	f, ok := rt.FieldByName("ProfileName")
	if !ok {
		t.Fatal("FeatureFlags missing field ProfileName")
	}
	if f.Type.Kind() != reflect.String {
		t.Errorf("FeatureFlags.ProfileName is %v, want string", f.Type.Kind())
	}
}

func TestATDD_52_3_AC2_FullFeatureFlags_SetsProfileName(t *testing.T) {
	flags := FullFeatureFlags()
	if flags.ProfileName != "full" {
		t.Errorf("FullFeatureFlags().ProfileName = %q, want %q", flags.ProfileName, "full")
	}
}

// ---------- AC2: spawn 时 emit feature_profile strace 事件 ----------

func TestATDD_52_3_AC2_SpawnEmitsFeatureProfileEvent(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, _ := newTestKernel(t, llmFile)

	k.SetFeatureFlags(FeatureFlags{
		ProfileName:   "adaptive",
		Planning:      true,
		Replan:        true,
		Specialize:    true,
		DiscoverSkill: true,
		Spawn:         true,
		DiffMemory:    true,
		StemMatcher:   true,
		Immune:        false,
		Compaction:    true,
	})

	pid, err := k.Spawn("test feature_profile event", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found after spawn")
	}
	waitForExit(t, proc)

	var found bool
	proc.mu.Lock()
	ch := proc.DebugChan
	proc.mu.Unlock()

	if ch != nil {
	drain:
		for {
			select {
			case ev := <-ch:
				if ev.Syscall == "feature_profile" {
					found = true
					if got := ev.Args["profile"]; got != "adaptive" {
						t.Errorf("feature_profile.profile = %v, want %q", got, "adaptive")
					}
					if got := ev.Args["planning"]; got != true {
						t.Errorf("feature_profile.planning = %v, want true", got)
					}
					if got := ev.Args["immune"]; got != false {
						t.Errorf("feature_profile.immune = %v, want false", got)
					}
					if got := ev.Args["compaction"]; got != true {
						t.Errorf("feature_profile.compaction = %v, want true", got)
					}
					break drain
				}
			default:
				break drain
			}
		}
	}
	if !found {
		t.Error("spawn did not emit feature_profile strace event")
	}
}

func TestATDD_52_3_AC2_FeatureProfileEvent_AllFlagsPresent(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, _ := newTestKernel(t, llmFile)

	k.SetFeatureFlags(FeatureFlags{
		ProfileName: "baseline",
	})

	pid, err := k.Spawn("test all flags in event", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found after spawn")
	}
	waitForExit(t, proc)

	expectedKeys := []string{
		"profile", "planning", "replan", "specialize", "discover_skill",
		"spawn", "diff_memory", "stem_matcher", "immune", "compaction",
	}

	proc.mu.Lock()
	ch := proc.DebugChan
	proc.mu.Unlock()

	if ch != nil {
		for {
			select {
			case ev := <-ch:
				if ev.Syscall == "feature_profile" {
					for _, key := range expectedKeys {
						if _, exists := ev.Args[key]; !exists {
							t.Errorf("feature_profile event missing key %q", key)
						}
					}
					return
				}
			default:
				t.Error("feature_profile strace event not found")
				return
			}
		}
	}
	t.Error("DebugChan is nil, cannot verify feature_profile event")
}

// ---------- AC3: procInfoDisk FeatureProfile roundtrip ----------

func TestATDD_52_3_AC3_ProcInfoDisk_FeatureProfile_Roundtrip(t *testing.T) {
	original := vfs.ProcInfo{
		PID:            1,
		UUID:           "test-uuid-52-3",
		State:          3, // Dead
		Intent:         "roundtrip test",
		FeatureProfile: "adaptive",
	}

	disk := procInfoToDisk(original)
	if disk.FeatureProfile != "adaptive" {
		t.Errorf("procInfoToDisk: FeatureProfile = %q, want %q", disk.FeatureProfile, "adaptive")
	}

	restored := procInfoFromDisk(disk)
	if restored.FeatureProfile != "adaptive" {
		t.Errorf("procInfoFromDisk: FeatureProfile = %q, want %q", restored.FeatureProfile, "adaptive")
	}
}

func TestATDD_52_3_AC3_ProcInfoDisk_FeatureProfile_Omitempty(t *testing.T) {
	disk := procInfoDisk{
		PID:   1,
		UUID:  "test-uuid-omitempty",
		State: "Dead",
	}

	data, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if _, exists := m["feature_profile"]; exists {
		t.Error("procInfoDisk with empty FeatureProfile should omit feature_profile from JSON")
	}
}

func TestATDD_52_3_AC3_ProcInfoDisk_BackwardCompat_NoField(t *testing.T) {
	legacyJSON := `{"pid":1,"uuid":"legacy","state":"Dead","intent":"old","tokens_used":0,"created_at":"2026-01-01T00:00:00Z","ctx_id":0,"pipeline_index":0,"pipeline_total":0,"ppid":0}`

	var disk procInfoDisk
	if err := json.Unmarshal([]byte(legacyJSON), &disk); err != nil {
		t.Fatalf("unmarshal legacy JSON: %v", err)
	}

	info := procInfoFromDisk(disk)
	if info.FeatureProfile != "" {
		t.Errorf("legacy JSON without feature_profile: FeatureProfile = %q, want empty", info.FeatureProfile)
	}
}

// ---------- AC3: GetProcInfo 填充 FeatureProfile ----------

func TestATDD_52_3_AC3_GetProcInfo_PopulatesFeatureProfile(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _, _ := newTestKernel(t, llmFile)

	k.SetFeatureFlags(FeatureFlags{
		ProfileName: "core",
		Planning:    true,
		Replan:      true,
		Compaction:  true,
	})

	pid, err := k.Spawn("test GetProcInfo FeatureProfile", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	info, err := k.GetProcInfo(pid)
	if err != nil {
		t.Fatalf("GetProcInfo failed: %v", err)
	}

	if info.FeatureProfile != "core" {
		t.Errorf("GetProcInfo().FeatureProfile = %q, want %q", info.FeatureProfile, "core")
	}
}
