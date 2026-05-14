# Chitin Policy DSL v3

This document is the authoritative language reference for the v3 policy DSL implemented by `go/execution-kernel/internal/gov`.

Source of truth:

- Parser and evaluator: `go/execution-kernel/internal/gov/policy.go`
- Inheritance: `go/execution-kernel/internal/gov/inherit.go`
- Bounds: `go/execution-kernel/internal/gov/bounds.go`
- Severity ladder and lockdown: `go/execution-kernel/internal/gov/gate.go`, `go/execution-kernel/internal/gov/escalation.go`
- Canonical action vocabulary: `go/execution-kernel/internal/gov/action.go`
- Behavior tests: `go/execution-kernel/internal/gov/*_test.go`

This spec describes what the engine currently supports, not older v2 syntax and not planned features.

## 1. Evaluation Model

`chitin.yaml` is loaded from the current directory upward. Parent files merge into child files, with child rules overriding parent rules by `id`.

At evaluation time, the gate applies this sequence:

1. Lockdown short-circuit.
2. Policy evaluation.
3. Bounds evaluation for `git.push` and `github.pr.create` when policy allowed the action.
4. Monitor-mode override: a deny with effective `mode: monitor` becomes an allow-with-audit-row.
5. Budget-envelope checks, if an envelope is present.
6. Escalation counter update on deny.

Rule matching is deny-first and order-independent across effects:

1. All `deny` rules are scanned top-to-bottom; first matching deny wins.
2. If no deny matched, all `allow` rules are scanned top-to-bottom; first matching allow wins.
3. If no allow matched, the result is `default-deny`.

Only `allow` and `deny` participate in rule evaluation in v3. `effect: escalate` is rejected at load time. Do not rely on `effect: guide` or `effect: monitor`; they are not active rule effects in the current engine.

## 2. Grammar

The YAML surface accepted by the v3 engine can be expressed as:

```ebnf
policy              = mapping ;

mapping             = id
                      [ name ]
                      [ mode ]
                      [ pack ]
                      [ invariantModes ]
                      [ bounds ]
                      [ escalation ]
                      [ authorityConfig ]
                      rules ;

id                  = "id:" string ;
name                = "name:" string ;
mode                = "mode:" ( "monitor" | "guide" | "enforce" ) ;
pack                = "pack:" string ;

invariantModes      = "invariantModes:" newline { invariantModeEntry } ;
invariantModeEntry  = indent ruleID ":" modeValue newline ;
modeValue           = "monitor" | "guide" | "enforce" ;

bounds              = "bounds:" newline
                      [ indent "max_files_changed:" integer newline ]
                      [ indent "max_lines_changed:" integer newline ]
                      [ perActionBounds ] ;

perActionBounds     = indent "per_action:" newline { perActionEntry } ;
perActionEntry      = indent indent actionType ":" newline
                      [ indent indent indent "max_files_changed:" integer newline ]
                      [ indent indent indent "max_lines_changed:" integer newline ] ;

escalation          = "escalation:" newline
                      [ indent "elevated_threshold:" integer newline ]
                      [ indent "high_threshold:" integer newline ]
                      [ indent "lockdown_threshold:" integer newline ]
                      [ indent "max_retries_per_action:" integer newline ]
                      [ indent "deny_cascade_count:" integer newline ]
                      [ indent "deny_cascade_window_seconds:" integer newline ] ;

authorityConfig     = "authority:" newline
                      [ indent "trusted:" newline { trustedAuthority } ] ;

trustedAuthority    = indent "-" newline
                      indent indent "authority:" authorityValue newline
                      [ selectorFields ] ;
authorityValue      = "worker" | "supervisor" | "operator" | "system" ;

selectorFields      = { stableSelector | advisorySelector } ;
stableSelector      = agentInstanceID | agentFingerprint | workflowID ;
advisorySelector    = driver | model | role ;

rules               = "rules:" newline { rule } ;
rule                = indent "-" newline
                      indent indent "id:" string newline
                      indent indent "action:" actionMatcher newline
                      indent indent "effect:" effect newline
                      [ indent indent "target:" string newline ]
                      [ indent indent "target_regex:" string newline ]
                      [ params ]
                      [ branches ]
                      [ pathUnder ]
                      [ identityFields ]
                      [ indent indent "reason:" string newline ]
                      [ indent indent "suggestion:" string newline ]
                      [ indent indent "correctedCommand:" string newline ]
                      [ indent indent "escalation_weight:" integer newline ] ;

effect              = "allow" | "deny" ;
actionMatcher       = actionType | "[" actionType { "," actionType } "]" ;
params              = indent indent "params:" newline { indent indent indent string ":" string newline } ;
branches            = indent indent "branches:" yamlStringList newline ;
pathUnder           = indent indent "path_under:" yamlStringList newline ;

identityFields      = { identityField } ;
identityField       = agentInstanceID
                    | agentFingerprint
                    | driver
                    | model
                    | role
                    | stationPromptHash
                    | skillsToolsHash
                    | soulLens
                    | ruleAuthority
                    | workflowID ;

agentInstanceID     = keyValueOrList("agent_instance_id") ;
agentFingerprint    = keyValueOrList("agent_fingerprint") ;
driver              = keyValueOrList("driver") ;
model               = keyValueOrList("model") ;
role                = keyValueOrList("role") ;
stationPromptHash   = keyValueOrList("station_prompt_hash") ;
skillsToolsHash     = keyValueOrList("skills_tools_hash") ;
soulLens            = keyValueOrList("soul_lens") ;
ruleAuthority       = keyValueOrList("authority") ;
workflowID          = keyValueOrList("workflow_id") ;

keyValueOrList(k)   = k ":" string | k ":" yamlStringList ;
yamlStringList      = "[" string { "," string } "]" | newline { indent "-" string newline } ;
```

Notes:

- `action` accepts either one string or a non-empty list of strings.
- Identity selectors accept either one string or a non-empty list of exact strings.
- `target_regex` must compile as a Go regular expression at load time.
- Unknown YAML keys are currently ignored by the parser. Do not rely on undocumented keys.

## 3. Top-Level Keys

### 3.1 `id`, `name`, `mode`, `pack`

- `id` is the policy identifier.
- `name` is optional metadata.
- `mode` defaults to `guide` when omitted.
- Valid `mode` values are `monitor`, `guide`, and `enforce`.
- `pack` is parsed but not interpreted by the evaluator.

Example:

```yaml
id: chitin-governance-baseline
name: Chitin v3 baseline governance
mode: enforce
pack: workstation
rules:
  - id: allow-reads
    action: file.read
    effect: allow
    reason: "Reads are safe"
```

### 3.2 `invariantModes`

`invariantModes` overrides the decision mode for a specific rule id. This applies to ordinary rules and to bounds rule ids such as `bounds:max_files_changed`.

Example:

```yaml
id: soft-kill-bounds
mode: enforce
invariantModes:
  no-rm-recursive: guide
  "bounds:max_lines_changed": monitor
rules:
  - id: no-rm-recursive
    action: file.recursive_delete
    effect: deny
    reason: "Recursive delete is blocked"
```

Semantics:

- If a deny rule resolves to `mode: monitor`, the gate flips the final decision to allow while still logging the rule hit.
- If no override exists, the policy-wide `mode` applies.
- Bounds default to `enforce` unless overridden in `invariantModes`.

### 3.3 `bounds`

`bounds` defines blast-radius ceilings for push-shaped actions only:

- `git.push`
- `github.pr.create`

Top-level ceilings are defaults. `per_action` overrides merge by action type, and zero values fall back to the top-level default.

Example:

```yaml
bounds:
  max_files_changed: 25
  max_lines_changed: 500
  per_action:
    git.push:
      max_files_changed: 200
      max_lines_changed: 5000
    github.pr.create:
      max_files_changed: 100
      max_lines_changed: 2000
```

Semantics:

- Bounds are checked only after policy allowed the action.
- `git.push` is measured against `git diff --stat origin/main...HEAD`.
- `github.pr.create` is measured against the PR merge-base diff derived from `gh pr create --base/--head`.
- If diff stats cannot be computed or parsed, the engine fails closed with `rule_id: bounds:undetermined`.
- A ceiling of `0` means unset.
- `max_runtime_seconds` is not part of the v3 DSL and is ignored if present.

### 3.4 `escalation`

`escalation` configures defaults loaded into the policy:

```yaml
escalation:
  elevated_threshold: 3
  high_threshold: 7
  lockdown_threshold: 10
  max_retries_per_action: 3
  deny_cascade_count: 4
  deny_cascade_window_seconds: 300
```

Current v3 behavior:

- The severity ladder labels are `normal`, `elevated`, `high`, and `lockdown`.
- The persistent counter currently uses hard-coded thresholds of 3, 7, and 10 for `elevated`, `high`, and `lockdown`.
- `deny_cascade_count` and `deny_cascade_window_seconds` are used for shell-denial cascade lockdown detection.
- `max_retries_per_action` is parsed and inherited but is not currently consulted by the gate logic.

### 3.5 `authority`

`authority.trusted` maps verified identities to effective authorities. Each grant must include at least one stable selector:

- `agent_instance_id`
- `agent_fingerprint`
- `workflow_id`

`driver`, `model`, and `role` may refine a grant but cannot be the only selectors.

Example:

```yaml
authority:
  trusted:
    - authority: supervisor
      agent_fingerprint: fp-supervisor
      driver: hermes
      role: reviewer
```

Valid authorities are:

- `worker`
- `supervisor`
- `operator`
- `system`

## 4. Rules

Each rule is a conjunction: every populated field must match.

Example:

```yaml
- id: protected-system-path-write
  action: [file.write, file.delete]
  effect: deny
  path_under:
    - "/etc/"
    - "/boot/"
  reason: "System paths cannot be modified by agents"
  suggestion: "Use the operator shell for legitimate system changes"
  escalation_weight: 2
```

### 4.1 Required fields

- `id`
- `action`
- `effect`

Supported active effects:

- `allow`
- `deny`

Rejected effect:

- `escalate`

Do not author `guide` or `monitor` as rule effects. Those are decision modes, controlled by policy `mode` and `invariantModes`.

### 4.2 `action`

`action` matches canonical action types, not raw tool names. The canonical vocabulary is:

- `shell.exec`
- `file.read`
- `file.write`
- `file.delete`
- `file.move`
- `file.recursive_delete`
- `git.diff`
- `git.log`
- `git.status`
- `git.commit`
- `git.checkout`
- `git.branch.create`
- `git.branch.delete`
- `git.merge`
- `git.push`
- `git.force-push`
- `git.worktree.list`
- `git.worktree.add`
- `git.worktree.remove`
- `github.pr.create`
- `github.pr.view`
- `github.pr.list`
- `github.pr.merge`
- `github.pr.close`
- `github.issue.list`
- `github.issue.view`
- `github.issue.create`
- `github.issue.close`
- `github.api`
- `delegate.task`
- `http.request`
- `npm.install`
- `npm.script.run`
- `test.run`
- `mcp.call`
- `memory.access`
- `tool.custom`
- `hook.invoke`
- `kanban.call`
- `hermes.process`
- `infra.destroy`
- `unknown`

Example:

```yaml
- id: default-allow-git-read
  action: [git.diff, git.log, git.status, git.worktree.list]
  effect: allow
  reason: "Git read operations are safe"
```

### 4.3 `target`

`target` is a case-sensitive substring match against `Action.Target`.

Example:

```yaml
- id: no-env-file-write
  action: file.write
  effect: deny
  target: ".env"
  reason: "Secrets files must not be modified"
```

### 4.4 `target_regex`

`target_regex` is a Go regular expression matched against `Action.Target`. Invalid regexes are load-time errors.

Example:

```yaml
- id: hermes-no-frontier-spawn
  action: shell.exec
  effect: deny
  driver: hermes
  target_regex: '(?:^|[;&|]\s*|\s)(?:[\w./-]+/)?(?:codex|claude|gemini)(?:\s|$)'
  reason: "Hermes does not dispatch frontier coders directly"
```

### 4.5 `params`

`params` requires exact string equality on normalized action params. Each configured key must exist and stringify to the configured value.

Example:

```yaml
- id: no-gov-self-mod-via-shell
  action: file.write
  effect: deny
  params:
    via: inline-interpreter
  target_regex: '(?:(?:^|/)chitin\.yaml$|(?:^|/)\.chitin/)'
  reason: "Inline governance mutation is blocked"
```

### 4.6 `branches`

`branches` applies to branch-shaped targets.

Semantics:

- For `git.push`, `Action.Target` must equal one of the listed branch names. The normalizer emits `"<HEAD-implicit>"` for bare pushes such as `git push` or `git push origin`, so policies may match that sentinel explicitly.
- For `git.commit`, the engine additionally supports the sentinel `"<HEAD-implicit>"`.
- If `"<HEAD-implicit>"` is present for `git.commit`, the gate resolves the current branch from `Action.Path`.
- If current-branch resolution fails, the rule matches fail-closed.

Example:

```yaml
- id: no-protected-push
  action: git.push
  effect: deny
  branches: [main, master, "<HEAD-implicit>"]
  reason: "Protected branches cannot be pushed directly"
```

### 4.7 `path_under`

`path_under` is a literal prefix test on `Action.Target`.

Example:

```yaml
- id: protected-system-path-write
  action: [file.write, file.delete]
  effect: deny
  path_under:
    - "/etc/"
    - "/System/"
  reason: "System paths cannot be modified"
```

### 4.8 Identity selectors

These fields constrain a rule to specific dispatch identity dimensions:

- `agent_instance_id`
- `agent_fingerprint`
- `driver`
- `model`
- `role`
- `station_prompt_hash`
- `skills_tools_hash`
- `soul_lens`
- `authority`
- `workflow_id`

Each accepts either a string or a non-empty list of strings, and matches by exact equality.

Example:

```yaml
- id: reviewer-shell-deny
  action: shell.exec
  effect: deny
  role: reviewer
  reason: "Reviewers cannot run shell commands"
```

Important authority semantics:

- Rule `authority` matches the effective trusted authority, not merely the claimed authority from the environment.
- If no trusted grant matches but identity context exists, effective authority falls back to `worker`.
- A fully untagged external call may resolve to `external`.

### 4.9 `reason`, `suggestion`, `correctedCommand`

These strings are copied into the resulting decision when the rule fires.

Example:

```yaml
- id: no-rm-recursive
  action: file.recursive_delete
  effect: deny
  reason: "Recursive delete is blocked"
  suggestion: "Use git rm <specific-files> or a targeted rm"
  correctedCommand: "git rm <specific-files>"
```

### 4.10 `escalation_weight`

`escalation_weight` defaults to `1`. On deny, the gate increments the persistent denial counter by this weight.

Example:

```yaml
- id: no-governance-self-modification
  action: file.write
  effect: deny
  target_regex: '(?:(?:^|/)chitin\.yaml$|(?:^|/)\.chitin/)'
  reason: "Agents may not modify their own governance"
  escalation_weight: 2
```

## 5. Per-Driver Policy

The DSL does not have a top-level per-driver block. Per-driver behavior is expressed by ordinary rules using the `driver` identity selector.

Example:

```yaml
- id: hermes-no-frontier-spawn
  action: shell.exec
  effect: deny
  driver: hermes
  target_regex: '(?:^|[;&|]\s*|\s)(?:[\w./-]+/)?(?:codex|claude|gemini)(?:\s|$)'
  reason: "Hermes must dispatch through clawta/OpenClaw"

- id: default-allow-shell
  action: shell.exec
  effect: allow
  reason: "Generic shell allowed unless a deny rule matches first"
```

This means:

- The deny applies only when `FingerprintContext.Driver == "hermes"`.
- The same action from `codex`, `claude-code`, or another driver falls through to later matching rules.

The same selector mechanism works for `model`, `role`, `workflow_id`, and other identity fields.

## 6. Inheritance

When multiple `chitin.yaml` files are found between the current directory and filesystem root:

- Outermost policy loads first.
- Innermost policy loads last.
- Child rules override parent rules by identical `id`.
- Child `bounds.per_action` entries merge additively and override parent entries on collision.
- Child `invariantModes` override parent entries on collision.
- Child `authority.trusted` grants append to parent grants.
- A child may not weaken the parent `mode`.

Strictness order is:

- `monitor` < `guide` < `enforce`

Example:

Parent:

```yaml
id: strict-parent
mode: enforce
rules:
  - id: no-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "blocked"
```

Child:

```yaml
id: team-policy
mode: enforce
rules:
  - id: allow-reads
    action: file.read
    effect: allow
    reason: "Reads are safe"
```

Invalid child:

```yaml
id: child-loose
mode: monitor
rules:
  - id: allow-reads
    action: file.read
    effect: allow
    reason: "Reads are safe"
```

The invalid child fails to load because `monitor` weakens a parent `enforce`.

## 7. Default Deny

The policy DSL is fail-closed.

If no `allow` rule matches after deny rules are evaluated, the engine returns:

- `allowed: false`
- `rule_id: default-deny`
- `reason: no matching allow rule; policy default is deny`

Example:

```yaml
id: minimal
mode: guide
rules:
  - id: allow-reads
    action: file.read
    effect: allow
    reason: "Reads are safe"
```

Under this policy, `file.read` is allowed and `shell.exec` is denied by default.

## 8. Load-Time Validation

The loader rejects these configuration errors:

- invalid policy `mode`
- invalid `target_regex`
- empty entries in `action`
- empty entries in `branches`
- empty entries in `path_under`
- empty keys or values in `params`
- empty entries in identity matchers
- empty identity matcher lists
- `authority.trusted` grants with no stable selector
- invalid authority names
- `effect: escalate`

Examples of invalid v2-era or unsupported constructs:

```yaml
rules:
  - id: old-escalate
    action: shell.exec
    effect: escalate
```

```yaml
bounds:
  max_runtime_seconds: 900
```

`effect: escalate` is rejected. `max_runtime_seconds` is ignored and is not part of the v3 DSL.

## 9. Minimal Complete Example

```yaml
id: chitin-governance-baseline
name: Chitin v3 baseline governance
mode: enforce

invariantModes:
  no-rm-recursive: guide
  "bounds:max_lines_changed": monitor

bounds:
  max_files_changed: 25
  max_lines_changed: 500
  per_action:
    git.push:
      max_files_changed: 200
      max_lines_changed: 5000

escalation:
  elevated_threshold: 3
  high_threshold: 7
  lockdown_threshold: 10
  max_retries_per_action: 3
  deny_cascade_count: 4
  deny_cascade_window_seconds: 300

authority:
  trusted:
    - authority: supervisor
      agent_fingerprint: fp-supervisor
      driver: hermes

rules:
  - id: no-rm-recursive
    action: file.recursive_delete
    effect: deny
    reason: "Recursive delete is blocked"
    suggestion: "Use targeted file operations"
    correctedCommand: "git rm <specific-files>"

  - id: hermes-no-frontier-spawn
    action: shell.exec
    effect: deny
    driver: hermes
    target_regex: '(?:^|[;&|]\s*|\s)(?:[\w./-]+/)?(?:codex|claude|gemini)(?:\s|$)'
    reason: "Hermes must dispatch through clawta"

  - id: no-protected-push
    action: git.push
    effect: deny
    branches: [main, master, "<HEAD-implicit>"]
    reason: "Protected branches cannot be pushed directly"

  - id: protected-system-path-write
    action: [file.write, file.delete]
    effect: deny
    path_under:
      - "/etc/"
      - "/System/"
    reason: "System paths cannot be modified"
    escalation_weight: 2

  - id: default-allow-reads
    action: file.read
    effect: allow
    reason: "Reads are safe"

  - id: default-allow-shell
    action: shell.exec
    effect: allow
    reason: "Shell commands are allowed unless denied above"
```
