import { afterEach, describe, expect, it } from 'vitest';
import BetterSqlite3 from 'better-sqlite3';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  getAgentSummary,
  getSessionTimeline,
  listDecisions,
  listRuleSummaries,
} from '../src/inspect-queries.js';

const tempDirs: string[] = [];
afterEach(() => {
  for (const d of tempDirs) rmSync(d, { recursive: true, force: true });
  tempDirs.length = 0;
});

function tempDir(prefix: string): string {
  const d = mkdtempSync(join(tmpdir(), prefix));
  tempDirs.push(d);
  return d;
}

function writeDecisionLog(chitinDir: string): void {
  const rows = [
    {
      allowed: true,
      mode: 'enforce',
      rule_id: 'default-allow-shell',
      reason: 'allowed',
      agent: 'codex',
      driver: 'codex',
      action_type: 'shell.exec',
      action_target: 'pwd',
      ts: '2026-05-11T18:25:42Z',
    },
    {
      allowed: false,
      mode: 'enforce',
      rule_id: 'governance-mutation-authority-required',
      reason: 'blocked',
      suggestion: 'Ask the operator.',
      agent: 'codex',
      driver: 'codex',
      action_type: 'shell.exec',
      action_target: 'chitin-kernel --help',
      ts: '2026-05-11T18:25:43Z',
    },
    {
      allowed: true,
      mode: 'monitor',
      rule_id: 'router-heuristic:allow',
      agent: 'claude-code',
      driver: 'claude-code',
      action_type: 'router.signal',
      action_target: 'Bash:git status',
      predicted_blast: 0.275,
      floundering_score: 0.85,
      ts: '2026-05-11T18:25:44Z',
    },
  ];
  writeFileSync(
    join(chitinDir, 'gov-decisions-2026-05-11.jsonl'),
    rows.map((r) => JSON.stringify(r)).join('\n') + '\n',
  );
}

function event(overrides: Record<string, unknown>) {
  return {
    schema_version: '2',
    run_id: 'run-1',
    session_id: 'sess-1',
    surface: 'codex',
    driver_identity: { user: 'u', machine_id: 'm', machine_fingerprint: 'a'.repeat(64) },
    agent_instance_id: 'codex',
    parent_agent_id: null,
    agent_fingerprint: 'b'.repeat(64),
    event_type: 'decision',
    chain_id: 'chain-1',
    chain_type: 'session',
    parent_chain_id: null,
    seq: 0,
    prev_hash: null,
    this_hash: '0'.repeat(64),
    ts: '2026-05-11T18:25:42Z',
    labels: {},
    payload: {},
    ...overrides,
  };
}

describe('inspect queries', () => {
  it('lists decisions with filters, newest first, and exposes router scores', () => {
    const dir = tempDir('chitin-inspect-');
    writeDecisionLog(dir);

    const denied = listDecisions(dir, { decision: 'deny', agent: 'codex' });
    expect(denied).toHaveLength(1);
    expect(denied[0]).toMatchObject({
      allowed: false,
      decision: 'deny',
      ruleId: 'governance-mutation-authority-required',
      suggestion: 'Ask the operator.',
      driver: 'codex',
    });

    const signals = listDecisions(dir, { actionType: 'router.signal' });
    expect(signals).toHaveLength(1);
    expect(signals[0].signals).toEqual({
      predictedBlast: 0.275,
      flounderingScore: 0.85,
    });
  });

  it('returns a session timeline with chain continuity status', () => {
    const dir = tempDir('chitin-inspect-');
    writeFileSync(
      join(dir, 'events-run-1.jsonl'),
      [
        event({ seq: 0, prev_hash: null, this_hash: 'a'.repeat(64), event_type: 'session_start' }),
        event({ seq: 1, prev_hash: 'a'.repeat(64), this_hash: 'b'.repeat(64), event_type: 'decision' }),
        event({ seq: 2, prev_hash: 'not-the-tail', this_hash: 'c'.repeat(64), event_type: 'session_end' }),
      ].map((r) => JSON.stringify(r)).join('\n') + '\n',
    );

    const timeline = getSessionTimeline(dir, 'chain-1');
    expect(timeline.chainId).toBe('chain-1');
    expect(timeline.events.map((e) => e.eventType)).toEqual(['session_start', 'decision', 'session_end']);
    expect(timeline.chainHealth.ok).toBe(false);
    expect(timeline.chainHealth.breaks).toEqual([
      {
        seq: 2,
        expectedPrevHash: 'b'.repeat(64),
        actualPrevHash: 'not-the-tail',
      },
    ]);
  });

  it('summarizes an agent from decisions and read-only gov.db state', () => {
    const dir = tempDir('chitin-inspect-');
    writeDecisionLog(dir);
    const db = new BetterSqlite3(join(dir, 'gov.db'));
    db.exec(`
      CREATE TABLE agent_state (
        agent TEXT PRIMARY KEY,
        total INTEGER NOT NULL DEFAULT 0,
        locked INTEGER NOT NULL DEFAULT 0,
        locked_ts TEXT
      );
      INSERT INTO agent_state (agent, total, locked, locked_ts)
      VALUES ('codex', 7, 0, NULL);
    `);
    db.close();

    const summary = getAgentSummary(dir, 'codex');
    expect(summary).toMatchObject({
      agent: 'codex',
      allowCount: 1,
      denyCount: 1,
      decisionCount: 2,
      state: { level: 'high', locked: false, totalDenials: 7 },
    });
    expect(summary.rules[0]).toMatchObject({
      ruleId: 'default-allow-shell',
      count: 1,
    });
  });

  it('summarizes rule hits with examples', () => {
    const dir = tempDir('chitin-inspect-');
    writeDecisionLog(dir);

    const summaries = listRuleSummaries(dir);
    expect(summaries[0]).toMatchObject({
      ruleId: 'default-allow-shell',
      count: 1,
      allowCount: 1,
      denyCount: 0,
    });
    expect(summaries.map((summary) => summary.ruleId)).toContain('governance-mutation-authority-required');
    expect(summaries[0].examples[0]).toMatchObject({
      actionType: 'shell.exec',
      agent: 'codex',
    });
  });
});
