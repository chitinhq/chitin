import type { Item } from './schema.js';

export type Notifier = (item: Item) => Promise<void>;

const registry = new Map<string, Notifier>();

export function register(name: string, fn: Notifier): void {
  registry.set(name, fn);
}

export async function dispatch(item: Item, notifierName: string): Promise<void> {
  const fn = registry.get(notifierName);
  if (!fn) throw new Error(`Unknown notifier: ${notifierName}`);
  await fn(item);
}

export function registeredNotifiers(): string[] {
  return [...registry.keys()];
}

// Test-only: clear the notifier registry. Exposed via __test__ to keep
// it out of the public API surface but available for vitest fixtures
// that need a clean registry per-test (otherwise tests cross-contaminate).
export const __test__ = {
  clearRegistry(): void {
    registry.clear();
  },
};
