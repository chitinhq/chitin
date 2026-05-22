package ingest

import (
	"strings"
	"testing"
)

// Spec 079 filter tests — the crux of the spec. These cover the P1 slice's
// filter invariants: every item passes the filter (FR-005), an operator seed
// raises trust but never bypasses it (FR-008), a low-signal item is dropped
// with a recorded reason (FR-007), an item too thin to assess is held for
// operator review (FR-010), and the filter is DETERMINISTIC (FR-009, SC-004).
//
// SCOPE — the P1 filter is the deterministic heuristic; the real
// credibility/relevance/value model with the optional classifier (US3,
// T021–T025) is a documented TODO in filter.go. These tests already pin the
// non-negotiable invariants the US3 filter must also satisfy.

// highSignalItem is a substantial, titled, operator-seeded, long-form item —
// the kind the filter should keep.
func highSignalItem(ref string) IngestItem {
	return IngestItem{
		SourceRef: ref,
		Title:     "A Rigorous Treatment of Durable Execution Patterns",
		Content: strings.Repeat(
			"Durable execution decouples a workflow's logical progress from process liveness. "+
				"This article examines retry semantics, replay determinism, and activity idempotency. ", 12),
		Medium: MediumArticle,
		Trust:  TrustOperatorSeeded,
	}
}

// lowSignalItem is a thin, untitled, gathered, off-topic fragment — the kind
// the filter should drop.
func lowSignalItem(ref string) IngestItem {
	return IngestItem{
		SourceRef: ref,
		Content:   "buy now click here limited offer act fast win prizes today only sponsored content",
		Medium:    MediumWebPage,
		Trust:     TrustGathered,
	}
}

var techTopic = FilterTopic{
	Name:     "durable execution",
	Keywords: []string{"durable", "execution", "retry", "replay", "determinism", "activity", "workflow"},
}

// TestFilter_KeepsHighSignal proves a substantial, on-topic item is kept with
// a rank and a keep rationale (US3 acceptance scenario 1).
func TestFilter_KeepsHighSignal(t *testing.T) {
	v := NewFilter().Evaluate(highSignalItem("https://example.com/good"), techTopic)
	if v.Disposition != DispositionKept {
		t.Fatalf("a high-signal item should be kept, got %s (reason: %s)", v.Disposition, v.Reason)
	}
	if v.Rank < keepThreshold {
		t.Errorf("a kept item's rank %.2f should be >= keep threshold %.2f", v.Rank, keepThreshold)
	}
	if v.Reason == "" {
		t.Error("a kept verdict should carry a keep rationale")
	}
}

// TestFilter_DropsLowSignalWithReason proves FR-007: a low-signal item is
// dropped, with an auditable recorded reason naming the weakest dimension.
func TestFilter_DropsLowSignalWithReason(t *testing.T) {
	v := NewFilter().Evaluate(lowSignalItem("https://spam.example.com/junk"), techTopic)
	if v.Disposition != DispositionDropped {
		t.Fatalf("a low-signal item should be dropped, got %s", v.Disposition)
	}
	if v.Reason == "" {
		t.Error("a dropped verdict MUST carry a recorded reason (FR-007)")
	}
	if !strings.Contains(v.Reason, "low-signal") {
		t.Errorf("the drop reason should explain the low-signal verdict: %q", v.Reason)
	}
}

// TestFilter_HoldsThinReading proves FR-010: an item too thin to assess is
// held for operator review — never silently kept, never silently dropped.
func TestFilter_HoldsThinReading(t *testing.T) {
	thin := IngestItem{
		SourceRef: "https://example.com/stub",
		Content:   "short.",
		Trust:     TrustGathered,
	}
	v := NewFilter().Evaluate(thin, techTopic)
	if v.Disposition != DispositionHeld {
		t.Fatalf("a too-thin reading should be held for operator review, got %s", v.Disposition)
	}
	if v.Reason == "" {
		t.Error("a held verdict must explain the uncertainty (FR-010)")
	}
}

// TestFilter_HoldsItemWithNoContent proves an item that reaches the filter
// with no read content is held — surfaced to the operator, never vanished.
func TestFilter_HoldsItemWithNoContent(t *testing.T) {
	v := NewFilter().Evaluate(IngestItem{SourceRef: "https://example.com/empty"}, techTopic)
	if v.Disposition != DispositionHeld {
		t.Errorf("an item with no content must be held, got %s", v.Disposition)
	}
}

// TestFilter_OperatorSeedDoesNotBypass proves FR-008 and the spec edge case
// "an operator-fed item is itself low-signal": an operator-seeded marker on a
// genuinely low-signal item raises its credibility but does NOT carry it over
// the keep bar — it is still dropped (or held), with the seed recorded.
func TestFilter_OperatorSeedDoesNotBypass(t *testing.T) {
	// The SAME thin junk content, but submitted as an operator seed.
	junk := lowSignalItem("https://example.com/operator-junk")
	junk.Trust = TrustOperatorSeeded

	v := NewFilter().Evaluate(junk, techTopic)
	if v.Disposition == DispositionKept {
		t.Fatalf("an operator-seeded LOW-signal item must NOT be kept — the seed must not bypass the filter (FR-008)")
	}
	// Whichever non-keep disposition, the verdict records the operator seed.
	if v.Trust != TrustOperatorSeeded {
		t.Error("the verdict must record that this was an operator-seeded item")
	}
	if v.Disposition == DispositionDropped && !strings.Contains(v.Reason, "operator-seeded") {
		t.Errorf("a dropped operator-seeded item's reason should note the seed did not bypass the filter: %q", v.Reason)
	}
}

// TestFilter_DeterministicAcross100Runs proves FR-009 / SC-004 — the spec's
// crux: a batch mixing high- and low-signal items yields IDENTICAL ranking
// and IDENTICAL keep/drop decisions across 100 repeated runs.
func TestFilter_DeterministicAcross100Runs(t *testing.T) {
	batch := []IngestItem{
		highSignalItem("https://example.com/a"),
		lowSignalItem("https://example.com/b"),
		highSignalItem("https://example.com/c"),
		lowSignalItem("https://example.com/d"),
		{SourceRef: "https://example.com/e", Content: "short.", Trust: TrustGathered}, // held
	}
	f := NewFilter()

	first := f.FilterBatch(batch, techTopic)
	for run := 0; run < 100; run++ {
		got := f.FilterBatch(batch, techTopic)
		if len(got) != len(first) {
			t.Fatalf("run %d: verdict count %d != %d", run, len(got), len(first))
		}
		for i := range got {
			if got[i].SourceRef != first[i].SourceRef {
				t.Fatalf("run %d position %d: order drift — %q != %q (FR-009)",
					run, i, got[i].SourceRef, first[i].SourceRef)
			}
			if got[i].Disposition != first[i].Disposition {
				t.Fatalf("run %d: disposition drift for %q — %s != %s (SC-004)",
					run, got[i].SourceRef, got[i].Disposition, first[i].Disposition)
			}
			if got[i].Rank != first[i].Rank {
				t.Fatalf("run %d: rank drift for %q — %v != %v (SC-004)",
					run, got[i].SourceRef, got[i].Rank, first[i].Rank)
			}
		}
	}
}

// TestFilter_BatchKeepsAllHighDropsAllLow proves SC-003: on a mixed batch the
// filter keeps 100% of the high-signal items and drops 100% of the low-signal
// ones, each drop carrying a reason.
func TestFilter_BatchKeepsAllHighDropsAllLow(t *testing.T) {
	high := []string{"https://example.com/h1", "https://example.com/h2", "https://example.com/h3"}
	low := []string{"https://example.com/l1", "https://example.com/l2"}

	var batch []IngestItem
	for _, r := range high {
		batch = append(batch, highSignalItem(r))
	}
	for _, r := range low {
		batch = append(batch, lowSignalItem(r))
	}

	verdicts := NewFilter().FilterBatch(batch, techTopic)
	byRef := map[string]Verdict{}
	for _, v := range verdicts {
		byRef[v.SourceRef] = v
	}
	for _, r := range high {
		if byRef[r].Disposition != DispositionKept {
			t.Errorf("high-signal %q should be kept, got %s", r, byRef[r].Disposition)
		}
	}
	for _, r := range low {
		v := byRef[r]
		if v.Disposition != DispositionDropped {
			t.Errorf("low-signal %q should be dropped, got %s", r, v.Disposition)
		}
		if v.Reason == "" {
			t.Errorf("dropped %q must carry a reason (FR-007)", r)
		}
	}
}

// TestFilter_BatchOrderIndependent proves the batch ranking does not depend
// on input order — the same set of items in a different order yields the same
// sorted verdicts (FR-009, the named tie-breaker holds).
func TestFilter_BatchOrderIndependent(t *testing.T) {
	a := highSignalItem("https://example.com/a")
	b := highSignalItem("https://example.com/b")
	f := NewFilter()

	forward := f.FilterBatch([]IngestItem{a, b}, techTopic)
	reverse := f.FilterBatch([]IngestItem{b, a}, techTopic)
	if len(forward) != len(reverse) {
		t.Fatal("verdict count differs between orderings")
	}
	for i := range forward {
		if forward[i].SourceRef != reverse[i].SourceRef {
			t.Errorf("position %d: ranking depends on input order — %q vs %q",
				i, forward[i].SourceRef, reverse[i].SourceRef)
		}
	}
}
