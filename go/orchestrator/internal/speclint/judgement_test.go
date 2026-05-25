package speclint

import (
	"strings"
	"testing"
)

// seededPhrasesFile mirrors the .specify/judgement-phrases.txt seeded by
// spec 115 T019. Kept inline so the test is hermetic — no IO.
const seededPhrasesFile = `consider
might want
is this really
could be
should this be split
should this be merged
\b(P1|P2|P3)\b
in scope
out of scope
`

func mustParse(t *testing.T, content string) *JudgementPhrases {
	t.Helper()
	p, err := ParseJudgementPhrases(content)
	if err != nil {
		t.Fatalf("ParseJudgementPhrases: %v", err)
	}
	return p
}

func TestClassify_NilPhrases_Mechanical(t *testing.T) {
	if got := ClassifyDesignJudgement("anything goes", nil); got != Mechanical {
		t.Errorf("nil phrases: got %q, want %q", got, Mechanical)
	}
}

func TestClassify_EmptyPhrases_Mechanical(t *testing.T) {
	phrases := mustParse(t, "")
	if got := ClassifyDesignJudgement("consider this", phrases); got != Mechanical {
		t.Errorf("empty phrases: got %q, want %q", got, Mechanical)
	}
}

func TestClassify_EmptyComment_Mechanical(t *testing.T) {
	phrases := mustParse(t, seededPhrasesFile)
	if got := ClassifyDesignJudgement("", phrases); got != Mechanical {
		t.Errorf("empty comment: got %q, want %q", got, Mechanical)
	}
}

func TestClassify_SeededPhrasesHit(t *testing.T) {
	phrases := mustParse(t, seededPhrasesFile)
	cases := []struct {
		name    string
		comment string
	}{
		{"consider literal", "I'd consider extracting this into a helper."},
		{"case-insensitive Consider", "Consider whether this is the right boundary."},
		{"might want", "You might want to add a test for the empty case."},
		{"is this really", "is this really what we want for the iteration cap?"},
		{"could be", "this could be the wrong abstraction"},
		{"split US", "should this be split into US3 and US4?"},
		{"merge US", "should this be merged with spec 113's workflow?"},
		{"P1 priority", "I'd bump this to P1 priority — gating the loop"},
		{"P2 priority", "isn't this a P2 concern, not P1?"},
		{"in scope", "is the linter in scope for this spec or spec 117?"},
		{"out of scope", "feels out of scope for the MVP slice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyDesignJudgement(tc.comment, phrases); got != DesignJudgement {
				t.Errorf("comment %q: got %q, want %q", tc.comment, got, DesignJudgement)
			}
		})
	}
}

func TestClassify_MechanicalComments(t *testing.T) {
	phrases := mustParse(t, seededPhrasesFile)
	cases := []struct {
		name    string
		comment string
	}{
		{"typo", "typo: 'recieve' should be 'receive'"},
		{"missing FR", "FR-005 is referenced in tasks.md but not defined in spec.md"},
		{"plain prose", "this code path doesn't handle the empty input boundary"},
		{"P1 substring not word", "the P11 sensor reading should be normalized"},
		{"in-scope as compound", "in-scope-issues.md was not updated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyDesignJudgement(tc.comment, phrases); got != Mechanical {
				t.Errorf("comment %q: got %q, want %q", tc.comment, got, Mechanical)
			}
		})
	}
}

func TestClassify_MultiplePatternsMatch_StillDesignJudgement(t *testing.T) {
	phrases := mustParse(t, seededPhrasesFile)
	comment := "consider whether this is in scope or out of scope for P2 priority"
	if got := ClassifyDesignJudgement(comment, phrases); got != DesignJudgement {
		t.Errorf("multi-match comment: got %q, want %q", got, DesignJudgement)
	}
}

func TestParse_SkipsBlankLinesAndComments(t *testing.T) {
	content := `# operator-editable judgement phrases

consider

# section: scope phrases
in scope
`
	phrases := mustParse(t, content)
	if got, want := phrases.Count(), 2; got != want {
		t.Fatalf("pattern count: got %d, want %d", got, want)
	}
}

func TestParse_BadRegexReturnsError(t *testing.T) {
	content := "consider\n[unclosed\n"
	_, err := ParseJudgementPhrases(content)
	if err == nil {
		t.Fatal("expected error for malformed regex, got nil")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name offending line, got: %v", err)
	}
	if !strings.Contains(err.Error(), "[unclosed") {
		t.Errorf("error should quote offending pattern, got: %v", err)
	}
}
