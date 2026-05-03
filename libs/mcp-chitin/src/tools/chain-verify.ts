import { parseKernelJSON, runKernel } from '../kernel.js';

export interface ChainVerifyArgs {
  sessionId: string;
  dir?: string;
}

export interface ChainVerifyResult {
  verified: boolean;
  chain_id: string;
  reason?: string;
  phase_2_note?: string;
}

export function chainVerifyTool(args: ChainVerifyArgs): ChainVerifyResult {
  const kernelArgs = ['chain-verify', '--session-id', args.sessionId];
  if (args.dir) kernelArgs.push('--dir', args.dir);
  return parseKernelJSON<ChainVerifyResult>(runKernel(kernelArgs));
}
