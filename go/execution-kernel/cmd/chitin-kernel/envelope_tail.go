package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// cmdEnvelopeTail streams ~/.chitin/gov-decisions-<date>.jsonl through a
// per-line formatter. Optional <id> arg filters to one envelope; --stats
// adds periodic envelope summary lines (queried from sqlite).
//
// Polling fallback only in v1: 250ms tick. inotify (fsnotify) can be
// added in Milestone D when audit-log rotation lands and exact-time
// readers matter more.
//
// Day rollover: opens today's file at start. If the date changes
// mid-tail, the tail keeps following the original file. Documented
// limitation; long-running operator tails should restart at midnight.
func cmdEnvelopeTail(args []string) {
	fs := flag.NewFlagSet("envelope tail", flag.ExitOnError)
	stats := fs.Bool("stats", false, "emit periodic envelope summary lines")
	statsEvery := fs.Duration("stats-every", 30*time.Second, "cadence for --stats summary lines")
	pollInterval := fs.Duration("poll", 250*time.Millisecond, "poll interval for new log lines")
	fromStart := fs.Bool("from-start", false, "replay all lines from today's log start before following")
	fs.Parse(args)
	if *pollInterval <= 0 {
		exitErr("envelope_tail_poll", "--poll must be > 0")
	}
	filter := ""
	if fs.NArg() == 1 {
		filter = fs.Arg(0)
	} else if fs.NArg() > 1 {
		exitErr("envelope_tail_args", "usage: chitin-kernel envelope tail [<id>] [--stats] [--from-start]")
	}

	dir := chitinDir()
	date := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(dir, "gov-decisions-"+date+".jsonl")

	state := &tailState{
		denials: map[string]int64{},
	}

	var statsTicker <-chan time.Time
	if *stats {
		// Print one stats line up-front so operators see envelope state
		// even before any decisions have flowed.
		printStats(filter, state)
		t := time.NewTicker(*statsEvery)
		defer t.Stop()
		statsTicker = t.C
	}

	var f *os.File
	var reader *bufio.Reader
	var offset int64

	openOrWait := func() {
		for {
			var err error
			f, err = os.Open(path)
			if err == nil {
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				exitErr("envelope_tail_open", err.Error())
			}
			select {
			case <-statsTicker:
				printStats(filter, state)
			case <-time.After(*pollInterval):
			}
		}
		if !*fromStart {
			fi, err := f.Stat()
			if err != nil {
				exitErr("envelope_tail_stat", err.Error())
			}
			offset = fi.Size()
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				exitErr("envelope_tail_seek", err.Error())
			}
		}
		// 1 MiB scanner buffer covers the practical Decision row max
		// (Reason + Suggestion + CorrectedCommand fields are each
		// bounded; nothing in the v1 schema produces multi-MB rows).
		reader = bufio.NewReaderSize(f, 1<<20)
	}

	openOrWait()
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			processLine(strings.TrimRight(line, "\n"), filter, state, os.Stdout)
		}
		if err == nil {
			continue
		}
		if !errors.Is(err, io.EOF) {
			exitErr("envelope_tail_read", err.Error())
		}
		// EOF: wait for new bytes (or a stats tick).
		select {
		case <-statsTicker:
			printStats(filter, state)
		case <-time.After(*pollInterval):
		}
	}
}

type tailState struct {
	denials map[string]int64 // envelope_id → denials seen since tail start
}

// processLine decodes one JSONL line, applies the optional envelope-id
// filter, and emits one formatted line. Unparseable lines are emitted
// verbatim with a [parse-error] prefix on stderr — they shouldn't exist
// in a well-formed audit log, but the operator-watch surface mustn't
// silently drop signal.
func processLine(line, filter string, state *tailState, out io.Writer) {
	if line == "" {
		return
	}
	var rec auditRow
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		fmt.Fprintf(os.Stderr, "[parse-error] %s\n", line)
		return
	}
	if filter != "" && rec.EnvelopeID != filter {
		return
	}
	if !rec.Allowed {
		state.denials[rec.EnvelopeID]++
	}
	fmt.Fprintln(out, formatRow(rec))
}

// auditRow mirrors the shape WriteLog emits. Fields not relevant to the
// formatter are still decoded so unknown keys don't surface as errors.
type auditRow struct {
	Allowed      bool     `json:"allowed"`
	Mode         string   `json:"mode"`
	RuleID       string   `json:"rule_id"`
	Reason       string   `json:"reason,omitempty"`
	Agent        string   `json:"agent,omitempty"`
	ActionType   string   `json:"action_type"`
	ActionTarget string   `json:"action_target"`
	Ts           string   `json:"ts"`
	EnvelopeID   string   `json:"envelope_id,omitempty"`
	Tier         gov.Tier `json:"tier,omitempty"`
	CostUSD      float64  `json:"cost_usd,omitempty"`
	InputBytes   int64    `json:"input_bytes,omitempty"`
	OutputBytes  int64    `json:"output_bytes,omitempty"`
	ToolCalls    int64    `json:"tool_calls,omitempty"`
}

// formatRow renders one audit-log line per the plan §"envelope_tail.go":
//
//	2026-04-29T15:01:02Z  claude-code  T0  $0.000   file.read /path/...     ALLOW
//
// Tab-separated columns; widths chosen for terminal scan-readability.
// The trailing verdict (ALLOW/DENY) on the right edge means a deny
// stands out visually even when the line is long.
func formatRow(r auditRow) string {
	verdict := "ALLOW"
	if !r.Allowed {
		verdict = "DENY"
	}
	agent := r.Agent
	if agent == "" {
		agent = "-"
	}
	tier := string(r.Tier)
	if tier == "" {
		tier = "-"
	}
	target := r.ActionTarget
	if len(target) > 60 {
		target = target[:57] + "..."
	}
	return fmt.Sprintf("%-20s  %-12s  %-3s  $%-7.4f  %-22s  %-60s  %s",
		r.Ts, agent, tier, r.CostUSD, r.ActionType, target, verdict)
}

// printStats reads the current envelope state from sqlite and prints
// one line per envelope (or just the filtered one). Uses humanBytes for
// MB/GB formatting and tallies in-memory denials seen since tail start.
//
// Empty-state behavior:
//   - filter="" + no envelopes in sqlite → prints nothing.
//   - filter set + envelope-not-found → prints a [stats] line noting it.
func printStats(filter string, state *tailState) {
	store, _, err := openBudgetStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[stats] open: %v\n", err)
		return
	}
	defer store.Close()
	if filter != "" {
		env, err := store.Load(filter)
		if err != nil {
			fmt.Fprintf(os.Stdout, "[stats] envelope %s: not found (%v)\n", filter, err)
			return
		}
		st, err := env.Inspect()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[stats] inspect %s: %v\n", filter, err)
			return
		}
		fmt.Fprintln(os.Stdout, formatStats(st, state.denials[filter]))
		return
	}
	envs, err := store.List(0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[stats] list: %v\n", err)
		return
	}
	for _, st := range envs {
		fmt.Fprintln(os.Stdout, formatStats(st, state.denials[st.ID]))
	}
}

func formatStats(st gov.EnvelopeState, denials int64) string {
	closed := ""
	if st.ClosedAt != "" {
		closed = " [CLOSED]"
	}
	return fmt.Sprintf("[stats] envelope %s: calls %s, bytes %s, $%.2f (informational), denials %d%s",
		st.ID,
		formatCapStatusInt(st.SpentCalls, st.Limits.MaxToolCalls),
		formatCapStatusBytes(st.SpentBytes, st.Limits.MaxInputBytes),
		st.SpentUSD,
		denials,
		closed,
	)
}

func formatCapStatusInt(spent, max int64) string {
	if max <= 0 {
		return fmt.Sprintf("%d/uncapped", spent)
	}
	return fmt.Sprintf("%d/%d", spent, max)
}

func formatCapStatusBytes(spent, max int64) string {
	if max <= 0 {
		return fmt.Sprintf("%s/uncapped", humanBytes(spent))
	}
	return fmt.Sprintf("%s/%s", humanBytes(spent), humanBytes(max))
}

// humanBytes renders bytes as B / KB / MB / GB. Decimal (1000-base) so
// the displayed numbers match what operators see on cloud bills and
// `du -h --si` defaults; binary 1024-base would be marginally more
// accurate but inconsistent with the rest of the operator surface.
func humanBytes(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%dB", n)
	}
	f := float64(n)
	switch {
	case n < 1000*1000:
		return fmt.Sprintf("%.1fKB", f/1000)
	case n < 1000*1000*1000:
		return fmt.Sprintf("%.1fMB", f/(1000*1000))
	default:
		return fmt.Sprintf("%.1fGB", f/(1000*1000*1000))
	}
}
