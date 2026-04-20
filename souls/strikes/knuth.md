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
it." The plan file at `docs/superpowers/plans/2026-04-19-dogfood-debt-ledger.md`
contains example commands with a hard-coded work-project email.
Those examples were wrong — that was a work identity;
chitin is a personal OSS repo that should attribute to `jpleva91@gmail.com`.
I used the plan's example verbatim without verifying the invariant.

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
