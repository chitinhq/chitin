import BetterSqlite3 from 'better-sqlite3';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { ensureIndexed } from './ensure-indexed.js';

export type DecisionKind = 'allow' | 'deny';

export interface DecisionFilters {
  readonly driver?: string;
  readonly agent?: string;
  readonly actionType?: string;
  readonly decision?: DecisionKind;
  readonly since?: Date;
  readonly limit?: number;
}

export interface DecisionSignals {
  readonly predictedBlast?: number;
  readonly flounderingScore?: number;
  readonly driftScore?: number;
  readonly routingDecision?: string;
}

export interface DecisionRow {
  readonly ts: string;
  readonly allowed: boolean;
  readonly decision: DecisionKind;
  readonly mode?: string;
  readonly ruleId?: string;
  readonly reason?: string;
  readonly suggestion?: string;
  readonly escalation?: string;
  readonly agent?: string;
  readonly driver?: string;
  readonly actionType: string;
  readonly actionTarget: string;
  readonly envelopeId?: string;
  readonly tier?: string;
  readonly workflowId?: string;
  readonly signals?: DecisionSignals;
}

export interface TimelineEvent {
  readonly ts: string;
  readonly seq: number;
  readonly eventType: string;
  readonly surface: string;
  readonly sessionId: string;
  readonly chainId: string;
  readonly prevHash: string | null;
  readonly thisHash: string;
  readonly labels: Record<string, unknown>;
  readonly payload: Record<string, unknown>;
}

export interface ChainBreak {
  readonly seq: number;
  readonly expectedPrevHash: string | null;
  readonly actualPrevHash: string | null;
}

export interface SessionTimeline {
  readonly chainId: string;
  readonly events: TimelineEvent[];
  readonly chainHealth: {
    readonly ok: boolean;
    readonly breaks: ChainBreak[];
  };
}

export interface RuleHitSummary {
  readonly ruleId: string;
  readonly count: number;
  readonly allowCount: number;
  readonly denyCount: number;
  readonly examples: DecisionRow[];
}

export interface AgentStateSummary {
  readonly totalDenials: number;
  readonly locked: boolean;
  readonly lockedTs?: string;
  readonly level: 'normal' | 'elevated' | 'high' | 'lockdown';
}

export interface AgentSummary {
  readonly agent: string;
  readonly decisionCount: number;
  readonly allowCount: number;
  readonly denyCount: number;
  readonly rules: RuleHitSummary[];
  readonly recentDecisions: DecisionRow[];
  readonly state?: AgentStateSummary;
}

interface RawDecision {
  readonly allowed?: unknown;
  readonly mode?: unknown;
  readonly rule_id?: unknown;
  readonly reason?: unknown;
  readonly suggestion?: unknown;
  readonly escalation?: unknown;
  readonly agent?: unknown;
  readonly driver?: unknown;
  readonly action_type?: unknown;
  readonly action_target?: unknown;
  readonly ts?: unknown;
  readonly envelope_id?: unknown;
  readonly tier?: unknown;
  readonly workflow_id?: unknown;
  readonly predicted_blast?: unknown;
  readonly floundering_score?: unknown;
  readonly drift_score?: unknown;
  readonly routing_decision?: unknown;
}

interface EventRow {
  readonly ts: string;
  readonly seq: number;
  readonly event_type: string;
  readonly surface: string;
  readonly session_id: string;
  readonly chain_id: string;
  readonly prev_hash: string | null;
  readonly this_hash: string;
  readonly labels: string;
  readonly payload: string;
}

export function listDecisions(chitinDir: string, filters: DecisionFilters = {}): DecisionRow[] {
  if (!existsSync(chitinDir)) return [];

  const rows: DecisionRow[] = [];
  for (const name of readdirSync(chitinDir)) {
    if (!/^gov-decisions-\d{4}-\d{2}-\d{2}\.jsonl$/.test(name)) continue;
    const content = readFileSync(join(chitinDir, name), 'utf8');
    for (const line of content.split('\n')) {
      const row = parseDecisionLine(line);
      if (!row || !matchesDecisionFilters(row, filters)) continue;
      rows.push(row);
    }
  }

  rows.sort((a, b) => b.ts.localeCompare(a.ts));
  return typeof filters.limit === 'number' ? rows.slice(0, filters.limit) : rows;
}

export function getSessionTimeline(chitinDir: string, chainId: string): SessionTimeline {
  ensureIndexed(chitinDir);
  const dbPath = join(chitinDir, 'events.db');
  if (!existsSync(dbPath)) {
    return { chainId, events: [], chainHealth: { ok: true, breaks: [] } };
  }

  const db = new BetterSqlite3(dbPath, { readonly: true });
  try {
    const rows = db
      .prepare(
        `SELECT ts, seq, event_type, surface, session_id, chain_id, prev_hash,
                this_hash, labels, payload
         FROM events
         WHERE chain_id = ?
         ORDER BY seq ASC, ts ASC`,
      )
      .all(chainId) as EventRow[];
    const events = rows.map((row) => ({
      ts: row.ts,
      seq: row.seq,
      eventType: row.event_type,
      surface: row.surface,
      sessionId: row.session_id,
      chainId: row.chain_id,
      prevHash: row.prev_hash,
      thisHash: row.this_hash,
      labels: parseJSONRecord(row.labels),
      payload: parseJSONRecord(row.payload),
    }));
    const breaks = findChainBreaks(events);
    return { chainId, events, chainHealth: { ok: breaks.length === 0, breaks } };
  } finally {
    db.close();
  }
}

export function getAgentSummary(chitinDir: string, agent: string): AgentSummary {
  const decisions = listDecisions(chitinDir, { agent });
  const allowCount = decisions.filter((row) => row.decision === 'allow').length;
  const denyCount = decisions.filter((row) => row.decision === 'deny').length;
  return {
    agent,
    decisionCount: decisions.length,
    allowCount,
    denyCount,
    rules: summarizeRules(decisions),
    recentDecisions: decisions.slice(0, 10),
    state: readAgentState(chitinDir, agent),
  };
}

export function listRuleSummaries(chitinDir: string, filters: DecisionFilters = {}): RuleHitSummary[] {
  return summarizeRules(listDecisions(chitinDir, filters));
}

function parseDecisionLine(line: string): DecisionRow | null {
  const trimmed = line.trim();
  if (!trimmed) return null;

  let raw: RawDecision;
  try {
    raw = JSON.parse(trimmed) as RawDecision;
  } catch {
    return null;
  }

  if (typeof raw.ts !== 'string' || typeof raw.action_type !== 'string') return null;
  const allowed = raw.allowed === true;
  const actionTarget = typeof raw.action_target === 'string' ? raw.action_target : '';
  const signals = decisionSignals(raw);
  return {
    ts: raw.ts,
    allowed,
    decision: allowed ? 'allow' : 'deny',
    mode: stringField(raw.mode),
    ruleId: stringField(raw.rule_id),
    reason: stringField(raw.reason),
    suggestion: stringField(raw.suggestion),
    escalation: stringField(raw.escalation),
    agent: stringField(raw.agent),
    driver: stringField(raw.driver),
    actionType: raw.action_type,
    actionTarget,
    envelopeId: stringField(raw.envelope_id),
    tier: stringField(raw.tier),
    workflowId: stringField(raw.workflow_id),
    signals: Object.keys(signals).length > 0 ? signals : undefined,
  };
}

function matchesDecisionFilters(row: DecisionRow, filters: DecisionFilters): boolean {
  if (filters.driver && row.driver !== filters.driver) return false;
  if (filters.agent && row.agent !== filters.agent) return false;
  if (filters.actionType && row.actionType !== filters.actionType) return false;
  if (filters.decision && row.decision !== filters.decision) return false;
  if (filters.since && Date.parse(row.ts) < filters.since.getTime()) return false;
  return true;
}

function decisionSignals(raw: RawDecision): DecisionSignals {
  const signals: Record<string, number | string> = {};
  if (typeof raw.predicted_blast === 'number') signals.predictedBlast = raw.predicted_blast;
  if (typeof raw.floundering_score === 'number') signals.flounderingScore = raw.floundering_score;
  if (typeof raw.drift_score === 'number') signals.driftScore = raw.drift_score;
  if (typeof raw.routing_decision === 'string' && raw.routing_decision !== '') {
    signals.routingDecision = raw.routing_decision;
  }
  return signals;
}

function stringField(value: unknown): string | undefined {
  return typeof value === 'string' && value !== '' ? value : undefined;
}

function parseJSONRecord(value: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(value) as unknown;
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? parsed as Record<string, unknown>
      : {};
  } catch {
    return {};
  }
}

function findChainBreaks(events: TimelineEvent[]): ChainBreak[] {
  const breaks: ChainBreak[] = [];
  let expectedPrevHash: string | null = null;
  for (const event of events) {
    if (event.prevHash !== expectedPrevHash) {
      breaks.push({ seq: event.seq, expectedPrevHash, actualPrevHash: event.prevHash });
    }
    expectedPrevHash = event.thisHash;
  }
  return breaks;
}

function summarizeRules(decisions: readonly DecisionRow[]): RuleHitSummary[] {
  const byRule = new Map<string, DecisionRow[]>();
  for (const decision of decisions) {
    const ruleId = decision.ruleId ?? '<unknown>';
    const existing = byRule.get(ruleId) ?? [];
    existing.push(decision);
    byRule.set(ruleId, existing);
  }
  return [...byRule.entries()]
    .map(([ruleId, rows]) => ({
      ruleId,
      count: rows.length,
      allowCount: rows.filter((row) => row.decision === 'allow').length,
      denyCount: rows.filter((row) => row.decision === 'deny').length,
      examples: rows.slice(0, 3),
    }))
    .sort((left, right) => {
      if (right.count !== left.count) return right.count - left.count;
      return left.ruleId.localeCompare(right.ruleId);
    });
}

function readAgentState(chitinDir: string, agent: string): AgentStateSummary | undefined {
  const dbPath = join(chitinDir, 'gov.db');
  if (!existsSync(dbPath)) return undefined;
  const db = new BetterSqlite3(dbPath, { readonly: true });
  try {
    const row = db
      .prepare('SELECT total, locked, locked_ts FROM agent_state WHERE agent = ?')
      .get(agent) as { total: number; locked: number; locked_ts: string | null } | undefined;
    if (!row) return undefined;
    return {
      totalDenials: row.total,
      locked: row.locked === 1,
      lockedTs: row.locked_ts ?? undefined,
      level: escalationLevel(row.total, row.locked === 1),
    };
  } catch {
    return undefined;
  } finally {
    db.close();
  }
}

function escalationLevel(totalDenials: number, locked: boolean): AgentStateSummary['level'] {
  if (locked || totalDenials >= 10) return 'lockdown';
  if (totalDenials >= 7) return 'high';
  if (totalDenials >= 3) return 'elevated';
  return 'normal';
}
