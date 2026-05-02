// Gatekeeper notify (Phase 2 step 4 — bare visibility layer).
//
// What it is:
//   The terminal step the reviewGraphWorkflow calls after the
//   escalation loop returns. Takes the ReviewGraphResult + PR
//   metadata, posts a Slack digest, returns a structured outcome
//   the workflow can include in its return value for telemetry.
//
// What it is NOT (yet):
//   Auto-merge. The factory design's §6 gatekeeper has an auto-merge
//   path on `action='approve' AND CI green`, but auto-merging the
//   swarm's own work is the highest-risk capability we'd ship — it
//   needs a soak window first. v1 is "post the digest, let the
//   operator decide." Auto-merge lands behind a CHITIN_GATEKEEPER_
//   AUTO_MERGE flag in a follow-up entry once we have telemetry on
//   the review-graph's false-approve rate.
//
// Why this is an activity (vs a workflow step or pure function):
//   Slack lives outside Temporal. Workflows can't make HTTP calls
//   directly — they need an activity to do it deterministically (the
//   workflow replays from history; HTTP results are not replay-safe).
//   Pure-function would work for shaping the digest; we keep that
//   half pure (`buildGatekeeperDigest`) and wrap it in
//   `runGatekeeperNotify` for the HTTP boundary.

import type { ReviewGraphAction, ReviewGraphResult } from './review-graph-workflow.ts';
import type { PrMeta } from './review-graph.ts';

export interface GatekeeperInput {
  result: ReviewGraphResult;
  pr_meta: PrMeta;
  /** Repo slug — purely audit context for the digest header. */
  repo: string;
  /** Backlog entry id — same. */
  entry_id: string;
}

export interface GatekeeperOutcome {
  action: ReviewGraphAction;
  notified: boolean;
  /** When notified=false: reason. When notified=true: whether the
   *  Slack post itself returned ok (best-effort; failures don't
   *  propagate, only get logged). */
  reason: string;
  digest: string;
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

/**
 * Post the gatekeeper digest to Slack. Failure is logged but never
 * propagates — visibility is best-effort, never a workflow blocker.
 *
 * Activity-shaped: this is what reviewGraphWorkflow proxies. Slack
 * env-var lookup happens inside the function (vs. at module load) so
 * tests can drive a deterministic outcome regardless of the host's
 * env.
 */
export async function runGatekeeperNotify(input: GatekeeperInput): Promise<GatekeeperOutcome> {
  const digest = buildGatekeeperDigest(input);
  const webhook = process.env.CHITIN_SLACK_WEBHOOK_URL?.trim();

  if (!webhook) {
    return {
      action: input.result.action,
      notified: false,
      reason: 'CHITIN_SLACK_WEBHOOK_URL unset — digest emitted to journal only',
      digest,
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
        blocks: [
          {
            type: 'section',
            text: { type: 'mrkdwn', text: digest },
          },
        ],
      }),
      signal: ctrl.signal,
    });
    if (!resp.ok) {
      return {
        action: input.result.action,
        notified: false,
        reason: `slack post non-ok status=${resp.status}`,
        digest,
      };
    }
    return {
      action: input.result.action,
      notified: true,
      reason: 'posted',
      digest,
    };
  } catch (err) {
    return {
      action: input.result.action,
      notified: false,
      reason: `slack post failed: ${err instanceof Error ? err.message : String(err)}`,
      digest,
    };
  } finally {
    clearTimeout(timer);
  }
}

export const __test__ = {
  ACTION_EMOJI,
  ACTION_HEADLINE,
  renderFindings,
  renderTierLog,
};
