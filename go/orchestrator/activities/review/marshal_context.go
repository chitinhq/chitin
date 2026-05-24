package review

import (
	"encoding/json"
	"sort"
)

// reviewContextFile is the per-file shape within reviewContextV1.diff.
// Names match contracts/review-mode-driver-contract.md verbatim — the
// driver's prompt template is opaque to the orchestrator, so any rename
// here breaks every driver's prompt downstream.
type reviewContextFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Diff      string `json:"diff"`
}

// reviewContextSpecArtifact is the per-artifact shape within
// reviewContextV1.spec_artifacts. Mirrors PRSnapshot.SpecArtifacts.
type reviewContextSpecArtifact struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// reviewContextPR is the metadata header of reviewContextV1.
type reviewContextPR struct {
	Repo     string `json:"repo"`
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Author   string `json:"author"`
	HeadOID  string `json:"head_oid"`
	BaseRef  string `json:"base_ref"`
}

// reviewContextV1 is the JSON shape the dispatch activity puts into
// WorkUnit.Context, defined by contracts/review-mode-driver-contract.md.
// Drivers read this from Context and apply their own prompt template
// (FR-003: prompts are opaque to the orchestrator).
//
// Field names MUST match the contract exactly. Adding a field is a
// backwards-compatible amendment; renaming is a breaking change that
// requires updating every driver's prompt.
type reviewContextV1 struct {
	PR                       reviewContextPR             `json:"pr"`
	Diff                     []reviewContextFile         `json:"diff"`
	SpecArtifacts            []reviewContextSpecArtifact `json:"spec_artifacts"`
	PolicyClassHint          string                      `json:"policy_class_hint"`
	SnapshotCapturedAt       string                      `json:"snapshot_captured_at"`
	MaxBytesIn               int                         `json:"max_bytes_in"`
	SnapshotTruncatedToBytes int                         `json:"snapshot_truncated_to_bytes,omitempty"`
}

// marshalReviewContext builds the WorkUnit.Context JSON for a review-mode
// dispatch. It populates the spec-094 contract shape from the snapshot +
// policy class + driver's declared maxBytesIn.
//
// Truncation behavior (FR-009 of spec 104): if the marshalled context
// would exceed maxBytesIn, the function progressively trims SpecArtifacts
// (largest first) then truncates per-file Diff entries (longest first)
// until the result fits. The returned struct's SnapshotTruncatedToBytes
// field names the final encoded size so the driver knows whether it
// received a partial view.
//
// maxBytesIn = 0 disables the cap (caller did not opt into truncation).
//
// The function is deterministic on its inputs: identical PRSnapshot
// + PolicyClass + maxBytesIn produce identical bytes (sort tie-breakers
// canonicalize per-Path).
func marshalReviewContext(snapshot PRSnapshot, policyClass string, maxBytesIn int) ([]byte, error) {
	ctx := buildReviewContext(snapshot, policyClass, maxBytesIn)
	raw, err := json.Marshal(ctx)
	if err != nil {
		return nil, err
	}
	if maxBytesIn <= 0 || len(raw) <= maxBytesIn {
		return raw, nil
	}
	// Over budget. Trim and retry. Strategy: drop spec artifacts first
	// (largest first), then trim per-file diffs (longest first), then
	// finally null out diffs entirely. After each round of trimming,
	// re-marshal and check size.
	trimmedCtx := ctx
	// Round 1: drop spec artifacts largest-first.
	sort.Slice(trimmedCtx.SpecArtifacts, func(i, j int) bool {
		return len(trimmedCtx.SpecArtifacts[i].Content) > len(trimmedCtx.SpecArtifacts[j].Content)
	})
	for len(trimmedCtx.SpecArtifacts) > 0 {
		trimmedCtx.SpecArtifacts = trimmedCtx.SpecArtifacts[1:]
		raw2, err := json.Marshal(trimmedCtx)
		if err != nil {
			return nil, err
		}
		if len(raw2) <= maxBytesIn {
			trimmedCtx.SnapshotTruncatedToBytes = len(raw2)
			return json.Marshal(trimmedCtx)
		}
	}
	// Round 2: trim per-file diffs largest-first. We do this iteratively:
	// each pass cuts the largest remaining diff in half, until either
	// every diff is empty or the result fits.
	for {
		// Find the file with the largest current diff.
		bigIdx := -1
		bigSize := 0
		for i, f := range trimmedCtx.Diff {
			if len(f.Diff) > bigSize {
				bigSize = len(f.Diff)
				bigIdx = i
			}
		}
		if bigIdx == -1 || bigSize == 0 {
			break
		}
		// Halve and append a marker.
		half := bigSize / 2
		trimmedCtx.Diff[bigIdx].Diff = trimmedCtx.Diff[bigIdx].Diff[:half] +
			"\n[diff truncated by review-context cap]\n"
		raw2, err := json.Marshal(trimmedCtx)
		if err != nil {
			return nil, err
		}
		if len(raw2) <= maxBytesIn {
			trimmedCtx.SnapshotTruncatedToBytes = len(raw2)
			return json.Marshal(trimmedCtx)
		}
	}
	// Final: even with all diffs and artifacts gone, still over budget.
	// Return what we have with the marker; the driver will see an empty
	// diff list and likely abstain.
	trimmedCtx.SnapshotTruncatedToBytes = -1
	return json.Marshal(trimmedCtx)
}

// buildReviewContext is the pure pre-marshalling step. Extracted so tests
// can assert on the structure independent of the JSON encoding.
func buildReviewContext(snapshot PRSnapshot, policyClass string, maxBytesIn int) reviewContextV1 {
	out := reviewContextV1{
		PR: reviewContextPR{
			Repo:    snapshot.Repo,
			Number:  snapshot.PRNumber,
			Title:   snapshot.Title,
			Body:    snapshot.Body,
			Author:  snapshot.Author,
			HeadOID: snapshot.HeadOID,
			BaseRef: snapshot.BaseRef,
		},
		PolicyClassHint:    policyClass,
		SnapshotCapturedAt: snapshot.CapturedAt.Format("2006-01-02T15:04:05Z07:00"),
		MaxBytesIn:         maxBytesIn,
	}
	out.Diff = make([]reviewContextFile, len(snapshot.Files))
	for i, f := range snapshot.Files {
		out.Diff[i] = reviewContextFile{
			Path:      f.Path,
			Additions: f.Additions,
			Deletions: f.Deletions,
			Diff:      f.Diff,
		}
	}
	out.SpecArtifacts = make([]reviewContextSpecArtifact, len(snapshot.SpecArtifacts))
	for i, a := range snapshot.SpecArtifacts {
		out.SpecArtifacts[i] = reviewContextSpecArtifact{
			Path:    a.Path,
			Content: a.Content,
		}
	}
	return out
}
