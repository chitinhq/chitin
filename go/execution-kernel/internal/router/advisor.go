package router

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Why an advisor here at all, given that Hermes ships a "smart mode"
// approval gate (`approvals.mode: smart` in Hermes' `config.yaml`)?
// The two systems answer different questions and live at different
// layers of the stack. Hermes' smart mode is a per-tool-call,
// per-Hermes-driver gate that decides whether to surface an approval
// prompt to the operator on dangerous patterns; it is a routing
// decision local to one driver's loop and authoritative only inside
// Hermes' own runtime.
//
// chitin's advisor is upstream of any specific driver. It sits behind
// `chitin-kernel router evaluate --hook-stdin`, which is wired into
// every driver chitin gates (Claude Code, Codex, Gemini, Hermes
// itself). It runs only when a kernel-level heuristic (blast radius,
// floundering chain, plugin signal) crosses a threshold the policy
// declared as advisor-worthy, and its output is a structured
// continue/takeover verdict plus a nudge — not an operator prompt.
// The verdict feeds the chain envelope and gov-decisions audit log
// the same way any other gate decision does.
//
// The asymmetry is deliberate. Hermes' smart mode is the right home
// for "ask the human now" because Hermes owns the chat surface where
// the human already is. chitin's advisor is the right home for
// "ask another model whether this looks off" because chitin owns the
// cross-driver canonical-action vocabulary the heuristics score
// against. Neither subsumes the other; collapsing them would either
// force chitin to re-implement Hermes' gateway (the mistake culled
// on 2026-05-08, see
// docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md) or
// force Hermes' approval gate to understand chitin's chain-replay
// semantics. The advisor justifies itself by being the only LLM
// second-opinion that runs across all drivers from one canonical
// action vocabulary; smart mode justifies itself by owning the chat.

// CallAdvisor spawns `claude -p` (sub-billed Claude Code Pro plan)
// with a structured advisor prompt and parses the
// <<<ROUTER_ADVISOR>>>{...} marker from stdout. Returns
// (response, nil) on success, (nil, err) on failure.
//
// Constraint: NO metered API calls. We use the Claude Code CLI's
// `claude -p` interface which authenticates against the user's
// Claude Code subscription. Per the Anthropic AUP clarification
// (2026-05-02), headless `claude -p` is supported for automation.
func CallAdvisor(ctx context.Context, req AdvisorRequest, binary string, timeout time.Duration) (*AdvisorResponse, error) {
	if binary == "" {
		binary = "claude"
	}
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	prompt := buildAdvisorPrompt(req)
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, binary, "-p", "--dangerously-skip-permissions", "--output-format", "text")
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("advisor spawn failed: %w", err)
	}
	resp := parseAdvisorOutput(string(out))
	if resp == nil {
		return nil, fmt.Errorf("advisor output missing or malformed marker")
	}
	return resp, nil
}

func buildAdvisorPrompt(req AdvisorRequest) string {
	heur, _ := json.MarshalIndent(req.HeuristicOutcome, "", "  ")
	action, _ := json.MarshalIndent(req.ProposedAction, "", "  ")
	return fmt.Sprintf(`You are a chitin ROUTER ADVISOR — called when a lower-tier agent's tool call triggered a heuristic threshold.

Your job: decide whether the agent should proceed (with what nudge), OR whether the action should be blocked / escalated to a higher-tier human takeover.

PROPOSED ACTION:
%s

HEURISTIC OUTCOME:
%s

CALLER CONTEXT:
- caller_tier: %s
- chain_depth: %d (max chain_depth before takeover: typically 3)

QUESTION:
%s

CONTEXT:
%s

YOUR OUTPUT — emit EXACTLY this JSON line as your final output (no other text after it):

<<<ROUTER_ADVISOR>>>{"nudge": "<short text shown to the agent>", "verdict": "continue"|"takeover", "escalate": <bool>}
`,
		string(action), string(heur), req.CallerTier, req.ChainDepth,
		req.Question, req.Context,
	)
}

func parseAdvisorOutput(stdout string) *AdvisorResponse {
	const marker = "<<<ROUTER_ADVISOR>>>"
	lastIdx := strings.LastIndex(stdout, marker)
	if lastIdx < 0 {
		return nil
	}
	rest := stdout[lastIdx+len(marker):]
	if !strings.HasPrefix(rest, "{") {
		return nil
	}
	depth := 0
	end := -1
	for i, ch := range rest {
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
	}
	if end < 0 {
		return nil
	}
	var resp AdvisorResponse
	if err := json.Unmarshal([]byte(rest[:end]), &resp); err != nil {
		return nil
	}
	if resp.Verdict != "continue" && resp.Verdict != "takeover" {
		return nil
	}
	return &resp
}
