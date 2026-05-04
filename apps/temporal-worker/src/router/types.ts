// Shared types for the router pipeline. The pipeline shape:
//   stdin (Claude Code PreToolUse payload)
//     → kernel hook (deterministic verdict)
//     → heuristics (blast-radius, drift, floundering)
//     → advisor (LLM nudge if heuristic fires)
//     → stdout (composed verdict + advisor message)

/** Inbound payload from Claude Code's PreToolUse hook protocol. */
export interface HookInput {
  hook_event_name: 'PreToolUse';
  tool_name: string;
  tool_input: Record<string, unknown>;
  cwd?: string;
  session_id?: string;
}

/** Outbound payload back to Claude Code. The `decision` field
 *  controls execution; `message` is shown to the agent in its
 *  next observation. */
export interface HookOutput {
  decision: 'allow' | 'deny';
  message?: string;
  /** Internal: which path produced this decision (kernel | heuristic | advisor). */
  source?: 'kernel' | 'heuristic-deny' | 'advisor-allow' | 'advisor-deny' | 'kernel-allow';
}

/** Heuristic score — pure function over (action, session-context). */
export interface HeuristicScore {
  /** 0.0–1.0. Higher = more concerning. */
  score: number;
  /** Whether this heuristic fired (score >= configured threshold). */
  fired: boolean;
  /** Short-form reason for telemetry. */
  reason: string;
  /** Free-form per-axis breakdown for debugging. */
  axis?: Record<string, number>;
}

/** Combined heuristic outcome — one entry per heuristic that ran. */
export interface HeuristicOutcome {
  blast_radius?: HeuristicScore;
  drift?: HeuristicScore;
  floundering?: HeuristicScore;
  /** Aggregate: any heuristic fired? */
  any_fired: boolean;
}

/** Advisor request payload. */
export interface AdvisorRequest {
  question: string;
  context: string;
  proposed_action: HookInput;
  heuristic_outcome: HeuristicOutcome;
  /** Tier of the calling agent (informs which advisor model is selected). */
  caller_tier?: string;
  /** Depth of the chain — used to bound recursion. */
  chain_depth: number;
}

/** Advisor response payload. */
export interface AdvisorResponse {
  /** Free-form nudge text shown to the agent. */
  nudge: string;
  /** Verdict: continue lets the action through, takeover blocks + signals re-dispatch. */
  verdict: 'continue' | 'takeover';
  /** If true, the router should chain to the next-tier advisor. */
  escalate: boolean;
  /** Optional cost telemetry (tokens / dollars / wall_ms). */
  cost?: { tokens?: number; usd?: number; wall_ms?: number };
}

/** Router policy — read from chitin.yaml's `router:` section. */
export interface RouterPolicy {
  enabled: boolean;
  heuristics: {
    blast_radius?: { enabled: boolean; threshold: number };
    drift?: { enabled: boolean; threshold: number };
    floundering?: {
      enabled: boolean;
      max_loop_count: number;
      max_stall_seconds: number;
    };
  };
  advisor: {
    enabled: boolean;
    /** Triggers — any matched fires the advisor. */
    when: Array<
      | 'blast_radius_above_threshold'
      | 'drift_detected'
      | 'floundering_detected'
      | 'kernel_denied'
    >;
    chain: { max_depth: number; tier_steps: string[] };
    /** Model id (e.g., 'claude-code-headless', 'gemini-cli', 'openclaw-glm-flash'). */
    model: string;
  };
}

/** Default policy used when chitin.yaml omits the router section. */
export const DEFAULT_ROUTER_POLICY: RouterPolicy = {
  enabled: false, // off by default — operator opts in
  heuristics: {
    blast_radius: { enabled: true, threshold: 0.6 },
    drift: { enabled: true, threshold: 0.5 },
    floundering: { enabled: true, max_loop_count: 3, max_stall_seconds: 600 },
  },
  advisor: {
    enabled: true,
    when: ['blast_radius_above_threshold', 'floundering_detected'],
    chain: { max_depth: 3, tier_steps: ['T2', 'T3', 'T4'] },
    model: 'claude-code-headless',
  },
};
