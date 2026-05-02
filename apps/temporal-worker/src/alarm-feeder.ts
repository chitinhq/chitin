// §7 telemetry → backlog flywheel.
//
// The daily rollup (chitin-swarm-rollup.timer) writes
// ~/.cache/chitin/swarm-rollups/<YYYY-MM-DD>.json including an
// `alarms` array — natural-language strings the rollup detector
// fires when something regresses (bucket-B re-appears, success-rate
// dips, dispatch volume craters, etc).
//
// Today: alarms post to Slack and live in the rollup JSON. Operator
// reads, decides, files an investigation entry by hand.
//
// This module: closes that loop. A daily timer reads the latest
// rollup, dedups alarms against existing investigate-* backlog
// entries, drafts an in_design entry per new alarm with
// role:researcher (so the existing groomer + downstream chain can
// pick it up). Cap is conservative (default 1/day) — alarm bursts
// shouldn't flood the backlog.
//
// Pattern matches researcher.ts / lessons.ts / debt-curator.ts /
// groomer.ts: cron-fired script, idempotent, telemetry log line,
// no Temporal involvement. The rollup is a flat-file boundary; this
// module reads it.

import { fileURLToPath } from 'node:url';
import { readFile, writeFile } from 'node:fs/promises';
import { readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { homedir } from 'node:os';

// ─── Types ────────────────────────────────────────────────────────────────

export interface AlarmFeederOptions {
  rollupsDir: string;
  backlogPath: string;
  cap: number;
  /** ISO-8601 timestamp injected by runner. */
  now: string;
  log?: (line: string) => void;
}

export interface AlarmFeederResult {
  total_alarms: number;
  new_entries: number;
  duplicates_skipped: number;
  rollup_present: boolean;
}

interface RollupShape {
  alarms?: string[];
  // The other fields are read by the gatekeeper; we only need alarms.
}

// ─── Alarm signature → entry id ──────────────────────────────────────────

/**
 * Derive a stable id from an alarm string. We don't want a small
 * wording change ("5.3%" → "6.1%") to file a new entry, so the
 * signature is the FIRST WORDS of the alarm before any metric — the
 * "kind" of alarm, not the specific numbers.
 *
 * Examples:
 *   "BUCKET-B REGRESSION: 1/19 runs contaminated (5.3%) — PR #123 ..."
 *     → "bucket-b-regression"
 *   "SUCCESS RATE DROP: claude-code-headless tier T2 fell to 60% ..."
 *     → "success-rate-drop"
 */
export function alarmSignature(alarm: string): string {
  // Take the leading uppercase phrase up to the first colon, normalize.
  const colonIdx = alarm.indexOf(':');
  const head = colonIdx >= 0 ? alarm.slice(0, colonIdx) : alarm.slice(0, 60);
  return head
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60);
}

export function alarmEntryId(signature: string): string {
  return `investigate-${signature}`;
}

// ─── Backlog id parsing (mirror of groomer.ts) ───────────────────────────

const BACKLOG_ID_RE = /^### `([^`]+)`/gm;

export function parseBacklogIds(text: string): Set<string> {
  const ids = new Set<string>();
  for (const match of text.matchAll(BACKLOG_ID_RE)) {
    ids.add(match[1]);
  }
  return ids;
}

// ─── Rollup reading ──────────────────────────────────────────────────────

/**
 * Read the most-recent rollup JSON from `rollupsDir`. Returns
 * { rollup: RollupShape } when found, { rollup: undefined } when
 * absent or unreadable. Tests inject a tmpdir with synthetic JSON.
 */
export function readLatestRollup(rollupsDir: string): RollupShape | undefined {
  let names: string[];
  try {
    names = readdirSync(rollupsDir).filter((n) => n.endsWith('.json'));
  } catch {
    return undefined;
  }
  if (names.length === 0) return undefined;
  names.sort();
  const latest = names[names.length - 1];
  try {
    const raw = readFileSync(resolve(rollupsDir, latest), 'utf8');
    return JSON.parse(raw) as RollupShape;
  } catch {
    return undefined;
  }
}

// ─── Drafted entry shape ─────────────────────────────────────────────────

/**
 * Render a draft `in_design` backlog entry that the existing
 * groomer + downstream chain will pick up. Uses role:researcher so
 * the resulting workflow goes through the researcher prompt
 * template (PR #143) — alarms are usually "what regressed?
 * investigate" tasks, which is researcher-shape.
 */
export function renderAlarmEntry(alarm: string, entryId: string, now: string): string {
  return [
    `### \`${entryId}\``,
    '',
    '```yaml',
    `id: ${entryId}`,
    'tier: TBD',
    'status: in_design',
    'estimated_loc: TBD',
    'blocks: []',
    'file: TBD',
    'references_signal: chitin-swarm-rollup alarms',
    'role: researcher',
    '```',
    '',
    `Auto-filed by chitin-alarm-feeder.timer at ${now} from a swarm-rollup alarm:`,
    '',
    `> ${alarm}`,
    '',
    'Researcher role: read the alarm + the latest swarm-rollup JSON at `~/.cache/chitin/swarm-rollups/<YYYY-MM-DD>.json`; identify the root cause (recent dispatch failures, driver regressions, governance edits, etc); propose either a fix entry or `status: needs_human` if the cause is non-obvious. Operator: groom this entry once it has a real `tier` / `file:` / `estimated_loc`.',
    '',
  ].join('\n');
}

export function appendBacklogEntry(text: string, rendered: string): string {
  const trimmed = text.replace(/\s+$/, '');
  return `${trimmed}\n\n${rendered}`;
}

// ─── Runner ──────────────────────────────────────────────────────────────

export async function runAlarmFeeder(
  opts: AlarmFeederOptions,
): Promise<AlarmFeederResult> {
  const log = opts.log ?? ((l: string) => console.log(l));

  const rollup = readLatestRollup(opts.rollupsDir);
  const alarms = rollup?.alarms ?? [];

  const backlog = await readFile(opts.backlogPath, 'utf8').catch(() => '');
  const knownIds = parseBacklogIds(backlog);

  let next = backlog;
  let newEntries = 0;
  let duplicates_skipped = 0;
  const seenInRun = new Set<string>();
  for (const alarm of alarms) {
    if (newEntries >= opts.cap) break;
    const sig = alarmSignature(alarm);
    if (!sig) continue;
    const entryId = alarmEntryId(sig);
    if (knownIds.has(entryId) || seenInRun.has(entryId)) {
      duplicates_skipped++;
      continue;
    }
    seenInRun.add(entryId);
    next = appendBacklogEntry(next, renderAlarmEntry(alarm, entryId, opts.now));
    newEntries++;
  }

  if (newEntries > 0) {
    await writeFile(opts.backlogPath, next, 'utf8');
  }

  const result: AlarmFeederResult = {
    total_alarms: alarms.length,
    new_entries: newEntries,
    duplicates_skipped,
    rollup_present: rollup !== undefined,
  };

  log(
    JSON.stringify({
      ts: new Date().toISOString(),
      component: 'alarm-feeder',
      ...result,
    }),
  );

  return result;
}

// ─── Main ────────────────────────────────────────────────────────────────

const DEFAULT_ROLLUPS_DIR = resolve(homedir(), '.cache/chitin/swarm-rollups');
const DEFAULT_BACKLOG_PATH = resolve(process.cwd(), 'docs/swarm-backlog.md');
const DEFAULT_CAP = 1;

async function main(): Promise<void> {
  const cap = Number(process.env.CHITIN_ALARM_FEEDER_CAP ?? String(DEFAULT_CAP));
  await runAlarmFeeder({
    rollupsDir: DEFAULT_ROLLUPS_DIR,
    backlogPath: DEFAULT_BACKLOG_PATH,
    cap,
    now: new Date().toISOString(),
  });
}

const isMain = process.argv[1] === fileURLToPath(import.meta.url);
if (isMain) {
  main().catch((err) => {
    console.error(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'error',
        component: 'alarm-feeder',
        msg: 'alarm-feeder tick fatal',
        error: err instanceof Error ? err.message : String(err),
      }),
    );
    process.exit(1);
  });
}

export const __test__ = {
  BACKLOG_ID_RE,
  DEFAULT_CAP,
};
