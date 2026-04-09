package kernel

import (
	"testing"
)

func TestScoreSkillMatch_ExactNameMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "code-review", Description: "Review code changes", SearchHint: "lint review"}
	score := scoreSkillMatch(ds, "code-review")
	if score < 12 {
		t.Errorf("expected exact name match score >= 12, got %d", score)
	}
}

func TestScoreSkillMatch_PartialNameMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "code-review", Description: "Review code changes", SearchHint: "lint review"}
	score := scoreSkillMatch(ds, "code")
	if score < 6 {
		t.Errorf("expected partial name match score >= 6, got %d", score)
	}
}

func TestScoreSkillMatch_DescriptionMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "formatter", Description: "Format source files", SearchHint: ""}
	score := scoreSkillMatch(ds, "source")
	if score < 2 {
		t.Errorf("expected description match score >= 2, got %d", score)
	}
}

func TestScoreSkillMatch_SearchHintMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "deploy", Description: "Deploy to cloud", SearchHint: "kubernetes docker containers"}
	score := scoreSkillMatch(ds, "kubernetes")
	if score < 4 {
		t.Errorf("expected search_hint match score >= 4, got %d", score)
	}
}

func TestScoreSkillMatch_NoMatch(t *testing.T) {
	ds := DeferredSkillMeta{Name: "formatter", Description: "Format code", SearchHint: "prettify"}
	score := scoreSkillMatch(ds, "database")
	if score != 0 {
		t.Errorf("expected 0 score for no match, got %d", score)
	}
}

func TestScoreSkillMatch_CombinedScore(t *testing.T) {
	// "code-review" exact name (12) + hint contains "code-review" (4) + desc contains "code-review" (2) = 18
	ds := DeferredSkillMeta{Name: "code-review", Description: "Run code-review checks", SearchHint: "code-review lint"}
	score := scoreSkillMatch(ds, "code-review")
	if score < 12 {
		t.Errorf("expected combined score >= 12, got %d", score)
	}
}

func TestScoreSkillMatch_EmptyQuery(t *testing.T) {
	ds := DeferredSkillMeta{Name: "test", Description: "test", SearchHint: ""}
	score := scoreSkillMatch(ds, "")
	if score != 0 {
		t.Errorf("expected 0 score for empty query, got %d", score)
	}
}
