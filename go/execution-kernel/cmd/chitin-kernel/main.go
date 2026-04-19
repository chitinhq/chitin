package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

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

func exitErr(kind, msg string) {
	out, _ := json.Marshal(map[string]string{"error": kind, "message": msg})
	fmt.Fprintln(os.Stderr, string(out))
	os.Exit(2)
}
