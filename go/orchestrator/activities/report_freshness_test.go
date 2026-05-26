package activities

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckReportFreshness_EmitsExpectedEvents(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "0")
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	fresh := writeActivityReport(t, dir, "fresh.html", now.Add(-1*time.Hour))
	stale := writeActivityReport(t, dir, "stale.html", now.Add(-100*time.Hour))
	missing := filepath.Join(dir, "missing.html")
	config := writeReportConfig(t, dir, fresh, stale, missing)

	sink := &MemoryReportChainSink{}
	notifier := &captureReportNotifier{}
	act := NewCheckReportFreshness(sink, notifier)
	act.now = func() time.Time { return now }

	out, err := act.Execute(context.Background(), CheckReportFreshnessInput{
		PathsConfigPath: config,
		Cadence:         "manual",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Checked != 3 || len(out.Stale) != 1 || len(out.Missing) != 1 {
		t.Fatalf("out = %+v, want checked=3 stale=1 missing=1", out)
	}
	gotTypes := eventTypes(sink.Events)
	wantTypes := []string{EventReportMissing, EventStaleReportDetected, EventStaleReportEscalated, EventReportFresh}
	if !sameStrings(gotTypes, wantTypes) {
		t.Fatalf("event types = %v, want %v", gotTypes, wantTypes)
	}
	var detected StaleReportDetectedPayload
	decodePayload(t, sink.Events[1], &detected)
	if detected.Path != stale || detected.AgeSource != "mtime" || detected.SLAHours != 24 {
		t.Fatalf("detected payload = %+v", detected)
	}
	var summary ReportFreshPayload
	decodePayload(t, sink.Events[3], &summary)
	if summary.CheckedCount != 3 || summary.FreshCount != 1 || summary.StaleCount != 1 || summary.MissingCount != 1 || summary.Cadence != "manual" {
		t.Fatalf("summary payload = %+v", summary)
	}
	if len(notifier.events) != 1 {
		t.Fatalf("notifications = %d, want 1", len(notifier.events))
	}
}

func TestCheckReportFreshness_RateLimitsRepeatedEscalations(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "0")
	start := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	stale := writeActivityReport(t, dir, "stale.html", start.Add(-100*time.Hour))
	config := filepath.Join(dir, "report-freshness.yaml")
	if err := os.WriteFile(config, []byte("paths:\n  - path: "+stale+"\n    sla_hours: 24\nescalation_cooldown_hours: 24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &MemoryReportChainSink{}
	notifier := &captureReportNotifier{}
	act := NewCheckReportFreshness(sink, notifier)

	for i := 0; i < 5; i++ {
		now := start.Add(time.Duration(i) * 15 * time.Minute)
		_, err := act.Execute(context.Background(), CheckReportFreshnessInput{PathsConfigPath: config, Now: now})
		if err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}

	escalated := countEvents(sink.Events, EventStaleReportEscalated)
	suppressed := filterEvents(sink.Events, EventStaleReportSuppressed)
	if escalated != 1 || len(suppressed) != 4 {
		t.Fatalf("escalated=%d suppressed=%d, want 1/4; events=%v", escalated, len(suppressed), eventTypes(sink.Events))
	}
	if len(notifier.events) != 1 {
		t.Fatalf("notifications = %d, want 1", len(notifier.events))
	}
	prevRemaining := 25.0
	for i, ev := range suppressed {
		var payload StaleReportSuppressedPayload
		decodePayload(t, ev, &payload)
		if payload.SuppressedCount != i+1 {
			t.Fatalf("suppressed_count[%d] = %d, want %d", i, payload.SuppressedCount, i+1)
		}
		if payload.CooldownRemainingHours >= prevRemaining {
			t.Fatalf("cooldown did not decrease: %.2f after %.2f", payload.CooldownRemainingHours, prevRemaining)
		}
		prevRemaining = payload.CooldownRemainingHours
	}
}

type captureReportNotifier struct {
	events []NotificationEvent
}

func (c *captureReportNotifier) Notify(_ context.Context, ev NotificationEvent) error {
	c.events = append(c.events, ev)
	return nil
}

func writeActivityReport(t *testing.T, dir, name string, mtime time.Time) string {
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

func writeReportConfig(t *testing.T, dir string, paths ...string) string {
	t.Helper()
	body := "paths:\n"
	for _, p := range paths {
		body += "  - path: " + p + "\n    sla_hours: 24\n"
	}
	path := filepath.Join(dir, "report-freshness.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func eventTypes(events []ReportChainEvent) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.EventType)
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func decodePayload(t *testing.T, ev ReportChainEvent, out any) {
	t.Helper()
	if err := json.Unmarshal(ev.Payload, out); err != nil {
		t.Fatalf("decode %s: %v", ev.EventType, err)
	}
}

func countEvents(events []ReportChainEvent, eventType string) int {
	return len(filterEvents(events, eventType))
}

func filterEvents(events []ReportChainEvent, eventType string) []ReportChainEvent {
	var out []ReportChainEvent
	for _, ev := range events {
		if ev.EventType == eventType {
			out = append(out, ev)
		}
	}
	return out
}
