package loop

import (
	"context"
	"testing"
)

// TestIngest_StaticReader_FiltersToWindow proves a static reader serves only
// its own source's records that fall within the window.
func TestIngest_StaticReader_FiltersToWindow(t *testing.T) {
	reader := NewStaticTelemetryReader(SourceCI, []TelemetryRecord{
		rec("in", SourceCI, 15, "failure", "sig", "076"),        // inside (10,20].
		rec("out-late", SourceCI, 25, "failure", "sig", "076"),  // after the window.
		rec("wrong-src", SourcePR, 15, "failure", "sig", "076"), // dropped — not CI.
	})
	got, err := reader.Read(context.Background(),
		TelemetryWindow{Since: ts(10), Until: ts(20)})
	if err != nil {
		t.Fatalf("Read errored: %v", err)
	}
	if len(got) != 1 || got[0].ID != "in" {
		t.Errorf("Read returned %v, want exactly the in-window CI record", ids(got))
	}
}

// TestIngest_UnreachableLayer_EmptyContribution proves the spec-078 FR-002
// edge case: an unreachable telemetry layer yields an empty contribution and a
// NIL error — a dead source never fails the cycle.
func TestIngest_UnreachableLayer_EmptyContribution(t *testing.T) {
	reader := NewUnreachableTelemetryReader(SourceGovernance)
	got, err := reader.Read(context.Background(), TelemetryWindow{Until: ts(60)})
	if err != nil {
		t.Errorf("an unreachable layer must return a nil error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("an unreachable layer must contribute zero records, got %d", len(got))
	}
}

// TestIngestActivity_Execute proves the IngestTelemetry activity reads its
// source's contribution and reports it reachable.
func TestIngestActivity_Execute(t *testing.T) {
	act := NewIngestActivity([]TelemetryReader{
		NewStaticTelemetryReader(SourceCI, []TelemetryRecord{
			rec("ci-1", SourceCI, 5, "failure", "sig", "076"),
		}),
	})
	res, err := act.Execute(context.Background(), IngestInput{
		Source: SourceCI,
		Window: TelemetryWindow{Since: ts(0), Until: ts(60)},
	})
	if err != nil {
		t.Fatalf("Execute errored: %v", err)
	}
	if !res.Reachable {
		t.Error("a bound, readable source must report Reachable")
	}
	if len(res.Records) != 1 || res.Records[0].ID != "ci-1" {
		t.Errorf("Execute returned %v, want the one CI record", ids(res.Records))
	}
}

// TestIngestActivity_UnboundSource proves a source with no bound reader yields
// an empty contribution and a nil error — never a cycle-failing error
// (spec 078 FR-002 edge case).
func TestIngestActivity_UnboundSource(t *testing.T) {
	act := NewIngestActivity(nil) // no readers at all.
	res, err := act.Execute(context.Background(), IngestInput{
		Source: SourceBench,
		Window: TelemetryWindow{Until: ts(60)},
	})
	if err != nil {
		t.Errorf("an unbound source must not error, got %v", err)
	}
	if res.Reachable || len(res.Records) != 0 {
		t.Errorf("an unbound source must be an empty unreachable contribution; got %+v", res)
	}
}
