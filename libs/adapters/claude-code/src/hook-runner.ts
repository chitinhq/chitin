import { spawnSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { hookToEventType } from './hook-dispatch';

export interface HookInput {
  hook_event_name: string;
  tool_use_id?: string;
  tool_name?: string;
  tool_input?: Record<string, unknown>;
  tool_response?: Record<string, unknown>;
  error?: string;
  session_id?: string;
  prompt?: string;
  [key: string]: unknown;
}

export interface AdapterContext {
  runID: string;
  sessionID: string;
  agentInstanceID: string;
  surface: 'claude-code';
  driverIdentity: { user: string; machine_id: string; machine_fingerprint: string };
  agentFingerprint: string;
  labels: Record<string, string>;
  kernelBinary: string;
  chitinDir: string;
}

export interface HookResult {
  eventType: string;
  emittedSeq?: number;
  thisHash?: string;
  skipped: boolean;
}

export function runHook(input: HookInput, ctx: AdapterContext): HookResult {
  const eventType = hookToEventType(input.hook_event_name, input as Record<string, unknown>);
  if (!eventType) {
    return { eventType: '', skipped: true };
  }

  const chainID = deriveChainID(input, ctx);
  const chainType: 'session' | 'tool_call' =
    eventType === 'intended' || eventType === 'executed' || eventType === 'failed'
      ? 'tool_call'
      : 'session';
  const parentChainID: string | null =
    chainType === 'tool_call' ? ctx.sessionID : null;

  const payload = buildPayload(eventType, input);

  const eventStub = {
    schema_version: '2',
    run_id: ctx.runID,
    session_id: ctx.sessionID,
    surface: ctx.surface,
    driver_identity: ctx.driverIdentity,
    agent_instance_id: ctx.agentInstanceID,
    parent_agent_id: null as string | null,
    agent_fingerprint: ctx.agentFingerprint,
    event_type: eventType,
    chain_id: chainID,
    chain_type: chainType,
    parent_chain_id: parentChainID,
    seq: 0,
    prev_hash: null,
    this_hash: '',
    ts: new Date().toISOString(),
    labels: ctx.labels,
    payload,
  };

  const dir = mkdtempSync(join(tmpdir(), 'chitin-emit-'));
  const evPath = join(dir, 'ev.json');
  writeFileSync(evPath, JSON.stringify(eventStub));
  try {
    const res = spawnSync(
      ctx.kernelBinary,
      ['emit', '--dir', ctx.chitinDir, '--event-file', evPath],
      { encoding: 'utf8' },
    );
    if (res.status !== 0) {
      return { eventType, skipped: false };
    }
    const parsed = JSON.parse(res.stdout || '{}');
    return {
      eventType,
      emittedSeq: parsed.seq,
      thisHash: parsed.this_hash,
      skipped: false,
    };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

function deriveChainID(input: HookInput, ctx: AdapterContext): string {
  if (input.tool_use_id) return input.tool_use_id;
  return ctx.sessionID;
}

function buildPayload(eventType: string, input: HookInput): Record<string, unknown> {
  switch (eventType) {
    case 'user_prompt':
      return { text: String(input.prompt ?? '') };
    case 'intended':
      return {
        tool_name: String(input.tool_name ?? 'unknown'),
        raw_input: (input.tool_input ?? {}) as Record<string, unknown>,
        action_type: classifyActionType(input.tool_name as string | undefined),
      };
    case 'executed':
      return { duration_ms: 0 };
    case 'failed':
      return {
        duration_ms: 0,
        error_kind: 'unknown',
        error: String(input.error ?? 'unspecified'),
      };
    default:
      return minimalPayloadFor(eventType);
  }
}

function minimalPayloadFor(eventType: string): Record<string, unknown> {
  if (eventType === 'session_end') {
    return {
      reason: 'hook-default',
      totals: {
        turn_count: 0,
        tool_call_count: 0,
        total_input_tokens: 0,
        total_output_tokens: 0,
        total_duration_ms: 0,
      },
    };
  }
  if (eventType === 'compaction') {
    return { reason: 'hook-default' };
  }
  if (eventType === 'session_start') {
    return {
      cwd: process.cwd(),
      client_info: { name: 'claude-code', version: 'unknown' },
      model: { name: 'unknown', provider: 'anthropic' },
      system_prompt_hash: '0'.repeat(64),
      tool_allowlist_hash: '0'.repeat(64),
      agent_version: 'unknown',
    };
  }
  return {};
}

function classifyActionType(tool: string | undefined): string {
  if (!tool) return 'read';
  const t = tool.toLowerCase();
  if (t === 'read' || t === 'grep' || t === 'glob' || t === 'ls') return 'read';
  if (t === 'write' || t === 'edit' || t === 'multiedit') return 'write';
  if (t === 'bash') return 'exec';
  if (t.includes('git')) return 'git';
  if (t.includes('webfetch') || t.includes('websearch')) return 'net';
  return 'read';
}

export function runHookFromStdin(ctx: AdapterContext): HookResult {
  const raw = readFileSync(0, 'utf8');
  const input = JSON.parse(raw) as HookInput;
  return runHook(input, ctx);
}
