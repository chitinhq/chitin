import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import Database from 'better-sqlite3';
import type { Command } from 'commander';

const LEDGER_REL = 'docs/observations/governance-debt-ledger.md';

export interface LintFinding {
  level: 'error' | 'warn';
  gdl: string;
  msg: string;
}

export interface LedgerEntry {
  id: string;
  body: string;
}

export interface StubInput {
  lane: string;
  chain?: string;
  seq?: string;
  hash?: string;
  surface: string;
  repo: string;
  soul: string;
  today: string;
}

export function registerLedger(program: Command): void {
  const ledger = program.command('ledger').description('Governance-debt ledger tools');

  ledger
    .command('new')
    .description('Append a new stub entry to the ledger')
    .argument('<lane>', 'lane: 1 | 2 | 3 (fix / determinism / soul-routing)')
    .option('--chain <id>', 'chain_id reference (stable)')
    .option('--seq <n>', 'seq number within chain')
    .option('--hash <sha>', 'this_hash (first 12 chars are kept)')
    .option('--surface <s>', 'surface (e.g. claude-code)', 'claude-code')
    .option('--repo <name>', 'repo name (e.g. chitin)', 'chitin')
    .option('--soul <id>', 'active soul id', 'davinci')
    .option('--ledger <path>', 'ledger path — resolved relative to cwd unless absolute', LEDGER_REL)
    .action(handleNew);

  ledger
    .command('lint')
    .description('Validate ledger entry integrity')
    .option('--ledger <path>', 'ledger path — resolved relative to cwd unless absolute', LEDGER_REL)
    .option('--db <path>', 'events.db path for trace_ref resolution')
    .option('--strict', 'fail on unresolved trace refs', false)
    .action(handleLint);
}

function handleNew(
  laneArg: string,
  opts: {
    chain?: string;
    seq?: string;
    hash?: string;
    surface: string;
    repo: string;
    soul: string;
    ledger: string;
  },
): void {
  const body = readFileSync(opts.ledger, 'utf8');
  const nextId = nextGDL(body);
  const today = new Date().toISOString().slice(0, 10);
  const entry = buildStubEntry(nextId, {
    lane: laneArg,
    chain: opts.chain,
    seq: opts.seq,
    hash: opts.hash,
    surface: opts.surface,
    repo: opts.repo,
    soul: opts.soul,
    today,
  });
  writeFileSync(opts.ledger, body.trimEnd() + '\n' + entry);
  console.log(`appended GDL-${pad(nextId)} to ${opts.ledger}`);
}

function handleLint(opts: { ledger: string; db?: string; strict: boolean }): void {
  const body = readFileSync(opts.ledger, 'utf8');
  const blocks = splitEntries(body);

  const findings: LintFinding[] = [
    ...lintStructural(body, blocks),
    ...lintTraceRefs(blocks, opts.db, opts.strict),
    ...lintGraduationMarkers(blocks),
  ];

  for (const f of findings) {
    const tag = f.level === 'error' ? 'ERROR' : 'WARN';
    console.log(`[${tag}]  ${f.gdl.padEnd(10)} ${f.msg}`);
  }
  const errors = findings.filter((f) => f.level === 'error').length;
  console.log(`\n${findings.length} findings (${errors} errors)`);
  process.exit(errors > 0 ? 1 : 0);
}

export function buildStubEntry(id: number, inp: StubInput): string {
  const laneLabel = laneLabelFor(inp.lane);
  return [
    '',
    `### GDL-${pad(id)} — <one-line what the platform should have caught>`,
    '',
    `- **Observed:** ${inp.today}, chain \`${inp.chain ?? '<chain_id>'}\`, seq \`${inp.seq ?? '<n>'}\`, hash \`${(inp.hash ?? '<this_hash>').slice(0, 12)}\``,
    `- **Surface / repo:** ${inp.surface} / ${inp.repo}`,
    '- **Finding:** <what happened, one paragraph>',
    `- **Lane:** ${laneLabel}`,
    '- **Severity:** low / medium / high',
    '- **Graduated:** <null>',
    `- **Soul active:** ${inp.soul} @ <soul_hash[:8]>`,
    '',
  ].join('\n');
}

export function laneLabelFor(lane: string): string {
  const l = lane.toLowerCase();
  if (lane === '1' || l === 'fix') return '① FIX';
  if (lane === '2' || l === 'determinism') return '② DETERMINISM';
  if (lane === '3' || l === 'soul-routing' || l === 'soul') return '③ SOUL ROUTING';
  throw new Error(`unknown lane: ${lane}`);
}

export function nextGDL(body: string): number {
  const matches = body.matchAll(/^### GDL-(\d+)/gm);
  let max = 0;
  for (const m of matches) {
    const n = parseInt(m[1], 10);
    if (n > max) max = n;
  }
  return max + 1;
}

export function pad(n: number): string {
  return n.toString().padStart(3, '0');
}

export function splitEntries(body: string): LedgerEntry[] {
  const out: LedgerEntry[] = [];
  let current: LedgerEntry | null = null;
  for (const line of body.split('\n')) {
    const m = /^### (GDL-\d+)\b/.exec(line);
    if (m) {
      if (current) out.push(current);
      current = { id: m[1], body: '' };
      continue;
    }
    if (current) current.body += line + '\n';
  }
  if (current) out.push(current);
  return out;
}

export function lintStructural(body: string, blocks: LedgerEntry[]): LintFinding[] {
  const findings: LintFinding[] = [];

  const ids = new Set<string>();
  for (const m of body.matchAll(/^### (GDL-\d+)\b.*$/gm)) {
    const id = m[1];
    if (ids.has(id)) {
      findings.push({ level: 'error', gdl: id, msg: 'duplicate ID' });
    }
    ids.add(id);
  }

  for (const b of blocks) {
    if (!/\*\*Observed:\*\*/.test(b.body)) {
      findings.push({ level: 'error', gdl: b.id, msg: 'missing Observed field' });
    }
    if (!/\*\*Lane:\*\*\s+[①②③]/.test(b.body)) {
      findings.push({ level: 'error', gdl: b.id, msg: 'missing or malformed Lane' });
    }
    if (!/\*\*Graduated:\*\*/.test(b.body)) {
      findings.push({ level: 'warn', gdl: b.id, msg: 'missing Graduated field' });
    }
  }

  return findings;
}

export function lintTraceRefs(
  blocks: LedgerEntry[],
  dbPath: string | undefined,
  strict: boolean,
): LintFinding[] {
  if (!dbPath || !existsSync(dbPath)) return [];
  const findings: LintFinding[] = [];
  let db: Database.Database;
  try {
    db = new Database(dbPath, { readonly: true });
  } catch (err) {
    return [{ level: 'warn', gdl: '*', msg: `events.db open failed: ${String(err)}` }];
  }
  try {
    let stmt: Database.Statement;
    try {
      stmt = db.prepare('SELECT 1 FROM events WHERE chain_id = ? LIMIT 1');
    } catch (err) {
      return [
        { level: 'warn', gdl: '*', msg: `events.db has no events table: ${String(err)}` },
      ];
    }
    for (const b of blocks) {
      const m = /chain\s+`([^`]+)`/.exec(b.body);
      if (!m) continue;
      if (m[1].startsWith('<')) continue;
      const row = stmt.get(m[1]);
      if (!row) {
        findings.push({
          level: strict ? 'error' : 'warn',
          gdl: b.id,
          msg: `chain_id not in events.db: ${m[1]}`,
        });
      }
    }
  } finally {
    db.close();
  }
  return findings;
}

export function lintGraduationMarkers(blocks: LedgerEntry[]): LintFinding[] {
  const findings: LintFinding[] = [];
  for (const b of blocks) {
    const m = /\*\*Graduated:\*\*.*?(issue|PR|souls PR)\s*#(\d+)/i.exec(b.body);
    if (!m) continue;
    const num = m[2];
    const issue = spawnSync('gh', ['issue', 'view', num, '--json', 'number'], { encoding: 'utf8' });
    if (issue.status === 0) continue;
    const pr = spawnSync('gh', ['pr', 'view', num, '--json', 'number'], { encoding: 'utf8' });
    if (pr.status === 0) continue;
    findings.push({
      level: 'warn',
      gdl: b.id,
      msg: `graduated marker #${num} not resolvable via gh`,
    });
  }
  return findings;
}
