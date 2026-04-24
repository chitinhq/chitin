package gov

import "time"

// Gate orchestrates policy evaluation, bounds check, escalation counting,
// and decision logging. One instance per gate subprocess invocation.
type Gate struct {
	Policy  Policy
	Counter *Counter
	LogDir  string
	Cwd     string
}

// Evaluate is the single entry point: normalize-already-done Action →
// Decision, with side effects (counter increment on deny, log append).
//
// Sequence:
//  1. Lockdown short-circuit — if agent is locked, deny immediately.
//  2. Policy evaluation (rule matching).
//  3. Bounds check (only for push-shaped actions; skipped otherwise).
//  4. Monitor-mode override: if matched rule says deny but the effective
//     mode is monitor, flip Allowed=true (log-only, non-blocking).
//  5. Counter increment if denied.
//  6. Decision log append (deny OR allow — decisions are all audit data).
func (g *Gate) Evaluate(a Action, agent string) Decision {
	now := time.Now().UTC().Format(time.RFC3339)

	// 1. Lockdown takes precedence over any rule.
	if g.Counter != nil && g.Counter.IsLocked(agent) {
		d := Decision{
			Allowed: false, Mode: "enforce", RuleID: "lockdown",
			Reason: "agent in lockdown — contact operator",
			Escalation: "lockdown", Action: a, Agent: agent, Ts: now,
		}
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

	// 5. Counter on deny. Allow policy override of weight via rule.
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

	// 6. Log.
	_ = WriteLog(d, g.LogDir)

	return d
}
