# Unknown-Rate Alarm Runbook

Chitin's `unknown` action type is the fail-closed bucket for tool calls that
the driver normalizers do not understand. A sustained unknown rate means a
driver surface changed or a dispatcher routed the wrong tool shape into a
normalizer.

## Alarm

Run the guardrail against the local chain:

```sh
cd go/execution-kernel
CHITIN_UNKNOWN_RATE_DIR="$HOME/.chitin" \
CHITIN_UNKNOWN_RATE_DAY="$(date -u +%F)" \
go test ./internal/gov -run TestUnknownRateAlarmCurrentChainOptIn -count=1
```

The test fails when any `(agent, UTC day)` bucket has an unexpected unknown
rate of at least `1%`.

Omit `CHITIN_UNKNOWN_RATE_DAY` for forensics across every
`gov-decisions-*.jsonl` file in the state directory. Historical bad days, such
as `2026-05-07`, will still fail by design.

The alarm excludes the documented Hermes intentional-unknown tools:

- `clarify`
- `image_generate`
- `text_to_speech`
- `vision_analyze`
- `cronjob`

These are chat/modality/scheduling surfaces that intentionally still map to
`unknown` until chitin has a stable canonical action for them.

## Triage

1. Read the failing test output. It includes:
   - `(agent, day)`
   - unknown count, total count, and rate
   - top unknown tool names with counts
   - sample locations as `chain_id:seq` when present, otherwise
     `gov-decisions-YYYY-MM-DD.jsonl:line`
2. Inspect the raw daily log:

```sh
jq -c 'select(.agent=="AGENT" and .action_type=="unknown") |
  {ts, agent, action_target, rule_id, chain_id, seq}' \
  "$HOME/.chitin/gov-decisions-YYYY-MM-DD.jsonl" | head -50
```

3. Decide whether each top tool is:
   - a real new tool that needs a canonical mapping in the relevant
     `internal/driver/<driver>/normalize.go`
   - a cross-driver leak that should be normalized through the correct driver
     or fixed in the upstream dispatcher
   - an intentional unknown that belongs in the documented whitelist

## Known Reference Points

- `2026-05-07`: Hermes hit roughly `67%` unknowns because runtime plumbing such
  as `kanban_show`, `kanban_block`, and `process` was unmapped.
- `2026-05-09`: after the Hermes fixes, the remaining unknowns were much lower
  and concentrated in specific unmapped tools such as `skills_list`, `memory`,
  and `skill_manage`, plus intentional `clarify` calls.

## Fix

Add or repair the driver normalizer mapping, then add focused tests near that
driver. For Hermes, start with:

- `go/execution-kernel/internal/driver/hermes/normalize.go`
- `go/execution-kernel/internal/gov/action.go`

Do not route approvals, orchestration, or LLM consultation into chitin. The
kernel should only normalize, govern, record the chain, and stamp pure-Go
signals.
