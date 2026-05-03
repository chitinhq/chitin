import { spawnSync } from 'node:child_process';
import { envelopeList, envelopeGrant, gateReset, chainInfo, gateStatus } from './chitin.ts';
import {
  formatEnvelopeList,
  formatGrantResult,
  formatGateReset,
  formatChainInfo,
  formatError,
  formatGateStatus,
  type SlackResponse,
} from './format.ts';

// Slash subcommands and block actions that change state. The server
// gates these on the SLACK_ADMIN_USER_IDS allowlist; read-only
// commands fall through.
const DESTRUCTIVE_SLASH_SUBCOMMANDS: ReadonlySet<string> = new Set([
  'envelope-grant',
  'gate-reset',
]);

export function isDestructiveAction(slashSubcommand: string): boolean {
  return DESTRUCTIVE_SLASH_SUBCOMMANDS.has(slashSubcommand);
}

// 30 s: enough for a slow gh round-trip (auth, network) but short
// enough to surface a stuck call as an error rather than a hung
// request thread. Slack's interactive endpoint should respond fast;
// any operation that takes longer than this is suspect.
const GH_TIMEOUT_MS = 30_000;

// Optional repo override. When unset, gh defaults to inferring the
// repo from the current working directory — fragile when the daemon
// runs as a system service. SLACK_APP_REPO=owner/repo pins it.
const GH_REPO = process.env.SLACK_APP_REPO?.trim() || undefined;

// Slash command: /chitin <subcommand> [args...]
// Slack sends: command=/chitin, text=<everything after /chitin>
export function handleSlashCommand(text: string): SlackResponse {
  const parts = text.trim().split(/\s+/).filter(Boolean);
  const sub = parts[0] ?? '';

  try {
    switch (sub) {
      case 'envelope-status': {
        const envs = envelopeList();
        return formatEnvelopeList(envs);
      }
      case 'envelope-grant': {
        const id = parts[1];
        const calls = parseInt(parts[2] ?? '', 10);
        if (!id || Number.isNaN(calls) || calls <= 0) {
          return formatError('Usage: /chitin envelope-grant <id> <calls>');
        }
        envelopeGrant(id, calls);
        return formatGrantResult(id, calls);
      }
      case 'gate-reset': {
        const agent = parts[1];
        if (!agent) return formatError('Usage: /chitin gate-reset <agent>');
        gateReset(agent);
        return formatGateReset(agent);
      }
      case 'gate-status': {
        const agent = parts[1];
        if (!agent) return formatError('Usage: /chitin gate-status <agent>');
        const st = gateStatus(agent);
        return formatGateStatus(st);
      }
      case 'chain-info': {
        const sessionId = parts[1];
        if (!sessionId) return formatError('Usage: /chitin chain-info <session_id>');
        const info = chainInfo(sessionId);
        return formatChainInfo(sessionId, info);
      }
      default:
        return formatError(
          `Unknown subcommand \`${sub}\`. Available: envelope-status, envelope-grant, gate-reset, gate-status, chain-info`,
        );
    }
  } catch (err) {
    return formatError(err instanceof Error ? err.message : String(err));
  }
}

// Block action: button clicks in L1 notification messages.
// action_id values: gate_reset, grant_500_calls, approve_pr
export function handleBlockAction(actionId: string, value: string): SlackResponse {
  try {
    switch (actionId) {
      case 'gate_reset': {
        const agent = value;
        if (!agent) return formatError('gate_reset: missing agent value');
        gateReset(agent);
        return formatGateReset(agent);
      }
      case 'grant_500_calls': {
        const id = value;
        if (!id) return formatError('grant_500_calls: missing envelope id value');
        envelopeGrant(id, 500);
        return formatGrantResult(id, 500);
      }
      case 'approve_pr': {
        // The gatekeeper handles auto-merge; this button lets operator trigger
        // a manual squash-merge. PR number is stored in the button value.
        const prNum = parseInt(value, 10);
        if (Number.isNaN(prNum) || prNum <= 0) {
          return formatError(`approve_pr: invalid PR number "${value}"`);
        }
        const ghArgs = ['pr', 'merge', String(prNum), '--squash', '--delete-branch'];
        if (GH_REPO) ghArgs.push('--repo', GH_REPO);
        const r = spawnSync('gh', ghArgs, {
          encoding: 'utf8',
          timeout: GH_TIMEOUT_MS,
        });
        // spawnSync surfaces missing-binary / timeout via result.error,
        // not stderr. Both are failure modes; report both rather than
        // swallowing the error case as a bare "failed".
        if (r.error) {
          return formatError(`approve_pr spawn failed: ${r.error.message}`);
        }
        if (r.signal === 'SIGTERM') {
          return formatError(`approve_pr timed out after ${GH_TIMEOUT_MS}ms`);
        }
        if (r.status !== 0) {
          const detail = (r.stderr || r.stdout || '').slice(0, 300);
          return formatError(`gh pr merge exited ${r.status}: ${detail}`);
        }
        return { response_type: 'ephemeral', text: `✅ PR #${prNum} merged` };
      }
      default:
        return formatError(`Unknown action: ${actionId}`);
    }
  } catch (err) {
    return formatError(err instanceof Error ? err.message : String(err));
  }
}
