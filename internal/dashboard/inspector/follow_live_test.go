// Package inspector — follow_live_test.go (Story 38-5 PR11 Step 4(c))
package inspector

import (
	"strings"
	"testing"
)

func TestStopFollowLive_AlreadyOff_Noop(t *testing.T) {
	state := InspectorState{FollowLive: false}
	got, stopped := StopFollowLive(state)
	if stopped {
		t.Error("FollowLive=false 应返回 stopped=false（idempotent）")
	}
	if got.FollowLive {
		t.Error("FollowLive 应保持 false")
	}
}

func TestStopFollowLive_Active_ClearsFlag(t *testing.T) {
	state := InspectorState{FollowLive: true}
	got, stopped := StopFollowLive(state)
	if !stopped {
		t.Error("FollowLive=true → StopFollowLive 应返回 stopped=true")
	}
	if got.FollowLive {
		t.Error("FollowLive 应被清零")
	}
}

func TestStopFollowLive_PreservesOtherFields(t *testing.T) {
	state := InspectorState{
		FollowLive: true,
		Lens:       2,
		DiffMode:   true,
		Detail:     nil,
	}
	got, _ := StopFollowLive(state)
	if got.Lens != 2 {
		t.Errorf("Lens 应保留为 2, got %d", got.Lens)
	}
	if !got.DiffMode {
		t.Error("DiffMode 应保留")
	}
}

func TestFollowLiveStoppedMsg_Constant(t *testing.T) {
	if FollowLiveStoppedMsg == "" {
		t.Error("FollowLiveStoppedMsg 不应为空")
	}
	// 检测中文 + F 提示同时存在（Story 36-6 AC-13 历史行为）
	if !strings.Contains(FollowLiveStoppedMsg, "Follow live: off") {
		t.Errorf("FollowLiveStoppedMsg 应含 \"Follow live: off\", got %q", FollowLiveStoppedMsg)
	}
	if !strings.Contains(FollowLiveStoppedMsg, "F") {
		t.Errorf("FollowLiveStoppedMsg 应含 F 恢复提示, got %q", FollowLiveStoppedMsg)
	}
}
