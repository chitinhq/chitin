package ingest

import (
	"fmt"
	"sort"
	"strings"
)

// The signal/noise filter — the crux of spec 079: "the hardest part is
// disseminating good information from bad." Every item, fed or gathered,
// passes this filter; there is NO path around it (FR-005). The filter ranks
// each item for credibility, relevance, and value (FR-006), keeps the
// high-signal items, DROPS the low-signal ones with a recorded reason
// (FR-007), and HOLDS the ones it cannot confidently assess for operator
// review (FR-010). Noise never passes silently.
//
// Determinism (FR-009, the spec's crux SC-004): the filter is a PURE function
// of an item's content and its trust marker. It reads no clock, no
// randomness, no map iteration order — the same item always yields the same
// Verdict, and the same batch always yields the same ranking and the same
// keep/drop decisions. This file has no Temporal import precisely so that
// determinism can be proven by `go test` alone.
//
// SCOPE — P1 (US1): this is the deterministic-HEURISTIC filter. Spec 079
// FR-006 allows the heuristics to be combined with "a small classifier
// model"; that classifier (US3, T024 — plugged in via the spec-075 local-LLM
// driver, with a deterministic-heuristic fallback) is a documented TODO
// below. For P1 the heuristic IS the filter — and it already enforces the
// non-negotiable spec invariants: keep/drop/hold are exhaustive, every drop
// carries a reason, and an operator seed raises trust but never bypasses
// scoring.
//
// TODO(spec 079 US3 / T021–T024): replace/augment this heuristic with the
// real credibility/relevance/value model. Plug the optional small classifier
// in via the spec-075 local-LLM driver; when the classifier is unavailable
// the filter MUST fall back to exactly these heuristics and mark affected
// items held-for-operator-review — never wave a batch through unfiltered
// (FR-006, FR-010, edge case "the filter's classifier model is unavailable").

// keepThreshold and holdFloor partition the [0,1] rank line into three bands.
// An item ranking >= keepThreshold is kept; an item ranking < holdFloor is
// dropped; an item between the two is too uncertain to call and is held for
// operator review (FR-010). The bands are fixed constants — moving them is a
// deliberate, reviewable change, never a runtime decision, so the filter
// stays deterministic (FR-009).
const (
	keepThreshold = 0.60
	holdFloor     = 0.40
)

// minReadableContentLen is the shortest Content the filter will score on its
// merits. Below this the reading is too thin to assess credibility, relevance
// or value — the item is HELD for operator review rather than guessed at
// (FR-010). This is the "cannot confidently assess" boundary.
const minReadableContentLen = 80

// FilterTopic is the relevance frame for a filter run — the subject the
// pipeline is gathering knowledge for. For an operator-fed item (US1) the
// topic may be empty: the operator's submission IS the relevance signal, so
// an empty topic means relevance is judged structurally, not against
// keywords. US2's broad-net gathering always names a topic (a documented TODO
// in gather.go).
type FilterTopic struct {
	// Name is the human-readable topic name, recorded in verdicts for audit.
	Name string `json:"name"`
	// Keywords are the lower-cased terms an item's content is matched
	// against for the relevance assessment. Empty keywords => relevance is
	// scored structurally (operator-fed mode).
	Keywords []string `json:"keywords"`
}

// Filter is the signal/noise filter. It is a pure, dependency-free value: a
// zero Filter is usable and runs the deterministic heuristic. It carries no
// Temporal types and no I/O — every method is a pure function of its inputs.
type Filter struct{}

// NewFilter returns the deterministic heuristic filter. It takes no
// dependencies — the P1 filter is pure heuristics. When the spec-075 local-LLM
// classifier is added (US3, T024), it will be an OPTIONAL field here with a
// fallback to these same heuristics.
func NewFilter() Filter { return Filter{} }

// Evaluate ranks one item and returns its Verdict — the single-item filter
// step (FR-005, FR-006, FR-007, FR-010). It is the heart of the filter and is
// PURE and DETERMINISTIC: the same item and topic always yield the same
// verdict.
//
// The operator-seeded trust marker raises the item's credibility by a fixed
// boost (FR-008) — but the boost is bounded so it CANNOT, alone, carry a
// genuinely low-signal item over the keep bar: an operator-fed item is still
// scored, and may still be dropped or held (edge case "an operator-fed item
// is itself low-signal", US1 acceptance scenario 4).
//
// Prompt-injection containment (FR-013): Content is treated strictly as data.
// The filter MATCHES against it (keyword counts, length, structure); it never
// interprets it as instructions. There is no path by which embedded text
// changes what the filter does — only how it scores.
func (Filter) Evaluate(it IngestItem, topic FilterTopic) Verdict {
	// An item with no reading cannot be filtered — this is a pipeline bug
	// (the fetch+read stage must run first), surfaced as a held verdict so
	// it lands in front of the operator rather than vanishing.
	if !it.HasContent() {
		return HeldVerdict(it, 0, 0, 0, 0,
			"item reached the filter with no read content — held for operator review (fetch/read stage produced an empty item)")
	}

	content := it.Content
	contentLen := len([]rune(strings.TrimSpace(content)))

	// Boundary: a reading too thin to assess. Not silently kept, not silently
	// dropped — held for operator review (FR-010).
	if contentLen < minReadableContentLen {
		return HeldVerdict(it, 0, 0, 0, 0, fmt.Sprintf(
			"reading is only %d characters — too thin to assess credibility, relevance, or value; held for operator review",
			contentLen))
	}

	cred := credibilityScore(it)
	rel := relevanceScore(it, topic)
	val := valueScore(it)

	// Rank combines the three assessments. The weights are fixed constants —
	// credibility weighted highest because the spec's thesis is separating
	// CREDIBLE information from noise. The combination is a plain weighted
	// mean: deterministic, decomposable, auditable.
	rank := 0.45*cred + 0.30*rel + 0.25*val
	rank = clamp01(rank)

	switch {
	case rank >= keepThreshold:
		return KeptVerdict(it, rank, cred, rel, val, fmt.Sprintf(
			"kept: rank %.2f >= keep threshold %.2f (credibility %.2f, relevance %.2f, value %.2f)",
			rank, keepThreshold, cred, rel, val))
	case rank < holdFloor:
		return DroppedVerdict(it, rank, cred, rel, val, dropReason(rank, cred, rel, val, it.Trust))
	default:
		return HeldVerdict(it, rank, cred, rel, val, fmt.Sprintf(
			"uncertain: rank %.2f sits between the drop floor %.2f and the keep threshold %.2f — held for operator review (credibility %.2f, relevance %.2f, value %.2f)",
			rank, holdFloor, keepThreshold, cred, rel, val))
	}
}

// FilterBatch evaluates a whole batch and returns the verdicts in a stable,
// deterministic order (FR-009, SC-004) — sorted by rank descending, with the
// SourceRef as the final tie-breaker so two items with an identical rank
// always order the same way regardless of input order. The batch keep/drop
// decisions are exactly the per-item Evaluate decisions; FilterBatch only
// imposes a deterministic ranking over them.
func (f Filter) FilterBatch(items []IngestItem, topic FilterTopic) []Verdict {
	verdicts := make([]Verdict, 0, len(items))
	for _, it := range items {
		verdicts = append(verdicts, f.Evaluate(it, topic))
	}
	sort.SliceStable(verdicts, func(i, j int) bool {
		if verdicts[i].Rank != verdicts[j].Rank {
			return verdicts[i].Rank > verdicts[j].Rank // higher signal first
		}
		// Final tie-breaker: a stable id. A sort without a named tie-breaker
		// is not sorted — two equal-rank items must order identically every
		// run (FR-009).
		return verdicts[i].SourceRef < verdicts[j].SourceRef
	})
	return verdicts
}

// credibilityScore assesses an item's credibility, 0..1 (FR-006). It is a
// deterministic heuristic over structural signals plus the operator-seed
// trust boost (FR-008). The boost is bounded (operatorSeedBoost) so a vouch
// raises trust but cannot drag a genuinely weak item over the keep bar.
func credibilityScore(it IngestItem) float64 {
	// Structural baseline: a titled item with substantial content reads as
	// more credible than an untitled fragment.
	score := 0.35
	if strings.TrimSpace(it.Title) != "" {
		score += 0.10
	}
	if len([]rune(it.Content)) >= 400 {
		score += 0.10
	}
	// A truncated reading is a weaker credibility signal — the assessment
	// stands on a partial source.
	if it.Truncated {
		score -= 0.05
	}
	// The operator-seed trust boost (FR-008). A gathered item gets nothing.
	score += it.Trust.CredibilityBoost()
	return clamp01(score)
}

// relevanceScore assesses an item's relevance to the filter topic, 0..1
// (FR-006). With keywords, it is the fraction of distinct topic keywords the
// content mentions. With no keywords (operator-fed mode), relevance is scored
// structurally — the operator's submission IS the relevance signal, so a
// readable operator-fed item gets a solid structural relevance baseline.
func relevanceScore(it IngestItem, topic FilterTopic) float64 {
	if len(topic.Keywords) == 0 {
		// Operator-fed mode: no topic to match against. A readable item the
		// operator hand-picked is presumed on-topic; structural baseline.
		if it.Trust == TrustOperatorSeeded {
			return 0.70
		}
		// A gathered item with no topic has no relevance frame at all — a
		// weak, uncertain signal.
		return 0.45
	}
	lowerContent := strings.ToLower(it.Content + " " + it.Title)
	hits := 0
	for _, kw := range topic.Keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw == "" {
			continue
		}
		if strings.Contains(lowerContent, kw) {
			hits++
		}
	}
	if len(topic.Keywords) == 0 {
		return 0.45
	}
	return clamp01(float64(hits) / float64(len(topic.Keywords)))
}

// valueScore assesses an item's value, 0..1 (FR-006) — a deterministic
// heuristic over content depth. A substantial reading carries more value than
// a thin one; a truncated reading is discounted because the value estimate
// rests on a partial source.
func valueScore(it IngestItem) float64 {
	runes := len([]rune(strings.TrimSpace(it.Content)))
	// A simple monotone depth curve: longer readings score higher, saturating.
	var score float64
	switch {
	case runes >= 1200:
		score = 0.85
	case runes >= 600:
		score = 0.70
	case runes >= 300:
		score = 0.55
	default:
		score = 0.40
	}
	if it.Truncated {
		score -= 0.10
	}
	return clamp01(score)
}

// dropReason builds the auditable reason a low-signal item was dropped
// (FR-007). It names the WEAKEST dimension so the operator sees exactly why a
// pick did not survive (US1 acceptance scenario 4) — and, for an
// operator-seeded item, explicitly records that the seed raised trust but did
// not, and must not, bypass the filter (FR-008).
func dropReason(rank, cred, rel, val float64, trust TrustMarker) string {
	dim, score := "credibility", cred
	if rel < score {
		dim, score = "relevance", rel
	}
	if val < score {
		dim, score = "value", val
	}
	reason := fmt.Sprintf(
		"dropped as low-signal: rank %.2f < drop floor %.2f; weakest dimension is %s at %.2f (credibility %.2f, relevance %.2f, value %.2f)",
		rank, holdFloor, dim, score, cred, rel, val)
	if trust == TrustOperatorSeeded {
		reason += " — note: this was an operator-seeded item; the seed raised its credibility but did not let it bypass the filter (FR-008)"
	}
	return reason
}

// clamp01 bounds x to the [0,1] interval. Scores are probabilities-of-signal;
// the heuristic's additions and subtractions could in principle escape the
// range, and a rank outside [0,1] would make the band thresholds meaningless.
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
