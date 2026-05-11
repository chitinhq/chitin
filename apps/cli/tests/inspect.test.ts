import { describe, expect, it } from 'vitest';
import {
  parseSince,
  renderAgentSummary,
  renderDecisionRows,
  renderDenialRows,
  renderRuleSummaries,
  renderSessionTimeline,
} from '../src/commands/inspect.js';

describe('inspect renderers', () => {
  const rows = [
    {
      ts: '2026-05-11T18:25:43Z',
      allowed: false,
      decision: 'deny' as const,
      mode: 'enforce',
      ruleId: 'governance-mutation-authority-required',
      reason: 'blocked',
      suggestion: 'Ask the operator.',
      agent: 'codex',
      driver: 'codex',
      actionType: 'shell.exec',
      actionTarget: 'chitin-kernel --help',
    },
    {
      ts: '2026-05-11T18:25:44Z',
      allowed: true,
      decision: 'allow' as const,
      mode: 'monitor',
      ruleId: 'router-heuristic:allow',
      agent: 'claude-code',
      driver: 'claude-code',
      actionType: 'router.signal',
      actionTarget: 'Bash:git status',
      signals: { predictedBlast: 0.275, flounderingScore: 0.85 },
    },
  ];

  it('renders a compact live decision view', () => {
    const out = renderDecisionRows(rows);
    expect(out).toContain('2026-05-11T18:25:43Z deny  codex');
    expect(out).toContain('governance-mutation-authority-required');
    expect(out).toContain('blast=0.275 flounder=0.85');
  });

  it('renders denials with reason and suggestion', () => {
    const out = renderDenialRows(rows);
    expect(out).toContain('governance-mutation-authority-required');
    expect(out).toContain('blocked');
    expect(out).toContain('Ask the operator.');
    expect(out).not.toContain('router-heuristic:allow');
  });

  it('parses relative since durations', () => {
    const now = new Date('2026-05-11T18:30:00Z');
    expect(parseSince('5m', now)?.toISOString()).toBe('2026-05-11T18:25:00.000Z');
    expect(parseSince('2h', now)?.toISOString()).toBe('2026-05-11T16:30:00.000Z');
    expect(parseSince('1d', now)?.toISOString()).toBe('2026-05-10T18:30:00.000Z');
    expect(() => parseSince('soon', now)).toThrow(/invalid --since/);
  });

  it('renders session timeline and chain health', () => {
    const out = renderSessionTimeline({
      chainId: 'chain-1',
      events: [
        {
          ts: '2026-05-11T18:25:42Z',
          seq: 0,
          eventType: 'session_start',
          surface: 'codex',
          sessionId: 'sess-1',
          chainId: 'chain-1',
          prevHash: null,
          thisHash: 'a'.repeat(64),
          labels: {},
          payload: {
            decision: 'allow',
            rule_id: 'default-allow-shell',
            action_type: 'shell.exec',
            action_target: 'pwd',
            predicted_blast: 0.275,
          },
        },
      ],
      chainHealth: {
        ok: false,
        breaks: [{ seq: 1, expectedPrevHash: 'a'.repeat(64), actualPrevHash: 'bad' }],
      },
    });
    expect(out).toContain('chain chain-1');
    expect(out).toContain('health broken');
    expect(out).toContain('seq=0');
    expect(out).toContain('allow default-allow-shell shell.exec pwd blast=0.275');
    expect(out).toContain('break seq=1 expected=a'.repeat(1));
  });

  it('renders agent summary with sticky state and top rules', () => {
    const out = renderAgentSummary({
      agent: 'codex',
      allowCount: 4,
      decisionCount: 6,
      denyCount: 2,
      recentDecisions: rows,
      rules: [
        {
          allowCount: 1,
          count: 2,
          denyCount: 1,
          examples: rows,
          ruleId: 'default-allow-shell',
        },
      ],
      state: {
        level: 'high',
        locked: false,
        totalDenials: 7,
      },
    });
    expect(out).toContain('agent codex');
    expect(out).toContain('decisions=6 allow=4 deny=2');
    expect(out).toContain('state=high total_denials=7 locked=false');
    expect(out).toContain('default-allow-shell count=2 allow=1 deny=1');
  });

  it('renders rule summaries with examples', () => {
    const out = renderRuleSummaries([
      {
        allowCount: 1,
        count: 2,
        denyCount: 1,
        examples: rows,
        ruleId: 'default-allow-shell',
      },
    ]);
    expect(out).toContain('default-allow-shell count=2 allow=1 deny=1');
    expect(out).toContain('example codex shell.exec chitin-kernel --help');
  });
});
