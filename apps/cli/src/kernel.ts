import { chmodSync, copyFileSync, existsSync, mkdirSync, statSync, writeFileSync } from 'node:fs';
import { homedir, platform as osPlatform, arch as osArch } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';

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
  // fileURLToPath, not URL.pathname: the latter leaves the bundled-binary
  // lookup pointing at a %20 path on spaces and a non-portable /C:/ path on
  // Windows, so a perfectly valid vendored kernel would be missed.
  const here = dirname(fileURLToPath(import.meta.url));
  return join(here, '..', 'vendor', getKernelAssetSpec(platform, arch).assetName);
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

  const packaged = getPackagedKernelPath();
  if (isExecutable(packaged)) {
    copyFileSync(packaged, cached);
    if (osPlatform() !== 'win32') chmodSync(cached, 0o755);
    return cached;
  }

  // Download fallback. Trust model: the binary comes from the chitinhq
  // release URL over HTTPS. When CHITIN_KERNEL_SHA256 is set, the bytes are
  // verified against it before the file is written and made executable —
  // operators wiring this into automation should pin that checksum.
  const url = getKernelDownloadURL(version);
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`failed to download kernel from ${url}: ${res.status} ${res.statusText}`);
  }
  const bytes = new Uint8Array(await res.arrayBuffer());
  const expectedSha = process.env.CHITIN_KERNEL_SHA256?.trim().toLowerCase();
  if (expectedSha) {
    const actualSha = createHash('sha256').update(bytes).digest('hex');
    if (actualSha !== expectedSha) {
      throw new Error(
        `kernel checksum mismatch for ${url}: expected ${expectedSha}, got ${actualSha}`,
      );
    }
  }
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
