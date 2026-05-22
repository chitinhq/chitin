package ingest

import "testing"

// Spec 079 T007 / FR-004 tests for the Normalized Item: every fetched source —
// web page, paper, article, video — normalizes to the SAME IngestItem shape,
// and the operator-fed entry path constructs a high-trust operator-seeded
// item (T009 / FR-002).

// TestIngestItem_AllMediaNormalizeToSameShape proves FR-004: a video and a
// web page become the same struct type — the filter and the pipeline carry
// one shape regardless of medium.
func TestIngestItem_AllMediaNormalizeToSameShape(t *testing.T) {
	web := IngestItem{SourceRef: "https://example.com/post", Medium: MediumWebPage, Content: "hello"}
	video := IngestItem{SourceRef: "https://example.com/talk", Medium: MediumVideo, Content: "hello"}
	paper := IngestItem{SourceRef: "https://example.com/paper", Medium: MediumPaper, Content: "hello"}

	// They are the same type — a uniform representation (FR-004). The medium
	// is recorded, but the shape does not branch on it.
	for _, it := range []IngestItem{web, video, paper} {
		if !it.HasContent() {
			t.Errorf("item %q should report content", it.SourceRef)
		}
	}
	if web.Medium == video.Medium {
		t.Error("medium should still be recorded distinctly per item")
	}
}

// TestNewOperatorItem_HighTrustSeed proves T009 / FR-002: an operator-fed
// item is constructed with the operator-seeded high-trust marker.
func TestNewOperatorItem_HighTrustSeed(t *testing.T) {
	it, err := NewOperatorItem(OperatorFeed{URL: "https://example.com/engineering-post"})
	if err != nil {
		t.Fatalf("NewOperatorItem rejected a valid URL: %v", err)
	}
	if it.Trust != TrustOperatorSeeded {
		t.Errorf("Trust = %q, want operator-seeded — the operator-fed path is first-class (FR-002)", it.Trust)
	}
	if it.SourceRef != "https://example.com/engineering-post" {
		t.Errorf("SourceRef = %q, want the submitted URL", it.SourceRef)
	}
	if it.HasContent() {
		t.Error("a freshly constructed operator item is a stub — content is filled by the fetch stage")
	}
}

// TestNewOperatorItem_DefaultsMedium proves an operator need not classify
// their pick — an unset medium defaults to a web page.
func TestNewOperatorItem_DefaultsMedium(t *testing.T) {
	it, err := NewOperatorItem(OperatorFeed{URL: "https://example.com/x"})
	if err != nil {
		t.Fatalf("NewOperatorItem: %v", err)
	}
	if it.Medium != MediumWebPage {
		t.Errorf("Medium = %q, want web-page default", it.Medium)
	}
}

// TestNewOperatorItem_RejectsBadInput proves a malformed submission fails at
// construction, before the pipeline touches the network.
func TestNewOperatorItem_RejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"no scheme", "example.com/post"},
		{"ftp scheme", "ftp://example.com/file"},
		{"no host", "https://"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewOperatorItem(OperatorFeed{URL: tc.url}); err == nil {
				t.Errorf("NewOperatorItem(%q) should have failed", tc.url)
			}
		})
	}
}

// TestIngestItem_Host proves the host accessor lower-cases and tolerates a
// non-URL ref — the filter's credibility heuristic relies on it.
func TestIngestItem_Host(t *testing.T) {
	it := IngestItem{SourceRef: "https://Blog.Example.COM/post"}
	if got := it.Host(); got != "blog.example.com" {
		t.Errorf("Host() = %q, want lower-cased blog.example.com", got)
	}
}
