# Requirements Checklist — 102 PR Review Workflow Wiring Completion

Design-stage verification. Items marked `[x]` were satisfied at spec authoring; the "Deferred to implementation" section enumerates gates the impl PRs (PR-A + PR-B) must satisfy.

## Empirical grounding

- [x] Spec opens with the exact 4 gaps surfaced during the 2026-05-23 live demo
- [x] Each gap is cited with file + line number (`workflows/hello.go:16-26`, `pr_review.go:118`, etc.)
- [x] Live demo result (cell 14 unclosable) is the spec's surfacing moment
- [x] PR #950 is named as the validation case for SC-001

## Scope discipline

- [x] Strictly v1.0.1 — backfill of spec 094 v1.0's contract, not new features
- [x] Spec 094 v1.1 amendment (class-routed arbiter, review_required col) explicitly deferred
- [x] Spec 093 merge queue auto-trigger explicitly out of scope (this is operator-manual)
- [x] Spec 101 cost-aware reviewer selection orthogonal — composes cleanly
- [x] Spec 099 GitHub-native Copilot orthogonal — different trigger path

## Implementation feasibility

- [x] Wiring fixes (gaps 1-3) are line-edit changes — trivial
- [x] Gap 4 (CapturePRSnapshot) is real impl but bounded — gh CLI + JSON parse + struct populate
- [x] Two-PR split (PR-A wiring, PR-B impl) keeps each under bounds gate
- [x] PR-A can ship without PR-B (degrades gracefully with snapshot stub)
- [x] PR-B closes cell 14 of the system-state matrix

## Determinism + replay safety

- [x] CapturePRSnapshot is deterministic per (repo, pr_number, head_sha) — SC-003 invariant
- [x] SnapshotHashRef is the audit anchor (FR-032 from spec 094) — content-bearing fields only
- [x] Workflow + activity registration is idempotent on restart — SC-004

## Composition with existing specs

- [x] Spec 094: extends, doesn't amend (v1.0.1 is a hygiene release)
- [x] Spec 075: reads from driver registry, adds CapCodeReview to openclaw (and audits gemini/hermes)
- [x] Spec 097: reuses Temporal-host + driver-registry plumbing from `schedule` subcommand
- [x] Spec 101: orthogonal — 102 wires the workflow; 101 wires cost-aware selection inside it

## Constitution

- [x] §1 kernel-only chain writer: preserved (pr_review_completed / pr_review_failed via existing emit)
- [x] §6 swarm tooling exception: code lives under `go/orchestrator/`
- [x] §7 swarm is the orchestrator: load-bearing — completes the orchestrator's PR-review surface

## Deferred to implementation

### PR-A (wiring)

1. **openclaw CapCodeReview justification text:** comment explaining why GLM 5.1 is the right second primary (frontier model, paid sub, no Anthropic credit burn).
2. **Workflow Register reorg:** decide whether to leave Register in `hello.go` (current home) or extract to `register.go`. If extracting, also move HelloWorkflow + SequenceWorkflow registration to keep the function symmetric.
3. **review.RegisterActivities signature:** `(w worker.Worker, deps ReviewActivityDeps)`. `ReviewActivityDeps` struct shape: at minimum `Registry *driver.Registry, GitHubClient GitHubClient`. Verify all 4 review activities' constructor dependencies.
4. **review-pr CLI duplication:** the 2026-05-23 session built a local `review_pr.go` (`/tmp/chitin-demo-dispatch/.../cmd/chitin-orchestrator/review_pr.go`). PR-A should copy it into the chitin repo with minor cleanup (e.g., the `dialTemporal` helper reuse from spec 097's `client.go`).
5. **PR-A integration tests:** stub `CapturePRSnapshot` registration; assert the workflow can start, select reviewers, halt-on-snapshot. Doesn't need the real snapshot impl.

### PR-B (CapturePRSnapshot impl)

6. **gh CLI invocation vs go-github SDK:** decide. gh CLI is simpler (already in PATH for operator), but go-github SDK is more testable. Recommendation: gh CLI for v1.0.1 to minimize new deps; revisit if rate-limit / retry needs harden.
7. **PRSnapshot.Files field shape:** verify what `types.go` declares; populate from `gh pr view --json files` output. Diff content NOT included in v1.0.1 (just metadata: path, additions, deletions, status) — full diff capture is a v1.1 extension.
8. **SpecArtifacts extraction:** scan `gh pr view --json files` for paths matching `.specify/specs/NNN-name/{spec,plan,tasks}.md`. For each, fetch content via `gh api /repos/owner/repo/contents/<path>?ref=<head_sha>` or equivalent. Empty array if no spec artifacts.
9. **Rate-limit handling:** Temporal retry with backoff. GitHub's REST API has 5000-req/hr unauthenticated, 5000/hr authenticated with PAT. Should be plenty for review workflows.
10. **Operator runbook `docs/operator/review-pr.md`:** when to use it, what dialectic does, how to read the chain event output, troubleshooting (no reviewers, snapshot failed, workflow stuck).
11. **End-to-end test recipe:** spec the exact steps for SC-001 demo against PR #950 so a future operator can re-run it.
12. **pr_review_completed chain event schema:** payload shape, every field.
