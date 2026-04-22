package gov

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WriteLog appends a Decision as one JSON line to
// <dir>/gov-decisions-<utc-date>.jsonl. Daily-rotated; append-only.
// Tolerates ENOSPC (logs to stderr, drops the line, returns nil).
func WriteLog(d Decision, dir string) error {
	if d.Ts == "" {
		d.Ts = time.Now().UTC().Format(time.RFC3339)
	}
	date := strings.Split(d.Ts, "T")[0]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir log dir: %w", err)
	}
	path := filepath.Join(dir, "gov-decisions-"+date+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(struct {
		Allowed          bool   `json:"allowed"`
		Mode             string `json:"mode"`
		RuleID           string `json:"rule_id"`
		Reason           string `json:"reason,omitempty"`
		Suggestion       string `json:"suggestion,omitempty"`
		CorrectedCommand string `json:"corrected_command,omitempty"`
		Escalation       string `json:"escalation,omitempty"`
		ActionType       string `json:"action_type"`
		ActionTarget     string `json:"action_target"`
		Ts               string `json:"ts"`
	}{
		Allowed: d.Allowed, Mode: d.Mode, RuleID: d.RuleID,
		Reason: d.Reason, Suggestion: d.Suggestion,
		CorrectedCommand: d.CorrectedCommand, Escalation: d.Escalation,
		ActionType: string(d.Action.Type), ActionTarget: d.Action.Target,
		Ts: d.Ts,
	})
	if err != nil {
		return fmt.Errorf("marshal decision: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		// Best-effort on ENOSPC — log once to stderr, don't fail the gate call
		fmt.Fprintf(os.Stderr, "gov: decision log write failed: %v\n", err)
		return nil
	}
	return nil
}
