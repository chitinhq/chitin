# chattr +i workspace hardening investigation

Date: 2026-05-11

## Question

Could agent workspaces use Linux `chattr +i` immutable bits as an extra
guardrail against governance bypasses?

## Finding

`chattr +i` is not a good default for whole agent workspaces. It is
filesystem-specific, generally requires elevated capability, breaks normal
agent edits, and is invisible to non-Linux environments. It also does not
replace chitin's kernel gate: an immutable-bit failure happens after the tool
is already attempted, while chitin's value is the pre-tool decision and audit
chain.

## Where it may help

Use it only as an operator-owned hardening layer for small, stable paths that
agents should never mutate directly, such as a checked-out `chitin.yaml`,
wrapper scripts, or hook installation directories. Do not apply it to task
worktrees, source trees under active edit, build outputs, or caches.

## Recommendation

Keep `chattr +i` out of the kernel hot path and installer defaults. If the
operator wants defense in depth on Linux, document an opt-in runbook that
sets immutable bits on policy and hook files outside active worktrees, with
explicit `chattr -i` rollback steps. The enforceable chitin-side control
remains typed pre-tool governance plus the denial cascade lockdown.
