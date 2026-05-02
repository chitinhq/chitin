// Tests for the Slack notifier. We assert two invariants:
//   1. With CHITIN_SLACK_WEBHOOK_URL unset, every notify* call is a
//      true no-op — fetch must never be called.
//   2. extractPrUrl correctly pulls the GitHub PR URL out of the apply
//      step's stdout/stderr, which is how the dispatcher decides
//      whether to attach a PR link to the Slack message.
//
// Why no end-to-end test against a fake server: the notifier is fire-
// and-forget; correctness of "Slack received the right blocks" is
// confirmed by manual smoke-test once the user wires the webhook.

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';

describe('notify (Slack disabled)', () => {
  let originalUrl: string | undefined;
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    originalUrl = process.env.CHITIN_SLACK_WEBHOOK_URL;
    delete process.env.CHITIN_SLACK_WEBHOOK_URL;
    fetchSpy = vi.spyOn(globalThis, 'fetch' as never);
    vi.resetModules();
  });

  afterEach(() => {
    if (originalUrl === undefined) delete process.env.CHITIN_SLACK_WEBHOOK_URL;
    else process.env.CHITIN_SLACK_WEBHOOK_URL = originalUrl;
    fetchSpy.mockRestore();
    vi.resetModules();
  });

  it('notifyDispatchStart is a no-op when webhook URL is unset', async () => {
    const { notifyDispatchStart } = await import('../src/notify.ts');
    await notifyDispatchStart({
      entry_id: 'test-entry',
      tier: 'T1',
      driver: 'copilot',
      workflow_id: 'wf-1',
    });
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('notifyDispatchComplete is a no-op when webhook URL is unset', async () => {
    const { notifyDispatchComplete } = await import('../src/notify.ts');
    await notifyDispatchComplete({
      entry_id: 'test-entry',
      workflow_id: 'wf-1',
      exit_code: 0,
      duration_ms: 1000,
      commits_added: 1,
      uncommitted: false,
      pr_url: 'https://github.com/org/repo/pull/42',
    });
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('notifyDispatchError is a no-op when webhook URL is unset', async () => {
    const { notifyDispatchError } = await import('../src/notify.ts');
    await notifyDispatchError({
      entry_id: 'test-entry',
      workflow_id: 'wf-1',
      stage: 'submit',
      error: 'boom',
    });
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});

describe('extractPrUrl', () => {
  it('finds the first github PR url in arbitrary text', async () => {
    const { extractPrUrl } = await import('../src/notify.ts');
    const text = `
      ok 12 files changed, 30 insertions(+), 5 deletions(-)
      pull request created at https://github.com/chitinhq/chitin/pull/107
      done
    `;
    expect(extractPrUrl(text)).toBe('https://github.com/chitinhq/chitin/pull/107');
  });

  it('returns undefined when no PR url is present', async () => {
    const { extractPrUrl } = await import('../src/notify.ts');
    expect(extractPrUrl('apply skipped — no tracked diff')).toBeUndefined();
  });

  it('matches owners and repos with dots, dashes, underscores', async () => {
    const { extractPrUrl } = await import('../src/notify.ts');
    const text = 'See https://github.com/foo.bar/baz_qux/pull/9';
    expect(extractPrUrl(text)).toBe('https://github.com/foo.bar/baz_qux/pull/9');
  });

  it('picks the first url when multiple are present', async () => {
    const { extractPrUrl } = await import('../src/notify.ts');
    const text = `
      previous: https://github.com/a/b/pull/1
      current:  https://github.com/a/b/pull/2
    `;
    expect(extractPrUrl(text)).toBe('https://github.com/a/b/pull/1');
  });
});

describe('notify (Slack enabled, mocked fetch)', () => {
  let originalUrl: string | undefined;
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    originalUrl = process.env.CHITIN_SLACK_WEBHOOK_URL;
    process.env.CHITIN_SLACK_WEBHOOK_URL = 'https://hooks.slack.com/services/T/B/C';
    fetchSpy = vi
      .spyOn(globalThis, 'fetch' as never)
      .mockResolvedValue(new Response('ok', { status: 200 }) as never);
    vi.resetModules();
  });

  afterEach(() => {
    if (originalUrl === undefined) delete process.env.CHITIN_SLACK_WEBHOOK_URL;
    else process.env.CHITIN_SLACK_WEBHOOK_URL = originalUrl;
    fetchSpy.mockRestore();
    vi.resetModules();
  });

  it('posts to the webhook with a JSON body when enabled', async () => {
    const { notifyDispatchStart } = await import('../src/notify.ts');
    await notifyDispatchStart({
      entry_id: 'foo',
      tier: 'T0',
      driver: 'copilot',
      workflow_id: 'wf-x',
    });
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('https://hooks.slack.com/services/T/B/C');
    expect(init.method).toBe('POST');
    const body = JSON.parse(init.body as string);
    expect(body.text).toContain('foo');
    expect(body.text).toContain('T0');
    expect(body.blocks).toBeDefined();
  });

  it('does not throw when fetch rejects (visibility is best-effort)', async () => {
    fetchSpy.mockRejectedValueOnce(new Error('connection refused'));
    const { notifyDispatchStart } = await import('../src/notify.ts');
    await expect(
      notifyDispatchStart({
        entry_id: 'foo',
        tier: 'T0',
        driver: 'copilot',
        workflow_id: 'wf-x',
      }),
    ).resolves.toBeUndefined();
  });
});
