package gov

import (
	"errors"
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
	// Fingerprint dimensions for the routing-as-learning-system (P2).
	// Stamped onto every Decision this Gate writes when populated. The
	// CLI hook layer reads these from CHITIN_MODEL / CHITIN_ROLE /
	// CHITIN_WORKFLOW_ID / CHITIN_FINGERPRINT and sets them on the Gate
	// at construction. Empty values omit the JSON field (omitempty on
	// Decision); pre-fingerprint dispatches (manual operator runs, older
	// swarm builds) keep writing the smaller schema with no breakage.
	Fingerprint FingerprintContext
}

// FingerprintContext carries the four routing dimensions the kernel
// stamps on every Decision it writes. All optional — when missing the
// Decision row omits the corresponding JSON field. See
// libs/contracts/src/fingerprint.ts for the canonical-hash side.
type FingerprintContext struct {
	Model       string
	Role        string
	WorkflowID  string
	Fingerprint string
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
		stampFingerprint(&d, g.Fingerprint)
		_ = WriteLog(d, g.LogDir)
		final = d
		return
	}

	// 2. Policy evaluate.
	d := g.Policy.Evaluate(a)
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
	envelopeDeny := d.RuleID == "envelope-exhausted" ||
		d.RuleID == "envelope-closed" ||
		d.RuleID == "envelope-not-found"
	if !d.Allowed && !envelopeDeny && g.Counter != nil {
		g.Counter.RecordDenial(agent, a.Fingerprint(), weight)
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
	stampFingerprint(&d, g.Fingerprint)

	// 8. Log.
	_ = WriteLog(d, g.LogDir)

	// 9. F4 addendum: OnDecision callback fires from the deferred
	// callsite at the top of Evaluate (single-source-of-truth so a
	// future short-circuit can't skip the callback). Order is preserved:
	// WriteLog runs before the deferred OnDecision because defers fire
	// AFTER the function returns.
	final = d
	return
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

// stampFingerprint writes the routing-as-learning-system dims onto d.
// Empty-string values are pass-through — Decision's omitempty JSON tags
// drop them on serialization, so pre-fingerprint dispatches still emit
// the smaller schema with no breakage. Single call site (lockdown path
// + main flow) prevents drift if the field set grows.
func stampFingerprint(d *Decision, ctx FingerprintContext) {
	d.Model = ctx.Model
	d.Role = ctx.Role
	d.WorkflowID = ctx.WorkflowID
	d.Fingerprint = ctx.Fingerprint
}
