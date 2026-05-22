package loop

import (
	"context"
	"fmt"
	"sort"
)

// TelemetryReader is the read surface for one telemetry source — the seam
// between the loop and the Chitin Telemetry layer (spec 078 FR-002, plan:
// "Telemetry is read, not owned"). One reader per layer: governance/chitin
// chain, run history, CI, bench, PR, agent telemetry.
//
// It is an INTERFACE so the loop does not hard-depend on a concrete telemetry
// transport — the gov-decisions chain, an OTLP query API, a SQLite read-model
// each plug in behind it. A reader MUST be READ-ONLY: Read returns records, it
// never writes. The loop is a consumer of telemetry (spec 078 Out of Scope).
//
// CRITICAL (spec 078 FR-002, edge case: unreachable telemetry layer): a reader
// for a missing or unreachable layer MUST return an empty slice and a nil
// error — never a hard error. An unreachable layer is an EMPTY CONTRIBUTION,
// so one dead source can never fail or block the whole cycle.
type TelemetryReader interface {
	// Source is the layer this reader reads.
	Source() TelemetrySource
	// Read returns the telemetry records for this source within the window's
	// (Since, Until] bounds. An unreachable layer returns (nil, nil) — an
	// empty contribution, never an error (spec 078 FR-002 edge case).
	Read(ctx context.Context, window TelemetryWindow) ([]TelemetryRecord, error)
}

// StaticTelemetryReader is a fixed-content TelemetryReader — it serves a
// pre-supplied slice of records, filtered to the requested window. It is the
// reader US1 ships with for the on-demand cycle and for tests: a cycle is fed
// a fixed telemetry window (spec 078 US1 Independent Test), and a static
// reader is exactly that fixed window made into the reader contract.
//
// TODO(spec-078-US1/T009): concrete per-layer readers — a gov-decisions chain
// reader, a Temporal run-history reader, a CI/bench/PR reader against the
// Chitin Telemetry layer — replace StaticTelemetryReader for live operation.
// The StaticTelemetryReader keeps the ingest contract proven and the loop
// workflow testable end-to-end without standing up every telemetry layer.
type StaticTelemetryReader struct {
	// source is the layer this reader claims to read.
	source TelemetrySource
	// records is the fixed record set; Read filters it to the window.
	records []TelemetryRecord
	// unreachable, when true, makes Read return (nil, nil) regardless of
	// records — it simulates a missing or down telemetry layer so the
	// empty-contribution edge case (spec 078 FR-002) is directly testable.
	unreachable bool
}

// NewStaticTelemetryReader builds a static reader for one source over a fixed
// record set. Records whose Source does not match are dropped — a reader only
// ever serves its own layer.
func NewStaticTelemetryReader(source TelemetrySource, records []TelemetryRecord) *StaticTelemetryReader {
	own := make([]TelemetryRecord, 0, len(records))
	for _, r := range records {
		if r.Source == source {
			own = append(own, r)
		}
	}
	return &StaticTelemetryReader{source: source, records: own}
}

// NewUnreachableTelemetryReader builds a static reader that simulates an
// unreachable layer: Read always returns an empty contribution and a nil
// error (spec 078 FR-002 edge case).
func NewUnreachableTelemetryReader(source TelemetrySource) *StaticTelemetryReader {
	return &StaticTelemetryReader{source: source, unreachable: true}
}

// Source returns the layer this reader reads.
func (r *StaticTelemetryReader) Source() TelemetrySource { return r.source }

// Read returns the reader's records that fall within the window. An
// unreachable reader returns (nil, nil) — an empty contribution, never an
// error (spec 078 FR-002 edge case: a dead layer must not fail the cycle).
func (r *StaticTelemetryReader) Read(_ context.Context, window TelemetryWindow) ([]TelemetryRecord, error) {
	if r.unreachable {
		return nil, nil // a missing/down layer is an empty contribution.
	}
	var out []TelemetryRecord
	for _, rec := range r.records {
		if window.Contains(rec) {
			out = append(out, rec)
		}
	}
	return out, nil
}

// IngestInput is the typed input to the IngestTelemetry activity — the one
// source to read and the window bounding the read.
type IngestInput struct {
	// Source is the telemetry layer to ingest from.
	Source TelemetrySource `json:"source"`
	// Window carries the (Since, Until] bounds; its Records are ignored on
	// input — the activity fills the contribution.
	Window TelemetryWindow `json:"window"`
}

// IngestResult is the typed output of the IngestTelemetry activity — one
// source's contribution to the telemetry window.
type IngestResult struct {
	// Source echoes the layer read, for correlation.
	Source TelemetrySource `json:"source"`
	// Records is this source's contribution. It is empty — never an error —
	// when the layer is missing or unreachable (spec 078 FR-002 edge case).
	Records []TelemetryRecord `json:"records"`
	// Reachable is false when the source's layer could not be reached; the
	// cycle proceeds regardless, this is purely for telemetry/diagnostics.
	Reachable bool `json:"reachable"`
}

// IngestActivity is the IngestTelemetry activity (spec 078 FR-002). Reading a
// telemetry layer is I/O — a SIDE EFFECT — so it MUST run in an activity,
// never in workflow code. The loop workflow dispatches one IngestTelemetry per
// source (AllSources) and merges every contribution into the cycle's window.
//
// The activity is bound at worker-host startup to one TelemetryReader per
// source. A source with no bound reader, or whose reader reports an
// unreachable layer, yields an empty contribution — never an activity error
// (spec 078 FR-002 edge case: an unreachable layer must not fail the cycle).
type IngestActivity struct {
	// readers maps a source to its read-only TelemetryReader. A source absent
	// from the map is treated as an unreachable layer — empty contribution.
	readers map[TelemetrySource]TelemetryReader
}

// NewIngestActivity builds an IngestTelemetry activity bound to a set of
// per-source readers. A nil or partial map is fine — any unbound source is an
// unreachable layer and contributes nothing (spec 078 FR-002 edge case).
func NewIngestActivity(readers []TelemetryReader) *IngestActivity {
	m := make(map[TelemetrySource]TelemetryReader, len(readers))
	for _, r := range readers {
		if r != nil {
			m[r.Source()] = r
		}
	}
	return &IngestActivity{readers: m}
}

// ActivityName is the stable Temporal activity name IngestTelemetry registers
// under and the loop workflow dispatches to.
func (a *IngestActivity) ActivityName() string { return "IngestTelemetry" }

// Execute ingests one source's telemetry contribution for a window. It is the
// activity function registered with the Temporal worker.
//
// An unbound source or an unreachable layer is NOT an error — it is an empty
// contribution with Reachable=false (spec 078 FR-002 edge case). The error
// return is reserved for a genuine reader fault on a layer that IS reachable;
// even then the loop workflow treats a single source's ingest fault as an
// empty contribution and continues — one dead source never fails the cycle.
func (a *IngestActivity) Execute(ctx context.Context, in IngestInput) (IngestResult, error) {
	reader, bound := a.readers[in.Source]
	if !bound {
		// No reader for this layer — an empty contribution, not an error.
		return IngestResult{Source: in.Source, Reachable: false}, nil
	}
	records, err := reader.Read(ctx, in.Window)
	if err != nil {
		// A reachable layer that genuinely faulted: surface it so the workflow
		// can decide. The workflow's ingest step swallows it into an empty
		// contribution (spec 078 FR-002 edge case) — see ingestAllSources.
		return IngestResult{Source: in.Source, Reachable: false},
			fmt.Errorf("loop: IngestTelemetry reading %s: %w", in.Source, err)
	}
	// Defensive: keep only records that genuinely belong to this source and
	// window, and order them canonically so the activity's output is
	// deterministic regardless of the reader's internal ordering.
	out := make([]TelemetryRecord, 0, len(records))
	for _, rec := range records {
		if rec.Source == in.Source && in.Window.Contains(rec) {
			out = append(out, rec)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Timestamp.Before(out[j].Timestamp)
		}
		return out[i].ID < out[j].ID
	})
	return IngestResult{Source: in.Source, Records: out, Reachable: true}, nil
}
