package detail

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// ATDD 52.3 AC#4: Dashboard Detail 面板展示 feature profile
//
// 来源 Story：_bmad-output/implementation-artifacts/52-3-observability-cli-strace-procinfo.md
// Story Key: 52-3-observability-cli-strace-procinfo
//
// ============================== RED PHASE ==============================
// 预期失败原因：
//   1. GetProcDetailResponse 无 FeatureProfile 字段（编译错误）
//   2. render.go 不渲染 Profile 行（断言失败）
// ======================================================================

// ---------- AC4: 非 full profile 渲染 Profile 行 ----------

func TestATDD_52_3_AC4_RenderFeatureProfile_NonFull(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:            42,
			UUID:           "test-uuid-52-3",
			State:          "running",
			Intent:         "ablation test",
			Provider:       "claude",
			Model:          "sonnet",
			FeatureProfile: "adaptive",
		},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)

	if !strings.Contains(got, "Profile:") {
		t.Errorf("expected render output to contain 'Profile:' for non-full profile, got:\n%s", got)
	}
	if !strings.Contains(got, "adaptive") {
		t.Errorf("expected render output to contain 'adaptive', got:\n%s", got)
	}
}

func TestATDD_52_3_AC4_RenderFeatureProfile_Baseline(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:            42,
			UUID:           "test-uuid-baseline",
			State:          "running",
			Intent:         "baseline ablation",
			Provider:       "claude",
			Model:          "sonnet",
			FeatureProfile: "baseline",
		},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)

	if !strings.Contains(got, "Profile:") {
		t.Errorf("expected render output to contain 'Profile:' for baseline profile, got:\n%s", got)
	}
	if !strings.Contains(got, "baseline") {
		t.Errorf("expected render output to contain 'baseline', got:\n%s", got)
	}
}

// ---------- AC4: full profile 不渲染 Profile 行（减少视觉噪声） ----------

func TestATDD_52_3_AC4_RenderFeatureProfile_Full_Hidden(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:            42,
			UUID:           "test-uuid-full",
			State:          "running",
			Intent:         "full profile test",
			Provider:       "claude",
			Model:          "sonnet",
			FeatureProfile: "full",
		},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)

	if strings.Contains(got, "Profile:") {
		t.Errorf("expected render output to NOT contain 'Profile:' for full profile (default), got:\n%s", got)
	}
}

// ---------- AC4: 空 profile 不渲染 Profile 行 ----------

func TestATDD_52_3_AC4_RenderFeatureProfile_Empty_Hidden(t *testing.T) {
	state := DetailState{
		Detail: &ipc.GetProcDetailResponse{
			PID:      42,
			UUID:     "test-uuid-empty",
			State:    "running",
			Intent:   "empty profile test",
			Provider: "claude",
			Model:    "sonnet",
		},
	}
	ctx := RenderContext{SelectedPID: types.PID(42)}
	got := Render(state, ctx, 80)

	if strings.Contains(got, "Profile:") {
		t.Errorf("expected render output to NOT contain 'Profile:' when FeatureProfile is empty, got:\n%s", got)
	}
}
