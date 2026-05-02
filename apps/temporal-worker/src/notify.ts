// Slack notifier for the autonomous swarm — gives the operator
// visibility into dispatcher events without tailing journalctl.
//
// Activation: set CHITIN_SLACK_WEBHOOK_URL to a Slack incoming webhook
// URL (Slack Apps → Incoming Webhooks → New). If unset, every notify*
// call is a no-op — safe to leave wired up in dev environments where
// the operator hasn't configured Slack yet.
//
// Failure mode: every Slack call has a 3s timeout and swallows errors.
// A flaky Slack endpoint must NEVER block dispatch; visibility is
// nice-to-have, the swarm running is the actual product.

import type { DriverId, Tier } from '@chitin/contracts';

const SLACK_WEBHOOK_URL = process.env.CHITIN_SLACK_WEBHOOK_URL?.trim();
const SLACK_TIMEOUT_MS = 3000;

interface SlackBlock {
  type: string;
  text?: { type: string; text: string };
  fields?: Array<{ type: string; text: string }>;
  elements?: Array<{ type: string; text: string }>;
}

interface SlackPayload {
  text: string;
  blocks?: SlackBlock[];
}

async function postSlack(payload: SlackPayload): Promise<void> {
  if (!SLACK_WEBHOOK_URL) return;
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), SLACK_TIMEOUT_MS);
  try {
    const resp = await fetch(SLACK_WEBHOOK_URL, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(payload),
      signal: ctrl.signal,
    });
    if (!resp.ok) {
      // Fire-and-forget: log to stderr but do not throw.
      console.error(
        JSON.stringify({
          ts: new Date().toISOString(),
          level: 'warn',
          component: 'notify',
          msg: 'slack post non-ok',
          status: resp.status,
        }),
      );
    }
  } catch (err) {
    console.error(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'warn',
        component: 'notify',
        msg: 'slack post failed',
        error: err instanceof Error ? err.message : String(err),
      }),
    );
  } finally {
    clearTimeout(timer);
  }
}

export interface DispatchStart {
  entry_id: string;
  tier: Tier;
  driver: DriverId;
  workflow_id: string;
}

export async function notifyDispatchStart(ev: DispatchStart): Promise<void> {
  await postSlack({
    text: `🦞 dispatch start \`${ev.entry_id}\` (${ev.tier} → ${ev.driver})`,
    blocks: [
      {
        type: 'section',
        text: {
          type: 'mrkdwn',
          text: `*🦞 swarm dispatch start*  \`${ev.entry_id}\``,
        },
      },
      {
        type: 'context',
        elements: [
          { type: 'mrkdwn', text: `tier *${ev.tier}*  •  driver *${ev.driver}*  •  wf \`${ev.workflow_id}\`` },
        ],
      },
    ],
  });
}

export interface DispatchComplete {
  entry_id: string;
  workflow_id: string;
  exit_code: number;
  duration_ms: number;
  /** From the activity result (captured before apply ran). May understate
   *  reality if apply auto-committed tracked uncommitted changes — see
   *  auto_committed. */
  commits_added: number;
  uncommitted: boolean;
  pr_url?: string;
  /** True when the apply step (or PR creation inside it) failed. The
   *  operator already got a separate notifyDispatchError; this flag is
   *  for the summary so we don't render misleading "no work produced"
   *  text in the same channel. */
  apply_failed?: boolean;
  /** True when the apply step actually pushed the branch to origin. */
  pushed?: boolean;
  /** True when apply auto-committed tracked uncommitted changes (so
   *  commits_added=0 from the activity result no longer means "no
   *  work"). */
  auto_committed?: boolean;
}

export async function notifyDispatchComplete(ev: DispatchComplete): Promise<void> {
  // "Real work produced" considers both committed and auto-committed work,
  // and treats a successful push as evidence regardless of commits_added
  // (since apply only pushes when there's tracked work to push).
  const producedWork = ev.commits_added > 0 || ev.auto_committed === true || ev.pushed === true;
  const ok = ev.exit_code === 0 && producedWork;
  const icon = ev.apply_failed ? '⚠️' : ev.pr_url ? '✅' : ok ? '🟢' : ev.exit_code === 0 ? '⚪' : '❌';
  const summary = ev.pr_url
    ? `PR opened — <${ev.pr_url}|${ev.pr_url.split('/').pop()}>`
    : ev.apply_failed
      ? ev.pushed
        ? 'apply failed after push — branch is on origin, PR creation/cleanup needs manual attention (see error)'
        : 'apply failed (see error notification)'
      : ok
        ? ev.pushed
          ? `pushed ${ev.commits_added || (ev.auto_committed ? 'auto-committed' : '?')} commit(s) but no PR URL captured`
          : `committed ${ev.commits_added} (no PR yet)`
        : ev.exit_code === 0
          ? 'no work produced (empty worktree)'
          : `exit ${ev.exit_code}`;
  const dur = (ev.duration_ms / 1000).toFixed(1);
  await postSlack({
    text: `${icon} ${ev.entry_id} — ${summary} (${dur}s)`,
    blocks: [
      {
        type: 'section',
        text: {
          type: 'mrkdwn',
          text: `${icon} *${ev.entry_id}* — ${summary}`,
        },
      },
      {
        type: 'context',
        elements: [
          {
            type: 'mrkdwn',
            text:
              `exit \`${ev.exit_code}\`  •  ${dur}s  •  commits *${ev.commits_added}*` +
              (ev.auto_committed ? ' (+auto)' : '') +
              `  •  pushed *${ev.pushed ? 'yes' : 'no'}*  •  uncommitted *${ev.uncommitted}*  •  wf \`${ev.workflow_id}\``,
          },
        ],
      },
    ],
  });
}

export interface DispatchError {
  entry_id: string;
  workflow_id?: string;
  stage: 'submit' | 'workflow' | 'apply';
  error: string;
}

export async function notifyDispatchError(ev: DispatchError): Promise<void> {
  await postSlack({
    text: `🚨 dispatch error \`${ev.entry_id}\` at ${ev.stage}: ${ev.error.slice(0, 300)}`,
    blocks: [
      {
        type: 'section',
        text: {
          type: 'mrkdwn',
          text: `*🚨 dispatch error*  \`${ev.entry_id}\` — ${ev.stage}`,
        },
      },
      {
        type: 'section',
        text: {
          type: 'mrkdwn',
          text: '```' + ev.error.slice(0, 2000) + '```',
        },
      },
      {
        type: 'context',
        elements: [
          {
            type: 'mrkdwn',
            text: ev.workflow_id ? `wf \`${ev.workflow_id}\`` : 'no workflow id',
          },
        ],
      },
    ],
  });
}

export async function notifyTickIdle(reason: string): Promise<void> {
  // Optional, very low-noise: only post if explicitly enabled, since
  // most ticks are idle and we don't want to spam the channel.
  if (process.env.CHITIN_SLACK_NOTIFY_IDLE !== '1') return;
  await postSlack({
    text: `💤 dispatcher idle — ${reason}`,
  });
}

// Pull the first https://github.com/.../pull/<n> URL out of arbitrary
// text (typically the apply step's stdout/stderr).
const PR_URL_RE = /https:\/\/github\.com\/[\w.-]+\/[\w.-]+\/pull\/\d+/;
export function extractPrUrl(text: string): string | undefined {
  const m = text.match(PR_URL_RE);
  return m ? m[0] : undefined;
}
