package gov

import "time"

// Gate orchestrates policy evaluation, bounds check, escalation counting,
// envelope spend, and decision logging. One instance per gate subprocess
// invocation.
//
// EstimateCost and ClassifyTier are optional callbacks that decouple the
// gov package from internal/cost and internal/tier (which import gov for
// Action). Wire them in at the cmd layer; nil means "default behavior":
// tier=Unset, delta={ToolCalls:1}.
type Gate struct {
	Policy       Policy
	Counter      *Counter
	LogDir       string
	Cwd          string
	EstimateCost func(a Action, agent string) CostDelta
	ClassifyTier func(a Action) Tier
}

// Evaluate is the single entry point: normalize-already-done Action →
// Decision, with side effects (counter increment on deny, envelope
// debit on allow, log append).
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
//  6. Counter increment on deny.
//  7. Stamp envelope/tier/cost fields on Decision.
//  8. Log append.
//
// Envelope exhaustion is NOT subject to monitor-mode override — caps are
// hard contracts even when policy is in monitor.
func (g *Gate) Evaluate(a Action, agent string, envelope *BudgetEnvelope) Decision {
	now := time.Now().UTC().Format(time.RFC3339)

	// 1. Lockdown takes precedence over any rule.
	if g.Counter != nil && g.Counter.IsLocked(agent) {
		d := Decision{
			Allowed: false, Mode: "enforce", RuleID: "lockdown",
			Reason:     "agent in lockdown — contact operator",
			Escalation: "lockdown", Action: a, Agent: agent, Ts: now,
		}
		stampEnvelope(&d, envelope, g, a, agent, false)
		_ = WriteLog(d, g.LogDir)
		return d
	}

	// 2. Policy evaluate.
	d := g.Policy.Evaluate(a)
	d.Ts = now
	d.Agent = agent

	// 3. Bounds — only for push-shaped when policy allows the action so far.
	if d.Allowed && (a.Type == ActGitPush || a.Type == ActGithubPRCreate) {
		bd := CheckBounds(a, g.Policy, g.Cwd)
		if !bd.Allowed {
			d = bd
			d.Ts = now
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
				reason := "envelope " + envelope.ID + ": " + err.Error()
				d = Decision{
					Allowed: false, Mode: g.Policy.Mode, RuleID: "envelope-exhausted",
					Reason: reason, Action: a, Agent: agent, Ts: now,
				}
			}
		}
	}

	// 6. Counter on deny. Allow policy override of weight via rule.
	weight := 1
	for _, r := range g.Policy.Rules {
		if r.ID == d.RuleID && r.EscalationWeight > 0 {
			weight = r.EscalationWeight
			break
		}
	}
	if !d.Allowed && g.Counter != nil {
		g.Counter.RecordDenial(agent, a.Fingerprint(), weight)
		d.Escalation = g.Counter.Level(agent)
	} else if g.Counter != nil {
		d.Escalation = g.Counter.Level(agent)
	}

	// 7. Stamp envelope/tier/cost fields. We do this for both allow and
	// deny so audit-log analytics can join cost-classified decisions
	// regardless of outcome.
	if envelope != nil {
		d.EnvelopeID = envelope.ID
		d.Tier = tier
		d.CostUSD = delta.USD
		d.InputBytes = delta.InputBytes
		d.OutputBytes = delta.OutputBytes
		d.ToolCalls = delta.ToolCalls
	}

	// 8. Log.
	_ = WriteLog(d, g.LogDir)

	return d
}

// stampEnvelope is the lockdown-path helper: lockdown returns early
// before the normal stamp step, but we still want EnvelopeID/Tier on the
// audit row so post-hoc envelope-scoped queries see the lockdown event.
func stampEnvelope(d *Decision, envelope *BudgetEnvelope, g *Gate, a Action, agent string, _ bool) {
	if envelope == nil {
		return
	}
	d.EnvelopeID = envelope.ID
	if g.ClassifyTier != nil {
		d.Tier = g.ClassifyTier(a)
	}
	if g.EstimateCost != nil {
		delta := g.EstimateCost(a, agent)
		d.CostUSD = delta.USD
		d.InputBytes = delta.InputBytes
		d.OutputBytes = delta.OutputBytes
		d.ToolCalls = delta.ToolCalls
	}
}
