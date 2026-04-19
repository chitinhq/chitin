package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/chain"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/emit"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
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

func exitErr(kind, msg string) {
	out, _ := json.Marshal(map[string]string{"error": kind, "message": msg})
	fmt.Fprintln(os.Stderr, string(out))
	os.Exit(2)
}
