// chitin-kernel reads an agent hook payload on stdin, normalizes it
// into a canonical event, and appends it to the local JSONL ground
// truth. Phase 1 is monitor-only: this binary never blocks a tool call.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "chitin-kernel: skeleton — full wiring in Phase 7")
	os.Exit(0) // Phase 1 monitor-only — always allow.
}
