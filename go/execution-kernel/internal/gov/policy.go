package gov

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Policy is the merged rule set evaluated on every gate call.
// Loaded from YAML; LoadWithInheritance merges parent chitin.yaml
// files into a single Policy before evaluation.
type Policy struct {
	ID             string            `yaml:"id"`
	Name           string            `yaml:"name,omitempty"`
	Mode           string            `yaml:"mode,omitempty"` // monitor | enforce | guide; default guide
	Pack           string            `yaml:"pack,omitempty"`
	InvariantModes map[string]string `yaml:"invariantModes,omitempty"` // ruleID → mode
	Bounds         Bounds            `yaml:"bounds,omitempty"`
	Escalation     EscalationConfig  `yaml:"escalation,omitempty"`
	Authority      AuthorityConfig   `yaml:"authority,omitempty"`
	Rules          []Rule            `yaml:"rules"`
}

// Rule is one entry in the policy. Evaluated top-to-bottom; first match wins.
type Rule struct {
	ID                string            `yaml:"id"`
	Action            ActionMatcher     `yaml:"action"`                 // single type OR list of types
	Effect            string            `yaml:"effect"`                 // allow | deny | guide | monitor
	Target            string            `yaml:"target,omitempty"`       // substring match on Action.Target
	TargetRegex       string            `yaml:"target_regex,omitempty"` // regex match on Action.Target
	Params            map[string]string `yaml:"params,omitempty"`       // exact match on Action.Params string values
	Branches          []string          `yaml:"branches,omitempty"`     // for git push/commit protected-branch checks
	PathUnder         []string          `yaml:"path_under,omitempty"`   // for file.* — match if Action.Target begins with any
	AgentInstanceID   IdentityMatcher   `yaml:"agent_instance_id,omitempty"`
	AgentFingerprint  IdentityMatcher   `yaml:"agent_fingerprint,omitempty"`
	Driver            IdentityMatcher   `yaml:"driver,omitempty"`
	Model             IdentityMatcher   `yaml:"model,omitempty"`
	Role              IdentityMatcher   `yaml:"role,omitempty"`
	StationPromptHash IdentityMatcher   `yaml:"station_prompt_hash,omitempty"`
	SkillsToolsHash   IdentityMatcher   `yaml:"skills_tools_hash,omitempty"`
	SoulLens          IdentityMatcher   `yaml:"soul_lens,omitempty"`
	Authority         IdentityMatcher   `yaml:"authority,omitempty"`
	WorkflowID        IdentityMatcher   `yaml:"workflow_id,omitempty"`
	Reason            string            `yaml:"reason,omitempty"`
	Suggestion        string            `yaml:"suggestion,omitempty"`
	CorrectedCommand  string            `yaml:"correctedCommand,omitempty"`
	EscalationWeight  int               `yaml:"escalation_weight,omitempty"` // default 1

	// compiledRegex is populated by ApplyDefaults from TargetRegex so we
	// validate patterns at load time (fail-closed on bad regex) rather than
	// silently-return-false at each eval.
	compiledRegex *regexp.Regexp `yaml:"-"`
}

// Bounds are the blast-radius ceilings checked for push-shaped actions.
//
// Top-level MaxFilesChanged/MaxLinesChanged are the DEFAULT ceilings —
// applied to any push-shaped action whose action_type doesn't have a
// per-action override in PerAction. PerAction overrides allow doc-batch
// pushes (e.g. wiki regen, multi-doc rewrites) to use a higher ceiling
// without widening it for code commits — closes #70.
//
// Example:
//
//	bounds:
//	  max_files_changed: 25      # default — code commits stay tight
//	  max_lines_changed: 500
//	  per_action:
//	    git.push:
//	      max_files_changed: 200    # doc batches via git push allowed
//	      max_lines_changed: 5000
//	    github.pr.create:
//	      max_files_changed: 100    # PR-create is reviewed, slightly looser
//	      max_lines_changed: 2000
//
// Per the spec, MaxRuntimeSeconds was removed in v1 (no reliable way
// to time a future action before it runs) and may return in v2 with
// post-action runtime tracking.
type Bounds struct {
	MaxFilesChanged int                     `yaml:"max_files_changed"`
	MaxLinesChanged int                     `yaml:"max_lines_changed"`
	PerAction       map[string]ActionBounds `yaml:"per_action,omitempty"`
}

// ActionBounds is the per-action override for blast-radius ceilings.
// Zero values mean "fall back to the top-level Bounds default".
type ActionBounds struct {
	MaxFilesChanged int `yaml:"max_files_changed"`
	MaxLinesChanged int `yaml:"max_lines_changed"`
}

// AuthorityConfig declares operator-owned identity grants. Hook/env
// metadata may claim an authority, but only these trusted grants (or an
// explicitly populated trusted context in-process) can turn that claim into
// the effective Decision.Authority value.
type AuthorityConfig struct {
	Trusted []TrustedAuthority `yaml:"trusted,omitempty"`
}

// TrustedAuthority grants an effective authority to identities matching all
// non-empty selector fields. At least one stable selector is required so
// caller-controlled context like driver/model/role cannot grant authority by
// itself.
type TrustedAuthority struct {
	Authority        string `yaml:"authority"`
	AgentInstanceID  string `yaml:"agent_instance_id,omitempty"`
	AgentFingerprint string `yaml:"agent_fingerprint,omitempty"`
	Driver           string `yaml:"driver,omitempty"`
	Model            string `yaml:"model,omitempty"`
	Role             string `yaml:"role,omitempty"`
	WorkflowID       string `yaml:"workflow_id,omitempty"`
}

func (t TrustedAuthority) hasSelector() bool {
	return t.hasStableSelector()
}

func (t TrustedAuthority) hasStableSelector() bool {
	return t.AgentInstanceID != "" ||
		t.AgentFingerprint != "" ||
		t.WorkflowID != ""
}

// effectiveBounds returns the bounds that apply to actionType — the
// PerAction override merged with the top-level defaults. Zero values
// in the override fall back to the default; non-zero values win.
func (b Bounds) effectiveBounds(actionType string) ActionBounds {
	out := ActionBounds{
		MaxFilesChanged: b.MaxFilesChanged,
		MaxLinesChanged: b.MaxLinesChanged,
	}
	if override, ok := b.PerAction[actionType]; ok {
		if override.MaxFilesChanged > 0 {
			out.MaxFilesChanged = override.MaxFilesChanged
		}
		if override.MaxLinesChanged > 0 {
			out.MaxLinesChanged = override.MaxLinesChanged
		}
	}
	return out
}

// EscalationConfig overrides the default escalation thresholds.
type EscalationConfig struct {
	ElevatedThreshold        int `yaml:"elevated_threshold"`          // default 3
	HighThreshold            int `yaml:"high_threshold"`              // default 7
	LockdownThreshold        int `yaml:"lockdown_threshold"`          // default 10
	MaxRetriesPerFp          int `yaml:"max_retries_per_action"`      // default 3
	DenyCascadeCount         int `yaml:"deny_cascade_count"`          // default 4
	DenyCascadeWindowSeconds int `yaml:"deny_cascade_window_seconds"` // default 300
}

// Decision is the result of evaluating an Action against a Policy.
//
// Cost-governance fields (EnvelopeID, Tier, CostUSD, InputBytes,
// OutputBytes, ToolCalls) are populated when Gate.Evaluate is called
// with a non-nil *BudgetEnvelope. All are `,omitempty` so audit-log
// readers built against the v1 schema tolerate the v3 extension.
type Decision struct {
	Allowed          bool   `json:"allowed"`
	Mode             string `json:"mode"` // monitor | enforce | guide
	RuleID           string `json:"rule_id"`
	Reason           string `json:"reason,omitempty"`
	Suggestion       string `json:"suggestion,omitempty"`
	CorrectedCommand string `json:"corrected_command,omitempty"`
	Escalation       string `json:"escalation,omitempty"` // normal | elevated | high | lockdown
	Action           Action `json:"-"`
	Agent            string `json:"agent,omitempty"`
	Ts               string `json:"ts"`

	EnvelopeID  string  `json:"envelope_id,omitempty"`
	Tier        Tier    `json:"tier,omitempty"`
	CostUSD     float64 `json:"cost_usd,omitempty"` // informational; cap fires on calls + bytes
	InputBytes  int64   `json:"input_bytes,omitempty"`
	OutputBytes int64   `json:"output_bytes,omitempty"`
	ToolCalls   int64   `json:"tool_calls,omitempty"`

	// CallerOrigin is stamped when Gate.Evaluate is called WITHOUT an envelope.
	// It captures `file:line` of the caller via runtime.Caller so the audit log
	// self-identifies which call sites are not envelope-wrapped. Empty when an
	// envelope was supplied (the EnvelopeID field then carries the audit anchor).
	// Surfaced for the analysis layer's `decisions_missing_envelope_id` finding.
	CallerOrigin string `json:"caller_origin,omitempty"`

	// Typed agent identity dimensions. Stamped on every decision the
	// kernel writes when the dispatching agent supplies them via the
	// CHITIN_* env vars (or equivalent hook payload fields). All are
	// optional + omitempty for backwards compatibility: pre-identity
	// dispatches write rows without these fields and existing readers
	// tolerate the omission.
	//
	// Agent remains the legacy display/source field. AgentInstanceID
	// identifies one running instance/session; AgentFingerprint is the
	// canonical config hash from libs/contracts/src/fingerprint.ts;
	// Fingerprint is retained as its legacy JSON alias for analytics
	// already reading that field.
	AgentInstanceID   string `json:"agent_instance_id,omitempty"`
	AgentFingerprint  string `json:"agent_fingerprint,omitempty"`
	Driver            string `json:"driver,omitempty"`
	Model             string `json:"model,omitempty"`
	Role              string `json:"role,omitempty"`
	StationPromptHash string `json:"station_prompt_hash,omitempty"`
	SkillsToolsHash   string `json:"skills_tools_hash,omitempty"`
	SoulLens          string `json:"soul_lens,omitempty"`
	ClaimedAuthority  string `json:"claimed_authority,omitempty"`
	Authority         string `json:"authority,omitempty"`
	WorkflowID        string `json:"workflow_id,omitempty"`
	Fingerprint       string `json:"fingerprint,omitempty"`

	// EscalationID was removed in cull Phase 3 (2026-05-08). The
	// pending_approvals table that this referenced is gone; any
	// audit-trail join via this id no longer makes sense.

	// Router-heuristic signal metadata, stamped by the router-hook
	// post-2026-05-08 cull. All four are optional + omitempty so
	// non-router decisions (operator-CLI gate-evaluate, plain
	// gov.Gate.Evaluate calls without a router) keep their existing
	// schema. Consumers that care (hermes' approval flow, operator-
	// wired chain readers) join the router-stamped row with the
	// kernel's enforcement row via (ts, action_target). See
	// docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md.
	PredictedBlast   float64 `json:"predicted_blast,omitempty"`
	FlounderingScore float64 `json:"floundering_score,omitempty"`
	DriftScore       float64 `json:"drift_score,omitempty"`
	RoutingDecision  string  `json:"routing_decision,omitempty"`

	// Worktree diagnostic metadata is stamped when a side-effect action is
	// evaluated from the primary git checkout instead of a linked worktree.
	// This is intentionally audit-only for now: it does not change Allowed,
	// Mode, escalation counters, or envelope spend. The chain needs operator
	// data before this becomes an enforceable invariant.
	WorktreeDiagnosticRuleID string `json:"worktree_diagnostic_rule_id,omitempty"`
	WorktreeStatus           string `json:"worktree_status,omitempty"` // primary
	WorktreeReason           string `json:"worktree_reason,omitempty"`

	// Effect is the rule's effect value as parsed from chitin.yaml
	// (allow|deny|guide|monitor). Internal to the gate's flow control;
	// not serialized to the chain (the chain only cares about the
	// resolved Allowed + RuleID). The escalate effect was removed in
	// cull Phase 3 (2026-05-08).
	Effect string `json:"-"`
}

// ActionMatcher is a yaml.Unmarshaler that accepts either a single
// action type string or a list of strings.
type ActionMatcher []string

func (a *ActionMatcher) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*a = []string{node.Value}
		return nil
	}
	if node.Kind == yaml.SequenceNode {
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		*a = list
		return nil
	}
	return fmt.Errorf("action must be string or list of strings, got %v", node.Kind)
}

// Matches returns true if the given ActionType appears in the matcher.
func (a ActionMatcher) Matches(t ActionType) bool {
	s := string(t)
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}

// IdentityMatcher accepts either a scalar string or a list of exact string
// values. Empty matcher means "no identity constraint" for that rule.
type IdentityMatcher []string

func (m *IdentityMatcher) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*m = []string{node.Value}
		return nil
	}
	if node.Kind == yaml.SequenceNode {
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		if len(list) == 0 {
			return fmt.Errorf("identity matcher list must not be empty")
		}
		*m = list
		return nil
	}
	return fmt.Errorf("identity matcher must be string or list of strings, got %v", node.Kind)
}

func (m IdentityMatcher) Matches(value string) bool {
	if len(m) == 0 {
		return true
	}
	for _, candidate := range m {
		if value == candidate {
			return true
		}
	}
	return false
}

// LoadPolicyFile reads and parses a single chitin.yaml. Returns an error
// on malformed YAML or any rule with a regex that fails to compile —
// fail-closed at load time rather than silently-false at eval time.
func LoadPolicyFile(path string) (Policy, error) {
	return LoadPolicyFileWithOptions(path, PolicyLoadOptions{})
}

func LoadPolicyFileWithOptions(path string, opts PolicyLoadOptions) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("read policy: %w", err)
	}
	if err := VerifyPolicySignatureBytes(path, data, opts); err != nil {
		return Policy{}, err
	}
	p, err := parsePolicyYAML(data)
	if err != nil {
		return Policy{}, fmt.Errorf("policy %s: %w", path, err)
	}
	return p, nil
}

// parsePolicyYAML is the single entry point for turning chitin.yaml bytes
// into a validated Policy. Unmarshal → ApplyDefaults → reject `effect:
// escalate` (cull Phase 3, 2026-05-08 — the escalate effect was a
// parallel implementation of hermes' built-in approval system; see
// docs/decisions/ for the cull rationale).
func parsePolicyYAML(data []byte) (Policy, error) {
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Policy{}, fmt.Errorf("parse: %w", err)
	}
	if err := p.ApplyDefaults(); err != nil {
		return Policy{}, fmt.Errorf("validate: %w", err)
	}
	for _, r := range p.Rules {
		if r.Effect == "escalate" {
			return Policy{}, fmt.Errorf("rule %s: effect: escalate is no longer supported (cull Phase 3, 2026-05-08); use deny and let hermes' approval system prompt the operator", r.ID)
		}
	}
	return p, nil
}

// ApplyDefaults fills in unset fields with their baseline values,
// validates that Mode is one of {monitor,guide,enforce}, and compiles
// every rule's TargetRegex. Returns a non-nil error on any validation
// failure (fail-closed at load time rather than silent at eval time).
func (p *Policy) ApplyDefaults() error {
	if p.Mode == "" {
		p.Mode = "guide"
	}
	switch p.Mode {
	case "monitor", "guide", "enforce":
	default:
		return fmt.Errorf("invalid mode=%q: must be one of monitor|guide|enforce", p.Mode)
	}
	if p.Escalation.ElevatedThreshold == 0 {
		p.Escalation.ElevatedThreshold = 3
	}
	if p.Escalation.HighThreshold == 0 {
		p.Escalation.HighThreshold = 7
	}
	if p.Escalation.LockdownThreshold == 0 {
		p.Escalation.LockdownThreshold = 10
	}
	if p.Escalation.MaxRetriesPerFp == 0 {
		p.Escalation.MaxRetriesPerFp = 3
	}
	if p.Escalation.DenyCascadeCount == 0 {
		p.Escalation.DenyCascadeCount = 4
	}
	if p.Escalation.DenyCascadeWindowSeconds == 0 {
		p.Escalation.DenyCascadeWindowSeconds = 300
	}
	for i, grant := range p.Authority.Trusted {
		if grant.Authority == "" {
			return fmt.Errorf("authority.trusted[%d]: authority is required", i)
		}
		switch grant.Authority {
		case "worker", "supervisor", "operator", "system":
		default:
			return fmt.Errorf("authority.trusted[%d]: invalid authority=%q: must be one of worker|supervisor|operator|system", i, grant.Authority)
		}
		if !grant.hasSelector() {
			return fmt.Errorf("authority.trusted[%d]: at least one stable identity selector is required", i)
		}
	}
	for i := range p.Rules {
		if p.Rules[i].EscalationWeight == 0 {
			p.Rules[i].EscalationWeight = 1
		}
		if p.Rules[i].TargetRegex != "" {
			re, err := regexp.Compile(p.Rules[i].TargetRegex)
			if err != nil {
				return fmt.Errorf("rule %q: invalid target_regex: %w", p.Rules[i].ID, err)
			}
			p.Rules[i].compiledRegex = re
		}
		// Reject empty entries in list-typed rule fields. An empty
		// path_under entry would match every Action.Target (every
		// string begins with ""); an empty branches or action entry
		// is a config typo from an editor leaving a stray blank list
		// item. Surface these at load time rather than letting the
		// rule silently widen its apparent surface at eval.
		for j, pu := range p.Rules[i].PathUnder {
			if pu == "" {
				return fmt.Errorf("rule %q: path_under[%d] is empty (would match every path); remove the entry or fill it in", p.Rules[i].ID, j)
			}
		}
		for j, b := range p.Rules[i].Branches {
			if b == "" {
				return fmt.Errorf("rule %q: branches[%d] is empty; remove the entry or fill it in", p.Rules[i].ID, j)
			}
		}
		for j, a := range p.Rules[i].Action {
			if a == "" {
				return fmt.Errorf("rule %q: action[%d] is empty; remove the entry or fill it in", p.Rules[i].ID, j)
			}
		}
		for k, v := range p.Rules[i].Params {
			if k == "" {
				return fmt.Errorf("rule %q: params contains an empty key; remove the entry or fill it in", p.Rules[i].ID)
			}
			if v == "" {
				return fmt.Errorf("rule %q: params[%q] is empty; remove the entry or fill it in", p.Rules[i].ID, k)
			}
		}
		if err := validateIdentityMatcher(p.Rules[i].ID, "agent_instance_id", p.Rules[i].AgentInstanceID); err != nil {
			return err
		}
		if err := validateIdentityMatcher(p.Rules[i].ID, "agent_fingerprint", p.Rules[i].AgentFingerprint); err != nil {
			return err
		}
		if err := validateIdentityMatcher(p.Rules[i].ID, "driver", p.Rules[i].Driver); err != nil {
			return err
		}
		if err := validateIdentityMatcher(p.Rules[i].ID, "model", p.Rules[i].Model); err != nil {
			return err
		}
		if err := validateIdentityMatcher(p.Rules[i].ID, "role", p.Rules[i].Role); err != nil {
			return err
		}
		if err := validateIdentityMatcher(p.Rules[i].ID, "station_prompt_hash", p.Rules[i].StationPromptHash); err != nil {
			return err
		}
		if err := validateIdentityMatcher(p.Rules[i].ID, "skills_tools_hash", p.Rules[i].SkillsToolsHash); err != nil {
			return err
		}
		if err := validateIdentityMatcher(p.Rules[i].ID, "soul_lens", p.Rules[i].SoulLens); err != nil {
			return err
		}
		if err := validateIdentityMatcher(p.Rules[i].ID, "authority", p.Rules[i].Authority); err != nil {
			return err
		}
		if err := validateIdentityMatcher(p.Rules[i].ID, "workflow_id", p.Rules[i].WorkflowID); err != nil {
			return err
		}
	}
	return nil
}

func validateIdentityMatcher(ruleID, field string, matcher IdentityMatcher) error {
	for i, v := range matcher {
		if v == "" {
			return fmt.Errorf("rule %q: %s[%d] is empty; remove the entry or fill it in", ruleID, field, i)
		}
	}
	return nil
}

// Evaluate walks the rule list in three passes so deny precedence is
// rule-order-independent: first pass checks all deny rules (first match
// wins), second pass checks all allow rules (first match wins). If no
// rule matches, fail-closed default-deny.
//
// This matters because a leading allow-* rule like default-allow-shell
// must NOT override a later deny rule like no-destructive-rm. With
// single-pass order-dependent evaluation, a permissive allow rule
// placed early silently re-permits everything below it.
//
// (The third escalate-effect pass was removed in cull Phase 3 — see
// commit log. Hermes' tools/approval.py handles operator-prompted
// approvals natively.)
func (p Policy) Evaluate(a Action) Decision {
	return p.evaluate(a, FingerprintContext{})
}

// EvaluateWithFingerprint evaluates a policy with typed identity context.
// The legacy Evaluate method remains action-only for callers that have not
// wired CHITIN_* identity yet; identity-constrained rules simply do not match
// there. Gate.Evaluate uses this path so policy can constrain autonomous
// agents by stable fingerprint dimensions without trusting claimed authority.
func (p Policy) EvaluateWithFingerprint(a Action, ctx FingerprintContext) Decision {
	ctx.Authority = ResolveTrustedAuthority(ctx, p.Authority)
	return p.evaluate(a, ctx)
}

func (p Policy) evaluate(a Action, ctx FingerprintContext) Decision {
	for _, r := range p.Rules {
		if r.Effect != "deny" || !r.matchesWithFingerprint(a, ctx) {
			continue
		}
		return p.decisionFromRule(r, false, a)
	}
	for _, r := range p.Rules {
		if r.Effect != "allow" || !r.matchesWithFingerprint(a, ctx) {
			continue
		}
		return p.decisionFromRule(r, true, a)
	}
	// Fail-closed default
	return Decision{
		Allowed: false,
		Mode:    p.Mode,
		RuleID:  "default-deny",
		Reason:  "no matching allow rule; policy default is deny",
		Action:  a,
	}
}

func (p Policy) decisionFromRule(r Rule, allowed bool, a Action) Decision {
	mode := p.Mode
	if m, ok := p.InvariantModes[r.ID]; ok {
		mode = m
	}
	return Decision{
		Allowed:          allowed,
		Mode:             mode,
		RuleID:           r.ID,
		Reason:           r.Reason,
		Suggestion:       r.Suggestion,
		CorrectedCommand: r.CorrectedCommand,
		Action:           a,
		Effect:           r.Effect,
	}
}

func (r Rule) matches(a Action) bool {
	return r.matchesWithFingerprint(a, FingerprintContext{})
}

func (r Rule) matchesWithFingerprint(a Action, ctx FingerprintContext) bool {
	if !r.Action.Matches(a.Type) {
		return false
	}
	if !r.identityMatches(ctx) {
		return false
	}
	for key, want := range r.Params {
		got, ok := a.Params[key]
		if !ok || fmt.Sprint(got) != want {
			return false
		}
	}
	// Branch condition: Action.Target must be in the list. For git.commit,
	// the target is the raw command, so policies can opt into resolving the
	// current HEAD by including the existing "<HEAD-implicit>" sentinel.
	if len(r.Branches) > 0 {
		if !r.branchMatches(a) {
			return false
		}
	}
	// PathUnder: Action.Target must begin with one of the prefixes
	if len(r.PathUnder) > 0 {
		under := false
		for _, p := range r.PathUnder {
			if len(a.Target) >= len(p) && a.Target[:len(p)] == p {
				under = true
				break
			}
		}
		if !under {
			return false
		}
	}
	// Target: case-sensitive substring match (renamed from containsFold —
	// previous impl was neither case-folding nor efficient).
	if r.Target != "" {
		if !strings.Contains(a.Target, r.Target) {
			return false
		}
	}
	// TargetRegex: compiled at load time via ApplyDefaults; a missing
	// compiledRegex with a non-empty TargetRegex would indicate LoadPolicyFile
	// was bypassed, which we treat as fail-closed (don't match).
	if r.TargetRegex != "" {
		if r.compiledRegex == nil {
			return false
		}
		if !r.compiledRegex.MatchString(a.Target) {
			return false
		}
	}
	return true
}

func (r Rule) branchMatches(a Action) bool {
	for _, b := range r.Branches {
		if a.Target == b {
			return true
		}
	}
	if a.Type != ActGitCommit || !branchesContain(r.Branches, "<HEAD-implicit>") {
		return false
	}
	branch := currentGitBranch(a.Path)
	if branch == "" {
		// Protected-branch commit rules are safety rules. If the policy opted
		// into resolving the implicit current HEAD but branch resolution is
		// indeterminate, match the rule so an earlier deny fails closed instead
		// of falling through to a broad git.commit allow.
		return true
	}
	for _, b := range r.Branches {
		if branch == b {
			return true
		}
	}
	return branch == "HEAD"
}

func branchesContain(branches []string, want string) bool {
	for _, b := range branches {
		if b == want {
			return true
		}
	}
	return false
}

func currentGitBranch(cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		return ""
	}
	if out, err := gitOutput(cwd, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		return out
	}
	out, err := gitOutput(cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

func (r Rule) identityMatches(ctx FingerprintContext) bool {
	agentFingerprint := firstNonEmpty(ctx.AgentFingerprint, ctx.Fingerprint)
	return r.AgentInstanceID.Matches(ctx.AgentInstanceID) &&
		r.AgentFingerprint.Matches(agentFingerprint) &&
		r.Driver.Matches(ctx.Driver) &&
		r.Model.Matches(ctx.Model) &&
		r.Role.Matches(ctx.Role) &&
		r.StationPromptHash.Matches(ctx.StationPromptHash) &&
		r.SkillsToolsHash.Matches(ctx.SkillsToolsHash) &&
		r.SoulLens.Matches(ctx.SoulLens) &&
		r.Authority.Matches(ctx.Authority) &&
		r.WorkflowID.Matches(ctx.WorkflowID)
}
