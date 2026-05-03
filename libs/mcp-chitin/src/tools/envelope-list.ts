import { parseKernelJSON, runKernel } from '../kernel.js';

export interface EnvelopeListArgs {
  limit?: number;
}

export interface EnvelopeState {
  id: string;
  status: string;
  max_tool_calls: number;
  max_input_bytes: number;
  budget_usd: number;
  used_tool_calls: number;
  used_input_bytes: number;
  used_usd: number;
  created_at: string;
  closed_at?: string;
}

export function envelopeListTool(args: EnvelopeListArgs): EnvelopeState[] {
  const kernelArgs = ['envelope', 'list'];
  if (args.limit !== undefined) kernelArgs.push('--limit', String(args.limit));
  return parseKernelJSON<EnvelopeState[]>(runKernel(kernelArgs));
}
