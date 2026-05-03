import type { EnvelopeState, ChainInfo, GateStatus } from './chitin.ts';

export interface SlackBlock {
  type: string;
  text?: { type: string; text: string };
  fields?: Array<{ type: string; text: string }>;
  elements?: Array<{ type: string; action_id?: string; text?: { type: string; text: string }; style?: string; value?: string }>;
  accessory?: { type: string; action_id: string; text: { type: string; text: string }; value?: string; style?: string };
}

export interface SlackResponse {
  response_type?: 'ephemeral' | 'in_channel';
  text: string;
  blocks?: SlackBlock[];
}

export function formatEnvelopeList(envs: EnvelopeState[]): SlackResponse {
  if (envs.length === 0) {
    return { text: 'No envelopes found.', response_type: 'ephemeral' };
  }
  const header: SlackBlock = {
    type: 'section',
    text: { type: 'mrkdwn', text: `*Budget Envelopes* (${envs.length})` },
  };
  const rows: SlackBlock[] = envs.slice(0, 10).map((e) => {
    const callsLine = e.max_tool_calls > 0
      ? `calls: ${e.used_tool_calls}/${e.max_tool_calls}`
      : `calls: ${e.used_tool_calls} (uncapped)`;
    const statusEmoji = e.status === 'open' ? '🟢' : '🔴';
    return {
      type: 'section',
      text: {
        type: 'mrkdwn',
        text: `${statusEmoji} \`${e.id}\` — ${e.status}\n${callsLine}`,
      },
      accessory: e.status === 'open' ? {
        type: 'button',
        action_id: 'grant_500_calls',
        text: { type: 'plain_text', text: 'Grant +500 calls' },
        style: 'primary',
        value: e.id,
      } : undefined,
    };
  });
  return {
    response_type: 'ephemeral',
    text: `${envs.length} envelopes`,
    blocks: [header, ...rows],
  };
}

export function formatGrantResult(id: string, calls: number): SlackResponse {
  return {
    response_type: 'ephemeral',
    text: `✅ Granted +${calls} calls to envelope \`${id}\``,
  };
}

export function formatGateReset(agent: string): SlackResponse {
  return {
    response_type: 'ephemeral',
    text: `✅ Gate reset for agent \`${agent}\` — lockdown cleared`,
  };
}

export function formatChainInfo(chainId: string, info: ChainInfo): SlackResponse {
  if (!info.exists) {
    return {
      response_type: 'ephemeral',
      text: `Chain \`${chainId}\` not found`,
    };
  }
  return {
    response_type: 'ephemeral',
    text: `Chain \`${chainId}\``,
    blocks: [
      {
        type: 'section',
        fields: [
          { type: 'mrkdwn', text: `*chain_id*\n\`${chainId}\`` },
          { type: 'mrkdwn', text: `*last_seq*\n${info.last_seq ?? '—'}` },
          { type: 'mrkdwn', text: `*last_hash*\n\`${(info.last_hash ?? '—').slice(0, 12)}\`` },
        ],
      },
    ],
  };
}

export function formatGateStatus(status: GateStatus): SlackResponse {
  const locked = status.locked;
  const icon = locked ? '🔒' : '🟢';
  return {
    response_type: 'ephemeral',
    text: `${icon} Agent \`${status.agent}\` — level=${status.level} locked=${locked}`,
    blocks: locked ? [
      {
        type: 'section',
        text: {
          type: 'mrkdwn',
          text: `${icon} *Agent \`${status.agent}\` is locked down*\nlevel=\`${status.level}\`  policy=\`${status.policy_id}\``,
        },
        accessory: {
          type: 'button',
          action_id: 'gate_reset',
          text: { type: 'plain_text', text: 'Reset lockdown' },
          style: 'danger',
          value: status.agent,
        },
      },
    ] : undefined,
  };
}

export function formatError(msg: string): SlackResponse {
  return { response_type: 'ephemeral', text: `❌ ${msg}` };
}
