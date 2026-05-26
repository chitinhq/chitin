---
spec_id: 122
title: Report freshness canary — detect stale dashboards before operators read them
status: Draft
owner: chitinhq
created: 2026-05-26
depends_on:
  - 070
  - 114
related:
  - 064
  - 078
  - 118
  - 121
---

# Spec 122 — Report freshness canary

## Why

On 2026-05-26 the Ares product-agent cron walked Chitin's
telemetry surfaces and surfaced a concrete failure of the
reports hub: the operator's at-a-glance dashboards are stale
relative to live state, but nothing tells the operator they're
stale.

**The evidence** (captured by Ares at 11:08 EDT, in
`Chitin/Product Agent/2026-05-26 Telemetry Product Scan.md`):

  - `chain-summary-latest.html` is titled `2026-05-20` and was
    last modified `2026-05-20 20:11 EDT`. Live `chitin-kernel
    chain stats --window-hours 24` on 2026-05-26 reports
    **4,661** decisions vs the report's **3,346** — divergence
    is real, not cosmetic.
  - `board-audit-latest.html` was generated `2026-05-21 09:01
    EDT` and shows `blocked=6, done=250, in_progress=3,
    ready=20, todo=14, triage=7`. The live chitin board DB
    (`~/.hermes/kanban/boards/chitin/kanban.db`) reports
    `archived=2, done=8, ready=4, triage=8`. The dashboard is
    not just stale — it doesn't even match the right board DB
    (split-source ambiguity).
  - `industry-scan-latest.html` is titled `2026-05-20`, last
    modified `2026-05-20 09:04 EDT`. Six days stale, no badge,
    no warning.

**The cost.** Stale-but-plausible is worse than missing. A
missing dashboard prompts the operator to investigate; a stale
one that renders normal numbers prompts decisions on numbers
that don't match reality. Yesterday's operator-touch decisions
on board counts, denial rates, and recent activity were partly
made against state that was 5-6 days behind.

**Why this spec, not "fix the report generators":** the report
generators live across multiple repos (some chitin, some
hermes, some local-ai-lab). Telling each generator to
self-renew is a coordination problem and doesn't help when one
of them silently breaks. A chitin-owned canary that watches
the resulting HTML files is the load-bearing detector
regardless of which generator failed. The producers can
*optionally* opt into a metadata contract that makes the
detector smarter, but the canary works on `mtime` alone in the
common case — no producer change required.

**Composability:**

  - **Spec 114** (operator escalation surface) — stale-report
    escalations route through the same Discord notify surface
    as other operator-decision-needed signals. The reason
    taxonomy gets a new closed value `stale_report`.
  - **Spec 064** (telemetry-spec-feedback) — the canary is
    itself a telemetry consumer and a chain emitter; it
    composes with the analysis lib's window-loader pattern.
  - **Spec 078** (self-improvement loop) — when the canary
    fires on the same path repeatedly across N days, that's a
    SpecProposal candidate ("the X generator is broken; fix
    it" — but the canary itself doesn't propose, just emits;
    spec 078's analyzer picks the pattern up).
  - **Spec 118** (factory dispatch failed reason taxonomy) —
    parallel pattern; both define a closed reason taxonomy
    that operator-facing consumers read.

## User stories

### US1 (P1) — Canary detects stale reports and emits a chain event

> As the autonomous loop, a deterministic activity runs on a
> schedule (initial: every 6 hours), walks a configured list
> of report asset paths, and for each one whose effective
> `generated_at` is older than its `freshness_sla_hours`,
> emits a `stale_report_detected` chain event with a
> structured payload identifying the path, the observed age,
> and the SLA it breached. Reports under their SLA produce
> NO per-path event — the canary is silent on green at the
> per-path level. (A single per-run `report_fresh` summary
> event is emitted by FR-004 to prove the canary ran; it is
> NOT a per-path signal.)

**Independent test:** Run the canary against a fixture
directory containing two reports — one 1 hour old (SLA 24h),
one 100 hours old (SLA 24h). Assert exactly ONE
`stale_report_detected` event fires, payload identifies the
100-hour-old path with `age_hours: 100, sla_hours: 24`. No
event fires for the fresh report.

### US2 (P1) — Canary works on `mtime` alone — no producer changes required

> As the operator deploying the canary today against the
> existing reports hub, the canary uses file `mtime` as the
> effective `generated_at` when the report doesn't embed
> structured metadata. Today's three stale reports
> (`chain-summary-latest.html`, `board-audit-latest.html`,
> `industry-scan-latest.html`) MUST be detectable by the
> initial canary deployment without modifying their producers.

**Independent test:** Point the canary at
`/home/red/labs/local-ai-lab/wiki/assets/` with a 24h SLA.
Assert all three reports are flagged stale based on `mtime`
alone, no embedded-metadata code path was reached.

### US3 (P2) — Producers can opt into embedded metadata for richer signal

> As a report producer (any author of a `*-latest.html`), I
> can embed a machine-readable metadata block in my HTML —
> `<!-- chitin-report-meta: {...} -->` — declaring
> `generated_at`, `source_window_start`, `source_window_end`,
> `source_commands`, and `freshness_sla_hours`. The canary
> prefers the embedded `generated_at` over `mtime` when
> present.

**Independent test:** A fixture HTML file embeds a valid
metadata block declaring `generated_at` 30 minutes in the
past but the file's `mtime` is 1 week old. Assert the canary
treats it as fresh (uses the embedded value, not `mtime`).
A second fixture embeds an unparseable block; assert the
canary falls back to `mtime` and the event payload carries
`age_source: "mtime"` (the fallback signal — there is no
separate "fallback reason" field, the source enum IS the
signal per FR-003).

### US4 (P2) — Escalation hits Discord through spec 114

> As the operator getting interrupted, a `stale_report_detected`
> event flows through spec 114's escalation surface to Discord,
> with a message naming the report, the age in hours, and a
> direct URL to the report file. Repeat escalations for the same
> path are rate-limited (initial: at most once per 24h per path)
> so a chronically-broken generator doesn't spam.

**Independent test:** Fire 5 `stale_report_detected` events for
the same path within 1 hour. Assert exactly ONE Discord
notification is posted; the other 4 produce
`stale_report_suppressed` events whose `suppressed_count` field
(see data model) reflects the running per-path suppression
count within the active cooldown window (1, 2, 3, 4 on the
four suppressed events).

### US5 (P2) — Operator can probe manually via CLI

> As the operator wanting to audit the reports hub on demand,
> `chitin-orchestrator reports check` walks the same
> configured paths the canary uses and prints a table of
> path / age / SLA / status (fresh|stale|missing) to stdout.
> Exit code 0 on all-fresh, 2 on any-stale, 3 on any-missing
> — composes with shell scripts.

**Independent test:** Run `chitin-orchestrator reports check`
against the fixture from US1. Assert stdout contains a table
with two rows, exit code is 2 (stale detected), stderr is
empty.

### US6 (P3) — Board audit augmentation (recorded for producers)

> As a board-audit report producer, the report SHOULD include
> the board DB path and board slug used to generate the audit,
> so a future canary alert ("board-audit-latest.html is stale
> AND points at the wrong board DB") is diagnosable from the
> report itself. This spec does NOT modify the producer; it
> declares the contract so the producer's owner can opt in.

**Independent test:** None for this spec — the test belongs
to whichever PR implements the producer change. The spec
records the contract: `<!-- chitin-report-meta:
{board_db_path: "...", board_slug: "...", ...} -->`.

## Functional requirements

- **FR-001** A deterministic activity `CheckReportFreshness`
  MUST be added at `go/orchestrator/activities/report_freshness.go`.
  Input: `{paths_config_path string}`. Output:
  `{checked int, stale []StaleReport, missing []string}`.
  The activity contains no LLM call and no network I/O —
  pure filesystem + time math.

- **FR-002** The watched-paths config lives at
  `~/.chitin/report-freshness.yaml` (override via
  `--config` flag and `$CHITIN_REPORT_FRESHNESS_CONFIG`).
  Schema:
  ```yaml
  paths:                              # required
    - path: string                    # absolute file path
      sla_hours: int                  # per-path freshness budget
  cadence_minutes: int                # optional; default 360 (6h) per FR-006
  escalation_cooldown_hours: int      # optional; default 24 per FR-007
  ```
  The initial bundled config covers the three paths Ares
  found stale on 2026-05-26 (each as an absolute path under
  `/home/red/labs/local-ai-lab/wiki/assets/`):
  `chain-summary-latest.html`, `board-audit-latest.html`,
  `industry-scan-latest.html`, each with a default
  `sla_hours: 24`.

- **FR-003** The canary MUST resolve a report's effective
  `generated_at` in this order:
  1. Embedded metadata block (US3, FR-005)
  2. File `mtime` (US2)
  3. If the file doesn't exist: treat as `missing`, NOT stale
  Each fallback step records the source in the event payload
  (`age_source: "embedded" | "mtime"`).

- **FR-004** The canary MUST emit one chain event per
  detected staleness. Closed event taxonomy for this spec:
  `stale_report_detected` (US1), `report_fresh` (debug-only,
  emitted at most once per canary run summarising the
  fresh-count to prove the canary ran), `report_missing`
  (US5 missing path), `stale_report_escalated` (US4, after
  the rate-limit check passes), `stale_report_suppressed`
  (US4, when rate-limited). Event payloads MUST conform to
  the data-model section below.

- **FR-005** The embedded-metadata regex MUST match the
  literal pattern `<!--\s*chitin-report-meta:\s*({[^}]*})\s*-->`
  case-insensitive across the first 4 KiB of the file (so
  the canary doesn't read 10 MiB of HTML to find the block).
  The captured JSON MUST parse via `encoding/json`; parse
  failure falls back per FR-003.

- **FR-006** The canary MUST be registered as a chitin
  scheduled job per spec 097's schedule-registration pattern
  (`schedules.EnsureSchedules`). Initial cadence: every 6
  hours. Operator can override via the same config (FR-002):
  `cadence_minutes: int, default 360`.

- **FR-007** A `stale_report_detected` event whose path was
  the subject of a `stale_report_escalated` event within the
  last `escalation_cooldown_hours` (default 24) MUST emit a
  `stale_report_suppressed` event INSTEAD of routing to
  escalation. The cooldown is queried from the chain
  (deterministic lookup by path).

- **FR-008** Non-suppressed `stale_report_detected` events
  MUST route through the existing `DiscordNotify` activity
  (introduced by spec 080, reused by spec 114 FR-009 for the
  daily digest — see `go/orchestrator/activities/notify.go`).
  The chain reason kind for these escalations is `stale_report`,
  a new closed-taxonomy value added by this spec to spec 114's
  enum (see FR-008 closed reason taxonomy in
  `go/orchestrator/internal/queue/reason.go`). The Discord
  message body MUST include: report path, age in hours, SLA,
  age source, and a `file://` URL.

- **FR-009** A CLI subcommand `chitin-orchestrator reports check`
  is introduced by this spec (US5). Flags: `--config <path>`
  override. Output: a fixed-column table to stdout. Exit
  codes: 0 all-fresh, 2 any-stale, 3 any-missing. The
  subcommand reuses the FR-001 activity's pure-function
  core; both surfaces share one code path.

- **FR-010** A CLI subcommand `chitin-orchestrator reports list`
  is introduced by this spec (US5 sibling). Output: the
  configured watched paths and their SLAs (read-only echo
  of the config). Exit code: 0 always; 1 only on config
  parse error.

- **FR-011** The activity MUST be idempotent and side-effect
  free apart from chain emit — running it twice in
  succession over the same input produces the same emit
  pattern (modulo the per-path rate-limit, which is
  deterministic given the chain history).

- **FR-012** The canary's existence MUST itself be observable:
  the scheduled-job registration (FR-006) creates a Temporal
  schedule whose presence is verifiable via
  `chitin-orchestrator schedules list` (existing subcommand).
  Operators MUST be able to see "the canary is running" without
  reading the chain.

## Success criteria

- **SC-001** Within 24 hours of deployment, the three
  paths Ares flagged on 2026-05-26 each produce exactly
  ONE `stale_report_detected` event and ONE Discord
  notification (deduplicated per FR-007). Measured by
  chain query + Discord channel inspection.

- **SC-002** Once a stale report's generator is repaired
  and the file is refreshed (mtime now under SLA), the
  canary STOPS emitting for that path within one
  cadence cycle (6 hours). Measured by absence of new
  `stale_report_detected` events for the repaired path
  after refresh.

- **SC-003** A 6-month uninterrupted canary run produces
  no false-positive escalations (no Discord notify for a
  report that was actually fresh). Measured by chain
  audit: every `stale_report_escalated` event correlates
  to a report whose `mtime` or embedded `generated_at`
  was indeed older than SLA at emit time.

- **SC-004** Operator-touch-time on "is this dashboard
  stale?" drops to zero on the happy path. Measured by
  the operator reporting (qualitative) and by absence of
  manual `stat -c %y *.html` operations in the operator's
  shell history.

- **SC-005** When a report generator dies silently for 30+
  days, the canary's per-path event stream demonstrates
  the pattern (≥ 30 escalation-suppressed events with the
  same path), making it a candidate for spec 078's
  self-improvement loop to surface as a SpecProposal.
  Measured at next 078 cycle after the canary ships.

## Scope

In:
  - `activities/report_freshness.go` — pure-function +
    activity wrapper per FR-001
  - `cmd/chitin-orchestrator/reports.go` — new
    `reports check` + `reports list` subcommands per
    FR-009 + FR-010
  - `schedules/report_freshness_schedule.go` — schedule
    registration per FR-006
  - Bundled config `internal/reportfreshness/default-config.yaml`
    + the deployed copy at `~/.chitin/report-freshness.yaml`
    written by an installer (or by the spec's runbook)
  - Chain emit sites for the 5 event types per FR-004
  - Spec 114 reason-taxonomy extension: add `stale_report`
    to the closed enum
  - Tests + documentation

Out:
  - Modifying report generators to embed the metadata block
    (US3 is consumer-side; producers opt in via a separate
    PR in whichever repo owns the generator)
  - HTML-level stale-badge rendering (the reports index
    page is in `labs/local-ai-lab`, out of chitin's repo
    boundary — that change happens there, optionally,
    informed by chain events the canary emits)
  - Cross-repo orchestration of producer updates
  - Backfill / historical replay (canary fires forward
    from deployment time only)
  - Encryption / signed metadata (the trust boundary on
    `~/.chitin/report-freshness.yaml` is the operator's
    home directory; same posture as the rest of `~/.chitin/`)

## Data model

Chain event payloads (closed schema for this spec):

```
stale_report_detected: {
  path: string,             // absolute file path
  generated_at: string,     // RFC 3339, the effective value
  age_hours: float,
  sla_hours: int,
  age_source: "embedded" | "mtime"
}

report_fresh: {
  checked_count: int,
  fresh_count: int,
  stale_count: int,
  missing_count: int,
  cadence: "scheduled" | "manual",
  clock_skew: bool          // true iff any per-path embedded
                            // generated_at was in the future
                            // during this run; see edge case
}

report_missing: {
  path: string,
  sla_hours: int           // for context — what we expected to find
}

stale_report_escalated: {
  path: string,
  age_hours: float,
  sla_hours: int,
  notify_message_id: string  // Discord message id, if returned
}

stale_report_suppressed: {
  path: string,
  age_hours: float,
  sla_hours: int,
  cooldown_remaining_hours: float,
  prior_escalation_at: string,
  suppressed_count: int     // running count of suppressions
                            // for this path within the active
                            // cooldown window (1-indexed, per US4)
}
```

## Edge cases

  - **Config file missing.** First-run case: bundle a
    default config (FR-002). If both the configured path
    and the bundled default are missing, the canary logs
    the absence to stderr and emits `report_fresh`
    with `checked_count: 0`. Doesn't fail the schedule.
  - **Path exists but is a directory.** Treat as
    `missing` per FR-003 step 3. A future producer
    might emit a directory of reports; that's a separate
    spec.
  - **Embedded metadata's `generated_at` is in the
    future.** Treat as `fresh` (age is negative).
    Probably a clock skew on the producer; emit a single
    chain note (`report_fresh` with a `clock_skew: true`
    flag in payload) and continue. Don't escalate.
  - **Embedded metadata's `freshness_sla_hours` overrides
    the config's per-path SLA.** Producer wins: a
    producer that knows it generates hourly should
    declare `freshness_sla_hours: 2`, and the canary
    honours that even if the config says 24. The case
    where producer-declared SLA is *longer* than the
    config's also honors the producer — the producer
    knows its own freshness budget better than the
    config.
  - **Report file is being written when the canary
    reads it (atomic-rename race).** The canary's `mtime`
    read happens BEFORE its content read — if the file
    disappears between the two, treat as `missing`.
  - **Chain query for cooldown returns stale data.**
    The chain is eventually consistent on the read
    side; a duplicate escalation is preferable to a
    missed one. The cooldown check is best-effort;
    when in doubt, escalate. Operator can tune
    `escalation_cooldown_hours` upward if duplicate
    escalations become annoying.
  - **The canary itself misses a tick** (orchestrator
    down, schedule paused). The next tick catches up
    — no backfill, no event for the skipped window.
    The schedule's existence is verifiable per FR-012,
    so a missing canary is detectable by inspection.
  - **Embedded metadata has both a `generated_at` AND
    the file is recent enough that `mtime` would say
    `fresh` but `generated_at` says `stale`.** This
    happens when a producer touches the file without
    regenerating it. Per FR-003 the embedded value
    wins — escalates as stale. This is the right
    answer; the file is lying about its freshness.

## Composability

  - **Spec 070** (work-unit primitives) — the canary is a
    deterministic activity, registered the same way other
    deterministic activities are. No work-unit shape
    change.
  - **Spec 097** (schedule registration) — the canary's
    schedule registration follows the existing pattern;
    `schedules.EnsureSchedules` gains one new entry.
  - **Spec 114** (operator escalation surface) — escalation
    flows through the existing Notify path; reason
    taxonomy gains `stale_report` (one new value).
  - **Spec 064** (telemetry-spec-feedback) — the canary IS
    a telemetry-derived signal; its events feed the same
    chain that 064 reads.
  - **Spec 078** (self-improvement loop) — chronic stale
    reports become a SpecProposal candidate (SC-005); 078
    detects the pattern without 122 doing anything special.
  - **Spec 118** (failure-reason taxonomy) — parallel
    pattern. The closed-taxonomy approach to reasons is
    the same.
  - **Spec 121** (blob store) — orthogonal; canary outputs
    are tiny chain events, never large enough to need
    externalization.
  - **Future producer opt-in** — when a producer (chitin
    or otherwise) adds the embedded metadata block, the
    canary's signal sharpens automatically. No code
    coordination required — the producer publishes the
    block, the canary picks it up on the next tick.
