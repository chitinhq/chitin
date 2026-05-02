// Gatekeeper — Phase 2 step 4 (visibility) + step 5 (auto-merge gates).
//
// Two responsibilities, both run as the terminal step of
// reviewGraphWorkflow:
//
// 1. **Notify** — post a Slack digest of the chain's terminal state
//    so the operator has visibility regardless of merge outcome.
//    (Shipped in #149.)
//
// 2. **Auto-merge** — when the chain action is `approve` AND every
//    §6 design-doc gate passes AND the operator has flipped the
//    CHITIN_GATEKEEPER_AUTO_MERGE env var, call `gh pr merge` to
//    actually close the loop without operator hands. Off by default
//    until the operator has a soak window of telemetry to trust the
//    chain's false-approve rate.
//
// §6 gates implemented in v1 (everything that's checkable from
// stdlib + gh CLI without standing up a separate analytics path):
//   - CI green (every check on the PR conclusion=SUCCESS)
//   - Reviewer found no 🔴 findings (already in ReviewGraphResult)
//   - Diff doesn't touch T5-shape paths (chitin.yaml, .chitin/,
//     go/execution-kernel/internal/gov/)
//   - Diff intersects entry's declared file: scope (the bucket-B
//     diff-vs-scope mismatch signal)
//   - Action = 'approve' (terminal state)
//   - t5_shape flag false
//
// §6 gates DEFERRED to a follow-up entry (need telemetry plumbing):
//   - Bucket-B rate < 0% in last 24h (depends on the swarm-rollup's
//     bucket-B detector landing first)
//   - Driver+tier success rate >= 70% this week (analysis.swarm_runs
//     would need to expose this; not yet done)
//   The gate function accepts the optional inputs so when those
//   sources land, the wiring is one function-arg away.
//
// Why this is an activity (vs a workflow step or pure function):
//   Slack + gh CLI live outside Temporal. Workflows can't make HTTP
//   or shell calls directly — they need an activity to do it
//   deterministically (the workflow replays from history; HTTP/CLI
//   results are not replay-safe). The gate evaluation itself is a
//   pure function (`evaluateGates`) so it's testable without a
//   network round-trip.

import { execFileSync } from 'node:child_process';
import type { ReviewGraphAction, ReviewGraphResult } from './review-graph-workflow.ts';
import type { PrMeta } from './review-graph.ts';

export interface GatekeeperInput {
  result: ReviewGraphResult;
  pr_meta: PrMeta;
  /** Repo slug — purely audit context for the digest header. */
  repo: string;
  /** Backlog entry id — same. */
  entry_id: string;
  /** Backlog entry's declared `file:` scope, comma-separated. The
   *  diff-vs-scope intersection gate uses this. Optional — entries
   *  without scope skip that gate. */
  entry_file_scope?: string;
}

export interface GatekeeperOutcome {
  action: ReviewGraphAction;
  notified: boolean;
  /** When notified=false: reason. When notified=true: whether the
   *  Slack post itself returned ok (best-effort; failures don't
   *  propagate, only get logged). */
  reason: string;
  digest: string;
  /** Auto-merge result. `null` when the merge path wasn't taken
   *  (action != 'approve' OR auto-merge flag off OR a gate failed).
   *  When taken: `merged: true` after a successful gh pr merge,
   *  `merged: false` otherwise (with reason). */
  merge: {
    attempted: boolean;
    merged: boolean;
    reason: string;
    gate_failures: string[];
  };
}

export interface GateInputs {
  result: ReviewGraphResult;
  /** Files in the PR diff (gh pr diff --name-only). */
  pr_files: string[];
  /** Entry's declared `file:` field — comma-separated paths. May be
   *  undefined (legacy entries without scope). */
  entry_file_scope: string | undefined;
  /** Whether all CI checks on the PR concluded SUCCESS. */
  ci_green: boolean;
}

export interface GateEvaluation {
  passed: boolean;
  failures: string[];
}

const ACTION_EMOJI: Record<ReviewGraphAction, string> = {
  approve: '✅',
  'request-changes': '🟡',
  'escalate-to-operator': '🚨',
  'parse-failure-at-r4': '⚠️',
};

const ACTION_HEADLINE: Record<ReviewGraphAction, string> = {
  approve: 'review chain approved',
  'request-changes': 'reviewer requested changes',
  'escalate-to-operator': 'review chain escalated to operator',
  'parse-failure-at-r4': 'review chain parse-failure cascade — every tier emit failed to parse',
};

/**
 * Build a markdown digest for the operator. Pure — exported so tests
 * can pin the format invariant without mocking the HTTP boundary.
 */
export function buildGatekeeperDigest(input: GatekeeperInput): string {
  const { result, pr_meta, entry_id, repo } = input;
  const emoji = ACTION_EMOJI[result.action];
  const headline = ACTION_HEADLINE[result.action];

  const findingsSection = renderFindings(result);
  const tiersSection = renderTierLog(result);

  const prLine = pr_meta.pr_url ? `<${pr_meta.pr_url}|#${pr_meta.pr_number ?? '?'}>` : '(no PR url)';

  // Slack mrkdwn tolerates github-flavored markdown for the most
  // common cases; we deliberately stay simple so the same digest
  // renders identically when piped to journalctl.
  return [
    `${emoji} *${headline}*`,
    `repo \`${repo}\`  •  entry \`${entry_id}\`  •  PR ${prLine}`,
    `final_tier=${result.final_tier}  •  diff=${pr_meta.diff_loc} LOC across ${pr_meta.files_changed} file(s)`,
    result.t5_shape ? '⚠️  t5_shape detected — gatekeeper escalates regardless of action' : '',
    findingsSection,
    tiersSection,
  ]
    .filter((line) => line !== '')
    .join('\n');
}

function renderFindings(result: ReviewGraphResult): string {
  const findings = result.output?.findings ?? [];
  if (findings.length === 0) {
    return result.action === 'approve'
      ? 'no findings — chain approved cleanly'
      : 'no structured findings emitted';
  }
  // Cap at 8 to keep Slack rendering reasonable; full audit lives in
  // the temporal UI.
  const head = findings.slice(0, 8);
  const lines = head.map((f) => {
    const loc = f.line ? `${f.file}:${f.line}` : f.file;
    return `  - ${f.severity} ${loc} (${f.category}): ${f.summary}`;
  });
  if (findings.length > head.length) {
    lines.push(`  - … +${findings.length - head.length} more (see Temporal UI for full set)`);
  }
  return ['Findings:', ...lines].join('\n');
}

function renderTierLog(result: ReviewGraphResult): string {
  if (result.tier_log.length === 0) return '';
  const lines = result.tier_log.map((entry) => {
    const status = entry.parsed ? 'parsed' : 'parse-fail';
    const decision = entry.output?.decision ?? '—';
    return `  - ${entry.tier}: ${status}  decision=${decision}`;
  });
  return ['Tier log:', ...lines].join('\n');
}

// ─── §6 auto-merge gates ─────────────────────────────────────────────────

/**
 * T5-shape paths chitin's governance treats as human-only. Touching
 * any of these auto-fails the gate even if every other signal is
 * green — these changes need an operator's eyes per the design
 * (governance can't self-modify).
 */
const T5_FORBIDDEN_PATH_PREFIXES = [
  'chitin.yaml',
  '.chitin/',
  'go/execution-kernel/internal/gov/',
  // Hook installers — touching these bypasses the gate they enforce.
  'go/execution-kernel/internal/hookinstall/',
];

/**
 * Pure §6 gate evaluation. All gates must pass for auto-merge to be
 * safe; any failure populates `failures` with a human-readable
 * reason the operator sees in the Slack digest.
 */
export function evaluateGates(inputs: GateInputs): GateEvaluation {
  const failures: string[] = [];

  if (inputs.result.action !== 'approve') {
    failures.push(`action=${inputs.result.action} (only 'approve' is mergeable)`);
  }

  if (inputs.result.t5_shape) {
    failures.push('t5_shape detected — operator escalation only');
  }

  if (!inputs.ci_green) {
    failures.push('CI not green — at least one check did not conclude SUCCESS');
  }

  // 🔴 = real bug. Even if the chain says approve (rare combination —
  // approve normally implies no 🔴), surface it as a hard fail.
  const redFindings = (inputs.result.output?.findings ?? []).filter(
    (f) => f.severity === '🔴',
  );
  if (redFindings.length > 0) {
    failures.push(`${redFindings.length} 🔴 finding(s) — reviewer flagged real bug`);
  }

  // T5-shape path check on the diff itself (defense in depth — the
  // implementor's gate already blocks chitin.yaml writes, but if
  // governance changed by another route, the merge gate catches it).
  const t5Touched = inputs.pr_files.filter((f) =>
    T5_FORBIDDEN_PATH_PREFIXES.some((p) => f === p || f.startsWith(p)),
  );
  if (t5Touched.length > 0) {
    failures.push(`diff touches T5-shape path(s): ${t5Touched.join(', ')}`);
  }

  // Diff-vs-scope intersection — the bucket-B detection signal.
  // Compute only when the entry has a declared scope; entries
  // without a `file:` field skip this gate (the operator is
  // responsible for those).
  if (inputs.entry_file_scope) {
    const scopeFiles = inputs.entry_file_scope
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    if (scopeFiles.length > 0 && inputs.pr_files.length > 0) {
      const intersects = inputs.pr_files.some((diffFile) =>
        scopeFiles.some((rawScope) => {
          // Strip trailing slash so 'apps/foo/' and 'apps/foo' both
          // match 'apps/foo/bar.ts'.
          const scopeFile = rawScope.replace(/\/+$/, '');
          return (
            diffFile === scopeFile ||
            diffFile.startsWith(`${scopeFile}/`) ||
            scopeFile.startsWith(`${diffFile}/`)
          );
        }),
      );
      if (!intersects) {
        failures.push(
          `diff doesn't intersect entry's declared file: scope (${inputs.pr_files.length} file(s) changed, none match)`,
        );
      }
    }
  }

  return { passed: failures.length === 0, failures };
}

/**
 * Live CI status check via gh CLI. Returns true when EVERY check on
 * the PR has concluded SUCCESS. Required + non-required checks both
 * count — auto-merge is conservative; a flaky CodeQL false-positive
 * is the operator's problem, not the gatekeeper's bypass.
 */
export function checkCiGreen(prNumber: number): boolean {
  try {
    const out = execFileSync(
      'gh',
      [
        'pr',
        'view',
        String(prNumber),
        '--json',
        'statusCheckRollup',
      ],
      { encoding: 'utf8' },
    );
    const parsed = JSON.parse(out) as {
      statusCheckRollup?: { conclusion?: string; status?: string }[];
    };
    const checks = parsed.statusCheckRollup ?? [];
    if (checks.length === 0) return false; // no checks at all → not green
    return checks.every((c) => c.conclusion === 'SUCCESS');
  } catch {
    return false;
  }
}

/**
 * Live diff file enumeration via gh CLI. Returns the list of
 * repo-relative paths that changed. Failure → empty list (caller
 * sees the empty diff and the file-scope gate skips, but the CI
 * gate will catch the underlying problem).
 */
export function fetchPrFiles(prNumber: number): string[] {
  try {
    const out = execFileSync('gh', ['pr', 'diff', String(prNumber), '--name-only'], {
      encoding: 'utf8',
    });
    return out
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean);
  } catch {
    return [];
  }
}

/**
 * Squash-merge the PR via gh CLI. Returns true on success. Failure
 * (network, ref protection, mergeable=false) returns false with
 * the error string surfaced to the digest.
 */
export function mergeViaGh(prNumber: number): { ok: boolean; error?: string } {
  try {
    execFileSync('gh', ['pr', 'merge', String(prNumber), '--squash', '--delete-branch'], {
      encoding: 'utf8',
    });
    return { ok: true };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return { ok: false, error: msg.slice(-500) };
  }
}

/**
 * Run the gatekeeper: evaluate §6 gates, optionally auto-merge, post
 * Slack digest, return structured outcome. Activity-shaped — this is
 * what reviewGraphWorkflow proxies. Env reads are inside the function
 * (vs at module load) so tests drive a deterministic outcome.
 *
 * Auto-merge gating:
 *   - Off by default (CHITIN_GATEKEEPER_AUTO_MERGE != '1' → notify-only).
 *   - When on, every §6 gate must pass. ANY failure → no merge,
 *     digest documents the failure(s), operator merges manually.
 *   - Merge failure (gh exit non-zero) is non-fatal — surfaces in
 *     the digest, operator retries.
 */
export async function runGatekeeperNotify(input: GatekeeperInput): Promise<GatekeeperOutcome> {
  // ── Auto-merge evaluation ────────────────────────────────────────
  const autoMergeOn = process.env.CHITIN_GATEKEEPER_AUTO_MERGE === '1';
  const merge: GatekeeperOutcome['merge'] = {
    attempted: false,
    merged: false,
    reason: autoMergeOn ? 'evaluating gates' : 'CHITIN_GATEKEEPER_AUTO_MERGE off — notify only',
    gate_failures: [],
  };

  if (autoMergeOn && input.result.action === 'approve') {
    if (input.pr_meta.pr_number === undefined) {
      merge.reason = 'pr_number missing — cannot auto-merge';
    } else {
      const prFiles = fetchPrFiles(input.pr_meta.pr_number);
      const ciGreen = checkCiGreen(input.pr_meta.pr_number);
      const evaluation = evaluateGates({
        result: input.result,
        pr_files: prFiles,
        entry_file_scope: input.entry_file_scope,
        ci_green: ciGreen,
      });
      merge.gate_failures = evaluation.failures;

      if (evaluation.passed) {
        merge.attempted = true;
        const mergeResult = mergeViaGh(input.pr_meta.pr_number);
        merge.merged = mergeResult.ok;
        merge.reason = mergeResult.ok
          ? `gates passed; gh pr merge succeeded (#${input.pr_meta.pr_number})`
          : `gates passed; gh pr merge failed: ${mergeResult.error ?? 'unknown'}`;
      } else {
        merge.reason = `${evaluation.failures.length} gate(s) failed — notify only`;
      }
    }
  }

  // ── Digest (now includes merge outcome) ─────────────────────────
  const digest = buildGatekeeperDigestWithMerge(input, merge);

  // ── Slack post ──────────────────────────────────────────────────
  const webhook = process.env.CHITIN_SLACK_WEBHOOK_URL?.trim();
  if (!webhook) {
    return {
      action: input.result.action,
      notified: false,
      reason: 'CHITIN_SLACK_WEBHOOK_URL unset — digest emitted to journal only',
      digest,
      merge,
    };
  }

  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 3000);
  try {
    const resp = await fetch(webhook, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({
        text: digest,
        blocks: [{ type: 'section', text: { type: 'mrkdwn', text: digest } }],
      }),
      signal: ctrl.signal,
    });
    if (!resp.ok) {
      return {
        action: input.result.action,
        notified: false,
        reason: `slack post non-ok status=${resp.status}`,
        digest,
        merge,
      };
    }
    return {
      action: input.result.action,
      notified: true,
      reason: 'posted',
      digest,
      merge,
    };
  } catch (err) {
    return {
      action: input.result.action,
      notified: false,
      reason: `slack post failed: ${err instanceof Error ? err.message : String(err)}`,
      digest,
      merge,
    };
  } finally {
    clearTimeout(timer);
  }
}

/**
 * Build the digest enriched with the merge outcome. Falls back to
 * buildGatekeeperDigest when there's no merge to report (the v1
 * notify-only behavior pre-#149).
 */
export function buildGatekeeperDigestWithMerge(
  input: GatekeeperInput,
  merge: GatekeeperOutcome['merge'],
): string {
  const base = buildGatekeeperDigest(input);
  const mergeSection = renderMergeSection(merge);
  return mergeSection ? `${base}\n${mergeSection}` : base;
}

function renderMergeSection(merge: GatekeeperOutcome['merge']): string {
  if (merge.merged) {
    return `🤖 Auto-merged: ${merge.reason}`;
  }
  if (merge.attempted && !merge.merged) {
    return `⚠️  Auto-merge attempted but failed: ${merge.reason}`;
  }
  if (merge.gate_failures.length > 0) {
    return [
      '🛑 Auto-merge gates failed (operator merges manually):',
      ...merge.gate_failures.map((f) => `  - ${f}`),
    ].join('\n');
  }
  // No merge attempted, no gate failures → either action!='approve'
  // OR auto-merge flag off. Don't add visual noise to the digest.
  return '';
}

export const __test__ = {
  ACTION_EMOJI,
  ACTION_HEADLINE,
  renderFindings,
  renderTierLog,
};
