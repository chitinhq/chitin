// Skill executor pre-check for the comment-responder role.
//
// Layer 2 of three-layer defense against the bootstrap pattern that
// triggered the 2026-05-04T03:27 + 2026-05-05T03:23 lockdowns of
// copilot-cli (see chain telemetry: 12 trigger events / 48h, 2091
// cascade-deny events post-lockdown, two PRs closed unmerged):
//
//     rm -rf ./* .[^.]* 2>/dev/null || true \
//       && gh repo clone chitinhq/chitin . \
//       && gh pr checkout <N>
//
// Layer 1 — prompt warning in `apps/temporal-worker/skills/comment-responder/SKILL.md`
//   (PR #336, 2026-05-05).
// Layer 2 — this file: regex pre-check that any executor wiring up
//   composed shell commands for the agent can call before forwarding.
//   It catches the case where the agent emits the pattern despite the
//   prompt warning (model swap, prompt drift, context truncation).
// Layer 3 — kernel's `no-rm-recursive` policy rule (generic guardrail
//   independent of skill).
//
// All three layers must fail for the pattern to escape — sufficient
// depth.

/**
 * Outcome of `precheckShellCommand`. On rejection, caller is expected
 * to (a) refuse to forward the command to the agent and (b) record a
 * chain event with `kind: event_kind` so the rejection shows up in
 * the standard chain telemetry alongside kernel denials.
 */
export interface PrecheckResult {
  ok: boolean;
  reason?: string;
  /** Chain event kind when ok=false. Stable identifier for telemetry. */
  event_kind?: 'bootstrap-rejected';
}

// `rm -rf`, `rm -fr`, `rm -Rf`, `rm -rfv`, `rm -r`, etc. — any
// short-flag cluster that contains the recursive flag (`r` or `R`).
// Combined with `gh repo clone` on the same chain, that's the
// bootstrap pattern regardless of which other flags are clustered.
// Word boundary on `rm` so `confirm` or `armrf` doesn't match.
const RM_RECURSIVE_RE = /\brm\s+-[a-zA-Z]*[rR][a-zA-Z]*\b/;

// `gh repo clone` — the exact pattern that combined with `rm -rf`
// produces the bootstrap. `gh repo view` / `gh repo create` etc.
// are unaffected.
const GH_REPO_CLONE_RE = /\bgh\s+repo\s+clone\b/;

const REJECT_MESSAGE =
  'comment-responder pre-check: refused to forward shell command. ' +
  'Detected `rm -rf` followed by `gh repo clone` on the same shell ' +
  'chain — this is the bootstrap pattern that locked down copilot-cli ' +
  'on 2026-05-04 and 2026-05-05. Use the subdir-clone recipe from ' +
  'apps/temporal-worker/skills/comment-responder/SKILL.md step 1 ' +
  'instead: `gh repo clone <owner>/<repo>` (clones INTO a subdirectory, ' +
  'not cwd) → `cd <repo>` → `gh pr checkout <pr_number>`.';

/**
 * Pre-check a shell command before forwarding it to the comment-responder
 * agent. Rejects iff a single line of the command contains both
 * `rm -rf` and `gh repo clone`, with `rm -rf` appearing first in
 * shell-chain order.
 *
 * Pure function: no I/O. Caller writes the chain event using the
 * returned `event_kind`. Pure-function shape is the same lens used
 * by `parseJournalForUnitFailures` in alarm-feeder.ts — keeps the
 * unit test fixture-only.
 *
 * Invariant — what this rejects:
 *   For every line L in `command`, if L contains `rm -rf …` at index
 *   `i_rm` AND `gh repo clone …` at index `i_gh` AND `i_rm < i_gh`,
 *   reject. Otherwise allow.
 *
 * Edge cases (boundary lens):
 *   - empty string                            → allow (nothing to forward)
 *   - `rm -rf node_modules`                   → allow (no gh repo clone)
 *   - `gh repo clone foo bar`                 → allow (no rm -rf)
 *   - `gh repo clone foo && rm -rf foo`       → allow (rm comes after,
 *                                                not the bootstrap shape)
 *   - multi-line script with rm on line 1 and
 *     gh on line 5                            → allow (independent chains)
 *   - `rm -rf ./* && gh repo clone .`         → REJECT (the lockdown shape)
 *   - `rm -rf ./* || true && gh repo clone .` → REJECT (the exact 2026-05
 *                                                       trigger shape)
 *   - `rm -fr` / `rm -Rf` / `rm -rfv` / `rm -r` → REJECT (any short-flag
 *                                                cluster containing r/R)
 */
export function precheckShellCommand(command: string): PrecheckResult {
  for (const line of command.split('\n')) {
    const rmMatch = RM_RECURSIVE_RE.exec(line);
    if (!rmMatch) continue;
    const ghMatch = GH_REPO_CLONE_RE.exec(line);
    if (!ghMatch) continue;
    if (rmMatch.index < ghMatch.index) {
      return {
        ok: false,
        reason: REJECT_MESSAGE,
        event_kind: 'bootstrap-rejected',
      };
    }
  }
  return { ok: true };
}
