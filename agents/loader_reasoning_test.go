package agents

import (
	"testing"

	"github.com/rnixai/rnix/skills"
)

func TestAgentLoader_PlanningDefault(t *testing.T) {
	sl := skills.NewSkillLoader([]string{"../skills/testdata"})
	al := NewAgentLoader([]string{"testdata"}, sl, nil)

	info, err := al.Load("mock-agent")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if info.Manifest.Planning != nil {
		t.Errorf("Planning = %v, want nil (default = enabled)", *info.Manifest.Planning)
	}
}

func TestAgentLoader_PlanningExplicitTrue(t *testing.T) {
	sl := skills.NewSkillLoader([]string{"../skills/testdata"})
	al := NewAgentLoader([]string{"testdata"}, sl, nil)

	info, err := al.Load("planning-true")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if info.Manifest.Planning == nil {
		t.Fatal("Planning = nil, want *true")
	}
	if !*info.Manifest.Planning {
		t.Errorf("Planning = %v, want true", *info.Manifest.Planning)
	}
}

func TestAgentLoader_PlanningExplicitFalse(t *testing.T) {
	sl := skills.NewSkillLoader([]string{"../skills/testdata"})
	al := NewAgentLoader([]string{"testdata"}, sl, nil)

	info, err := al.Load("planning-false")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if info.Manifest.Planning == nil {
		t.Fatal("Planning = nil, want *false")
	}
	if *info.Manifest.Planning {
		t.Errorf("Planning = %v, want false", *info.Manifest.Planning)
	}
}
