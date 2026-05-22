package ingest

import "testing"

// Spec 079 FR-014 tests for deduplication. The dedup logic is wired into the
// US1 path (an operator resubmission is a no-op surface) and is the shared
// key US2's broad-net gathering will use against the knowledge base — the
// live-read-model wiring is the documented T018 TODO in dedup.go.

// TestCanonicalRef_FoldsEquivalentURLs proves two URLs that point at the same
// source canonicalize to the same key — so they are recognized as duplicates.
func TestCanonicalRef_FoldsEquivalentURLs(t *testing.T) {
	same := []string{
		"https://example.com/post",
		"https://example.com/post/",
		"https://www.example.com/post",
		"HTTPS://Example.COM/post",
		"https://example.com/post#section-2",
	}
	want := CanonicalRef(same[0])
	for _, u := range same[1:] {
		if got := CanonicalRef(u); got != want {
			t.Errorf("CanonicalRef(%q) = %q, want %q — equivalent URLs must fold to one key", u, got, want)
		}
	}
}

// TestCanonicalRef_KeepsDistinctURLsDistinct proves canonicalization is
// conservative — genuinely different sources stay different (a path is
// case-sensitive; a query param may identify a distinct resource).
func TestCanonicalRef_KeepsDistinctURLsDistinct(t *testing.T) {
	if CanonicalRef("https://example.com/PostA") == CanonicalRef("https://example.com/posta") {
		t.Error("paths are case-sensitive — distinct paths must not fold")
	}
	if CanonicalRef("https://example.com/p?id=1") == CanonicalRef("https://example.com/p?id=2") {
		t.Error("a query parameter may identify a distinct resource — must not fold")
	}
}

// TestKnownRefs_IsDuplicate proves FR-014: a source already in the knowledge
// base is recognized as a duplicate via the canonical key.
func TestKnownRefs_IsDuplicate(t *testing.T) {
	known := NewKnownRefs([]string{"https://example.com/already-ingested"})

	if !known.IsDuplicate("https://www.example.com/already-ingested/") {
		t.Error("an equivalent form of a known source must be detected as a duplicate (FR-014)")
	}
	if known.IsDuplicate("https://example.com/brand-new") {
		t.Error("a never-seen source must not be flagged as a duplicate")
	}
}

// TestKnownRefs_Add proves a source surfaced mid-run is recognized as a
// duplicate by a later candidate in the same run (FR-014).
func TestKnownRefs_Add(t *testing.T) {
	known := NewKnownRefs(nil)
	if known.IsDuplicate("https://example.com/x") {
		t.Fatal("an empty set must flag nothing")
	}
	known.Add("https://example.com/x")
	if !known.IsDuplicate("https://example.com/x") {
		t.Error("a just-added source must be detected as a duplicate")
	}
}

// TestKnownRefs_EmptyRefIgnored proves an empty or unparseable ref is never a
// false-positive duplicate.
func TestKnownRefs_EmptyRefIgnored(t *testing.T) {
	known := NewKnownRefs([]string{"", "   "})
	if known.IsDuplicate("") {
		t.Error("an empty ref must not be a duplicate")
	}
	if len(known) != 0 {
		t.Errorf("empty refs must not enter the set, got %d entries", len(known))
	}
}
