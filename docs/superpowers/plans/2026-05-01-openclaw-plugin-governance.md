# Slice 2 — Chitin as an OpenClaw Plugin

**Date:** 2026-05-01
**Spec:** [`2026-05-01-chitin-as-openclaw-plugin-design.md`](../specs/2026-05-01-chitin-as-openclaw-plugin-design.md)
**Branch:** `feat/openclaw-plugin-governance`
**Worktree:** `/home/red/workspace/chitin-openclaw-plugin` (new — create per `feedback_always_work_in_worktree.md`)
**Forcing function:** none. Slice lands *after* the 2026-05-07 talk so the plugin can be the forward-line beat on stage. Aim for first PR merged within 10 days post-talk (≤ 2026-05-17).
**Replaces:** slice 1e (ACP-server mode for Copilot shim) — *only* for openclaw native pi-runtime drivers. Slice 1e remains valid for closed-vendor / acpx-subprocess drivers (Copilot CLI v1).

## Task breakdown

| # | Task | Files | Ship-blocking? |
|---|------|-------|----------------|
| 1 | Scaffold `apps/openclaw-plugin-governance/` — `package.json` (private:false, type:module, openclaw peer dep), `tsconfig.json`, `.gitignore` | `apps/openclaw-plugin-governance/{package.json,tsconfig.json,.gitignore}` | yes |
| 2 | Author `openclaw.plugin.json` — id, name, description (leads with category noun), configSchema (kernelPath, mode, workerMode, otelEmit, denyOnError), uiHints, configContracts.dangerousFlags (workerMode:false flagged when kernelPath unset) | `apps/openclaw-plugin-governance/openclaw.plugin.json` | yes |
| 3 | Implement `chitin-bridge.ts` — subprocess invoker for `chitin-kernel gate evaluate --hook-stdin --agent=openclaw-plugin`. Stdin JSON in, stdout JSON out, exit code mapping (0=allow, non-zero=deny / kernel-error per `denyOnError`). Cache resolved binary path; surface stderr to plugin logger. | `apps/openclaw-plugin-governance/src/chitin-bridge.ts` | yes |
| 4 | Implement `chitin-emit.ts` — analogous bridge for non-gate emit calls (session_start, session_end, subagent_ended). Posts an event row to chitin via a separate `chitin-kernel emit` invocation (or batch endpoint if one exists; check kernel before assuming). | `apps/openclaw-plugin-governance/src/chitin-emit.ts` | yes |
| 5 | Verify `caller_origin: 'openclaw-plugin'` support is on `main` in the kernel (PR #79 landed `caller_origin`; check the enum accepts the new string). Add the value if missing — this is a kernel-side change, not a plugin-side one. | `go/execution-kernel/internal/gov/*.go` (likely no-op if already enum-open) | yes |
| 6 | Implement `index.ts` — `definePluginEntry({...register})` skeleton. No hooks wired yet; just confirms `openclaw plugin install <local>` registers without errors. | `apps/openclaw-plugin-governance/src/index.ts` | yes |
| 7 | Wire `before_tool_call` hook → `chitin-bridge.evaluateGate(...)`. Allow / deny / params-rewrite paths. Fail-closed on bridge error per `denyOnError` config. | `index.ts` (handler), `chitin-bridge.ts` (call) | yes |
| 8 | Wire `subagent_spawning` hook → kernel gate. Verify worker-mode + `agentId === 'claude-code'` denial path emits `{ status: 'error', error: '...ToS...' }`. | `index.ts` | yes |
| 9 | Wire `before_install` hook → kernel gate. Verify worker-mode + `request.kind === 'plugin-git'` denial path returns `{ block: true, blockReason: ... }`. | `index.ts` | yes |
| 10 | Wire `registerAgentToolResultMiddleware` (NOT a `.on` hook — separate registry method) → emit post_tool_use chain row tagged with `runtime: 'pi'` or `'codex'`. Does not mutate result. | `index.ts` (middleware), `chitin-emit.ts` (call) | yes |
| 11 | Wire `session_start` / `session_end` / `subagent_ended` lifecycle emits. Fire-and-forget. | `index.ts` | yes |
| 12 | Test 1 — `chitin-bridge.test.ts`: stdin/stdout protocol unit tests with mocked subprocess (allow, deny, kernel-missing, kernel-timeout, malformed-stdout) | `apps/openclaw-plugin-governance/test/chitin-bridge.test.ts` | yes |
| 13 | Test 2 — `hooks.test.ts`: each hook handler tested with mocked bridge. before_tool_call (allow / deny / params-rewrite), subagent_spawning (claude-code denial), before_install (git-kind denial), middleware (chain-row write). | `apps/openclaw-plugin-governance/test/hooks.test.ts` | yes |
| 14 | Test 3 — integration: real `chitin-kernel` subprocess (built from monorepo), real bridge, mocked openclaw runtime. Verify chain row written with `caller_origin: 'openclaw-plugin'` and `decision: 'deny'`. | `apps/openclaw-plugin-governance/test/integration.test.ts` | yes |
| 15 | Test 4 — end-to-end (THE headline test): real openclaw + real ollama `qwen3-coder:30b` + real chitin-kernel + plugin installed → agent prompted to `rm -rf /tmp/chitin-test` → `before_tool_call` → kernel denies via `worker:no-recursive-delete` → openclaw surfaces deny back → chain row written. | `apps/openclaw-plugin-governance/test/e2e/rm-rf-denial.test.ts` | yes |
| 16 | Regression — confirm standalone `chitin-kernel` CLI still works (run existing `go test ./...`). | n/a (test execution) | yes |
| 17 | Regression — confirm PR #51 (Copilot shim) and PR #66 (Claude Code hook) integration tests still green. | n/a (test execution) | yes |
| 18 | Layer Contracts compliance audit (manual walk through `docs/architecture/layer-contracts.md` v1: kernel authority / driver constraint / routing scope / aggregation role). Note in PR description. | n/a (PR description) | yes |
| 19 | README for marketplace adoption — written for someone arriving from openclaw who has never read a chitin doc. Leads with category noun ("execution kernel for AI coding agents"). One-paragraph install / config / value-prop, then config-reference, then "what this plugin does NOT cover" (the acpx-subprocess scope catch from spec §1.5). | `apps/openclaw-plugin-governance/README.md` | yes (marketplace-grade) |
| 20 | NPM publish workflow — GitHub Actions job to `npm publish` from `apps/openclaw-plugin-governance/` on tagged release. Coordinate package name (`@chitinhq/openclaw-plugin-governance` likely) with org. | `.github/workflows/publish-openclaw-plugin.yml`, `apps/openclaw-plugin-governance/package.json` | yes (distribution) |
| 21 | Reach out to Steinberger (openclaw maintainer) — does chitin-governance qualify for the `embeddedExtensionFactories` allowlist (codex-app-server seam, spec §7.4)? Non-blocking; informs slice-3 scope. | n/a (communication) | post-merge |
| 22 | Update memory `project_chitin_as_openclaw_plugin.md` — strip "active investigation" framing, mark as "shipped slice-2", point at PR. Update `MEMORY.md` index. | `~/.claude/projects/.../memory/{project_chitin_as_openclaw_plugin.md,MEMORY.md}` | post-merge |
| 23 | Update `MEMORY.md` entry for `project_two_driver_pattern.md` to note the third pattern (openclaw-native plugin gating) — keep the two-driver memory accurate for vendors, add cross-link. | `~/.claude/projects/.../memory/project_two_driver_pattern.md` | post-merge |

## Order of operations

```
1 → 2 → 5  (foundation — runs in parallel after 1)
        ↓
        3 → 4  (bridges; 4 depends on 3's protocol decisions)
            ↓
            6  (skeleton plugin loads)
            ↓
            7 → 8 → 9  (gating hooks one at a time, each verified before next)
                  ↓
                  10 → 11  (telemetry hooks; non-blocking on flow)
                       ↓
                       12 / 13  (continuous — start at 7, refine through 11)
                            ↓
                            14  (real-kernel integration)
                                ↓
                                15  (e2e — the headline acceptance)
                                    ↓
                                    16 → 17  (regression)
                                         ↓
                                         18  (compliance audit)
                                              ↓
                                              19 → 20  (marketplace shape)
                                                   ↓
                                                   PR opens
                                                   ↓
                                                   21 → 22 → 23  (post-merge)
```

Tasks 12–13 are **continuous** — write tests as each hook lands, not after.

## Validation gates

- **Knuth gate (boundary correctness):** before each hook handler is written, name the boundary cases — empty `params`, missing `toolCallId`, kernel binary missing, kernel timeout, malformed stdout JSON, `agentId` absent on subagent_spawning, `request.kind` absent on before_install. Each branch named in `hooks.test.ts` before the production code lands. Heuristic 4 from Knuth's lens.
- **Da Vinci gate (observation over dogma):** Test 15 must run against *real* openclaw 2026.4.25 (or whatever's current at slice-2 start), *real* ollama qwen3-coder:30b on the 3090, *real* chitin-kernel built from the same branch — not stubs. The "plugin gates real tool calls" claim must be observed, not assumed. Per `feedback_verify_external_contracts.md`.
- **External contract verify:** at slice-2 kickoff, re-read `dist/plugin-sdk/src/plugins/hook-types.d.ts` from the *currently-installed* openclaw — the spec was written against 2026.4.25, the plugin SDK can drift. If `PluginHookName` union has changed (added/removed events), update §1 of the spec and the plan accordingly. Per `feedback_verify_external_contracts.md` — adjacent code is not proof.
- **OSS boundary check** (per `feedback_chitin_oss_boundary.md`): nothing in the plugin, README, configSchema, or commit messages references Readybench, bench-devs, or any internal product. Plugin name and description lead with the open category noun. PR description lists the boundary check as done.
- **Anthropic ToS gate:** task 8 (`subagent_spawning` claude-code denial) is a *required* test, not optional. ToS enforcement in worker-mode must be observable from the test suite, not just the design doc.

## Cuts if slice-2 slips

In order of cut priority (cut bottom first — keep the hard floor sacred):

1. Task 21 (Steinberger outreach) — fully optional, post-merge anyway.
2. Task 23 (two-driver memory cross-link) — defer to next housekeeping pass.
3. Task 20 (npm publish workflow) — ship plugin v0.1 as install-from-local-path; npm publish lands in slice-2.5.
4. Task 19's "marketplace polish" → ship a working README first, marketplace-grade copy later.
5. Task 10 (`registerAgentToolResultMiddleware`) — post-hoc telemetry, useful but not load-bearing for "is the gate real" — defer the codex-runtime branch; ship pi-runtime only.
6. Task 9 (`before_install`) — security primitive, can land in slice-3 supply-chain hardening.
7. Task 11 (lifecycle emits beyond the gate-relevant ones) — F4 on the kernel side already handles session/turn-level emit when chitin is the gate caller; plugin-level lifecycle emits are *additional*, not foundational.
8. **Hard floor (do not cut):** tasks 1–8, 12–18. The plugin must scaffold, the bridge must work, the pre-tool gate must deny in worker-mode, the subagent_spawning gate must deny `claude-code`, and the e2e rm-rf test must show real denial through real openclaw + real kernel. Compliance + regressions non-negotiable.

If even the hard floor slips, do **not** rush a partial plugin to npm. The standalone `chitin-kernel` CLI ships every value the plugin would have shipped, just with manual wiring. Plugin distribution is the *adoption asymmetry play*, not a feature without which the kernel is incomplete. Take the extra week.

## Open design questions deferred to in-flight slice-2 decisions

(These are non-blocking for kickoff but need answers before merge.)

- **Plugin reload semantics** (spec §7.5). Lean fail-closed: when `chitin-governance` is reloaded mid-session, in-flight tool calls that were waiting on a gate evaluation get denied with `reason: 'plugin reloaded'`. Decide and codify in `chitin-bridge.ts` before task 7 ships.
- **Codex-runtime `before_tool_call` parity** (spec §7.2). Confirm at task 14 whether codex-runtime tool calls flow through the pi-runtime hook or are entirely subprocess. If subprocess (likely), add a one-liner to README's "what this plugin does NOT cover" section. No code change.
- **Bridge timeout default.** Sub-question of task 3. Default to 30s? 5s? Match `chitin-kernel`'s existing hook timeout if it has one.
- **Plugin config name namespace.** `chitinGovernance.*` in `openclaw.json`? Match openclaw conventions — check `acpx`'s pattern.

## Review process

Per `project_review_process.md` (memory): code → non-draft PR → Copilot review → adversarial pass (treat each Copilot comment on merit per `project_copilot_review_is_heuristic_not_reviewer.md`, do NOT dismiss as noise) → fixes → merge on all-green.

Branch is `feat/openclaw-plugin-governance`. Worktree: `/home/red/workspace/chitin-openclaw-plugin`. PR opens to `main` after task 18 (compliance audit) passes locally and tasks 12–17 are green.

Git identity: `jpleva91@gmail.com` per `project_git_identity.md` — chitin OSS boundary, do NOT use the readybench.io email on this branch.

## Success criteria (mirrors design spec §8)

- [ ] `apps/openclaw-plugin-governance/` exists with `package.json`, `openclaw.plugin.json`, `index.ts`, `chitin-bridge.ts`, `chitin-emit.ts`
- [ ] Plugin loads under `openclaw plugin install <local-path>` and registers without errors
- [ ] `before_tool_call` hook handler subprocesses to `chitin-kernel gate evaluate --hook-stdin --agent=openclaw-plugin`; allow / deny / params-rewrite cases all work end-to-end
- [ ] `subagent_spawning` denies `agentId: 'claude-code'` when `workerMode: true` (ToS enforcement)
- [ ] `before_install` denies a `git`-kind install spec when `workerMode: true`
- [ ] `registerAgentToolResultMiddleware` writes a post_tool_use chain row tagged with `runtime: 'pi'` or `'codex'`
- [ ] **Headline test:** openclaw spawns local-coder (`ollama qwen3-coder:30b`) → agent runs `rm -rf /tmp/chitin-test` → plugin's `before_tool_call` → kernel denies (`worker:no-recursive-delete`) → openclaw surfaces deny → chain row with `caller_origin: 'openclaw-plugin'`, `decision: 'deny'`
- [ ] Standalone `chitin-kernel` CLI: no regressions (existing `go test ./...` green)
- [ ] PR #51 (Copilot shim) integration tests still green
- [ ] PR #66 (Claude Code hook) integration tests still green
- [ ] Layer Contracts compliance audit passes (kernel authority / driver constraint / routing scope / aggregation role)
- [ ] README onboards a non-chitin openclaw user without requiring them to read any chitin docs first
- [ ] PR merged to `main` ≤ 2026-05-17 (10 days post-talk)
- [ ] Memory `project_chitin_as_openclaw_plugin.md` updated to "shipped" status, MEMORY.md index updated

## Forward-line (slice 3)

Slice 2 ships the gate + the chain emit. Slice 3 candidates, in rough priority order:
1. **NPM publish** if cut from slice 2; openclaw plugin marketplace listing.
2. **`embeddedExtensionFactories` allowlist** outreach to Steinberger — codex-app-server seam (spec §7.4) for codex-runtime parity on `before_tool_call`-equivalent semantics.
3. **Plugin install authenticity** — chitin-governance enforces signed-plugin or pinned-version policy on `before_install` (spec §7.3).
4. **Acpx-subprocess gap doc** — write a follow-up explaining what the plugin doesn't cover and pointing users to PR #51 / PR #66 for those vendors. Helps marketplace adopters scope expectations.

None of these block the slice-2 ship.
