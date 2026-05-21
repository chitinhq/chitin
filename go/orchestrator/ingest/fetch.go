package ingest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// The fetch + read stage (US1, T008; FR-004, FR-012). Fetching a source is a
// SIDE EFFECT — network egress — so it MUST run in a Temporal ACTIVITY, never
// in workflow code. This file defines the FetchAndRead activity: it fetches a
// source through the kernel's typed-egress gate, then reads the response into
// a Normalized IngestItem.
//
// KERNEL-GATED EGRESS (FR-012, the non-negotiable boundary): every fetch MUST
// pass the chitin kernel's typed-egress / trust-policy check before the
// network is touched; a fetch to a domain outside the trust policy MUST be
// DENIED by the kernel, not silently completed. The chitin kernel already
// enforces typed-egress on network actions (spec 079 Assumptions) — this
// pipeline RELIES on that governance, it does not rebuild it. The EgressGate
// interface below is the seam: in production it is bound to the kernel's
// real typed-egress check at worker-host startup; in tests it is a fake.
//
// PROMPT-INJECTION CONTAINMENT (FR-013): the fetched body is read as DATA into
// IngestItem.Content. It is never parsed for, and never acted on as,
// instructions. The read stage extracts text; it does not interpret it.

// EgressGate is the kernel's typed-egress / trust-policy check (FR-012, spec
// 079 Key Entities: Egress Gate). The fetch activity calls Allow before every
// network touch; a denied URL is NOT fetched. This interface is the seam to
// the chitin kernel — the pipeline depends on the abstraction, the worker
// host binds the concrete kernel gate.
//
// TODO(spec 079 / kernel integration): bind the production EgressGate to the
// chitin kernel's real typed-egress check at worker-host startup. The kernel
// enforces typed-egress on network actions today (spec 079 Assumptions); this
// seam exists so the binding is one wiring change in main, not a rewrite.
type EgressGate interface {
	// Allow reports whether egress to url is permitted by the trust policy.
	// A denied egress returns ok=false with a human-readable reason; the
	// fetch activity then records a denied fetch and does NOT touch the
	// network (FR-012, edge case "egress to a domain outside the trust
	// policy"). An err is a gate fault (the policy could not be evaluated),
	// distinct from a clean deny.
	Allow(ctx context.Context, url string) (ok bool, reason string, err error)
}

// allowAllGate is the default EgressGate used when no kernel gate is bound. It
// permits every URL — a DEVELOPMENT-ONLY fallback. It exists so a zero-value
// FetchActivity is usable in tests; production MUST bind the real kernel gate.
//
// TODO(spec 079 / kernel integration): production startup MUST replace this
// with the kernel's typed-egress gate. allowAllGate is not a trust policy —
// it is the absence of one; FR-012 is satisfied only by the kernel gate.
type allowAllGate struct{}

func (allowAllGate) Allow(context.Context, string) (bool, string, error) {
	return true, "egress gate not bound — development fallback permits all (FR-012 requires the kernel gate in production)", nil
}

// HTTPDoer is the minimal http.Client surface the fetch activity needs. It is
// an interface so a test can inject a fake transport without a network.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// fetchTimeout bounds a single source fetch — a source slower than this is
// treated as a failed fetch (recorded, the batch continues; FR-015).
const fetchTimeout = 20 * time.Second

// maxReadBytes caps how much of a response body the read stage ingests. A
// video transcript or a long document beyond this is read as a BOUNDED,
// representative extract with IngestItem.Truncated=true — never failed,
// never silently truncated without record (spec 079 edge case "a video or
// long document exceeds practical reading limits").
const maxReadBytes = 512 * 1024

// FetchInput is the typed input to the FetchAndRead activity — the source to
// fetch and the trust marker the resulting IngestItem carries (operator-seeded
// for US1; gathered for US2's broad-net path, a documented TODO).
type FetchInput struct {
	// SourceRef is the URL to fetch.
	SourceRef string `json:"source_ref"`
	// Medium is the source's declared original form, carried onto the item.
	Medium Medium `json:"medium"`
	// Trust is the provenance class the resulting item carries.
	Trust TrustMarker `json:"trust"`
}

// FetchResult is the typed output of the FetchAndRead activity. A failed or
// denied fetch is a RESULT (Fetched=false with a reason), not an activity
// error — so the pipeline records the failure and continues the batch
// (FR-015); a workflow-level error is reserved for a genuine activity fault.
type FetchResult struct {
	// Fetched is true iff the source was fetched and read into an item. False
	// covers both a kernel deny (FR-012) and a failed fetch (FR-015).
	Fetched bool `json:"fetched"`
	// Denied is true when the fetch was DENIED by the egress gate — distinct
	// from a fetch that was attempted and failed. A denied fetch never
	// touched the network.
	Denied bool `json:"denied"`
	// Item is the Normalized IngestItem — populated only when Fetched is true.
	Item IngestItem `json:"item"`
	// Reason is the human-readable account: the egress decision, or why the
	// fetch failed. Always populated for an audit trail.
	Reason string `json:"reason"`
}

// FetchActivity is the FetchAndRead activity (US1 T008; FR-004, FR-012). It
// holds the kernel egress gate and an HTTP client, both bound at worker-host
// startup. A zero-value FetchActivity is usable for tests — it falls back to
// allowAllGate and a default http.Client — but production MUST bind the real
// kernel gate via NewFetchActivity.
type FetchActivity struct {
	// gate is the kernel's typed-egress check. Nil falls back to allowAllGate
	// (development only — see the TODO on allowAllGate).
	gate EgressGate
	// http is the HTTP client used for the fetch. Nil falls back to a
	// default client with the fetch timeout.
	http HTTPDoer
}

// NewFetchActivity returns a FetchAndRead activity bound to the kernel egress
// gate. Pass the chitin kernel's typed-egress gate as gate (FR-012); a nil
// gate falls back to the development allow-all gate, which production MUST
// NOT use. A nil client gets a default http.Client.
func NewFetchActivity(gate EgressGate, client HTTPDoer) *FetchActivity {
	return &FetchActivity{gate: gate, http: client}
}

// FetchActivityName is the stable Temporal activity name FetchAndRead
// registers under and the ingestion workflow dispatches to.
const FetchActivityName = "FetchAndRead"

// ActivityName returns the activity's registration name.
func (*FetchActivity) ActivityName() string { return FetchActivityName }

// Execute fetches one source through the kernel egress gate and reads it into
// a Normalized IngestItem. It is the activity function registered with the
// Temporal worker.
//
// The order is the FR-012 invariant: gate FIRST, network SECOND. The egress
// gate is consulted before any socket is opened; a denied URL is never
// fetched. A clean deny and a failed fetch both return Fetched=false as a
// RESULT (the batch continues; FR-015) — the error return is reserved for a
// genuine activity fault (a gate that could not be evaluated).
func (a *FetchActivity) Execute(ctx context.Context, in FetchInput) (FetchResult, error) {
	ref := strings.TrimSpace(in.SourceRef)
	if ref == "" {
		return FetchResult{
			Fetched: false,
			Reason:  "fetch input has an empty source ref — nothing to fetch",
		}, nil
	}

	// --- FR-012: kernel-gated egress — the gate is consulted BEFORE the
	// network is touched. A denied URL is never fetched.
	gate := a.gate
	if gate == nil {
		gate = allowAllGate{}
	}
	ok, gateReason, gateErr := gate.Allow(ctx, ref)
	if gateErr != nil {
		// The policy itself could not be evaluated — a genuine activity
		// fault, surfaced so the workflow's retry policy can act. The
		// network is NOT touched.
		return FetchResult{}, fmt.Errorf(
			"ingest: egress gate could not evaluate %q: %w", ref, gateErr)
	}
	if !ok {
		// A clean kernel DENY (FR-012). The source is not fetched; the
		// pipeline records a denied fetch and continues the batch.
		return FetchResult{
			Fetched: false,
			Denied:  true,
			Reason: fmt.Sprintf(
				"egress to %q denied by the kernel typed-egress / trust policy: %s", ref, gateReason),
		}, nil
	}

	// --- the fetch — only reached once the kernel has allowed egress.
	client := a.http
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout}
	}
	reqCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, ref, nil)
	if err != nil {
		return FetchResult{
			Fetched: false,
			Reason:  fmt.Sprintf("could not build fetch request for %q: %v", ref, err),
		}, nil
	}
	req.Header.Set("User-Agent", "chitin-ingest/0 (spec-079 information-ingestion-pipeline)")

	resp, err := client.Do(req)
	if err != nil {
		// Unreachable / DNS / timeout — a failed fetch, recorded; the batch
		// continues (FR-015). Not an activity error.
		return FetchResult{
			Fetched: false,
			Reason:  fmt.Sprintf("fetch of %q failed: %v", ref, err),
		}, nil
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		// Paywalled / not-found / server error — a failed fetch (FR-015).
		return FetchResult{
			Fetched: false,
			Reason:  fmt.Sprintf("fetch of %q returned HTTP %d", ref, resp.StatusCode),
		}, nil
	}

	// --- the read — bounded; a body beyond maxReadBytes is a representative
	// extract with Truncated=true, never a silent truncation (spec 079 edge
	// case: long document / video).
	limited := io.LimitReader(resp.Body, maxReadBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return FetchResult{
			Fetched: false,
			Reason:  fmt.Sprintf("reading the body of %q failed: %v", ref, err),
		}, nil
	}
	truncated := false
	if len(raw) > maxReadBytes {
		raw = raw[:maxReadBytes]
		truncated = true
	}

	content, title := ExtractText(string(raw))
	item := IngestItem{
		SourceRef:     ref,
		Title:         title,
		Content:       content,
		Medium:        in.Medium,
		Trust:         in.Trust,
		FetchedAtUnix: time.Now().Unix(), // wall clock — legitimate in an ACTIVITY, never workflow code.
		Truncated:     truncated,
	}
	reason := fmt.Sprintf("fetched %q (HTTP %d, %d bytes read)", ref, resp.StatusCode, len(raw))
	if truncated {
		reason += " — bounded extract: source exceeds the read limit"
	}
	return FetchResult{Fetched: true, Item: item, Reason: reason}, nil
}

// ExtractText reads raw fetched bytes into normalized text content and a
// title. It is a deterministic, pure function — the read half of FR-004.
//
// PROMPT-INJECTION CONTAINMENT (FR-013): this function treats the input as
// untrusted DATA. It strips HTML tags and normalizes whitespace; it never
// interprets the text as instructions, never follows embedded directives,
// never executes anything. The output is plain text the filter SCORES — the
// filter, likewise, only matches against it (see filter.go). There is no path
// in this package by which fetched content becomes an instruction.
//
// SCOPE — P1: this is a minimal HTML-to-text reduction sufficient for the
// operator-fed path. A richer reader (readability extraction, video-transcript
// handling, PDF text) is a documented follow-up.
//
// TODO(spec 079): richer medium-aware reading — readability-style main-content
// extraction, video transcript ingestion, PDF/paper text extraction — so the
// bounded extract for a long medium (FR-004, edge case) is genuinely
// representative rather than a head-of-document slice.
func ExtractText(raw string) (content, title string) {
	if !utf8.ValidString(raw) {
		raw = strings.ToValidUTF8(raw, "")
	}
	title = extractTitle(raw)
	content = stripTags(raw)
	content = collapseWhitespace(content)
	return content, title
}

// extractTitle pulls the text of the first <title> element, trimmed. It
// returns "" when the source carries no title. Pure string scanning — no
// interpretation of the content.
func extractTitle(raw string) string {
	lower := strings.ToLower(raw)
	open := strings.Index(lower, "<title")
	if open < 0 {
		return ""
	}
	gt := strings.IndexByte(raw[open:], '>')
	if gt < 0 {
		return ""
	}
	start := open + gt + 1
	close := strings.Index(lower[start:], "</title>")
	if close < 0 {
		return ""
	}
	return collapseWhitespace(raw[start : start+close])
}

// stripTags removes everything between '<' and '>' — a deterministic
// tag-stripper. Tags are DROPPED, not interpreted: <script>, <style>, and any
// other tag's surrounding markup are removed as data. Content inside <script>
// / <style> elements is also dropped so it is never scored as readable text.
func stripTags(raw string) string {
	// Drop <script>...</script> and <style>...</style> bodies wholesale.
	raw = dropElement(raw, "script")
	raw = dropElement(raw, "style")

	var b strings.Builder
	b.Grow(len(raw))
	inTag := false
	for _, r := range raw {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// dropElement removes every <name>...</name> element (body included) from raw.
// It is used to drop <script> and <style> so their contents never reach the
// readable text — containment, not execution.
func dropElement(raw, name string) string {
	lower := strings.ToLower(raw)
	openTag := "<" + name
	closeTag := "</" + name + ">"
	for {
		open := strings.Index(lower, openTag)
		if open < 0 {
			return raw
		}
		closeIdx := strings.Index(lower[open:], closeTag)
		if closeIdx < 0 {
			// Unclosed element — drop from the open tag to the end.
			return raw[:open]
		}
		end := open + closeIdx + len(closeTag)
		raw = raw[:open] + raw[end:]
		lower = strings.ToLower(raw)
	}
}

// collapseWhitespace trims and collapses runs of whitespace to single spaces —
// a deterministic normalization so two readings of the same source produce
// byte-identical Content (and therefore identical filter scores; FR-009).
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
