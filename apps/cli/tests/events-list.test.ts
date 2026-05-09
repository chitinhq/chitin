import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

// Mock better-sqlite3 constructor
const mockAll = vi.fn(() => []);
const mockPrepare = vi.fn(() => ({ all: mockAll }));
const mockClose = vi.fn();

vi.mock('better-sqlite3', () => {
  return {
    default: class {
      prepare = mockPrepare;
      close = mockClose;
    },
    __esModule: true,
  };
});

vi.mock('@chitin/telemetry', () => ({
  ensureIndexed: vi.fn(),
}));

vi.mock('node:fs', async () => {
  const actual = await vi.importActual('node:fs');
  return { ...actual, existsSync: vi.fn().mockReturnValue(true) };
});

import { listRecentEvents } from '../src/commands/events-list';
import { existsSync } from 'node:fs';

describe('listRecentEvents', () => {
  beforeEach(() => {
    vi.mocked(existsSync).mockReturnValue(true);
    mockAll.mockReturnValue([]);
    mockPrepare.mockClear();
    mockClose.mockClear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('prints "(no events captured yet)" if db file does not exist', () => {
    vi.mocked(existsSync).mockReturnValue(false);
    const spy = vi.spyOn(process.stdout, 'write').mockImplementation(() => true);

    listRecentEvents('/fake/.chitin', { skipEnsureIndexed: true });

    expect(spy).toHaveBeenCalledWith('(no events captured yet)\n');
    spy.mockRestore();
  });

  it('queries all events when no filters provided', () => {
    listRecentEvents('/fake/.chitin', { skipEnsureIndexed: true });

    expect(mockPrepare).toHaveBeenCalledTimes(1);
    const query = mockPrepare.mock.calls[0][0] as string;
    expect(query).toContain('FROM events');
    expect(query).not.toContain('WHERE');
  });

  it('filters by surface when provided', () => {
    listRecentEvents('/fake/.chitin', { surface: 'claude-code', skipEnsureIndexed: true });

    const query = mockPrepare.mock.calls[0][0] as string;
    expect(query).toContain('WHERE');
    expect(query).toContain('surface = ?');
  });

  it('filters by run when provided', () => {
    listRecentEvents('/fake/.chitin', { run: 'abc-123', skipEnsureIndexed: true });

    const query = mockPrepare.mock.calls[0][0] as string;
    expect(query).toContain('WHERE');
    expect(query).toContain('run_id = ?');
  });

  it('combines surface and run filters with AND', () => {
    listRecentEvents('/fake/.chitin', {
      surface: 'copilot',
      run: 'xyz',
      skipEnsureIndexed: true,
    });

    const query = mockPrepare.mock.calls[0][0] as string;
    expect(query).toContain('surface = ?');
    expect(query).toContain('AND');
    expect(query).toContain('run_id = ?');
  });

  it('uses default limit of 50 when not specified', () => {
    listRecentEvents('/fake/.chitin', { skipEnsureIndexed: true });

    // .all(...params, limit) where params=[] and limit=50
    expect(mockAll).toHaveBeenCalledWith(50);
  });

  it('uses custom limit when provided', () => {
    listRecentEvents('/fake/.chitin', { limit: 10, skipEnsureIndexed: true });

    expect(mockAll).toHaveBeenCalledWith(10);
  });

  it('formats and prints each row', () => {
    mockAll.mockReturnValue([
      { ts: '2026-05-09T12:00:00Z', surface: 'copilot', event_type: 'gate', chain_id: 'abc123def456', session_id: 's1' },
    ]);
    const spy = vi.spyOn(process.stdout, 'write').mockImplementation(() => true);

    listRecentEvents('/fake/.chitin', { skipEnsureIndexed: true });

    expect(spy).toHaveBeenCalledWith(expect.stringContaining('gate'));
    expect(spy).toHaveBeenCalledWith(expect.stringContaining('copilot'));
    spy.mockRestore();
  });

  it('closes the database connection even on empty results', () => {
    listRecentEvents('/fake/.chitin', { skipEnsureIndexed: true });

    expect(mockClose).toHaveBeenCalled();
  });

  it('calls ensureIndexed by default', async () => {
    const { ensureIndexed } = await import('@chitin/telemetry');

    listRecentEvents('/fake/.chitin', {});

    expect(ensureIndexed).toHaveBeenCalledWith('/fake/.chitin');
  });
});