package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeGhRunner is the test injection for the CapturePRSnapshot activity.
// Each test composes a response map keyed by the first arg of the gh
// call (e.g., "pr view 953 ...") and the runner returns the matched
// stdout or a synthetic error. The matcher uses a substring on the
// full args slice joined by spaces so callers can be precise without
// reconstructing exact argv ordering.
type fakeGhRunner struct {
	// responses maps a substring of the joined args to the stdout to
	// return. The first matching key (in iteration order) wins; tests
	// that need ordered matches register a single key per call.
	responses []fakeGhResponse
}

type fakeGhResponse struct {
	matchSubstr string
	stdout      string
	err         error
}

func (f *fakeGhRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	joined := strings.Join(args, " ")
	for _, r := range f.responses {
		if strings.Contains(joined, r.matchSubstr) {
			if r.err != nil {
				return nil, r.err
			}
			return []byte(r.stdout), nil
		}
	}
	return nil, fmt.Errorf("fakeGhRunner: no response registered for %q", joined)
}

// TestCapturePRSnapshot_HappyPath covers the dominant case from spec 094
// R-SNAP: a PR that touches one impl file and one spec-kit artifact. The
// snapshot must include both in Files[] (with diff text split correctly)
// and the spec-kit one ALSO in SpecArtifacts[] (with content fetched
// from the gh contents API).
func TestCapturePRSnapshot_HappyPath(t *testing.T) {
	viewJSON := `{
		"title": "feat: thing",
		"body": "Closes #1",
		"headRefOid": "abc123",
		"baseRefName": "main",
		"author": {"login": "claudia-bot"},
		"files": [
			{"path": "go/orchestrator/foo.go", "additions": 10, "deletions": 2},
			{"path": ".specify/specs/100-thing/spec.md", "additions": 50, "deletions": 0}
		]
	}`
	diff := "diff --git a/go/orchestrator/foo.go b/go/orchestrator/foo.go\n" +
		"index 1..2 100644\n" +
		"--- a/go/orchestrator/foo.go\n" +
		"+++ b/go/orchestrator/foo.go\n" +
		"@@ -1,2 +1,10 @@\n" +
		"+new line\n" +
		"diff --git a/.specify/specs/100-thing/spec.md b/.specify/specs/100-thing/spec.md\n" +
		"new file mode 100644\n" +
		"--- /dev/null\n" +
		"+++ b/.specify/specs/100-thing/spec.md\n" +
		"@@ -0,0 +1,50 @@\n" +
		"+# Feature Specification: thing\n"
	specContent := "# Feature Specification: thing\n\n... the post-PR file content ...\n"

	runner := &fakeGhRunner{responses: []fakeGhResponse{
		{matchSubstr: "pr view", stdout: viewJSON},
		{matchSubstr: "pr diff", stdout: diff},
		{matchSubstr: "contents/.specify/specs/100-thing/spec.md", stdout: specContent},
	}}
	a := NewCapturePRSnapshot(runner)
	snap, err := a.Execute(context.Background(), PRReviewInput{
		Repo: "chitinhq/chitin", PRNumber: 953, PRAuthor: "jpleva91",
		PolicyClass: "impl", ArbiterType: ArbiterMachine,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if snap.HeadOID != "abc123" {
		t.Errorf("HeadOID = %q, want abc123", snap.HeadOID)
	}
	if snap.Title != "feat: thing" || snap.Author != "claudia-bot" || snap.BaseRef != "main" {
		t.Errorf("metadata mismatch: %+v", snap)
	}
	if len(snap.Files) != 2 {
		t.Fatalf("Files len = %d, want 2", len(snap.Files))
	}
	if snap.Files[0].Path != "go/orchestrator/foo.go" || snap.Files[0].Additions != 10 || snap.Files[0].Deletions != 2 {
		t.Errorf("Files[0] mismatch: %+v", snap.Files[0])
	}
	if !strings.Contains(snap.Files[0].Diff, "+new line") {
		t.Errorf("Files[0].Diff missing impl-file hunk: %q", snap.Files[0].Diff)
	}
	if !strings.Contains(snap.Files[1].Diff, "+# Feature Specification: thing") {
		t.Errorf("Files[1].Diff missing spec hunk: %q", snap.Files[1].Diff)
	}
	if len(snap.SpecArtifacts) != 1 {
		t.Fatalf("SpecArtifacts len = %d, want 1", len(snap.SpecArtifacts))
	}
	if snap.SpecArtifacts[0].Path != ".specify/specs/100-thing/spec.md" {
		t.Errorf("SpecArtifacts[0].Path = %q", snap.SpecArtifacts[0].Path)
	}
	if !strings.Contains(snap.SpecArtifacts[0].Content, "post-PR file content") {
		t.Errorf("SpecArtifacts[0].Content not fetched: %q", snap.SpecArtifacts[0].Content)
	}
	if snap.CapturedAt.IsZero() {
		t.Error("CapturedAt is zero — should be set to time.Now().UTC()")
	}
}

// TestCapturePRSnapshot_NoSpecArtifacts confirms a PR that touches only
// implementation files yields an empty SpecArtifacts slice — and no
// gh-api call is attempted (registered fake would error if it were).
func TestCapturePRSnapshot_NoSpecArtifacts(t *testing.T) {
	viewJSON := `{
		"title": "fix: bug",
		"body": "",
		"headRefOid": "def456",
		"baseRefName": "main",
		"author": {"login": "j-pleva"},
		"files": [{"path": "go/foo.go", "additions": 3, "deletions": 1}]
	}`
	diff := "diff --git a/go/foo.go b/go/foo.go\n@@ -1 +1,3 @@\n+x\n+y\n+z\n"
	runner := &fakeGhRunner{responses: []fakeGhResponse{
		{matchSubstr: "pr view", stdout: viewJSON},
		{matchSubstr: "pr diff", stdout: diff},
		// no contents/ response — if the activity attempts one, the
		// runner returns "no response registered" which would fail.
	}}
	a := NewCapturePRSnapshot(runner)
	snap, err := a.Execute(context.Background(), PRReviewInput{
		Repo: "chitinhq/chitin", PRNumber: 1,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(snap.SpecArtifacts) != 0 {
		t.Errorf("SpecArtifacts = %+v, want empty for non-spec PR", snap.SpecArtifacts)
	}
}

// TestCapturePRSnapshot_PRViewFail confirms gh pr view failure surfaces
// as an activity error — the workflow halts the gate per FR-026.
func TestCapturePRSnapshot_PRViewFail(t *testing.T) {
	runner := &fakeGhRunner{responses: []fakeGhResponse{
		{matchSubstr: "pr view", err: errors.New("gh: pr not found")},
	}}
	a := NewCapturePRSnapshot(runner)
	_, err := a.Execute(context.Background(), PRReviewInput{Repo: "x/y", PRNumber: 1})
	if err == nil {
		t.Fatal("Execute returned nil error; expected wrapped gh failure")
	}
	if !strings.Contains(err.Error(), "gh pr view") {
		t.Errorf("error %q does not name failing step", err.Error())
	}
}

// TestCapturePRSnapshot_SpecArtifactFetchFail confirms a per-artifact
// fetch failure is silently skipped (best-effort) and the snapshot still
// returns — but with the failed artifact absent from SpecArtifacts.
// Files[] still includes the artifact's path so reviewer drivers can
// at least see WHICH spec the PR touched.
func TestCapturePRSnapshot_SpecArtifactFetchFail(t *testing.T) {
	viewJSON := `{
		"title": "t",
		"headRefOid": "abc",
		"baseRefName": "main",
		"author": {"login": "u"},
		"files": [{"path": ".specify/specs/100-x/spec.md", "additions": 1, "deletions": 0}]
	}`
	runner := &fakeGhRunner{responses: []fakeGhResponse{
		{matchSubstr: "pr view", stdout: viewJSON},
		{matchSubstr: "pr diff", stdout: "diff --git a/.specify/specs/100-x/spec.md b/.specify/specs/100-x/spec.md\n@@\n+x\n"},
		{matchSubstr: "contents/.specify/specs/100-x/spec.md", err: errors.New("gh: 404")},
	}}
	a := NewCapturePRSnapshot(runner)
	snap, err := a.Execute(context.Background(), PRReviewInput{Repo: "x/y", PRNumber: 1})
	if err != nil {
		t.Fatalf("Execute returned error %v; want nil (artifact fetch is best-effort)", err)
	}
	if len(snap.Files) != 1 {
		t.Errorf("Files len = %d, want 1", len(snap.Files))
	}
	if len(snap.SpecArtifacts) != 0 {
		t.Errorf("SpecArtifacts len = %d, want 0 (fetch failed)", len(snap.SpecArtifacts))
	}
}

// TestSnapshot_HashRefStability cross-checks that two snapshots
// captured from the same gh fixture produce the same SnapshotHashRef
// (the FR-032 audit anchor). Ordering of Files / SpecArtifacts must be
// canonicalized inside SnapshotHashRef so this test passes even if the
// underlying activity returned them in a different order.
func TestSnapshot_HashRefStability(t *testing.T) {
	viewJSON := `{
		"title": "t",
		"headRefOid": "h",
		"baseRefName": "main",
		"author": {"login": "u"},
		"files": [
			{"path": "b.go", "additions": 1, "deletions": 0},
			{"path": "a.go", "additions": 1, "deletions": 0}
		]
	}`
	diff := "diff --git a/b.go b/b.go\n@@\n+x\n" +
		"diff --git a/a.go b/a.go\n@@\n+y\n"
	runner := &fakeGhRunner{responses: []fakeGhResponse{
		{matchSubstr: "pr view", stdout: viewJSON},
		{matchSubstr: "pr diff", stdout: diff},
	}}
	a := NewCapturePRSnapshot(runner)
	snap1, err := a.Execute(context.Background(), PRReviewInput{Repo: "x/y", PRNumber: 1})
	if err != nil {
		t.Fatalf("Execute #1: %v", err)
	}
	snap2, err := a.Execute(context.Background(), PRReviewInput{Repo: "x/y", PRNumber: 1})
	if err != nil {
		t.Fatalf("Execute #2: %v", err)
	}
	if SnapshotHashRef(snap1) != SnapshotHashRef(snap2) {
		t.Errorf("hash refs differ across identical inputs: %q vs %q", SnapshotHashRef(snap1), SnapshotHashRef(snap2))
	}
}

// TestSplitUnifiedDiff covers the diff-splitter's key behaviors: per-file
// boundary detection, post-image path extraction, empty-input handling.
func TestSplitUnifiedDiff(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if got := splitUnifiedDiff(""); len(got) != 0 {
			t.Errorf("empty input → %v, want empty map", got)
		}
	})
	t.Run("two files", func(t *testing.T) {
		in := "diff --git a/x/foo.go b/x/foo.go\n@@ x\n+a\n" +
			"diff --git a/y/bar.go b/y/bar.go\n@@ y\n+b\n"
		got := splitUnifiedDiff(in)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if !strings.Contains(got["x/foo.go"], "+a") {
			t.Errorf("x/foo.go diff missing +a: %q", got["x/foo.go"])
		}
		if !strings.Contains(got["y/bar.go"], "+b") {
			t.Errorf("y/bar.go diff missing +b: %q", got["y/bar.go"])
		}
		if strings.Contains(got["x/foo.go"], "+b") {
			t.Errorf("x/foo.go diff contaminated with y/bar.go hunk: %q", got["x/foo.go"])
		}
	})
	t.Run("rename preserves b path", func(t *testing.T) {
		// gh pr diff for a rename: a/ side and b/ side differ. We key
		// by the b/ (post-image) path because gh pr view --json files
		// reports the new path.
		in := "diff --git a/old.go b/new.go\nrename from old.go\nrename to new.go\n"
		got := splitUnifiedDiff(in)
		if _, ok := got["new.go"]; !ok {
			t.Errorf("expected key new.go, got %v", got)
		}
		if _, ok := got["old.go"]; ok {
			t.Errorf("unexpected key old.go in %v", got)
		}
	})
}

// TestEncodeURLPath covers the per-segment URL encoding used to build
// the gh contents-API URL. The key invariant is: '/' separators are
// preserved (not encoded); reserved characters within a segment are
// percent-encoded. Without this, a spec file with '{' '}' '?' or ' '
// in its path would fail to fetch — silently, because the fetch
// returns 404 and the best-effort skip path swallows it.
func TestEncodeURLPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{".specify/specs/100-x/spec.md", ".specify/specs/100-x/spec.md"},
		{"path/with space/file.md", "path/with%20space/file.md"},
		{"a/b{c}.md", "a/b%7Bc%7D.md"},
		{"a/b?c.md", "a/b%3Fc.md"},
		{"a/b#c.md", "a/b%23c.md"},
		{"single-segment.md", "single-segment.md"},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := encodeURLPath(c.in); got != c.want {
				t.Errorf("encodeURLPath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestCapturePRSnapshot_PathWithSpecialChars exercises the end-to-end
// path-encoding integration: a spec file whose path contains a reserved
// character flows through the fake gh runner with a percent-encoded URL.
// Failure mode this test guards against: the activity asks gh for
// '.../contents/100-x/{thing}.md?ref=h' (literal braces) — most HTTP
// intermediaries strip them, gh returns 400 or 404, and the best-effort
// skip path swallows it silently. With per-segment escape the URL
// becomes '.../contents/100-x/%7Bthing%7D.md?ref=h' and the fetch
// succeeds.
func TestCapturePRSnapshot_PathWithSpecialChars(t *testing.T) {
	viewJSON := `{
		"title": "t",
		"headRefOid": "abc",
		"baseRefName": "main",
		"author": {"login": "u"},
		"files": [{"path": ".specify/specs/100-x/contracts/{thing}.md", "additions": 1, "deletions": 0}]
	}`
	diff := "diff --git a/.specify/specs/100-x/contracts/{thing}.md b/.specify/specs/100-x/contracts/{thing}.md\n@@\n+x\n"
	content := "raw spec contract content"
	// Match on the percent-encoded form — if the activity sends the
	// raw '{' '}' the substring match fails, the runner errors, and
	// the artifact is silently skipped (PathWithSpecialChars fails).
	runner := &fakeGhRunner{responses: []fakeGhResponse{
		{matchSubstr: "pr view", stdout: viewJSON},
		{matchSubstr: "pr diff", stdout: diff},
		{matchSubstr: "contracts/%7Bthing%7D.md", stdout: content},
	}}
	a := NewCapturePRSnapshot(runner)
	snap, err := a.Execute(context.Background(), PRReviewInput{Repo: "x/y", PRNumber: 1})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(snap.SpecArtifacts) != 1 {
		t.Fatalf("SpecArtifacts len = %d, want 1; the path was likely passed unencoded", len(snap.SpecArtifacts))
	}
	if snap.SpecArtifacts[0].Path != ".specify/specs/100-x/contracts/{thing}.md" {
		t.Errorf("SpecArtifacts[0].Path = %q, want unescaped %q",
			snap.SpecArtifacts[0].Path, ".specify/specs/100-x/contracts/{thing}.md")
	}
	if snap.SpecArtifacts[0].Content != content {
		t.Errorf("SpecArtifacts[0].Content = %q, want %q", snap.SpecArtifacts[0].Content, content)
	}
}

// TestCapDiff covers the per-file + total-budget truncation logic
// added after a real production failure: a 100-file cleanup PR produced
// a 7.1 MiB activity output that exceeded Temporal's 2 MiB payload
// limit, causing the dialectic to halt-retry-halt indefinitely.
//
// Invariants:
//   - small diff under both caps → returned verbatim
//   - diff over the per-file cap → truncated with marker; final length
//     fits in MaxPerFileDiffBytes
//   - cumulative budget exhausted → diff replaced with budget marker
//   - the per-file marker itself never exceeds the per-file cap
func TestCapDiff(t *testing.T) {
	small := strings.Repeat("x", 1000)
	t.Run("small fits verbatim", func(t *testing.T) {
		got, _ := capDiff(small, 0)
		if got != small {
			t.Errorf("small diff was modified: len=%d", len(got))
		}
	})

	huge := strings.Repeat("x", 100*1024) // 100 KiB
	t.Run("over per-file cap is truncated", func(t *testing.T) {
		got, origSize := capDiff(huge, 0)
		if origSize != len(huge) {
			t.Errorf("originalSize = %d, want %d", origSize, len(huge))
		}
		if len(got) > MaxPerFileDiffBytes {
			t.Errorf("truncated diff len=%d exceeds per-file cap=%d", len(got), MaxPerFileDiffBytes)
		}
		if !strings.HasSuffix(got, truncatedMarker) {
			t.Errorf("truncated diff missing marker; tail=%q", got[len(got)-80:])
		}
	})

	t.Run("budget exhausted returns empty", func(t *testing.T) {
		// Pretend we've already used the full budget.
		got, _ := capDiff(huge, MaxTotalDiffBytes)
		if got != "" {
			t.Errorf("got %q, want empty (budget exhausted)", got[:min(40, len(got))])
		}
	})

	t.Run("budget partially exhausted truncates harder", func(t *testing.T) {
		// 1 KiB left in the budget — much less than the per-file cap.
		got, _ := capDiff(huge, MaxTotalDiffBytes-1024)
		if len(got) > 1024 {
			t.Errorf("len=%d exceeds remaining budget=1024", len(got))
		}
	})

	t.Run("cumulative budget enforced across many files", func(t *testing.T) {
		fileSize := 30 * 1024 // each file just under per-file cap
		filesThatFit := MaxTotalDiffBytes / fileSize
		used := 0
		for i := 0; i <= filesThatFit+5; i++ {
			diff, _ := capDiff(strings.Repeat("y", fileSize), used)
			used += len(diff)
			// Post-budget files return empty, so used never grows past
			// MaxTotalDiffBytes (the boundary file may add a marker, but
			// capDiff sizes the boundary cut so cut+marker <= remaining).
			if used > MaxTotalDiffBytes {
				t.Fatalf("after %d files, total bytes=%d exceeded budget %d", i, used, MaxTotalDiffBytes)
			}
		}
	})
}

// TestCapturePRSnapshot_LargePRPayload integration-tests the cap path
// against the regression that triggered this fix: a PR with many files
// whose total raw diff is multi-megabyte. The snapshot's total
// Files[].Diff size MUST stay under MaxTotalDiffBytes so the activity
// output fits Temporal's 2 MiB payload limit.
func TestCapturePRSnapshot_LargePRPayload(t *testing.T) {
	// 50 files × 100 KiB each = 5 MiB raw — well over the limit.
	const nFiles = 50
	const perFileSize = 100 * 1024

	var filesJSON []string
	var diffParts []string
	for i := 0; i < nFiles; i++ {
		filesJSON = append(filesJSON, fmt.Sprintf(`{"path": "file%d.go", "additions": 1, "deletions": 0}`, i))
		// Per-file unified diff section; the payload is what blows up.
		body := strings.Repeat("y", perFileSize)
		diffParts = append(diffParts, fmt.Sprintf("diff --git a/file%d.go b/file%d.go\n@@\n+%s\n", i, i, body))
	}
	viewJSON := fmt.Sprintf(`{
		"title": "huge cleanup",
		"headRefOid": "abc",
		"baseRefName": "main",
		"author": {"login": "u"},
		"files": [%s]
	}`, strings.Join(filesJSON, ","))
	diff := strings.Join(diffParts, "")

	runner := &fakeGhRunner{responses: []fakeGhResponse{
		{matchSubstr: "pr view", stdout: viewJSON},
		{matchSubstr: "pr diff", stdout: diff},
	}}
	a := NewCapturePRSnapshot(runner)
	snap, err := a.Execute(context.Background(), PRReviewInput{Repo: "x/y", PRNumber: 1})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Every file's path is still present — the reviewer sees the full
	// list of changed files even when content is truncated.
	if len(snap.Files) != nFiles {
		t.Errorf("Files len = %d, want %d", len(snap.Files), nFiles)
	}

	// The aggregate Diff payload size MUST fit under the cap.
	totalDiffBytes := 0
	for _, f := range snap.Files {
		totalDiffBytes += len(f.Diff)
	}
	if totalDiffBytes > MaxTotalDiffBytes {
		t.Errorf("aggregate diff bytes = %d, exceeds cap %d", totalDiffBytes, MaxTotalDiffBytes)
	}

	// At least one file should carry the per-file truncation marker
	// (the boundary file where budget ran out); subsequent files have
	// empty Diff (budget-exhausted, no marker).
	sawTruncMarker := false
	emptyDiffCount := 0
	for _, f := range snap.Files {
		if strings.HasSuffix(f.Diff, truncatedMarker) {
			sawTruncMarker = true
		}
		if f.Diff == "" {
			emptyDiffCount++
		}
	}
	if !sawTruncMarker {
		t.Errorf("expected at least one file with truncated marker; saw %d empty-diff files across %d total",
			emptyDiffCount, nFiles)
	}
	if emptyDiffCount == 0 {
		t.Errorf("expected at least one budget-exhausted (empty Diff) file in a %d-MiB synthetic PR",
			(nFiles*perFileSize)/1024/1024)
	}
}


// TestCapturePRSnapshot_RequiresRepoAndPR ensures the activity rejects
// the obvious caller bug of an empty Repo or zero PRNumber before
// spending a gh call. Cheap defensive validation per spec 097 plan's
// "fail user-error before network".
func TestCapturePRSnapshot_RequiresRepoAndPR(t *testing.T) {
	a := NewCapturePRSnapshot(&fakeGhRunner{}) // empty runner; would error on Run
	if _, err := a.Execute(context.Background(), PRReviewInput{PRNumber: 1}); err == nil {
		t.Error("empty Repo: expected error, got nil")
	}
	if _, err := a.Execute(context.Background(), PRReviewInput{Repo: "x/y", PRNumber: 0}); err == nil {
		t.Error("zero PRNumber: expected error, got nil")
	}
	if _, err := a.Execute(context.Background(), PRReviewInput{Repo: "x/y", PRNumber: -5}); err == nil {
		t.Error("negative PRNumber: expected error, got nil")
	}
}
