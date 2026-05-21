package ingest

import "strings"

// Deduplication against the knowledge base (FR-014, spec 079 edge case "a
// source already in the knowledge base"). A gathered candidate already
// present in the knowledge base MUST be deduplicated rather than re-ingested.
//
// The deduplication logic is PURE — no Temporal import, no I/O — so it can be
// exhaustively unit-tested by `go test`; it is dispatched as a spec-076
// `deterministic` node (FR-017, SC-008), never a frontier agent.
//
// SCOPE — P1: deduplication matters most for US2's broad-net gathering, where
// many candidates may collide with existing knowledge. For the US1
// operator-fed path the operator submits one item at a time, so collision is
// rare — but the dedup CHECK is wired into the US1 workflow already (an
// already-ingested operator resubmission is a no-op surface), and the
// canonicalization below is the shared key both paths use.
//
// TODO(spec 079 US2 / T018): wire IsDuplicate against the LIVE knowledge base
// read-model rather than an in-memory set. The knowledge base's storage and
// retrieval design is spec 078's surface (spec 079 Out of Scope) — when that
// read-model exists, the gathering workflow queries it for the set of known
// SourceRefs and passes them here. The matching logic in this file does not
// change; only its input source does.

// CanonicalRef normalizes a source reference into the stable key dedup
// matches on. Two URLs that point at the same source — differing only in
// trailing slash, scheme case, host case, or a "www." prefix — MUST canonical-
// ize to the same key so they are recognized as duplicates (FR-014).
//
// It is deterministic and pure: the same input always yields the same key.
// SCOPE — P1: a deliberately conservative normalization. It does NOT strip
// query strings or fragments — two URLs differing in a query parameter may
// be genuinely different resources, and over-aggressive canonicalization
// would merge distinct sources. A richer canonicalization (tracking-param
// stripping, known-equivalent host folding) is a documented follow-up.
func CanonicalRef(ref string) string {
	r := strings.TrimSpace(ref)
	if r == "" {
		return ""
	}
	// Drop a URL fragment — "#section" never identifies a distinct source.
	if h := strings.IndexByte(r, '#'); h >= 0 {
		r = r[:h]
	}
	// Lower-case the scheme and host without lower-casing the path (paths can
	// be case-sensitive). Split on the first "://".
	if i := strings.Index(r, "://"); i >= 0 {
		scheme := strings.ToLower(r[:i])
		rest := r[i+3:]
		// The host runs up to the first '/'.
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			rest = strings.ToLower(rest)
		} else {
			rest = strings.ToLower(rest[:slash]) + rest[slash:]
		}
		r = scheme + "://" + rest
	}
	// Fold a leading "www." host prefix.
	r = strings.Replace(r, "://www.", "://", 1)
	// Drop a single trailing slash so ".../post" and ".../post/" match.
	r = strings.TrimRight(r, "/")
	return r
}

// KnownRefs is the set of source references already in the knowledge base,
// keyed by their canonical form. It is the input to IsDuplicate.
//
// For US1 it is typically empty or tiny (the operator feeds one item at a
// time). For US2 it is populated from the knowledge-base read-model — the
// T018 TODO above.
type KnownRefs map[string]struct{}

// NewKnownRefs builds a KnownRefs set from a slice of existing source
// references, canonicalizing each. It is the constructor the ingestion
// workflow uses to turn a list of already-surfaced SourceRefs into a dedup
// lookup.
func NewKnownRefs(existing []string) KnownRefs {
	set := make(KnownRefs, len(existing))
	for _, ref := range existing {
		key := CanonicalRef(ref)
		if key == "" {
			continue
		}
		set[key] = struct{}{}
	}
	return set
}

// IsDuplicate reports whether ref is already present in the knowledge base —
// the FR-014 check. It canonicalizes ref and tests set membership; the
// comparison is deterministic. A duplicate candidate is skipped rather than
// re-ingested (edge case "a source already in the knowledge base").
func (k KnownRefs) IsDuplicate(ref string) bool {
	key := CanonicalRef(ref)
	if key == "" {
		return false
	}
	_, ok := k[key]
	return ok
}

// Add records ref as now-present in the knowledge base. The ingestion
// workflow calls it after surfacing a kept item so a later candidate in the
// SAME run that points at the just-surfaced source is recognized as a
// duplicate (FR-014).
func (k KnownRefs) Add(ref string) {
	key := CanonicalRef(ref)
	if key == "" {
		return
	}
	k[key] = struct{}{}
}
