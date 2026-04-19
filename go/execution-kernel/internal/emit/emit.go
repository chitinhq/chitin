// Package emit appends Events to the canonical JSONL log and updates the chain index.
// This is the sole write path for .chitin/events-<run_id>.jsonl.
package emit

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/chain"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/hash"
)

// Emitter appends events to a JSONL log and updates the chain index.
type Emitter struct {
	LogPath string
	Index   *chain.Index
}

// Emit appends ev to the log. Recomputes Seq, PrevHash, ThisHash using the chain index.
// On return, ev's Seq/PrevHash/ThisHash fields reflect what was written.
func (e *Emitter) Emit(ev *event.Event) error {
	info, err := e.Index.Get(ev.ChainID)
	if err != nil {
		return fmt.Errorf("chain index lookup: %w", err)
	}
	if info == nil {
		ev.Seq = 0
		ev.PrevHash = nil
	} else {
		ev.Seq = info.LastSeq + 1
		prev := info.LastHash
		ev.PrevHash = &prev
	}

	m, err := ev.ToMap()
	if err != nil {
		return fmt.Errorf("event to map: %w", err)
	}
	delete(m, "this_hash")

	h, err := hash.HashEvent(m)
	if err != nil {
		return fmt.Errorf("hash event: %w", err)
	}
	ev.ThisHash = h
	m["this_hash"] = h

	line, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	f, err := os.OpenFile(e.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	if err := e.Index.Upsert(ev.ChainID, ev.Seq, ev.ThisHash); err != nil {
		return fmt.Errorf("chain index upsert: %w", err)
	}
	return nil
}
