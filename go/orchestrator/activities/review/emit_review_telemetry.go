package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review/verdict"
)

// EmitReviewTelemetryInput is the per-invocation telemetry record (spec
// 094 FR-032). One event per closed ReviewerInvocation: driver_id, role,
// verdict enum or failure_kind, content hashes for the three structured
// fields, elapsed ms, snapshot hash ref. Raw text lives in workflow
// history (FR-033) — only hashes are emitted to OTLP.
type EmitReviewTelemetryInput struct {
	WorkflowID      string             `json:"workflow_id"`
	Repo            string             `json:"repo"`
	PRNumber        int                `json:"pr_number"`
	Invocation      ReviewerInvocation `json:"invocation"`
}

// EmitReviewTelemetryActivityName is the stable Temporal name.
const EmitReviewTelemetryActivityName = "EmitReviewTelemetry"

// EmitReviewTelemetry is the per-invocation telemetry sink. v1 stub:
// computes the content hashes deterministically and would emit to the
// orchestrator's OTLP sink in production. For Phase 2 foundational the
// sink itself is a no-op — the workflow's testsuite tests mock this
// activity entirely and assert on the call arguments, so the production
// sink is wired in a follow-up.
//
// The hash computation here is the actual production logic: the
// follow-up PR adds the OTLP emit call, but it operates on the same
// hashes computed here.
//
// TODO(spec-094-impl PR #2): wire the actual OTLP sink (probably via
// the existing activities.EmitTickTelemetry pattern).
func EmitReviewTelemetry(_ context.Context, _ EmitReviewTelemetryInput) error {
	// no-op in this slice; future PR replaces with otlp.Emit(...).
	return nil
}

// HashStringList returns the SHA-256 hex hash of a deterministic JSON
// encoding of a string list. Empty list hashes consistently (the JSON
// encoding "[]" is stable) so two empty fields have the same hash.
func HashStringList(items []string) string {
	if items == nil {
		items = []string{} // canonicalize nil to empty for stable hash
	}
	buf, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// VerdictTelemetryFields extracts the FR-032 hashed fields from one closed
// invocation. Exported so future telemetry sinks (and tests) can compute
// them off-activity.
type VerdictTelemetryFields struct {
	VerdictEnum         string `json:"verdict"`
	FailureKind         string `json:"failure_kind"`
	ConcernsHash        string `json:"concerns_hash"`
	RecommendationsHash string `json:"recommendations_hash"`
	BlockersHash        string `json:"blockers_hash"`
}

// VerdictTelemetry computes the FR-032 telemetry fields for one closed
// invocation. On a successful verdict, VerdictEnum is set and FailureKind
// is empty; on a failure, VerdictEnum is empty and FailureKind names the
// kind. Content hashes are computed from the verdict's three lists; on a
// failure they all hash an empty list (canonical "empty").
func VerdictTelemetry(inv ReviewerInvocation) VerdictTelemetryFields {
	out := VerdictTelemetryFields{}
	if v := inv.Outcome.Verdict; v != nil {
		out.VerdictEnum = string(v.Verdict)
		out.ConcernsHash = HashStringList(v.Concerns)
		out.RecommendationsHash = HashStringList(v.Recommendations)
		out.BlockersHash = HashStringList(v.Blockers)
	} else if f := inv.Outcome.Failure; f != nil {
		out.FailureKind = string(f.Kind)
		empty := HashStringList(nil)
		out.ConcernsHash, out.RecommendationsHash, out.BlockersHash = empty, empty, empty
	}
	return out
}

// summarizeBlockers is a tiny helper exported for tests — joins the first
// few blockers into one line for use in telemetry reasons. Kept in this
// file because the OTLP record is the primary consumer.
func summarizeBlockers(v *verdict.StructuredVerdict, limit int) string {
	if v == nil || len(v.Blockers) == 0 {
		return ""
	}
	n := limit
	if n > len(v.Blockers) {
		n = len(v.Blockers)
	}
	return strings.Join(v.Blockers[:n], "; ")
}
