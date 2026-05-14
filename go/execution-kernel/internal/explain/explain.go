package explain

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

type Args struct {
	StateDir    string
	Cwd         string
	PolicyFile  string
	EventID     string
	NearMissMax int
}

type Report struct {
	EventID       string
	PolicyPath    string
	PolicySources []string
	Event         EventRef
	Decision      gov.Decision
	Rule          *RuleReport
	Bounds        BoundsReport
	Signals       *SignalReport
	NearMisses    []NearMiss
}

type EventRef struct {
	Ts               string
	RunID            string
	SessionID        string
	ChainID          string
	Seq              int64
	ThisHash         string
	ActionType       string
	ActionTarget     string
	DecisionOutcome  string
	Agent            string
	AgentInstanceID  string
	Driver           string
	OriginalToolName string
}

type RuleReport struct {
	ID      string
	Effect  string
	Mode    string
	Reason  string
	Match   gov.MatchExplanation
	Summary string
}

type BoundsReport struct {
	Status          string
	RuleID          string
	Reason          string
	MaxFilesChanged int
	MaxLinesChanged int
}

type SignalReport struct {
	RuleID           string
	Ts               string
	PredictedBlast   float64
	FlounderingScore float64
	DriftScore       float64
	RoutingDecision  string
}

type NearMiss struct {
	RuleID   string
	Effect   string
	Score    float64
	Failures []gov.MatchCheck
}

type decisionEvent struct {
	event.Event
	Payload map[string]any
}

func Build(args Args) (*Report, error) {
	if args.EventID == "" {
		return nil, fmt.Errorf("event_id is required")
	}
	if args.StateDir == "" {
		return nil, fmt.Errorf("state_dir is required")
	}
	if args.Cwd == "" {
		return nil, fmt.Errorf("cwd is required")
	}
	if args.NearMissMax <= 0 {
		args.NearMissMax = 3
	}

	policy, sources, policyPath, err := loadPolicy(args)
	if err != nil {
		return nil, err
	}

	ev, err := readDecisionEvent(args.StateDir, args.EventID)
	if err != nil {
		return nil, err
	}
	action := gov.Action{
		Type:   gov.ActionType(stringField(ev.Payload, "action_type")),
		Target: stringField(ev.Payload, "action_target"),
		Path:   args.Cwd,
	}
	decision, err := readDecision(args.StateDir, ev)
	if err != nil {
		return nil, err
	}
	ctx := fingerprintFromDecision(decision, policy)
	report := &Report{
		EventID:       args.EventID,
		PolicyPath:    policyPath,
		PolicySources: sources,
		Event: EventRef{
			Ts:               ev.Ts,
			RunID:            ev.RunID,
			SessionID:        ev.SessionID,
			ChainID:          ev.ChainID,
			Seq:              ev.Seq,
			ThisHash:         ev.ThisHash,
			ActionType:       string(action.Type),
			ActionTarget:     action.Target,
			DecisionOutcome:  stringField(ev.Payload, "decision"),
			Agent:            firstNonEmpty(ev.Labels["agent"], ev.AgentInstanceID),
			AgentInstanceID:  firstNonEmpty(ev.AgentInstanceID, ev.Labels["agent_instance_id"]),
			Driver:           firstNonEmpty(ev.Labels["driver"], decision.Driver),
			OriginalToolName: stringField(ev.Payload, "tool_name"),
		},
		Decision: decision,
		Bounds:   explainBounds(policy, action, args.Cwd),
		Signals:  readSignals(args.StateDir, decision, action),
	}

	if rule := findRule(policy, decision.RuleID); rule != nil {
		match := rule.ExplainMatch(action, ctx)
		report.Rule = &RuleReport{
			ID:      rule.ID,
			Effect:  rule.Effect,
			Mode:    effectiveMode(policy, rule.ID),
			Reason:  firstNonEmpty(rule.Reason, decision.Reason),
			Match:   match,
			Summary: buildRuleSummary(*rule, match),
		}
	}
	report.NearMisses = collectNearMisses(policy, decision.RuleID, action, ctx, args.NearMissMax)
	return report, nil
}

func loadPolicy(args Args) (gov.Policy, []string, string, error) {
	if args.PolicyFile != "" {
		policy, err := gov.LoadPolicyFile(args.PolicyFile)
		if err != nil {
			return gov.Policy{}, nil, "", fmt.Errorf("load policy: %w", err)
		}
		return policy, []string{args.PolicyFile}, args.PolicyFile, nil
	}
	policy, sources, err := gov.LoadWithInheritance(args.Cwd)
	if err != nil {
		return gov.Policy{}, nil, "", fmt.Errorf("load policy: %w", err)
	}
	policyPath := ""
	if len(sources) > 0 {
		policyPath = sources[len(sources)-1]
	}
	return policy, sources, policyPath, nil
}

func readDecisionEvent(stateDir, eventID string) (*decisionEvent, error) {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "events-") && strings.HasSuffix(name, ".jsonl") {
			files = append(files, filepath.Join(stateDir, name))
		}
	}
	sort.Strings(files)
	for _, path := range files {
		ev, err := findDecisionEventInFile(path, eventID)
		if err != nil {
			return nil, err
		}
		if ev != nil {
			return ev, nil
		}
	}
	return nil, fmt.Errorf("event %q not found", eventID)
}

func findDecisionEventInFile(path, eventID string) (*decisionEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var ev decisionEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.EventType != "decision" {
			continue
		}
		if len(ev.Payload) == 0 && len(ev.Event.Payload) > 0 {
			_ = json.Unmarshal(ev.Event.Payload, &ev.Payload)
		}
		if chooseEventID(ev) != eventID {
			continue
		}
		return &ev, nil
	}
	return nil, scanner.Err()
}

func readDecision(stateDir string, ev *decisionEvent) (gov.Decision, error) {
	key := decisionKey(
		ev.Ts,
		stringField(ev.Payload, "action_type"),
		stringField(ev.Payload, "action_target"),
		firstNonEmpty(ev.AgentInstanceID, ev.Labels["agent_instance_id"]),
		ev.Labels["driver"],
		firstNonEmpty(ev.Labels["agent"], ev.AgentInstanceID),
	)
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return gov.Decision{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "gov-decisions-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		path := filepath.Join(stateDir, name)
		found, err := findDecisionInFile(path, key)
		if err != nil {
			return gov.Decision{}, err
		}
		if found != nil {
			return *found, nil
		}
	}
	return gov.Decision{}, fmt.Errorf("decision row for event %q not found", chooseEventID(*ev))
}

func findDecisionInFile(path, key string) (*gov.Decision, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		d, err := parseDecisionLine(scanner.Bytes())
		if err != nil {
			continue
		}
		if strings.HasPrefix(d.RuleID, "router-heuristic:") {
			continue
		}
		if decisionKey(d.Ts, string(d.Action.Type), d.Action.Target, d.AgentInstanceID, d.Driver, d.Agent) == key {
			return &d, nil
		}
	}
	return nil, scanner.Err()
}

func explainBounds(policy gov.Policy, action gov.Action, cwd string) BoundsReport {
	if action.Type != gov.ActGitPush && action.Type != gov.ActGithubPRCreate {
		return BoundsReport{Status: "not_applicable", Reason: "bounds only apply to push-shaped actions"}
	}
	action.Path = cwd
	eff := policy.Bounds.EffectiveBounds(string(action.Type))
	if eff.MaxFilesChanged == 0 && eff.MaxLinesChanged == 0 {
		return BoundsReport{Status: "not_configured", Reason: "no bounds configured for this action"}
	}
	d := gov.CheckBounds(action, policy, cwd)
	status := "within_bounds"
	if !d.Allowed {
		status = "denied"
	}
	return BoundsReport{
		Status:          status,
		RuleID:          d.RuleID,
		Reason:          d.Reason,
		MaxFilesChanged: eff.MaxFilesChanged,
		MaxLinesChanged: eff.MaxLinesChanged,
	}
}

func readSignals(stateDir string, decision gov.Decision, action gov.Action) *SignalReport {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return nil
	}
	decisionTS, err := parseTimestamp(decision.Ts)
	if err != nil {
		return nil
	}
	var best *SignalReport
	bestDelta := 24 * time.Hour
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "gov-decisions-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		path := filepath.Join(stateDir, name)
		_ = scanDecisionFile(path, func(d gov.Decision) {
			if !strings.HasPrefix(d.RuleID, "router-heuristic:") {
				return
			}
			if decision.Agent != "" && d.Agent != "" && d.Agent != decision.Agent {
				return
			}
			if d.PredictedBlast == 0 && d.FlounderingScore == 0 && d.DriftScore == 0 && d.RoutingDecision == "" {
				return
			}
			if action.Target != "" && !strings.HasSuffix(d.Action.Target, action.Target) {
				return
			}
			ts, err := parseTimestamp(d.Ts)
			if err != nil {
				return
			}
			delta := ts.Sub(decisionTS)
			if delta < 0 {
				delta = -delta
			}
			if delta > 2*time.Second || delta >= bestDelta {
				return
			}
			bestDelta = delta
			best = &SignalReport{
				RuleID:           d.RuleID,
				Ts:               d.Ts,
				PredictedBlast:   d.PredictedBlast,
				FlounderingScore: d.FlounderingScore,
				DriftScore:       d.DriftScore,
				RoutingDecision:  d.RoutingDecision,
			}
		})
	}
	return best
}

func parseTimestamp(ts string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, ts)
}

func collectNearMisses(policy gov.Policy, matchedRuleID string, action gov.Action, ctx gov.FingerprintContext, max int) []NearMiss {
	type scored struct {
		NearMiss
		order int
	}
	candidates := make([]scored, 0, len(policy.Rules))
	for i, rule := range policy.Rules {
		if rule.ID == matchedRuleID {
			continue
		}
		match := rule.ExplainMatch(action, ctx)
		if match.Matched || match.Score < 0.6 || match.Failed == 0 || match.Failed > 2 {
			continue
		}
		if !firstCheckMatched(match.Checks, "action") {
			continue
		}
		failures := failedChecks(match.Checks)
		candidates = append(candidates, scored{
			NearMiss: NearMiss{
				RuleID:   rule.ID,
				Effect:   rule.Effect,
				Score:    match.Score,
				Failures: failures,
			},
			order: i,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if len(candidates[i].Failures) != len(candidates[j].Failures) {
			return len(candidates[i].Failures) < len(candidates[j].Failures)
		}
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].order < candidates[j].order
	})
	if len(candidates) > max {
		candidates = candidates[:max]
	}
	out := make([]NearMiss, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.NearMiss)
	}
	return out
}

func buildRuleSummary(rule gov.Rule, match gov.MatchExplanation) string {
	if match.Matched {
		return fmt.Sprintf("matched %s rule %q", rule.Effect, rule.ID)
	}
	return fmt.Sprintf("rule %q matched %d of %d checks", rule.ID, match.Passed, len(match.Checks))
}

func findRule(policy gov.Policy, ruleID string) *gov.Rule {
	for i := range policy.Rules {
		if policy.Rules[i].ID == ruleID {
			return &policy.Rules[i]
		}
	}
	return nil
}

func effectiveMode(policy gov.Policy, ruleID string) string {
	if mode, ok := policy.InvariantModes[ruleID]; ok {
		return mode
	}
	return policy.Mode
}

func fingerprintFromDecision(d gov.Decision, policy gov.Policy) gov.FingerprintContext {
	ctx := gov.FingerprintContext{
		AgentInstanceID:   d.AgentInstanceID,
		AgentFingerprint:  firstNonEmpty(d.AgentFingerprint, d.Fingerprint),
		Driver:            d.Driver,
		Model:             d.Model,
		Role:              d.Role,
		StationPromptHash: d.StationPromptHash,
		SkillsToolsHash:   d.SkillsToolsHash,
		SoulLens:          d.SoulLens,
		ClaimedAuthority:  d.ClaimedAuthority,
		WorkflowID:        d.WorkflowID,
		Fingerprint:       d.Fingerprint,
	}
	ctx.Authority = gov.ResolveTrustedAuthority(ctx, policy.Authority)
	if ctx.Authority == "" {
		ctx.Authority = d.Authority
	}
	return ctx
}

func failedChecks(checks []gov.MatchCheck) []gov.MatchCheck {
	out := make([]gov.MatchCheck, 0, len(checks))
	for _, check := range checks {
		if !check.Matched {
			out = append(out, check)
		}
	}
	return out
}

func firstCheckMatched(checks []gov.MatchCheck, name string) bool {
	for _, check := range checks {
		if check.Name == name {
			return check.Matched
		}
	}
	return false
}

func parseDecisionLine(line []byte) (gov.Decision, error) {
	type wire struct {
		gov.Decision
		ActionType   string `json:"action_type"`
		ActionTarget string `json:"action_target"`
	}
	var row wire
	if err := json.Unmarshal(line, &row); err != nil {
		return gov.Decision{}, err
	}
	row.Decision.Action = gov.Action{Type: gov.ActionType(row.ActionType), Target: row.ActionTarget}
	return row.Decision, nil
}

func scanDecisionFile(path string, fn func(gov.Decision)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		d, err := parseDecisionLine(scanner.Bytes())
		if err != nil {
			continue
		}
		fn(d)
	}
	return scanner.Err()
}

func chooseEventID(ev decisionEvent) string {
	for _, key := range []string{"event_id", "ulid", "tool_use_id"} {
		if v := stringField(ev.Payload, key); v != "" {
			return v
		}
	}
	return ev.ThisHash
}

func decisionKey(ts, actionType, actionTarget, agentInstanceID, driver, agent string) string {
	return strings.Join([]string{ts, actionType, actionTarget, agentInstanceID, driver, agent}, "\x1f")
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
