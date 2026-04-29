package cost

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ExecutorRate is the cost-rate snapshot for one executor.
//
// USDPerInputKtok and USDPerOutputKtok are approximate, informational
// values pinned in chitin.yaml. They do NOT drive real-time cap
// enforcement — calls + bytes do. They are surfaced via
// `envelope inspect` and `envelope tail --stats` as
// "informational, approximate" $ figures.
//
// Sources (snapshot 2026-04-29; verify before quoting):
//   - claude-code  (Anthropic Sonnet 4.6): $3 / Mtok input, $15 / Mtok output
//   - claude-code-local (localhost Ollama bridge): {0,0} — local inference
//   - copilot-cli  (Copilot CLI flat-rate):
//       fictional per-token; the "premium request" cap is the real
//       constraint. We pin a placeholder for telemetry uniformity but
//       this dimension is fundamentally a lie until we can OTEL-reconcile
//       against actual model.usage data from the upstream surface.
type ExecutorRate struct {
	USDPerInputKtok  float64 `yaml:"usd_per_input_ktok"`
	USDPerOutputKtok float64 `yaml:"usd_per_output_ktok"`
	BytesPerToken    float64 `yaml:"bytes_per_token"`
}

// RateTable is keyed by executor (e.g. "copilot-cli", "claude-code").
type RateTable map[string]ExecutorRate

// DefaultRates returns a baseline table for the executors we support
// today. Values are intentionally documented as "informational,
// approximate" — the cap enforcement is on calls + bytes.
func DefaultRates() RateTable {
	return RateTable{
		"claude-code":       {USDPerInputKtok: 0.003, USDPerOutputKtok: 0.015, BytesPerToken: 4},
		"claude-code-local": {USDPerInputKtok: 0, USDPerOutputKtok: 0, BytesPerToken: 4},
		"copilot-cli":       {USDPerInputKtok: 0, USDPerOutputKtok: 0, BytesPerToken: 4},
	}
}

// LoadRates reads `cost.rates.<executor>` from a chitin.yaml file at
// path, merging on top of DefaultRates. Missing file → defaults; merge
// is per-executor (loaded entries override defaults).
func LoadRates(path string) (RateTable, error) {
	out := DefaultRates()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("read rates: %w", err)
	}
	var raw struct {
		Cost struct {
			Rates map[string]ExecutorRate `yaml:"rates"`
		} `yaml:"cost"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse rates: %w", err)
	}
	for k, v := range raw.Cost.Rates {
		out[k] = v
	}
	return out, nil
}
