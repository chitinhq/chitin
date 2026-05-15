import { describe, expect, it } from 'vitest';
import { getKernelAssetSpec, getKernelDownloadURL } from '../src/kernel';

describe('getKernelAssetSpec', () => {
  it('maps darwin arm64 to the expected asset name', () => {
    expect(getKernelAssetSpec('darwin', 'arm64').assetName).toBe('chitin-kernel-darwin-arm64');
  });

  it('maps win32 x64 to the expected asset name with .exe', () => {
    expect(getKernelAssetSpec('win32', 'x64').assetName).toBe('chitin-kernel-windows-x64.exe');
  });
});

describe('getKernelDownloadURL', () => {
  it('builds a GitHub release URL by default', () => {
    const prev = process.env.CHITIN_KERNEL_BASE_URL;
    delete process.env.CHITIN_KERNEL_BASE_URL;
    delete process.env.CHITIN_KERNEL_DOWNLOAD_URL;
    expect(getKernelDownloadURL('1.2.3', 'linux', 'x64')).toBe(
      'https://github.com/chitinhq/chitin/releases/download/v1.2.3/chitin-kernel-linux-x64',
    );
    if (prev) process.env.CHITIN_KERNEL_BASE_URL = prev;
  });

  it('honors CHITIN_KERNEL_BASE_URL overrides', () => {
    process.env.CHITIN_KERNEL_BASE_URL = 'https://example.test/releases';
    delete process.env.CHITIN_KERNEL_DOWNLOAD_URL;
    expect(getKernelDownloadURL('1.2.3', 'linux', 'arm64')).toBe(
      'https://example.test/releases/chitin-kernel-linux-arm64',
    );
    delete process.env.CHITIN_KERNEL_BASE_URL;
  });
});
