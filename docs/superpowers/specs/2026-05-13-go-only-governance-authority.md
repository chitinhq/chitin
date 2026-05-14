---
status: open
owner: claude-code
kanban: t_742ee3ea
implementation_pr: null
superseded_by: null
effective_from: '2026-05-13'
effective_to: null
---

# Spec: Go is the only governance authority

Date: 2026-05-13
Status: spec — open
Kanban: `t_742ee3ea` (priority 25)
Source: `docs/archive/audits/2026-05-13-architecture-audit.md` — Top finding 1
Author: claude-code (operator-controlled, spec writer)

## Problem

Architecture audit 2026-05-13 measured `gate` references across the
tree: ~65 hits in `go/`, but also live references in `python/`,
`apps/`, `scripts/`, and `swarm/`. `governance` also spans multiple
surfaces.

This is acceptable iff non-Go surfaces only **invoke** the Go gate
(`chitin-kernel gate ...` / `chitin-bridge.mjs` / equivalent adapter)
and never re-evaluate the decision themselves. The audit can't see
the distinction from raw keyword counts. We need a check that turns
the invariant into a CI-enforced property.

Concrete sites observed today:

- `apps/openclaw-plugin-governance/src/chitin-bridge.mjs` — adapter
  that ships chain events from OpenClaw to chitin. Legitimate.
- `apps/cli/src/commands/inspect.ts`, `ledger.ts` — operator-facing
  CLI; surfaces existing decisions. Legitimate read-only.
- `python/analysis/templates/*.py` — `default_deny_unknown.py`,
  `bounds_max_files_changed.py`, `no_destructive_rm.py`. These look
  like Python authoring of rules. **This is the smell.** If those
  templates encode decision logic, they are a second authority.
- `scripts/install-*.sh`, `scripts/chitin-*` — installers and ops
  helpers. Should not contain gate logic, only setup.
- `swarm/bin/*` — orchestration. Should not evaluate gate decisions;
  may *consume* them via `chitin-kernel decisions`.

## Invariant (the claim)

> For every action `a` produced inside chitin's runtime path,
> exactly one process — the `chitin-kernel` Go binary — computes the
> `Allow | Deny` decision. All other surfaces either (a) ship the
> action into the kernel as a gate request, (b) read the result
> back, or (c) display it to a human.

A violation is any non-Go code path that:

1. Reads a rule definition file (`chitin.yaml`, `.chitin/rules/*`)
   and produces a verdict, OR
2. Implements a regex / predicate that decides whether an action is
   permitted (vs. just normalizing or routing it), OR
3. Maintains its own allow/deny list parallel to the kernel's.

## Decision

Add a CI boundary check (`scripts/check-governance-boundary.sh` +
matching CI job) that runs on every PR. The check:

1. Greps non-Go source trees for *decision-shaped* identifiers —
   `Allow`, `Deny`, `Decision`, `Verdict`, `EvaluateRule`,
   `is_allowed`, `should_deny`, etc.
2. Subtracts an **explicit allowlist** of adapter / display files
   (committed at `scripts/governance-boundary.allow`).
3. Fails the build on any unexplained hit, with a message pointing
   at this spec.

The allowlist is the operator's lever — each entry must include a
one-line reason. New adapter code goes through one PR that adds the
file *and* its allowlist entry, so the boundary stays explicit.

## In scope

1. **`scripts/check-governance-boundary.sh`** — the linter. Pure
   shell + ripgrep; runs in <2s on the current tree.
2. **`scripts/governance-boundary.allow`** — text file, one path
   per line, `# reason` permitted. Initial entries seeded from the
   current legitimate adapters listed above.
3. **CI wiring** — invoked from existing PR workflow (alongside the
   other `scripts/check-*.sh` linters). Fails the workflow on hit.
4. **`python/analysis/templates/` audit** — read each template,
   classify as "rule-authoring helper for Go to load" vs "Python
   evaluator that decides at runtime". Rule-authoring helpers are
   adapters (allowlisted). Python evaluators are violations and
   must move into `go/execution-kernel/internal/gov/`.
5. **One-line invariant comment** at the top of
   `go/execution-kernel/internal/gov/gate.go` stating the claim
   above, so future readers find the contract at its enforcement
   point.

## Out of scope (followups)

- AST-level analysis of TypeScript / Python — string-grep is enough
  for the first iteration; the false-positive surface is small
  because the allowlist absorbs it.
- Sandboxing the Go process itself / cryptographic policy signing —
  covered by `2026-05-12-no-gov-self-mod-bypass.md`. This spec is
  about *where the logic lives*, not *who can edit the rule file*.
- Moving the OpenClaw plugin's bridge into Go — adapter surface is
  legitimate; that's a separate consolidation decision.

## Approach detail

### Detection patterns

`check-governance-boundary.sh` runs ripgrep with these patterns,
restricted to non-`go/` paths and excluding tests + this spec:

```
\b(Allow|Deny|Decision|Verdict)\b\s*[:=({\[]
\b(is_allowed|should_(?:allow|deny)|evaluate_(?:rule|gate|policy))\b
\bnew\s+(Gate|Policy)Decision\b
```

Patterns chosen so they fire on **definition** sites
(`Decision = Allow` / `function evaluateRule(...)`) but not on
consumption sites (`if (decision === "Allow")`, `print(verdict)`).
Consumers reading a result back from the kernel string-match
literals — they don't redefine the type.

### Allowlist format

```
# scripts/governance-boundary.allow
apps/openclaw-plugin-governance/src/chitin-bridge.mjs   # adapter: ships openclaw events into chitin-kernel gate
apps/cli/src/commands/inspect.ts                        # read-only operator view of past decisions
apps/cli/src/commands/ledger.ts                         # read-only ledger viewer
# (no python/analysis/templates here — those are audited in §4)
```

Lines starting `#` ignored. Trailing `# reason` mandatory and the
linter rejects entries missing it.

### CI integration

```yaml
- name: governance boundary
  run: bash scripts/check-governance-boundary.sh
```

The linter exits non-zero with output like:

```
governance-boundary: violation
  scripts/foo.sh:42  matched `is_allowed`
  fix: move logic into go/execution-kernel/internal/gov/
       or add the path to scripts/governance-boundary.allow
       with a one-line reason.
```

## Verification

- **Unit-of-correctness:** on a clean tree (post-audit fixes), the
  linter exits 0.
- **Regression:** add a fixture PR that adds
  `python/analysis/evil.py` with `def is_allowed(action): return True`
  and confirm CI rejects it.
- **Allowlist exercise:** add a fixture PR that adds an adapter file
  + allowlist entry; CI passes.
- **Bypass attempt:** try renaming `is_allowed` to `permits` in a
  Python file — linter misses it (expected; pattern set is the
  contract). File a followup to broaden patterns if a real bypass
  appears.

## Done-condition

- `scripts/check-governance-boundary.sh` exists and passes on `main`.
- `scripts/governance-boundary.allow` exists with all current
  legitimate adapters enumerated.
- The CI workflow runs the linter on every PR.
- `python/analysis/templates/` has been audited and each file is
  either (a) moved to Go, (b) refactored into a rule-data file the
  Go kernel loads, or (c) allowlisted with a justification.
- The invariant comment is present at the top of
  `go/execution-kernel/internal/gov/gate.go`.

## Effort

M. Linter + allowlist + CI wiring is a half-day. The Python template
audit is the variable — anywhere from a half-day (if all templates
are rule-data) to two days (if any encode runtime evaluation logic
that must move into Go).
