# Strike log — Hamilton

Operational feedback against the Hamilton soul. Each entry records a case
where the lens (or the agent operating under it) produced work that failed
to meet its own stated heuristics. Kept separately from
`souls/canonical/hamilton.md` so the soul_hash remains stable for forensic
event chains.

Format per strike:
- ID, date, session context
- What the lens was supposed to catch
- What actually happened
- Which heuristic was violated
- Remediation / learning

---

## Strike 1 — 2026-04-20 — Merged PR #26 14 seconds before Copilot's third review landed

**Context.** First operational trial of the `/ship-review` skill (written
earlier the same session). Hamilton was the active adversarial lens and
stayed loaded through Phase 4's failure-mode batch. The cycle on
chitinhq/chitin PR #26 went: Copilot review (11 findings) → Hamilton
adversarial review (6 findings) → Knuth + Hamilton patch commits → push at
15:49 UTC → poll for Copilot re-review → merge at 16:00:18 UTC.

**What the lens was supposed to catch.**
Heuristic 3 — *"Trained users won't make that mistake" is the bug report.*
The polling loop that decided "silence = approval" after 10 minutes was
the operator's own homemade defense, trusting the operator to pick the
right interval. Hamilton's own rule: if the defense depends on a human
being careful at 3am, it's not a defense.

Heuristic 1 — *The system will fail in ways you didn't design for.* I
wrote the `/ship-review` skill with a poll loop, then trusted the poll
loop. The failure mode "Copilot reviews BETWEEN your last poll and your
merge call" was not in my model. Hamilton's whole job is to name that
class.

**What actually happened.**
Copilot submitted a third review at 16:00:04 UTC with 7 new findings (2
real bugs, 1 real documentation bug, 2 ergonomic nits — notably that the
kernel emits `events-<run_id>.jsonl` while my runbook referenced the
non-existent `events.jsonl` path). I merged at 16:00:18 UTC — 14 seconds
later — because my last poll (several minutes prior) had shown no new
reviews and I interpreted the silence as approval. The findings were
real and material; they had to be addressed in a follow-up PR (#28).

The merge wasn't a hard failure — the code shipped was still better than
main before the PR — but a documented operator runbook shipped with a
wrong filename that would mislead on-call response. That exactly fits the
class of regression Hamilton is supposed to flag.

**Heuristic violated.**
Primary: heuristic 3 ("trained users won't make that mistake" is the bug
report). The polling loop was the footgun; trusting it as a defense was
the fire.

Secondary: heuristic 1 (design for the mistake). No guard in the merge
step that re-checked Copilot state immediately before `gh pr merge`. The
skill's Phase 6 checklist said "CI is green" and "no blocking comments"
— both were true at poll time, neither was re-verified at merge time.

**Remediation.**
- PR #28 filed the same session with fixes for the 7 missed findings.
- `/ship-review` skill patched: Phase 1 documents correct Copilot
  re-review trigger (`gh pr edit --add-reviewer @copilot`, per GitHub
  docs — NOT `@copilot` mentions or `/copilot review` slash-style
  comments which have no documented effect). Phase 6 gains a pre-merge
  freshness check: fetch `gh pr view --json reviews` immediately before
  `gh pr merge`, and if the latest Copilot review is newer than the last
  poll, loop back to Phase 3 instead of merging.
- ELO: Hamilton −1 → 1499. Event logged in `souls/elo.md`.

**Learning for the lens.**
Hamilton's heuristic 3 must bind specifically to operator procedures I
*myself* write in the same session. When I author a workflow AND run it,
the review of the workflow and the review of the work must be separate
passes under different eyes. A lens cannot adversarially review its own
instrument.

The skill-author and the skill-user should not be the same lens on the
same day.
