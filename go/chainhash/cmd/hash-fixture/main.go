// Command hash-fixture computes chainhash.HashEvent for every event in the
// parity corpus and prints the results as JSON to stdout.
//
// It is the shared fixture the cross-language hash-parity test
// (libs/run-sdk/tests/hash-parity.test.ts) invokes to compare the Go hash
// output against the TypeScript hashEvent output. Run from the module root:
//
//	go run ./cmd/hash-fixture
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/chitinhq/chitin/go/chainhash"
)

type corpusEntry struct {
	Name  string         `json:"name"`
	Event map[string]any `json:"event"`
}

type result struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

func main() {
	data, err := os.ReadFile("testdata/parity-corpus.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "read corpus:", err)
		os.Exit(1)
	}
	var corpus []corpusEntry
	if err := json.Unmarshal(data, &corpus); err != nil {
		fmt.Fprintln(os.Stderr, "parse corpus:", err)
		os.Exit(1)
	}
	out := make([]result, 0, len(corpus))
	for _, e := range corpus {
		h, err := chainhash.HashEvent(e.Event)
		if err != nil {
			fmt.Fprintf(os.Stderr, "hash %s: %v\n", e.Name, err)
			os.Exit(1)
		}
		out = append(out, result{Name: e.Name, Hash: h})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
}
