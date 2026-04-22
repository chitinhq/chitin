# Chitin Governance Setup

Chitin's governance layer (`chitin-kernel gate`) evaluates every agent tool
call against `chitin.yaml` and either allows, denies silently (enforce),
or denies with educational feedback (guide).

## Quick install (hermes)

1. Build the kernel:
   ```bash
   cd go/execution-kernel && go build -o ~/.local/bin/chitin-kernel ./cmd/chitin-kernel
   ```

2. Install the hermes plugin:
   ```bash
   mkdir -p ~/.hermes/plugins/chitin-governance
   # Copy the plugin files from the chitin-governance-v1 worktree's deployment
   # bundle — see the plan at docs/superpowers/plans/2026-04-22-chitin-governance-v1.md
   # Task 12 for verbatim contents.
   ```

3. Enable the plugin in `~/.hermes/config.yaml`:
   ```yaml
   plugins:
     enabled:
       - chitin-sink
       - chitin-governance
   ```

4. Restart hermes gateway: `hermes gateway restart`.

## The three modes

- **monitor** — log decisions; allow execution. Governance-visible but non-blocking. Use during policy development.
- **enforce** — block silently; return `reason` only. No agent-readable educational feedback.
- **guide** — block AND return `reason` + `suggestion` + `correctedCommand` as the agent's next-turn input. The agent sees why it was blocked and the recommended alternative.

Global `mode:` sets the default. Per-rule `invariantModes:` overrides.

## Kill switches

- **Soft**: set `mode: monitor` in `chitin.yaml`. All denials become log-only.
- **Hard**: `chitin-kernel gate lockdown --agent=<agent-name>`. That agent is denied all actions until reset.
- **Clear**: `chitin-kernel gate reset --agent=<agent-name>`.

## Escalation ladder

Denials accumulate per-agent in `~/.chitin/gov.db`:
- 0–2 denials: **normal** — deny with feedback.
- 3–6: **elevated** — feedback includes a warning.
- 7–9: **high** — tighter restrictions (reserved for v2 policy features).
- 10+: **lockdown** — agent-wide; all actions denied.

Lockdown is sticky across sessions. Only `gate reset` clears.

## CLI reference

```bash
chitin-kernel gate evaluate --tool=<name> --args-json=<json> --agent=<name> [--cwd=<path>]
chitin-kernel gate status --cwd=<path> --agent=<name>
chitin-kernel gate lockdown --agent=<name>
chitin-kernel gate reset --agent=<name>
```

Exit codes: 0 = allow, 1 = deny, 2 = internal error.

## Decision log

Every gate call appends one JSON line to `~/.chitin/gov-decisions-<YYYY-MM-DD>.jsonl`.
v2 will add `chitin-kernel ingest-policy` to fold these into the chitin event chain.
