package verdict

import (
	"errors"
	"strings"
	"testing"
)

// TestValidate is a table-driven test covering every FR-014 invariant.
// Each row names the invariant under test and the expected outcome so a
// failure points the reader at the specific rule that regressed.
func TestValidate(t *testing.T) {
	cases := []struct {
		name           string
		in             StructuredVerdict
		wantOK         bool
		wantInvariant  string // substring of the ValidationError.Invariant on failure cases
	}{
		// --- approve ---
		{
			name:   "approve_minimal_valid",
			in:     StructuredVerdict{Verdict: Approve},
			wantOK: true,
		},
		{
			name:   "approve_with_concerns_valid",
			in:     StructuredVerdict{Verdict: Approve, Concerns: []string{"nit"}},
			wantOK: true,
		},
		{
			name:          "approve_with_blockers_invalid",
			in:            StructuredVerdict{Verdict: Approve, Blockers: []string{"x"}},
			wantInvariant: "approve_blockers_must_be_empty",
		},

		// --- approve-with-comments ---
		{
			name:   "approve_with_comments_concerns_only_valid",
			in:     StructuredVerdict{Verdict: ApproveWithComments, Concerns: []string{"watch this"}},
			wantOK: true,
		},
		{
			name:   "approve_with_comments_recommendations_only_valid",
			in:     StructuredVerdict{Verdict: ApproveWithComments, Recommendations: []string{"factor out"}},
			wantOK: true,
		},
		{
			name:   "approve_with_comments_both_valid",
			in:     StructuredVerdict{Verdict: ApproveWithComments, Concerns: []string{"c"}, Recommendations: []string{"r"}},
			wantOK: true,
		},
		{
			name:          "approve_with_comments_empty_lists_invalid",
			in:            StructuredVerdict{Verdict: ApproveWithComments},
			wantInvariant: "approve_with_comments_requires_concerns_or_recommendations",
		},
		{
			name:          "approve_with_comments_with_blockers_invalid",
			in:            StructuredVerdict{Verdict: ApproveWithComments, Concerns: []string{"c"}, Blockers: []string{"b"}},
			wantInvariant: "approve_with_comments_blockers_must_be_empty",
		},

		// --- request-changes ---
		{
			name:   "request_changes_minimal_valid",
			in:     StructuredVerdict{Verdict: RequestChanges, Blockers: []string{"missing tests"}},
			wantOK: true,
		},
		{
			name:   "request_changes_with_concerns_and_recs_valid",
			in:     StructuredVerdict{Verdict: RequestChanges, Blockers: []string{"b"}, Concerns: []string{"c"}, Recommendations: []string{"r"}},
			wantOK: true,
		},
		{
			name:          "request_changes_empty_blockers_invalid",
			in:            StructuredVerdict{Verdict: RequestChanges},
			wantInvariant: "request_changes_requires_blockers",
		},

		// --- abstain ---
		{
			name:   "abstain_minimal_valid",
			in:     StructuredVerdict{Verdict: Abstain},
			wantOK: true,
		},
		{
			name:   "abstain_with_reason_valid",
			in:     StructuredVerdict{Verdict: Abstain, Reason: "no spec context"},
			wantOK: true,
		},
		{
			name:          "abstain_with_concerns_invalid",
			in:            StructuredVerdict{Verdict: Abstain, Concerns: []string{"c"}},
			wantInvariant: "abstain_lists_must_be_empty",
		},
		{
			name:          "abstain_with_recommendations_invalid",
			in:            StructuredVerdict{Verdict: Abstain, Recommendations: []string{"r"}},
			wantInvariant: "abstain_lists_must_be_empty",
		},
		{
			name:          "abstain_with_blockers_invalid",
			in:            StructuredVerdict{Verdict: Abstain, Blockers: []string{"b"}},
			wantInvariant: "abstain_lists_must_be_empty",
		},

		// --- unknown enum ---
		{
			name:          "unknown_enum_value_invalid",
			in:            StructuredVerdict{Verdict: Enum("maybe")},
			wantInvariant: "", // checked via errors.Is(ErrUnknownEnum)
		},
		{
			name:          "empty_enum_value_invalid",
			in:            StructuredVerdict{Verdict: Enum("")},
			wantInvariant: "",
		},

		// --- empty-string list entries ---
		{
			name:          "concerns_empty_entry_invalid",
			in:            StructuredVerdict{Verdict: Approve, Concerns: []string{""}},
			wantInvariant: "concerns_entry_must_be_non_empty",
		},
		{
			name:          "recommendations_empty_entry_invalid",
			in:            StructuredVerdict{Verdict: Approve, Recommendations: []string{""}},
			wantInvariant: "recommendations_entry_must_be_non_empty",
		},
		{
			name:          "blockers_empty_entry_invalid",
			in:            StructuredVerdict{Verdict: RequestChanges, Blockers: []string{""}},
			wantInvariant: "blockers_entry_must_be_non_empty",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.in)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("Validate(%+v) = %v, want nil", tc.in, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate(%+v) = nil, want failure (%s)", tc.in, tc.wantInvariant)
			}
			// Unknown-enum cases are detected via ErrUnknownEnum, not invariant name.
			if tc.wantInvariant == "" {
				if !errors.Is(err, ErrUnknownEnum) {
					t.Fatalf("Validate(%+v) = %v, want errors.Is(ErrUnknownEnum)", tc.in, err)
				}
				return
			}
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Validate(%+v) = %v, want *ValidationError", tc.in, err)
			}
			if !strings.Contains(verr.Invariant, tc.wantInvariant) {
				t.Fatalf("Validate(%+v).Invariant = %q, want substring %q", tc.in, verr.Invariant, tc.wantInvariant)
			}
		})
	}
}

// TestEnumIsApproveShaped covers the two-membered predicate the aggregator
// uses to short-circuit on dual-approval. A regression here would silently
// allow a request-changes verdict to count as approval, so guard explicitly.
func TestEnumIsApproveShaped(t *testing.T) {
	for _, tc := range []struct {
		in   Enum
		want bool
	}{
		{Approve, true},
		{ApproveWithComments, true},
		{RequestChanges, false},
		{Abstain, false},
		{Enum(""), false},
		{Enum("maybe"), false},
	} {
		if got := tc.in.IsApproveShaped(); got != tc.want {
			t.Errorf("Enum(%q).IsApproveShaped() = %v, want %v", string(tc.in), got, tc.want)
		}
	}
}

// TestEnumValid is a tiny sanity check; ensures Valid() and IsApproveShaped()
// don't drift on the closed enum set.
func TestEnumValid(t *testing.T) {
	for _, e := range []Enum{Approve, ApproveWithComments, RequestChanges, Abstain} {
		if !e.Valid() {
			t.Errorf("%q.Valid() = false, want true", string(e))
		}
	}
	for _, e := range []Enum{"", "maybe", "block"} {
		if Enum(e).Valid() {
			t.Errorf("%q.Valid() = true, want false", e)
		}
	}
}
