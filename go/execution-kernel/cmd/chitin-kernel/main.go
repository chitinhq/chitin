package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/chain"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/emit"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/health"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/hookinstall"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/ingest"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/kstate"
)

func main() {
	if len(os.Args) < 2 {
		exitErr("no_subcommand", "usage: chitin-kernel <subcommand> [flags]")
	}
	sub := os.Args[1]
	args := os.Args[2:]
	switch sub {
	case "init":
		cmdInit(args)
	case "emit":
		cmdEmit(args)
	case "chain-info":
		cmdChainInfo(args)
	case "ingest-transcript":
		cmdIngestTranscript(args)
	case "ingest-otel":
		cmdIngestOTEL(args)
	case "ingest-hermes":
		cmdIngestHermes(args)
	case "sweep-transcripts":
		cmdSweepTranscripts(args)
	case "install-hook":
		cmdInstallHook(args)
	case "uninstall-hook":
		cmdUninstallHook(args)
	case "install":
		cmdInstall(args)
	case "uninstall":
		cmdUninstall(args)
	case "health":
		cmdHealth(args)
	case "gate":
		cmdGate(args)
	case "drive":
		if len(args) < 1 {
			exitErr("drive_no_driver", "usage: chitin-kernel drive <driver> [flags]")
		}
		driver := args[0]
		driverArgs := args[1:]
		switch driver {
		case "copilot":
			os.Exit(cmdDriveCopilot(driverArgs))
		default:
			exitErr("drive_unknown_driver", driver)
		}
	default:
		exitErr("unknown_subcommand", sub)
	}
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	dir := fs.String("dir", ".chitin", "path to .chitin state dir")
	force := fs.Bool("force", false, "wipe existing state")
	fs.Parse(args)
	abs, err := filepath.Abs(*dir)
	if err != nil {
		exitErr("init_path", err.Error())
	}
	if err := kstate.Init(abs, *force); err != nil {
		exitErr("init_failed", err.Error())
	}
	fmt.Println(`{"ok":true}`)
}

func cmdEmit(args []string) {
	fs := flag.NewFlagSet("emit", flag.ExitOnError)
	dir := fs.String("dir", ".chitin", "path to .chitin state dir")
	eventFile := fs.String("event-file", "", "path to JSON file containing a v2 Event")
	fs.Parse(args)
	if *eventFile == "" {
		exitErr("missing_event_file", "--event-file is required")
	}
	raw, err := os.ReadFile(*eventFile)
	if err != nil {
		exitErr("read_event_file", err.Error())
	}
	var ev event.Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		exitErr("parse_event", err.Error())
	}
	absDir, _ := filepath.Abs(*dir)
	if err := kstate.Init(absDir, false); err != nil {
		exitErr("init", err.Error())
	}
	idx, err := chain.OpenIndex(filepath.Join(absDir, "chain_index.sqlite"))
	if err != nil {
		exitErr("open_index", err.Error())
	}
	defer idx.Close()
	// Reconcile the index against all JSONL files before every emit. This
	// guarantees that a crash between JSONL append and Upsert (or a deleted
	// chain_index.sqlite) cannot cause a silent seq=0 fork. O(JSONL) per emit
	// is acceptable at Phase 1.5 volumes; Phase 2 can add incremental reconcile.
	if err := idx.RebuildFromJSONL(absDir); err != nil {
		exitErr("rebuild_index", err.Error())
	}
	em := emit.Emitter{
		LogPath: filepath.Join(absDir, fmt.Sprintf("events-%s.jsonl", ev.RunID)),
		Index:   idx,
	}
	if err := em.Emit(&ev); err != nil {
		exitErr("emit", err.Error())
	}
	out, _ := json.Marshal(map[string]any{
		"ok":        true,
		"seq":       ev.Seq,
		"this_hash": ev.ThisHash,
	})
	fmt.Println(string(out))
}

func cmdChainInfo(args []string) {
	fs := flag.NewFlagSet("chain-info", flag.ExitOnError)
	dir := fs.String("dir", ".chitin", "path to .chitin state dir")
	chainID := fs.String("chain-id", "", "chain_id to look up")
	fs.Parse(args)
	if *chainID == "" {
		exitErr("missing_chain_id", "--chain-id is required")
	}
	absDir, _ := filepath.Abs(*dir)
	idx, err := chain.OpenIndex(filepath.Join(absDir, "chain_index.sqlite"))
	if err != nil {
		exitErr("open_index", err.Error())
	}
	defer idx.Close()
	// Reconcile before serving chain state so callers always see consistent data.
	if err := idx.RebuildFromJSONL(absDir); err != nil {
		exitErr("rebuild_index", err.Error())
	}
	info, err := idx.Get(*chainID)
	if err != nil {
		exitErr("lookup", err.Error())
	}
	if info == nil {
		fmt.Println(`{"exists":false}`)
		return
	}
	out, _ := json.Marshal(map[string]any{
		"exists":    true,
		"last_seq":  info.LastSeq,
		"last_hash": info.LastHash,
	})
	fmt.Println(string(out))
}

// cmdIngestTranscript reads a Claude Code session JSONL transcript and operates
// in one of two modes:
//
// Parse-only mode (no --envelope-template):
//   Parses the transcript, saves a checkpoint recording the byte offset, and
//   prints {"ok":true,"turns":[...]} to stdout.  Useful for external callers
//   that want the parsed turn data without emitting to a chain.
//
// Emit mode (--envelope-template <file>):
//   In addition to parsing and checkpointing, emits one assistant_turn event
//   per parsed turn into .chitin/events-<run_id>.jsonl using the transactional
//   Emitter.  The template JSON must contain all required envelope fields
//   (schema_version, run_id, session_id, surface, chain_id, chain_type="session");
//   missing fields cause a loud failure before any emission.  On success prints
//   {"ok":true,"emitted":N,"turns":N}.
func cmdIngestTranscript(args []string) {
	fs := flag.NewFlagSet("ingest-transcript", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: chitin-kernel ingest-transcript --session-id <id> --transcript-path <file> [--dir <dir>] [--envelope-template <file>]")
		fs.PrintDefaults()
	}
	dir := fs.String("dir", ".chitin", "path to .chitin state dir")
	sessionID := fs.String("session-id", "", "session_id of the transcript to ingest")
	transcriptPath := fs.String("transcript-path", "", "path to Claude Code session JSONL transcript")
	envelopeTemplateFile := fs.String("envelope-template", "", "path to JSON envelope template; if set, emits assistant_turn events per parsed turn")
	fs.Parse(args)
	if *sessionID == "" || *transcriptPath == "" {
		exitErr("missing_args", "--session-id and --transcript-path required")
	}
	absDir, _ := filepath.Abs(*dir)
	if err := kstate.Init(absDir, false); err != nil {
		exitErr("init", err.Error())
	}
	cpPath := filepath.Join(absDir, "transcript_checkpoint.json")
	cp, err := ingest.LoadCheckpoint(cpPath)
	if err != nil {
		exitErr("load_checkpoint", err.Error())
	}
	prev := cp[*sessionID]
	f, err := os.Open(*transcriptPath)
	if err != nil {
		exitErr("open_transcript", err.Error())
	}
	defer f.Close()
	finfo, err := f.Stat()
	if err != nil {
		exitErr("stat_transcript", err.Error())
	}
	// Clamp a stale checkpoint: if the recorded offset exceeds the current file
	// size, the file was truncated / rotated (or the checkpoint was tampered
	// with). Clamp to size and warn, so we neither seek past EOF silently nor
	// skip newly-appended content. See adversarial review Probe 7.
	if prev.LastIngestOffset > finfo.Size() {
		fmt.Fprintf(
			os.Stderr,
			`{"warning":"checkpoint_ahead_of_file","session_id":%q,"checkpoint_offset":%d,"file_size":%d}`+"\n",
			*sessionID, prev.LastIngestOffset, finfo.Size(),
		)
		prev.LastIngestOffset = finfo.Size()
	}
	if prev.LastIngestOffset > 0 {
		if _, err := f.Seek(prev.LastIngestOffset, 0); err != nil {
			exitErr("seek", err.Error())
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		exitErr("read", err.Error())
	}
	turns, err := ingest.ParseAssistantTurns(data)
	if err != nil {
		exitErr("parse", err.Error())
	}
	cp[*sessionID] = ingest.CheckpointEntry{
		TranscriptPath:   *transcriptPath,
		LastIngestOffset: finfo.Size(),
		Status:           "complete",
	}
	if err := ingest.SaveCheckpoint(cpPath, cp); err != nil {
		exitErr("save_checkpoint", err.Error())
	}

	// Emit mode: when --envelope-template is provided, emit one assistant_turn
	// event per parsed turn using the transactional Emitter.
	if *envelopeTemplateFile != "" {
		rawTmpl, err := os.ReadFile(*envelopeTemplateFile)
		if err != nil {
			exitErr("read_envelope_template", err.Error())
		}
		var tmpl event.Event
		if err := json.Unmarshal(rawTmpl, &tmpl); err != nil {
			exitErr("parse_envelope_template", err.Error())
		}
		// Validate before touching the chain index — illegal states caught here.
		if err := ingest.ValidateEnvelopeTemplate(&tmpl); err != nil {
			exitErr("invalid_envelope_template", err.Error())
		}
		idx, err := chain.OpenIndex(filepath.Join(absDir, "chain_index.sqlite"))
		if err != nil {
			exitErr("open_index", err.Error())
		}
		defer idx.Close()
		// Reconcile index from JSONL for parity with cmdEmit (Blocker 1 path).
		if err := idx.RebuildFromJSONL(absDir); err != nil {
			exitErr("rebuild_index", err.Error())
		}
		em := emit.Emitter{
			LogPath: filepath.Join(absDir, fmt.Sprintf("events-%s.jsonl", tmpl.RunID)),
			Index:   idx,
		}
		n, err := ingest.EmitTurns(&em, &tmpl, turns)
		if err != nil {
			exitErr("emit", err.Error())
		}
		out, _ := json.Marshal(map[string]any{"ok": true, "emitted": n, "turns": n})
		fmt.Println(string(out))
		return
	}

	// Parse-only mode: print parsed turns as JSON (original behavior).
	out, _ := json.Marshal(map[string]any{"ok": true, "turns": turns})
	fmt.Println(string(out))
}

// cmdIngestOTEL reads an OTLP/protobuf file and, in emit mode, produces
// one model_turn envelope event per successfully translated
// openclaw.model.usage span. Quarantined spans go to
// <dir>/otel-quarantine/.
//
// Parse-only mode (no --envelope-template): prints JSON
// {"ok":true,"events":[{event_type, ts, surface, chain_id, payload}, ...],
// "quarantined":[{reason, span_name, trace_id, span_id, span_raw}, ...]}.
// Useful for debugging. Shape is stable across all four span types — the
// per-translator concrete struct is hidden behind the TranslatedSpan
// interface.
//
// Emit mode (--envelope-template <file>): emits events via the
// transactional Emitter. Exit codes:
//
//	0 — all mappable spans emitted, zero quarantined.
//	2 — some spans quarantined; non-quarantined spans emitted durably.
//	non-zero-other — fatal station failure.
func cmdIngestOTEL(args []string) {
	fs := flag.NewFlagSet("ingest-otel", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: chitin-kernel ingest-otel --from <file.pb> --dialect openclaw [--envelope-template <file>] [--dir <dir>]")
		fs.PrintDefaults()
	}
	from := fs.String("from", "", "path to OTLP/protobuf file")
	dialect := fs.String("dialect", "", "dialect (only 'openclaw' supported in v1)")
	envelopeTemplateFile := fs.String("envelope-template", "", "path to JSON envelope template; if set, emits events")
	dir := fs.String("dir", ".chitin", "path to .chitin state dir")
	fs.Parse(args)

	if *from == "" || *dialect == "" {
		exitErr("missing_args", "--from and --dialect are required")
	}
	if *dialect != "openclaw" {
		exitErr("unsupported_dialect", fmt.Sprintf("only 'openclaw' is supported in v1, got %q", *dialect))
	}
	data, err := os.ReadFile(*from)
	if err != nil {
		exitErr("read_from", err.Error())
	}
	rs, err := ingest.DecodeTraces(data)
	if err != nil {
		exitErr("otlp_decode_failed", err.Error())
	}
	spans, quarantined, err := ingest.ParseOpenClawSpans(rs)
	if err != nil {
		exitErr("parse", err.Error())
	}

	absDir, _ := filepath.Abs(*dir)

	if *envelopeTemplateFile == "" {
		// Parse-only mode. Build a stable shape from the TranslatedSpan
		// interface — never serialize the concrete translator structs
		// directly, which would leak CamelCase field names and
		// per-type-divergent shapes.
		//
		// Apply the same (ts asc, span_id asc) ordering that EmitEvents
		// uses, so parse-only output is deterministic across runs and
		// matches what emit mode would produce for the same input.
		sort.SliceStable(spans, func(i, j int) bool {
			if spans[i].Ts() != spans[j].Ts() {
				return spans[i].Ts() < spans[j].Ts()
			}
			return spans[i].SpanID() < spans[j].SpanID()
		})
		type parseOnlySpan struct {
			EventType string          `json:"event_type"`
			Ts        string          `json:"ts"`
			Surface   string          `json:"surface"`
			ChainID   string          `json:"chain_id"`
			Payload   json.RawMessage `json:"payload"`
		}
		events := make([]parseOnlySpan, 0, len(spans))
		for _, s := range spans {
			payload, err := s.Payload()
			if err != nil {
				exitErr("payload_marshal", err.Error())
			}
			events = append(events, parseOnlySpan{
				EventType: s.EventType(),
				Ts:        s.Ts(),
				Surface:   s.Surface(),
				ChainID:   s.ChainID(),
				Payload:   payload,
			})
		}
		out, _ := json.Marshal(map[string]any{
			"ok":          true,
			"events":      events,
			"quarantined": quarantined,
		})
		fmt.Println(string(out))
		return
	}

	// Emit mode.
	if err := kstate.Init(absDir, false); err != nil {
		exitErr("init", err.Error())
	}
	rawTmpl, err := os.ReadFile(*envelopeTemplateFile)
	if err != nil {
		exitErr("read_envelope_template", err.Error())
	}
	var tmpl event.Event
	if err := json.Unmarshal(rawTmpl, &tmpl); err != nil {
		exitErr("parse_envelope_template", err.Error())
	}
	if err := ingest.ValidateEnvelopeTemplate(&tmpl); err != nil {
		exitErr("invalid_envelope_template", err.Error())
	}
	idx, err := chain.OpenIndex(filepath.Join(absDir, "chain_index.sqlite"))
	if err != nil {
		exitErr("open_index", err.Error())
	}
	defer idx.Close()
	if err := idx.RebuildFromJSONL(absDir); err != nil {
		exitErr("rebuild_index", err.Error())
	}
	em := emit.Emitter{
		LogPath: filepath.Join(absDir, fmt.Sprintf("events-%s.jsonl", tmpl.RunID)),
		Index:   idx,
	}
	n, err := ingest.EmitEvents(&em, absDir, &tmpl, spans, quarantined)
	if err != nil {
		exitErr("emit", err.Error())
	}
	out, _ := json.Marshal(map[string]any{
		"ok":          true,
		"emitted":     n,
		"quarantined": len(quarantined),
	})
	fmt.Println(string(out))

	if len(quarantined) > 0 {
		os.Exit(2)
	}
}

// cmdIngestHermes reads a chitin-sink JSONL file and, in emit mode, produces
// one model_turn envelope event per successfully translated post_api_request
// line. Non-primary events and malformed lines go to
// <dir>/hermes-quarantine/.
//
// Parse-only mode (no --envelope-template): prints JSON
// {"ok":true,"turns":[...],"quarantined":[...]}. Useful for debugging.
//
// Emit mode (--envelope-template <file>): emits events via the transactional
// Emitter. Exit codes mirror cmdIngestOTEL:
//
//	0 — all mappable events emitted, zero quarantined.
//	2 — some events quarantined; non-quarantined events emitted durably.
//	non-zero-other — fatal station failure.
func cmdIngestHermes(args []string) {
	fs := flag.NewFlagSet("ingest-hermes", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: chitin-kernel ingest-hermes --from <file.jsonl> [--envelope-template <file>] [--dir <dir>]")
		fs.PrintDefaults()
	}
	from := fs.String("from", "", "path to chitin-sink JSONL file")
	envelopeTemplateFile := fs.String("envelope-template", "", "path to JSON envelope template; if set, emits events")
	dir := fs.String("dir", ".chitin", "path to .chitin state dir")
	fs.Parse(args)

	if *from == "" {
		exitErr("missing_args", "--from is required")
	}
	data, err := os.ReadFile(*from)
	if err != nil {
		exitErr("read_from", err.Error())
	}
	turns, quarantined, err := ingest.ParseHermesEvents(data)
	if err != nil {
		exitErr("parse", err.Error())
	}

	absDir, _ := filepath.Abs(*dir)

	if *envelopeTemplateFile == "" {
		out, _ := json.Marshal(map[string]any{
			"ok":          true,
			"turns":       turns,
			"quarantined": quarantined,
		})
		fmt.Println(string(out))
		return
	}

	if err := kstate.Init(absDir, false); err != nil {
		exitErr("init", err.Error())
	}
	rawTmpl, err := os.ReadFile(*envelopeTemplateFile)
	if err != nil {
		exitErr("read_envelope_template", err.Error())
	}
	var tmpl event.Event
	if err := json.Unmarshal(rawTmpl, &tmpl); err != nil {
		exitErr("parse_envelope_template", err.Error())
	}
	if err := ingest.ValidateEnvelopeTemplate(&tmpl); err != nil {
		exitErr("invalid_envelope_template", err.Error())
	}
	idx, err := chain.OpenIndex(filepath.Join(absDir, "chain_index.sqlite"))
	if err != nil {
		exitErr("open_index", err.Error())
	}
	defer idx.Close()
	if err := idx.RebuildFromJSONL(absDir); err != nil {
		exitErr("rebuild_index", err.Error())
	}
	em := emit.Emitter{
		LogPath: filepath.Join(absDir, fmt.Sprintf("events-%s.jsonl", tmpl.RunID)),
		Index:   idx,
	}
	n, err := ingest.EmitHermesTurns(&em, absDir, &tmpl, turns, quarantined)
	if err != nil {
		exitErr("emit", err.Error())
	}
	out, _ := json.Marshal(map[string]any{
		"ok":          true,
		"emitted":     n,
		"quarantined": len(quarantined),
	})
	fmt.Println(string(out))

	if len(quarantined) > 0 {
		os.Exit(2)
	}
}

func cmdSweepTranscripts(args []string) {
	// Phase 1.5 sweep stub: no-op. Future impl will discover orphaned transcripts.
	_ = args
	fmt.Println(`{"ok":true,"swept":0}`)
}

func cmdInstallHook(args []string) {
	fs := flag.NewFlagSet("install-hook", flag.ExitOnError)
	dir := fs.String("dir", ".chitin", "path to .chitin state dir")
	sessionID := fs.String("session-id", "", "session id")
	adapter := fs.String("adapter", os.Getenv("CHITIN_ADAPTER_BINARY"), "adapter binary path")
	adapterShell := fs.Bool("adapter-shell", false, "allow shell command string for adapter (security: trust-on-use)")
	fs.Parse(args)
	if *sessionID == "" {
		exitErr("missing_session_id", "--session-id required")
	}
	if *adapter == "" {
		exitErr("missing_adapter", "--adapter or CHITIN_ADAPTER_BINARY required")
	}
	if !*adapterShell {
		if err := hookinstall.ValidateAdapter(*adapter); err != nil {
			exitErr("invalid_adapter", err.Error())
		}
	} else {
		if err := hookinstall.ValidateAdapterShell(*adapter); err != nil {
			exitErr("invalid_adapter", err.Error())
		}
		fmt.Fprintf(os.Stderr, `{"warning":"adapter_shell_trusted","message":"--adapter-shell skips path validation. The adapter string will be invoked via shell semantics on every hook."}`+"\n")
	}
	absDir, _ := filepath.Abs(*dir)
	if err := hookinstall.Install(absDir, *sessionID, *adapter); err != nil {
		exitErr("install", err.Error())
	}
	fmt.Println(`{"ok":true}`)
}

func cmdUninstallHook(args []string) {
	fs := flag.NewFlagSet("uninstall-hook", flag.ExitOnError)
	dir := fs.String("dir", ".chitin", "path to .chitin state dir")
	sessionID := fs.String("session-id", "", "session id")
	fs.Parse(args)
	if *sessionID == "" {
		exitErr("missing_session_id", "--session-id required")
	}
	absDir, _ := filepath.Abs(*dir)
	if err := hookinstall.Uninstall(absDir, *sessionID); err != nil {
		exitErr("uninstall", err.Error())
	}
	fmt.Println(`{"ok":true}`)
}

func cmdInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	surface := fs.String("surface", "", "surface to install (claude-code)")
	global := fs.Bool("global", false, "install into user-level settings (always-on)")
	adapter := fs.String("adapter", os.Getenv("CHITIN_ADAPTER_BINARY"), "adapter binary path")
	adapterShell := fs.Bool("adapter-shell", false, "allow shell command string for adapter (security: trust-on-use)")
	fs.Parse(args)
	if *surface == "" {
		exitErr("missing_surface", "--surface required")
	}
	if !*global {
		exitErr("not_implemented", "non-global install is not yet supported via `install`; use `install-hook` for session-scoped")
	}
	if *adapter == "" {
		exitErr("missing_adapter", "--adapter or CHITIN_ADAPTER_BINARY required")
	}
	if !*adapterShell {
		if err := hookinstall.ValidateAdapter(*adapter); err != nil {
			exitErr("invalid_adapter", err.Error())
		}
	} else {
		if err := hookinstall.ValidateAdapterShell(*adapter); err != nil {
			exitErr("invalid_adapter", err.Error())
		}
		fmt.Fprintf(os.Stderr, `{"warning":"adapter_shell_trusted","message":"--adapter-shell skips path validation. The adapter string will be invoked via shell semantics on every hook."}`+"\n")
	}
	switch *surface {
	case "claude-code":
		if err := hookinstall.InstallGlobal(*adapter); err != nil {
			exitErr("install_global", err.Error())
		}
	default:
		exitErr("unknown_surface", *surface)
	}
	fmt.Println(`{"ok":true}`)
}

func cmdUninstall(args []string) {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	surface := fs.String("surface", "", "surface to uninstall (claude-code)")
	global := fs.Bool("global", false, "uninstall from user-level settings")
	fs.Parse(args)
	if *surface == "" {
		exitErr("missing_surface", "--surface required")
	}
	if !*global {
		exitErr("not_implemented", "non-global uninstall not supported via `uninstall`")
	}
	switch *surface {
	case "claude-code":
		if err := hookinstall.UninstallGlobal(); err != nil {
			exitErr("uninstall_global", err.Error())
		}
	default:
		exitErr("unknown_surface", *surface)
	}
	fmt.Println(`{"ok":true}`)
}

func cmdHealth(args []string) {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	dir := fs.String("dir", ".chitin", "path to .chitin state dir")
	windowHours := fs.Int("window-hours", 24, "window size in hours")
	fs.Parse(args)
	if *windowHours <= 0 {
		exitErr("invalid_window_hours", "--window-hours must be > 0")
	}
	absDir, err := filepath.Abs(*dir)
	if err != nil {
		exitErr("health_abs", err.Error())
	}
	rep, err := health.Gather(absDir, time.Duration(*windowHours)*time.Hour)
	if err != nil {
		exitErr("health", err.Error())
	}
	out, err := json.Marshal(rep)
	if err != nil {
		exitErr("health_marshal", err.Error())
	}
	fmt.Println(string(out))
}

func exitErr(kind, msg string) {
	out, _ := json.Marshal(map[string]string{"error": kind, "message": msg})
	fmt.Fprintln(os.Stderr, string(out))
	os.Exit(2)
}

// cmdGate dispatches subcommands: evaluate, status, lockdown, reset.
//
// evaluate: --tool <name> --args-json <json> --agent <name> [--cwd <path>]
//   Stdout: Decision JSON. Exit 0=allow, 1=deny, 2=internal error.
//
// status:   --cwd <path> --agent <name>
// lockdown: --agent <name>
// reset:    --agent <name>
func cmdGate(args []string) {
	if len(args) < 1 {
		exitErr("gate_no_subcommand", "usage: chitin-kernel gate {evaluate|status|lockdown|reset} [flags]")
	}
	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "evaluate":
		cmdGateEvaluate(subArgs)
	case "status":
		cmdGateStatus(subArgs)
	case "lockdown":
		cmdGateLockdown(subArgs)
	case "reset":
		cmdGateReset(subArgs)
	default:
		exitErr("gate_unknown_subcommand", sub)
	}
}

func cmdGateEvaluate(args []string) {
	fs := flag.NewFlagSet("gate evaluate", flag.ExitOnError)
	tool := fs.String("tool", "", "tool name (e.g. terminal, write_file)")
	argsJSON := fs.String("args-json", "{}", "tool args as JSON")
	agent := fs.String("agent", "", "agent identifier (e.g. hermes)")
	cwd := fs.String("cwd", ".", "cwd the action would execute against")
	fs.Parse(args)

	if *tool == "" || *agent == "" {
		exitErr("gate_missing_args", "--tool and --agent required")
	}

	var argsMap map[string]any
	if err := json.Unmarshal([]byte(*argsJSON), &argsMap); err != nil {
		exitErr("gate_bad_args_json", err.Error())
	}

	action, err := gov.Normalize(*tool, argsMap)
	if err != nil {
		exitErr("gate_normalize", err.Error())
	}
	action.Path = *cwd

	absCwd, _ := filepath.Abs(*cwd)
	policy, _, err := gov.LoadWithInheritance(absCwd)
	if err != nil {
		// Distinguish "no policy found" (intentional deny, exit 1) from
		// "policy invalid" (misconfiguration, exit 2) so the plugin's
		// scope-override fallthrough only applies to genuinely-unpoliced
		// cwds — not to silent policy bugs.
		errMsg := err.Error()
		isNoPolicy := strings.HasPrefix(errMsg, "no_policy_found")
		ruleID := "no_policy_found"
		exitCode := 1
		if !isNoPolicy {
			ruleID = "policy_invalid"
			exitCode = 2
		}
		out := map[string]any{
			"allowed": false, "mode": "enforce", "rule_id": ruleID,
			"reason":        errMsg,
			"action_type":   string(action.Type),
			"action_target": action.Target,
		}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
		os.Exit(exitCode)
	}

	home, _ := os.UserHomeDir()
	chitinDir := filepath.Join(home, ".chitin")
	_ = os.MkdirAll(chitinDir, 0o755)
	counter, err := gov.OpenCounter(filepath.Join(chitinDir, "gov.db"))
	if err != nil {
		exitErr("gate_counter", err.Error())
	}
	defer counter.Close()

	gate := &gov.Gate{
		Policy: policy, Counter: counter,
		LogDir: chitinDir, Cwd: absCwd,
	}
	d := gate.Evaluate(action, *agent)

	// Include action metadata in the CLI output. The Decision struct tags
	// Action as json:"-" to keep the chain-event payload lean, but the CLI
	// caller often wants to see what was evaluated without parsing args back.
	b, _ := json.Marshal(d)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		exitErr("gate_marshal_decision", err.Error())
	}
	out["action_type"] = string(action.Type)
	out["action_target"] = action.Target
	b, _ = json.Marshal(out)
	fmt.Println(string(b))
	if d.Allowed {
		os.Exit(0)
	}
	os.Exit(1)
}

func cmdGateStatus(args []string) {
	fs := flag.NewFlagSet("gate status", flag.ExitOnError)
	cwd := fs.String("cwd", ".", "cwd to load policy from")
	agent := fs.String("agent", "", "agent to report state for")
	fs.Parse(args)

	absCwd, _ := filepath.Abs(*cwd)
	policy, sources, err := gov.LoadWithInheritance(absCwd)
	if err != nil {
		exitErr("status_load_policy", err.Error())
	}

	home, _ := os.UserHomeDir()
	counter, err := gov.OpenCounter(filepath.Join(home, ".chitin", "gov.db"))
	if err != nil {
		exitErr("status_counter", err.Error())
	}
	defer counter.Close()

	level := "unset"
	locked := false
	if *agent != "" {
		level = counter.Level(*agent)
		locked = counter.IsLocked(*agent)
	}

	out := map[string]any{
		"policy_id": policy.ID, "mode": policy.Mode,
		"policy_sources": sources,
		"rules_count": len(policy.Rules),
		"agent": *agent, "level": level, "locked": locked,
	}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
}

func cmdGateLockdown(args []string) {
	fs := flag.NewFlagSet("gate lockdown", flag.ExitOnError)
	agent := fs.String("agent", "", "agent to lock down")
	fs.Parse(args)
	if *agent == "" {
		exitErr("lockdown_missing_agent", "--agent required")
	}
	counter, err := openStateCounter()
	if err != nil {
		exitErr("lockdown_counter", err.Error())
	}
	defer counter.Close()
	counter.Lockdown(*agent)
	fmt.Println(`{"ok":true,"action":"lockdown","agent":"` + *agent + `"}`)
}

func cmdGateReset(args []string) {
	fs := flag.NewFlagSet("gate reset", flag.ExitOnError)
	agent := fs.String("agent", "", "agent to reset")
	fs.Parse(args)
	if *agent == "" {
		exitErr("reset_missing_agent", "--agent required")
	}
	counter, err := openStateCounter()
	if err != nil {
		exitErr("reset_counter", err.Error())
	}
	defer counter.Close()
	counter.Reset(*agent)
	fmt.Println(`{"ok":true,"action":"reset","agent":"` + *agent + `"}`)
}

// openStateCounter opens ~/.chitin/gov.db, creating the parent dir if needed.
// Shared by gate subcommands that need to touch escalation state without
// a full policy load.
func openStateCounter() (*gov.Counter, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("user home: %w", err)
	}
	stateDir := filepath.Join(home, ".chitin")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir state dir: %w", err)
	}
	return gov.OpenCounter(filepath.Join(stateDir, "gov.db"))
}
