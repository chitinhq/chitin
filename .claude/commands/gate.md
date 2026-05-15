# /gate — Run the adversarial review before PR open, not after

Codify a recurring finding as a permanent, typed check — a lint rule,
a CI check, or a pre-PR hook — so the adversarial review runs *before*
the PR exists instead of as a comment after. Every gate is a finding
that can never be filed again.

## Usage

```
/gate "<finding>"               — Promote a described finding into a gate
/gate from invariant <prim>     — Enforce that a /invariant primitive is used
/gate preflight                 — Run all active gates against the current branch
/gate list                      — Show active gates + what each one catches
```

## What a gate is

A gate is **specific and typed, not regex mush** (the sentinel
promotion rule). It takes one of three forms:

| Form | Runs | Example |
|---|---|---|
| Lint rule | On save / pre-commit | "no raw `json.Unmarshal` on event payloads — use `hashNormalized`" |
| CI check | On push | "every `chitin.yaml` change ships a regression test in the same PR" |
| Pre-PR hook | `/gate preflight` | "no `delegate_task(acp_command=copilot)` — route through clawta" |

`/gate preflight` is the adversarial pass itself: run it before
`/land` or `gh pr create` and it catches the findings that would
otherwise come back as review comments.

## Promotion rule

Promote a finding into a gate only when **all** hold (mirrors the
sentinel role's promotion rule — do not invent policy from weak
evidence):

1. The pattern repeats — it is a class, not a one-off.
2. The check is specific and typed — it names the exact shape.
3. The predicted false-positive rate is bounded and acceptable.
4. You can prove the gate with a test: one fixture it catches, one it
   correctly passes.

If the finding is real but not yet repeating, leave it as a `/verdict`
note on the PR — don't gate it yet.

## The contract (in order)

1. **Name the finding** as a one-line claim: *"PRs that touch
   `internal/gov` without a paired test are unsafe to merge."*
2. **Pick the form** — lint / CI / pre-PR — by where it can fire
   *earliest* and still be cheap.
3. **Write the gate + its two-fixture test** (one caught, one passed).
4. **Register it** so `/gate list` and `/gate preflight` pick it up.
5. **Backfill once**: run the new gate against open branches and file
   the hits as `/verdict rework` notes — don't silently break them.

## Chains with

- **Input** ← `/invariant` (a primitive exists → gate enforces its
  use) or `/mine` (a deny rule repeats → gate stops the action shape).
- **Runs before** → `/land` and `gh pr create`: `/gate preflight` is
  the pre-open adversarial review.
- **Output** → `/verdict`: preflight hits become `rework` dispositions
  with the gate name as the must-fix reason.

Goal-runner form: `/goal "make <finding> impossible to ship"` →
`/gate "<finding>"` → `/gate preflight` on every branch after.

## Anti-patterns

- **No broad allow/deny from weak evidence.** One occurrence is a
  `/verdict` note, not a gate.
- **No bulk-tuning.** One gate per finding, each independently
  testable and removable.
- **Don't gate what `/invariant` should kill.** If the class can be
  made structurally impossible by a primitive, do that first — a gate
  is for what *can't* be designed out.

## Why this exists

Remote-control interview, 2026-05-14: "turn the Clawta findings
taxonomy into actual lint/CI gates so the adversarial review runs
before PR open, not after." Accountability for PR quality without the
authority to set merge gates is review with partial leverage. This is
the gate-setting half.
