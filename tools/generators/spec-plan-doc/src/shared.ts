// Minimal Tree interface that matches what Nx passes at runtime.
// Defined locally so no @nx/devkit dependency is needed.
export interface Tree {
  root: string;
  write(filePath: string, content: string | Buffer): void;
  exists(filePath: string): boolean;
  read(filePath: string, encoding: BufferEncoding): string | null;
  read(filePath: string): Buffer | null;
}

export function isoDate(): string {
  return new Date().toISOString().slice(0, 10);
}

// Convert kebab-case id to a human-readable title.
// "dogfood-debt-ledger" → "Dogfood Debt Ledger"
export function idToTitle(id: string): string {
  return id
    .split('-')
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ');
}
