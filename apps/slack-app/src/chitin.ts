import { spawnSync } from 'node:child_process';

export interface EnvelopeState {
  id: string;
  status: string;
  max_tool_calls: number;
  used_tool_calls: number;
  max_input_bytes: number;
  used_input_bytes: number;
  budget_usd: number;
  created_at: string;
  closed_at?: string;
}

export interface ChainInfo {
  exists: boolean;
  last_seq?: number;
  last_hash?: string;
}

export interface GateStatus {
  agent: string;
  level: string;
  locked: boolean;
  policy_id: string;
  mode: string;
}

function run(binary: string, args: string[]): { ok: boolean; stdout: string; stderr: string } {
  const result = spawnSync(binary, args, { encoding: 'utf8', timeout: 10000 });
  return {
    ok: result.status === 0,
    stdout: result.stdout ?? '',
    stderr: result.stderr ?? '',
  };
}

const KERNEL = process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel';

export function envelopeList(): EnvelopeState[] {
  const r = run(KERNEL, ['envelope', 'list', '--limit=50']);
  if (!r.ok) throw new Error(`envelope list failed: ${r.stderr}`);
  return JSON.parse(r.stdout) as EnvelopeState[];
}

export function envelopeGrant(id: string, calls: number): void {
  const r = run(KERNEL, ['envelope', 'grant', id, `--calls=${calls}`, '--reason=slack-operator-grant']);
  if (!r.ok) throw new Error(`envelope grant failed: ${r.stderr}`);
}

export function gateReset(agent: string): void {
  const r = run(KERNEL, ['gate', 'reset', `--agent=${agent}`]);
  if (!r.ok) throw new Error(`gate reset failed: ${r.stderr}`);
}

export function chainInfo(chainId: string): ChainInfo {
  const r = run(KERNEL, ['chain-info', `--chain-id=${chainId}`]);
  if (!r.ok) throw new Error(`chain-info failed: ${r.stderr}`);
  return JSON.parse(r.stdout) as ChainInfo;
}

export function gateStatus(agent: string): GateStatus {
  const r = run(KERNEL, ['gate', 'status', `--agent=${agent}`]);
  if (!r.ok) throw new Error(`gate status failed: ${r.stderr}`);
  return JSON.parse(r.stdout) as GateStatus;
}
