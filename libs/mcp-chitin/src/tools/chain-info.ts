import { parseKernelJSON, runKernel } from '../kernel.js';

export interface ChainInfoArgs {
  chainId: string;
  dir?: string;
}

export type ChainInfoResult =
  | { exists: false }
  | { exists: true; last_seq: number; last_hash: string };

export function chainInfoTool(args: ChainInfoArgs): ChainInfoResult {
  const kernelArgs = ['chain-info', '--chain-id', args.chainId];
  if (args.dir) kernelArgs.push('--dir', args.dir);
  return parseKernelJSON<ChainInfoResult>(runKernel(kernelArgs));
}
