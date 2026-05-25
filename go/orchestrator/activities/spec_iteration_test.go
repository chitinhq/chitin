package activities

import (
	"context"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/worktree"
)

// validSpecIterationInput returns an input that passes every guard, so
// each guard test can name the single field it invalidates.
func validSpecIterationInput() IterateSpecReviewInput {
	return IterateSpecReviewInput{
		PRNumber:   1234,
		PRBranch:   "feat/115-us1",
		TargetRepo: "/tmp/chitin",
		Repo:       "chitinhq/chitin",
		ReviewID:   99,
		SpecDir:    ".specify/specs/115-spec-pr-iteration",
		Round:      1,
		DriverID:   "claudecode",
		WorkUnitID: "iterate-spec-1234-review-99",
	}
}

// TestIterateSpecReview_ActivityNameStable locks the Temporal activity
// name. The workflow dispatches by literal string; a rename here would
// silently break dispatch.
func TestIterateSpecReview_ActivityNameStable(t *testing.T) {
	act := NewIterateSpecReview(nil, nil)
	if got := act.ActivityName(); got != "IterateSpecReview" {
		t.Errorf("ActivityName drift: want %q got %q", "IterateSpecReview", got)
	}
}

// TestIterateSpecReview_NoManagerOrRegistry asserts the wiring guard:
// nil Manager or Registry returns a populated Explanation rather than
// panicking or returning a misleading "not yet implemented" success.
func TestIterateSpecReview_NoManagerOrRegistry(t *testing.T) {
	act := NewIterateSpecReview(nil, nil)
	res, err := act.Execute(context.Background(), validSpecIterationInput())
	if err != nil {
		t.Fatalf("Execute must be fail-soft, returned err: %v", err)
	}
	if res.PushedFixup {
		t.Error("expected PushedFixup=false with nil Manager/Registry")
	}
	if !strings.Contains(res.Explanation, "no Manager or Registry bound") {
		t.Errorf("explanation should name the missing deps, got %q", res.Explanation)
	}
}

// TestIterateSpecReview_InputGuards asserts every input-guard branch
// surfaces a populated Explanation that names the missing field, so a
// misconfigured dispatcher is debuggable from the workflow result alone.
func TestIterateSpecReview_InputGuards(t *testing.T) {
	mgr, err := worktree.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	reg := driver.NewRegistry()
	act := NewIterateSpecReview(mgr, reg)

	cases := []struct {
		name    string
		mutate  func(*IterateSpecReviewInput)
		wantSub string
	}{
		{"PRNumber zero", func(i *IterateSpecReviewInput) { i.PRNumber = 0 }, "missing PRNumber"},
		{"PRBranch empty", func(i *IterateSpecReviewInput) { i.PRBranch = "" }, "missing PRNumber"},
		{"TargetRepo empty", func(i *IterateSpecReviewInput) { i.TargetRepo = "" }, "missing PRNumber"},
		{"Repo empty", func(i *IterateSpecReviewInput) { i.Repo = "" }, "missing PRNumber"},
		{"ReviewID zero", func(i *IterateSpecReviewInput) { i.ReviewID = 0 }, "missing ReviewID"},
		{"SpecDir empty", func(i *IterateSpecReviewInput) { i.SpecDir = "" }, "missing SpecDir"},
		{"DriverID empty", func(i *IterateSpecReviewInput) { i.DriverID = "" }, "missing DriverID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validSpecIterationInput()
			tc.mutate(&in)
			res, err := act.Execute(context.Background(), in)
			if err != nil {
				t.Fatalf("Execute must be fail-soft, returned err: %v", err)
			}
			if !strings.Contains(res.Explanation, tc.wantSub) {
				t.Errorf("explanation should contain %q, got %q", tc.wantSub, res.Explanation)
			}
			if res.PushedFixup {
				t.Errorf("guarded round should not report PushedFixup, got %+v", res)
			}
		})
	}
}

// TestIterateSpecReview_PlaceholderBodyAlwaysNilError asserts the T011
// contract: with valid wiring and inputs the placeholder body returns
// nil error and an Explanation that names the not-yet-implemented
// status, so future T012/T014/T016 work has a stable surface to extend.
func TestIterateSpecReview_PlaceholderBodyAlwaysNilError(t *testing.T) {
	mgr, err := worktree.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	reg := driver.NewRegistry()
	act := NewIterateSpecReview(mgr, reg)

	res, err := act.Execute(context.Background(), validSpecIterationInput())
	if err != nil {
		t.Fatalf("Execute must be fail-soft, returned err: %v", err)
	}
	if !strings.Contains(res.Explanation, "not yet implemented") {
		t.Errorf("placeholder explanation should name not-yet-implemented state, got %q", res.Explanation)
	}
	if res.PushedFixup {
		t.Errorf("placeholder must not report PushedFixup, got %+v", res)
	}
}
