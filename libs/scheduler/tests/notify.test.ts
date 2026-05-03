import { describe, it, expect, beforeEach } from 'vitest';
import { register, dispatch, registeredNotifiers } from '../src/notify.js';
import type { Item } from '../src/index.js';

function makeTask(): Item {
  return {
    id: 'task-1',
    item_type: 'task',
    title: 'Test task',
    status: 'open',
    created_at: '2026-05-03T08:00:00.000Z',
  };
}

describe('notify registry', () => {
  beforeEach(() => {
    // Clear registry between tests by registering over existing names
  });

  it('registers and dispatches a notifier', async () => {
    const calls: Item[] = [];
    register('test-notifier', async (item) => { calls.push(item); });

    await dispatch(makeTask(), 'test-notifier');
    expect(calls).toHaveLength(1);
    expect(calls[0].id).toBe('task-1');
  });

  it('throws on unknown notifier', async () => {
    await expect(dispatch(makeTask(), 'nonexistent')).rejects.toThrow('Unknown notifier');
  });

  it('lists registered notifiers', () => {
    register('list-test-a', async () => {});
    register('list-test-b', async () => {});
    const names = registeredNotifiers();
    expect(names).toContain('list-test-a');
    expect(names).toContain('list-test-b');
  });

  it('replaces existing notifier on re-register', async () => {
    const first: string[] = [];
    const second: string[] = [];
    register('replaceable', async (item) => { first.push(item.id); });
    register('replaceable', async (item) => { second.push(item.id); });

    await dispatch(makeTask(), 'replaceable');
    expect(first).toHaveLength(0);
    expect(second).toHaveLength(1);
  });
});

describe('ntfy notifier', () => {
  it('posts to ntfy URL with item summary', async () => {
    const requests: { url: string; body: string }[] = [];
    const mockFetch = async (url: string, opts: RequestInit) => {
      requests.push({ url: url as string, body: opts.body as string });
      return new Response('', { status: 200 });
    };

    const original = global.fetch;
    global.fetch = mockFetch as typeof fetch;

    try {
      process.env['NTFY_URL'] = 'http://ntfy.example.com';
      process.env['NTFY_TOPIC'] = 'test-topic';

      const { ntfyNotify } = await import('../src/notify/ntfy.js');
      await ntfyNotify(makeTask());

      expect(requests).toHaveLength(1);
      expect(requests[0].url).toBe('http://ntfy.example.com/test-topic');
      expect(requests[0].body).toContain('Test task');
    } finally {
      global.fetch = original;
      delete process.env['NTFY_URL'];
      delete process.env['NTFY_TOPIC'];
    }
  });
});

describe('slack notifier', () => {
  it('posts to slack webhook with item text', async () => {
    const requests: { url: string; body: string }[] = [];
    const mockFetch = async (url: string, opts: RequestInit) => {
      requests.push({ url: url as string, body: opts.body as string });
      return new Response('', { status: 200 });
    };

    const original = global.fetch;
    global.fetch = mockFetch as typeof fetch;

    try {
      process.env['SLACK_WEBHOOK_URL'] = 'http://slack.example.com/webhook';

      const { slackNotify } = await import('../src/notify/slack.js');
      await slackNotify(makeTask());

      expect(requests).toHaveLength(1);
      expect(requests[0].url).toBe('http://slack.example.com/webhook');
      const payload = JSON.parse(requests[0].body) as { text: string };
      expect(payload.text).toContain('Test task');
    } finally {
      global.fetch = original;
      delete process.env['SLACK_WEBHOOK_URL'];
    }
  });

  it('throws when SLACK_WEBHOOK_URL is not set', async () => {
    delete process.env['SLACK_WEBHOOK_URL'];
    const { slackNotify } = await import('../src/notify/slack.js');
    await expect(slackNotify(makeTask())).rejects.toThrow('SLACK_WEBHOOK_URL');
  });
});
