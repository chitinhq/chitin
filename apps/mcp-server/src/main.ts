#!/usr/bin/env node
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import { z } from 'zod';
import {
  KernelError,
  chainInfoTool,
  chainVerifyTool,
  decisionsRecentTool,
  envelopeCloseTool,
  envelopeGrantTool,
  envelopeListTool,
  gateResetTool,
  gateStatusTool,
} from '@chitin/mcp-chitin';

const server = new McpServer({
  name: 'chitin',
  version: '0.0.1',
});

function ok(data: unknown) {
  return { content: [{ type: 'text' as const, text: JSON.stringify(data, null, 2) }] };
}

function mcpError(err: unknown) {
  const msg =
    err instanceof KernelError
      ? `kernel error [${err.kind}] exit ${err.exitCode}: ${err.detail}`
      : String(err);
  return { isError: true as const, content: [{ type: 'text' as const, text: msg }] };
}

function run<T>(fn: () => T) {
  try {
    return ok(fn());
  } catch (err) {
    return mcpError(err);
  }
}

server.tool(
  'chitin_envelope_list',
  'List budget envelopes tracked by chitin-kernel.',
  { limit: z.number().int().nonnegative().optional().describe('Max envelopes to return (0 = all); default 20') },
  ({ limit }) => run(() => envelopeListTool({ limit })),
);

server.tool(
  'chitin_envelope_grant',
  'Grant additional budget to an existing envelope.',
  {
    id: z.string().min(1).describe('Envelope ID'),
    // Deltas must be non-negative — this tool grants budget; reductions
    // (and especially negative deltas that could re-open a closed
    // envelope) are not allowed via MCP. Non-int values rejected by the
    // outer int() constraint.
    calls: z.number().int().nonnegative().optional().describe('Non-negative delta to add to max_tool_calls'),
    bytes: z.number().int().nonnegative().optional().describe('Non-negative delta to add to max_input_bytes'),
    usd: z.number().nonnegative().optional().describe('Non-negative delta to add to budget_usd'),
    reason: z.string().optional().describe('Operator-supplied reason recorded in audit log'),
  },
  (args) => {
    // Require at least one positive delta. An empty grant call would
    // still touch closed_at and write a 0-delta audit row, which
    // contradicts the tool's stated intent ("Grant additional budget").
    const totalDelta = (args.calls ?? 0) + (args.bytes ?? 0) + (args.usd ?? 0);
    if (totalDelta <= 0) {
      return {
        content: [{ type: 'text' as const, text: 'error: chitin_envelope_grant requires at least one positive delta among {calls, bytes, usd}' }],
        isError: true,
      };
    }
    return run(() => { envelopeGrantTool(args); return { ok: true }; });
  },
);

server.tool(
  'chitin_envelope_close',
  'Close an envelope so no further spending is allowed.',
  { id: z.string().min(1).describe('Envelope ID to close') },
  ({ id }) => run(() => { envelopeCloseTool({ id }); return { ok: true }; }),
);

server.tool(
  'chitin_gate_status',
  'Report per-agent escalation level and active policy for a given working directory.',
  {
    agent: z.string().optional().describe('Agent identifier (e.g. claude-code)'),
    cwd: z.string().optional().describe('Working directory to load policy from (default: cwd)'),
  },
  (args) => run(() => gateStatusTool(args)),
);

server.tool(
  'chitin_gate_reset',
  'Reset lockdown state for an agent, clearing accumulated escalation level.',
  { agent: z.string().min(1).describe('Agent identifier to reset (e.g. claude-code)') },
  ({ agent }) => run(() => gateResetTool({ agent })),
);

server.tool(
  'chitin_chain_info',
  'Return chain state (last seq + hash) for a session ID.',
  {
    chainId: z.string().min(1).describe('Session/chain ID to look up'),
    dir: z.string().optional().describe('Path to .chitin state dir (default: .chitin)'),
  },
  (args) => run(() => chainInfoTool(args)),
);

server.tool(
  'chitin_chain_verify',
  'Verify chain linkage integrity for a session (Phase-1.5 stub).',
  {
    sessionId: z.string().min(1).describe('Session ID to verify'),
    dir: z.string().optional().describe('Path to .chitin state dir (default: .chitin)'),
  },
  (args) => run(() => chainVerifyTool(args)),
);

server.tool(
  'chitin_decisions_recent',
  'Return the most recent governance decisions from the local decision log.',
  {
    dir: z.string().optional().describe('Path to chitin state dir (default: $CHITIN_HOME or ~/.chitin)'),
    windowHours: z.number().positive().optional().describe('Only return decisions within this many hours (default: 24)'),
    limit: z.number().int().positive().optional().describe('Max decisions to return (default: 100)'),
  },
  (args) => run(() => decisionsRecentTool(args)),
);

const transport = new StdioServerTransport();
await server.connect(transport);
