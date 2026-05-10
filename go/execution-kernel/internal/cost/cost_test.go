package cost

import (
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

func TestEstimate_ZeroBytesPerToken_Defaults(t *testing.T) {
	rates := RateTable{
		"test-agent": {USDPerInputKtok: 5.0, BytesPerToken: 0},
	}
	// BytesPerToken <= 0 should default to 4
	d := Estimate(gov.Action{Type: gov.ActFileRead, Target: "abcd"}, "test-agent", rates)
	if d.USD == 0 {
		t.Errorf("USD=0 with zero BPT; expected default BPT=4 to be used")
	}
	// 4 bytes / 4 BPT = 1 token, 1/1000 ktok * $5 = $0.005
	if d.USD < 0.004 || d.USD > 0.006 {
		t.Errorf("USD=%v, want ~0.005 with default BPT", d.USD)
	}
}

func TestEstimate_NegativeBytesPerToken_Defaults(t *testing.T) {
	rates := RateTable{
		"test-agent": {USDPerInputKtok: 2.0, BytesPerToken: -1},
	}
	d := Estimate(gov.Action{Type: gov.ActFileRead, Target: string(make([]byte, 8))}, "test-agent", rates)
	if d.USD == 0 {
		t.Errorf("USD=0 with negative BPT; expected default BPT=4 to be used")
	}
}
