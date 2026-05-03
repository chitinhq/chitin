import { describe, it, expect } from 'vitest';
import { ingest } from '../src/ingest.js';
import type { AnthropicClient } from '../src/ingest.js';

function mockClient(response: unknown): AnthropicClient {
  return {
    async createMessage() {
      return {
        content: [{ type: 'text', text: JSON.stringify(response) }],
      };
    },
  };
}

function failingClient(error: string): AnthropicClient {
  return {
    async createMessage() {
      throw new Error(error);
    },
  };
}

function malformedClient(): AnthropicClient {
  return {
    async createMessage() {
      return { content: [{ type: 'text', text: 'not json at all' }] };
    },
  };
}

const NOW = '2026-05-03T08:00:00.000Z';

describe('ingest', () => {
  it('returns parsed task items from valid response', async () => {
    const mockItems = [
      {
        item_type: 'task',
        id: 'abc123',
        title: 'Fix login bug',
        status: 'open',
        created_at: NOW,
        priority: 1,
        deadline: '2026-05-04T09:00:00.000Z',
      },
    ];

    const result = await ingest('Fix login bug urgently', {
      now: NOW,
      client: mockClient(mockItems),
    });

    expect(result.items).toHaveLength(1);
    expect(result.items[0].title).toBe('Fix login bug');
    expect(result.items[0].item_type).toBe('task');
    expect(result.telemetry.item_count).toBe(1);
    expect(result.telemetry.event_type).toBe('ingest_result');
  });

  it('returns empty array on API error without throwing', async () => {
    const result = await ingest('some text', {
      now: NOW,
      client: failingClient('network error'),
    });

    expect(result.items).toHaveLength(0);
    expect(result.telemetry.parse_failure).toBeDefined();
    expect(result.telemetry.parse_failure?.reason).toContain('network error');
  });

  it('returns empty array on malformed JSON without throwing', async () => {
    const result = await ingest('some text', {
      now: NOW,
      client: malformedClient(),
    });

    expect(result.items).toHaveLength(0);
    expect(result.telemetry.parse_failure).toBeDefined();
  });

  it('filters out invalid items from partial response', async () => {
    const mixed = [
      {
        item_type: 'task',
        id: 'valid1',
        title: 'Valid task',
        status: 'open',
        created_at: NOW,
      },
      { item_type: 'unknown', id: 'bad', title: 'Bad item' },
      null,
    ];

    const result = await ingest('mixed input', {
      now: NOW,
      client: mockClient(mixed),
    });

    expect(result.items).toHaveLength(1);
    expect(result.items[0].id).toBe('valid1');
  });

  it('returns event items correctly', async () => {
    const mockItems = [
      {
        item_type: 'event',
        id: 'ev1',
        title: 'Team standup',
        status: 'open',
        created_at: NOW,
        start: '2026-05-04T09:00:00.000Z',
        duration_min: 30,
      },
    ];

    const result = await ingest('standup tomorrow', {
      now: NOW,
      client: mockClient(mockItems),
    });

    expect(result.items[0].item_type).toBe('event');
    expect(result.decisions[0].rationale).toBe('ingested:event');
  });

  it('uses preferred_model in telemetry', async () => {
    const result = await ingest('task', {
      now: NOW,
      preferred_model: 'claude-sonnet-4-6',
      client: mockClient([]),
    });

    expect(result.telemetry.model).toBe('claude-sonnet-4-6');
  });
});
