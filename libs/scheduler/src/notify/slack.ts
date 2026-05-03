// Slack notifier for scheduler events — outbound-only, no interactivity.
//
// Webhook config (in priority order):
//   1. ~/.chitin/secrets/slack-webhook.url (one line, trimmed; gitignored)
//   2. CHITIN_SLACK_WEBHOOK_URL environment variable
// If neither is present every notify* call is a no-op.
//
// Failure model: 3 s timeout, errors swallowed to stderr. Slack visibility
// is nice-to-have; the scheduler must never stall waiting for it.
//
// Smoke test (dry-run, no Slack message sent):
//   tsx libs/scheduler/src/notify/slack.ts --test
// Live smoke test (posts to the configured webhook):
//   tsx libs/scheduler/src/notify/slack.ts --test --live

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { homedir } from 'node:os';

const SLACK_TIMEOUT_MS = 3_000;

function loadWebhookUrl(): string | undefined {
  try {
    const secretPath = join(homedir(), '.chitin', 'secrets', 'slack-webhook.url');
    const url = readFileSync(secretPath, 'utf8').trim();
    if (url) return url;
  } catch {
    // File absent or unreadable — fall through to env var.
  }
  return process.env.CHITIN_SLACK_WEBHOOK_URL?.trim() || undefined;
}

const SLACK_WEBHOOK_URL = loadWebhookUrl();

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
      console.error(
        JSON.stringify({
          ts: new Date().toISOString(),
          level: 'warn',
          component: 'scheduler/notify',
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
        component: 'scheduler/notify',
        msg: 'slack post failed',
        error: err instanceof Error ? err.message : String(err),
      }),
    );
  } finally {
    clearTimeout(timer);
  }
}

// --- Scheduled item starts (5–15 min lead) ---

export interface ScheduleStart {
  entry_id: string;
  scheduled_at: string;
  lead_min: number;
  description?: string;
}

export async function notifyScheduleStart(ev: ScheduleStart): Promise<void> {
  await postSlack({
    text: `📅 schedule start \`${ev.entry_id}\` in ${ev.lead_min}m`,
    blocks: [
      {
        type: 'section',
        text: {
          type: 'mrkdwn',
          text:
            `*📅 scheduled item starting*  \`${ev.entry_id}\`` +
            (ev.description ? ` — ${ev.description}` : ''),
        },
      },
      {
        type: 'context',
        elements: [
          {
            type: 'mrkdwn',
            text: `scheduled \`${ev.scheduled_at}\`  •  lead *${ev.lead_min}m*`,
          },
        ],
      },
    ],
  });
}

// --- Gov denial (severity high or escalation_count > 5) ---

export interface GovDenial {
  entry_id: string;
  tool_name: string;
  action_type: string;
  severity: 'high' | 'irreversible';
  escalation_count: number;
  reason: string;
}

export async function notifyGovDenial(ev: GovDenial): Promise<void> {
  await postSlack({
    text: `🚫 gov denial \`${ev.entry_id}\` — ${ev.tool_name} (${ev.severity})`,
    blocks: [
      {
        type: 'section',
        text: {
          type: 'mrkdwn',
          text: `*🚫 governance denial*  \`${ev.entry_id}\``,
        },
      },
      {
        type: 'section',
        text: {
          type: 'mrkdwn',
          text: '```' + ev.reason.slice(0, 2_000) + '```',
        },
      },
      {
        type: 'context',
        elements: [
          {
            type: 'mrkdwn',
            text:
              `tool \`${ev.tool_name}\`  •  action \`${ev.action_type}\`` +
              `  •  severity *${ev.severity}*  •  escalations *${ev.escalation_count}*`,
          },
        ],
      },
    ],
  });
}

// --- Lockdown trigger ---

export interface LockdownTrigger {
  reason: string;
  triggered_by: string;
  ts: string;
}

export async function notifyLockdownTrigger(ev: LockdownTrigger): Promise<void> {
  await postSlack({
    text: `🔒 lockdown triggered by \`${ev.triggered_by}\`: ${ev.reason.slice(0, 200)}`,
    blocks: [
      {
        type: 'section',
        text: {
          type: 'mrkdwn',
          text: `*🔒 lockdown triggered*`,
        },
      },
      {
        type: 'section',
        text: {
          type: 'mrkdwn',
          text: ev.reason.slice(0, 2_000),
        },
      },
      {
        type: 'context',
        elements: [
          {
            type: 'mrkdwn',
            text: `triggered by \`${ev.triggered_by}\`  •  \`${ev.ts}\``,
          },
        ],
      },
    ],
  });
}

// --- Swarm PR merged ---

export interface SwarmPrMerged {
  entry_id: string;
  pr_url: string;
  pr_number: number;
  title: string;
  merged_by?: string;
}

export async function notifySwarmPrMerged(ev: SwarmPrMerged): Promise<void> {
  await postSlack({
    text: `🎉 PR #${ev.pr_number} merged — \`${ev.entry_id}\``,
    blocks: [
      {
        type: 'section',
        text: {
          type: 'mrkdwn',
          text: `*🎉 swarm PR merged*  <${ev.pr_url}|#${ev.pr_number}> — ${ev.title}`,
        },
      },
      {
        type: 'context',
        elements: [
          {
            type: 'mrkdwn',
            text:
              `entry \`${ev.entry_id}\`` +
              (ev.merged_by ? `  •  merged by *${ev.merged_by}*` : ''),
          },
        ],
      },
    ],
  });
}

// --- Smoke test ---
// Invariant: --test (no --live) never calls fetch; --test --live calls fetch exactly once.

export async function runSmokeTest(opts: { live?: boolean } = {}): Promise<void> {
  const payload: SlackPayload = {
    text: '🧪 chitin scheduler notify slack --test',
    blocks: [
      {
        type: 'section',
        text: {
          type: 'mrkdwn',
          text: '*🧪 scheduler Slack notifier — smoke test*',
        },
      },
      {
        type: 'context',
        elements: [
          {
            type: 'mrkdwn',
            text:
              `webhook ${SLACK_WEBHOOK_URL ? 'configured ✓' : 'NOT SET — no-op'}` +
              `  •  ts \`${new Date().toISOString()}\``,
          },
        ],
      },
    ],
  };

  if (!opts.live) {
    // Dry run: print what would be sent, never touch the network.
    console.log(JSON.stringify(payload, null, 2));
    return;
  }

  await postSlack(payload);
}

// Standalone runner: tsx libs/scheduler/src/notify/slack.ts --test [--live]
const isCli =
  typeof process !== 'undefined' &&
  (process.argv[1]?.endsWith('slack.ts') || process.argv[1]?.endsWith('slack.js'));

if (isCli) {
  const args = process.argv.slice(2);
  if (args.includes('--test')) {
    const live = args.includes('--live');
    console.error(`scheduler slack notifier smoke test (${live ? 'LIVE' : 'dry-run'})`);
    await runSmokeTest({ live });
    if (!live) console.error('dry-run complete — no Slack messages sent');
  } else {
    console.error('usage: tsx slack.ts --test [--live]');
    process.exit(1);
  }
}
