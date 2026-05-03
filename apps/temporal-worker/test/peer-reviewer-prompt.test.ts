import { describe, expect, it } from 'vitest';
import { buildPeerReviewerPrompt } from '../src/peer-reviewer/prompt.ts';
import type { BacklogEntry } from '../src/grooming/parse-backlog.ts';

function makeEntry(description: string): BacklogEntry {
  return {
    id: 'peer-review-pr-199',
    status: 'ready',
    role: 'peer-reviewer',
    description,
    rawFrontmatter: '',
    rawSection: '',
  };
}

describe('buildPeerReviewerPrompt', () => {
  it('frames the role as second-opinion (not part of the R1-R3 chain)', () => {
    const prompt = buildPeerReviewerPrompt(makeEntry('x'));
    expect(prompt).toMatch(/SECOND OPINION/);
    expect(prompt).toMatch(/independent of Copilot's R0 review/);
    expect(prompt).toContain('docs/design/2026-05-02-swarm-as-software-factory.md');
  });

  it('embeds entry id and detail', () => {
    const detail = 'Review PR https://github.com/chitinhq/chitin/pull/199';
    const prompt = buildPeerReviewerPrompt(makeEntry(detail));
    expect(prompt).toContain('peer-review-pr-199');
    expect(prompt).toContain(detail);
  });

  it('lists the four review-finding axes (correctness/scope/security/observability/tests)', () => {
    const prompt = buildPeerReviewerPrompt(makeEntry('x'));
    expect(prompt).toMatch(/Correctness/);
    expect(prompt).toMatch(/Scope drift/);
    expect(prompt).toMatch(/Security/);
    expect(prompt).toMatch(/Observability/);
    expect(prompt).toMatch(/Test coverage/);
  });

  it('describes the structured output marker with the three severity buckets', () => {
    const prompt = buildPeerReviewerPrompt(makeEntry('x'));
    expect(prompt).toContain('<<<PEER_REVIEW>>>');
    expect(prompt).toMatch(/red.*yellow.*green.*verdict/);
  });

  it('explicitly enforces ONE review per dispatch (no spam)', () => {
    const prompt = buildPeerReviewerPrompt(makeEntry('x'));
    expect(prompt).toMatch(/One review per dispatch/);
  });

  it('forbids self-dispatching downstream agents', () => {
    const prompt = buildPeerReviewerPrompt(makeEntry('x'));
    expect(prompt).toMatch(/Don't dispatch a comment-responder yourself/);
    expect(prompt).toMatch(/Don't escalate to R2\/R3 directly/);
  });

  it('marks the role as read-only (no checkout, no test runs)', () => {
    const prompt = buildPeerReviewerPrompt(makeEntry('x'));
    expect(prompt).toMatch(/read-only/);
    expect(prompt).toMatch(/Don't checkout the branch/);
  });

  it('requires deduping against R0 (Copilot) before posting', () => {
    const prompt = buildPeerReviewerPrompt(makeEntry('x'));
    expect(prompt).toMatch(/Don't repeat what Copilot's R0 already flagged/);
  });
});
