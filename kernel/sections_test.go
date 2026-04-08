package kernel

import (
	"testing"
)

func TestScoreSkillMatch_NameMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "code-review", Description: "Review code changes", SearchHint: "lint review"}
	score := scoreSkillMatch(ds, []string{"code"})
	if score <= 0 {
		t.Errorf("expected positive score for name match, got %f", score)
	}
}

func TestScoreSkillMatch_DescriptionMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "formatter", Description: "Format source files", SearchHint: ""}
	score := scoreSkillMatch(ds, []string{"source"})
	if score <= 0 {
		t.Errorf("expected positive score for description match, got %f", score)
	}
}

func TestScoreSkillMatch_SearchHintMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "deploy", Description: "Deploy to cloud", SearchHint: "kubernetes docker containers"}
	score := scoreSkillMatch(ds, []string{"kubernetes"})
	if score <= 0 {
		t.Errorf("expected positive score for search_hint match, got %f", score)
	}
}

func TestScoreSkillMatch_NoMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "formatter", Description: "Format code", SearchHint: "prettify"}
	score := scoreSkillMatch(ds, []string{"database"})
	if score != 0 {
		t.Errorf("expected 0 score for no match, got %f", score)
	}
}

func TestScoreSkillMatch_MultipleKeywords(t *testing.T) {
	ds := DeferredSkillMeta{Name: "code-review", Description: "Review code changes", SearchHint: "lint"}
	score := scoreSkillMatch(ds, []string{"code", "review"})
	if score <= 0 {
		t.Errorf("expected positive score, got %f", score)
	}
	// Both keywords match name, and "review" also matches description
	if score <= 0.3 {
		t.Errorf("expected high score for multi-keyword match, got %f", score)
	}
}

func TestScoreSkillMatch_CappedAtOne(t *testing.T) {
	ds := DeferredSkillMeta{Name: "code-review-lint", Description: "code review lint check", SearchHint: "code review lint"}
	score := scoreSkillMatch(ds, []string{"code"})
	if score > 1.0 {
		t.Errorf("score should be capped at 1.0, got %f", score)
	}
}

func TestScoreSkillMatch_EmptyKeywords(t *testing.T) {
	ds := DeferredSkillMeta{Name: "test", Description: "test", SearchHint: ""}
	score := scoreSkillMatch(ds, nil)
	if score != 0 {
		t.Errorf("expected 0 score for empty keywords, got %f", score)
	}
}
