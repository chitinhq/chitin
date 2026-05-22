# Quickstart: Proving a Driver Is Governed

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

The probe procedure that verifies a driver's governance — the acceptance test
for US1 and US3, and the validation `chitin doctor` performs internally (US4).
A driver is **proven governed** only when this procedure yields an attributed
`gov-decision`. A green `chitin doctor` line is not proof on its own — that
false positive is the defect US4 fixes.

## The probe (per driver)

1. **Create a fresh worktree** (constitution §2 — never probe in the primary
   checkout):
   ```bash
   git worktree add --detach /tmp/gov-probe-<driver> HEAD
   ```

2. **Run the driver one-shot** in that worktree with a single, trivial,
   tool-using task and a unique token:
   ```bash
   # claude / codex / gemini / copilot — non-interactive, one shell call:
   "Run exactly one shell command: echo <unique-token> — then stop."
   ```
   For hermes/clawta, seed a tiny unit of work via the swarm
   (`scripts/smoke-hermes-clawta-chain.sh` automates this).

3. **Check the telemetry**: a Governance Decision attributed to that driver
   must appear, dated within the probe window:
   ```bash
   jq -c 'select(.driver=="<driver>" or .agent=="<driver>")' \
     ~/.chitin/gov-decisions-$(date -u +%F).jsonl | tail
   ```

4. **Tear the worktree down**:
   ```bash
   git worktree remove --force /tmp/gov-probe-<driver>
   ```

## Verdict rules

- **governed** — step 3 returned ≥1 attributed decision.
- **ungoverned** — the driver ran (step 2 succeeded) but step 3 returned
  nothing.
- **unverified** — the driver's CLI could not start (e.g. unauthenticated).
  Distinct from ungoverned — report as `unverified`, never `governed`.

## After a kernel redeploy (US1 / US2)

```bash
# 1. redeploy — rebuilds chitin-kernel from main, smoke-tests, installs.
bash scripts/install-kernel.sh

# 2. confirm the binary is current (not stale).
chitin health        # must not report kernel-staleness

# 3. for hermes/clawta only — restart so the runtime loads the new plugins,
#    then re-probe (step 1–4 above) and confirm decisions resume.
```

## What "done" looks like

- Every runnable driver: probe → attributed `gov-decision` (SC-001).
- `chitin doctor` verdict == probe result for every driver (SC-005).
- One `jq`/query against the unified interface shows all drivers (SC-004).
- A merged kernel change reaches the running binary within the redeploy
  cadence (SC-006).
