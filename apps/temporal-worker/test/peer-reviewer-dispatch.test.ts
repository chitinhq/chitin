import { describe, expect, it } from 'vitest';
import {
  buildPeerReviewerEntry,
  buildPeerReviewerRequest,
  extractPrNumber,
  peerReviewerWorkflowIdForPr,
} from '../src/peer-reviewer/dispatch.ts';

describe('peerReviewerWorkflowIdForPr', () => {
  it('produces a stable id per PR number', () => {
    expect(peerReviewerWorkflowIdForPr(199)).toBe('peer-review-pr-199');
    expect(peerReviewerWorkflowIdForPr(207)).toBe('peer-review-pr-207');
  });
});

describe('extractPrNumber', () => {
  it('parses a github pull URL', () => {
    expect(extractPrNumber('https://github.com/chitinhq/chitin/pull/207')).toBe(207);
  });

  it('throws on a URL without /pull/', () => {
    expect(() => extractPrNumber('https://example.com/x')).toThrow(/does not contain/);
  });
});

describe('buildPeerReviewerEntry', () => {
  it('embeds the PR URL in the description (the prompt step-0 guard reads it)', () => {
    const entry = buildPeerReviewerEntry(
      'https://github.com/chitinhq/chitin/pull/207',
      'chitinhq/chitin',
    );
    expect(entry.description).toContain('https://github.com/chitinhq/chitin/pull/207');
    expect(entry.role).toBe('peer-reviewer');
    expect(entry.id).toBe('peer-review-pr-207');
    expect(entry.status).toBe('ready');
  });

  it('includes the repo slug', () => {
    const entry = buildPeerReviewerEntry(
      'https://github.com/chitinhq/chitin/pull/1',
      'chitinhq/chitin',
    );
    expect(entry.description).toContain('chitinhq/chitin');
  });
});

describe('buildPeerReviewerRequest', () => {
  it('produces an ExecutionRequest with read-only bounds', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/207',
      repo: 'chitinhq/chitin',
    });
    expect(req.write_policy).toBe('none');
    expect(req.network_policy).toBe('allowlist');
    expect(req.role).toBe('peer-reviewer');
    expect(req.task_class).toBe('exploration');
    expect(req.risk_level).toBe('low');
  });

  it('omits base_ref so the activity skips worktree+apply', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.base_ref).toBeUndefined();
  });

  it('default driver is copilot, default tier is T2', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.allowed_drivers).toEqual(['copilot']);
    expect(req.tier).toBe('T2');
  });

  it('honors driver + tier overrides', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
      driver: 'claude-code-headless',
      tier: 'T3',
    });
    expect(req.allowed_drivers).toEqual(['claude-code-headless']);
    expect(req.tier).toBe('T3');
  });

  it('workflow_id matches the stable per-PR id', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/207',
      repo: 'chitinhq/chitin',
    });
    expect(req.workflow_id).toBe('peer-review-pr-207');
  });

  it('embeds the prompt with the PR URL in entry detail (step-0 guard wired)', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/207',
      repo: 'chitinhq/chitin',
    });
    expect(req.prompt).toContain('https://github.com/chitinhq/chitin/pull/207');
    // Sanity: the prompt's step-0 guard markers are present
    expect(req.prompt).toMatch(/Verify your dispatch shape FIRST/);
  });

  it('wall_timeout=900s — peer review shouldn\'t be slow', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.bounds.wall_timeout_s).toBe(900);
  });
});
