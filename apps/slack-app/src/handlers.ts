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
        const r = spawnSync('gh', ['pr', 'merge', String(prNum), '--squash', '--delete-branch'], {
          encoding: 'utf8',
        });
        if (r.status !== 0) {
          return formatError(`gh pr merge failed: ${(r.stderr ?? '').slice(0, 300)}`);
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
