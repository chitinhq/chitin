package gov

// Contract: chitin-kernel's Go gate is the only authority that computes Allow/Deny policy decisions.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// captureCallerOrigin returns "file.go:line" of the caller skip frames up
// the stack, or "" if runtime.Caller fails. Used to self-identify gate
// callers that don't pass a *BudgetEnvelope, so the audit log stops being
// silent about which paths bypass cost-governance.
//
// skip=2 means: skip captureCallerOrigin's own frame and Gate.Evaluate's
// frame, returning the frame of Evaluate's caller.
func captureCallerOrigin(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return ""
	}
	return filepath.Base(file) + ":" + strconv.Itoa(line)
}

// Gate orchestrates policy evaluation, bounds check, escalation counting,
// envelope spend, and decision logging. One instance per gate subprocess
// invocation.
//
// EstimateCost and ClassifyTier are optional callbacks that decouple the
// gov package from internal/cost and internal/tier (which import gov for
// Action). Wire them in at the cmd layer; nil means "default behavior":
// tier=Unset, delta={ToolCalls:1}.
//
// OnDecision is the F4-addendum callback that fires after WriteLog with
// the final Decision. The CLI layer wires it to emit a `decision` chain
// event via the canonical emit path, so F4's OTEL projection picks up
// every gated action across every driver. Optional; nil preserves the
// pre-F4 audit-log-only behavior.
type Gate struct {
	Policy       Policy
	Counter      *Counter
	LogDir       string
	Cwd          string
	EstimateCost func(a Action, agent string) CostDelta
	ClassifyTier func(a Action) Tier
	OnDecision   func(d *Decision)
	// Agent identity dimensions stamped onto every Decision this Gate
	// writes when populated. The CLI hook layer reads these from CHITIN_*
	// env vars and sets them on the Gate at construction. Empty values
	// omit the JSON field (omitempty on Decision); pre-identity dispatches
	// keep writing the smaller schema with no breakage.
	Fingerprint FingerprintContext

	// ChainID and SessionID correlate every Decision this Gate writes to
	// the kernel event-chain and the dispatching agent's session. The CLI
	// hook layer sets them from the hook payload's session id (or the
	// `gate evaluate --session-id` flag). Empty values omit the JSON
	// field. Stamped independently of the envelope so a decision groups
	// into its session in the console even when no envelope is active.
	ChainID   string
	SessionID string

	// NoRecord, when true, suppresses persistent side effects: no
	// Counter.RecordDenial increment, no WriteLog chain-event append.
	// Read-only state (Counter.IsLocked, Counter.Level) is still
	// consulted so the returned Decision reflects current escalation
	// without mutating it. Used by `gate evaluate --no-record` so an
	// operator validating policy rules ad-hoc doesn't pollute the
	// production agent_state DB and trip lockdown for the real agent
	// of the same name (root cause of 2026-05-06's hermes-locked-by-
	// smoke-tests incident: protected-system-path probes against
	// --agent=hermes drove real hermes past the threshold). Production
	// hooks (gate_hook.go) leave this false so audit + escalation work
	// as designed.
	NoRecord bool

	// EscalateStore + NotifyHermes were removed in cull Phase 3
	// (2026-05-08). Hermes' tools/approval.py provides the operator-
	// prompt + reply-parse + persistent-allowlist natively; chitin
	// no longer maintains its own pending_approvals + bridge POST.
}

// FingerprintContext carries the typed identity dimensions the kernel
// stamps on every Decision it writes. All optional — when missing the
// Decision row omits the corresponding JSON field. See
// libs/contracts/src/fingerprint.ts for the canonical-hash side.
type FingerprintContext struct {
	AgentInstanceID   string
	AgentFingerprint  string
	Driver            string
	Model             string
	Role              string
	StationPromptHash string
	SkillsToolsHash   string
	SoulLens          string
	ClaimedAuthority  string
	Authority         string
	WorkflowID        string
	Fingerprint       string
}

// FingerprintContextFromEnv reads the CHITIN_* env vars that the
// dispatching agent sets on its spawn. Single helper so every Gate
// constructor in the kernel + drivers populates the same way; before
// this helper, gate_hook.go was the only caller, leaving copilot
// driver runs and the operator-CLI gate-evaluate path writing
// fingerprint-less rows even when the env was set (Copilot finding
// on PR #294).
//
// Read precedence per dimension is:
//
//  1. CHITIN_<DIM> — the direct name read by the kernel.
//  2. CHITIN_DISPATCH_<DIM> — the dispatch_meta-shaped name introduced
//     by PR #344 so the dispatcher's (role, model, tier, driver) tuple
//     can flow into the kernel without re-stamping the legacy vars.
//
// Default: when neither var is populated, Role falls back to
// "external" so the chain-event/(decision-row) (untagged) bucket
// shrinks to genuinely-external events (raw shell hooks, ad-hoc
// operator CLI calls). Without this default, ~92% of rows landed in
// the (untagged) × (none) bucket of `fingerprint-outcomes` simply
// because no dispatch context was wired through. System-level
// housekeeping (chain rotation, alarm-feeder) must set
// CHITIN_ROLE=system explicitly so it stays separable from external.
//
// Model has no string-typed default: a missing dispatch context is
// represented as an empty string, which the Decision JSON layer drops
// via omitempty (downstream readers interpret a missing field as
// null per the schema).
//
// Reads are best-effort — missing env vars produce empty strings.
func FingerprintContextFromEnv() FingerprintContext {
	role := firstNonEmpty(os.Getenv("CHITIN_ROLE"), os.Getenv("CHITIN_DISPATCH_ROLE"))
	if role == "" {
		role = "external"
	}
	agentFingerprint := firstNonEmpty(
		os.Getenv("CHITIN_AGENT_FINGERPRINT"),
		os.Getenv("CHITIN_FINGERPRINT"),
		os.Getenv("CHITIN_DISPATCH_AGENT_FINGERPRINT"),
	)
	return FingerprintContext{
		AgentInstanceID: firstNonEmpty(
			os.Getenv("CHITIN_AGENT_INSTANCE_ID"),
			os.Getenv("CHITIN_DISPATCH_AGENT_INSTANCE_ID"),
		),
		AgentFingerprint: agentFingerprint,
		Driver:           firstNonEmpty(os.Getenv("CHITIN_DRIVER"), os.Getenv("CHITIN_DISPATCH_DRIVER")),
		Model:            firstNonEmpty(os.Getenv("CHITIN_MODEL"), os.Getenv("CHITIN_DISPATCH_MODEL")),
		Role:             role,
		StationPromptHash: firstNonEmpty(
			os.Getenv("CHITIN_STATION_PROMPT_HASH"),
			os.Getenv("CHITIN_DISPATCH_STATION_PROMPT_HASH"),
		),
		SkillsToolsHash: firstNonEmpty(
			os.Getenv("CHITIN_SKILLS_TOOLS_HASH"),
			os.Getenv("CHITIN_DISPATCH_SKILLS_TOOLS_HASH"),
		),
		SoulLens: firstNonEmpty(
			os.Getenv("CHITIN_SOUL_LENS"),
			os.Getenv("CHITIN_DISPATCH_SOUL_LENS"),
			os.Getenv("CHITIN_ACTIVE_SOUL"),
		),
		ClaimedAuthority: firstNonEmpty(os.Getenv("CHITIN_AUTHORITY"), os.Getenv("CHITIN_DISPATCH_AUTHORITY")),
		WorkflowID: firstNonEmpty(
			os.Getenv("CHITIN_WORKFLOW_ID"),
			os.Getenv("CHITIN_DISPATCH_WORKFLOW_ID"),
		),
		Fingerprint: agentFingerprint,
	}
}

// firstNonEmpty returns the first argument that is not an empty string,
// or "" if all are empty. Used by FingerprintContextFromEnv to express
// the CHITIN_<DIM> → CHITIN_DISPATCH_<DIM> precedence in one line per
// dimension without nested if-trees.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Evaluate is the single entry point: normalize-already-done Action →
// Decision, with side effects (counter increment on policy/bounds deny,
// envelope debit on allow, log append).
//
// The envelope parameter is optional:
//   - nil: v1 behavior — pure policy gate, no envelope plumbing, no
//     extra fields on Decision.
//   - non-nil: classify tier, estimate cost via the Gate callbacks,
//     debit on allow (converting allow→deny on exhausted/closed), and
//     stamp EnvelopeID + Tier + cost fields on the Decision row.
//
// Sequence:
//  1. Lockdown short-circuit.
//  2. Policy evaluation.
//  3. Bounds check (push-shaped only).
//  4. Monitor-mode override on policy decisions.
//  5. Envelope.Spend on allow (if envelope != nil).
//  6. Counter increment on deny — but NOT on envelope-budget denials.
//     Caps are operator-imposed, not agent misbehavior; counting them
//     would lockdown a compliant agent for hitting its budget.
//  7. Stamp envelope/tier/cost fields on Decision.
//  8. Log append.
//
// Envelope exhaustion is NOT subject to monitor-mode override — caps are
// hard contracts even when policy is in monitor.
//
// OnDecision is fired exactly once per Evaluate via a single defer-based
// callsite (closes #76). Both the lockdown short-circuit and the normal
// path now route through the same exit point, so a future short-circuit
// (rate-limit, circuit-breaker) can't silently skip the callback.
// Pass-by-copy preserves the existing contract that callbacks can't
// mutate the Decision the caller sees.
func (g *Gate) Evaluate(a Action, agent string, envelope *BudgetEnvelope) (final Decision) {
	now := time.Now().UTC().Format(time.RFC3339)

	// Single OnDecision callsite. Defer fires after the named-return
	// `final` is set; reads `final` rather than capturing a per-branch
	// variable so any branch's return value flows through.
	defer func() {
		if g.OnDecision != nil {
			dCopy := final
			g.OnDecision(&dCopy)
		}
	}()

	// Capture caller_origin once, up front — used only when envelope == nil
	// so we can identify which call sites bypass cost-governance plumbing.
	// skip=2: captureCallerOrigin's own frame + Evaluate's frame → caller.
	var callerOrigin string
	if envelope == nil {
		callerOrigin = captureCallerOrigin(2)
	}

	// 1. Lockdown takes precedence over any rule.
	if g.Counter != nil && g.Counter.IsLocked(agent) {
		d := Decision{
			Allowed: false, Mode: "enforce", RuleID: "lockdown",
			Reason:     "agent in lockdown — contact operator",
			Escalation: "lockdown", Action: a, Agent: agent, Ts: now,
			CallerOrigin: callerOrigin,
		}
		stampEnvelope(&d, envelope, g, a, agent)
		stampFingerprint(&d, g.Fingerprint, g.Policy.Authority)
		stampWorktreeDiagnostic(&d, a, g.Cwd)
		stampSession(&d, g.ChainID, g.SessionID)
		if !g.NoRecord {
			_ = WriteLog(d, g.LogDir)
		}
		final = d
		return
	}

	// 2. Policy evaluate.
	d := g.Policy.EvaluateWithFingerprint(a, g.Fingerprint)
	d.Ts = now
	d.Agent = agent
	d.CallerOrigin = callerOrigin

	// 3. Bounds — only for push-shaped when policy allows the action so far.
	// Caught in PR #79 review: overwriting d with bd dropped both Agent and
	// CallerOrigin from the bounds-deny audit row. Preserve them explicitly.
	if d.Allowed && (a.Type == ActGitPush || a.Type == ActGithubPRCreate) {
		bd := CheckBounds(a, g.Policy, g.Cwd)
		if !bd.Allowed {
			d = bd
			d.Ts = now
			d.Agent = agent
			d.CallerOrigin = callerOrigin
		}
	}

	// 4. Monitor mode override: if we're in monitor mode and the rule
	// would deny, flip to allow (log-only). Do NOT override on enforce/guide.
	if !d.Allowed && d.Mode == "monitor" {
		d.Allowed = true
	}

	// (Step 4.5 — escalate-effect resolution — removed in cull Phase 3,
	// 2026-05-08. Hermes' tools/approval.py provides the operator-
	// prompt + reply-parse natively; chitin no longer maintains its
	// own pending_approvals + Wait + bridge POST + grant table.)

	// 4.6 — Operator-authorized bypass (added 2026-05-18).
	// Replaces the manual vim/sed workflow operator has been using for
	// chitin.yaml edits. The bypass requires BOTH:
	//   (a) the deny came from one of the explicitly-bypassable rules
	//       below (NOT a blanket bypass — only governance-self-mod
	//       and the protected-branch false-positives that block
	//       legitimate feature-branch work today)
	//   (b) CHITIN_GOV_OPERATOR_AUTHORIZED=1 is set in the env at
	//       Evaluate time — operator sets this in their shell when
	//       granting authorization
	//
	// SCOPE: env-var lifetime is process-scoped (Unix env), not
	// per-action. To close the bypass window, unset the env var.
	// Sub-process inheritance is a documented risk — callers spawning
	// workers (clawta-poller, kanban-dispatch.lobster) MUST scrub
	// CHITIN_GOV_OPERATOR_AUTHORIZED from the spawned env to prevent
	// authorization from leaking into ambient sub-worker capability.
	// See spawn-time invariant + regression test in workflows.
	//
	// Logged as RuleID="operator-authorized-bypass" so the audit row
	// surfaces every fire. Bypass DOES NOT flip Spend cost accounting
	// (envelope still charged) or skip the OnDecision callback.
	if !d.Allowed && os.Getenv("CHITIN_GOV_OPERATOR_AUTHORIZED") == "1" {
		bypassable := map[string]bool{
			"no-governance-self-modification": true,
			"no-gov-self-mod-via-shell":       true,
			"no-force-push":                   true,
			"no-commit-to-protected":          true,
			"no-protected-push":               true,
		}
		if bypassable[d.RuleID] {
			origRule := d.RuleID
			origReason := d.Reason
			d.Allowed = true
			d.RuleID = "operator-authorized-bypass"
			d.Reason = fmt.Sprintf(
				"OPERATOR-AUTHORIZED BYPASS via CHITIN_GOV_OPERATOR_AUTHORIZED=1; "+
					"original deny was %q (%s). Bypass window stays open for the "+
					"lifetime of this process env; unset the env var to close it. "+
					"Sub-process inheritance is the caller's responsibility — "+
					"scrub this var before spawning workers.",
				origRule, origReason,
			)
		}
	}

	// 5. Envelope spend on allow. Compute delta via callbacks even when
	// the policy denies — so the audit row records what would have been
	// spent for telemetry. But only call Spend when allowed.
	var delta CostDelta
	var tier Tier
	if envelope != nil {
		if g.ClassifyTier != nil {
			tier = g.ClassifyTier(a)
		}
		if g.EstimateCost != nil {
			delta = g.EstimateCost(a, agent)
		} else {
			delta = CostDelta{ToolCalls: 1}
		}
		if d.Allowed {
			if err := envelope.Spend(delta); err != nil {
				// Distinguish RuleID by error class so audit-log
				// analytics can split exhausted (over cap) from closed
				// (operator-closed envelope) from not-found (caller bug
				// or data race). All three deny, but they mean different
				// things downstream.
				ruleID := "envelope-exhausted"
				switch {
				case errors.Is(err, ErrEnvelopeClosed):
					ruleID = "envelope-closed"
				case errors.Is(err, ErrEnvelopeNotFound):
					ruleID = "envelope-not-found"
				}
				reason := "envelope " + envelope.ID + ": " + err.Error()
				d = Decision{
					Allowed: false, Mode: g.Policy.Mode, RuleID: ruleID,
					Reason: reason, Action: a, Agent: agent, Ts: now,
					CallerOrigin: callerOrigin,
				}
			}
		}
	}

	// 6. Counter on deny — but skip envelope-budget denials. Operators
	// imposing caps is not the agent misbehaving; counting envelope hits
	// against the lockdown ladder would force-lock a perfectly compliant
	// agent after ~10 budget-denied calls. All envelope-* RuleIDs
	// (exhausted, closed, not-found) are budget-class and exempt.
	weight := 1
	for _, r := range g.Policy.Rules {
		if r.ID == d.RuleID && r.EscalationWeight > 0 {
			weight = r.EscalationWeight
			break
		}
	}
	// Denials exempt from escalation toward lockdown. Envelope-budget
	// denials are operator-imposed caps, not agent misbehavior. The
	// "wrong-place" denials — committing to a protected branch, a
	// side-effect from the primary checkout — are redirections: the deny
	// already prevented the harm, so escalating the agent toward lockdown
	// on top just deadlocks it during normal work (ticket t_2356307a).
	// Genuinely dangerous repeats (rm -rf, governance self-mod) are NOT
	// exempt and still escalate.
	escalationExempt := d.RuleID == "envelope-exhausted" ||
		d.RuleID == "envelope-closed" ||
		d.RuleID == "envelope-not-found" ||
		d.RuleID == "no-commit-to-protected" ||
		d.RuleID == "worktree-required"
	if !d.Allowed && !escalationExempt && g.Counter != nil {
		if !g.NoRecord {
			// Log on failure rather than silently swallow — a SQLite
			// failure here is the "agent never locks" path. We still
			// fall through and stamp Escalation from Level (which reads
			// from the DB, so the row may be stale) so the caller gets a
			// Decision; the operator sees the error on stderr and can
			// repair the DB before the next call.
			if err := g.Counter.RecordActionDenial(agent, string(a.Type), a.Fingerprint(), weight); err != nil {
				fmt.Fprintf(os.Stderr, "gov: RecordDenial failed agent=%s rule=%s: %v\n", agent, d.RuleID, err)
			}
			pruneBefore := denyCascadeSince(g.Policy.Escalation)
			if pruneBefore > 0 {
				if err := g.Counter.PruneActionDenialsBefore(pruneBefore); err != nil {
					fmt.Fprintf(os.Stderr, "gov: PruneActionDenials failed agent=%s rule=%s: %v\n", agent, d.RuleID, err)
				}
			}
			if a.Type == ActShellExec && denyCascadeFired(g.Counter, agent, g.Policy.Escalation) {
				g.Counter.Lockdown(agent)
			}
		}
		d.Escalation = g.Counter.Level(agent)
	} else if g.Counter != nil {
		d.Escalation = g.Counter.Level(agent)
	}

	// 7. Stamp envelope/tier/cost fields on the Decision row. We do this
	// for both allow and deny so audit-log analytics can join cost-
	// classified decisions regardless of outcome.
	stampEnvelopeWith(&d, envelope, tier, delta)
	// 7a. Stamp fingerprint dims (P2 routing-as-learning-system) so the
	// row carries (model, role, workflow_id, fingerprint) for joining
	// against PR/review outcomes downstream.
	stampFingerprint(&d, g.Fingerprint, g.Policy.Authority)
	// 7b. Stamp worktree posture, and — when the policy graduates it to an
	// enforced invariant (invariantModes: worktree-required: enforce) —
	// deny a side-effect action evaluated from the primary git checkout.
	// Kernel-side, so --no-verify / CHITIN_KERNEL_BIN tricks cannot reach
	// around it. Honors operator presence: an operator working
	// interactively in the primary checkout is not an autonomous agent.
	// Placed after step 6 so a worktree deny does NOT increment the
	// lockdown counter — the agent is redirected to a worktree, not
	// escalated toward lockdown.
	stampWorktreeDiagnostic(&d, a, g.Cwd)
	if d.Allowed && d.WorktreeStatus == "primary" &&
		g.Policy.InvariantModes["worktree-required"] == "enforce" &&
		!operatorPresenceBypass() {
		d.Allowed = false
		d.RuleID = "worktree-required"
		d.Mode = "enforce"
		d.Reason = "side-effect action from the primary git checkout is denied; " +
			"create a linked worktree and work on a feature branch"
		d.Suggestion = "git worktree add ../<repo>-<slug> -b <type>/<slug> origin/main, then cd into it"
	}
	// 7c. Stamp chain/session correlation so the console can group this
	// decision into its agent session even when no envelope is present.
	stampSession(&d, g.ChainID, g.SessionID)

	// 8. Log. Suppressed by NoRecord so smoke evaluations don't pollute
	// daily chain rollups (fingerprint_outcomes, swarm_health) with
	// synthetic deny rows. The Decision still flows back to the caller
	// via the named return — what changes is durability.
	if !g.NoRecord {
		_ = WriteLog(d, g.LogDir)
	}

	// 9. F4 addendum: OnDecision callback fires from the deferred
	// callsite at the top of Evaluate (single-source-of-truth so a
	// future short-circuit can't skip the callback). Order is preserved:
	// WriteLog runs before the deferred OnDecision because defers fire
	// AFTER the function returns.
	final = d
	return
}

func denyCascadeFired(counter *Counter, agent string, cfg EscalationConfig) bool {
	if counter == nil || cfg.DenyCascadeCount <= 0 || cfg.DenyCascadeWindowSeconds <= 0 {
		return false
	}
	since := denyCascadeSince(cfg)
	count, err := counter.CountActionDenialsSince(agent, string(ActShellExec), since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gov: CountActionDenialsSince failed agent=%s; forcing lockdown: %v\n", agent, err)
		return true
	}
	return count >= cfg.DenyCascadeCount
}

func denyCascadeSince(cfg EscalationConfig) int64 {
	if cfg.DenyCascadeWindowSeconds <= 0 {
		return 0
	}
	return time.Now().UTC().Add(-time.Duration(cfg.DenyCascadeWindowSeconds) * time.Second).Unix()
}

// stampEnvelope is the lockdown-path helper: it does its own classify +
// estimate because the lockdown short-circuit returns before the main
// flow's classify/estimate runs. We still want EnvelopeID/Tier on the
// audit row so post-hoc envelope-scoped queries see lockdown events.
//
// Defaults match the main-flow Evaluate path exactly so audit
// telemetry is consistent: nil EstimateCost yields {ToolCalls:1}, not
// the zero CostDelta — otherwise lockdown rows would have ToolCalls=0
// while ordinary denials have ToolCalls=1, breaking analytics joins.
func stampEnvelope(d *Decision, envelope *BudgetEnvelope, g *Gate, a Action, agent string) {
	if envelope == nil {
		return
	}
	var tier Tier
	if g.ClassifyTier != nil {
		tier = g.ClassifyTier(a)
	}
	delta := CostDelta{ToolCalls: 1}
	if g.EstimateCost != nil {
		delta = g.EstimateCost(a, agent)
	}
	stampEnvelopeWith(d, envelope, tier, delta)
}

// stampEnvelopeWith writes the envelope/tier/cost fields onto d. Single
// source of truth for the field layout — the lockdown path and the main
// flow both go through this so they can never drift.
func stampEnvelopeWith(d *Decision, envelope *BudgetEnvelope, tier Tier, delta CostDelta) {
	if envelope == nil {
		return
	}
	d.EnvelopeID = envelope.ID
	d.Tier = tier
	d.CostUSD = delta.USD
	d.InputBytes = delta.InputBytes
	d.OutputBytes = delta.OutputBytes
	d.ToolCalls = delta.ToolCalls
}

// stampFingerprint writes the typed identity dims onto d.
// Empty-string values are pass-through — Decision's omitempty JSON tags
// drop them on serialization, so pre-identity dispatches still emit the
// smaller schema with no breakage. Single call site (lockdown path +
// main flow) prevents drift if the field set grows.
func stampFingerprint(d *Decision, ctx FingerprintContext, authority AuthorityConfig) {
	agentFingerprint := firstNonEmpty(ctx.AgentFingerprint, ctx.Fingerprint)
	d.AgentInstanceID = ctx.AgentInstanceID
	d.AgentFingerprint = agentFingerprint
	d.Driver = ctx.Driver
	d.Model = ctx.Model
	d.Role = ctx.Role
	d.StationPromptHash = ctx.StationPromptHash
	d.SkillsToolsHash = ctx.SkillsToolsHash
	d.SoulLens = ctx.SoulLens
	d.ClaimedAuthority = ctx.ClaimedAuthority
	d.Authority = ResolveTrustedAuthority(ctx, authority)
	d.WorkflowID = ctx.WorkflowID
	d.Fingerprint = agentFingerprint
}

// stampSession writes the chain/session correlation ids onto d. Empty
// values are pass-through — Decision's omitempty tags drop them, so
// pre-session dispatches keep the smaller schema. Single call site
// pattern (lockdown path + main flow) prevents drift, mirroring
// stampFingerprint. The console groups decisions into sessions by
// chain_id || session_id || envelope_id; before these ids were stamped
// that grouping rode solely on envelope_id, so an envelope regression
// silently un-chained an agent from the console (Hermes, 2026-05).
func stampSession(d *Decision, chainID, sessionID string) {
	d.ChainID = chainID
	d.SessionID = sessionID
}

func ResolveTrustedAuthority(ctx FingerprintContext, authority AuthorityConfig) string {
	if ctx.Authority != "" {
		return ctx.Authority
	}
	for _, grant := range authority.Trusted {
		if grant.matches(ctx) {
			return grant.Authority
		}
	}
	if ctx.Role == "external" && ctx.ClaimedAuthority == "" && ctx.AgentInstanceID == "" &&
		ctx.AgentFingerprint == "" && ctx.Driver == "" && ctx.Model == "" &&
		ctx.StationPromptHash == "" && ctx.SkillsToolsHash == "" &&
		ctx.WorkflowID == "" && ctx.Fingerprint == "" {
		return "external"
	}
	if ctx.ClaimedAuthority != "" || ctx.AgentInstanceID != "" || ctx.AgentFingerprint != "" ||
		ctx.Driver != "" || ctx.Model != "" || ctx.Role != "" || ctx.StationPromptHash != "" ||
		ctx.SkillsToolsHash != "" || ctx.WorkflowID != "" || ctx.Fingerprint != "" {
		return "worker"
	}
	return ""
}

func (t TrustedAuthority) matches(ctx FingerprintContext) bool {
	if !t.hasStableSelector() {
		return false
	}
	if t.AgentInstanceID != "" && t.AgentInstanceID != ctx.AgentInstanceID {
		return false
	}
	if t.AgentFingerprint != "" && t.AgentFingerprint != firstNonEmpty(ctx.AgentFingerprint, ctx.Fingerprint) {
		return false
	}
	if t.Driver != "" && t.Driver != ctx.Driver {
		return false
	}
	if t.Model != "" && t.Model != ctx.Model {
		return false
	}
	if t.Role != "" && t.Role != ctx.Role {
		return false
	}
	if t.WorkflowID != "" && t.WorkflowID != ctx.WorkflowID {
		return false
	}
	return true
}

// findRuleEscalation removed in cull Phase 3 (2026-05-08).
