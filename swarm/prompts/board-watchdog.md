Board watchdog: detect rapid promote-demote loops AND auto-groom triage tickets on the chitin AND readybench kanban boards.

For EACH board (chitin, readybench), run:
  hermes kanban --board <board> ls

Board spec roots:
- chitin: ~/workspace/chitin/.specify/specs
- readybench: ~/workspace/.specify/specs

Then:

**Loop detection**: If any ticket was promoted and demoted more than 3 times in the last 24 hours, check whether a spec-kit entry now exists BEFORE re-blocking.

For each loop-detected ticket:
1. Read the ticket body:
   hermes kanban --board <board> show <id>
2. Check BOTH accepted spec bindings:
   a. Reverse binding (preferred for current specs): exact ticket id appears in an existing board-appropriate spec.md:
      grep -R --include=spec.md -nE '(^|[^A-Za-z0-9_])<id>([^A-Za-z0-9_]|$)' <spec_root>
   b. Forward binding: ticket body names an existing `.specify/specs/NNN-<slug>/spec.md` path under that board's spec root.
3. If either binding exists: the manual-spec requirement is satisfied, BUT spec presence alone is not readiness. Before unblocking, inspect the matched spec.md for explicit blockers such as `Blocked until:`, `blocked until`, `Depends on:`, `dependency`, or `tracked in t_XXXXXXXX`. If the spec declares an unsatisfied blocker, DO NOT unblock or promote. Leave/restore the ticket blocked with a dependency reason (not a manual-spec reason), assign to red, and report it as `dependency-blocked`, not `spec-satisfied`.

   Example: if a spec says `Blocked until: chitin-kernel drivers list --json` / `tracked in t_7c9d02b7`, verify that dependency first. If `chitin-kernel drivers list --json` still returns `unknown_subcommand`, keep the ticket blocked for `dependency gate: waiting on t_7c9d02b7 / chitin-kernel drivers list --json`.

   Only if a spec binding exists AND no explicit dependency blocker remains: do NOT re-block for manual spec. If the ticket is currently blocked only for `promote-demote loop detected: needs manual spec`, clear that stale block with:
      kanban-flow unblock <id> --author board-watchdog
   Then comment once if needed:
      🔄 Loop previously detected; spec-kit entry now exists — stale manual-spec block cleared/ignored.
   Report it as `spec-satisfied`, not as a spec queue item.
4. If no spec binding exists: block with:
      kanban-flow block <id> "promote-demote loop detected: needs manual spec" --author board-watchdog
   and assign to red:
      hermes kanban --board <board> assign <id> red

Important: historical loop comments are audit history. They are not enough to re-block a ticket after a valid spec exists.

**Hard write boundary**: Do NOT create, edit, overwrite, or delete any `.specify/` files, `llms.txt`, constitution files, or ticket bodies. Do NOT run `hermes kanban specify` from this watchdog job. The watchdog may only: read board/spec state; add comments; assign tickets; block tickets; unblock tickets when an existing, reviewed spec binding is present and no explicit dependency blocker remains. Missing/partial specs must be reported in the spec queue for a human/operator PR workflow, not written by the watchdog.

**Reviewed-spec requirement**: A spec file created or modified by the watchdog is NOT a reviewed dispatch anchor. For shared/workspace-side specs (readybench uses `~/workspace/.specify/specs`), do not unblock/promote merely because a spec file exists. Require an explicit operator/Clawta approval signal in ticket comments or a merged workspace-side PR before treating the spec as reviewed. If review status is unknown, keep the ticket blocked and report it as `needs spec review`, not `spec-satisfied`.

**Auto-groom triage tickets**: For each triage ticket:
1. Read its body with: hermes kanban --board <board> show <id>
2. If it already has an existing, reviewed spec-kit binding and no explicit dependency blocker, promote it to ready:
   kanban-flow ready <id> --author board-watchdog --assignee codex
   Do not edit its body or create specs.
3. If it lacks a reviewed spec-kit binding, is vague, unclear, or needs a human to write a spec, block it and assign to red:
   kanban-flow block <id> "needs spec: <one-line reason>" --author board-watchdog
   hermes kanban --board <board> assign <id> red

**Escalation**: After processing all triage/loop tickets, post **exactly one Discord message** summarizing tickets that still need specs. Do not include `spec-satisfied` tickets in the spec queue.

**Hard output budget**: The entire escalation post MUST be a single Discord message — fewer than 1500 characters, NOT split into parts. The cron transport splits long output into multi-part `(N/N)` messages which floods agent inboxes and buries operator messages (see ticket `t_74c2cab6`). Length discipline is the fix.

Format (single message, ≤1500 chars):

🔔 **Spec queue — <board>**: N tickets need specs

Top 3 by priority:
  • t_xxxx: Title — reason
  • t_yyyy: Title — reason
  • t_zzzz: Title — reason

Full list: run `hermes kanban --board <board> ls --status blocked --assignee red` for all N tickets and their block reasons.

Rules:
- List at most 3 tickets inline. The full set is available via the command above; don't enumerate it.
- One ticket per line. No multi-line descriptions.
- Truncate each title to ~50 chars if needed.
- Skip the post entirely when there's nothing to escalate.
- Do not raise an error merely because the report contains findings; complete normally after reporting.