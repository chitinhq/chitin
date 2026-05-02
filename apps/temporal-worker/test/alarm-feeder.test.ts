import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  alarmEntryId,
  alarmSignature,
  appendBacklogEntry,
  parseBacklogIds,
  readLatestRollup,
  renderAlarmEntry,
  runAlarmFeeder,
} from '../src/alarm-feeder.ts';

// ─── alarmSignature ──────────────────────────────────────────────────────

describe('alarmSignature', () => {
  it('extracts a stable kind-id from the leading phrase before colon', () => {
    expect(alarmSignature('BUCKET-B REGRESSION: 1/19 runs contaminated (5.3%)')).toBe(
      'bucket-b-regression',
    );
    expect(alarmSignature('SUCCESS RATE DROP: claude-code-headless tier T2 fell to 60%')).toBe(
      'success-rate-drop',
    );
  });

  it('numbers in the middle do not change the signature (rate variations dedup)', () => {
    expect(alarmSignature('BUCKET-B REGRESSION: 1/19 runs contaminated (5.3%)')).toBe(
      alarmSignature('BUCKET-B REGRESSION: 2/30 runs contaminated (6.7%)'),
    );
  });

  it("falls back to the first 60 chars when no colon is present", () => {
    const sig = alarmSignature('Plain alarm with no colon delimiter');
    expect(sig).toBe('plain-alarm-with-no-colon-delimiter');
  });
});

describe('alarmEntryId', () => {
  it("prefixes signature with 'investigate-'", () => {
    expect(alarmEntryId('bucket-b-regression')).toBe('investigate-bucket-b-regression');
  });
});

// ─── parseBacklogIds ─────────────────────────────────────────────────────

describe('parseBacklogIds', () => {
  it("extracts ids from `### \\`<id>\\`` headings", () => {
    const md = "# Backlog\n\n### `entry-one`\n\n### `entry-two`\n";
    expect(parseBacklogIds(md)).toEqual(new Set(['entry-one', 'entry-two']));
  });

  it("returns empty for backlog without ### headings", () => {
    expect(parseBacklogIds('# Backlog\n\nintro\n')).toEqual(new Set());
  });
});

// ─── readLatestRollup ────────────────────────────────────────────────────

describe('readLatestRollup', () => {
  let scratch: string;

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'alarm-feeder-rollup-test-'));
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  it("returns undefined when the rollup dir is missing", () => {
    expect(readLatestRollup(join(scratch, 'nope'))).toBeUndefined();
  });

  it("returns undefined when no rollup files exist", () => {
    expect(readLatestRollup(scratch)).toBeUndefined();
  });

  it("reads alarms from the lex-newest JSON", () => {
    writeFileSync(join(scratch, '2026-04-30.json'), JSON.stringify({ alarms: ['old'] }), 'utf8');
    writeFileSync(join(scratch, '2026-05-02.json'), JSON.stringify({ alarms: ['new', 'fresh'] }), 'utf8');
    const r = readLatestRollup(scratch);
    expect(r?.alarms).toEqual(['new', 'fresh']);
  });

  it("returns undefined on malformed JSON (does not throw)", () => {
    writeFileSync(join(scratch, '2026-05-02.json'), 'not json', 'utf8');
    expect(readLatestRollup(scratch)).toBeUndefined();
  });
});

// ─── renderAlarmEntry ────────────────────────────────────────────────────

describe('renderAlarmEntry', () => {
  it("renders an in_design backlog entry with role:researcher", () => {
    const out = renderAlarmEntry(
      'BUCKET-B REGRESSION: 1/19 runs contaminated',
      'investigate-bucket-b-regression',
      '2026-05-02T18:00:00Z',
    );
    expect(out).toContain('### `investigate-bucket-b-regression`');
    expect(out).toContain('role: researcher');
    expect(out).toContain('status: in_design');
    expect(out).toContain('BUCKET-B REGRESSION');
    expect(out).toContain('chitin-alarm-feeder.timer');
  });
});

// ─── runAlarmFeeder ──────────────────────────────────────────────────────

describe('runAlarmFeeder', () => {
  let scratch: string;
  let backlogPath: string;
  let rollupsDir: string;
  let logs: string[];

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'alarm-feeder-test-'));
    rollupsDir = join(scratch, 'rollups');
    backlogPath = join(scratch, 'swarm-backlog.md');
    logs = [];
    writeFileSync(backlogPath, '# Swarm Backlog\n\n## Entries\n\n', 'utf8');
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  function writeRollup(name: string, alarms: string[]) {
    try {
      mkdtempSync(rollupsDir);
    } catch {
      // dir might exist; handled by node fs
    }
    // Ensure dir exists.
    const fs = require('node:fs') as typeof import('node:fs');
    fs.mkdirSync(rollupsDir, { recursive: true });
    writeFileSync(join(rollupsDir, name), JSON.stringify({ alarms }), 'utf8');
  }

  it("does nothing when the rollup directory doesn't exist", async () => {
    const r = await runAlarmFeeder({
      rollupsDir,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      log: (l) => logs.push(l),
    });
    expect(r.rollup_present).toBe(false);
    expect(r.new_entries).toBe(0);
    expect(r.total_alarms).toBe(0);
    expect(readFileSync(backlogPath, 'utf8')).toContain('## Entries');
  });

  it("does nothing when the rollup has no alarms", async () => {
    writeRollup('2026-05-02.json', []);
    const r = await runAlarmFeeder({
      rollupsDir,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      log: (l) => logs.push(l),
    });
    expect(r.rollup_present).toBe(true);
    expect(r.new_entries).toBe(0);
    expect(r.total_alarms).toBe(0);
  });

  it("files a backlog entry per new alarm", async () => {
    writeRollup('2026-05-02.json', [
      'BUCKET-B REGRESSION: 1/19 runs contaminated (5.3%)',
      'SUCCESS RATE DROP: copilot tier T2 fell to 50%',
    ]);
    const r = await runAlarmFeeder({
      rollupsDir,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(2);
    const md = readFileSync(backlogPath, 'utf8');
    expect(md).toContain('### `investigate-bucket-b-regression`');
    expect(md).toContain('### `investigate-success-rate-drop`');
    expect(md).toContain('role: researcher');
    // Telemetry shape
    const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
    expect(parsed.component).toBe('alarm-feeder');
    expect(parsed.new_entries).toBe(2);
  });

  it("dedups against existing backlog entries (idempotent re-runs)", async () => {
    writeRollup('2026-05-02.json', ['BUCKET-B REGRESSION: 1/19 contaminated']);
    writeFileSync(
      backlogPath,
      "# Backlog\n\n### `investigate-bucket-b-regression`\n\nalready filed\n",
      'utf8',
    );
    const r = await runAlarmFeeder({
      rollupsDir,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(0);
    expect(r.duplicates_skipped).toBe(1);
  });

  it("dedups within a run when two alarms share the same kind-signature", async () => {
    writeRollup('2026-05-02.json', [
      'BUCKET-B REGRESSION: 1/19 contaminated (5%)',
      'BUCKET-B REGRESSION: 2/30 contaminated (7%)',
    ]);
    const r = await runAlarmFeeder({
      rollupsDir,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(1);
    expect(r.duplicates_skipped).toBe(1);
  });

  it("caps entries written per run", async () => {
    writeRollup('2026-05-02.json', [
      'ALARM ONE: x',
      'ALARM TWO: y',
      'ALARM THREE: z',
      'ALARM FOUR: w',
    ]);
    const r = await runAlarmFeeder({
      rollupsDir,
      backlogPath,
      cap: 2,
      now: '2026-05-02T18:00:00Z',
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(2);
  });

  it("doesn't touch the backlog when nothing was filed", async () => {
    writeRollup('2026-05-02.json', []);
    const before = readFileSync(backlogPath, 'utf8');
    await runAlarmFeeder({
      rollupsDir,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      log: (l) => logs.push(l),
    });
    expect(readFileSync(backlogPath, 'utf8')).toBe(before);
  });
});

// ─── appendBacklogEntry sanity ───────────────────────────────────────────

describe('appendBacklogEntry', () => {
  it("appends rendered entry at the end", () => {
    const md = '# x\n\n### `existing`\n\nblah\n';
    const out = appendBacklogEntry(md, '### `new`\n\nfresh\n');
    expect(out).toContain('existing');
    expect(out).toContain('new');
    expect(out.indexOf('new')).toBeGreaterThan(out.indexOf('existing'));
  });
});
