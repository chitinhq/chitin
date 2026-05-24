package review

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleSnapshot() PRSnapshot {
	return PRSnapshot{
		Repo:     "chitinhq/chitin",
		PRNumber: 953,
		HeadOID:  "abc123",
		Title:    "spec(103)",
		Body:     "loop spec",
		Author:   "jpleva91",
		BaseRef:  "main",
		Files: []PRFile{
			{Path: "go/foo.go", Additions: 5, Deletions: 2, Diff: "diff --git a/go/foo.go b/go/foo.go\n@@\n+x\n"},
		},
		SpecArtifacts: []SpecArtifact{
			{Path: ".specify/specs/103-x/spec.md", Content: "# Spec\n"},
		},
		CapturedAt: time.Date(2026, 5, 24, 5, 0, 0, 0, time.UTC),
	}
}

func TestMarshalReviewContext_HappyPath(t *testing.T) {
	raw, err := marshalReviewContext(sampleSnapshot(), "impl", 0)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded reviewContextV1
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.PR.Repo != "chitinhq/chitin" || decoded.PR.Number != 953 {
		t.Errorf("PR mismatch: %+v", decoded.PR)
	}
	if decoded.PolicyClassHint != "impl" {
		t.Errorf("policy_class_hint = %q, want impl", decoded.PolicyClassHint)
	}
	if len(decoded.Diff) != 1 || decoded.Diff[0].Path != "go/foo.go" {
		t.Errorf("Diff = %+v", decoded.Diff)
	}
	if len(decoded.SpecArtifacts) != 1 || decoded.SpecArtifacts[0].Path != ".specify/specs/103-x/spec.md" {
		t.Errorf("SpecArtifacts = %+v", decoded.SpecArtifacts)
	}
	if decoded.SnapshotCapturedAt != "2026-05-24T05:00:00Z" {
		t.Errorf("captured_at = %q", decoded.SnapshotCapturedAt)
	}
}

func TestMarshalReviewContext_Deterministic(t *testing.T) {
	s := sampleSnapshot()
	a, _ := marshalReviewContext(s, "impl", 0)
	b, _ := marshalReviewContext(s, "impl", 0)
	if string(a) != string(b) {
		t.Errorf("not deterministic; sizes %d vs %d", len(a), len(b))
	}
}

func TestMarshalReviewContext_TruncatesSpecArtifactsFirst(t *testing.T) {
	s := sampleSnapshot()
	// Add a huge spec artifact that overflows a tight budget.
	s.SpecArtifacts = append(s.SpecArtifacts, SpecArtifact{
		Path: ".specify/specs/103-x/plan.md", Content: strings.Repeat("x", 10000),
	})
	raw, err := marshalReviewContext(s, "impl", 2000) // tight cap
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(raw) > 2000 {
		t.Errorf("output %d bytes > cap 2000", len(raw))
	}
	var decoded reviewContextV1
	_ = json.Unmarshal(raw, &decoded)
	// SpecArtifacts should be reduced (largest dropped first).
	if len(decoded.SpecArtifacts) >= 2 {
		t.Errorf("expected SpecArtifacts trimmed, got %d entries", len(decoded.SpecArtifacts))
	}
}

func TestMarshalReviewContext_TruncatesDiffsAfterArtifacts(t *testing.T) {
	s := sampleSnapshot()
	s.SpecArtifacts = nil // already minimal
	// Pump a huge diff so even with empty artifacts we overflow.
	s.Files = []PRFile{{
		Path: "big.go", Additions: 1000, Deletions: 0,
		Diff: strings.Repeat("x", 5000),
	}}
	raw, err := marshalReviewContext(s, "impl", 1000) // tight cap
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(raw) > 1000 {
		t.Errorf("output %d bytes > cap 1000", len(raw))
	}
	var decoded reviewContextV1
	_ = json.Unmarshal(raw, &decoded)
	if !strings.Contains(decoded.Diff[0].Diff, "[diff truncated by review-context cap]") {
		t.Errorf("expected truncation marker in diff, got: %q", decoded.Diff[0].Diff[:200])
	}
	if decoded.SnapshotTruncatedToBytes == 0 {
		t.Errorf("expected SnapshotTruncatedToBytes set")
	}
}

func TestMarshalReviewContext_NoCap(t *testing.T) {
	s := sampleSnapshot()
	// maxBytesIn=0 disables the cap; should produce verbatim regardless of size.
	s.Files[0].Diff = strings.Repeat("x", 100000)
	raw, err := marshalReviewContext(s, "impl", 0)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(raw) < 100000 {
		t.Errorf("expected verbatim output (>100k), got %d", len(raw))
	}
}
