# Dogfood-Driven Governance-Debt Ledger — Design Spec

**Date:** 2026-04-19
**Status:** Approved, pending user review before writing-plans.
**Active soul during design:** da Vinci (quorum default, 2026-04-19).
**Supersedes:** the "Phase 2 is governance, route to Phase 2 when asked what's next" framing in earlier planning docs. Phase 2 remains the goal; this spec is the evidence pipeline that defines its shape.

## One-line thesis

Turn chitin into permanent, user-level observability on the RTX 3090 dev
box, and layer a lightweight weekly review that produces a
governance-debt ledger feeding three triage lanes (fix / determinism /
soul-routing). The ledger *is* the Phase 2 design input — every future
governance rule cites a `GDL-NNN` entry as its evidence.

## Background / thesis alignment

Addy Osmani's *Agent Stack Bet* names the failure this spec fights:
**governance debt** — the silent accumulation of security and audit risk
from having no visibility into what agents do. His four architectural
bets align precisely with chitin's existing design:

| Addy's bet | Chitin mechanism |
|---|---|
| Agent identity & governance (embedded, not bolted-on) | Go-kernel side-effect hard rule; Phase 2 policy engine (future) |
| Universal context | Surface-neutral v2 event envelope |
| Durable persistence | `chain_id` + `prev_hash` + `seq` survives session/process boundaries |
| Platform abstraction | Per-surface adapters emit the same envelope |

Dogfooding quantifies the debt as it accumulates. Phase 2 pays it down.

## Scope

**In scope:**
- User-level Claude Code capture on this box (`chitinhq/chitin` + Readybench work + orphan sessions).
- Debt ledger artifact + entry format + three-lane triage model.
- Weekly review cadence and protocol.
- Trip-wires and soul-handoff mechanics.
- Graduation paths: Lane ① → issue, Lane ② → Phase 2 spec, Lane ③ → souls PR.
- Self-telemetry and install-verification.
- openclaw workstream (install + spike-question answers + minimum viable capture) — per user overrule of quorum default.
- GH Actions composite-action stub (chitin's own CI capturing itself).

**Out of scope:**
- Automated analyzer passes on accumulated traces (Approach 3 — deferred until ledger reveals patterns worth automating).
- Multi-machine aggregation (hetzner dead; Murphy on his own Mac without chitin).
- Cloud offering (end of strategic arc; dogfooding produces evidence that justifies it).
- Policy pack authoring (emerges from Lane ② saturation, not designed here).
- Redaction pipeline (deferred — no client secrets on this box; flag if topology changes).
- Copilot CLI install (documented extension path only; implementation later).

## Architecture sketch

```
┌─────────────────────────────────────────────────────────────┐
│  CAPTURE — always on, user-level hook                       │
│  Every Claude Code session on this box → chitin hook        │
│  emits v2 envelope+payload events                           │
│  (openclaw workstream parallel; same kernel, same envelope) │
└─────────────────────────────┬───────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  ACCUMULATION — per-repo                                    │
│  events.jsonl (append, hash-linked)                         │
│  events.db (SQLite index, lazy-rebuilds from JSONL)         │
│  orphan sessions → ~/.chitin/                               │
└─────────────────────────────┬───────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  REVIEW — weekly, da Vinci lens                             │
│  Skim chain-depth dist, hook failures, anomalies            │
│  Produce ledger entries: trace_ref → finding → lane         │
└─────────────────────────────┬───────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  TRIAGE — 3 lanes                                           │
│  ① FIX            → issue → Knuth hardens                   │
│  ② DETERMINISM    → Phase 2 policy candidate                │
│  ③ SOUL ROUTING   → empirical best_stages update            │
└─────────────────────────────┬───────────────────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  GRADUATION — when a lane saturates                         │
│  ① → hardening PR                                           │
│  ② → Phase 2 spec brainstorm (full design cycle)            │
│  ③ → souls/ PR updating canonical vs experimental           │
└─────────────────────────────────────────────────────────────┘
```

Cross-domain analog: `git` remote transports or `systemd` service backends
— one contract (the event envelope), many implementations (surfaces).

## Capture architecture

### 1. Install mechanism — per-surface pattern

| Surface | Install mechanism | Always-on? | Install command |
|---|---|---|---|
| Claude Code | Merge user-level `~/.claude/settings.json` | Yes, per-machine | `chitin install --surface claude-code` |
| Copilot CLI | Config / extension (not yet investigated) | Yes, per-machine | `chitin install --surface copilot-cli` (deferred) |
| openclaw | Plugin registration via openclaw's plugin system (pending spike answers) | Yes, per-machine | `chitin install --surface openclaw` |
| GH Actions | Reusable composite action in this repo | Per-workflow | `uses: chitinhq/chitin/actions/observe@v1` |

**Generic subcommand:** `chitin-kernel install --surface <name> [--global]`
with matching `uninstall`. First implementation in scope: `--surface
claude-code`. Stub exercise: `--surface gh-actions` composite action.

**Binary placement:** `~/.local/bin/chitin-kernel` via `make install-kernel`
symlink target. Rebuilds don't break `~/.claude/settings.json` because the
settings point at the stable path, not at the build output. Uninstall
removes the symlink.

### 2. Events landing

- **Repo-local default:** walk up from cwd looking for existing `.chitin/`;
  use it if found. Covers chitin repo, bench-devs-platform once cloned, any
  future chitin-aware repo.
- **Orphan fallback:** `~/.chitin/events.jsonl` when cwd has no enclosing
  `.chitin/` inside a workspace boundary.
- **Chain integrity:** same `chain_id` / `prev_hash` / `seq` contract as
  Phase 1.5. No special-casing for orphan events — they're just a different
  storage root.

### 3. Privacy / redaction

- No client secrets on this box. Readybench repo is user's own; chitin is
  open-source. Defer redaction.
- Flag: if/when a repo with real third-party secrets lands here, redesign
  redaction before the first session in that repo.
- Never-commit list for chitin's `.gitignore`: no additions. Chitin's own
  `.chitin/events.jsonl` stays committed (dogfood-eating-itself is the
  demo).
- Never-commit list for bench-devs-platform's `.gitignore`: `.chitin/`
  added on first session. Events capture locally, ledger entries flow to
  chitin's repo, zero artifacts in Readybench's git history.

### 4. openclaw workstream (parallel to Claude Code install)

**Scope per Jobs overrule (quorum default was tighten; user kept proposed
scope):**

1. **Install openclaw locally** on this RTX 3090 box. Smoke-verify.
2. **Answer the SPIKE's 4 open questions by observation:**
   - Does it expose a plugin/hook API, or must we wrap at process level?
   - What streams does it emit during a session?
   - How does it identify session boundaries?
   - Where's the tool-call decision/execution boundary, if any?
   Answers replace `libs/adapters/openclaw/SPIKE.md` with a real
   `README.md`.
3. **Implement minimum viable capture** — pick adapter strategy based on
   what observation reveals; ship at least `session_start` / `session_end`
   firing on real openclaw use; one inner event type only if investigation
   surfaces an obvious hook point.

**Structural mitigations (honoring the four dissenting souls without
re-fighting the vote):**

- **Socrates mitigation:** cost estimates carry explicit uncertainty. If
  investigation reveals capture implementation will be >5 days of elapsed
  effort, split into a follow-up spec rather than force through.
- **Knuth mitigation:** the capture implementation section is a
  placeholder filled in by observation — no implementation strategy is
  pre-committed. The one-sentence invariant for capture is written after
  the investigation, not before.
- **Sun Tzu mitigation:** sequence enforced — install + answer-the-4-questions
  ships first; capture implementation starts only after those complete.
- **Jokić mitigation:** openclaw is a separable workstream. If it stalls,
  the Claude Code + ledger + cadence slice ships independently.

## The governance-debt ledger

### Location

`chitin/docs/observations/governance-debt-ledger.md` — chitin repo only.
Readybench never sees it, even when entries reference traces from work
done inside bench-devs-platform.

### Entry shape

```markdown
### GDL-NNN — <one-line what the platform should have caught>

- **Observed:** YYYY-MM-DD, chain `<chain_id>`, seq `<n>`, hash `<this_hash[:12]>`
- **Surface / repo:** claude-code / chitin  |  claude-code / bench-devs-platform  |  openclaw / chitin  |  ...
- **Finding:** what happened, one paragraph.
- **Lane:** ① FIX | ② DETERMINISM | ③ SOUL ROUTING
- **Severity:** low / medium / high (impact if this recurs at scale)
- **Graduated:** <null> | issue #N | phase-2 candidate | souls PR #N
- **Soul active:** <soul_id> @ <soul_hash[:8]>
```

### Cross-repo trace refs

- Prefer stable refs: `chain_id:seq` or content-addressed `this_hash`.
- File-path refs (`bench-devs-platform/.chitin/events.jsonl#L42`) are
  fallback only — machine-local.
- Quoting trace content: paraphrase if content could identify internal
  Readybench logic. Most entries will be chitin-on-chitin; cross-repo
  entries are the minority.

### Invariants

- `GDL-NNN` IDs are stable; never renumber.
- Every entry has a `trace_ref` pointing to a real event. No speculative
  entries.
- Lane classification is one-way. To reclassify, create a new entry
  cross-linking the old; never mutate history.
- Entry quality rule: an entry is worth writing only if future-you, reading
  cold, would say "yes, a policy should have existed for this." Not "here's
  a thing I did that I could have done better."

## Review cadence & protocol

### Cadence

Weekly. Default Friday afternoon; adjustable. Duration target 20–30
minutes; if it bloats, the tooling needs work.

### Protocol per session

1. **Quick metrics skim** (3 min) — `chitin events tree` on last-N sessions:
   - Session count by surface
   - Hook-failure count (non-zero → Knuth Lane ① finding)
   - Chain-depth distribution
   - Orphaned chains (`session_start` with no `session_end`)
2. **Narrative replay** (10–15 min) — pick 2–3 recent sessions across
   available repos (chitin always available; bench-devs-platform once
   cloned here; orphan sessions from `~/.chitin/` are valid picks too).
   Run `chitin replay <session_id>`. Ask:
   - Did the trace tell the session's story accurately?
   - Were there moments the platform could have intervened usefully?
   - Did the active soul's choices correlate with outcome quality?
3. **Ledger writing** (5–10 min) — create `GDL-NNN` entries for findings.
   One paragraph max. Don't over-polish.

### Trigger events (interrupt cadence)

- Hook silently dropped an event → immediate Knuth session.
- Readybench session had a near-miss (almost committed a secret, ran
  something destructive) → immediate write-up.
- A lane crosses its saturation threshold → graduate that lane.

### Anti-patterns

- Don't skim ≥4 weeks in one sitting; catch up gradually.
- Don't write entries "just because it's Friday"; empty week = empty entry count.
- Don't filter boring sessions before review.

### Tooling (in scope)

- `chitin review --last 7d` — emits the week's skim metrics in one shot.
- Optional: `chitin ledger new <lane>` — creates a stub entry with
  auto-filled `GDL-NNN`, date, and prompts for the trace ref.
- `chitin ledger lint` — verifies `GDL-NNN` uniqueness, trace_ref
  resolution, graduated-marker integrity.

## Trip-wires & soul handoff

### Default driver

da Vinci (always-on until trip-wire; temporary handoff; return when slice
complete).

### Trip-wire matrix

| Trigger | Temporary lens | Duration | Return when |
|---|---|---|---|
| Boundary bug, Lane ① severity≥medium | Knuth | Until invariant restored + test/proof | PR merged |
| Lane ② ≥10 entries | Jobs → full brainstorm | Phase 2 spec session | Phase 2 spec committed |
| Lane ③ ≥10 entries, ≥3 souls | Measured soul + Socrates | One ledger sweep | Souls PR merged |
| openclaw capture >5 days | Jokić | One reset session | Decision made, spec amended |
| Hook silently drops event | Knuth | Root cause + fix + regression test | Dropped trace replayed OK |
| PR review starts | Socrates | Per PR | PR merged |

### Handoff mechanics

- **Explicit** — session text says "handing to Knuth for this boundary fix"
  so the trace itself records the handoff (Lane ③ evidence for future).
- **Scoped** — non-default soul owns one clear slice, not the session.
- **Reversible** — da Vinci returns when slice completes; don't drift.
- **Global default (`~/.claude/CLAUDE.md`) untouched** by temporary handoffs.
  Only re-quorum changes the default.

### Quorum reconvening

- **Scheduled:** at end of each major graduation (Phase 2 spec committed;
  openclaw capture shipped; souls library updated).
- **Triggered:** user can convene anytime ("ask the quorum").
- **Emergency:** two trip-wires fire in one session → mini-quorum for
  triage priority.

### Guardrails

- No soul-shopping (handoff to escape discomfort).
- No handoff ping-pong (>3 handoffs = mis-scoped work; replan).
- No silent default drift (multi-session non-default lens → call quorum).

## Outputs & graduation

### Lane ① — FIX → issues + hardening PRs

- **Trigger:** any entry `severity: medium` or higher.
- **Artifact:** GH issue in `chitinhq/chitin`, title `[GDL-NNN] <one-line>`.
- **Owner:** Knuth.
- **Closure:** issue closed → entry marked `graduated: issue #N → closed`
  with commit SHA.
- **Batch rule:** ≥5 stacked Lane ① entries without addressed = itself a
  Lane ② finding.

### Lane ② — DETERMINISM → Phase 2 policy + spec

- **Trigger:** ≥10 entries OR single high-severity load-bearing entry.
- **Artifact path:**
  1. Each entry annotates with proto-policy: "when X occurs, platform
     should Y."
  2. At saturation, convene brainstorm for
     `docs/superpowers/specs/YYYY-MM-DD-phase-2-governance-design.md`.
  3. Every rule in Phase 2 spec cites the `GDL-NNN` that motivated it.
- **Owner:** Jobs (spec arbitration) → full brainstorm cycle.
- **Closure:** Phase 2 spec committed → contributing entries marked
  `graduated: phase-2 candidate → folded into <path>`.

### Lane ③ — SOUL ROUTING → souls/ PR

- **Trigger:** ≥10 entries spanning ≥3 souls.
- **Artifact:** PR to `souls/` updating `best_stages` frontmatter based on
  observed outcomes. Possible promotion/demotion between canonical and
  experimental tiers.
- **Procedure:** aggregate by `soul_active`; compute rough outcome quality;
  compare observed vs self-reported `best_stages`; PR the diff with
  evidence.
- **Owner:** measured soul (self-review) + Socrates.
- **Closure:** PR merged → entries marked `graduated: souls PR #N → merged`.

### Retrospectives (cross-lane)

- Every major graduation → 1-page note at
  `docs/observations/retrospectives/YYYY-MM-DD-<event>.md`:
  - What ledger entries predicted vs what shipped
  - Which soul calls were right/wrong
  - Next arc's ledger-watching focus
- Retrospectives feed next quorum reconvening.

### Non-goals

- No SLA on graduation speed (trip-wires handle urgency).
- No cross-lane bundling (each lane graduates independently).
- No retroactive lane reclassification.
- No direct promotion from `GDL-NNN` to public artifacts (blog posts,
  talks, customer copy).

## Testing & validation

### Layer 1 — Install verification (one-shot)

`chitin install --surface claude-code` prints a verification checklist:
- Hook entry exists in `~/.claude/settings.json`.
- `chitin-kernel` binary at expected path, executable.
- Smoke session (`claude -p "hello"`) produces ≥1 event in expected
  events.jsonl.
- Chain linkage hashes validate on smoke session.

Failure → install aborts; no partial state left.

### Layer 2 — Self-telemetry (daily, passive)

`chitin health` CLI subcommand with pass/warn/fail per check:
- Events per day per surface.
- Hook-failure count (non-zero → Lane ① trip-wire).
- Schema-drift events (non-zero → immediate stop).

### Layer 3 — Ledger health (weekly, during review)

Inspected manually during Friday skim:
- Entries-per-week trending to zero with active work → ledger blind.
- Lane distribution extreme imbalance → protocol under-reading.
- Graduation rate stalled → triggers too strict or owner-lens missing.

### Layer 4 — Graduation proof (per graduation event)

- Every graduated artifact cites contributing `GDL-NNN`s.
- Phase 2 spec rules not cited by ≥1 entry must be flagged in
  retrospective as speculative.

### Layer 5 — Quarterly audit (soul-level)

- Convene quorum every 3 months.
- Read retrospectives; verify ledger predictions matched reality.
- Ask Addy's question: *is debt decreasing or increasing?* Compare Lane ②
  new-entry rate to Phase 2 graduation rate.
- Decide whether default lens still fits.

### Automated tests (CI)

- `install-global-hook` merges vs overwrites `~/.claude/settings.json`,
  handles missing file, handles malformed existing file.
- `uninstall-global-hook` removes only what it added; leaves unrelated
  hooks intact.
- Integration test: provision throwaway `$HOME`, run install → stub hook
  event → verify event lands with correct chain linkage.

### Manual checks (acceptable for this tool)

- `chitin ledger lint`:
  - `GDL-NNN` ID uniqueness.
  - `trace_ref` resolves (chain_id:seq exists in events.db).
  - `graduated:` markers with issue/PR numbers point to real GH objects.

### The anti-Hawthorne guard

- Default assumption: act normally; ledger is for future-you, not
  grading-you.
- Entry quality rule (repeat for emphasis): worth writing only if
  future-you would say "a policy should have existed for this."

## Exit criteria for this spec

1. All 7 design sections approved by user. ✓ (confirmed in brainstorming session)
2. This spec committed to `docs/superpowers/specs/`. (completed with this write)
3. User reviews committed spec. (pending)
4. Writing-plans skill invoked, producing implementation plan. (pending)

## Open questions (defer to implementation plan)

- Exact merge semantics for user-level `settings.json` when existing
  hooks conflict with chitin's — union, replace-with-warning, or error?
- Should `chitin health` run as a systemd timer/cron, or pure-manual?
  Lean manual for v1; revisit if self-telemetry rots.
- Where do retrospectives live if `docs/observations/` feels too chitin-
  centric? (Probably fine — chitin observes itself; retrospectives are
  part of that.)
- GH Actions composite action scope: chitin-only initially, or ship a
  public reusable version? Lean chitin-only; publicize after debugging.

## Traceability

- Addy Osmani, *The Agent Stack Bet* — governance debt framing,
  bolted-on vs embedded governance, four architectural bets.
- Phase 1.5 observability chain contract (`docs/superpowers/specs/2026-04-19-observability-chain-contract-design.md`) — the event envelope this design builds on.
- Souls library (`souls/canonical/`, `souls/experimental/`) — the `soul_id`
  + `soul_hash` provenance referenced by Lane ③.
- Quorum decision, 2026-04-19 — da Vinci default lens set for this arc.
- User overrule on openclaw scope, 2026-04-19 — Jobs dissent adopted over
  Sun Tzu / Socrates / Knuth / Jokić's tighten recommendation, with
  structural mitigations.
