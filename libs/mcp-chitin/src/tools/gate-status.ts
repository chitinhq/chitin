import { parseKernelJSON, runKernel } from '../kernel.js';

export interface GateStatusArgs {
  agent?: string;
  cwd?: string;
}

export interface GateStatusResult {
  policy_id: string;
  mode: string;
  policy_sources: string[];
  rules_count: number;
  agent: string;
  level: string;
  locked: boolean;
}

export function gateStatusTool(args: GateStatusArgs): GateStatusResult {
  const kernelArgs = ['gate', 'status'];
  if (args.agent) kernelArgs.push('--agent', args.agent);
  if (args.cwd) kernelArgs.push('--cwd', args.cwd);
  return parseKernelJSON<GateStatusResult>(runKernel(kernelArgs));
}
