// Apply step for a grooming pass.
//
// Reads a grooming report (JSON file produced by groom-pass.ts), modifies
// docs/swarm-backlog.md per the recommendations, and (optionally) opens a
// GitHub issue per ready entry. Idempotent re-application is the eventual
// goal but slice 4 MVP applies once and trusts git as the rollback.
//
// Usage:
//   pnpm exec tsx apps/temporal-worker/src/grooming/apply-recommendations.ts \
//     --report tmp/grooming-pass-<id>.json [--dry-run] [--issues]
//
// --dry-run: print the proposed swarm-backlog.md diff and issue list, do not
//   write or open issues. Default mode (no --apply).
// --issues: also open one GitHub issue per ready entry. Off by default —
//   the operator opts in after reviewing the dry-run.

import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { execSync } from 'node:child_process';
import type { GroomingRecommendation } from './parse-recommendation.ts';
import { parseBacklog, type BacklogEntry } from './parse-backlog.ts';

const BACKLOG_PATH = resolve(process.cwd(), 'docs/swarm-backlog.md');

interface ReportRow {
  entryId: string;
  workflowId: string;
  exitCode: number;
  durationMs: number;
  parse:
    | { ok: true; recommendation: GroomingRecommendation }
    | { ok: false; error: string };
}

async function main() {
  const reportPath = readFlag('--report');
  if (!reportPath) {
    console.error('usage: apply-recommendations --report <path> [--dry-run] [--issues]');
    process.exit(2);
  }
  const dryRun = !process.argv.includes('--apply');
  const openIssues = process.argv.includes('--issues');

  const report: ReportRow[] = JSON.parse(readFileSync(reportPath, 'utf8'));
  const valid = report.filter((r): r is ReportRow & { parse: { ok: true; recommendation: GroomingRecommendation } } => r.parse.ok);
  console.log(`[apply] valid recommendations: ${valid.length}/${report.length}`);

  const entries = parseBacklog(BACKLOG_PATH);
  const updates = computeBacklogUpdates(entries, valid);
  const updated = applyUpdates(BACKLOG_PATH, updates);

  console.log(`[apply] backlog edits: ${updates.length}`);
  for (const u of updates) {
    console.log(`  ${u.kind.padEnd(8)}  ${u.entryId.padEnd(40)} ${u.summary}`);
  }

  if (dryRun) {
    console.log('\n[apply] DRY RUN — no files written, no issues opened.');
    console.log('[apply] preview of updated swarm-backlog.md (first 80 lines):');
    console.log('---');
    console.log(updated.split('\n').slice(0, 80).join('\n'));
    console.log('---');
    console.log('[apply] re-run with --apply to write; add --issues to also open GitHub issues.');
    return;
  }

  writeFileSync(BACKLOG_PATH, updated);
  console.log(`[apply] wrote ${BACKLOG_PATH}`);

  if (openIssues) {
    const issueUrls: string[] = [];
    for (const u of updates) {
      if (u.kind !== 'promote') continue;
      const url = openIssue(u.entryId, u.recommendation!);
      if (url) {
        console.log(`  issue → ${url}`);
        issueUrls.push(url);
      }
    }
    console.log(`[apply] opened ${issueUrls.length} issues`);
  } else {
    console.log('[apply] skipping issue creation (use --issues to opt in).');
  }
}

interface BacklogUpdate {
  kind: 'promote' | 'decompose' | 'noop';
  entryId: string;
  summary: string;
  recommendation?: GroomingRecommendation;
  oldSection?: string;
  newSection?: string;
}

function computeBacklogUpdates(
  entries: BacklogEntry[],
  recs: Array<ReportRow & { parse: { ok: true; recommendation: GroomingRecommendation } }>,
): BacklogUpdate[] {
  const updates: BacklogUpdate[] = [];
  for (const r of recs) {
    const entry = entries.find((e) => e.id === r.entryId);
    if (!entry) {
      updates.push({
        kind: 'noop',
        entryId: r.entryId,
        summary: 'entry not found in backlog',
      });
      continue;
    }
    const rec = r.parse.recommendation;
    if (rec.status === 'ready') {
      updates.push({
        kind: 'promote',
        entryId: r.entryId,
        summary: `→ ready, tier=${rec.tierRecommendation}, loc≈${rec.estimatedLoc}`,
        recommendation: rec,
        oldSection: entry.rawSection,
        newSection: buildPromotedSection(entry, rec),
      });
    } else if (rec.status === 'still_in_design' && rec.decomposition.length >= 2) {
      updates.push({
        kind: 'decompose',
        entryId: r.entryId,
        summary: `decomposed into ${rec.decomposition.length} sub-entries`,
        recommendation: rec,
        oldSection: entry.rawSection,
        newSection: buildDecomposedSections(entry, rec),
      });
    } else {
      updates.push({
        kind: 'noop',
        entryId: r.entryId,
        summary: `recommendation status=${rec.status} — not actionable, leaving entry unchanged`,
      });
    }
  }
  return updates;
}

function applyUpdates(backlogPath: string, updates: BacklogUpdate[]): string {
  let text = readFileSync(backlogPath, 'utf8');
  for (const u of updates) {
    if (!u.oldSection || !u.newSection) continue;
    if (!text.includes(u.oldSection)) {
      console.warn(`[apply] WARNING: cannot locate old section for ${u.entryId} — skipping.`);
      continue;
    }
    text = text.replace(u.oldSection, u.newSection);
  }
  return text;
}

function buildPromotedSection(entry: BacklogEntry, rec: GroomingRecommendation): string {
  // Rewrite the frontmatter: status → ready, tier → recommended tier,
  // estimated_loc → numeric estimate. Preserve other fields where present.
  const fm = updateFrontmatter(entry.rawFrontmatter, {
    status: 'ready',
    tier: rec.tierRecommendation,
    estimated_loc: String(rec.estimatedLoc),
  });
  // Append the implementation steps to the description if not already present.
  const stepsBlock = rec.implementationSteps.map((s) => `- ${s}`).join('\n');
  const description = entry.description.trim();
  const groomedNote =
    `\n\n**Groomed ${new Date().toISOString().split('T')[0]} (${rec.confidence} confidence):** ${rec.reasoning}\n\nImplementation steps:\n${stepsBlock}`;
  return `### \`${entry.id}\`

\`\`\`yaml
${fm}
\`\`\`

${description}${groomedNote}
`;
}

function buildDecomposedSections(
  entry: BacklogEntry,
  rec: GroomingRecommendation,
): string {
  // Replace the original section with: a placeholder note pointing to the
  // sub-entries, plus N new sub-entry sections.
  const subSections = rec.decomposition
    .map(
      (d) => `### \`${d.id}\`

\`\`\`yaml
id: ${d.id}
tier: ${d.tier}
status: in_design
parent: ${entry.id}
\`\`\`

${d.title}

(Decomposed from \`${entry.id}\` on ${new Date().toISOString().split('T')[0]}.)
`,
    )
    .join('\n');
  const parentNote = `### \`${entry.id}\` (decomposed)

\`\`\`yaml
id: ${entry.id}
status: decomposed
decomposed_into: [${rec.decomposition.map((d) => d.id).join(', ')}]
decomposed_at: ${new Date().toISOString().split('T')[0]}
\`\`\`

${entry.description.trim()}

**Groomed:** ${rec.reasoning}
`;
  return `${parentNote}\n${subSections}`;
}

function updateFrontmatter(raw: string, fields: Record<string, string>): string {
  const lines = raw.split('\n');
  const seen = new Set<string>();
  const out: string[] = [];
  for (const line of lines) {
    const m = line.match(/^(\w+):\s*(.*)$/);
    if (m && fields[m[1]] !== undefined) {
      out.push(`${m[1]}: ${fields[m[1]]}`);
      seen.add(m[1]);
    } else {
      out.push(line);
    }
  }
  for (const key of Object.keys(fields)) {
    if (!seen.has(key)) out.push(`${key}: ${fields[key]}`);
  }
  return out.join('\n');
}

function openIssue(entryId: string, rec: GroomingRecommendation): string | null {
  const title = `[swarm/${rec.tierRecommendation}] ${entryId}`;
  const body = [
    `Groomed from \`docs/swarm-backlog.md\` entry \`${entryId}\` on ${new Date().toISOString().split('T')[0]}.`,
    '',
    `**Tier:** ${rec.tierRecommendation}  •  **Estimated LOC:** ${rec.estimatedLoc}  •  **Confidence:** ${rec.confidence}`,
    '',
    `**Reasoning:** ${rec.reasoning}`,
    '',
    '## Implementation steps',
    '',
    rec.implementationSteps.map((s) => `- [ ] ${s}`).join('\n'),
    '',
    '---',
    '',
    'See `docs/swarm-backlog.md` for full entry context. Generated by the slice-4 grooming pass.',
  ].join('\n');
  try {
    const labels = `swarm,tier-${rec.tierRecommendation.toLowerCase()}`;
    const out = execSync(
      `gh issue create --title ${shellQuote(title)} --body ${shellQuote(body)} --label ${shellQuote(labels)}`,
      { encoding: 'utf8' },
    );
    const url = out.trim().split('\n').pop() ?? '';
    return url;
  } catch (err) {
    console.warn(`[apply] failed to open issue for ${entryId}: ${err instanceof Error ? err.message : String(err)}`);
    return null;
  }
}

function shellQuote(s: string): string {
  return `'${s.replace(/'/g, "'\\''")}'`;
}

function readFlag(name: string): string | null {
  const idx = process.argv.indexOf(name);
  if (idx < 0 || idx + 1 >= process.argv.length) return null;
  return process.argv[idx + 1];
}

main().catch((err) => {
  console.error('[apply] fatal:', err);
  process.exit(1);
});
