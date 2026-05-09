package cost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRates_MissingFileReturnsDefaults(t *testing.T) {
	rates, err := LoadRates("/nonexistent/path/chitin.yaml")
	if err != nil {
		t.Fatalf("LoadRates missing file: %v", err)
	}
	def := DefaultRates()
	if len(rates) != len(def) {
		t.Errorf("missing file rates len=%d, want %d", len(rates), len(def))
	}
	for k, v := range def {
		if rates[k] != v {
			t.Errorf("rates[%q]=%v, want %v", k, rates[k], v)
		}
	}
}

func TestLoadRates_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	yamlContent := []byte("cost:\n  rates:\n    claude-code:\n      usd_per_input_ktok: 0.005\n      usd_per_output_ktok: 0.025\n      bytes_per_token: 3\n")
	path := filepath.Join(dir, "chitin.yaml")
	if err := os.WriteFile(path, yamlContent, 0o644); err != nil {
		t.Fatal(err)
	}

	rates, err := LoadRates(path)
	if err != nil {
		t.Fatalf("LoadRates: %v", err)
	}

	// Overridden executor
	if rates["claude-code"].USDPerInputKtok != 0.005 {
		t.Errorf("claude-code USDPerInputKtok=%v, want 0.005", rates["claude-code"].USDPerInputKtok)
	}
	if rates["claude-code"].USDPerOutputKtok != 0.025 {
		t.Errorf("claude-code USDPerOutputKtok=%v, want 0.025", rates["claude-code"].USDPerOutputKtok)
	}
	if rates["claude-code"].BytesPerToken != 3 {
		t.Errorf("claude-code BytesPerToken=%v, want 3", rates["claude-code"].BytesPerToken)
	}

	// Defaults still present for non-overridden executors
	if _, ok := rates["claude-code-local"]; !ok {
		t.Error("claude-code-local missing from merged rates")
	}
	if _, ok := rates["copilot-cli"]; !ok {
		t.Error("copilot-cli missing from merged rates")
	}
}

func TestLoadRates_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chitin.yaml")
	if err := os.WriteFile(path, []byte("cost:\n  rates:\n    - not_a_map\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRates(path)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestLoadRates_EmptyFileReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chitin.yaml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	rates, err := LoadRates(path)
	if err != nil {
		t.Fatalf("LoadRates empty file: %v", err)
	}
	def := DefaultRates()
	if len(rates) != len(def) {
		t.Errorf("empty file rates len=%d, want %d", len(rates), len(def))
	}
}

func TestLoadRates_NewExecutor(t *testing.T) {
	dir := t.TempDir()
	yamlContent := []byte("cost:\n  rates:\n    my-custom-executor:\n      usd_per_input_ktok: 0.01\n      usd_per_output_ktok: 0.05\n      bytes_per_token: 2\n")
	path := filepath.Join(dir, "chitin.yaml")
	if err := os.WriteFile(path, yamlContent, 0o644); err != nil {
		t.Fatal(err)
	}

	rates, err := LoadRates(path)
	if err != nil {
		t.Fatalf("LoadRates: %v", err)
	}

	custom, ok := rates["my-custom-executor"]
	if !ok {
		t.Fatal("my-custom-executor not in rates")
	}
	if custom.USDPerInputKtok != 0.01 {
		t.Errorf("custom USDPerInputKtok=%v, want 0.01", custom.USDPerInputKtok)
	}
	if custom.BytesPerToken != 2 {
		t.Errorf("custom BytesPerToken=%v, want 2", custom.BytesPerToken)
	}

	// Defaults still present
	if len(rates) < 3 { // 3 defaults + 1 custom
		t.Errorf("expected at least 4 rates, got %d", len(rates))
	}
}