package ingest

import (
	"fmt"
	"net/url"
	"strings"
)

// Medium is the original form of a fetched source — a web page, a paper, an
// article, or a video. It is recorded for provenance and lets later stages
// reason about reading limits (a video transcript vs a short post), but every
// medium normalizes to the SAME IngestItem shape (FR-004): nothing downstream
// branches on the medium except where the spec explicitly calls for it.
type Medium string

const (
	// MediumWebPage is a generic web page or blog post.
	MediumWebPage Medium = "web-page"
	// MediumArticle is a long-form article (engineering post, news piece).
	MediumArticle Medium = "article"
	// MediumPaper is an academic or technical paper.
	MediumPaper Medium = "paper"
	// MediumVideo is a video — ingested via its transcript/description; the
	// reading is a bounded, representative extract, never a silent truncation
	// (spec 079 edge case "a video or long document exceeds reading limits").
	MediumVideo Medium = "video"
)

// IngestItem is the Normalized Item (FR-004, spec 079 Key Entities) — the
// uniform representation EVERY fetched source becomes, regardless of whether
// it started as a web page, a paper, an article, or a video. It is the single
// shape the filter ranks and the pipeline carries; the read stage produces
// it, the filter consumes it.
//
// An IngestItem holds external, UNTRUSTED content. Content is data, never
// instructions (FR-013): no stage — reading or filtering — ever acts on
// directives embedded in Content. The filter scores Content's signal; it does
// not obey it.
type IngestItem struct {
	// SourceRef is the canonical source reference — the URL the item was
	// fetched from (or a document/file ref). It is the dedup key (FR-014)
	// and the audit anchor: every drop and every kept item points back to it.
	SourceRef string `json:"source_ref"`

	// Title is the source's title, extracted by the read stage. It may be
	// empty when the source carries none; the filter treats an empty title
	// as weak (but not disqualifying) signal.
	Title string `json:"title"`

	// Content is the source's extracted text — the reading the filter scores.
	// For a bounded medium (a long document, a video transcript) this is a
	// bounded, representative extract; Truncated records when that happened.
	// UNTRUSTED: never executed, never obeyed (FR-013).
	Content string `json:"content"`

	// Medium is the source's original form. Recorded for provenance; the
	// item's shape does not depend on it (FR-004).
	Medium Medium `json:"medium"`

	// Trust is the item's provenance class — operator-seeded or gathered. It
	// is carried into the filter, where it raises trust but never bypasses
	// scoring (FR-008).
	Trust TrustMarker `json:"trust"`

	// FetchedAtUnix is the wall-clock fetch time, in Unix seconds, recorded
	// by the fetch ACTIVITY (never by workflow code — a workflow reads no
	// clock). Zero means the item was constructed without a fetch (a feed
	// stub before fetching). Used only for provenance, never for filter
	// scoring — scoring must be deterministic and clock-independent (FR-009).
	FetchedAtUnix int64 `json:"fetched_at_unix"`

	// Truncated is true when Content is a bounded extract of a larger source
	// rather than its full text (spec 079 edge case: a long document/video).
	// The filter records it so a truncated reading is never mistaken for a
	// complete one.
	Truncated bool `json:"truncated"`
}

// OperatorFeed is the operator-fed entry path (US1, FR-001, FR-002) — the
// first-class way the operator submits a specific URL/article/video directly
// into the pipeline. It is the input to NewOperatorItem and to the ingestion
// workflow's operator-fed mode.
type OperatorFeed struct {
	// URL is the source the operator submits. It is validated by
	// NewOperatorItem before the pipeline touches the network.
	URL string `json:"url"`
	// Medium is the operator's declared form of the source. Empty defaults
	// to MediumWebPage — the operator need not classify their pick.
	Medium Medium `json:"medium"`
	// Note is an optional operator note carried as provenance — why the
	// operator thought this was worth the swarm knowing. It is provenance
	// only; it never feeds the filter score (FR-009 determinism).
	Note string `json:"note"`
}

// NewOperatorItem constructs an IngestItem for an operator-fed source (US1,
// T009; FR-001, FR-002). The returned item carries the TrustOperatorSeeded
// high-trust marker — the operator's hand-picked input is a first-class path,
// not an afterthought (spec 079 US1).
//
// It validates the URL up front: an item the pipeline cannot fetch should
// fail at submission, not mid-workflow. The returned item is a STUB — it has
// a SourceRef and a trust marker but no Content; the fetch + read activity
// fills Content in (see fetch.go). Construction is pure: no network, no
// clock — hence safe to call from anywhere, including a workflow's input
// validation.
func NewOperatorItem(feed OperatorFeed) (IngestItem, error) {
	ref := strings.TrimSpace(feed.URL)
	if ref == "" {
		return IngestItem{}, fmt.Errorf("ingest: operator feed has an empty URL")
	}
	u, err := url.Parse(ref)
	if err != nil {
		return IngestItem{}, fmt.Errorf("ingest: operator feed URL %q is not a valid URL: %w", ref, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return IngestItem{}, fmt.Errorf(
			"ingest: operator feed URL %q must be http or https, got scheme %q", ref, u.Scheme)
	}
	if u.Host == "" {
		return IngestItem{}, fmt.Errorf("ingest: operator feed URL %q has no host", ref)
	}

	medium := feed.Medium
	if medium == "" {
		medium = MediumWebPage
	}
	return IngestItem{
		SourceRef: ref,
		Medium:    medium,
		Trust:     TrustOperatorSeeded,
	}, nil
}

// Host returns the host of the item's SourceRef, lower-cased, or "" when the
// ref does not parse as a URL. The filter uses it for credibility heuristics
// (a known-credible domain); dedup uses the full SourceRef, not the host.
func (it IngestItem) Host() string {
	u, err := url.Parse(it.SourceRef)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// HasContent reports whether the item has been read — true once the fetch +
// read activity has filled Content. A stub item (just constructed, not yet
// fetched) has no content; the filter is never run on a stub.
func (it IngestItem) HasContent() bool {
	return strings.TrimSpace(it.Content) != ""
}
