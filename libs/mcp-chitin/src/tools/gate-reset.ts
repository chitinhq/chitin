import { parseKernelJSON, runKernel } from '../kernel.js';

export interface GateResetArgs {
  agent: string;
}

export interface GateResetResult {
  ok: boolean;
  action: string;
  agent: string;
}

export function gateResetTool(args: GateResetArgs): GateResetResult {
  return parseKernelJSON<GateResetResult>(runKernel(['gate', 'reset', '--agent', args.agent]));
}
