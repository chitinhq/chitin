import type { Item } from '../schema.js';
import { register } from '../notify.js';

function itemSummary(item: Item): string {
  if (item.item_type === 'task') {
    const parts = [item.title];
    if (item.deadline) parts.push(`due ${item.deadline.slice(0, 10)}`);
    if (item.est_min) parts.push(`~${item.est_min}min`);
    return parts.join(' · ');
  }
  if (item.item_type === 'event') {
    return `${item.title} @ ${item.start.slice(11, 16)}`;
  }
  return item.title;
}

export async function ntfyNotify(item: Item): Promise<void> {
  const baseUrl = process.env['NTFY_URL'] ?? 'https://ntfy.sh';
  const topic = process.env['NTFY_TOPIC'] ?? 'chitin-scheduler';
  const url = `${baseUrl}/${topic}`;

  const res = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'text/plain',
      Title: item.title,
      Priority: item.item_type === 'task' && item.priority === 1 ? 'urgent' : 'default',
      Tags: item.item_type,
    },
    body: itemSummary(item),
  });

  if (!res.ok) {
    const text = await res.text();
    throw new Error(`ntfy POST failed ${res.status}: ${text}`);
  }
}

register('ntfy', ntfyNotify);
