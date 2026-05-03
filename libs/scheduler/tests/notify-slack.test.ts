import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// Test the two safety properties Copilot called out:
//   1. dry-run never touches the network
//   2. missing webhook URL is a no-op even in live mode
//
// The notifier reads SLACK_WEBHOOK_URL at module-load time (chitin-home
// secrets file → CHITIN_SLACK_WEBHOOK_URL env var → undefined). Tests
// use vi.resetModules() to control which version of the module loads,
// since the module-level constant is what gates the fetch.

describe('slack notifier — safety properties', () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    // Don't let any host env leak into the module's webhook resolution.
    delete process.env.CHITIN_SLACK_WEBHOOK_URL;
    process.env.CHITIN_HOME = '/nonexistent-test-home-' + Math.random();
    vi.resetModules();
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('ok', { status: 200 }) as Response,
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
    delete process.env.CHITIN_SLACK_WEBHOOK_URL;
    delete process.env.CHITIN_HOME;
  });

  it('dry-run never calls fetch — even with webhook configured', async () => {
    process.env.CHITIN_SLACK_WEBHOOK_URL = 'https://hooks.slack.com/test';
    vi.resetModules();
    const { runSmokeTest } = await import('../src/notify/slack.js');

    await runSmokeTest({ live: false });

    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('live mode with no webhook is a no-op (zero fetch calls)', async () => {
    // Arrange: no env var, nonexistent CHITIN_HOME → no webhook resolves.
    const { runSmokeTest } = await import('../src/notify/slack.js');

    await runSmokeTest({ live: true });

    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('live mode with webhook configured calls fetch exactly once', async () => {
    process.env.CHITIN_SLACK_WEBHOOK_URL = 'https://hooks.slack.com/test';
    vi.resetModules();
    const { runSmokeTest } = await import('../src/notify/slack.js');

    await runSmokeTest({ live: true });

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url] = fetchSpy.mock.calls[0]!;
    expect(url).toBe('https://hooks.slack.com/test');
  });

  it('CHITIN_HOME redirection is honored (not hard-coded ~/.chitin)', async () => {
    // The webhook lookup resolves the chitin-home dir via $CHITIN_HOME
    // first, falling back to ~/.chitin. With $CHITIN_HOME set to a
    // nonexistent path, no webhook file is found, so live mode is a
    // no-op (already covered above) — but also no exception is raised.
    process.env.CHITIN_HOME = '/this-path-does-not-exist';
    vi.resetModules();
    const { runSmokeTest } = await import('../src/notify/slack.js');

    await expect(runSmokeTest({ live: true })).resolves.toBeUndefined();
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
