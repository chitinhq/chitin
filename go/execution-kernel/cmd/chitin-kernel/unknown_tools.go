package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

type unknownToolLogEntry struct {
	TS            string `json:"ts"`
	Driver        string `json:"driver"`
	Agent         string `json:"agent,omitempty"`
	RawToolName   string `json:"raw_tool_name"`
	ActionTarget  string `json:"action_target,omitempty"`
	Cwd           string `json:"cwd,omitempty"`
	HookEventName string `json:"hook_event_name,omitempty"`
}

// logUnknownTool persists every ActUnknown normalization result to a
// side-channel file that keeps the raw driver tool name. Decision rows
// only carry action_type/action_target, so without this file operators
// cannot distinguish a truly novel tool from a driver-normalizer gap.
func logUnknownTool(chitinDir string, action gov.Action, driver, agent, rawToolName, cwd, hookEventName string) error {
	if action.Type != gov.ActUnknown {
		return nil
	}
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(chitinDir, "unknown-tools.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	entry := unknownToolLogEntry{
		TS:            time.Now().UTC().Format(time.RFC3339),
		Driver:        driver,
		Agent:         agent,
		RawToolName:   rawToolName,
		ActionTarget:  action.Target,
		Cwd:           cwd,
		HookEventName: hookEventName,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, string(b)); err != nil {
		return err
	}
	return nil
}

func reportUnknownToolLogError(w interface{ Write([]byte) (int, error) }, err error) {
	if err == nil || w == nil {
		return
	}
	writeJSONLine(w, map[string]string{"error": "unknown_tool_log", "message": err.Error()})
}
