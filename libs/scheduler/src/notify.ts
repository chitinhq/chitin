import type { Item } from './schema.js';

export type Notifier = (item: Item) => Promise<void>;

const registry = new Map<string, Notifier>();

export function register(name: string, fn: Notifier): void {
  registry.set(name, fn);
}

export async function dispatch(item: Item, notifier_name: string): Promise<void> {
  const fn = registry.get(notifier_name);
  if (!fn) throw new Error(`Unknown notifier: ${notifier_name}`);
  await fn(item);
}

export function registeredNotifiers(): string[] {
  return [...registry.keys()];
}
