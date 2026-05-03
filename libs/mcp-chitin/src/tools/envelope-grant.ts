import { kernelOkOrThrow, runKernel } from '../kernel.js';

export interface EnvelopeGrantArgs {
  id: string;
  calls?: number;
  bytes?: number;
  usd?: number;
  reason?: string;
}

export function envelopeGrantTool(args: EnvelopeGrantArgs): void {
  const kernelArgs = ['envelope', 'grant', args.id];
  if (args.calls !== undefined) kernelArgs.push(`--calls=${args.calls}`);
  if (args.bytes !== undefined) kernelArgs.push(`--bytes=${args.bytes}`);
  if (args.usd !== undefined) kernelArgs.push(`--usd=${args.usd}`);
  if (args.reason !== undefined) kernelArgs.push(`--reason=${args.reason}`);
  kernelOkOrThrow(runKernel(kernelArgs));
}
