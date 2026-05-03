import { describe, expect, it } from 'vitest';
import { buildCommentResponderPrompt } from '../src/comment-responder/prompt.ts';
import type { BacklogEntry } from '../src/grooming/parse-backlog.ts';

function makeEntry(description: string): BacklogEntry {
  return {
    id: 'comment-respond-pr-199',
    status: 'ready',
    role: 'comment-responder',
    description,
    rawFrontmatter: '',
    rawSection: '',
  };
}

describe('buildCommentResponderPrompt', () => {
  it('frames the role with reference to the factory design', () => {
    const prompt = buildCommentResponderPrompt(makeEntry('Address comments on PR #199'));
    expect(prompt).toMatch(/comment-responder role/);
    expect(prompt).toContain('docs/design/2026-05-02-swarm-as-software-factory.md');
  });

  it('cites the operator do-not-dismiss-as-noise rule', () => {
    const prompt = buildCommentResponderPrompt(makeEntry('x'));
    expect(prompt).toMatch(/do NOT dismiss as noise/i);
    expect(prompt).toMatch(/PR #78 caught 8 of 11/);
  });

  it('surfaces the entry detail verbatim', () => {
    const detail = 'Address comments on PR https://github.com/chitinhq/chitin/pull/199';
    const prompt = buildCommentResponderPrompt(makeEntry(detail));
    expect(prompt).toContain(detail);
  });

  it('embeds the entry id', () => {
    const prompt = buildCommentResponderPrompt(makeEntry('x'));
    expect(prompt).toContain('comment-respond-pr-199');
  });

  it('lists the three decision verbs (APPLY / DISMISS / ESCALATE)', () => {
    const prompt = buildCommentResponderPrompt(makeEntry('x'));
    expect(prompt).toContain('APPLY');
    expect(prompt).toContain('DISMISS');
    expect(prompt).toContain('ESCALATE');
  });

  it('describes the structured output marker', () => {
    const prompt = buildCommentResponderPrompt(makeEntry('x'));
    expect(prompt).toContain('<<<COMMENT_RESPONSE>>>');
    expect(prompt).toMatch(/applied.*dismissed.*escalated/);
  });

  it('forbids dismissal without source-of-truth citation', () => {
    const prompt = buildCommentResponderPrompt(makeEntry('x'));
    expect(prompt).toMatch(/DISMISS without.*source.*forbidden/i);
  });

  it('forbids disabling tests as a fix', () => {
    const prompt = buildCommentResponderPrompt(makeEntry('x'));
    expect(prompt).toMatch(/Don't disable tests/);
  });

  it('caps the agent at one commit per dispatch (re-dispatch ladder lives in dispatcher)', () => {
    const prompt = buildCommentResponderPrompt(makeEntry('x'));
    expect(prompt).toMatch(/AT MOST ONE commit/);
  });

  it('guards against wrong-dispatcher-path: refuse to act without a PR URL', () => {
    const prompt = buildCommentResponderPrompt(makeEntry('x'));
    expect(prompt).toMatch(/Verify your dispatch shape FIRST/);
    expect(prompt).toMatch(/no PR URL in dispatch context/);
    expect(prompt).toMatch(/skipped_reason/);
  });

  it('instructs per-thread replies for durable resolution', () => {
    const prompt = buildCommentResponderPrompt(makeEntry('x'));
    expect(prompt).toMatch(/Reply to each individual review comment thread/);
    expect(prompt).toMatch(/comments\/<comment_id>\/replies/);
    expect(prompt).toMatch(/durable record/);
  });
});
