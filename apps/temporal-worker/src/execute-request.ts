// chitin-execute-request — generic CLI invoked by the per-PR
// dispatchers post-Temporal. Reads an ExecutionRequest from
// `--request-file <path>`, calls runAgentTurn, prints the
// ActivityResult JSON to stdout.
//
// Pre-cut-over: client.workflow.start(executeRequestWorkflow, {args:[req]})
// → activity layer calls runAgentTurn(req).
// Post-cut-over: spawn-execute-request.ts writes req to disk + spawns
// this CLI detached → it calls runAgentTurn(req) directly. Same code
// path, no Temporal round-trip.
//
// Always exits 0 on success, 1 on uncaught error. The detached
// dispatcher captures stdout/stderr via redirected log file; control
// flow lives in those logs, not the exit code.

import { parseArgs } from 'node:util';
import { readFileSync } from 'node:fs';
import { ExecutionRequestSchema } from '@chitin/contracts';
import { runAgentTurn } from './activity.ts';

async function main(): Promise<void> {
  const { values } = parseArgs({
    options: {
      'request-file': { type: 'string' },
    },
    strict: false,
  });

  const path = values['request-file'] as string | undefined;
  if (!path) {
    process.stderr.write('chitin-execute-request: --request-file <path> is required\n');
    process.exit(1);
  }

  const reqJson = readFileSync(path, 'utf8');
  const req = ExecutionRequestSchema.parse(JSON.parse(reqJson));
  const result = await runAgentTurn(req);
  process.stdout.write(JSON.stringify(result) + '\n');
}

main().catch((err) => {
  process.stderr.write(
    JSON.stringify({ error: err instanceof Error ? err.message : String(err) }) + '\n',
  );
  process.exit(1);
});
