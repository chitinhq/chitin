// judgement_test.go — hermetic SC-003 contract test for spec 115 T023.
//
// Runs a curated corpus of 20 hand-labelled Copilot-style review comments
// (10 mechanical, 10 design-judgement) through ClassifyDesignJudgement
// using the FR-007 default phrase set and asserts the SC-003 thresholds:
//
//   - precision on DesignJudgement ≥ 80%
//   - recall on Mechanical          ≥ 90%
//
// False positives (mechanical classified as judgement) are preferred over
// false negatives per FR-007 / SC-003: an escalated mechanical comment
// costs the operator one read; an un-escalated judgement comment burns a
// driver round and still escalates. The recall-on-mechanical floor caps
// that operator cost — at most one in ten mechanical comments may be
// misrouted to the escalation queue.
//
// Lives in the same package as judgement.go (T013) so it exercises the
// production API surface directly. No I/O — the default phrase set comes
// from DefaultJudgementPhrases(), not the optional operator-editable file
// at `.specify/judgement-phrases.txt`.
package speclint

import (
	"strings"
	"testing"
)

// TestClassifyDesignJudgement_HandLabeledCorpus is the SC-003 gate. The
// corpus deliberately mirrors the kinds of Copilot comments observed on
// real spec PRs (#1050's eight findings are the motivating dataset called
// out in spec 115's "Why" section). If a future phrase-set edit pushes
// either metric below threshold, this test fails and the operator must
// either tune the phrase file or accept the new precision/recall tradeoff
// by updating the thresholds with a documented rationale.
func TestClassifyDesignJudgement_HandLabeledCorpus(t *testing.T) {
	type labeled struct {
		body string
		want Class
	}

	corpus := []labeled{
		// --- 10 design-judgement comments (operator escalation expected) ---
		{"Consider whether US3 is really P2 vs P3 priority — the success criteria don't clearly differentiate it from US2.", DesignJudgement},
		{"Should this be split into two specs? FR-005 through FR-008 feel like a separate concern from FR-001 through FR-004.", DesignJudgement},
		{"Is this really in scope for this spec? FR-010 looks like it belongs in spec 113.", DesignJudgement},
		{"You might want to merge US1 and US3 — they describe the same iteration loop with different driver capability tags.", DesignJudgement},
		{"Could be a stretch goal — the linter rule L06 is doing a lot more work than the other rules and might warrant its own spec.", DesignJudgement},
		{"Should this be merged with spec 114's queue surface? The escalation paths overlap significantly.", DesignJudgement},
		{"Consider whether P1 is the right priority for US2 — the operator can still triage manually if the linter doesn't run.", DesignJudgement},
		{"Is this really mechanical? FR-007's classifier feels more like a design choice than a deterministic check.", DesignJudgement},
		{"Out of scope for now, but worth flagging: cross-repo spec aggregation will eventually need a similar discriminator.", DesignJudgement},
		{"Could be a P3 — the design-judgement classifier is nice-to-have, not blocking the iteration loop.", DesignJudgement},

		// --- 10 mechanical comments (driver iteration expected) ---
		{"The `chitin-kernel events` CLI referenced on line 78 doesn't exist — see `chitin-kernel --help`.", Mechanical},
		{"Broken cross-ref: depends_on lists spec 097 but there's no `.specify/specs/097-*/` directory.", Mechanical},
		{"`pr_iteration_skipped` event referenced in edge cases isn't in the FR-009 telemetry list — add to taxonomy or remove the reference.", Mechanical},
		{"Endpoint `gh api /pulls/N/comments/M/replies` returns 404 — that's not a real GitHub API path. Use `/pulls/N/comments` instead.", Mechanical},
		{"Reason `lease_lost` is referenced but not declared in any FR — needs taxonomy declaration in FR-010.", Mechanical},
		{"Frontmatter missing `status` field — required per L01.", Mechanical},
		{"Typo on line 113: `factor-listen` should be `factory-listen`.", Mechanical},
		{"FR-006 references the linter's allowlist but the path `.specify/cli-surfaces.txt` doesn't match T018's `.specify/known-cli-surfaces.txt`.", Mechanical},
		{"US2's `**Independent test:**` paragraph is missing — fails L07.", Mechanical},
		{"Task T011 references FR-099 which doesn't exist in spec.md.", Mechanical},
	}

	var labeledMech, labeledJudge int
	for _, c := range corpus {
		switch c.want {
		case Mechanical:
			labeledMech++
		case DesignJudgement:
			labeledJudge++
		}
	}
	if labeledMech != 10 || labeledJudge != 10 {
		t.Fatalf("corpus invariant: want 10 mechanical + 10 judgement, got %d + %d", labeledMech, labeledJudge)
	}

	phrases := DefaultJudgementPhrases()
	if len(phrases) == 0 {
		t.Fatalf("DefaultJudgementPhrases() returned empty set — classifier cannot fire")
	}

	var (
		mechAsMech    int // true mechanical, classified mechanical
		mechAsJudge   int // true mechanical, classified judgement (FP for judgement)
		judgeAsJudge  int // true judgement, classified judgement (TP for judgement)
		judgeAsMech   int // true judgement, classified mechanical (FN for judgement)
		misclassified []string
	)
	for _, c := range corpus {
		got := ClassifyDesignJudgement(c.body, phrases)
		switch {
		case c.want == Mechanical && got == Mechanical:
			mechAsMech++
		case c.want == Mechanical && got == DesignJudgement:
			mechAsJudge++
			misclassified = append(misclassified, "mechanical→judgement: "+c.body)
		case c.want == DesignJudgement && got == DesignJudgement:
			judgeAsJudge++
		case c.want == DesignJudgement && got == Mechanical:
			judgeAsMech++
			misclassified = append(misclassified, "judgement→mechanical: "+c.body)
		}
	}

	// SC-003 — precision on DesignJudgement ≥ 80%.
	// precision = TP / (TP + FP) = judgeAsJudge / (judgeAsJudge + mechAsJudge)
	predictedJudge := judgeAsJudge + mechAsJudge
	if predictedJudge == 0 {
		t.Fatalf("classifier produced zero DesignJudgement predictions on a corpus with 10 labelled judgement comments — phrase set is broken")
	}
	precisionJudge := float64(judgeAsJudge) / float64(predictedJudge)
	if precisionJudge < 0.80 {
		t.Errorf("precision on DesignJudgement = %.2f (%d/%d), want ≥ 0.80\nmisclassified:\n  %s",
			precisionJudge, judgeAsJudge, predictedJudge, strings.Join(misclassified, "\n  "))
	}

	// recall on Mechanical ≥ 90%.
	// recall = TP / (TP + FN) = mechAsMech / (mechAsMech + mechAsJudge)
	totalMech := mechAsMech + mechAsJudge
	recallMech := float64(mechAsMech) / float64(totalMech)
	if recallMech < 0.90 {
		t.Errorf("recall on Mechanical = %.2f (%d/%d), want ≥ 0.90\nmisclassified:\n  %s",
			recallMech, mechAsMech, totalMech, strings.Join(misclassified, "\n  "))
	}

	t.Logf("classifier metrics: precision_judgement=%.2f recall_mechanical=%.2f confusion=[mm=%d mj=%d jj=%d jm=%d]",
		precisionJudge, recallMech, mechAsMech, mechAsJudge, judgeAsJudge, judgeAsMech)
}
