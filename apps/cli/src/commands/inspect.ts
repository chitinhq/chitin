import type { Command } from 'commander';
import { resolveChitinDir } from '@chitin/contracts';
import {
  getAgentSummary,
  getSessionTimeline,
  listDecisions,
  listRuleSummaries,
  type AgentSummary,
  type DecisionFilters,
  type DecisionRow,
  type RuleHitSummary,
  type SessionTimeline,
} from '@chitin/telemetry';

interface InspectOpts {
  readonly chitinDir?: string;
  readonly workspace?: string;
  readonly limit?: number;
  readonly driver?: string;
  readonly agent?: string;
  readonly decision?: 'allow' | 'deny';
  readonly since?: string;
}

export function registerInspect(program: Command): void {
  const inspect = program.command('inspect').description('Inspect Chitin governance telemetry');

  inspect
    .command('live')
    .description('Show recent governance decisions')
    .option('--chitin-dir <dir>', 'state dir to read (default: resolved .chitin)')
    .option('--workspace <dir>', 'workspace dir for .chitin resolution (default: cwd)')
    .option('--limit <n>', 'max rows', parseLimit, 50)
    .option('--driver <name>', 'filter by driver')
    .option('--agent <id>', 'filter by agent')
    .option('--decision <allow|deny>', 'filter by decision')
    .option('--since <duration>', 'relative window: Nm, Nh, or Nd')
    .action((opts: InspectOpts) => {
      const rows = listDecisions(resolveInspectDir(opts), toDecisionFilters(opts));
      process.stdout.write(renderDecisionRows(rows));
    });

  inspect
    .command('denials')
    .description('Show recent denied governance decisions')
    .option('--chitin-dir <dir>', 'state dir to read (default: resolved .chitin)')
    .option('--workspace <dir>', 'workspace dir for .chitin resolution (default: cwd)')
    .option('--limit <n>', 'max rows', parseLimit, 50)
    .option('--driver <name>', 'filter by driver')
    .option('--agent <id>', 'filter by agent')
    .option('--since <duration>', 'relative window: Nm, Nh, or Nd')
    .action((opts: InspectOpts) => {
      const rows = listDecisions(resolveInspectDir(opts), {
        ...toDecisionFilters(opts),
        decision: 'deny',
      });
      process.stdout.write(renderDenialRows(rows));
    });

  inspect
    .command('session')
    .description('Show a chain/session timeline')
    .argument('<chain-id>', 'chain id to inspect')
    .option('--chitin-dir <dir>', 'state dir to read (default: resolved .chitin)')
    .option('--workspace <dir>', 'workspace dir for .chitin resolution (default: cwd)')
    .action((chainId: string, opts: InspectOpts) => {
      const timeline = getSessionTimeline(resolveInspectDir(opts), chainId);
      process.stdout.write(renderSessionTimeline(timeline));
    });

  inspect
    .command('agent')
    .description('Show recent governance summary for one agent')
    .argument('<agent-id>', 'agent id to inspect')
    .option('--chitin-dir <dir>', 'state dir to read (default: resolved .chitin)')
    .option('--workspace <dir>', 'workspace dir for .chitin resolution (default: cwd)')
    .action((agentId: string, opts: InspectOpts) => {
      process.stdout.write(renderAgentSummary(getAgentSummary(resolveInspectDir(opts), agentId)));
    });

  inspect
    .command('rules')
    .description('Show most-hit governance rules')
    .option('--chitin-dir <dir>', 'state dir to read (default: resolved .chitin)')
    .option('--workspace <dir>', 'workspace dir for .chitin resolution (default: cwd)')
    .option('--limit <n>', 'max rules', parseLimit, 20)
    .option('--driver <name>', 'filter by driver')
    .option('--agent <id>', 'filter by agent')
    .option('--since <duration>', 'relative window: Nm, Nh, or Nd')
    .action((opts: InspectOpts) => {
      const summaries = listRuleSummaries(resolveInspectDir(opts), toDecisionFilters(opts, false));
      process.stdout.write(renderRuleSummaries(summaries.slice(0, opts.limit)));
    });
}

export function renderDecisionRows(rows: readonly DecisionRow[]): string {
  if (rows.length === 0) return '(no decisions found)\n';
  return rows.map((row) => {
    const driver = row.driver ?? '-';
    const agent = row.agent ?? '-';
    const rule = row.ruleId ?? '-';
    const signal = renderSignals(row);
    return [
      row.ts,
      row.decision.padEnd(5),
      driver.padEnd(12),
      agent.padEnd(14),
      row.actionType.padEnd(14),
      rule,
      signal,
      row.actionTarget,
    ].filter((part) => part !== '').join(' ') + '\n';
  }).join('');
}

export function renderDenialRows(rows: readonly DecisionRow[]): string {
  const denials = rows.filter((row) => row.decision === 'deny');
  if (denials.length === 0) return '(no denials found)\n';
  return denials.map((row) => {
    const driver = row.driver ?? '-';
    const agent = row.agent ?? '-';
    const reason = row.reason ? ` reason=${row.reason}` : '';
    const suggestion = row.suggestion ? ` suggestion=${row.suggestion}` : '';
    return `${row.ts} ${driver}/${agent} ${row.ruleId ?? '-'} ${row.actionType} ${row.actionTarget}${reason}${suggestion}\n`;
  }).join('');
}

export function renderSessionTimeline(timeline: SessionTimeline): string {
  const lines = [
    `chain ${timeline.chainId}`,
    `health ${timeline.chainHealth.ok ? 'ok' : 'broken'}`,
  ];
  for (const event of timeline.events) {
    const payload = renderTimelinePayload(event.payload);
    lines.push(
      [
        event.ts,
        `seq=${event.seq}`,
        event.surface,
        event.eventType,
        `hash=${shortHash(event.thisHash)}`,
        payload,
      ].filter((part) => part !== '').join(' '),
    );
  }
  for (const chainBreak of timeline.chainHealth.breaks) {
    lines.push(
      `break seq=${chainBreak.seq} expected=${shortHash(chainBreak.expectedPrevHash)} actual=${shortHash(chainBreak.actualPrevHash)}`,
    );
  }
  return lines.join('\n') + '\n';
}

export function renderAgentSummary(summary: AgentSummary): string {
  const lines = [
    `agent ${summary.agent}`,
    `decisions=${summary.decisionCount} allow=${summary.allowCount} deny=${summary.denyCount}`,
  ];
  if (summary.state) {
    lines.push(
      `state=${summary.state.level} total_denials=${summary.state.totalDenials} locked=${summary.state.locked}`,
    );
  }
  if (summary.rules.length === 0) {
    lines.push('(no rule hits)');
  } else {
    lines.push(...summary.rules.map(renderRuleSummaryLine));
  }
  return lines.join('\n') + '\n';
}

export function renderRuleSummaries(summaries: readonly RuleHitSummary[]): string {
  if (summaries.length === 0) return '(no rule hits found)\n';
  const lines: string[] = [];
  for (const summary of summaries) {
    lines.push(renderRuleSummaryLine(summary));
    for (const example of summary.examples.slice(0, 2)) {
      lines.push(`  example ${example.agent ?? '-'} ${example.actionType} ${example.actionTarget}`);
    }
  }
  return lines.join('\n') + '\n';
}

export function parseSince(value: string | undefined, now = new Date()): Date | undefined {
  if (!value) return undefined;
  const match = /^(\d+)([mhd])$/.exec(value);
  if (!match) throw new Error(`invalid --since ${value}; use Nm, Nh, or Nd`);
  const amount = Number.parseInt(match[1], 10);
  const unit = match[2];
  const millis = unit === 'm'
    ? amount * 60_000
    : unit === 'h'
      ? amount * 60 * 60_000
      : amount * 24 * 60 * 60_000;
  return new Date(now.getTime() - millis);
}

function toDecisionFilters(opts: InspectOpts, includeLimit = true): DecisionFilters {
  return {
    driver: opts.driver,
    agent: opts.agent,
    decision: opts.decision,
    since: parseSince(opts.since),
    limit: includeLimit ? opts.limit : undefined,
  };
}

function resolveInspectDir(opts: InspectOpts): string {
  if (opts.chitinDir) return opts.chitinDir;
  return resolveChitinDir(process.cwd(), opts.workspace ?? '');
}

function renderSignals(row: DecisionRow): string {
  const signals = row.signals;
  if (!signals) return '';
  const parts: string[] = [];
  if (signals.predictedBlast !== undefined) parts.push(`blast=${signals.predictedBlast}`);
  if (signals.flounderingScore !== undefined) parts.push(`flounder=${signals.flounderingScore}`);
  if (signals.driftScore !== undefined) parts.push(`drift=${signals.driftScore}`);
  if (signals.routingDecision !== undefined) parts.push(`route=${signals.routingDecision}`);
  return parts.join(' ');
}

function parseLimit(value: string): number {
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    throw new Error(`invalid --limit ${value}`);
  }
  return parsed;
}

function shortHash(value: string | null): string {
  return value === null ? '<null>' : value.slice(0, 12);
}

function renderTimelinePayload(payload: Record<string, unknown>): string {
  const decision = stringPayload(payload, 'decision');
  const rule = stringPayload(payload, 'rule_id');
  const actionType = stringPayload(payload, 'action_type');
  const actionTarget = stringPayload(payload, 'action_target');
  const signals = renderPayloadSignals(payload);
  return [decision, rule, actionType, actionTarget, signals]
    .filter((part) => part !== undefined && part !== '')
    .join(' ');
}

function renderRuleSummaryLine(summary: RuleHitSummary): string {
  return `${summary.ruleId} count=${summary.count} allow=${summary.allowCount} deny=${summary.denyCount}`;
}

function renderPayloadSignals(payload: Record<string, unknown>): string {
  const parts: string[] = [];
  if (typeof payload.predicted_blast === 'number') parts.push(`blast=${payload.predicted_blast}`);
  if (typeof payload.floundering_score === 'number') parts.push(`flounder=${payload.floundering_score}`);
  if (typeof payload.drift_score === 'number') parts.push(`drift=${payload.drift_score}`);
  const routingDecision = stringPayload(payload, 'routing_decision');
  if (routingDecision) parts.push(`route=${routingDecision}`);
  return parts.join(' ');
}

function stringPayload(payload: Record<string, unknown>, key: string): string | undefined {
  const value = payload[key];
  return typeof value === 'string' && value !== '' ? value : undefined;
}
