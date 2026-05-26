package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReportsCheckAndList(t *testing.T) {
	now := time.Now().UTC()
	dir := t.TempDir()
	fresh := writeCLIReport(t, dir, "fresh.html", now.Add(-1*time.Hour))
	stale := writeCLIReport(t, dir, "stale.html", now.Add(-100*time.Hour))
	config := filepath.Join(dir, "report-freshness.yaml")
	if err := os.WriteFile(config, []byte("paths:\n  - path: "+fresh+"\n    sla_hours: 24\n  - path: "+stale+"\n    sla_hours: 24\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runReportsCheck(t.Context(), []string{"--config", config}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("reports check exit = %d, want 2; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"PATH", "AGE_HRS", "SLA_HRS", "STATUS", fresh, stale, "fresh", "stale"} {
		if !strings.Contains(out, want) {
			t.Fatalf("reports check output missing %q:\n%s", want, out)
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = runReportsList([]string{"--config", config}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("reports list exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out = stdout.String()
	if !strings.Contains(out, fresh) || !strings.Contains(out, stale) || !strings.Contains(out, "SLA_HOURS") {
		t.Fatalf("reports list output unexpected:\n%s", out)
	}
}

func TestSchedulesListIncludesReportFreshnessCanary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSchedules([]string{"list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("schedules list exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "report-freshness-canary") {
		t.Fatalf("schedules list missing report-freshness-canary:\n%s", stdout.String())
	}
}

func writeCLIReport(t *testing.T, dir, name string, mtime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}
