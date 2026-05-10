package gov

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ReadRecentArgs configures ReadRecent.
type ReadRecentArgs struct {
	// Dir is the chitin state dir containing gov-decisions-*.jsonl.
	Dir string
	// WindowHours is the look-back window. Decisions with ts older than
	// (now - WindowHours) are excluded. Must be > 0.
	WindowHours int
	// Limit caps the number of returned decisions (newest first).
	// Must be > 0.
	Limit int
	// Now is injectable for tests; if zero, time.Now() is used.
	Now time.Time
}

// ReadRecent returns the most recent governance decisions across daily
// gov-decisions-*.jsonl files in dir, newest first.
//
// Invariant: returned decisions satisfy
//
//	ts ∈ (now-windowHours·hour, now]   AND   len ≤ limit.
//
// Behavior:
//   - Files matching `gov-decisions-*.jsonl` are sorted by name in
//     descending order (filename embeds the UTC date, so this orders by
//     day newest-first).
//   - Within a file, lines are read in reverse so the newest entry of
//     that day comes first. This relies on the append-only writer in
//     WriteLog producing monotonic-ish timestamps within a day.
//   - Once a file's first scanned entry is older than the window, no
//     earlier daily file can contain in-window entries — scanning stops.
//   - Malformed JSON lines are skipped silently to keep the operator
//     query path resilient to a single corrupt write (e.g. ENOSPC mid-
//     line). Empty lines are skipped too.
//   - A missing dir returns an empty slice + nil error so callers can
//     query without first probing existence.
func ReadRecent(args ReadRecentArgs) ([]Decision, error) {
	if args.WindowHours <= 0 {
		return nil, errors.New("window_hours must be > 0")
	}
	if args.Limit <= 0 {
		return nil, errors.New("limit must be > 0")
	}
	now := args.Now
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-time.Duration(args.WindowHours) * time.Hour)

	entries, err := os.ReadDir(args.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Decision{}, nil
		}
		return nil, fmt.Errorf("read dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "gov-decisions-") && strings.HasSuffix(name, ".jsonl") {
			files = append(files, name)
		}
	}
	// Lexical descending = date descending (filenames embed YYYY-MM-DD).
	sort.Sort(sort.Reverse(sort.StringSlice(files)))

	out := make([]Decision, 0, args.Limit)
	for _, name := range files {
		results, stop, err := readFileReverse(filepath.Join(args.Dir, name), cutoff, args.Limit-len(out))
		if err != nil {
			// Skip files we can't read rather than failing the whole
			// query — operator may be running this against a dir with
			// a single permission-broken entry.
			continue
		}
		out = append(out, results...)
		if len(out) >= args.Limit {
			return out[:args.Limit], nil
		}
		if stop {
			// First entry encountered in this file was already pre-
			// cutoff; earlier files cannot contain in-window data.
			break
		}
	}
	return out, nil
}

// readFileReverse reads a single jsonl file, returns up to maxLines
// in-window decisions newest-first, and reports stop=true when the
// first scanned (newest) entry in the file is already pre-cutoff
// (signal that earlier daily files are also pre-cutoff).
func readFileReverse(path string, cutoff time.Time, maxLines int) (decs []Decision, stop bool, err error) {
	if maxLines <= 0 {
		return nil, false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Decision rows can carry long reason/suggestion strings — extend
	// the scanner buffer past bufio's 64 KiB default.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var lines []string
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// copy because Scanner reuses the byte slice
		lines = append(lines, string(line))
	}
	if err := scanner.Err(); err != nil {
		return nil, false, err
	}

	out := make([]Decision, 0, maxLines)
	firstSeen := false
	for i := len(lines) - 1; i >= 0; i-- {
		d, err := unmarshalDecisionLine([]byte(lines[i]))
		if err != nil {
			continue
		}
		if d.Ts == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, d.Ts)
		if err != nil {
			continue
		}
		if !firstSeen {
			firstSeen = true
			if ts.Before(cutoff) {
				// Newest entry in this file is pre-cutoff; earlier
				// daily files are guaranteed pre-cutoff too.
				return nil, true, nil
			}
		}
		if ts.Before(cutoff) {
			// Past the window inside this file — done with this file
			// but newer files (already read) may still have entries.
			return out, false, nil
		}
		out = append(out, d)
		if len(out) >= maxLines {
			return out, false, nil
		}
	}
	return out, false, nil
}

func unmarshalDecisionLine(line []byte) (Decision, error) {
	type wire struct {
		Decision
		ActionType   string `json:"action_type"`
		ActionTarget string `json:"action_target"`
	}
	var row wire
	if err := json.Unmarshal(line, &row); err != nil {
		return Decision{}, err
	}
	d := row.Decision
	d.Action = Action{Type: ActionType(row.ActionType), Target: row.ActionTarget}
	return d, nil
}
