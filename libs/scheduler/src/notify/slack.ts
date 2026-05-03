import type { Item } from '../schema.js';
import { register } from '../notify.js';

function itemText(item: Item): string {
  if (item.item_type === 'task') {
    const parts = [`*${item.title}*`];
    if (item.deadline) parts.push(`deadline: ${item.deadline.slice(0, 10)}`);
    if (item.est_min) parts.push(`est: ${item.est_min}min`);
    if (item.scheduled_start) parts.push(`slot: ${item.scheduled_start.slice(11, 16)}`);
    return parts.join(' | ');
  }
  if (item.item_type === 'event') {
    return `*${item.title}* @ ${item.start.slice(11, 16)}`;
  }
  return `*${item.title}*`;
}

export async function slackNotify(item: Item): Promise<void> {
  const webhookUrl = process.env['SLACK_WEBHOOK_URL'];
  if (!webhookUrl) throw new Error('SLACK_WEBHOOK_URL is not set');

  const res = await fetch(webhookUrl, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text: itemText(item) }),
  });

  if (!res.ok) {
    const text = await res.text();
    throw new Error(`Slack webhook failed ${res.status}: ${text}`);
  }
}

register('slack', slackNotify);
