// pr-event-ingester.ts
// Polls open PRs, matches §5 trigger matrix, enqueues review-graph workflows as needed.

import { execSync } from 'child_process';
import { TemporalClient } from './temporal-client';
import { enqueueReviewGraph } from './review-graph';
import { computeStartingTier } from './review-graph';
import { writeChainEvent } from './chain-events';

const POLL_INTERVAL_MS = 5 * 60 * 1000; // 5 minutes
const REPO = 'chitinhq/chitin';
const DISPATCHER_BRANCH_PREFIX = 'swarm/';

async function fetchOpenPRs() {
  const output = execSync(`gh api repos/${REPO}/pulls?state=open`, { encoding: 'utf-8' });
  return JSON.parse(output);
}

async function fetchReviewGraphWorkflows(temporal: TemporalClient) {
  // List all running review-graph workflows (by workflowId suffix)
  return temporal.listWorkflows({
    query: 'WorkflowType="review-graph" AND ExecutionStatus="Running"',
  });
}

function isDispatcherPR(pr: any) {
  return pr.head && pr.head.ref && pr.head.ref.startsWith(DISPATCHER_BRANCH_PREFIX);
}

function getParentWorkflowId(pr: any) {
  return `pr-ingest-${pr.number}`;
}

function getReviewGraphWorkflowId(parentWorkflowId: string) {
  return `${parentWorkflowId}-review-graph`;
}

function buildBacklogEntry(pr: any, tier: number, files: string[]) {
  return {
    tier,
    role: 'reviewer',
    file: files,
    parent_workflow_id: getParentWorkflowId(pr),
    pr_number: pr.number,
    pr_url: pr.html_url,
  };
}

async function fetchPRFiles(prNumber: number) {
  const output = execSync(`gh api repos/${REPO}/pulls/${prNumber}/files`, { encoding: 'utf-8' });
  const files = JSON.parse(output);
  return files.map((f: any) => f.filename);
}

async function fetchPRComments(prNumber: number) {
  const output = execSync(`gh api repos/${REPO}/pulls/${prNumber}/comments`, { encoding: 'utf-8' });
  return JSON.parse(output);
}

async function ingestPRs() {
  const temporal = new TemporalClient();
  const openPRs = await fetchOpenPRs();
  const runningWorkflows = await fetchReviewGraphWorkflows(temporal);
  const runningIds = new Set(runningWorkflows.map((w: any) => w.workflowId));

  for (const pr of openPRs) {
    let decision = 'skipped';
    try {
      if (isDispatcherPR(pr)) {
        decision = 'skipped_dispatcher_pr';
        continue;
      }
      const parentWorkflowId = getParentWorkflowId(pr);
      const reviewGraphWorkflowId = getReviewGraphWorkflowId(parentWorkflowId);
      if (runningIds.has(reviewGraphWorkflowId)) {
        decision = 'skipped_already_running';
        continue;
      }
      const files = await fetchPRFiles(pr.number);
      const comments = await fetchPRComments(pr.number);
      const reviewTier = computeStartingTier({
        pr,
        files,
        comments,
      });
      if (reviewTier === 0) {
        decision = 'skipped_r0';
        continue;
      }
      // Only dispatch for R1–R3
      if (reviewTier >= 1 && reviewTier <= 3) {
        const backlogEntry = buildBacklogEntry(pr, reviewTier, files);
        await enqueueReviewGraph(backlogEntry);
        decision = 'dispatched';
      } else {
        decision = 'skipped_non_dispatchable';
      }
    } catch (err) {
      decision = 'errored';
    } finally {
      await writeChainEvent({
        kind: 'pr_ingest_decision',
        pr_number: pr.number,
        decision,
      });
    }
  }
}

if (require.main === module) {
  ingestPRs().catch((err) => {
    console.error('PR event ingester failed:', err);
    process.exit(1);
  });
}
