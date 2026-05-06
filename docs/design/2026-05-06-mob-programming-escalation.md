# Mob-programming escalation: T0 worker + T1-T4 recursive consultants

Status: design extension. Updates and partially supersedes
`2026-05-06-kernel-gate-escalation.md` (the original peer-spawn design,
which assumed one-level-only escalation with `CHITIN_NO_ESCALATE=1`
recursive guard). This doc extends that to bounded recursive
escalation with tier-graded consultants.

Date: 2026-05-06 (later than the original)

## What changes

> **Old (step 4 shipped):** worker tool call → gate fires → spawn ONE
> peer (with `CHITIN_NO_ESCALATE=1`) → peer responds → done.
>
> **New (this doc):** worker tool call → gate fires → spawn T1
> consultant → if T1 ALSO can't help, T1's gate spawns T2 consultant →
> T2 → T3 → T4. Bounded by `max_depth=4`. Worker stays T0 throughout.

It's mob programming: junior dev (T0) at the keyboard. When stuck, ask
the senior next to them (T1). When THAT senior is stuck, ask the
principal engineer two desks over (T2). Each escalation is bounded;
all of it gets recorded; the original junior keeps the keyboard and
delivers the PR.

## The core invariant (extends the original)

> Kanban chooses work.
> **T0 is always the worker.** Always.
> Tiers T1-T4 are CONSULTANTS — the kernel routes to them when T0
> needs help, and recursively up the chain when each consultant also
> needs help. Bounded depth.
> Single PR per workflow. Replayable.

## Why this shape

Operator's framing (2026-05-06): "if we always start at tier 0, often
we will need to escalate to actually get work done. The goal is to
make tier 0 better over time with better determinism so that we can
slowly shift left, save tokens on more expensive models. We track
everything through chitin, are able to replay things to escalated
agents when needed."

Plus today's data:

| Tier | Dispatches today | Commits | Success rate |
|---|---|---|---|
| T1 | 4 | 0 | 0% |
| T2 | 19 | 7 | **37%** (the workhorse) |
| T3 | 14 | 0 | 0% (glm-5.1:cloud expensive failures) |
| T4 | 14 | 1 | 7% |

T2 is the only tier shipping. T1/T3/T4 are wasted as starting tiers.
But as CONSULTANTS to a T0 worker, T2 becomes the primary helper
called from inside T0's gate, with T3/T4 reserved for real
architectural escalation. That's what shift-left looks like in
practice.

## The recursive escalation flow

```
T0 worker tool call
  ↓ gate intercepts
  ↓ heuristic fires (floundering | blast_radius | drift)
  ↓ advisor: takeover + escalate=true
  ↓
spawn T1 consultant in fresh worktree, env: CHITIN_ESCALATION_DEPTH=1
  ↓ T1 makes its own tool calls
  ↓ gate intercepts T1's tool calls (CHITIN_ESCALATION_DEPTH=1 in env)
  ↓ if T1's heuristic fires AND depth<max_depth:
      ↓ spawn T2 consultant, env: CHITIN_ESCALATION_DEPTH=2
      ↓ ...recursive, up to depth=max_depth (4)
  ↓ T1 returns its result (which may include T2's contribution)
  ↓
T0 sees T1's response stitched into the deny-reason (per step 4)
T0 continues; may try again with the guidance, may pivot, may abandon
```

## The 5 open questions, resolved

These were the questions in my earlier message; resolutions baked in here:

### 1. Cost ceiling per worker session

**Per-workflow cap, not per-call.** Operator declares in chitin-routes.yaml:

```yaml
escalation:
  max_depth: 4                  # how deep the chain can go (T0 → T4)
  max_total_per_workflow: 10    # how many escalation hops total per worker session
  max_to_tier_per_workflow:
    T2: 8
    T3: 3
    T4: 1                       # T4 is last-resort architect; usually 0 or 1 per session
```

When the cap is hit, gate falls back to current behavior (deny + advisor nudge,
no peer spawn). Telemetry records the cap hit so operator sees the cliff.

### 2. Latency compound

**Per-tier shorter timeouts + concurrent execution where safe.**

```yaml
spawn_timeouts_per_tier:
  T1: 30   # quick consult
  T2: 60   # standard
  T3: 120  # heavy reasoning
  T4: 180  # architect — bounded
```

Worst-case full chain: 30+60+120+180 = 390s. Worker tool call already
times out at the worker's own wall budget; this stays inside it.

(Concurrent execution where the consultant doesn't depend on its
sub-consultant's response synchronously — deferred. Not in v1.)

### 3. Recursive context shape — the "replay"

Each consultant gets a structured prompt with the FULL upstream chain.
The schema (operator-facing):

```yaml
# T2 consultant's prompt context (when called by T1):
escalation_chain:
  - depth: 0
    actor: T0 worker (hermes/glm-flash)
    tool_call: {tool: Bash, input: {...}}
    chain_tail: [...recent events...]
    heuristic_fired: floundering (loop_count=3)
  - depth: 1
    actor: T1 consultant (copilot/gpt-4o-mini)
    response: "tried X, got result Y, doesn't seem to work because..."
    heuristic_fired: drift (advisor.verdict=takeover)
  - depth: 2 (you are here)
    actor: T2 consultant (copilot/claude-haiku-4-5)
    your_job: "review the chain above; provide concrete guidance OR
              indicate you also need to escalate"
```

Stored in chain events as `escalation_chain[]` so it's replayable
post-hoc. Operator can re-feed any chain to any model later for
audit / training data.

### 4. Replay mechanism

**Every escalation chain is captured automatically; operator-triggered
replay is a separate `chitin replay <escalation_id>` command.**

- Auto-capture: chain events of type `escalation_step` with the full
  context (tool call + heuristic + consultant response) per depth.
- Replay: `chitin replay esc-abc123 --to-tier T4` re-invokes T4 with
  the same upstream chain but skips the T1-T3 hops. Useful for
  comparing consultants OR for building eval datasets.

### 5. What signal triggers depth > 0 escalation?

**Same heuristics, different thresholds + a new "consultant-stuck" signal.**

**Strictly sequential** — every escalation goes UP exactly one tier.
No skipping (operator decision 2026-05-06: "use all models available
and build tooling to help them get better as we shift left").

At depth 0 (T0 worker), any of these → escalate to T1:
- Floundering (looping tool call)
- Blast radius (>25 files) — even though it's a "big" signal, the
  policy is "ask the next dev over first; let them decide if it
  warrants pulling in the principal"
- Drift (advisor.verdict=takeover)

At depth 1+ (consultant), any of these → escalate to next tier:
- Same heuristics as depth 0 (still applicable to the consultant's
  own tool calls)
- **Consultant explicit declaration**: response contains "ESCALATE"
  marker OR returns specific exit code → spawn next-tier consultant
- **Reduced-confidence signal**: consultant's advisor returns
  verdict=continue but escalate=true → spawn next-tier as a check

The "consultant-stuck" signal is new — it's the consultant SAYING
"I made progress but I'm not confident, get a second opinion."
Different from worker-floundering.

**Vision (the shift-left target)**: ideally T0 + T1 cover most work;
T2 fires occasionally on harder entries; T3/T4 fire rarely (real
architectural escalations). That's the success state — the
distribution of "depth at which the chain stopped" should skew
heavily toward 1. Operator's framing: "ideally t0 can handle most
with just t1 help — pipe dream but the vision."

## Default tier-graded routes (data-driven)

Per the matrix data + today's success rates:

```yaml
routes:
  default:
    - { driver: copilot, model: gpt-4o-mini }     # T1 consult, x0 = free
    - { driver: copilot, model: claude-haiku-4-5 } # T2, x0.33, today's workhorse
    - { driver: copilot, model: gpt-5.4 }          # T3, x1, terminal-bench 81.8%
    - { driver: claude,  model: claude-opus-4-7 }  # T4, Max sub absorbs
```

Each route entry IS one tier. Depth N picks `route[N]`. Old "rule
matched signal → look up route in routes map → first candidate" stays
for non-recursive escalations; tier-graded routes are the new shape
for recursive ones.

Notable swaps from today's TIER_DRIVER_DEFAULTS (which had T1=local
glm-flash, T3=glm-5.1:cloud):
- **T1 = copilot:gpt-4o-mini** (x0, free, fast — replaces local
  glm-flash which is the WORKER, not a consultant)
- **T3 = copilot:gpt-5.4** (x1, terminal-bench 81.8% — replaces
  glm-5.1:cloud which is currently shipping 0% commits)

## Migration path

Step 4 (currently shipped) is non-recursive. This extension lands as
**step 7** of the kernel-gate-escalation series:

1. ✅ Schema (#349)
2. ✅ routeFor (#350)
3. ✅ spawnPeer (#356)
4. ✅ in-gate wire (#357 + #368 fix)
5. **5b** ✅ hermes driver (#359)
6. **6** (deferred) — conformance feedback loop
7. **NEW**: Recursive escalation
   - 7a: Schema extension (max_depth, max_total, per-tier timeouts, tier-graded routes)
   - 7b: routeFor takes depth param + max_depth check
   - 7c: spawnPeer propagates `CHITIN_ESCALATION_DEPTH=N` env (REPLACES `CHITIN_NO_ESCALATE=1`)
   - 7d: gate-side: read CHITIN_ESCALATION_DEPTH from spawn env to decide whether to
        chain further; cap at max_depth
   - 7e: Provenance becomes `[]Provenance` (chain instead of single struct)
   - 7f: Dispatcher: always start at T0 (entry.tier becomes max-tier ceiling, not start)
   - 7g: chitin replay subcommand

Each step independently shippable. 7a-7e are in the kernel; 7f is
the runner; 7g is operator-facing.

## The shift-left target

**The goal isn't to USE the full T0→T4 chain on every dispatch.** The
goal is for the chain's TYPICAL stop-depth to decrease over time as
T0 (and T1) get better at the work.

Today's data: T2 ships 37% of attempts; T0/T1 are untested as workers.
After mob-prog-escalation lands, expect early-state distribution:
- T0 alone (no escalation): ~10-20% (easy entries)
- T0 + T1: ~30-40% (most common)
- T0 + T1 + T2: ~20-30% (mid-complexity)
- T0 + T1 + T2 + T3: ~10% (heavier reasoning)
- Full T0→T4: ~5% (real architectural escalations)

Then we capture WHY each chain stopped where it did:
- Did T1 truly handle it, or did T0 just give up early?
- Did T2 succeed because the prompt was clearer, or because of model
  capability?
- What does T0 do RIGHT before it floods that we can teach the next
  generation of T0 prompts/skills to do automatically?

That data is the moat. The conformance substrate
(`2026-05-05-conformance-substrate.md`) materializes it as
per-(driver, model, escalation-depth) capability vectors. Operators
of chitin in 6 months should see significantly fewer T2+ escalations
on the same workload than today, because T0 prompts/skills will
have absorbed the patterns the consultants taught them.

## What this is NOT

- **Not auto-tier-graduation per entry**. The entry's `tier:` field
  becomes a CEILING for that entry's escalations (operator's
  signal: "I expect this needs at most T3 help"). It doesn't move
  the starting tier or skip tiers.
- **Not parallel consultants**. One consultant at a time per depth.
  Parallel-consultants is a future design extension if compound
  latency is unacceptable.
- **Not training-loop yet**. The data this captures (escalation
  chains, success rates per chain pattern) will feed future
  prompt-improvement work but that loop isn't shipped here.

## Open questions for operator (before I code 7a)

1. **Default `max_total_per_workflow`** — proposed 10 hops total. Too
   high? Too low? (10 = roughly 2-3 calls per consultant tier on average
   if all 4 tiers fire.)
2. **Default `max_to_tier_T4`** — proposed 1. Too restrictive? (T4 burns
   x15 multiplier on Copilot OR Anthropic Max requests.)
3. **`CHITIN_ESCALATION_DEPTH=0` (no chain) override** — should
   operators be able to disable recursive escalation entirely (single
   peer-spawn only) per chitin-routes.yaml? Lean: yes, equivalent
   to today's behavior.
4. **Entry `tier:` field** — turn into ceiling (recursive can't go
   past), or ignore entirely now that T0 always starts? Lean: ceiling
   so operator can mark "this is a T2-worth entry, don't escalate to
   T4 even if stuck."

## The data-driven moat

This is the operator's framing, captured here as durable decision:
**chitin's moat is the observed-not-declared compatibility +
escalation telemetry**. Every escalation chain IS a training signal:
"T0 needed help on tool call X; T2 fixed it; T3 wasn't needed." Over
weeks, this becomes the substrate that lets T0 shift-left as we
improve its prompts/skills/scaffolding using what the consultants
did to unstick it.

The conformance substrate (`2026-05-05-conformance-substrate.md`) is
where these chains become per-(driver, model) capability vectors.
This doc + that doc are two halves of the same shift-left flywheel.
