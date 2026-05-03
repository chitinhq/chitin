import { describe, expect, it } from 'vitest';
import {
  buildCommentResponderEntry,
  buildCommentResponderRequest,
  commentResponderWorkflowIdForPr,
} from '../src/comment-responder/dispatch.ts';

describe('commentResponderWorkflowIdForPr', () => {
  it('produces a stable id per PR number', () => {
    expect(commentResponderWorkflowIdForPr(199)).toBe('comment-respond-pr-199');
    expect(commentResponderWorkflowIdForPr(207)).toBe('comment-respond-pr-207');
  });
});

describe('buildCommentResponderEntry', () => {
  it('embeds the PR URL in the description (prompt step-0 guard reads it)', () => {
    const entry = buildCommentResponderEntry(
      'https://github.com/chitinhq/chitin/pull/207',
      'chitinhq/chitin',
    );
    expect(entry.description).toContain('https://github.com/chitinhq/chitin/pull/207');
    expect(entry.role).toBe('comment-responder');
    expect(entry.id).toBe('comment-respond-pr-207');
  });

  it('mentions the do-NOT-dismiss-as-noise rule in the description', () => {
    const entry = buildCommentResponderEntry(
      'https://github.com/chitinhq/chitin/pull/1',
      'chitinhq/chitin',
    );
    expect(entry.description).toMatch(/do NOT dismiss as noise/);
  });
});

describe('buildCommentResponderRequest', () => {
  it('produces an ExecutionRequest with branch-write bounds', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/207',
      repo: 'chitinhq/chitin',
    });
    expect(req.write_policy).toBe('branch');
    expect(req.network_policy).toBe('allowlist');
    expect(req.role).toBe('comment-responder');
    expect(req.task_class).toBe('bug_fix');
    expect(req.risk_level).toBe('medium');
  });

  it('omits base_ref so the activity skips worktree+apply (agent does its own gh pr checkout)', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.base_ref).toBeUndefined();
  });

  it('default driver is copilot at T2 (judgment-tier reasoning)', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.allowed_drivers).toEqual(['copilot']);
    expect(req.tier).toBe('T2');
  });

  it('honors driver + tier overrides', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
      driver: 'claude-code-headless',
      tier: 'T4',
    });
    expect(req.allowed_drivers).toEqual(['claude-code-headless']);
    expect(req.tier).toBe('T4');
  });

  it('caps tool calls at 80 (handles 5-15 comments × evaluate+edit+commit+push)', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.bounds.max_tool_calls).toBe(80);
  });

  it('wall_timeout=1800s — tracks the R3 reviewer ceiling', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.bounds.wall_timeout_s).toBe(1800);
  });

  it('workflow_id matches the stable per-PR id', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/207',
      repo: 'chitinhq/chitin',
    });
    expect(req.workflow_id).toBe('comment-respond-pr-207');
  });

  it('embeds the prompt with the PR URL in entry detail (step-0 guard wired)', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/207',
      repo: 'chitinhq/chitin',
    });
    expect(req.prompt).toContain('https://github.com/chitinhq/chitin/pull/207');
    expect(req.prompt).toMatch(/Verify your dispatch shape FIRST/);
    expect(req.prompt).toMatch(/Reply to each individual review comment thread/);
  });
});
