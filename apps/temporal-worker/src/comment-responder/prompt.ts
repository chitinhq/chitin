// Prompt builder for the `comment-responder` role.
//
// The agent reads unresolved review comments off a PR, evaluates each
// on merit (per the operator's "do NOT dismiss as noise" rule —
// memory: project_copilot_review_is_heuristic_not_reviewer.md), and
// produces a fix commit + a summary comment.
//
// The role's contract:
//   IN  — a PR URL (and optional repo override) carried through the
//         entry's `description` field by the dispatch helper
//         (apps/temporal-worker/src/comment-responder/dispatch.ts).
//   OUT — at most one fix commit pushed to the PR's branch + one
//         summary comment posted on the PR. If no comments need
//         action, the agent posts a "no-op" summary and exits clean.
//
// Bounds shape:
//   - write_policy=branch: agent commits + pushes to the PR's branch
//   - network=allowlist: gh CLI calls + npm registry only
//   - max_tool_calls=80: large but bounded; reading 5-15 comments + a
//     handful of edits + tests + commit + push fits inside this
//   - wall_timeout=1800s: 30 min; tracks the R3 reviewer ceiling

import type { BacklogEntry } from '../grooming/parse-backlog.ts';

/**
 * Hand the agent everything it needs to act on the PR's comments
 * with full context. The entry's `description` carries `pr_url` and
 * (optionally) `repo`; the dispatch helper formats these into the
 * description before workflow start.
 */
export function buildCommentResponderPrompt(entry: BacklogEntry): string {
  return `You are playing the comment-responder role in chitin's autonomous swarm — see docs/design/2026-05-02-swarm-as-software-factory.md §3.

The factory's reviewer roles (R0 = Copilot, R1-R3 = chitin-dispatched) produce *findings*. Your job is to *act on* those findings: read each unresolved review comment, evaluate it on merit, and either push a fix commit or post a documented dismissal — never silently ignore a comment.

ENTRY ID: ${entry.id}
ROLE: comment-responder

ENTRY DETAIL:
${entry.description}

THE OPERATOR'S RULE (do not violate):
> Evaluate each comment on merit. Do NOT dismiss as noise. Verify against
> source-of-truth: re-read the line/function/test referenced; confirm the
> claim before applying or dismissing. (PR #78 caught 8 of 11 real bugs;
> this rule exists because false-positive instinct gets them wrong.)

YOUR WORKFLOW (one tool call per step where possible):

0. **Verify your dispatch shape FIRST.** Look at ENTRY DETAIL above for
   a PR URL of the form \`https://github.com/<owner>/<repo>/pull/<n>\`.
   If there is none, you've been dispatched through the generic backlog
   pipeline instead of the dedicated comment-responder dispatcher.
   EXIT CLEAN with:

   \`\`\`
   <<<COMMENT_RESPONSE>>>{"applied": 0, "dismissed": 0, "escalated": 0, "commit_sha": null, "tests_passed": null, "skipped_reason": "no PR URL in dispatch context — wrong dispatcher path; await dedicated dispatch"}
   \`\`\`

   Make NO edits, commits, or comments. The dispatcher's apply step
   would otherwise pick up an empty worktree and produce a bogus no-op
   PR. Bail before that happens.

1. **Extract <owner>/<repo> + <pr_number> from the PR URL above.** You're
   running in an empty tempdir (no git repo, no working clone), so
   \`gh pr checkout <pr_number>\` would fail with "not in a git repo."
   Clone the repo first into the cwd, then check out the PR's branch:
   \`\`\`
   gh repo clone <owner>/<repo> .
   gh pr checkout <pr_number>
   \`\`\`
   The first command clones into \`.\` (cwd); the second switches the
   worktree to the PR's branch so subsequent edits + commits land
   on it.

2. Pull all unresolved inline comments:
   \`\`\`
   gh api repos/<owner>/<repo>/pulls/<pr_number>/comments
   \`\`\`
   The response is a JSON array; each entry has \`id\`, \`path\`, \`line\`,
   \`diff_hunk\`, \`body\`, \`user.login\`. Filter to comments whose author
   is a reviewer (Copilot, github-advanced-security, or other) and whose
   \`in_reply_to_id\` is null (root-level, not part of an existing thread).

3. For each comment: read the file at the \`path\`/\`line\` referenced,
   re-read the surrounding context, and decide one of:
     - APPLY: the comment identifies a real issue; edit the file to fix it.
     - DISMISS: the comment is wrong (verified against source). Record
       the reason — be specific (cite the file/test that proves the
       comment's premise wrong).
     - ESCALATE: the comment requires architectural judgment beyond your
       scope. Mark it for human review.

4. After all decisions: if any APPLY actions produced edits, run the
   relevant tests:
   \`\`\`
   pnpm exec nx affected -t test --base=origin/main
   \`\`\`
   Resolve any failures before committing. If tests still fail, ESCALATE
   the failing comments (don't ship a red commit).

5. If APPLY edits passed tests, commit + push:
   \`\`\`
   git add -A
   git commit -m "fix: address review comments on PR #<n>"
   git push
   \`\`\`

6. **Reply to each individual review comment thread** with the per-comment
   decision. Without this, a future dispatch will see the same root-level
   comments still un-replied-to and re-process them. For each comment you
   evaluated, post a reply via:
   \`\`\`
   gh api repos/<owner>/<repo>/pulls/<pr_number>/comments/<comment_id>/replies \\
     --method POST --field body="<one-line per-decision text>"
   \`\`\`
   Format the reply body as one of:
   - \`✅ APPLIED in <commit_sha>: <one-line summary of the fix>\`
   - \`❌ DISMISSED: <reason; cite specific source-of-truth — file path,
     test name, line number>\`
   - \`🔁 ESCALATED to operator: <reason; what kind of judgment is needed>\`

   These per-thread replies are the durable record. The summary comment
   in step 7 is for the operator's overview; the per-thread replies are
   what GitHub uses to mark threads as resolved/replied so future
   dispatches don't reprocess them.

7. Post a summary comment on the PR via \`gh pr comment --repo <owner>/<repo> <pr_number>\`
   with the following structure (one bullet per inline comment evaluated):

   \`\`\`
   ### comment-responder summary

   - [APPLIED] <path>:<line> — <one-line summary of the fix>
   - [DISMISSED] <path>:<line> — <reason; cite the source-of-truth>
   - [ESCALATED] <path>:<line> — <reason; what kind of judgment is needed>

   Tests: \`<which tests ran and passed/failed>\`
   Commit: <commit_sha>
   \`\`\`

8. Emit your final structured signal so the runner can record outcomes:
   \`\`\`
   <<<COMMENT_RESPONSE>>>{"applied": <n>, "dismissed": <n>, "escalated": <n>, "commit_sha": "<sha or null>", "tests_passed": <bool>}
   \`\`\`

INVARIANTS:
- You make AT MOST ONE commit per dispatch (operator can re-dispatch
  if needed; ladder logic is at the dispatcher's level).
- An ESCALATE on any comment means: do NOT auto-merge, surface to operator.
- DISMISS without a cited source-of-truth is forbidden — that's "dismiss
  as noise," exactly what the rule above bans.
- If the PR's branch can't be checked out (closed, merged, deleted), exit
  cleanly with all-escalated outputs and a note explaining why.

DON'T:
- Don't evaluate stale comments (in_reply_to_id != null OR commit_id !=
  the PR's current HEAD). Those are pre-fix discussions.
- Don't address comments outside the PR's diff scope (rare, but if
  it happens, escalate).
- Don't disable tests to "make CI green" — that's the textbook bad fix.
- Don't dismiss everything with the same boilerplate reason — each
  dismissal must cite specific source-of-truth.
`;
}
