import { chmodSync, copyFileSync, existsSync, mkdirSync, statSync, writeFileSync } from 'node:fs';
import { homedir, platform as osPlatform, arch as osArch } from 'node:os';
import { dirname, join } from 'node:path';

export interface KernelAssetSpec {
  platform: NodeJS.Platform;
  arch: string;
  ext: string;
  assetName: string;
}

export function getKernelAssetSpec(
  platform: NodeJS.Platform = osPlatform(),
  arch: string = osArch(),
): KernelAssetSpec {
  const normalizedPlatform = platform === 'win32' ? 'windows' : platform;
  const ext = platform === 'win32' ? '.exe' : '';
  return {
    platform,
    arch,
    ext,
    assetName: `chitin-kernel-${normalizedPlatform}-${arch}${ext}`,
  };
}

export function getKernelCachePath(
  platform: NodeJS.Platform = osPlatform(),
  arch: string = osArch(),
): string {
  return join(homedir(), '.chitin', 'bin', getKernelAssetSpec(platform, arch).assetName);
}

export function getPackagedKernelPath(
  platform: NodeJS.Platform = osPlatform(),
  arch: string = osArch(),
): string {
  return join(dirname(new URL(import.meta.url).pathname), '..', 'vendor', getKernelAssetSpec(platform, arch).assetName);
}

export function getKernelDownloadURL(
  version: string,
  platform: NodeJS.Platform = osPlatform(),
  arch: string = osArch(),
): string {
  const envURL = process.env.CHITIN_KERNEL_DOWNLOAD_URL;
  if (envURL) return envURL;
  const baseURL = process.env.CHITIN_KERNEL_BASE_URL
    ?? `https://github.com/chitinhq/chitin/releases/download/v${version}`;
  return `${baseURL.replace(/\/$/, '')}/${getKernelAssetSpec(platform, arch).assetName}`;
}

export async function ensureKernelBinary(version: string): Promise<string> {
  const overridden = process.env.CHITIN_KERNEL_BINARY;
  if (overridden) {
    assertExecutable(overridden);
    return overridden;
  }

  const cached = getKernelCachePath();
  if (isExecutable(cached)) return cached;

  mkdirSync(dirname(cached), { recursive: true });

  const packaged = decodePath(getPackagedKernelPath());
  if (isExecutable(packaged)) {
    copyFileSync(packaged, cached);
    if (osPlatform() !== 'win32') chmodSync(cached, 0o755);
    return cached;
  }

  const url = getKernelDownloadURL(version);
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`failed to download kernel from ${url}: ${res.status} ${res.statusText}`);
  }
  const bytes = new Uint8Array(await res.arrayBuffer());
  writeFileSync(cached, bytes);
  if (osPlatform() !== 'win32') chmodSync(cached, 0o755);
  return cached;
}

export function isExecutable(path: string): boolean {
  try {
    if (!existsSync(path)) return false;
    const stat = statSync(path);
    if (!stat.isFile()) return false;
    if (osPlatform() === 'win32') return true;
    return (stat.mode & 0o111) !== 0;
  } catch {
    return false;
  }
}

export function assertExecutable(path: string): void {
  if (!isExecutable(path)) {
    throw new Error(`kernel binary is not executable: ${path}`);
  }
}

function decodePath(pathname: string): string {
  return decodeURIComponent(pathname);
}
