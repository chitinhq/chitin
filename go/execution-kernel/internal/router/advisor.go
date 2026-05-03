package router

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

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
