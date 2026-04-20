package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/chain"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/emit"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
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
	fs.Parse(args)
	if *sessionID == "" {
		exitErr("missing_session_id", "--session-id required")
	}
	if *adapter == "" {
		exitErr("missing_adapter", "--adapter or CHITIN_ADAPTER_BINARY required")
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

func exitErr(kind, msg string) {
	out, _ := json.Marshal(map[string]string{"error": kind, "message": msg})
	fmt.Fprintln(os.Stderr, string(out))
	os.Exit(2)
}
