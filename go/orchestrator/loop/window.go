package loop

import (
	"sort"
	"time"
)

// TelemetrySource names one cross-layer telemetry source the loop ingests
// from (spec 078 FR-002). It is a closed enumeration — the loop ingests from
// every reachable layer, not a single source, so a Finding's evidence can
// always be attributed to a named source.
type TelemetrySource string

const (
	// SourceGovernance is the governance / chitin-chain decision log — the
	// gov-decisions chain (spec 078 FR-002, Key Entities: Telemetry Window).
	SourceGovernance TelemetrySource = "governance"
	// SourceRunHistory is the orchestrator workflow run history (spec 070).
	SourceRunHistory TelemetrySource = "run_history"
	// SourceCI is CI outcome telemetry — workflow-run pass/fail.
	SourceCI TelemetrySource = "ci"
	// SourceBench is bench-result telemetry.
	SourceBench TelemetrySource = "bench"
	// SourcePR is pull-request outcome telemetry.
	SourcePR TelemetrySource = "pr"
	// SourceAgent is per-agent run telemetry.
	SourceAgent TelemetrySource = "agent"
)

// AllSources is the canonical, ordered set of telemetry sources the loop
// ingests. It is sorted so iteration over it is deterministic — a loop cycle
// dispatches one ingest activity per source in this fixed order.
var AllSources = []TelemetrySource{
	SourceAgent,
	SourceBench,
	SourceCI,
	SourceGovernance,
	SourcePR,
	SourceRunHistory,
}

// TelemetryRecord is one analyzed-over unit of telemetry — a single decision,
// run, CI result, bench result, or PR outcome. It is the evidence currency of
// the loop: a Finding cites the specific TelemetryRecords that ground it
// (spec 078 FR-004, Key Entities: Finding).
//
// The loop READS these records; it does not own telemetry collection
// (spec 078 Out of Scope — the loop is a consumer of telemetry).
type TelemetryRecord struct {
	// ID is the stable identity of the underlying telemetry record — a chain
	// decision hash, a workflow run id, a CI run id. It is the cite-able
	// reference an operator follows back to the raw telemetry.
	ID string `json:"id"`
	// Source is the layer the record came from.
	Source TelemetrySource `json:"source"`
	// Timestamp is when the underlying event occurred. It is used only to
	// bound the record within a window and for stable ordering — the loop
	// never reads the wall clock itself (that would be non-deterministic);
	// the timestamp is data carried in from the telemetry layer.
	Timestamp time.Time `json:"timestamp"`
	// Kind is a coarse classifier of the event — e.g. "ci_failure",
	// "pr_merged", "gov_denied". The analysis passes group on it.
	Kind string `json:"kind"`
	// Outcome is the event's result — "success" / "failure" / "denied" etc.
	// A recurring-failure pass keys on Outcome plus Signature.
	Outcome string `json:"outcome"`
	// Signature is a stable, normalized fingerprint of WHAT happened — a
	// failing command line, an error class, a denied action category. Two
	// records of the identical underlying failure share a Signature; this is
	// what lets the analysis collapse repeats into one Finding.
	Signature string `json:"signature"`
	// SpecRef is the chitin spec the event is about, when the telemetry
	// carries one — e.g. "076". It is how a Finding knows which spec a
	// proposal should target (spec 078 FR-003).
	SpecRef string `json:"spec_ref"`
	// Summary is a short human-readable description of the event, for the
	// operator reading a proposal's evidence.
	Summary string `json:"summary"`
}

// TelemetryWindow is the checkpoint-bounded slice of cross-layer telemetry a
// loop cycle ingests (spec 078 Key Entities: Telemetry Window). It is bounded
// by the previous cycle's checkpoint (exclusive lower bound) and the current
// cycle's start (inclusive upper bound), and holds the records ingested from
// every reachable source.
//
// A TelemetryWindow is a pure value: it carries no Temporal type and reads no
// wall clock. The bounds are supplied by the workflow (which gets them from
// workflow-deterministic time and the carried-forward checkpoint); the records
// are supplied by the ingest activities.
type TelemetryWindow struct {
	// Since is the exclusive lower bound — the previous cycle's checkpoint.
	// On the first cycle it is the zero time, meaning "from the beginning".
	Since time.Time `json:"since"`
	// Until is the inclusive upper bound — this cycle's start.
	Until time.Time `json:"until"`
	// Records is every telemetry record ingested into this window, across
	// every source. It is not assumed ordered; Sorted returns a deterministic
	// ordering for the analysis passes.
	Records []TelemetryRecord `json:"records"`
}

// Contains reports whether a record's timestamp falls within the window's
// (Since, Until] bounds. A zero Since means an unbounded lower bound — the
// first cycle ingests everything up to Until.
//
// The boundary is half-open at the bottom and closed at the top so two
// consecutive cycles — whose checkpoints meet at one instant — never both
// claim a record at exactly that instant and never skip one (spec 078 FR-011:
// each cycle ingests exactly the telemetry since the previous checkpoint).
func (w TelemetryWindow) Contains(rec TelemetryRecord) bool {
	t := rec.Timestamp
	if !w.Since.IsZero() && !t.After(w.Since) {
		return false // at or before the lower bound — belongs to a prior cycle.
	}
	if !w.Until.IsZero() && t.After(w.Until) {
		return false // after the upper bound — belongs to a later cycle.
	}
	return true
}

// Sorted returns the window's records in a fully deterministic order:
// timestamp ascending, then Source, then Kind, then ID as the final stable
// tie-breaker. A sort without a named final tie-breaker is not sorted — ID is
// unique per record, so this ordering is total and replay-stable.
func (w TelemetryWindow) Sorted() []TelemetryRecord {
	out := make([]TelemetryRecord, len(w.Records))
	copy(out, w.Records)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Timestamp.Equal(b.Timestamp) {
			return a.Timestamp.Before(b.Timestamp)
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.ID < b.ID
	})
	return out
}

// Empty reports whether the window holds no records — an empty or unreachable
// telemetry window. An empty window is a valid cycle outcome: the cycle
// completes as an empty cycle and advances its checkpoint, never blocks the
// loop (spec 078 edge case: empty / unreachable telemetry window).
func (w TelemetryWindow) Empty() bool { return len(w.Records) == 0 }

// Merge appends another source's records into the window. Ingest activities
// run one per source; the workflow merges each contribution into the window
// as it lands. Merge is order-independent — Sorted imposes the canonical
// ordering downstream.
func (w *TelemetryWindow) Merge(records []TelemetryRecord) {
	w.Records = append(w.Records, records...)
}

// CycleCheckpoint is the marker bounding telemetry ingest between consecutive
// cycles (spec 078 Key Entities: Cycle Checkpoint). It is advanced on cycle
// completion — including for an empty cycle, because silence is a valid
// outcome and a skipped advance would re-ingest a window (spec 078 FR-011).
//
// US1 (this slice) runs a single on-demand cycle, so the checkpoint is carried
// in and echoed out but never auto-advanced on a schedule. Continuous
// checkpoint advance across scheduled cycles is US3.
//
// TODO(spec-078-US3): the scheduled loop advances At to the cycle's Until on
// completion and carries it forward across Continue-As-New (FR-011, T023/T024).
type CycleCheckpoint struct {
	// At is the instant up to which telemetry has been ingested by a prior
	// cycle. The next cycle's window has Since == At. The zero value means no
	// prior cycle has run — the first cycle ingests from the beginning.
	At time.Time `json:"at"`
	// Cycle is the monotonically increasing cycle counter, for correlating a
	// window and its proposals to the cycle that produced them.
	Cycle int `json:"cycle"`
}
