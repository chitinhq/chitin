package ingest

import (
	"context"
	"strings"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

// Spec 079 T014 — the Temporal testsuite replay/determinism test for the
// operator-fed pipeline (US1 Independent Test; FR-001, FR-012; SC-001,
// SC-005). A known URL is fetched as a kernel-gated action, normalized,
// filtered, surfaced — and nothing in code or policy changes.
//
// Every side effect — the FetchAndRead activity, the SurfaceKnowledgeItem
// activity — is mocked so the test is hermetic and asserts on what the
// workflow did. The filter runs inline (it is pure) and is exercised for real.

// fetchOpts / surfaceOpts name the mock activities by the stable activity
// names the workflow dispatches to.
func fetchOpts() activity.RegisterOptions   { return activity.RegisterOptions{Name: FetchActivityName} }
func surfaceOpts() activity.RegisterOptions { return activity.RegisterOptions{Name: SurfaceActivityName} }

// goodArticleHTML is a substantial, on-topic article body — the filter keeps it.
const goodArticleHTML = "Durable execution decouples a workflow's logical progress from process " +
	"liveness. This article examines retry semantics, replay determinism, and activity idempotency " +
	"in depth, with worked examples of each. " +
	"Durable execution decouples a workflow's logical progress from process liveness. This article " +
	"examines retry semantics, replay determinism, and activity idempotency in depth."

var ingestTopic = FilterTopic{
	Name:     "durable execution",
	Keywords: []string{"durable", "execution", "retry", "replay", "determinism", "activity", "workflow"},
}

// TestIngestionWorkflow_OperatorFedItemFetchedFilteredSurfaced proves SC-001:
// an operator-fed URL is fetched (kernel-gated), normalized, filtered, and
// surfaced in the knowledge base in a single pipeline run.
func TestIngestionWorkflow_OperatorFedItemFetchedFilteredSurfaced(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	var gateConsulted, surfacedRef string
	var fetchTrust TrustMarker

	// Mock FetchAndRead: stand in for the kernel-gated fetch. It records the
	// URL the workflow asked to fetch (proving the fetch happened) and the
	// trust marker carried (proving the operator-seeded provenance flows).
	env.RegisterActivityWithOptions(
		func(_ context.Context, in FetchInput) (FetchResult, error) {
			gateConsulted = in.SourceRef
			fetchTrust = in.Trust
			return FetchResult{
				Fetched: true,
				Reason:  "fetched (mock kernel-gated)",
				Item: IngestItem{
					SourceRef: in.SourceRef,
					Title:     "A Rigorous Treatment of Durable Execution",
					Content:   goodArticleHTML,
					Medium:    in.Medium,
					Trust:     in.Trust,
				},
			}, nil
		},
		fetchOpts(),
	)
	// Mock SurfaceKnowledgeItem: record what reached the knowledge base.
	env.RegisterActivityWithOptions(
		func(_ context.Context, in SurfaceInput) (SurfaceResult, error) {
			surfacedRef = in.Item.SourceRef
			return SurfaceResult{Surfaced: true, SourceRef: in.Item.SourceRef}, nil
		},
		surfaceOpts(),
	)

	env.ExecuteWorkflow(IngestionWorkflow, IngestionInput{
		Feed:  OperatorFeed{URL: "https://example.com/durable-execution-post"},
		Topic: ingestTopic,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("ingestion workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("ingestion workflow errored: %v", err)
	}
	var res IngestionResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decoding ingestion result: %v", err)
	}

	if gateConsulted != "https://example.com/durable-execution-post" {
		t.Errorf("the fetch activity was not asked for the operator's URL: %q", gateConsulted)
	}
	if fetchTrust != TrustOperatorSeeded {
		t.Errorf("the fetched item must carry the operator-seeded marker, got %q", fetchTrust)
	}
	if res.Verdict.Disposition != DispositionKept {
		t.Fatalf("a high-signal operator-fed item should be kept, got %s (reason: %s)",
			res.Verdict.Disposition, res.Verdict.Reason)
	}
	if !res.Surfaced {
		t.Error("a kept item must be surfaced into the knowledge base (SC-001)")
	}
	if surfacedRef != "https://example.com/durable-execution-post" {
		t.Errorf("the surfaced item is not the operator's URL: %q", surfacedRef)
	}
}

// TestIngestionWorkflow_OperatorFedDropRecordsReason proves US1 acceptance
// scenario 4 / FR-007: an operator-fed item filtered out as low-signal records
// the drop and its reason — the operator can see why their pick did not
// survive — and the dropped item is NEVER surfaced (SC-005).
func TestIngestionWorkflow_OperatorFedDropRecordsReason(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	surfaceCalled := false

	// The fetch returns thin, off-topic junk — even with the operator seed,
	// the filter must drop it (FR-008).
	env.RegisterActivityWithOptions(
		func(_ context.Context, in FetchInput) (FetchResult, error) {
			return FetchResult{
				Fetched: true,
				Item: IngestItem{
					SourceRef: in.SourceRef,
					Content:   "buy now click here limited offer act fast win prizes today only sponsored content",
					Medium:    in.Medium,
					Trust:     in.Trust,
				},
			}, nil
		},
		fetchOpts(),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ SurfaceInput) (SurfaceResult, error) {
			surfaceCalled = true
			return SurfaceResult{Surfaced: true}, nil
		},
		surfaceOpts(),
	)

	env.ExecuteWorkflow(IngestionWorkflow, IngestionInput{
		Feed:  OperatorFeed{URL: "https://example.com/operator-junk"},
		Topic: ingestTopic,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow errored: %v", err)
	}
	var res IngestionResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decoding result: %v", err)
	}

	if res.Verdict.Disposition == DispositionKept {
		t.Fatal("a low-signal operator-fed item must not be kept — the seed must not bypass the filter (FR-008)")
	}
	if res.Verdict.Reason == "" {
		t.Error("the drop/hold must record a reason — the operator must see why (US1 scenario 4 / FR-007)")
	}
	if res.Surfaced {
		t.Error("a non-kept item must NOT be surfaced (SC-005)")
	}
	if surfaceCalled {
		t.Error("the surface activity must not run for a non-kept item (SC-005)")
	}
}

// TestIngestionWorkflow_DeniedFetchRecordedNotSurfaced proves FR-012: a fetch
// the kernel egress gate denies is recorded as a denied fetch — the run ends
// without a verdict and nothing is surfaced.
func TestIngestionWorkflow_DeniedFetchRecordedNotSurfaced(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	surfaceCalled := false
	env.RegisterActivityWithOptions(
		func(_ context.Context, in FetchInput) (FetchResult, error) {
			return FetchResult{
				Fetched: false,
				Denied:  true,
				Reason:  "egress to " + in.SourceRef + " denied by the kernel typed-egress policy",
			}, nil
		},
		fetchOpts(),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ SurfaceInput) (SurfaceResult, error) {
			surfaceCalled = true
			return SurfaceResult{}, nil
		},
		surfaceOpts(),
	)

	env.ExecuteWorkflow(IngestionWorkflow, IngestionInput{
		Feed: OperatorFeed{URL: "https://blocked.example.com/x"},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a denied fetch must not crash the workflow: %v", err)
	}
	var res IngestionResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decoding result: %v", err)
	}
	if !res.Denied {
		t.Error("a kernel-denied fetch must be recorded as Denied (FR-012)")
	}
	if res.Surfaced || surfaceCalled {
		t.Error("a denied fetch must surface nothing")
	}
	if !strings.Contains(res.FetchReason, "denied") {
		t.Errorf("the fetch reason must record the deny: %q", res.FetchReason)
	}
}

// TestIngestionWorkflow_InvalidFeedFailsFast proves a malformed operator
// submission fails fast — before any activity runs.
func TestIngestionWorkflow_InvalidFeedFailsFast(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	fetchCalled := false
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ FetchInput) (FetchResult, error) {
			fetchCalled = true
			return FetchResult{}, nil
		},
		fetchOpts(),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ SurfaceInput) (SurfaceResult, error) { return SurfaceResult{}, nil },
		surfaceOpts(),
	)

	env.ExecuteWorkflow(IngestionWorkflow, IngestionInput{
		Feed: OperatorFeed{URL: "not-a-url"},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil {
		t.Error("a malformed operator feed must fail the workflow")
	}
	if fetchCalled {
		t.Error("the fetch activity must not run for a malformed feed — fail fast")
	}
}

// TestIngestionWorkflow_Deterministic proves the workflow replays
// deterministically — the testsuite's replay checker runs as part of
// ExecuteWorkflow, and a repeated run yields the identical verdict. Combined
// with the filter's 100-run determinism test, this covers SC-004 for the
// workflow layer.
func TestIngestionWorkflow_Deterministic(t *testing.T) {
	run := func() IngestionResult {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()
		env.RegisterActivityWithOptions(
			func(_ context.Context, in FetchInput) (FetchResult, error) {
				return FetchResult{
					Fetched: true,
					Item: IngestItem{
						SourceRef: in.SourceRef, Title: "Durable Execution",
						Content: goodArticleHTML, Medium: in.Medium, Trust: in.Trust,
					},
				}, nil
			},
			fetchOpts(),
		)
		env.RegisterActivityWithOptions(
			func(_ context.Context, in SurfaceInput) (SurfaceResult, error) {
				return SurfaceResult{Surfaced: true, SourceRef: in.Item.SourceRef}, nil
			},
			surfaceOpts(),
		)
		env.ExecuteWorkflow(IngestionWorkflow, IngestionInput{
			Feed:  OperatorFeed{URL: "https://example.com/post"},
			Topic: ingestTopic,
		})
		if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
			t.Fatalf("run did not complete cleanly: %v", env.GetWorkflowError())
		}
		var res IngestionResult
		if err := env.GetWorkflowResult(&res); err != nil {
			t.Fatalf("decoding result: %v", err)
		}
		return res
	}

	first := run()
	for i := 0; i < 10; i++ {
		got := run()
		if got.Verdict.Disposition != first.Verdict.Disposition || got.Verdict.Rank != first.Verdict.Rank {
			t.Fatalf("run %d: verdict drift — %+v != %+v", i, got.Verdict, first.Verdict)
		}
		if got.Surfaced != first.Surfaced {
			t.Fatalf("run %d: surfaced drift", i)
		}
	}
}
