package activities

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/internal/reportfreshness"
)

const (
	EventStaleReportDetected   = "stale_report_detected"
	EventReportFresh           = "report_fresh"
	EventReportMissing         = "report_missing"
	EventStaleReportEscalated  = "stale_report_escalated"
	EventStaleReportSuppressed = "stale_report_suppressed"
)

const NotifyStaleReport NotificationKind = "stale-report"

type CheckReportFreshnessInput struct {
	PathsConfigPath string    `json:"paths_config_path"`
	Cadence         string    `json:"cadence,omitempty"`
	Now             time.Time `json:"now,omitempty"`
}

type CheckReportFreshnessOutput struct {
	Checked int                           `json:"checked"`
	Stale   []reportfreshness.StaleReport `json:"stale"`
	Missing []string                      `json:"missing"`
}

type StaleReportDetectedPayload struct {
	Path        string  `json:"path"`
	GeneratedAt string  `json:"generated_at"`
	AgeHours    float64 `json:"age_hours"`
	SLAHours    int     `json:"sla_hours"`
	AgeSource   string  `json:"age_source"`
}

type ReportFreshPayload struct {
	CheckedCount int    `json:"checked_count"`
	FreshCount   int    `json:"fresh_count"`
	StaleCount   int    `json:"stale_count"`
	MissingCount int    `json:"missing_count"`
	Cadence      string `json:"cadence"`
	ClockSkew    bool   `json:"clock_skew"`
}

type ReportMissingPayload struct {
	Path     string `json:"path"`
	SLAHours int    `json:"sla_hours"`
}

type StaleReportEscalatedPayload struct {
	Path            string  `json:"path"`
	AgeHours        float64 `json:"age_hours"`
	SLAHours        int     `json:"sla_hours"`
	NotifyMessageID string  `json:"notify_message_id"`
}

type StaleReportSuppressedPayload struct {
	Path                   string  `json:"path"`
	AgeHours               float64 `json:"age_hours"`
	SLAHours               int     `json:"sla_hours"`
	CooldownRemainingHours float64 `json:"cooldown_remaining_hours"`
	PriorEscalationAt      string  `json:"prior_escalation_at"`
	SuppressedCount        int     `json:"suppressed_count"`
}

type ReportChainEvent struct {
	EventType string          `json:"event_type"`
	Ts        time.Time       `json:"ts"`
	Payload   json.RawMessage `json:"payload"`
}

type ReportChainSink interface {
	// Emit appends one chain event. runID groups every event from a single
	// activity execution under the same chain run, so a downstream chain
	// query can stitch the cycle's detection → escalation/suppression chain
	// back together. eventType, payload, and ts are the event itself.
	Emit(ctx context.Context, runID, eventType string, payload any, ts time.Time) error
	LastEscalation(ctx context.Context, path string, since time.Time) (time.Time, bool, error)
	SuppressionCount(ctx context.Context, path string, since time.Time) (int, error)
}

type CheckReportFreshness struct {
	sink     ReportChainSink
	notifier Notifier
	now      func() time.Time
}

func NewCheckReportFreshness(sink ReportChainSink, notifier Notifier) *CheckReportFreshness {
	if sink == nil {
		sink = KernelReportChainSink{}
	}
	if notifier == nil {
		notifier = NewLogNotifier()
	}
	return &CheckReportFreshness{
		sink:     sink,
		notifier: notifier,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (a *CheckReportFreshness) ActivityName() string { return "CheckReportFreshness" }

func (a *CheckReportFreshness) Execute(ctx context.Context, in CheckReportFreshnessInput) (CheckReportFreshnessOutput, error) {
	now := in.Now
	if now.IsZero() {
		now = a.now()
	}
	cadence := in.Cadence
	if cadence == "" {
		cadence = "scheduled"
	}
	// runID groups this cycle's events under one chain run. Using the
	// activity start time keeps it deterministic for a given Now and stable
	// across all emits in the cycle.
	runID := fmt.Sprintf("report-freshness-%d", now.UnixNano())
	cfg, err := reportfreshness.LoadConfigOrDefault(in.PathsConfigPath)
	if err != nil {
		return CheckReportFreshnessOutput{}, err
	}
	res, err := reportfreshness.Check(ctx, cfg.Paths, now)
	if err != nil {
		return CheckReportFreshnessOutput{}, err
	}
	for _, row := range res.Rows {
		if row.Status == reportfreshness.StatusMissing {
			_ = a.emit(ctx, runID, EventReportMissing, ReportMissingPayload{
				Path:     row.Path,
				SLAHours: row.SLAHours,
			}, now)
		}
	}
	for _, stale := range res.Stale {
		detected := StaleReportDetectedPayload{
			Path:        stale.Path,
			GeneratedAt: stale.GeneratedAt.UTC().Format(time.RFC3339),
			AgeHours:    stale.AgeHours,
			SLAHours:    stale.SLAHours,
			AgeSource:   stale.AgeSource,
		}
		_ = a.emit(ctx, runID, EventStaleReportDetected, detected, now)
		if err := a.routeStaleReport(ctx, runID, stale, cfg.EscalationCooldownHours, now); err != nil {
			log.Printf("report freshness: route stale report %s: %v", stale.Path, err)
		}
	}
	_ = a.emit(ctx, runID, EventReportFresh, ReportFreshPayload{
		CheckedCount: res.Checked,
		FreshCount:   len(res.Fresh),
		StaleCount:   len(res.Stale),
		MissingCount: len(res.Missing),
		Cadence:      cadence,
		ClockSkew:    res.ClockSkew,
	}, now)
	return CheckReportFreshnessOutput{Checked: res.Checked, Stale: res.Stale, Missing: res.Missing}, nil
}

func (a *CheckReportFreshness) emit(ctx context.Context, runID, eventType string, payload any, ts time.Time) error {
	if os.Getenv("CHITIN_DISABLE_CHAIN_EMIT") == "1" {
		return nil
	}
	return a.sink.Emit(ctx, runID, eventType, payload, ts)
}

func (a *CheckReportFreshness) routeStaleReport(ctx context.Context, runID string, stale reportfreshness.StaleReport, cooldownHours int, now time.Time) error {
	if cooldownHours <= 0 {
		cooldownHours = reportfreshness.DefaultEscalationCooldownHours
	}
	since := now.Add(-time.Duration(cooldownHours) * time.Hour)
	if prior, ok, err := a.sink.LastEscalation(ctx, stale.Path, since); err != nil {
		return err
	} else if ok {
		suppressedCount, err := a.sink.SuppressionCount(ctx, stale.Path, prior)
		if err != nil {
			return err
		}
		payload := StaleReportSuppressedPayload{
			Path:                   stale.Path,
			AgeHours:               stale.AgeHours,
			SLAHours:               stale.SLAHours,
			CooldownRemainingHours: prior.Add(time.Duration(cooldownHours) * time.Hour).Sub(now).Hours(),
			PriorEscalationAt:      prior.UTC().Format(time.RFC3339),
			SuppressedCount:        suppressedCount + 1,
		}
		return a.emit(ctx, runID, EventStaleReportSuppressed, payload, now)
	}
	msg := fmt.Sprintf("stale report: %s age=%.1fh sla=%dh source=%s url=file://%s",
		stale.Path, stale.AgeHours, stale.SLAHours, stale.AgeSource, stale.Path)
	_ = a.notifier.Notify(ctx, NotificationEvent{
		Kind:    NotifyStaleReport,
		Summary: msg,
		URL:     "file://" + stale.Path,
	})
	return a.emit(ctx, runID, EventStaleReportEscalated, StaleReportEscalatedPayload{
		Path:            stale.Path,
		AgeHours:        stale.AgeHours,
		SLAHours:        stale.SLAHours,
		NotifyMessageID: "",
	}, now)
}

type KernelReportChainSink struct{}

func (KernelReportChainSink) Emit(ctx context.Context, runID, eventType string, payload any, ts time.Time) error {
	envelope := map[string]any{
		"schema_version":    "2",
		"event_type":        eventType,
		"run_id":            runID,
		"session_id":        "chitin-orchestrator-" + runID,
		"surface":           "chitin-orchestrator",
		"agent_instance_id": "chitin-orchestrator",
		// Matches deliver.go / sibling_rebase.go — the report-freshness activity
		// is scheduler-driven, not operator-CLI driven.
		"chain_type": "scheduler",
		"ts":         ts.UTC().Format(time.RFC3339Nano),
		"payload":    payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp("", "chitin-report-freshness-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.Write(body); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	bin := os.Getenv("CHITIN_KERNEL_BIN")
	if bin == "" {
		bin = "chitin-kernel"
	}
	emitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(emitCtx, bin, "emit", "-dir", reportChainDir(), "-event-file", tmpPath)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Printf("report freshness: chain emit failed: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (KernelReportChainSink) LastEscalation(_ context.Context, path string, since time.Time) (time.Time, bool, error) {
	events, err := scanReportEvents(reportChainDir(), since)
	if err != nil {
		return time.Time{}, false, err
	}
	var latest time.Time
	for _, ev := range events {
		if ev.EventType != EventStaleReportEscalated {
			continue
		}
		var payload StaleReportEscalatedPayload
		if json.Unmarshal(ev.Payload, &payload) != nil || payload.Path != path {
			continue
		}
		if ev.Ts.After(latest) {
			latest = ev.Ts
		}
	}
	return latest, !latest.IsZero(), nil
}

func (KernelReportChainSink) SuppressionCount(_ context.Context, path string, since time.Time) (int, error) {
	events, err := scanReportEvents(reportChainDir(), since)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, ev := range events {
		if ev.EventType != EventStaleReportSuppressed || ev.Ts.Before(since) {
			continue
		}
		var payload StaleReportSuppressedPayload
		if json.Unmarshal(ev.Payload, &payload) == nil && payload.Path == path {
			count++
		}
	}
	return count, nil
}

func scanReportEvents(chainDir string, since time.Time) ([]ReportChainEvent, error) {
	matches, err := filepath.Glob(filepath.Join(chainDir, "events-*.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []ReportChainEvent
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		out = append(out, scanReportEventsFile(f, since)...)
		_ = f.Close()
	}
	return out, nil
}

func reportChainDir() string {
	if d := os.Getenv("CHITIN_DIR"); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".chitin")
	}
	return ".chitin"
}

func scanReportEventsFile(r io.Reader, since time.Time) []ReportChainEvent {
	var out []ReportChainEvent
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), "stale_report_") && !strings.Contains(string(line), "report_fresh") && !strings.Contains(string(line), "report_missing") {
			continue
		}
		var row struct {
			EventType string          `json:"event_type"`
			Ts        string          `json:"ts"`
			Payload   json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, row.Ts)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, row.Ts)
		}
		if err != nil || (!since.IsZero() && ts.Before(since)) {
			continue
		}
		out = append(out, ReportChainEvent{EventType: row.EventType, Ts: ts, Payload: row.Payload})
	}
	return out
}

type MemoryReportChainSink struct {
	mu     sync.Mutex
	Events []ReportChainEvent
}

func (m *MemoryReportChainSink) Emit(_ context.Context, _ string, eventType string, payload any, ts time.Time) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = append(m.Events, ReportChainEvent{EventType: eventType, Ts: ts.UTC(), Payload: body})
	return nil
}

func (m *MemoryReportChainSink) LastEscalation(_ context.Context, path string, since time.Time) (time.Time, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var latest time.Time
	for _, ev := range m.Events {
		if ev.EventType != EventStaleReportEscalated || ev.Ts.Before(since) {
			continue
		}
		var payload StaleReportEscalatedPayload
		if json.Unmarshal(ev.Payload, &payload) == nil && payload.Path == path && ev.Ts.After(latest) {
			latest = ev.Ts
		}
	}
	return latest, !latest.IsZero(), nil
}

func (m *MemoryReportChainSink) SuppressionCount(_ context.Context, path string, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, ev := range m.Events {
		if ev.EventType != EventStaleReportSuppressed || ev.Ts.Before(since) {
			continue
		}
		var payload StaleReportSuppressedPayload
		if json.Unmarshal(ev.Payload, &payload) == nil && payload.Path == path {
			count++
		}
	}
	return count, nil
}
