package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, `{"error":"no_subcommand","message":"chitin-kernel <subcommand> [...]"}`)
		os.Exit(2)
	}
	// Subcommands land in Task 8+. For now:
	fmt.Fprintf(os.Stderr, `{"error":"unknown_subcommand","message":"%s"}`+"\n", os.Args[1])
	os.Exit(2)
}
