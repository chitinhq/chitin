// Slice 4 grooming dispatcher.
//
// For each in_design entry in docs/swarm-backlog.md, run a short
// agent turn with DRIVER=copilot to classify the entry, collect
// recommendations, and write a report.
//
// MVP scope: report only. The follow-up step (commit a backlog update +
// open issues) lives in a separate stage (apply-recommendations.ts) so the
// human can review the report between collection and commit.
//
// Pre-cut-over (Temporal): each entry → workflow.start(executeRequestWorkflow)
// + handle.result(). Now: each entry → direct runAgentTurn(req). Same
// code path, no orchestration round-trip.
//
// Usage:
//   pnpm exec tsx apps/runner/src/groom-pass.ts [--limit N]
//
// Reads:  docs/swarm-backlog.md
// Writes: tmp/grooming-pass-<passId>.json (report)
// Stdout: human-readable summary table

import { ExecutionRequestSchema } from '@chitin/contracts';
import { mkdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { runAgentTurn } from './activity.ts';
import { parseBacklog } from './grooming/parse-backlog.ts';
import { buildGroomingPrompt } from './grooming/prompt.ts';
import { parseRecommendation, type GroomingRecommendation } from './grooming/parse-recommendation.ts';

const BACKLOG_PATH = resolve(process.cwd(), 'docs/swarm-backlog.md');
const TMP_DIR = resolve(process.cwd(), 'tmp');

interface GroomingResult {
  entryId: string;
  workflowId: string;
  exitCode: number;
  durationMs: number;
  stdoutTailLen: number;
  parse:
    | { ok: true; recommendation: GroomingRecommendation }
    | { ok: false; error: string; rawExtract?: string };
}

async function main() {
  const limit = parseLimit(process.argv);
  const passId = `groom-${Date.now()}`;
  console.log(`[groom-pass] starting passId=${passId} backlog=${BACKLOG_PATH}`);

  const entries = parseBacklog(BACKLOG_PATH);
  const inDesign = entries.filter((e) => e.status === 'in_design');
  const targets = limit ? inDesign.slice(0, limit) : inDesign;
  console.log(
    `[groom-pass] found ${entries.length} entries; ${inDesign.length} in_design; will groom ${targets.length}`,
  );
  if (targets.length === 0) {
    console.log('[groom-pass] nothing to do — exiting');
    return;
  }

  const results: GroomingResult[] = [];

  // One agent turn per entry; sequential to keep load on the kernel and
  // Copilot session predictable. Slice 4b can parallelize once we trust
  // the calibration.
  for (const entry of targets) {
    const workflowId = `${passId}-${sanitize(entry.id)}`;
    const prompt = buildGroomingPrompt(entry);
    console.log(`[groom-pass] dispatch entry=${entry.id} workflowId=${workflowId}`);
    try {
      const req = ExecutionRequestSchema.parse({
        schema_version: '1',
        workflow_id: workflowId,
        run_id: `${workflowId}-attempt-1`,
        repo: 'chitinhq/chitin',
        task_class: 'exploration',
        risk_level: 'low',
        allowed_drivers: ['copilot'],
        network_policy: 'allowlist',
        write_policy: 'none',
        bounds: { max_tool_calls: 1, max_cost_usd: 0, wall_timeout_s: 90 },
        prompt,
      });
      const activityResult = await runAgentTurn(req);
      const parse = parseRecommendation(activityResult.stdout_tail, entry.id);
      results.push({
        entryId: entry.id,
        workflowId,
        exitCode: activityResult.exit_code,
        durationMs: activityResult.duration_ms,
        stdoutTailLen: activityResult.stdout_tail.length,
        parse,
      });
      if (parse.ok) {
        console.log(
          `  → ${parse.recommendation.status} tier=${parse.recommendation.tierRecommendation} confidence=${parse.recommendation.confidence}`,
        );
      } else {
        console.log(`  → parse failed: ${parse.error}`);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.log(`  → run error: ${msg}`);
      results.push({
        entryId: entry.id,
        workflowId,
        exitCode: -2,
        durationMs: 0,
        stdoutTailLen: 0,
        parse: { ok: false, error: `run error: ${msg}` },
      });
    }
  }

  mkdirSync(TMP_DIR, { recursive: true });
  const reportPath = resolve(TMP_DIR, `grooming-pass-${passId}.json`);
  writeFileSync(reportPath, JSON.stringify(results, null, 2));
  console.log(`\n[groom-pass] report → ${reportPath}`);
  printSummary(results);
}

function printSummary(results: GroomingResult[]) {
  const ok = results.filter((r) => r.parse.ok).length;
  const failed = results.length - ok;
  console.log(`\n=== summary ===`);
  console.log(`parsed: ${ok}/${results.length} (${failed} failures)`);
  for (const r of results) {
    if (r.parse.ok) {
      const rec = r.parse.recommendation;
      console.log(
        `  ${pad(r.entryId, 40)} ${pad(rec.status, 16)} ${rec.tierRecommendation}  conf=${rec.confidence}`,
      );
    } else {
      console.log(`  ${pad(r.entryId, 40)} FAIL ${r.parse.error.slice(0, 60)}`);
    }
  }
}

function pad(s: string, n: number): string {
  return s.length >= n ? s.slice(0, n) : s + ' '.repeat(n - s.length);
}

function sanitize(s: string): string {
  // Temporal workflow_id constraints: [a-zA-Z0-9_\-:.]{1,128}
  return s.replace(/[^a-zA-Z0-9_\-:.]/g, '-');
}

function parseLimit(argv: string[]): number | null {
  const idx = argv.indexOf('--limit');
  if (idx < 0 || idx + 1 >= argv.length) return null;
  const n = parseInt(argv[idx + 1], 10);
  return Number.isFinite(n) && n > 0 ? n : null;
}

main().catch((err) => {
  console.error('[groom-pass] fatal:', err);
  process.exit(1);
});
