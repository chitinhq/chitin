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
//
// OTEL (optional, F4): if non-nil, after a successful chain commit the emitter
// synchronously projects the event onto an OTLP/HTTP JSON span and POSTs it to
// the configured collector. OTEL emit failures are logged and dropped — they
// never affect the canonical chain write. Use EnableOTELFromEnv to wire it up.
// v1 is sync because the kernel runs as a short-lived CLI per emit; daemon-mode
// async is deferred. See otel.go ProjectAndPost.
type Emitter struct {
	LogPath string
	Index   *chain.Index
	OTEL    *otelExporter
}

// EnableOTELFromEnv configures OTEL projection from environment:
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT (preferred) or OTEL_EXPORTER_OTLP_ENDPOINT
// (with /v1/traces appended). When neither is set, OTEL stays disabled.
//
// Idempotent. Re-call after env changes if needed.
func (e *Emitter) EnableOTELFromEnv() {
	e.OTEL = newOTELExporter()
}

// Emit appends ev to the log. Recomputes Seq, PrevHash, ThisHash using the chain index.
// On return, ev's Seq/PrevHash/ThisHash fields reflect what was written.
//
// Crash-safety ordering:
//   - The BEGIN IMMEDIATE transaction is acquired first, serializing all
//     concurrent writers sharing the same chain_index.sqlite.
//   - JSONL append occurs inside the critical section (after tx acquired,
//     before tx.Commit). No other writer can observe an intermediate state.
//   - If the process dies after JSONL append but before tx.Commit, the DB
//     transaction rolls back — the index does not reflect the appended line.
//   - The next cmdEmit invocation calls RebuildFromJSONL first (Blocker 1),
//     which scans JSONL and advances the index to match. That is the intended
//     recovery path: the orphaned line is reconciled before the next emit.
func (e *Emitter) Emit(ev *event.Event) error {
	tx, err := e.Index.BeginEmit(ev.ChainID)
	if err != nil {
		return fmt.Errorf("begin emit: %w", err)
	}
	defer tx.Rollback() // no-op if Commit was called

	if tx.Current != nil {
		ev.Seq = tx.Current.LastSeq + 1
		prev := tx.Current.LastHash
		ev.PrevHash = &prev
	} else {
		ev.Seq = 0
		ev.PrevHash = nil
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
	if _, werr := f.Write(append(line, '\n')); werr != nil {
		f.Close()
		return fmt.Errorf("write: %w", werr)
	}
	if cerr := f.Close(); cerr != nil {
		return fmt.Errorf("close log: %w", cerr)
	}

	if err := tx.Commit(ev.Seq, ev.ThisHash); err != nil {
		return err
	}

	// F4: project to OTEL span and POST synchronously after commit.
	// Kernel JSONL/index commit is already complete; OTEL failures cannot
	// affect canonical state. Safe when OTEL is nil (no-op).
	e.OTEL.ProjectAndPost(ev, e.Index)
	return nil
}
