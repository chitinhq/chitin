// Package emit writes canonical events to the local JSONL ground truth.
// Path convention: <workspace>/.chitin/events-<run_id>.jsonl — append-only.
package emit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
)

// AppendEvent serializes ev to a single JSONL line and appends it to
// <workspace>/.chitin/events-<run_id>.jsonl. The .chitin directory is
// created if it does not exist. The write uses O_APPEND so concurrent
// invocations from different agent subprocesses interleave safely at
// the OS level.
func AppendEvent(workspace string, ev event.Event) error {
	dir := filepath.Join(workspace, ".chitin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("emit: mkdir .chitin: %w", err)
	}
	path := filepath.Join(dir, "events-"+ev.RunID+".jsonl")

	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("emit: marshal: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("emit: open %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("emit: write %s: %w", path, err)
	}
	return nil
}
