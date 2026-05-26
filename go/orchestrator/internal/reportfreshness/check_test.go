package reportfreshness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()

	fresh := writeReport(t, dir, "fresh.html", "", now.Add(-1*time.Hour))
	stale := writeReport(t, dir, "stale.html", "", now.Add(-100*time.Hour))
	missing := filepath.Join(dir, "missing.html")
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	embeddedFresh := writeReport(t, dir, "embedded-fresh.html",
		`<!-- chitin-report-meta: {"generated_at":"2026-05-26T11:30:00Z"} -->`, now.Add(-7*24*time.Hour))
	embeddedStale := writeReport(t, dir, "embedded-stale.html",
		`<!-- chitin-report-meta: {"generated_at":"2026-05-22T08:00:00Z"} -->`, now.Add(-1*time.Hour))

	res, err := Check(context.Background(), []WatchedPath{
		{Path: fresh, SLAHours: 24},
		{Path: stale, SLAHours: 24},
		{Path: missing, SLAHours: 24},
		{Path: subdir, SLAHours: 24},
		{Path: embeddedFresh, SLAHours: 24},
		{Path: embeddedStale, SLAHours: 24},
	}, now)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	byPath := map[string]ReportStatus{}
	for _, row := range res.Rows {
		byPath[row.Path] = row
	}
	assertStatus(t, byPath[fresh], StatusFresh, AgeSourceMTime)
	assertStatus(t, byPath[stale], StatusStale, AgeSourceMTime)
	assertStatus(t, byPath[missing], StatusMissing, "")
	assertStatus(t, byPath[subdir], StatusMissing, "")
	assertStatus(t, byPath[embeddedFresh], StatusFresh, AgeSourceEmbedded)
	assertStatus(t, byPath[embeddedStale], StatusStale, AgeSourceEmbedded)
	if len(res.Stale) != 2 {
		t.Fatalf("stale count = %d, want 2", len(res.Stale))
	}
	if len(res.Missing) != 2 {
		t.Fatalf("missing count = %d, want 2", len(res.Missing))
	}
}

func writeReport(t *testing.T, dir, name, body string, mtime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertStatus(t *testing.T, row ReportStatus, status Status, source string) {
	t.Helper()
	if row.Status != status || row.AgeSource != source {
		t.Fatalf("%s status/source = %s/%s, want %s/%s", row.Path, row.Status, row.AgeSource, status, source)
	}
}
