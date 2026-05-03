import { spawnSync } from 'node:child_process';

export interface KernelResult {
  stdout: string;
  stderr: string;
  status: number;
  error?: Error;
}

export function runKernel(args: string[]): KernelResult {
  const bin = process.env['CHITIN_KERNEL_BINARY'] ?? 'chitin-kernel';
  const res = spawnSync(bin, args, { encoding: 'utf8' });
  return {
    stdout: res.stdout ?? '',
    stderr: res.stderr ?? '',
    status: res.status ?? -1,
    error: res.error,
  };
}

export class KernelError extends Error {
  constructor(
    public readonly exitCode: number,
    public readonly kind: string,
    public readonly detail: string,
  ) {
    super(`kernel exited ${exitCode}: ${detail}`);
    this.name = 'KernelError';
  }
}

export function parseKernelJSON<T>(result: KernelResult): T {
  if (result.error) {
    throw new KernelError(-1, 'spawn_failed', result.error.message);
  }
  if (result.status !== 0) {
    let kind = 'nonzero_exit';
    const raw = result.stderr || result.stdout;
    try {
      const parsed = JSON.parse(raw) as { error?: string };
      if (parsed.error) kind = parsed.error;
    } catch {
      // use raw as detail
    }
    throw new KernelError(result.status, kind, raw.slice(0, 500));
  }
  try {
    return JSON.parse(result.stdout) as T;
  } catch {
    throw new KernelError(0, 'parse_failed', `not valid JSON: ${result.stdout.slice(0, 200)}`);
  }
}

export function kernelOkOrThrow(result: KernelResult): void {
  if (result.error) {
    throw new KernelError(-1, 'spawn_failed', result.error.message);
  }
  if (result.status !== 0) {
    let kind = 'nonzero_exit';
    const raw = result.stderr || result.stdout;
    try {
      const parsed = JSON.parse(raw) as { error?: string };
      if (parsed.error) kind = parsed.error;
    } catch {
      // use raw as detail
    }
    throw new KernelError(result.status, kind, raw.slice(0, 500));
  }
}
