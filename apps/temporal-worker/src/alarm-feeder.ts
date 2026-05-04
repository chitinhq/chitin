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
//
// §systemd-unit-failure: In addition to rollup alarms, the feeder also
// scans recent journalctl output for chitin-* units that exited with
// status=200/CHDIR or status=203/EXEC. Each detected failure emits a
// chain alarm event with kind=systemd-unit-failure and is fed into the
// same dedup+backlog pipeline as rollup alarms.

import { fileURLToPath } from 'node:url';
import { readFile, writeFile } from 'node:fs/promises';
import { readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { homedir } from 'node:os';
import { execSync } from 'node:child_process';

// ─── Types ────────────────────────────────────────────────────────────────

export interface AlarmFeederOptions {
  rollupsDir: string;
  backlogPath: string;
  cap: number;
  /** ISO-8601 timestamp injected by runner. */
  now: string;
  log?: (line: string) => void;
  /**
   * Synthetic journal text for testing. When set, journalctl is NOT
   * executed; this text is parsed instead. In production the feeder
   * runs journalctl automatically when this field is absent.
   */
  journalOutput?: string;
}

export interface AlarmFeederResult {
  total_alarms: number;
  new_entries: number;
  duplicates_skipped: number;
  rollup_present: boolean;
  /** How many chitin-* systemd unit failures were detected in the journal. */
  systemd_failures: number;
}

// ─── Systemd unit failure detection ─────────────────────────────────────

/**
 * Structured chain alarm event emitted when a chitin-* systemd unit
 * exits with a non-zero status that indicates a setup failure rather
 * than a transient workload exit (200/CHDIR = missing working directory,
 * 203/EXEC = binary not found/not executable).
 */
export interface SystemdUnitFailureEvent {
  kind: 'systemd-unit-failure';
  unit: string;
  status_code: string;
  failure_ts: string;
}

// Invariant: a line matches iff it references a chitin-* unit AND
// contains one of the two failure status codes. status=0/SUCCESS never
// matches because neither "200/CHDIR" nor "203/EXEC" appear in that string.
const FAILURE_STATUS_RE = /status=(200\/CHDIR|203\/EXEC)/;
const UNIT_FROM_LINE_RE = /(chitin-[^\s:]+):/;

// Short-format journal timestamp: "May  4 10:23:45" (month padded by a space
// for single-digit days). We capture it verbatim; callers receive it as-is.
const JOURNAL_TS_RE = /^([A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2})/;

/**
 * Parse raw journalctl short-format output and return one
 * `SystemdUnitFailureEvent` per line that matches both a chitin-* unit
 * reference and a 200/CHDIR or 203/EXEC exit status.
 *
 * Pure function — safe for unit tests with synthetic input.
 */
export function parseJournalForUnitFailures(output: string): SystemdUnitFailureEvent[] {
  const events: SystemdUnitFailureEvent[] = [];
  for (const line of output.split('\n')) {
    const statusMatch = FAILURE_STATUS_RE.exec(line);
    if (!statusMatch) continue;
    const unitMatch = UNIT_FROM_LINE_RE.exec(line);
    if (!unitMatch) continue;
    const tsMatch = JOURNAL_TS_RE.exec(line);
    events.push({
      kind: 'systemd-unit-failure',
      unit: unitMatch[1],
      status_code: statusMatch[1],
      failure_ts: tsMatch ? tsMatch[1] : new Date().toISOString(),
    });
  }
  return events;
}

/**
 * Run journalctl and return its stdout. Returns empty string if the
 * command is unavailable (e.g., macOS dev machines) or fails.
 */
export function collectJournalOutput(sinceHours = 2): string {
  try {
    return execSync(
      `journalctl --user -u "chitin-*" --since "${sinceHours} hours ago" -o short --no-pager`,
      { encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] },
    );
  } catch {
    return '';
  }
}

/**
 * Convert a `SystemdUnitFailureEvent` to a human-readable alarm string
 * compatible with the rollup-alarm pipeline (dedup + backlog filing).
 * Invariant: two failures of the SAME unit with ANY status code collapse
 * to the same `alarmSignature` — one backlog entry per unit, not per event.
 */
export function systemdFailureToAlarm(ev: SystemdUnitFailureEvent): string {
  return `SYSTEMD UNIT FAILURE ${ev.unit}: exited with status=${ev.status_code} at ${ev.failure_ts}`;
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
 * groomer + downstream chain will pick up. Uses `role: analyst` —
 * alarms surface from the swarm's INTERNAL telemetry (rollup,
 * gov-decisions, swarm_runs), and processing internal telemetry is
 * the analyst's scope per the design's §3 row. Researcher role is
 * for EXTERNAL signal pulls (arxiv / reddit / openclaw) — separate
 * lane.
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
    'role: analyst',
    '```',
    '',
    `Auto-filed by chitin-alarm-feeder.timer at ${now} from a swarm-rollup alarm:`,
    '',
    `> ${alarm}`,
    '',
    'Analyst role: use `python/analysis/` to read the latest swarm-rollup JSON at `~/.cache/chitin/swarm-rollups/<YYYY-MM-DD>.json` + the events-jsonl chain; identify the root cause (recent dispatch failures, driver regressions, governance edits, etc); write a markdown report to `python/analysis/out/<entry-id>.md` and emit a `<<<ANALYSIS>>>` JSON line with root_cause + recommended_action. Operator: groom this entry once it has a real `tier` / `file:` / `estimated_loc`.',
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

  // ── Rollup alarms ───────────────────────────────────────────────────
  const rollup = readLatestRollup(opts.rollupsDir);
  const rollupAlarms = rollup?.alarms ?? [];

  // ── Systemd journal scan ────────────────────────────────────────────
  const journalText =
    opts.journalOutput !== undefined ? opts.journalOutput : collectJournalOutput();
  const unitFailures = parseJournalForUnitFailures(journalText);

  // Emit a structured chain alarm event for each detected unit failure so
  // downstream consumers (slack-feeder, log processors) can route on it.
  for (const ev of unitFailures) {
    log(JSON.stringify({ ts: new Date().toISOString(), component: 'alarm-feeder', ...ev }));
  }

  // Convert unit failures to alarm strings and merge with rollup alarms.
  const systemdAlarms = unitFailures.map(systemdFailureToAlarm);
  const alarms = [...rollupAlarms, ...systemdAlarms];

  // ── Backlog dedup + filing ──────────────────────────────────────────
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
    systemd_failures: unitFailures.length,
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
  FAILURE_STATUS_RE,
  UNIT_FROM_LINE_RE,
};
