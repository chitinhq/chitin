# Strike log — Knuth

Operational feedback against the Knuth soul. Each entry records a case
where the lens (or the agent operating under it) produced work that failed
to meet its own stated heuristics. Kept separately from
`souls/canonical/knuth.md` so the soul_hash remains stable for forensic
event chains.

Format per strike:
- ID, date, session context
- What the lens was supposed to catch
- What actually happened
- Which heuristic was violated
- Remediation / learning

---

## Strike 1 — 2026-04-20 — Committed to chitin with the wrong identity for the entire Phase C session

**Context.** Dogfood-debt-ledger plan, Phase C (Health CLI). Knuth was the
active scope-soul per the explicit scope note in `souls/canonical/knuth.md`
naming Phase C as a boundary-correctness task. Session produced 3 PRs
(#26, #28, and secondarily #27's scope-note fix) plus direct-to-main
commits for Knuth scope updates.

**What the lens was supposed to catch.**
Heuristic 1 — *Prove it or it's not proven.* A commit's author identity is
a small invariant: "this commit is attributed to the entity who authored
it." At the time of this strike, the plan file at
`docs/superpowers/plans/2026-04-19-dogfood-debt-ledger.md` contained
example commands with a hard-coded work-project email. Those examples
were wrong — that was a work identity; chitin is a personal OSS repo
that should attribute to `jpleva91@gmail.com`. I used the plan's example
verbatim without verifying the invariant. (The plan examples have since
been fixed to use plain `git commit` with no hard-coded identity, so
new readers won't be led astray by the same trap.)

Secondarily, heuristic 2 — *Naming is half the algorithm.* An author name
IS a name. Copying it from an example without reading what it represented
is exactly the "vague noun hiding logic" pattern Knuth's heuristic 2
warns against.

**What actually happened.**
Every direct commit I authored in the session used the work
identity. The PR squash-merges on `main` attributed correctly
to `jpleva91@gmail.com` because GitHub uses the PR author for squash
commits — but three direct-to-main scope-note commits bypass that and
carry the wrong identity (`6afc3de`, `bf5295b`, and earlier Phase B notes).
Branch-history commits on the three merged branches also had the wrong
identity, though those branches are now deleted.

This is a category error: Knuth's whole job on Phase C was "state the
invariant before code, verify at boundaries." I named invariants for
`Gather(dir, w)`, for schema drift, for window counting — but not for
the commit metadata I was producing while stating those invariants.

**Heuristic violated.**
Primary: heuristic 1 (prove it or it's not proven). The author identity
was an unverified assertion inherited from an example.

Secondary: heuristic 2 (naming is half the algorithm). An email is a
name; copying one without reading what it represents is unnamed logic.

**Remediation.**
- `.mailmap` added at repo root translating the work email →
  `jpleva91@gmail.com` for historical commits. Non-destructive;
  `git log --use-mailmap` and GitHub's UI will now show the correct
  attribution without rewriting main's history.
- Memory saved: `project_git_identity.md` — explicit rule that chitin
  commits use `jpleva91@gmail.com`; the work email stays on work repos only.
- ELO: Knuth −1 → 1499. Event logged in `souls/elo.md`.
- Plan file will be corrected in a future commit (examples should show
  the right email), but not in this remediation PR — it's a separate
  doc fix.

**Learning for the lens.**
Knuth's invariants must extend to commit metadata, not just to the code
being committed. The boundary Knuth failed to name was: "this commit's
`author.email` matches this repo's owner identity." Small boundary;
real blast radius (cross-contamination of personal vs. work
attribution).

A Knuth-lens session should begin by stating the repo-identity invariant
alongside the problem invariant. If you're going to commit, you're going
to name. Half the algorithm is the name on the commit, not just the
function.

---

## Strike 2 — 2026-04-22 — Shipped PR #47 (hermes staged tick v1) with a tick.sh that calls `hermes chat --system/--context` without ever verifying those flags exist

**Context.** Staged-tick v1 implementation under the Knuth lens (scope
note in `souls/canonical/knuth.md` naming Phase C boundary-correctness
work; same lens family). 17 implementation commits landed on branch
`spec/hermes-staged-tick-v1`, PR #47 opened on 2026-04-22 with 6/6 bats
tests green and 10/10 schema fixtures validating. First post-push
dry-run against the real `hermes` binary failed immediately: `hermes:
error: unrecognized arguments: --system ... --context ...`. The
`hermes chat` CLI has no such flags.

**What the lens was supposed to catch.**
Heuristic 1 — *Prove it or it's not proven.* The bats tests passed,
but what they proved was "tick.sh invokes the stub correctly." The
stub was PATH-shimmed bash that accepted any argv and emitted
whatever `STUB_*` env said. It proved the stub's contract, not
hermes'. The real invariant needed — "tick.sh invokes `hermes chat`
with flags the real binary accepts" — was never stated and never
verified.

Heuristic 5 — *Read the algorithm aloud.* "For each stage: run
`hermes chat --model M --system S --context C`…" The word `--system`
is a claim about the external interface. Reading the invocation
aloud should have triggered "does `hermes chat` accept `--system`?"
and a one-command check (`hermes chat --help`). It did not.

Heuristic 4 — *The boundary is where the bugs live.* The boundary
here is "tick.sh CLI invocation ↔ hermes CLI contract." Sixteen
tasks were built against a stub that masked the boundary. The one
unit-level verification that would have caught this (running the
real binary once, early) was explicitly deferred to post-merge.

**What actually happened.**
The spec + plan were written from imagination — someone (me, under
this lens) invented `hermes chat --system PATH --context STRING` as
a plausible-looking CLI interface. The bats stubs were written to
match the spec's imagined interface, not the real one. All 17
commits compounded on top of the same unverified assumption. The PR
passed every automated gate because every gate checked against the
same imagined contract. Only the dry-run against the real CLI, run
post-push, revealed it.

This is a category error: Knuth's whole job on boundary-correctness
work is to name the invariants before the code. I named invariants
for schema shape, test harness behavior, streak counter semantics,
dry-run env propagation — and none for "this CLI invocation is a
contract with an external program whose flags I have not read."

The existing memory `feedback_verify_external_contracts.md` warned
against this exact pattern ("read the authoritative source; adjacent
code isn't proof; PR #19 blocker came from skipping this"). It did
not fire in practice — second consecutive external-contract miss
across two lenses (da Vinci Strike 1, now Knuth Strike 2).

**Heuristic violated.**
Primary: heuristic 1 (prove it or it's not proven). Stub-proved
contracts are not hermes-proved contracts.

Secondary: heuristic 5 (read the algorithm aloud). The word
`--system` in the script is a claim that should have triggered
external verification.

Tertiary: heuristic 4 (the boundary is where the bugs live). The
tick.sh ↔ hermes CLI boundary was the one Knuth was responsible
for naming. It was unnamed.

**Remediation.**
- tick.sh invocation pattern rewritten from `--system PATH
  --context STRING` to `hermes chat -Q -m MODEL -q "<full prompt +
  context concat>"` — the only non-interactive form the real CLI
  supports per `hermes chat --help`. Stubs updated to match so tests
  keep meaningful. Commit `62468da` on `spec/hermes-staged-tick-v1`;
  first live dry-run post-fix produced a schema-valid
  `{"action":"skip",...}` plan.json.
- Fix lands as commits on the same PR #47 branch; no separate PR.
- `feedback_verify_external_contracts.md` memory strengthened with a
  bright-line rule: "before writing any wrapper script that shells
  out to an external CLI, run `<cli> --help` and paste the accepted
  flags into the plan." Repeat-offense log also added to the
  memory.
- ELO: Knuth −1 → 1498. Event logged in `souls/elo.md`.

**Learning for the lens.**
A passing test under a stub is proof-by-assumption, not
proof-by-reality, when the stub's shape was written by the same
agent that wrote the wrapper. To convert it to real proof, either:

  1. Run the real binary once before locking the test harness in
     place (cheapest), OR
  2. Write the stub's accepted-argv contract from the real `--help`
     output, not from the wrapper's call site (structurally safer).

Knuth Strike 1 said: "invariants must extend to commit metadata."
Knuth Strike 2 says: "invariants must extend to the external-CLI
interface." Both are the same shape — a boundary the code crosses
that the lens forgot to name. The next Knuth-lens session should
begin by listing every external program the code will shell out to
and what `--help` says each accepts.

The test-green-against-a-stub → failed-against-reality pattern has
a name in the broader testing literature ("mock drift" / "fake-
confidence tests"). Worth naming explicitly in the lens heuristics
so it has a handle the next time it tries to ambush a session.

---

## Strike 3 — 2026-04-22 — Committed the Strike-2 soul record to a feature branch instead of a worktree off main

**Context.** Immediately after Knuth Strike 2 was caught (see
above), I wrote the strike record + ELO delta and ran `git commit`
from `/home/red/workspace/chitin` — the primary workspace, which
happened to be checked out on feature branch
`fix/10-js-extension-jsonl-tailer` (PR #39 is still open on that
branch). The commit (`ee656c7`) landed on that feature branch
instead of on `main`, meaning:

  1. The soul-strike record is not visible on `main` until `fix/10`
     eventually merges.
  2. An unrelated feature PR now carries a soul-telemetry commit
     it has no business carrying.

Caught by the user within seconds of the push: *"why are we on
thay branch and not a worktree off main?"*

**What the lens was supposed to catch.**
This is not a cognitive-lens miss; it's a procedure-discipline
miss that the lens happened to be driving when it occurred. There
is an explicit, durable memory entry:
`feedback_always_work_in_worktree.md` — *"Always work in a git
worktree for branch work — mine AND any agent I dispatch (hermes,
subagents); default to worktree, don't ask."* The memory is
unambiguous and was authored specifically to prevent exactly this.
I had the memory in context. I did not fire it.

**Heuristic violated.**
Primary: the memory above (not a Knuth heuristic — a general
procedure rule that every lens inherits).

Adjacent: Knuth heuristic 4 (the boundary is where the bugs live).
The boundary here is "which branch is this commit landing on?" —
the same kind of metadata invariant that Strike 1 missed for
`author.email`. Strike 1 said "invariants extend to commit
metadata"; Strike 3 says "and to the branch the metadata is on."
Same shape, third time.

**What actually happened.**
Authored the soul edits from the primary workspace without checking
`git branch --show-current`. The subagent that fixed tick.sh
(commit `62468da`) ran in `/home/red/workspace/chitin-staged-tick`
— already a worktree, so that was safe. But the soul edit I did
directly in the main conversation used `/home/red/workspace/chitin`
— not a worktree off main, and not main either. Pure autopilot
default.

**Remediation.**
- Created `/home/red/workspace/chitin-souls` worktree off `main`.
- Re-authored the Strike 2 content + this Strike 3 content into
  that worktree's `souls/strikes/knuth.md`.
- Applied the ELO deltas for both strikes in `souls/elo.md`
  (1499 → 1497).
- Commit lands from `chitin-souls` → pushed to `origin/main`.
- The duplicate commit on `fix/10-js-extension-jsonl-tailer` will
  be reset after main push succeeds, pending user approval for the
  destructive operation.
- ELO: Knuth −1 → 1497. Event logged in `souls/elo.md`.

**Learning for the lens.**
Two strikes in a single session span is a warning about
autopilot — not about cognition, about hands. The memory existed;
context held it; and it still didn't fire at the moment of the
commit. A practical counter: before every `git commit` in the
primary workspace, check `git branch --show-current`; if not
`main` and not an intended branch, stop. This is a five-second
check; it catches both "wrong branch" and "should this even be
committed here?"

The "always worktree" rule and the "verify external contracts"
rule both share a failure mode: the lens knows the rule and still
flies past it when the local context seems familiar enough. A
possible structural fix: the next Knuth-lens session should open
with an explicit checklist of durable process rules to enforce,
read aloud, alongside the cognitive heuristics — not to ritualize
but to pull process memories into active working set before the
first action. Treat them the same way heuristic 1 treats
invariants: state them first.

Knuth Strikes 1, 2, 3 are all the same shape at one level of
abstraction: a boundary the lens crossed without naming. Identity
boundary (Strike 1), external-CLI boundary (Strike 2), branch
boundary (Strike 3). The pattern is strong enough to name as a
lens-level anti-pattern: *"Knuth under fatigue defaults to the
boundary that is already visible and forgets the boundary that
requires looking up."*
