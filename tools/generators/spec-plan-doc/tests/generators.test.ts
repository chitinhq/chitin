import { describe, it, expect, vi, beforeEach } from 'vitest';
import specGenerator, { specTemplate, type SpecOptions } from '../src/spec/index.ts';
import planGenerator, { planTemplate, type PlanOptions } from '../src/plan/index.ts';
import observationGenerator, { observationTemplate, type ObservationOptions } from '../src/observation/index.ts';
import { idToTitle } from '../src/shared.ts';

// ── Shared mock tree factory ──────────────────────────────────────────

function makeMockTree(): { writes: Map<string, string>; root: string; write: (p: string, c: string) => void; exists: () => boolean; read: () => null } {
  const writes = new Map<string, string>();
  return {
    root: '/workspace',
    writes,
    write(filePath: string, content: string) {
      writes.set(filePath, content);
    },
    exists() { return false; },
    read() { return null; },
  };
}

// ── idToTitle ─────────────────────────────────────────────────────────

describe('idToTitle', () => {
  it('capitalizes each segment', () => {
    expect(idToTitle('foo-bar-baz')).toBe('Foo Bar Baz');
  });
  it('single segment', () => {
    expect(idToTitle('dogfood')).toBe('Dogfood');
  });
});

// ── spec generator ────────────────────────────────────────────────────

describe('specTemplate', () => {
  const base: Required<SpecOptions> = {
    id: 'my-feature',
    title: 'My Feature',
    lens: 'da Vinci',
    status: 'draft',
    supersedes: '—',
    tldr: 'Ship the thing.',
  };

  it('contains the title', () => {
    const out = specTemplate(base, '2026-05-03');
    expect(out).toContain('# My Feature — Design Spec');
  });

  it('stamps the date', () => {
    const out = specTemplate(base, '2026-05-03');
    expect(out).toContain('**Date:** 2026-05-03');
  });

  it('includes status, lens, supersedes', () => {
    const out = specTemplate(base, '2026-05-03');
    expect(out).toContain('**Status:** draft');
    expect(out).toContain('**Active soul during design:** da Vinci');
    expect(out).toContain('**Supersedes:** —');
  });

  it('includes tldr content', () => {
    const out = specTemplate(base, '2026-05-03');
    expect(out).toContain('Ship the thing.');
  });

  it('uses placeholder when tldr is empty', () => {
    const out = specTemplate({ ...base, tldr: '' }, '2026-05-03');
    expect(out).toContain('<!-- one-sentence thesis goes here -->');
  });
});

describe('specGenerator', () => {
  it('writes to docs/superpowers/specs/<date>-<id>-design.md', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-05-03T00:00:00Z'));
    const tree = makeMockTree();
    specGenerator(tree, { id: 'my-feature' });
    expect(tree.writes.has('docs/superpowers/specs/2026-05-03-my-feature-design.md')).toBe(true);
    vi.useRealTimers();
  });

  it('defaults lens to da Vinci and status to draft', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-05-03T00:00:00Z'));
    const tree = makeMockTree();
    specGenerator(tree, { id: 'my-feature' });
    const content = tree.writes.get('docs/superpowers/specs/2026-05-03-my-feature-design.md') ?? '';
    expect(content).toContain('**Active soul during design:** da Vinci');
    expect(content).toContain('**Status:** draft');
    vi.useRealTimers();
  });

  it('respects overridden lens and status flags', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-05-03T00:00:00Z'));
    const tree = makeMockTree();
    specGenerator(tree, { id: 'my-feature', lens: 'Knuth', status: 'Approved' });
    const content = tree.writes.get('docs/superpowers/specs/2026-05-03-my-feature-design.md') ?? '';
    expect(content).toContain('**Active soul during design:** Knuth');
    expect(content).toContain('**Status:** Approved');
    vi.useRealTimers();
  });
});

// ── plan generator ────────────────────────────────────────────────────

describe('planTemplate', () => {
  const base: Required<PlanOptions> = {
    id: 'my-plan',
    title: 'My Plan',
    lens: 'da Vinci',
    status: 'draft',
    spec: '',
    tldr: 'Do the work.',
  };

  it('contains the title', () => {
    const out = planTemplate(base, '2026-05-03');
    expect(out).toContain('# My Plan — Implementation Plan');
  });

  it('stamps the date', () => {
    const out = planTemplate(base, '2026-05-03');
    expect(out).toContain('**Date:** 2026-05-03');
  });

  it('includes goal and lens', () => {
    const out = planTemplate(base, '2026-05-03');
    expect(out).toContain('**Goal:** Do the work.');
    expect(out).toContain('**Active soul during planning:** da Vinci');
  });

  it('omits spec line when spec is empty', () => {
    const out = planTemplate(base, '2026-05-03');
    expect(out).not.toContain('**Spec:**');
  });

  it('includes spec line when provided', () => {
    const out = planTemplate({ ...base, spec: 'docs/superpowers/specs/2026-05-03-my-plan-design.md' }, '2026-05-03');
    expect(out).toContain('**Spec:** docs/superpowers/specs/2026-05-03-my-plan-design.md');
  });

  it('includes agentic worker reminder', () => {
    const out = planTemplate(base, '2026-05-03');
    expect(out).toContain('REQUIRED SUB-SKILL');
  });
});

describe('planGenerator', () => {
  it('writes to docs/superpowers/plans/<date>-<id>.md', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-05-03T00:00:00Z'));
    const tree = makeMockTree();
    planGenerator(tree, { id: 'my-plan' });
    expect(tree.writes.has('docs/superpowers/plans/2026-05-03-my-plan.md')).toBe(true);
    vi.useRealTimers();
  });

  it('defaults lens to da Vinci and status to draft', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-05-03T00:00:00Z'));
    const tree = makeMockTree();
    planGenerator(tree, { id: 'my-plan' });
    const content = tree.writes.get('docs/superpowers/plans/2026-05-03-my-plan.md') ?? '';
    expect(content).toContain('**Active soul during planning:** da Vinci');
    expect(content).toContain('**Status:** draft');
    vi.useRealTimers();
  });
});

// ── observation generator ─────────────────────────────────────────────

describe('observationTemplate', () => {
  const base: Required<ObservationOptions> = {
    id: 'my-finding',
    title: 'My Finding',
    lens: 'da Vinci',
    status: 'in-progress',
    type: 'post-mortem',
  };

  it('has frontmatter with date and status', () => {
    const out = observationTemplate(base, '2026-05-03');
    expect(out).toContain('date: 2026-05-03');
    expect(out).toContain('status: in-progress');
  });

  it('includes type and lens in frontmatter', () => {
    const out = observationTemplate(base, '2026-05-03');
    expect(out).toContain('type: post-mortem');
    expect(out).toContain('lens: da Vinci');
  });

  it('contains the title heading', () => {
    const out = observationTemplate(base, '2026-05-03');
    expect(out).toContain('# My Finding');
  });

  it('has Why / Findings / Next steps sections', () => {
    const out = observationTemplate(base, '2026-05-03');
    expect(out).toContain('## Why this exists');
    expect(out).toContain('## Findings');
    expect(out).toContain('## Next steps');
  });
});

describe('observationGenerator', () => {
  it('writes to docs/observations/<date>-<id>.md', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-05-03T00:00:00Z'));
    const tree = makeMockTree();
    observationGenerator(tree, { id: 'my-finding' });
    expect(tree.writes.has('docs/observations/2026-05-03-my-finding.md')).toBe(true);
    vi.useRealTimers();
  });

  it('defaults lens to da Vinci, status to in-progress, type to observation', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-05-03T00:00:00Z'));
    const tree = makeMockTree();
    observationGenerator(tree, { id: 'my-finding' });
    const content = tree.writes.get('docs/observations/2026-05-03-my-finding.md') ?? '';
    expect(content).toContain('lens: da Vinci');
    expect(content).toContain('status: in-progress');
    expect(content).toContain('type: observation');
    vi.useRealTimers();
  });

  it('respects overridden flags', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-05-03T00:00:00Z'));
    const tree = makeMockTree();
    observationGenerator(tree, { id: 'my-finding', lens: 'Knuth', status: 'complete', type: 'ab-test' });
    const content = tree.writes.get('docs/observations/2026-05-03-my-finding.md') ?? '';
    expect(content).toContain('lens: Knuth');
    expect(content).toContain('type: ab-test');
    vi.useRealTimers();
  });
});
