---
status: open
owner: claude-code
kanban: t_0567e90a
implementation_pr: null
superseded_by: null
effective_from: '2026-05-12'
effective_to: null
---

# Spec: close shell-subprocess governance-self-modification bypass

Date: 2026-05-12
Status: spec — open
Kanban: `t_0567e90a` (priority 80)
Author: claude-code (operator-controlled, spec writer)

## Problem

Demonstrated 2026-05-11 ~07:33 EDT and again ~07:47 EDT during the
Hermes-Clawta dispatch architecture work: any agent with `Bash` /
`shell.exec` permission can mutate `chitin.yaml` (and any other path
protected by `no-governance-self-modification`) by:

1. Writing a script to a non-protected path (`/tmp/foo.py`, allowed)
2. Invoking the script via `shell.exec` (`python3 /tmp/foo.py`)
3. The script's child process performs the `file.write` to
   `chitin.yaml` — outside chitin's hook surface

Rule count went from 28 → 29 with no gate denial. The
`no-governance-self-modification` rule fires on `action: file.write`
with `target_regex` matching the gov path, but the subprocess's file
writes never produce a chain event that chitin sees, so the deny never
fires.

Full ticket body has the threat model and 5 ranked mitigation options.

## Decision

The ticket recommends **Option 1 (chattr +i) now + Option 4 (signed
policy) Phase 2 + Option 3 (canon pattern-match) as defense in depth**.
Accepting that recommendation with one structural change: **Option 3 is
the immediate code-side defense (this spec), Option 1 is an operator
runbook (companion doc, not chitin code), and Option 4 is filed as a
followup ticket** because it's a separate architecture spec with
crypto + CI integration.

Rationale: Option 1 alone protects the file but nothing else and adds
operator friction without raising the cost of attack outside the
operator-edit window. Option 3 alone is brittle but raises the bar for
ALL gov-paths under the existing rule, with no operator workflow
change. Combined, Option 1 + Option 3 give layered defense today;
Option 4 closes the residual hole durably.

## Scope of this spec

**In scope:**

1. **Option 3 implementation** — extend chitin canon's shell-command
   normalization to detect gov-path writes performed inside script /
   interpreter invocations, and surface them as `file.write` actions
   against the resolved gov path. The
   `no-destructive-rm-via-execute-code` rule is the existing precedent
   (matches `subprocess.run([rm, -rf, ...])` and `shutil.rmtree`
   inside python source); we add a parallel rule for gov-path writes.
2. **Operator runbook** for Option 1 — short doc covering the
   `chattr +i chitin.yaml` workflow + the operator's edit sequence
   (`sudo chattr -i`, edit, `sudo chattr +i`).
3. **Followup-ticket creation** for Option 4 (signed policy).
4. **Test fixtures** for Option 3 — covering the inline (`python3 -c`),
   one-liner (`python3 /tmp/x.py` with detectable script body), and
   passthrough cases.

**Out of scope (deferred to followups):**

- Option 4 (signed policy) — separate spec.
- eBPF / ptrace / fanotify lower-layer hooks (Option 5) — overkill for
  current threat model; chitin is a Claude-Code-tool-call adjudicator,
  not a full sandboxing layer.
- Reading saved-script-files at gate-eval time (e.g., open
  `/tmp/foo.py` and pattern-match its content). Race-prone (script can
  be edited after gate sees it, before subprocess opens it) and
  performance-heavy. The followup-ticket for Option 4 supersedes this
  vector.
- Generalizing beyond gov paths to all protected paths. The rule we
  ship in this spec is specifically for `no-governance-self-modification`
  paths; extending the pattern to `protected-system-path-write` etc.
  is a separate ticket.

## Option 3 — canon pattern-match for inline gov-path mutations

### What we catch

When `shell.exec` invokes an interpreter (`python3`, `python`, `node`,
`bash`, `sh`, `ruby`, `perl`) with **inline code** (`-c`, `-e` flags),
canon must scan the inline content for gov-path write patterns and
re-classify the action as `file.write` against the gov path.

Patterns to detect inside the inline code argument:

| Language | Pattern (regex flavor) |
|---|---|
| python | `open\s*\(\s*['"]([^'"]+chitin\.yaml\|[^'"]*\.chitin/[^'"]*\|[^'"]*\.hermes/plugins/chitin-governance/[^'"]*)['"]` paired with mode `w`/`a`/`+` |
| python | `pathlib\.Path\(\s*['"]<gov-path>['"]\s*\)\.write_text` |
| python | `os\.rename\s*\(\s*[^,]+,\s*['"]<gov-path>['"]` |
| python | `subprocess\.run\(\s*\[[^\]]*['"](mv|cp|tee|>)['"][^\]]*['"]<gov-path>['"]` |
| node | `fs\.writeFile(?:Sync)?\s*\(\s*['"]<gov-path>['"]` |
| node | `fs\.appendFile(?:Sync)?\s*\(\s*['"]<gov-path>['"]` |
| shell (-c) | `(?:>|>>|tee)\s+['"]?<gov-path>['"]?` |
| shell (-c) | `(?:cp|mv|install)\s+[^;|&]*\s<gov-path>(?:\s|$)` |

Where `<gov-path>` is the same regex the existing rule uses:

```
(?:(?:^|/)chitin\.yaml$|(?:^|/)\.chitin/|(?:^|/)\.hermes/plugins/chitin-governance/)
```

If any pattern matches, canon emits the action as
`Action{Type: ActFileWrite, Target: <extracted-path>}` so the existing
`no-governance-self-modification` rule fires and denies.

### Known unmatched cases (documented limitations, not bugs)

- **Saved-script invocation** (`python3 /tmp/foo.py` where `/tmp/foo.py`
  is on disk and contains the write). The command line has no inline
  code; the script file isn't read. This is the vector Option 4
  (signed policy) closes properly. Documented in the suggestion text
  on the rule so operators understand the residual surface.
- **String-concatenation / obfuscation** (`open(chr(99)+chr(104)+...)`).
  Out of scope by precedent — chitin gates intent-discoverable patterns;
  obfuscation defeats every static rule.
- **Base64 / hex-encoded paths**. Same reason.
- **Indirect via `os.environ[...]`** where path is computed at runtime.
  Same.

The rule's suggestion text should say: *"This rule catches inline
gov-path writes from script interpreters. Saved-script bypass requires
operator workflow (chattr +i) + Phase 2 signed-policy work — see
docs/runbooks/governance-self-mod-defense.md."*

### Where the code lives

- Canon command parsing: `go/execution-kernel/internal/canon/normalize.go`
  — has the existing rm-rf-in-subprocess pattern for reference (search
  for `subprocess` or `shutil.rmtree`).
- Gov action types: `go/execution-kernel/internal/gov/action.go` — no
  new types needed; we re-emit `ActFileWrite`.
- Rule: chitin.yaml — add `no-governance-self-modification-via-shell`
  mirroring the existing `no-destructive-rm-via-execute-code` pattern,
  or extend the existing `no-governance-self-modification` rule's
  `action` field to a list `[file.write, shell.exec]` with a
  `target_regex` that matches both file paths AND inline-code patterns.
  Implementer's choice; recommend the new-rule approach for clarity.

```yaml
# Add near no-governance-self-modification.
- id: no-gov-self-mod-via-shell
  action: shell.exec
  effect: deny
  target_regex: |
    (?:python3?\s+-c\s+['"]?[^'"]*open\s*\(\s*['"][^'"]*(?:chitin\.yaml|\.chitin/|\.hermes/plugins/chitin-governance/)|...
  reason: "Inline gov-path mutation in shell-spawned interpreter — see no-governance-self-modification."
  suggestion: "Edit chitin.yaml via the documented operator workflow (chattr -i, edit, chattr +i). Never via agent-spawned subprocess."
```

(Worker should compose the full regex from the patterns table above;
the YAML snippet is illustrative, not final.)

## Operator runbook companion (`docs/runbooks/governance-self-mod-defense.md`)

Short doc — operator's defense workflow:

```bash
# One-time setup (per host)
sudo chattr +i /home/red/workspace/chitin/chitin.yaml
sudo chattr +i -R /home/red/.chitin/
sudo chattr +i -R /home/red/.hermes/plugins/chitin-governance/

# When making a legitimate operator edit
sudo chattr -i /home/red/workspace/chitin/chitin.yaml
$EDITOR /home/red/workspace/chitin/chitin.yaml
sudo chattr +i /home/red/workspace/chitin/chitin.yaml
git -C /home/red/workspace/chitin add chitin.yaml
git -C /home/red/workspace/chitin commit -S -m "policy: <change>"
# branch + PR follows the normal operator-edit flow

# Verify
lsattr /home/red/workspace/chitin/chitin.yaml  # should show -i---i-...
```

Document the WHY (closes the residual saved-script bypass that Option 3
can't catch; survives until Phase 2 signed-policy lands), the residual
gaps (root can override; doesn't generalize to all gov paths
automatically; user `sudo` access is required and is itself a trust
boundary), and the rollback (drop the immutable bit, `chattr -i -R`).

## Followup ticket for Option 4 (signed policy)

After this spec lands, file a kanban ticket:

> **Title:** Phase 2 governance defense: signed-policy verification at
> kernel load
>
> **Body:** Closes the residual saved-script bypass that Option 3
> can't catch. Operator signs `chitin.yaml` with a private key. Kernel
> verifies signature on load; tampered policy fails to load and the
> gate locks (preserves current fail-closed behavior). Needs:
> 1. Signing key management (where lives, rotation policy)
> 2. CI integration so legitimate operator edits produce a valid
>    signature without manual signing every time
> 3. Kernel load-time verification path
> 4. Documented break-glass for emergency operator override (loss
>    of key / urgent edit before CI completes)
>
> Parent: t_0567e90a.

This ticket should be created as part of implementing the current spec
(workflow: cross-reference the followup from the implementation PR).

## Acceptance

1. **Reproduce the original bypass before the fix.** Test fixture
   demonstrates that a `python3 -c "open('chitin.yaml','w')..."`
   shell.exec WAS allowed prior to this change.
2. **Same fixture is denied after the fix** with `rule_id =
   no-gov-self-mod-via-shell` (or the equivalent id the worker picks).
3. **Saved-script case explicitly documented as a known limitation**
   in both the spec and the suggestion text on the new rule. The rule
   does NOT need to deny `python3 /tmp/foo.py` invocations
   (acknowledged out-of-scope; Phase 2 closes it).
4. **Operator runbook landed** at
   `docs/runbooks/governance-self-mod-defense.md` covering the
   `chattr` workflow.
5. **Followup ticket filed** for Option 4 (signed policy). Body
   cross-references this ticket.
6. **No false positives on legitimate edits.** Operator edits to
   `chitin.yaml` via `$EDITOR` (which produces a `file.write` action
   from claude-code's hook, not a shell-spawned interpreter write) are
   still gated by the existing `no-governance-self-modification` rule.
   This spec's rule must not double-fire on those.
7. **Test fixtures cover each language interpreter** in the patterns
   table (python, node, bash one-liner with `>` redirect, etc.).

## Out of scope

- Signed-policy (Option 4) — separate ticket.
- Reading on-disk script files (heavy + race-prone — see Scope above).
- Generalizing the inline-code pattern detection to ALL protected
  paths (`no-env-file-write`, `protected-system-path-write`, etc.). The
  same pattern should extend to those, but it's a separate ticket once
  this lands and the worker confirms the canon-extension approach works.
- Hardening against operator-account compromise (out: chitin assumes
  the operator's local shell is trusted; threat model is rogue *agent*,
  not rogue operator).

## Implementation pointers for the worker

- Read `internal/canon/normalize.go` for the existing
  `no-destructive-rm-via-execute-code` precedent and mirror its
  structure.
- Tests live at `internal/gov/testdata/` and
  `internal/canon/normalize_test.go`. The new rule fixture should land
  next to the existing rm-rf-via-execute-code fixture for symmetry.
- Don't add new ActionTypes; re-emit `ActFileWrite` so the existing
  `no-governance-self-modification` rule does the actual deny work.
  (Alternative: add a new rule id; either is fine — implementer's
  call. Document the choice in the PR.)
- Operator runbook is plain markdown — no code. Aim for ~50 lines.
- Followup-ticket creation: `hermes kanban --board chitin create ...`
  in the implementation PR's body so it's visible without a separate
  step.

## Related

- Operator memory `feedback_no_commit_to_main_policy.md` already
  enforces operator-led gov edits via branch + PR. Current bypass
  undercut that; this spec closes the easy form.
- codex's identity-aware policy work (#418–#424) tightened WHO can do
  governance mutations; this spec closes the HOW (via
  shell-subprocess).
- Companion spec from today: `2026-05-12-no-commit-to-protected-branch.md`
  (PR #547) — same operator-memory-can't-enforce-universally root.
- Architecture ticket: `t_4fcfc0d1` (where this surface was first
  raised; spec'd here as standalone work to keep scope tight).
