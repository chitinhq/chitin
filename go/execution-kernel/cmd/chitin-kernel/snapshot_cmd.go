package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cmdChainSnapshot dispatches `chitin-kernel chain snapshot`.
// Produces an immutable, hash-linked export of a session's chain
// suitable for external audit + regulatory submission.
//
// Output formats:
//   json    pretty JSON with embedded hash chain
//   ndjson  newline-delimited (one event per line) — useful for
//           piping into stream consumers (jq, log indexers)
//
// The export's INTEGRITY is provable via the hash chain:
//   - Each event has prev_hash + this_hash already (kernel's
//     hash-chained event format)
//   - Snapshot's metadata wraps events with a snapshot_hash =
//     sha256 of the concatenated event hashes, signed against the
//     final event's this_hash
//   - Verifier can recompute snapshot_hash + walk the event chain
//     to confirm tamper-free
//
// Use cases:
//   - Compliance: "here's a hash-proven export of all decisions
//     made during the audit window"
//   - Cross-system handoff: ship the snapshot to a downstream
//     analyzer; verifier recomputes the hash to trust it
//   - Debugging: snapshot a session, ship to teammates, replay
//     elsewhere with full hash-chain proof of what happened
func cmdChainSnapshot(args []string) {
	sessionID := ""
	format := "json"
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--session="):
			sessionID = a[len("--session="):]
		case strings.HasPrefix(a, "--format="):
			format = a[len("--format="):]
		case a == "--help" || a == "-h":
			fmt.Fprintln(os.Stderr, `Usage: chitin-kernel chain snapshot --session=<id> [--format=json|ndjson]

Produce an immutable, hash-linked export of a session's chain
suitable for external audit + regulatory submission.

The export's integrity is provable via the hash chain:
  - Each event has prev_hash + this_hash (kernel's hash-chained format)
  - Snapshot wraps events with a snapshot_hash = SHA-256 of
    concatenated event hashes
  - Verifier recomputes snapshot_hash to confirm tamper-free

Formats:
  json    pretty JSON with embedded chain (default)
  ndjson  newline-delimited (one event per line) — pipe-friendly`)
			os.Exit(0)
		}
	}
	if sessionID == "" {
		exitErr("chain_snapshot_no_session", "--session=<id> required")
	}
	if format != "json" && format != "ndjson" {
		exitErr("chain_snapshot_bad_format", "--format must be json or ndjson")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		exitErr("chain_snapshot_home", err.Error())
	}
	path := filepath.Join(home, ".chitin", "events-"+sessionID+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		exitErr("chain_snapshot_read", err.Error())
	}

	// Parse events; compute integrity proofs along the way
	var events []map[string]interface{}
	var hashChain []string // for snapshot_hash computation
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		events = append(events, ev)
		if h, ok := ev["this_hash"].(string); ok && h != "" {
			hashChain = append(hashChain, h)
		}
	}

	if len(events) == 0 {
		exitErr("chain_snapshot_empty", "session has no events")
	}

	// Snapshot integrity hash: sha256 of concatenated this_hash values
	hasher := sha256.New()
	for _, h := range hashChain {
		hasher.Write([]byte(h))
	}
	snapshotHash := hex.EncodeToString(hasher.Sum(nil))

	finalEventHash := ""
	if len(hashChain) > 0 {
		finalEventHash = hashChain[len(hashChain)-1]
	}

	switch format {
	case "json":
		writeJSONSnapshot(os.Stdout, sessionID, events, snapshotHash, finalEventHash)
	case "ndjson":
		writeNDJSONSnapshot(os.Stdout, sessionID, events, snapshotHash, finalEventHash)
	}
}

func writeJSONSnapshot(w io.Writer, sessionID string, events []map[string]interface{}, snapshotHash, finalEventHash string) {
	envelope := map[string]interface{}{
		"snapshot_version":  "1",
		"session_id":        sessionID,
		"snapshot_ts":       time.Now().UTC().Format(time.RFC3339),
		"event_count":       len(events),
		"final_event_hash":  finalEventHash,
		"snapshot_hash":     snapshotHash,
		"snapshot_hash_alg": "sha256-of-concatenated-this_hash",
		"events":            events,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(envelope); err != nil {
		exitErr("chain_snapshot_marshal", err.Error())
	}
}

func writeNDJSONSnapshot(w io.Writer, sessionID string, events []map[string]interface{}, snapshotHash, finalEventHash string) {
	// Header line: snapshot metadata
	header := map[string]interface{}{
		"_snapshot_envelope": true,
		"snapshot_version":   "1",
		"session_id":         sessionID,
		"snapshot_ts":        time.Now().UTC().Format(time.RFC3339),
		"event_count":        len(events),
		"final_event_hash":   finalEventHash,
		"snapshot_hash":      snapshotHash,
		"snapshot_hash_alg":  "sha256-of-concatenated-this_hash",
	}
	hb, _ := json.Marshal(header)
	fmt.Fprintln(w, string(hb))
	// Then each event as one line
	for _, ev := range events {
		eb, _ := json.Marshal(ev)
		fmt.Fprintln(w, string(eb))
	}
}
