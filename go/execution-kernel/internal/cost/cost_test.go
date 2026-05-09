package cost

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestEstimate_DefaultsForUnknownExecutor(t *testing.T) {
	d := Estimate(gov.Action{Type: gov.ActFileRead, Target: "/etc/hosts"}, "unknown-agent", DefaultRates())
	if d.ToolCalls != 1 {
		t.Fatalf("ToolCalls=%d want 1", d.ToolCalls)
	}
	if d.InputBytes != int64(len("/etc/hosts")) {
		t.Fatalf("InputBytes=%d want %d", d.InputBytes, len("/etc/hosts"))
	}
	if d.USD != 0 {
		t.Fatalf("USD=%v want 0 for unknown executor", d.USD)
	}
}

func TestEstimate_LocalExecutorIsFree(t *testing.T) {
	d := Estimate(
		gov.Action{Type: gov.ActFileRead, Target: "/path"},
		"claude-code-local", DefaultRates(),
	)
	if d.USD != 0 {
		t.Fatalf("local USD=%v want 0", d.USD)
	}
	if d.ToolCalls != 1 {
		t.Fatalf("local ToolCalls=%d want 1", d.ToolCalls)
	}
}

func TestEstimate_TierBlind(t *testing.T) {
	// Identical action targets across different ActionTypes should
	// produce identical InputBytes — Estimate is tier-blind, the
	// type doesn't change byte counting.
	rates := DefaultRates()
	read := Estimate(gov.Action{Type: gov.ActFileRead, Target: "abc"}, "claude-code", rates)
	write := Estimate(gov.Action{Type: gov.ActFileWrite, Target: "abc"}, "claude-code", rates)
	if read.InputBytes != write.InputBytes {
		t.Fatalf("read.InputBytes=%d != write.InputBytes=%d (tier should not affect bytes)",
			read.InputBytes, write.InputBytes)
	}
}

func TestEstimate_USDScalesWithBytes(t *testing.T) {
	rates := RateTable{
		"x": {USDPerInputKtok: 1.0, BytesPerToken: 4},
	}
	// 4000 bytes / 4 BytesPerToken = 1000 tokens = 1 ktok = $1
	d := Estimate(gov.Action{Type: gov.ActFileRead, Target: string(make([]byte, 4000))}, "x", rates)
	if d.USD < 0.99 || d.USD > 1.01 {
		t.Fatalf("USD=%v want ~1.0", d.USD)
	}
}

func TestEstimate_OutputBytesAlwaysZero(t *testing.T) {
	// Estimate fires at PreToolUse time — output size is unknowable,
	// so OutputBytes must always be 0.
	rates := RateTable{
		"x": {USDPerInputKtok: 3.0, USDPerOutputKtok: 15.0, BytesPerToken: 4},
	}
	d := Estimate(gov.Action{Type: gov.ActShellExec, Target: "ls -la"}, "x", rates)
	if d.OutputBytes != 0 {
		t.Fatalf("OutputBytes=%d want 0 (unknowable at gate time)", d.OutputBytes)
	}
}

func TestEstimate_EmptyTarget(t *testing.T) {
	// Empty target string: InputBytes=0, USD should also be 0.
	rates := RateTable{
		"claude-code": {USDPerInputKtok: 0.003, BytesPerToken: 4},
	}
	d := Estimate(gov.Action{Type: gov.ActShellExec, Target: ""}, "claude-code", rates)
	if d.InputBytes != 0 {
		t.Fatalf("InputBytes=%d want 0 for empty target", d.InputBytes)
	}
	if d.USD != 0 {
		t.Fatalf("USD=%v want 0 for zero-length input", d.USD)
	}
}

func TestEstimate_ZeroBytesPerTokenFallback(t *testing.T) {
	// If BytesPerToken is 0, Estimate should fall back to default (4).
	rates := RateTable{
		"broken": {USDPerInputKtok: 1.0, BytesPerToken: 0},
	}
	// 12 bytes / fallback BPT(4) = 3 tokens → 3 * 1.0 / 1000 = 0.003
	d := Estimate(gov.Action{Type: gov.ActFileRead, Target: "/a/b/c/d/e/f"}, "broken", rates)
	if d.USD < 0.002 || d.USD > 0.004 {
		t.Fatalf("USD=%v want ~0.003 (fallback BytesPerToken=4)", d.USD)
	}
}

func TestEstimate_NegativeBytesPerTokenFallback(t *testing.T) {
	// Negative BytesPerToken should also trigger the fallback.
	rates := RateTable{
		"neg": {USDPerInputKtok: 1.0, BytesPerToken: -1},
	}
	d := Estimate(gov.Action{Type: gov.ActFileRead, Target: "/a/b/c/d/e/f"}, "neg", rates)
	if d.USD < 0.002 || d.USD > 0.004 {
		t.Fatalf("USD=%v want ~0.003 (fallback for negative BPT)", d.USD)
	}
}

func TestEstimate_CopilotCLIHasZeroUSD(t *testing.T) {
	d := Estimate(gov.Action{Type: gov.ActShellExec, Target: "npm install"}, "copilot-cli", DefaultRates())
	if d.USD != 0 {
		t.Fatalf("copilot-cli USD=%v want 0 (flat-rate model)", d.USD)
	}
}

func TestLoadRates_MissingFile(t *testing.T) {
	rates, err := LoadRates(filepath.Join(t.TempDir(), "nosuch.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// Should return defaults when file doesn't exist.
	if _, ok := rates["claude-code"]; !ok {
		t.Fatal("expected claude-code in default rates")
	}
}

func TestLoadRates_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte(`cost:
  rates:
    claude-code:
      usd_per_input_ktok: 0.005
      usd_per_output_ktok: 0.025
      bytes_per_token: 3.5
    custom-agent:
      usd_per_input_ktok: 0.01
      bytes_per_token: 2
`)
	if err := os.WriteFile(filepath.Join(dir, "chitin.yaml"), yaml, 0644); err != nil {
		t.Fatal(err)
	}

	rates, err := LoadRates(filepath.Join(dir, "chitin.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// Default entries should be present.
	if _, ok := rates["copilot-cli"]; !ok {
		t.Fatal("expected copilot-cli default to survive merge")
	}
	// Overridden entry should have new values.
	cc := rates["claude-code"]
	if cc.USDPerInputKtok != 0.005 {
		t.Fatalf("claude-code USDPerInputKtok=%v want 0.005", cc.USDPerInputKtok)
	}
	if cc.BytesPerToken != 3.5 {
		t.Fatalf("claude-code BytesPerToken=%v want 3.5", cc.BytesPerToken)
	}
	// New executor should be merged in.
	if _, ok := rates["custom-agent"]; !ok {
		t.Fatal("expected custom-agent to be merged from chitin.yaml")
	}
}

func TestLoadRates_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte(`not: [valid: yaml: structure`)
	if err := os.WriteFile(filepath.Join(dir, "chitin.yaml"), yaml, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRates(filepath.Join(dir, "chitin.yaml"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadRates_EmptyYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chitin.yaml"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	rates, err := LoadRates(filepath.Join(dir, "chitin.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// Empty file should yield defaults.
	if len(rates) != len(DefaultRates()) {
		t.Fatalf("empty YAML: got %d rates, want %d defaults", len(rates), len(DefaultRates()))
	}
}
