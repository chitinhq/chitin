// Package cost estimates per-Action cost as a CostDelta keyed on the
// executor (the agent actually running the tool call). The estimate
// is tier-blind: T0 vs T2 is metadata for downstream routing, not an
// input to cost.
//
// Real-time cap enforcement uses InputBytes and ToolCalls — both of
// which are observable at PreToolUse time. USD is informational and
// best-effort; per-token rates are partly fictional for Copilot CLI's
// flat-rate model and are pinned snapshots in chitin.yaml. Real $USD
// reconciliation comes later via OTEL ingest.
//
// See spec §"Why calls + bytes, not $USD".
package cost

import (
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// Estimate returns the CostDelta to debit for one Action by `executor`.
//
// The executor key is the agent that actually runs the tool call —
// e.g. "copilot-cli" for the openclaw acpx path, "claude-code" for the
// Anthropic-cloud Claude Code session, "claude-code-local" when
// ANTHROPIC_BASE_URL points at a localhost Ollama bridge. Unknown
// executors get a {ToolCalls:1, InputBytes:len(target)} default —
// honest about not knowing the rate, but still counts what we can.
//
// The byte estimate is the length of Action.Target (file path, command,
// URL — whichever applies). For richer estimates that include payload
// size, the caller should pre-populate Action.Params and the rate map
// can scale via BytesPerToken.
//
// OutputBytes is intentionally always 0 here. PreToolUse fires before
// the model has seen tool output, so output size is unknowable at gate
// time. Post-hoc OTEL ingest of model.usage will populate it later.
//
// USDPerOutputKtok on the rate row is currently unused by Estimate for
// the same reason: we have no OutputBytes to apply it to. The field is
// kept on ExecutorRate so chitin.yaml authors can pin output pricing
// alongside input pricing in one place; OTEL ingest will start
// honoring it once it can supply OutputBytes per Decision.
func Estimate(action gov.Action, executor string, rates RateTable) gov.CostDelta {
	delta := gov.CostDelta{
		ToolCalls:  1,
		InputBytes: int64(len(action.Target)),
	}
	rate, ok := rates[executor]
	if !ok {
		return delta
	}
	bpt := rate.BytesPerToken
	if bpt <= 0 {
		bpt = 4
	}
	tokens := float64(delta.InputBytes) / bpt
	delta.USD = tokens * rate.USDPerInputKtok / 1000.0
	return delta
}
