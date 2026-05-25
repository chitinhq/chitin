package activities

import (
	"testing"
)

// TestComposeDetail proves the standard prefix shape:
//   - empty reviewer → caller's detail unchanged
//   - reviewer + detail → "reviewer=<id> | <detail>"
//   - reviewer + empty detail → "reviewer=<id>"
//
// Stable shape so the operator can grep Discord messages by
// "reviewer=codex" or by detail-substring independently.
func TestComposeDetail(t *testing.T) {
	cases := []struct {
		name string
		in   EscalateInternalRereviewInput
		want string
	}{
		{
			name: "reviewer+detail",
			in:   EscalateInternalRereviewInput{ReviewerDriver: "codex", Detail: "approve-shaped but confidence=low"},
			want: "reviewer=codex | approve-shaped but confidence=low",
		},
		{
			name: "reviewer_only",
			in:   EscalateInternalRereviewInput{ReviewerDriver: "claudecode"},
			want: "reviewer=claudecode",
		},
		{
			name: "detail_only",
			in:   EscalateInternalRereviewInput{Detail: "no eligible re-reviewer"},
			want: "no eligible re-reviewer",
		},
		{
			name: "both_empty",
			in:   EscalateInternalRereviewInput{},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := composeDetail(tc.in); got != tc.want {
				t.Errorf("composeDetail = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSeverityFor proves every spec-116 escalation reason renders as
// SeverityAlert. The decision is intentional — even the low-confidence
// approve path (where autopilot did proceed) escalates as alert, not as
// ready, because the operator is being asked to verify that the loop
// shouldn't have stopped. Drift here would silently downgrade
// notifications that the operator needs to see.
func TestSeverityFor(t *testing.T) {
	reasons := []EscalateInternalRereviewReason{
		ReasonRereviewLowConfidence,
		ReasonRereviewRequestChanges,
		ReasonRereviewAbstain,
		ReasonRereviewSkipped,
		ReasonRereviewFailed,
	}
	for _, r := range reasons {
		if got := severityFor(r); got != SeverityAlert {
			t.Errorf("severityFor(%q) = %v, want SeverityAlert", string(r), got)
		}
	}
}

// TestEscalateInternalRereview_RejectsEmptyPRURL proves the activity
// short-circuits cleanly when the workflow forgot to populate PRURL —
// the helper notifyDiscordEscalation would drop the notice anyway, but
// reporting "skipped: empty PRURL" in the chain explanation makes the
// drop visible to operators looking at why no Discord ping fired.
func TestEscalateInternalRereview_RejectsEmptyPRURL(t *testing.T) {
	act := NewEscalateInternalRereview()
	res, err := act.Execute(nil, EscalateInternalRereviewInput{
		Reason: ReasonRereviewLowConfidence,
	})
	if err != nil {
		t.Fatalf("Execute returned err %v, want nil (fail-soft contract)", err)
	}
	if res.Notified {
		t.Errorf("Notified=true with empty PRURL; want false")
	}
	if res.Explanation != "skipped: empty PRURL" {
		t.Errorf("Explanation = %q, want %q", res.Explanation, "skipped: empty PRURL")
	}
}

// TestEscalateInternalRereview_RejectsEmptyReason proves the activity
// short-circuits when Reason isn't set — the chain event would have no
// classification, defeating the closed-taxonomy guarantee.
func TestEscalateInternalRereview_RejectsEmptyReason(t *testing.T) {
	act := NewEscalateInternalRereview()
	res, err := act.Execute(nil, EscalateInternalRereviewInput{
		PRURL: "https://github.com/owner/repo/pull/1",
	})
	if err != nil {
		t.Fatalf("Execute returned err %v, want nil (fail-soft contract)", err)
	}
	if res.Notified {
		t.Errorf("Notified=true with empty Reason; want false")
	}
	if res.Explanation != "skipped: empty Reason" {
		t.Errorf("Explanation = %q, want %q", res.Explanation, "skipped: empty Reason")
	}
}
