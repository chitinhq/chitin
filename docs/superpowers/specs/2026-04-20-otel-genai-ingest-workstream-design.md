# OTEL GenAI Ingest Workstream — Meta-Spec

**Date:** 2026-04-20
**Supplements:** `docs/superpowers/specs/2026-04-19-dogfood-debt-ledger-design.md` (Phase F)
**Supersedes (as active workstream):** `docs/superpowers/specs/2026-04-20-openclaw-adapter-implementation-design.md` (retained as historical record of the v1a/v1b cost analysis that tripped the Socrates gate)
**Status:** Meta-spec. Names sub-projects, sequences them, scopes the first. Each sub-project SP-1+ gets its own brainstorm → spec → plan cycle.

## Preamble

Phase F of the dogfood-debt-ledger plan tripped the Socrates gate when
a landscape scan established that shipping a bespoke openclaw adapter
(v1a process-wrap or v1b session-store poll) would produce working
code that gets thrown away the moment chitin adopts OTEL GenAI as its
canonical ingest format for non-hook-API surfaces. The parent spec's
Phase F therefore closed at F4 without implementing F5. This meta-spec
is the follow-up cycle the addendum owed.

The decisions below were reached through a brainstorming session
recorded in the conversation dated 2026-04-20. The six framing choices
that shape this spec:

1. **Framing:** Meta-spec first — decompose the workstream into
   sub-projects, then brainstorm the first sub-project in detail on
   its own cycle. (Rather than one big spec or jumping to implementation.)
2. **Canonical format posture:** OTEL GenAI at the ingest boundary;
   hash-chained chitin envelope stays the canonical internal store.
   The translator is the load-bearing abstraction.
3. **Finish line (v1 scope):** first live ingest — one real openclaw
   OTEL span translated into one chained envelope event, persisted to
   `.chitin/events/`. Proves receiver + translator + chain end-to-end.
   Everything beyond is follow-up.
4. **Ingestion mode:** hybrid — file-intake for v1, push receiver
   (`chitin otel-serve`) named as a follow-up sub-project gated on real
   demand.
5. **Empirical gate:** verify-first — the first sub-project is an
   empirical spike that captures a real openclaw OTEL payload before
   any translator code is written. No designs against assumed semconv
   compliance.
6. **Claude Code adapter posture:** CC stays bespoke. The stdin-hook
   integration is a first-class API; OTEL ingest is for surfaces that
   *don't* have one. The asymmetry is deliberate, not a migration debt.

## One-sentence invariant (meta-level)

Every OTEL span that chitin ingests either (a) becomes exactly one
chitin envelope event with valid `prev_hash`/`this_hash` chain linkage
and `labels.source = "otel"`, or (b) lands in `.chitin/otel-quarantine/`
with a typed reason — never a partial write, never a silently dropped
span, never a half-chained event in the main store.

## Architecture

The ingest pipeline has four stations. Each sub-project fills in one
station. Stations are independently replaceable.

```
[OTEL producer]  →  [intake]  →  [translator]  →  [chain writer]  →  [.chitin/events]
 (openclaw's         (v1: file-      (gen_ai.*        (prev_hash/        (unchanged;
  diagnostics-        based; v2+      span → chitin    this_hash          existing JSONL
  otel plugin)        push receiver)  envelope event)  linkage over       store)
                                                       the translated
                                                       events)
```

### Meta-level invariants (binding on all sub-projects)

1. **Stations are independently replaceable.** The intake (file → push
   receiver) can change without the translator changing; the
   translator's output shape is the chitin envelope regardless of
   where the input came from; the chain writer only sees envelope
   events and does not know OTEL exists. Any sub-project replaces
   exactly one station.

2. **Hash-chain integrity is preserved through translation.** Every
   OTEL span that becomes a chitin event gets a `chain_id`, `seq`,
   `prev_hash`, and `this_hash` written by the chain writer —
   identical in form to events from the Claude Code stdin-hook path.
   An OTEL-sourced event is indistinguishable from a native-source
   event inside `.chitin/events/` except via `labels.source = "otel"`
   for provenance audit.

### What is outside the meta-architecture

Named here so the scope boundary is visible; each is decided inside
its owning sub-project, not here.

- Raw OTEL span retention policy (keep alongside `.chitin/events`, or
  discard after translation). → Decided inside SP-1.
- Exact `gen_ai.*` → envelope mapping table. → SP-1 for the one
  span-type; SP-2 for the rest.
- Authentication / authorization for the push endpoint. → SP-3.
- OTEL export back out to downstream consumers (Langfuse, Grafana,
  Arize, etc.). Separate workstream, not owed by this meta-spec.

## Sub-projects

Each sub-project below is specified at meta-level only: one-sentence
invariant, scope, v1 deliverable, cost estimate, deferral reason. The
detailed design for each is owed inside that sub-project's own
brainstorm → spec cycle.

### SP-0 — Empirical spike: what does openclaw actually emit?

**Invariant.** One real openclaw agent turn, run with
`diagnostics-otel` enabled and exporting to a local file, produces
exactly one captured OTLP/JSON payload committed to the repo under
`docs/observations/` with the full span set documented (attribute
keys, event shapes, resource attrs).

**Scope.** Enable the plugin via
`openclaw plugins enable diagnostics-otel`. Configure the OTLP
exporter to write to a local file (or whichever local-sink mode the
plugin supports — that determination is part of the spike itself).
Run one agent task. Capture the output verbatim. Read it. Write a
short `docs/observations/2026-04-??-openclaw-otel-capture.md` that
lists: (a) the actual attribute keys used, (b) whether `gen_ai.*`
semconv compliance holds or deviates, (c) which span-types are
emitted per turn.

**v1 deliverable.** Committed capture file + observation doc. No
chitin code changes.

**Cost.** 0.5–1 day.

**Why separate from SP-1.** If the capture shows openclaw does not
emit `gen_ai.*` attributes — or emits some other shape entirely —
SP-1's translator design has to change or the meta-spec's escape
hatch fires. Spending build days against the wrong assumed schema
is exactly the failure mode the Socrates gate just caught in Phase F.

### SP-1 — First live ingest

**Invariant.** One openclaw OTLP/JSON file produced under SP-0
conditions, when passed to `chitin ingest --from <file>` (or
equivalent), produces exactly one `session_start` event in
`.chitin/events/` with valid `prev_hash` / `this_hash` linkage and
`labels.source = "otel"`.

**Scope.**

1. File intake command (Go CLI + adapter code) that reads an
   OTLP/JSON file and parses it into span objects.
2. Minimal translator: the *one* OTEL span-type that corresponds to
   "session starts" in openclaw's actual capture, mapped to
   `SessionStartPayload`.
3. Chain writer integration — the translated event goes through the
   same chain-writing path as stdin-hook events, with its `chain_id`
   derived deterministically from the OTEL `trace_id`
   (`chain_id = "otel:" + trace_id`).
4. End-to-end test: the SP-0 fixture round-trips through the pipeline
   and emerges as a valid chitin event.

**v1 deliverable.** `chitin ingest` subcommand, minimal translator
for the single span-type, end-to-end test using SP-0's captured
payload as fixture, docs update.

**Cost.** 3–5 days. Re-estimated after SP-0 if the captured schema
differs materially from semconv.

**Explicitly out of scope.**

- Other span-types (→ SP-2).
- Push endpoint (→ SP-3).
- CC migration (not happening per framing choice #6).
- Exact raw-OTEL retention policy is decided *inside* SP-1 but is
  not a separate deliverable.

### SP-2 — Complete openclaw translator

**Invariant.** Every distinct span-type observed in SP-0's capture
(plus any additional types that surface in repeated captures during
SP-2) maps to a defined chitin event type, with the full mapping
table committed and tested against fixtures.

**Scope.** Extend SP-1's translator to cover every span-type openclaw
emits. Define new chitin event types *only if* existing ones
(`user_prompt`, `assistant_turn`, `compaction`, `session_end`,
`intended`, `executed`, `failed`) cannot hold the data honestly.
Fixture tests for each mapping.

**v1 deliverable.** Complete translator, mapping table documentation,
fixtures, passing tests.

**Cost estimate.** 5–7 days — a gut estimate from outside evidence;
re-estimated after SP-1 ships.

**Why deferred.** The mapping table is genuinely unknown until SP-0
runs and SP-1 proves the pipeline. Pre-committing to it is the same
over-scope trap Phase F fell into.

### SP-3 — Push receiver (OTLP/HTTP endpoint)

**Invariant.** A long-lived `chitin otel-serve` process listens on a
configurable local port, accepts standard OTLP/HTTP POSTs, and feeds
them into the same translator station SP-1 and SP-2 built — producing
chitin events indistinguishable from file-intake events.

**Scope.** Go HTTP server. Install-path ergonomics (how does the user
start/stop it, how does `chitin install` know about it). Port
conflict handling. No new translation logic — purely a different
intake station.

**Cost estimate.** 3–5 days.

**Why deferred.** A push receiver is only worth building if there is
a user who wants to run openclaw (or another surface) in push mode
and found file-intake insufficient. That evidence does not exist yet.
The meta-spec names it so the option is visible but does not commit
to building it.

### SP-4 — Cross-surface diff / Lane-② findings

**Invariant.** Given at least one Claude Code stdin-hook session and
one openclaw OTEL session captured for the same or analogous task,
the ledger tooling can surface a concrete finding that highlights a
drift, inconsistency, or governance gap between the two surfaces.

**Scope.** Not a new pipeline station — it is the ledger output that
unlocks *because* both surfaces now feed the same envelope. Likely
looks like: a report command, a finding template for cross-surface
drift, and one real finding committed to the governance-debt ledger
as proof-of-life.

**Cost estimate.** 1–3 days of tooling, but gated entirely on SP-2
being done and enough real dogfood data.

**Why deferred.** Needs SP-2's complete translator + real usage
across both surfaces. Pre-spec'ing it is premature.

### Sequencing and dependencies

```
SP-0  →  SP-1  →  SP-2  →  SP-4
                   ↓
                 SP-3 (independent, gated on demand)
```

SP-0 blocks SP-1. SP-1 blocks SP-2 (SP-1 proves the station pattern
SP-2 extends). SP-2 blocks SP-4. SP-3 is independent of the 0→1→2
chain and lights up whenever there is evidence a push endpoint is
needed.

## Data flow rules

Meta-level only. Sub-projects fill in the actual mapping tables;
this section fixes only the boundary rules.

### Between stations

- **Producer → intake.** OTLP/JSON. Standard OpenTelemetry protocol,
  no custom format. Medium is a file on disk for v1; HTTP POST for
  SP-3. Downstream stations are unaware of which medium was used.
- **Intake → translator.** A deserialized OTEL span object (or list
  thereof) as Go structs. Intake owns the JSON parse; translator
  owns the semantic interpretation.
- **Translator → chain writer.** An envelope event *without* `seq`,
  `prev_hash`, or `this_hash` filled in yet. The translator produces
  all the other envelope fields (`run_id`, `session_id`, `surface`,
  `chain_id`, `chain_type`, `ts`, `labels`, `payload`) and an
  `event_type`; the chain writer appends `seq` / `prev_hash` /
  `this_hash` based on existing chain state. This split matches the
  stdin-hook path — translator and hook-runner are peers in what
  they produce, and the chain writer is the single place chains get
  closed.
- **Chain writer → store.** Unchanged from today. Fully-formed
  envelope event appended to the JSONL event log.

### Fixed meta-level data rules (binding on all sub-projects)

1. **`chain_id` derivation.** Deterministic:
   `chain_id = "otel:" + trace_id`. Same `trace_id` observed twice
   (e.g., same file re-ingested) produces the same `chain_id` — the
   chain writer's idempotency handles the dedup, same as stdin-hook
   replay today. Tie-breaker for the same `chain_id` but divergent
   payload is SP-1 scope.
2. **`chain_type`.** `session` for every translated event in v1.
   `tool_call` and any sub-chain structure is reserved for SP-2 when
   `gen_ai.tool.*` spans are mapped.
3. **`surface`.** Taken from the OTEL resource attribute
   `service.name`. If openclaw emits `service.name = "openclaw"`,
   that is the surface. Falls back to `"unknown"` if absent;
   translator logs a warning. This is what makes the translator
   portable across future OTEL-emitting surfaces without hardcoding.
4. **`labels.source`.** Always `"otel"` on translated events.
   Provenance is auditable at query time without parsing payload.
5. **`ts`.** The OTEL span's `start_time_unix_nano`, converted to
   the envelope's RFC3339 string. Not the ingest time.
6. **`driver_identity` and `agent_fingerprint`.** From OTEL resource
   attributes where present, fall back to chitin's local identity
   where absent. Exact fallback rules are SP-1 scope.

## Error handling stance

Meta-level stance only; per-station failure taxonomy is sub-project
detail.

### Default posture

Fail loud, do not corrupt. A translated event either lands in
`.chitin/events/` with valid chain linkage, or it does not land at
all. Partial writes, half-chained events, and silently-dropped spans
are refused — they poison the thing chitin's governance thesis rests
on (the chain as proof-of-authenticity). If the translator cannot
produce a valid envelope, the span gets written to a quarantine
sidecar (`.chitin/otel-quarantine/`) with a reason line and the
ingest command exits non-zero. No event gets into the main store
without full chain linkage.

### Per-station stances

1. **Intake (OTLP/JSON parse).** Malformed JSON or an unparseable
   OTLP payload fails the ingest command with a pointer to the byte
   offset that broke the parse. No events written. User fixes the
   source or re-captures.
2. **Translator (span → envelope).** A span whose type has no
   mapping in the current translator is a *known-unknown* for the
   active sub-project: the translator logs the span's `name` and
   attribute keys, writes the raw span to quarantine, and continues
   processing other spans in the same payload. Not a fatal error.
   The quarantine file is how SP-2's mapping table grows — every
   quarantined span is a gap the next sub-project has to close. A
   span whose type *is* mapped but whose required attributes are
   missing (e.g., an `assistant_turn` equivalent with no usage data)
   is a fatal translation error for that span only — quarantined
   with a typed reason, other spans continue.
3. **Chain writer.** Reuses the existing writer's stance. A span
   that would produce a `chain_id` collision with existing chain
   state is deduplicated (idempotent replay). A hash mismatch on
   read-back during chain-state load is a hard error — the same
   hard error the stdin-hook path already surfaces today, handled
   by the same code.
4. **Store.** Unchanged from today. Disk-full, permission errors,
   etc. are kernel-level failures that crash the ingest command
   cleanly; `.chitin/events/` is append-only JSONL, so a crashed
   ingest leaves the store consistent (the last event either fully
   landed or did not).

### Meta-level escape hatch

SP-0's capture may reveal openclaw emits something that is not
usable OTEL GenAI — wrong semconv, non-OTLP format, or nothing
usable for mapping. In that case the meta-spec's v1 scope itself is
invalid. The response is **not** to build a translator against
whatever openclaw emits; it is to stop the plan, amend this spec
with an "openclaw as first consumer was the wrong bet" note, and
pick a different first consumer (possibly an OTEL-compliant surface
like LiteLLM or any agent that already emits `gen_ai.*` spans).
This is the same trip-wire shape as the Socrates gate — the
meta-spec names the escape rather than pretending it cannot happen.

## Testing stance

Meta-level strategy only; test suites per sub-project are filled in
by each SP's plan.

### Core rule: fixture-driven translator tests, dogfood-driven pipeline tests

Every sub-project from SP-1 onward runs against captured OTEL
payloads, not synthesized ones. SP-0's deliverable is the first
fixture; SP-2 extends the fixture set by re-capturing whenever new
span-types show up. Synthetic OTEL payloads (hand-written JSON in
test code) are banned for this workstream. The Socrates lesson
applies directly: the schema-we-assume-openclaw-emits and the
schema-openclaw-actually-emits are different artifacts, and tests
that run against the former prove nothing about the latter. A test
runs against a real captured file or it does not count as coverage
for translator correctness.

### Per-sub-project testing shape

1. **SP-0.** Not a code sub-project. Its test is the act of running
   openclaw with the plugin enabled, inspecting the output, and
   confirming the capture file is non-empty and parses as valid
   OTLP/JSON. That is the gate.
2. **SP-1.** (a) Unit tests for the translator's single mapping —
   input is a parsed span from SP-0's fixture, output is a specific
   `SessionStartPayload` with expected fields. Boundary cases:
   missing resource attrs, missing required span attrs, malformed
   `start_time_unix_nano`. (b) Integration test for the full
   pipeline: `chitin ingest --from <SP-0 fixture>` produces one
   event in a fresh `.chitin/events/` store, event validates
   against the Zod envelope schema, hash-chain readback succeeds.
   (c) Dogfood test (live — not in CI): re-run openclaw with the
   plugin, capture a fresh file, ingest it, verify a new event
   shows up in `chitin events list`. Proof-of-life for the full
   station chain.
3. **SP-2.** Extends (a) with a fixture per span-type; extends (b)
   with a round-trip test that ingests a fixture containing *all*
   span-types from a full agent turn and verifies every span lands
   in exactly one event or exactly one quarantine entry — no span
   is silently dropped. (c) extends the dogfood test to cover a
   full turn.
4. **SP-3.** Intake-layer tests only — POST an OTLP payload to the
   running server, verify it emerges in `.chitin/events/`
   identically to the file-intake path for the same payload.
   Explicitly no re-test of translator logic; that is shared across
   stations.
5. **SP-4.** Integration test against a hand-composed ledger
   scenario (two envelope event streams from two surfaces, one
   known drift). Not the primary gate — the primary gate for SP-4
   is a real finding committed to
   `docs/observations/governance-debt-ledger.md`.

### Cross-cutting

- Every sub-project's CI includes a replay test: re-ingest the same
  SP-0 fixture twice, verify exactly one event lands (chain writer's
  idempotency is the safety net under replay).
- No mocked OTEL parser, no mocked chain writer. The stdin-hook path
  already exercises the chain writer against real disk; OTEL ingest
  shares that same writer, so the integration tests sit on top of
  real kernel code.

## Open risks

1. **openclaw may not emit `gen_ai.*` semconv.** The addendum
   verified openclaw ships OTLP export infrastructure but not
   semconv conformance. SP-0 is the gate that converts this from a
   risk to either a confirmation or an escape-hatch trigger.
2. **`chain_id = "otel:" + trace_id` may conflict with future
   surfaces.** If two future OTEL-emitting surfaces produce
   colliding `trace_id` values (e.g., both default to all-zero
   trace IDs during tests), their events collide in the same
   chitin chain. Mitigation: SP-1 adds a `surface` disambiguator
   *if* the SP-0 capture shows this as a real risk — otherwise the
   simple form stays. Do not design around a hypothetical surface
   that may never emit.
3. **File-intake for v1 is non-canonical OTEL.** Standard OTEL
   deployments assume push. If SP-0 reveals openclaw's
   `diagnostics-otel` plugin does not support file export at all,
   SP-1's scope shifts — either we build a minimal stdout-tail
   shim, or SP-3 (push receiver) becomes the v1 intake instead of a
   follow-up. The framing does not fail; only the intake station's
   implementation changes.
4. **CC asymmetry may become a positioning liability later.** If
   the market hardens around "every agent emits OTEL," chitin's
   stdin-hook integration for CC may look like debt even though
   it is a correct engineering choice today. If that happens, the
   follow-up is an *export* workstream (chitin emits OTEL out to
   downstream tools) — not a migration. Named here so the decision
   stays visible in future reviews.

## Self-review

### Placeholder scan

- No `TBD` or `TODO` literals in the spec.
- `docs/observations/2026-04-??-openclaw-otel-capture.md` contains
  a date placeholder — intentional; the exact date is set when SP-0
  runs, because SP-0 is not scheduled yet.
- SP-0 explicitly produces the first fixture, and every downstream
  SP is allowed to reference "SP-0's fixture" even though the
  fixture does not exist at spec-write time. This is the correct
  pattern per the addendum's "no code written until the addendum
  exists" rule — here, "no translator code written until SP-0's
  capture exists."

### Internal consistency

- The four-station architecture in §Architecture is used
  consistently in every sub-project scope (SP-1 fills stations
  intake + translator + chain-writer-integration; SP-2 extends
  translator; SP-3 replaces intake; SP-4 consumes the output of
  the store).
- `chain_id = "otel:" + trace_id` is stated once in §Data flow
  rules and referenced (not redefined) in SP-1's scope.
- The fail-loud + quarantine posture is stated once in §Error
  handling and not redefined per sub-project — each sub-project
  just inherits.

### Scope check

Each sub-project is sized to produce exactly one brainstorm → spec
→ plan cycle on its own (none of SP-0 through SP-4 bundles
multiple independent stations). The meta-spec itself does not
implement anything — it hands off to SP-0 as the next action.

### Ambiguity check

- "openclaw's actual capture" (used in SP-1, SP-2): unambiguously
  refers to the file SP-0 produces and commits under
  `docs/observations/`.
- "first span-type that corresponds to session starts" (SP-1
  scope): deliberately under-specified here — SP-1's spec names
  the exact OTEL span `name` / `kind` after SP-0 reveals what
  openclaw actually emits.
- "known drift" (SP-4 scope): genuinely unknown until SP-2 has
  real data. SP-4's spec names the specific drift type when it is
  written.

## Execution handoff

**Next action:** brainstorm SP-0 (empirical spike). SP-0 is small
enough that it may not need a full brainstorm → spec → plan cycle —
a short task description and an execution session is likely
sufficient. The meta-spec does not pre-commit to that; the SP-0
owner decides.

After SP-0 ships its capture + observation doc, run a new
brainstorming session for SP-1 against the real captured payload.
SP-1's spec is written against that evidence, not against this
meta-spec's assumptions.
