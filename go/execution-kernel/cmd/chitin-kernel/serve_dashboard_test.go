package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardMux_APIAndSPA(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CHITIN_HOME", home)
	t.Setenv("HOME", t.TempDir())

	events := `{"schema_version":"2","run_id":"run-1","session_id":"sess-dash","surface":"codex","agent_instance_id":"agent-1","agent_fingerprint":"fp","event_type":"decision","chain_id":"sess-dash","chain_type":"session","seq":0,"this_hash":"h1","ts":"2026-05-13T10:00:00Z","labels":{"driver":"codex","agent_instance_id":"agent-1","workflow_id":"t_8f110ab1"},"payload":{"event_id":"evt-1","tool_name":"shell.exec","action_type":"shell.exec","action_target":"echo hi","decision":"allow","rule_id":"allow-shell"}}` + "\n" +
		`{"schema_version":"2","run_id":"run-1","session_id":"sess-dash","surface":"codex","agent_instance_id":"agent-1","agent_fingerprint":"fp","event_type":"session_end","chain_id":"sess-dash","chain_type":"session","seq":1,"this_hash":"h2","ts":"2026-05-13T10:00:01Z","labels":{"driver":"codex","agent_instance_id":"agent-1","workflow_id":"t_8f110ab1"},"payload":{"reason":"clean"}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "events-run-1.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	gov := `{"allowed":true,"mode":"enforce","rule_id":"allow-shell","action_type":"shell.exec","action_target":"echo hi","ts":"2026-05-13T10:00:00Z","driver":"codex","agent_instance_id":"agent-1","agent":"agent-1","workflow_id":"t_8f110ab1","cost_usd":0.5}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "gov-decisions-2026-05-13.jsonl"), []byte(gov), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "chitin.yaml"), []byte("mode: enforce\nrules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<!doctype html><div id=\"root\">dashboard</div>"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := newDashboardMux(staticDir, cwd, 10)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sessions struct {
		Sessions []struct {
			SessionID string  `json:"session_id"`
			TicketID  string  `json:"ticket_id"`
			CostUSD   float64 `json:"cost_usd"`
			Success   *bool   `json:"success"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("sessions json: %v", err)
	}
	if len(sessions.Sessions) != 1 {
		t.Fatalf("sessions len=%d", len(sessions.Sessions))
	}
	if sessions.Sessions[0].TicketID != "t_8f110ab1" || sessions.Sessions[0].CostUSD != 0.5 {
		t.Fatalf("unexpected session row: %+v", sessions.Sessions[0])
	}
	if sessions.Sessions[0].Success == nil || !*sessions.Sessions[0].Success {
		t.Fatalf("expected success=true, got %+v", sessions.Sessions[0].Success)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/session/sess-dash", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"session_id": "sess-dash"`) {
		t.Fatalf("timeline status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/elo", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"placeholder": true`) {
		t.Fatalf("elo status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/policy", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"body": "mode: enforce\nrules: []\n"`) {
		t.Fatalf("policy status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/session/sess-dash", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "dashboard") {
		t.Fatalf("spa status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDashboardMux_BoundaryEmptySessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CHITIN_HOME", home)
	t.Setenv("HOME", t.TempDir())

	mux := newDashboardMux(t.TempDir(), t.TempDir(), 10)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("sessions status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Sessions []struct{} `json:"sessions"`
		Error    string     `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("sessions json: %v", err)
	}
	if body.Error != "" {
		t.Fatalf("unexpected error: %s", body.Error)
	}
	if len(body.Sessions) != 0 {
		t.Fatalf("sessions len=%d want 0", len(body.Sessions))
	}
}

func TestDashboardMux_BoundaryMaxRecentSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CHITIN_HOME", home)
	t.Setenv("HOME", t.TempDir())

	for i, sessionID := range []string{"sess-old", "sess-mid", "sess-new"} {
		body := fmt.Sprintf(`{"schema_version":"2","run_id":"run-%[1]s","session_id":"%[1]s","surface":"codex","agent_instance_id":"agent-1","agent_fingerprint":"fp","event_type":"decision","chain_id":"%[1]s","chain_type":"session","seq":0,"this_hash":"h%[1]s","ts":"2026-05-13T10:00:0%[2]dZ","labels":{"driver":"codex","agent_instance_id":"agent-1"},"payload":{"tool_name":"file.read","action_type":"file.read","action_target":"README.md","decision":"allow"}}`+"\n", sessionID, i)
		if err := os.WriteFile(filepath.Join(home, "events-"+sessionID+".jsonl"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mux := newDashboardMux(t.TempDir(), t.TempDir(), 2)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("sessions status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("sessions json: %v", err)
	}
	if len(body.Sessions) != 2 {
		t.Fatalf("sessions len=%d want 2", len(body.Sessions))
	}
	if body.Sessions[0].SessionID != "sess-new" || body.Sessions[1].SessionID != "sess-mid" {
		t.Fatalf("sessions order=%+v; want newest two", body.Sessions)
	}
}

func TestDashboardMux_BoundaryErrorMissingPolicy(t *testing.T) {
	t.Setenv("CHITIN_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	mux := newDashboardMux(t.TempDir(), t.TempDir(), 10)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/policy", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("policy status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error"`) || !strings.Contains(rec.Body.String(), "no chitin.yaml") {
		t.Fatalf("policy error body=%s", rec.Body.String())
	}
}
