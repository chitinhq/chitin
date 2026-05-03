import { kernelOkOrThrow, runKernel } from '../kernel.js';

export interface EnvelopeCloseArgs {
  id: string;
}

export function envelopeCloseTool(args: EnvelopeCloseArgs): void {
  kernelOkOrThrow(runKernel(['envelope', 'close', args.id]));
}
