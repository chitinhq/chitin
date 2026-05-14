export function formatAge(secondsAgo: number | null | undefined): string {
  if (!secondsAgo || secondsAgo < 0) return '–';
  const s = Math.floor(secondsAgo);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  const d = Math.floor(h / 24);
  if (d < 30) return `${d}d`;
  const mo = Math.floor(d / 30);
  return `${mo}mo`;
}

export function ageFromEpochSeconds(epoch?: number | null): string {
  if (!epoch) return '–';
  return formatAge(Math.floor(Date.now() / 1000) - epoch);
}

export function ageFromEpochMs(ms?: number | null): string {
  if (!ms) return '–';
  return formatAge(Math.floor((Date.now() - ms) / 1000));
}

export function fmtUsd(n: number | null | undefined, digits = 4): string {
  if (n == null) return '–';
  return `$${n.toFixed(digits)}`;
}

export function fmtPct(n: number | null | undefined, digits = 1): string {
  if (n == null || Number.isNaN(n)) return '–';
  return `${(n * 100).toFixed(digits)}%`;
}

export function fmtTs(ts: number | string | null | undefined): string {
  if (ts == null) return '–';
  let d: Date;
  if (typeof ts === 'number') d = new Date(ts < 2e10 ? ts * 1000 : ts);
  else d = new Date(ts);
  if (Number.isNaN(d.getTime())) return String(ts);
  return d.toISOString().replace('T', ' ').replace(/\.\d+Z$/, 'Z');
}

export function ulidToDate(ulid?: string): Date | null {
  if (!ulid || ulid.length < 10) return null;
  // ULID first 10 chars = base32 ms timestamp
  const ts = ulid.slice(0, 10);
  const ALPHA = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';
  let ms = 0;
  for (const c of ts.toUpperCase()) {
    const v = ALPHA.indexOf(c);
    if (v === -1) return null;
    ms = ms * 32 + v;
  }
  return new Date(ms);
}

export function shortenId(id: string, head = 6, tail = 4): string {
  if (!id) return '';
  if (id.length <= head + tail + 1) return id;
  return `${id.slice(0, head)}…${id.slice(-tail)}`;
}

export function priorityBarWidth(priority: number): string {
  const clamped = Math.max(0, Math.min(100, priority));
  return `${clamped}%`;
}

export function statusOrder(status: string): number {
  const order: Record<string, number> = {
    in_progress: 0, triage: 1, ready: 2, todo: 3, done: 4, archived: 5,
  };
  return order[status] ?? 99;
}
