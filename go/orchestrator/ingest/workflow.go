package ingest

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// The durable ingestion pipeline workflow (US1 T011; FR-001, FR-017). The
// pipeline runs as a Temporal workflow inside the spec-070 orchestrator
// module: an operator-fed item flows fetch → read → filter → surface, each
// step individually inspectable in Temporal history.
//
// DETERMINISM (spec 079 plan.md Constraints; Temporal's rule): IngestionWorkflow
// is a Temporal workflow and is strictly deterministic. It reads NO wall clock
// (the fetch activity stamps fetch time; the workflow never calls time.Now),
// performs NO I/O directly. The two side-effecting steps — the kernel-gated
// fetch and the knowledge-base projection — are ACTIVITIES. The signal/noise
// filter, by contrast, runs INLINE in the workflow: it is a pure,
// deterministic function (no clock, no randomness, no map iteration — see
// filter.go), so running it in workflow code is replay-safe and keeps it at
// zero frontier-token cost (FR-017, SC-008: the filter is a deterministic
// stage, not a frontier agent).
//
// SCOPE — this is the US1 (P1) operator-fed slice. The workflow drives ONE
// operator-fed item end to end. US2's broad-net gathering (many candidates,
// dedup against the knowledge base, bounded batches) is a documented TODO —
// see gather.go and the TODO at the foot of this file.

// Activity timeouts for the pipeline's two side-effecting legs. The fetch is
// network I/O against an arbitrary external source; the surface is a local
// knowledge-base write. Both are short — ingestion is low-throughput (spec 079
// plan.md Performance Goals).
const (
	fetchActivityTimeout   = 30 * time.Second
	surfaceActivityTimeout = 15 * time.Second
)

// IngestionInput is the typed input to IngestionWorkflow — one operator-fed
// source plus the topic frame the filter ranks it against (US1).
type IngestionInput struct {
	// Feed is the operator's submission — a specific URL/article/video
	// (FR-002). The workflow validates it, fetches it, and routes it through
	// the pipeline carrying the operator-seeded trust marker.
	Feed OperatorFeed `json:"feed"`
	// Topic is the relevance frame the filter scores the item against. For
	// the operator-fed path it is typically empty — the operator's
	// submission IS the relevance signal (see relevanceScore in filter.go).
	Topic FilterTopic `json:"topic"`
}

// IngestionResult is the typed output of IngestionWorkflow — the outcome of
// one pipeline run. It records every stage so a run is fully auditable: the
// fetch decision, the filter verdict, and whether the item was surfaced.
type IngestionResult struct {
	// SourceRef is the source the run processed.
	SourceRef string `json:"source_ref"`
	// Fetched is true iff the source was fetched and read. False covers both
	// a kernel egress deny and a failed fetch (FR-012, FR-015).
	Fetched bool `json:"fetched"`
	// Denied is true when the fetch was DENIED by the kernel egress gate
	// (FR-012) — distinct from a fetch that was attempted and failed.
	Denied bool `json:"denied"`
	// Verdict is the filter's per-item outcome — kept / dropped / held. It is
	// the empty Verdict only when the item was never fetched (no content to
	// filter); FetchReason then explains why.
	Verdict Verdict `json:"verdict"`
	// Surfaced is true iff a kept item reached the knowledge base. A dropped
	// or held item is never surfaced (SC-005) — Surfaced is then false and
	// the Verdict carries the recorded reason.
	Surfaced bool `json:"surfaced"`
	// FetchReason is the fetch stage's human-readable account — the egress
	// decision or the failure cause. Always populated.
	FetchReason string `json:"fetch_reason"`
}

// IngestionWorkflow is the durable operator-fed ingestion pipeline (US1; FR-001,
// FR-017). It drives one operator-fed source through fetch → read → filter →
// surface.
//
// The shape is a straight line because US1 is one item; every step is a
// distinct, inspectable workflow action:
//
//  1. Validate the operator feed — a malformed URL fails fast, before any
//     activity runs.
//  2. FetchAndRead activity — kernel-gated egress (FR-012), content read into
//     a Normalized IngestItem (FR-004). A denied or failed fetch settles the
//     run without a verdict; it is NOT a workflow error (FR-015).
//  3. Filter (inline, pure, deterministic) — every item passes the
//     signal/noise filter (FR-005); an operator-seeded marker raises trust
//     but never bypasses it (FR-008). The verdict is kept / dropped / held.
//  4. SurfaceKnowledgeItem activity — ONLY a kept verdict is surfaced into
//     the knowledge base (SC-005). A dropped or held verdict records its
//     reason and stops here — the operator can see why a pick did not
//     survive (US1 acceptance scenario 4, FR-007/FR-010).
//
// Nothing in this workflow changes code, policy, or configuration (FR-011):
// its only side effects are a kernel-gated fetch and a knowledge-base write.
func IngestionWorkflow(ctx workflow.Context, in IngestionInput) (IngestionResult, error) {
	logger := workflow.GetLogger(ctx)

	// --- 1. validate the operator feed -------------------------------------
	// Construction is pure (no clock, no I/O) — safe in workflow code. A
	// malformed URL is a permanent, non-retryable input error.
	stub, err := NewOperatorItem(in.Feed)
	if err != nil {
		return IngestionResult{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("ingestion: invalid operator feed: %v", err), "InvalidOperatorFeed", err)
	}

	result := IngestionResult{SourceRef: stub.SourceRef}

	// --- 2. fetch + read (kernel-gated egress) -----------------------------
	fetchCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: fetchActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			// A failed fetch is recorded as a result, not retried blindly; a
			// genuine transport fault still gets a couple of attempts.
			MaximumAttempts: 2,
		},
	})
	var fetched FetchResult
	fetchErr := workflow.ExecuteActivity(fetchCtx, FetchActivityName, FetchInput{
		SourceRef: stub.SourceRef,
		Medium:    stub.Medium,
		Trust:     stub.Trust,
	}).Get(ctx, &fetched)
	if fetchErr != nil {
		// A genuine activity fault (e.g. the egress gate could not be
		// evaluated). Settle the run failed-to-fetch; do not crash — the
		// pipeline records the failure (FR-015).
		logger.Error("ingestion: fetch activity faulted", "source", stub.SourceRef, "err", fetchErr)
		result.FetchReason = fmt.Sprintf("fetch activity faulted: %v", fetchErr)
		return result, nil
	}

	result.Fetched = fetched.Fetched
	result.Denied = fetched.Denied
	result.FetchReason = fetched.Reason
	if !fetched.Fetched {
		// A kernel deny (FR-012) or a failed fetch (FR-015). No content to
		// filter — the run ends here, recorded. Not a workflow error.
		logger.Info("ingestion: source not fetched", "source", stub.SourceRef,
			"denied", fetched.Denied, "reason", fetched.Reason)
		return result, nil
	}

	// --- 3. the signal/noise filter (inline, pure, deterministic) ----------
	// FR-005: every item passes the filter; there is no path around it.
	// FR-008: the operator-seeded marker on fetched.Item raises trust but
	// does not bypass scoring. Running the filter inline is replay-safe — it
	// is a pure function of the item and the topic (filter.go).
	verdict := NewFilter().Evaluate(fetched.Item, in.Topic)
	result.Verdict = verdict
	logger.Info("ingestion: filter verdict", "source", stub.SourceRef,
		"disposition", verdict.Disposition.String(), "rank", verdict.Rank)

	if verdict.Disposition != DispositionKept {
		// Dropped or held — the verdict's recorded reason stands as the
		// audit trail (FR-007 / FR-010). The item does NOT reach the
		// knowledge base (SC-005). The operator can see why their pick did
		// not survive (US1 acceptance scenario 4).
		return result, nil
	}

	// --- 4. surface the kept item into the knowledge base ------------------
	// Only a kept verdict reaches here (SC-005). NewKnowledgeItem re-checks
	// that invariant — a defense-in-depth guard at the boundary.
	knowledge, err := NewKnowledgeItem(fetched.Item, verdict)
	if err != nil {
		// A kept verdict that cannot become a KnowledgeItem is a pipeline
		// bug, not an operator error — surface it as a workflow error so it
		// is loud, never a silent non-surface.
		return result, temporal.NewApplicationError(
			fmt.Sprintf("ingestion: kept verdict for %q could not be surfaced: %v", stub.SourceRef, err),
			"SurfaceBug")
	}

	surfaceCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: surfaceActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3, // the surface activity is idempotent on SourceRef.
		},
	})
	var surfaced SurfaceResult
	if err := workflow.ExecuteActivity(surfaceCtx, SurfaceActivityName, SurfaceInput{
		Item: knowledge,
	}).Get(ctx, &surfaced); err != nil {
		logger.Error("ingestion: surface activity faulted", "source", stub.SourceRef, "err", err)
		return result, fmt.Errorf("ingestion: surfacing %q: %w", stub.SourceRef, err)
	}
	result.Surfaced = surfaced.Surfaced
	logger.Info("ingestion: item surfaced into the knowledge base", "source", stub.SourceRef)
	return result, nil
}

// RegisterDeps are the runtime dependencies the ingestion pipeline's
// activities need bound at worker-host startup. They are constructed in main
// once the chitin kernel egress gate and the knowledge base exist, then
// handed to Register.
type RegisterDeps struct {
	// Egress is the chitin kernel's typed-egress / trust-policy gate every
	// fetch passes (FR-012). A nil Egress falls back to the development
	// allow-all gate — production MUST bind the real kernel gate (see the
	// TODO on allowAllGate in fetch.go).
	Egress EgressGate
	// HTTP is the HTTP client the fetch activity uses. Nil gets a default
	// client with the fetch timeout.
	HTTP HTTPDoer
	// KnowledgeBase is the sink kept items are surfaced into (FR-011). A nil
	// KnowledgeBase falls back to the logging projector (see the TODO on
	// logKnowledgeBase in knowledge_base.go).
	KnowledgeBase KnowledgeBase
}

// Register wires the ingestion pipeline — the workflow and its activities —
// into the orchestrator's Temporal worker host. It is the ingest package's
// half of spec 079: main constructs RegisterDeps (kernel egress gate,
// knowledge-base sink) and calls Register alongside the orchestrator's
// existing workflows.Register / activities.Register.
//
// This slice deliberately does NOT modify cmd/chitin-orchestrator/main.go —
// Register is the single wiring seam. Adding the pipeline to a running
// orchestrator is one call:
//
//	ingest.Register(w, ingest.RegisterDeps{
//	    Egress:        kernelEgressGate,
//	    KnowledgeBase: knowledgeBaseSink,
//	})
//
// The activities are registered under stable string names (FetchActivityName,
// SurfaceActivityName) so the workflow dispatches to them by name — the same
// convention activities.RegisterSchedulerActivities follows.
func Register(w worker.Worker, deps RegisterDeps) {
	w.RegisterWorkflow(IngestionWorkflow)

	fetch := NewFetchActivity(deps.Egress, deps.HTTP)
	w.RegisterActivityWithOptions(fetch.Execute, activity.RegisterOptions{Name: fetch.ActivityName()})

	surface := NewSurfaceActivity(deps.KnowledgeBase)
	w.RegisterActivityWithOptions(surface.Execute, activity.RegisterOptions{Name: surface.ActivityName()})
}

// TODO(spec 079 US2 / T015–T020): the broad-net gathering workflow. US2 adds
// autonomous breadth on this proven core — a gathering activity invokes a
// tool-equipped agent via the spec-075 driver contract on a named topic
// (gather.go), and EVERY candidate it produces is routed through the IDENTICAL
// fetch → read → filter path IngestionWorkflow drives for an operator-fed
// item, carrying TrustGathered. The gathering workflow additionally:
//   - deduplicates each candidate against the knowledge base before fetching
//     (dedup.go, FR-014);
//   - records a failed fetch per-item without failing the run (FR-015 — the
//     FetchActivity already returns this as a result, not an error);
//   - bounds the gathered batch and queues the remainder (FR-016).
//
// TODO(spec 079 US3 / T021–T025): the real deterministic signal/noise filter
// replaces the P1 heuristic in filter.go — credibility/relevance/value with
// the optional spec-075 local-LLM classifier and a deterministic-heuristic
// fallback. IngestionWorkflow already calls NewFilter().Evaluate, so the
// filter swap is internal to filter.go — no workflow change.
//
// TODO(spec 079 / T028): emit per-run telemetry to the Chitin Telemetry layer
// (FR-018) — items fetched, filtered kept/dropped with reasons — via a
// telemetry activity, mirroring activities.TickTelemetry. Deferred from this
// slice to keep the US1 surface minimal; the IngestionResult already carries
// every field such telemetry would project.
