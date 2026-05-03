// Blast-radius heuristic: scores how big the consequences of an
// action are. Pure function over the hook input.
//
// Four axes (0.0–1.0 each, weighted into a single score):
//   - reversibility (inverted: 0=fully reversible, 1=irreversible)
//   - scope         (0=single file/no-op, 1=mass change/whole repo)
//   - visibility    (0=local, 1=public-internet effect like git push to public repo)
//   - counterparties (0=self only, 1=external service/user impacted)
//
// Combined: irreversibility carries most weight (0.4); scope 0.25;
// visibility 0.2; counterparties 0.15.

import type { HeuristicScore, HookInput } from '../types.ts';

interface Axes extends Record<string, number> {
  reversibility: number;
  scope: number;
  visibility: number;
  counterparties: number;
}

/** Pure: classify a hook input into axes. */
export function scoreAxes(input: HookInput): { axes: Axes; reason: string } {
  const command =
    typeof input.tool_input.command === 'string' ? input.tool_input.command : '';
  const filePath =
    typeof input.tool_input.file_path === 'string'
      ? input.tool_input.file_path
      : typeof input.tool_input.notebook_path === 'string'
        ? input.tool_input.notebook_path
        : '';

  // Read-shaped tools: maximally safe
  const readShaped = new Set([
    'Read', 'Glob', 'Grep', 'LS', 'TaskGet', 'TaskList', 'TaskOutput',
    'ToolSearch', 'AskUserQuestion', 'EnterPlanMode', 'ExitPlanMode',
  ]);
  if (readShaped.has(input.tool_name)) {
    return {
      axes: { reversibility: 1.0, scope: 0.0, visibility: 0.0, counterparties: 0.0 },
      reason: 'read-only-tool',
    };
  }

  // Outbound network
  const outboundNet = new Set(['WebFetch', 'WebSearch', 'PushNotification', 'RemoteTrigger']);
  if (outboundNet.has(input.tool_name)) {
    return {
      axes: { reversibility: 0.7, scope: 0.0, visibility: 0.5, counterparties: 0.7 },
      reason: 'outbound-network',
    };
  }

  // Local-file-write
  if (['Edit', 'Write', 'NotebookEdit', 'TodoWrite'].includes(input.tool_name)) {
    let scope = 0.2;
    let reason = 'local-file-write';
    if (/\/(chitin\.yaml|CLAUDE\.md|\.claude\/)/.test(filePath)) {
      scope = 0.8;
      reason = 'governance-config-write';
    } else if (/\/(node_modules|\.git|dist|build)\//.test(filePath)) {
      scope = 0.9;
      reason = 'generated-or-vcs-write';
    }
    return {
      axes: { reversibility: 0.6, scope, visibility: 0.0, counterparties: 0.0 },
      reason,
    };
  }

  // Bash: shape by command pattern
  if (input.tool_name === 'Bash') {
    if (/\brm\s+(-[rfRF]+\s+|--recursive\s+)/.test(command)) {
      return {
        axes: { reversibility: 0.0, scope: 1.0, visibility: 0.0, counterparties: 0.0 },
        reason: 'recursive-delete',
      };
    }
    if (/\bgit\s+push\s+(--force|-f)\b/.test(command)) {
      return {
        axes: { reversibility: 0.0, scope: 0.5, visibility: 0.9, counterparties: 0.7 },
        reason: 'force-push',
      };
    }
    if (/\bgit\s+reset\s+--hard\b/.test(command)) {
      return {
        axes: { reversibility: 0.0, scope: 0.4, visibility: 0.0, counterparties: 0.0 },
        reason: 'hard-reset',
      };
    }
    if (/\bgit\s+push\b/.test(command)) {
      return {
        axes: { reversibility: 0.2, scope: 0.4, visibility: 0.7, counterparties: 0.6 },
        reason: 'git-push',
      };
    }
    if (/\bgh\s+pr\s+(create|merge)\b|\bgh\s+issue\s+close\b/.test(command)) {
      return {
        axes: { reversibility: 0.4, scope: 0.3, visibility: 0.8, counterparties: 0.8 },
        reason: 'github-state-change',
      };
    }
    if (/\b(npm|pnpm)\s+publish\b/.test(command)) {
      return {
        axes: { reversibility: 0.0, scope: 0.5, visibility: 1.0, counterparties: 1.0 },
        reason: 'package-publish',
      };
    }
    if (/\b(curl|wget)\b/.test(command)) {
      return {
        axes: { reversibility: 0.7, scope: 0.0, visibility: 0.3, counterparties: 0.5 },
        reason: 'network-out',
      };
    }
    return {
      axes: { reversibility: 0.5, scope: 0.3, visibility: 0.0, counterparties: 0.0 },
      reason: 'generic-shell-exec',
    };
  }

  // Orchestration / scheduling — typically local, reversible
  const orchestration = new Set([
    'Task', 'Agent', 'Skill', 'TaskCreate', 'TaskUpdate', 'TaskStop',
    'CronCreate', 'CronDelete', 'CronList', 'ScheduleWakeup', 'Monitor',
    'EnterWorktree', 'ExitWorktree',
  ]);
  if (orchestration.has(input.tool_name)) {
    return {
      axes: { reversibility: 0.5, scope: 0.3, visibility: 0.0, counterparties: 0.0 },
      reason: 'orchestration-shape',
    };
  }

  // Unknown tool — moderate caution
  return {
    axes: { reversibility: 0.4, scope: 0.4, visibility: 0.0, counterparties: 0.0 },
    reason: `unknown-tool:${input.tool_name}`,
  };
}

/**
 * Pure: compute the blast-radius score for a hook input.
 * Threshold comparison happens at the caller.
 */
export function scoreBlastRadius(input: HookInput, threshold = 0.6): HeuristicScore {
  const { axes, reason } = scoreAxes(input);
  const score =
    (1 - axes.reversibility) * 0.4 +
    axes.scope * 0.25 +
    axes.visibility * 0.2 +
    axes.counterparties * 0.15;
  return {
    score: Number(score.toFixed(3)),
    fired: score >= threshold,
    reason,
    axis: axes,
  };
}
