package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// SnapshotHashRef returns the SHA-256 hex hash of the canonical JSON
// serialization of a PRSnapshot's content-bearing fields (Files +
// SpecArtifacts). It is the FR-032 audit anchor: every ReviewerInvocation
// records this hash in its telemetry event so an external observer can
// confirm two invocations saw the same PR state without re-shipping the
// diff content.
//
// The hash is over Files and SpecArtifacts only (not Title, Body, Author,
// etc.) because those are the fields a reviewer's verdict semantically
// depends on. Order is canonicalized by sorting Files by Path and
// SpecArtifacts by Path before hashing, so two snapshots that differ only
// in field order hash identically.
func SnapshotHashRef(s PRSnapshot) string {
	// Copy the content-bearing fields into a deterministic representation.
	type fileLine struct {
		Path      string `json:"path"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
		Diff      string `json:"diff"`
	}
	type specLine struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	type canonical struct {
		HeadOID       string     `json:"head_oid"`
		Files         []fileLine `json:"files"`
		SpecArtifacts []specLine `json:"spec_artifacts"`
	}

	files := make([]fileLine, len(s.Files))
	for i, f := range s.Files {
		files[i] = fileLine{Path: f.Path, Additions: f.Additions, Deletions: f.Deletions, Diff: f.Diff}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	specs := make([]specLine, len(s.SpecArtifacts))
	for i, a := range s.SpecArtifacts {
		specs[i] = specLine{Path: a.Path, Content: a.Content}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Path < specs[j].Path })

	canon := canonical{HeadOID: s.HeadOID, Files: files, SpecArtifacts: specs}
	buf, err := json.Marshal(canon)
	if err != nil {
		// Marshalling a value-only struct with only string/int fields cannot
		// fail in practice; if it ever does, return an empty hash rather
		// than panic.
		return ""
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}
