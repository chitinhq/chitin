# Contract: operator-arbiter surface (GitHub PR comment)

**Spec reference**: FR-017, FR-018, FR-024, FR-025 | **Research**: R-OPSURF

## Choice

Operator-as-arbiter verdicts are collected via a structured GitHub PR comment, parsed by the orchestrator. (See R-OPSURF for the decision rationale and rejected alternatives.)

## Flow

```
1. Workflow reaches arbiter dispatch.
2. dispatch_operator_arbiter activity posts the prompt comment (template below).
3. Same activity emits Discord notify (via spec 080 notifier) linking to the comment.
4. Activity long-polls PR comments for a reply from the operator's GitHub identity.
5. Heartbeat every 30s (Temporal activity heartbeat).
6. At the 2h workflow boundary, the workflow re-emits the Discord notify (FR-025).
7. On comment-receipt: parse YAML, validate against FR-014, return StructuredVerdict.
   - Parse failure: post a follow-up comment with the parse error and the corrected template.
     The activity continues to poll.
8. Once a valid verdict is parsed, the activity returns and the workflow's arbiter dispatch closes.
```

The operator may respond from any device with GitHub access — desktop, mobile, web UI, or `gh pr comment --body-file ...`.

## Prompt comment template (posted by the orchestrator)

```markdown
## Operator arbiter request

The primary reviewers disagreed on this PR. The dialectic gate needs your verdict.

| Primary | Driver | Verdict | Summary |
|---|---|---|---|
| 1 | `hermes` | `approve-with-comments` | (top concern from `concerns[0]`) |
| 2 | `openclaw` | `request-changes` | (top blocker from `blockers[0]`) |

**Spec context**: This PR is classified as `<policy-class>`. Per policy, the operator is the arbiter.

**Full primary verdicts** are in the workflow history; query via:
```
chitin-orchestrator merge-queue inspect <workflow-id>
```

**To submit your verdict**: reply to this comment with a fenced YAML block exactly matching the schema below. The orchestrator listens for the first comment authored by your GitHub identity whose body contains a fenced YAML block under this template's header.

```yaml
# operator-arbiter-verdict
verdict: approve | approve-with-comments | request-changes | abstain
concerns:
  - "..."
recommendations:
  - "..."
blockers:
  - "..."
reason: "...(only for abstain)..."
```

Notes:
- `verdict` must be exactly one of the four enum values.
- `approve` → blockers must be empty
- `approve-with-comments` → blockers empty AND at least one of concerns/recommendations non-empty
- `request-changes` → blockers must be non-empty
- `abstain` → all three lists must be empty (reason may be set)

The verdict is captured in workflow history exactly as parsed; it cannot be retroactively edited (per FR-034).
```

## Operator reply template (what the operator posts)

````markdown
```yaml
# operator-arbiter-verdict
verdict: request-changes
concerns: []
recommendations:
  - "Consider splitting the rebase logic and the merge logic into two activities for testability."
blockers:
  - "The new force-push activity does not use --force-with-lease as the spec mandates."
```
````

## Parser semantics

The orchestrator's poller looks for new comments on the PR authored by the operator's GitHub login (configured in the orchestrator's environment, e.g. `OPERATOR_GH_LOGIN=jpleva91`). For each new comment from that author received after the prompt was posted:

1. Extract the first fenced YAML block.
2. Verify the first line of the YAML body is the literal `# operator-arbiter-verdict` marker (so casual comments are ignored).
3. Parse the YAML into the `StructuredVerdict` shape.
4. Validate via `verdict.Validate(...)` per FR-014.
5. If valid, return.
6. If invalid, post a follow-up comment with the parse/validation error and continue polling.

The marker line `# operator-arbiter-verdict` is what distinguishes a verdict from any other YAML the operator might paste in a comment. The operator MAY paste additional discussion before or after the fenced YAML block — only the block matters for the parse.

## Polling cadence

The activity polls `gh api repos/{owner}/{repo}/issues/{pr}/comments` every 30 seconds (matching the activity-heartbeat cadence). Each poll uses the `since=<last-checked-iso8601>` query parameter so only new comments are returned.

## Failure modes

| Operator behaviour | Orchestrator-side treatment |
|---|---|
| Operator posts a malformed YAML (parse error) | Follow-up comment with diagnostic; polling continues. |
| Operator posts a YAML missing the marker line | Comment ignored (treated as casual discussion). |
| Operator posts a verdict with `verdict=approve` and non-empty blockers | Follow-up comment with FR-014 invariant error; polling continues. |
| Operator never responds | Workflow waits indefinitely (FR-027); Discord re-notify at the 2h boundary (FR-029). |
| Operator's GitHub identity is not configured in the orchestrator | The activity fails at start with `config_error: OPERATOR_GH_LOGIN unset`; the parent workflow halts with a clear reason. |

## What the operator does NOT need to do

- Operator does NOT need to use `gh pr review` or the GitHub native "request changes" / "approve" buttons. The native review states are NOT the audit substrate; the comment YAML is.
- Operator does NOT need to use a special CLI or web UI. Any GitHub-comment-capable interface (web, mobile, CLI) works.

## Re-notification heartbeat (FR-025, FR-029)

The dispatch activity does not directly control re-notification (it just heartbeats). The workflow has a parallel goroutine — `workflow.NewTimer(2h)` — that fires the Discord notify activity again at the 2h boundary. The notify activity records `notification.kind = "operator-arbiter.re-notify"` in telemetry so an external observer can distinguish initial dispatch from re-notification.

## Closure

Once a valid verdict is received, the activity:

1. Records the `ReviewerInvocation` outcome (DriverID = "operator", Role = "arbiter").
2. Emits the per-invocation telemetry event per FR-032.
3. Posts a final acknowledgement comment ("✅ operator verdict recorded: `<verdict>`") so the operator has confirmation outside Discord.
4. Returns the `StructuredVerdict` to the workflow.
