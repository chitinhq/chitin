# /invariant — Eliminate a bug class, don't fix a bug

Turn a **recurring** finding into a shared, tested primitive plus an
invariant test that makes the whole class structurally impossible.
The unit of work is the *class*, not the instance: "bugs classes
eliminated," not "PRs touched."

This is the upstream half of review. `/land` and adversarial review
find the same bug five times across five PRs; `/invariant` finds it
once and ends it.

## Usage

```
/invariant "<pattern>"        — Census + extract + prove for a described class
/invariant from <finding>     — Seed from a /mine row or a Clawta finding id
/invariant audit              — Scan the repo for the known recurring classes
/invariant list               — Show primitives already extracted + their gate
```

## The contract (in order)

**State the invariant in one sentence before any code.** If you can't,
the class isn't understood yet — stop and read more. Example:
*"Every event hash is computed over the schema-normalized form, so
stripping unknown keys can never change the hash."*

1. **Census.** Find *every* occurrence of the class, not the one in
   front of you. `rtk grep` the shape across the repo; list each
   call-site with `file:line`. The census is the scope — a class with
   one occurrence is not a class, it's a bug (go fix it directly).

2. **Extract.** Pull the defense into one named primitive. Refuse
   vague names — `validate`, `normalize`, `check` are names of things
   not yet understood. The honest name (`hashOverNormalizedForm`,
   `freezeManifestSnapshot`) usually collapses the function by half.

3. **Prove.** Write the invariant test as a claim over *branches*,
   not a single happy-path run. Walk the boundaries: empty, single,
   N-equal, max value, out-of-order, same-timestamp. A passing test is
   evidence; an invariant that holds across all inputs is the proof.

4. **Migrate.** Replace every call-site from the census with the
   primitive. The diff should show the ad-hoc defense *deleted*, not
   left beside the new one.

5. **Evidence.** The PR body states the invariant sentence, the
   census count ("9 call-sites → 1 primitive"), and the boundary
   cases the test covers. Title: `harden(<class>): <invariant sentence>`.

## Known recurring classes (audit targets)

| Class | Invariant | Primitive shape |
|---|---|---|
| hash-before-parse | Hash is computed over the schema-normalized form | `hashNormalized(raw)` |
| pointer aliasing | Manifest snapshots are immutable after capture | `freezeSnapshot` / JSON round-trip |
| finalization lifecycle | No event appends after a terminal event | emitter rejects post-`session_end` |
| schema-column defense | Missing columns degrade, never panic | `taskField(row, col)` with default |
| event-chain on timeout | Partial work on timeout captures the real chain hash | `chainHashOrDetect()`, never hard null |

`/invariant audit` greps for each shape and reports which still have
ad-hoc, un-primitived occurrences.

## Chains with

- **Input** ← `/mine` (a deny rule or PR-failure pattern that repeats)
  or a Clawta/Copilot finding that has shown up on more than one PR.
- **Output** → `/gate`: once the primitive exists, `/gate` makes its
  use enforceable so the class can't quietly come back.
- **Output** → `/verdict`: record the disposition of any PR this
  supersedes (`rework` → now `merge-ready` or closed).

Goal-runner form: `/goal "kill the <class> bug class"` → `/invariant`
→ `/gate from invariant <primitive>`.

## When to use

- A finding has appeared on **2+ PRs** or fires as a repeated deny.
- A `/mine` run surfaces the same rule or failure shape repeatedly.
- Adversarial review keeps writing the same comment.

## When NOT to use

- A genuinely one-off bug — fix it in its own PR, don't manufacture a
  primitive for a class of one.
- A finding you can't state as a one-sentence invariant — it's not
  ready; understand it first.

## Why this exists

Remote-control interview, 2026-05-14: "The same classes of bug keep
recurring across PRs… I find the same bug 5 times instead of once."
Sixty-bugs-fixed sessions are partly a symptom of a missing prevention
layer. This skill is that layer's entrypoint.
