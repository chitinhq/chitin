package replay

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/sidecar"
)

// Timeline is the dashboard-ready replay shape for one session chain.
type Timeline struct {
	SessionID string          `json:"session_id"`
	StartedAt string          `json:"started_at,omitempty"`
	EndedAt   string          `json:"ended_at,omitempty"`
	Filters   TimelineFilters `json:"filters,omitempty"`
	Summary   TimelineSummary `json:"summary"`
	Steps     []Step          `json:"steps"`
}

// TimelineFilters records the filters applied while building a timeline.
type TimelineFilters struct {
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Driver string `json:"driver,omitempty"`
	Tool   string `json:"tool,omitempty"`
}

// TimelineSummary aggregates the filtered steps.
type TimelineSummary struct {
	StepCount         int                 `json:"step_count"`
	ToolCallCount     int                 `json:"tool_call_count"`
	DispatchCount     int                 `json:"dispatch_count"`
	AllowedCount      int                 `json:"allowed_count"`
	DeniedCount       int                 `json:"denied_count"`
	SuccessRate       float64             `json:"success_rate"`
	TotalCostUSD      float64             `json:"total_cost_usd"`
	TotalInputTokens  int64               `json:"total_input_tokens"`
	TotalOutputTokens int64               `json:"total_output_tokens"`
	TotalTokens       int64               `json:"total_tokens"`
	DecisionsPerRule  map[string]int      `json:"decisions_per_rule"`
	TimeOnToolMs      map[string]int64    `json:"time_on_tool_ms"`
	CostByDriver      map[string]float64  `json:"cost_by_driver"`
	CostByTool        map[string]float64  `json:"cost_by_tool"`
	CostTimeline      []CostTimelinePoint `json:"cost_timeline"`
	CostHeatmap       []CostHeatmapCell   `json:"cost_heatmap"`
}

type CostTimelinePoint struct {
	Ts             string             `json:"ts"`
	StepIndex      int                `json:"step_index"`
	CostUSD        float64            `json:"cost_usd"`
	CumulativeUSD  float64            `json:"cumulative_usd"`
	Driver         string             `json:"driver,omitempty"`
	DriverCostsUSD map[string]float64 `json:"driver_costs_usd,omitempty"`
}

type CostHeatmapCell struct {
	Driver    string  `json:"driver"`
	Model     string  `json:"model"`
	CostUSD   float64 `json:"cost_usd"`
	Steps     int     `json:"steps"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
}

// Step is one ordered event in the replay timeline.
type Step struct {
	EventID    string          `json:"event_id,omitempty"`
	Type       string          `json:"type"`
	Ts         string          `json:"ts"`
	Driver     string          `json:"driver,omitempty"`
	Agent      string          `json:"agent,omitempty"`
	Model      string          `json:"model,omitempty"`
	Tool       string          `json:"tool,omitempty"`
	TokensIn   int64           `json:"tokens_in,omitempty"`
	TokensOut  int64           `json:"tokens_out,omitempty"`
	CostUSD    float64         `json:"cost_usd,omitempty"`
	Input      any             `json:"input,omitempty"`
	Output     any             `json:"output,omitempty"`
	Decision   *StepDecision   `json:"decision,omitempty"`
	Cost       *StepCost       `json:"cost,omitempty"`
	Prediction *StepPrediction `json:"prediction,omitempty"`
	DurationMs *int64          `json:"duration_ms,omitempty"`
	EnvelopeID string          `json:"envelope_id,omitempty"`
}

type StepDecision struct {
	Allowed          bool   `json:"allowed"`
	Mode             string `json:"mode,omitempty"`
	RuleID           string `json:"rule_id,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Suggestion       string `json:"suggestion,omitempty"`
	CorrectedCommand string `json:"corrected_command,omitempty"`
	Escalation       string `json:"escalation,omitempty"`
}

type StepCost struct {
	USD            float64 `json:"usd,omitempty"`
	InputTokens    int64   `json:"input_tokens,omitempty"`
	OutputTokens   int64   `json:"output_tokens,omitempty"`
	ThinkingTokens int64   `json:"thinking_tokens,omitempty"`
	TotalTokens    int64   `json:"total_tokens,omitempty"`
	InputBytes     int64   `json:"input_bytes,omitempty"`
	OutputBytes    int64   `json:"output_bytes,omitempty"`
}

type StepPrediction struct {
	PredictedBlast   float64 `json:"predicted_blast,omitempty"`
	FlounderingScore float64 `json:"floundering_score,omitempty"`
	DriftScore       float64 `json:"drift_score,omitempty"`
	RoutingDecision  string  `json:"routing_decision,omitempty"`
}

// ReplayOptions configures session timeline replay.
type ReplayOptions struct {
	SessionID string
	From      string
	To        string
	Driver    string
	Tool      string
}

type sessionEvent struct {
	event.Event
	payload map[string]any
}

type sidecarStore struct {
	store *sidecar.Store
}

type sidecarBundle struct {
	Prompt        any
	Thinking      any
	ToolInput     any
	ToolOutput    any
	ModelResponse any
}

type decisionJoin struct {
	Allowed          bool
	HasAllowed       bool
	Mode             string
	RuleID           string
	Reason           string
	Suggestion       string
	CorrectedCommand string
	Escalation       string
	CostUSD          float64
	InputBytes       int64
	OutputBytes      int64
	PredictedBlast   float64
	FlounderingScore float64
	DriftScore       float64
	RoutingDecision  string
	Driver           string
	Agent            string
	Model            string
	EnvelopeID       string
}

// BuildTimeline returns a structured session replay suitable for rendering.
func BuildTimeline(opts ReplayOptions) (*Timeline, error) {
	if opts.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	from, err := parseOptionalTS(opts.From)
	if err != nil {
		return nil, fmt.Errorf("parse --from: %w", err)
	}
	to, err := parseOptionalTS(opts.To)
	if err != nil {
		return nil, fmt.Errorf("parse --to: %w", err)
	}
	stateDir, err := chitinStateDir()
	if err != nil {
		return nil, err
	}
	events, err := readSessionEvents(stateDir, opts.SessionID)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("no chain events for session %s", opts.SessionID)
	}
	joins, err := readDecisionJoins(stateDir, events)
	if err != nil {
		return nil, err
	}
	sidecars, _ := openSidecarStore(filepath.Join(stateDir, "sidecar.db"))
	if sidecars != nil {
		defer sidecars.Close()
	}

	tl := &Timeline{
		SessionID: opts.SessionID,
		Filters: TimelineFilters{
			From:   opts.From,
			To:     opts.To,
			Driver: opts.Driver,
			Tool:   opts.Tool,
		},
		Summary: TimelineSummary{
			DecisionsPerRule: make(map[string]int),
			TimeOnToolMs:     make(map[string]int64),
			CostByDriver:     make(map[string]float64),
			CostByTool:       make(map[string]float64),
		},
	}

	for _, ev := range events {
		step, ok := buildStep(ev, joins[decisionKeyForEvent(ev)], sidecars)
		if !ok {
			continue
		}
		if !stepMatches(step, from, to, opts.Driver, opts.Tool) {
			continue
		}
		if tl.StartedAt == "" {
			tl.StartedAt = step.Ts
		}
		tl.EndedAt = step.Ts
		tl.Steps = append(tl.Steps, step)
	}
	propagateStepModels(tl.Steps)
	reconcileEnvelopeSpend(stateDir, tl.Steps)
	for i := range tl.Steps {
		accumulateSummary(&tl.Summary, tl.Steps[i])
	}
	tl.Summary.StepCount = len(tl.Steps)
	finalizeSummary(&tl.Summary, tl.Steps)
	if len(tl.Steps) == 0 {
		tl.StartedAt = ""
		tl.EndedAt = ""
	}
	return tl, nil
}

// RecentSession summarizes one chain for `chain sessions --recent`.
type RecentSession struct {
	SessionID string `json:"session_id"`
	LastTs    string `json:"last_ts"`
	FirstTs   string `json:"first_ts,omitempty"`
	Driver    string `json:"driver,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Events    int    `json:"events"`
}

// ListRecentSessions returns the most-recent session chains in the state dir.
func ListRecentSessions(limit int) ([]RecentSession, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("recent must be > 0")
	}
	stateDir, err := chitinStateDir()
	if err != nil {
		return nil, err
	}
	pattern := filepath.Join(stateDir, "events-*.jsonl")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	type agg struct {
		RecentSession
		lastParsed time.Time
	}
	bySession := map[string]*agg{}
	for _, path := range paths {
		if err := scanJSONLLines(path, func(line []byte) error {
			var ev event.Event
			if err := json.Unmarshal(line, &ev); err != nil {
				return nil
			}
			if ev.ChainID == "" || ev.ChainType != "session" {
				return nil
			}
			ts, err := parseTimestamp(ev.Ts)
			if err != nil {
				return nil
			}
			item := bySession[ev.ChainID]
			if item == nil {
				item = &agg{
					RecentSession: RecentSession{
						SessionID: ev.ChainID,
						LastTs:    ev.Ts,
						FirstTs:   ev.Ts,
						Driver:    valueOrEmpty(ev.Labels["driver"]),
						Agent:     deriveAgent(ev, nil),
					},
					lastParsed: ts,
				}
				bySession[ev.ChainID] = item
			}
			item.Events++
			if item.FirstTs == "" || ts.Before(mustParseOrZero(item.FirstTs)) {
				item.FirstTs = ev.Ts
			}
			if item.LastTs == "" || ts.After(item.lastParsed) {
				item.LastTs = ev.Ts
				item.lastParsed = ts
			}
			if item.Driver == "" {
				item.Driver = valueOrEmpty(ev.Labels["driver"])
			}
			if item.Agent == "" {
				item.Agent = deriveAgent(ev, nil)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	out := make([]RecentSession, 0, len(bySession))
	for _, item := range bySession {
		out = append(out, item.RecentSession)
	}
	sort.Slice(out, func(i, j int) bool {
		ti := mustParseOrZero(out[i].LastTs)
		tj := mustParseOrZero(out[j].LastTs)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return out[i].SessionID < out[j].SessionID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// WriteTimelineJSON writes a pretty JSON timeline.
func WriteTimelineJSON(w io.Writer, tl *Timeline) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(tl)
}

// WriteTimelineText renders an ASCII timeline for terminal inspection.
func WriteTimelineText(w io.Writer, tl *Timeline) {
	fmt.Fprintf(w, "session %s\n", tl.SessionID)
	fmt.Fprintf(w, "steps=%d tool_calls=%d cost=$%.4f tokens=%d\n",
		tl.Summary.StepCount, tl.Summary.ToolCallCount, tl.Summary.TotalCostUSD, tl.Summary.TotalTokens)
	if tl.StartedAt != "" || tl.EndedAt != "" {
		fmt.Fprintf(w, "window %s -> %s\n", emptyDash(tl.StartedAt), emptyDash(tl.EndedAt))
	}
	for _, step := range tl.Steps {
		label := step.Type
		if step.Tool != "" {
			label += " " + step.Tool
		}
		fmt.Fprintf(w, "| %s | %s", step.Ts, label)
		if step.Decision != nil {
			verdict := "deny"
			if step.Decision.Allowed {
				verdict = "allow"
			}
			fmt.Fprintf(w, " [%s", verdict)
			if step.Decision.RuleID != "" {
				fmt.Fprintf(w, " %s", step.Decision.RuleID)
			}
			fmt.Fprint(w, "]")
		}
		if step.DurationMs != nil {
			fmt.Fprintf(w, " %dms", *step.DurationMs)
		}
		if step.Cost != nil {
			if step.Cost.USD > 0 {
				fmt.Fprintf(w, " $%.4f", step.Cost.USD)
			}
			if step.Cost.TotalTokens > 0 {
				fmt.Fprintf(w, " %dtok", step.Cost.TotalTokens)
			}
		}
		if step.Prediction != nil && hasPrediction(*step.Prediction) {
			fmt.Fprintf(w, " blast=%.2f flounder=%.2f drift=%.2f",
				step.Prediction.PredictedBlast, step.Prediction.FlounderingScore, step.Prediction.DriftScore)
		}
		if step.Agent != "" || step.Driver != "" {
			fmt.Fprintf(w, " (%s/%s)", emptyDash(step.Driver), emptyDash(step.Agent))
		}
		fmt.Fprintln(w)
	}
}

func buildStep(ev sessionEvent, joined *decisionJoin, sidecars *sidecarStore) (Step, bool) {
	step := Step{
		EventID: chooseEventID(ev),
		Type:    ev.EventType,
		Ts:      ev.Ts,
		Driver:  deriveDriver(ev, joined),
		Agent:   deriveAgent(ev.Event, joined),
		Model:   deriveModel(ev.payload, joined),
		Tool:    deriveTool(ev.payload, joined),
	}
	if dur, ok := int64Field(ev.payload, "duration_ms"); ok {
		step.DurationMs = &dur
	}
	if joined != nil {
		step.Decision = &StepDecision{
			Allowed:          joined.Allowed,
			Mode:             joined.Mode,
			RuleID:           joined.RuleID,
			Reason:           joined.Reason,
			Suggestion:       joined.Suggestion,
			CorrectedCommand: joined.CorrectedCommand,
			Escalation:       joined.Escalation,
		}
		step.Cost = mergeCost(step.Cost, &StepCost{
			USD:         joined.CostUSD,
			InputBytes:  joined.InputBytes,
			OutputBytes: joined.OutputBytes,
		})
		step.EnvelopeID = joined.EnvelopeID
		if hasJoinedPrediction(joined) {
			step.Prediction = &StepPrediction{
				PredictedBlast:   joined.PredictedBlast,
				FlounderingScore: joined.FlounderingScore,
				DriftScore:       joined.DriftScore,
				RoutingDecision:  joined.RoutingDecision,
			}
		}
	} else if ev.EventType == "decision" {
		allowed := false
		switch valueOrEmpty(stringField(ev.payload, "decision")) {
		case "allow":
			allowed = true
		case "guide":
			allowed = false
		}
		step.Decision = &StepDecision{
			Allowed: allowed,
			RuleID:  stringField(ev.payload, "rule_id"),
			Reason:  stringField(ev.payload, "reason"),
		}
	}
	step = attachPayloadContent(step, ev, sidecars)
	normalizeStepCost(&step)
	if step.Type == "" || step.Ts == "" {
		return Step{}, false
	}
	return step, true
}

func attachPayloadContent(step Step, ev sessionEvent, sidecars *sidecarStore) Step {
	if sidecars != nil && step.EventID != "" {
		blob, err := sidecars.Get(step.EventID)
		if err == nil {
			if blob.ToolInput != nil {
				step.Input = blob.ToolInput
			} else if blob.Prompt != nil {
				step.Input = blob.Prompt
			}
			if blob.ToolOutput != nil {
				step.Output = blob.ToolOutput
			} else if blob.ModelResponse != nil {
				step.Output = blob.ModelResponse
			} else if blob.Thinking != nil && step.Output == nil {
				step.Output = map[string]any{"thinking": blob.Thinking}
			}
		}
	}
	switch ev.EventType {
	case "assistant_turn":
		if step.Output == nil {
			out := map[string]any{}
			if text := stringField(ev.payload, "text"); text != "" {
				out["text"] = text
			}
			if thinking := stringField(ev.payload, "thinking"); thinking != "" {
				out["thinking"] = thinking
			}
			if len(out) > 0 {
				step.Output = out
			}
		}
		if usage, ok := mapField(ev.payload, "usage"); ok {
			cost := &StepCost{
				InputTokens:    int64FromAny(usage["input_tokens"]),
				OutputTokens:   int64FromAny(usage["output_tokens"]),
				ThinkingTokens: int64FromAny(usage["thinking_tokens"]),
			}
			cost.TotalTokens = cost.InputTokens + cost.OutputTokens + cost.ThinkingTokens
			step.Cost = mergeCost(step.Cost, cost)
		}
	case "model_turn":
		step.Model = firstNonEmpty(step.Model, stringField(ev.payload, "model_name"))
		step.Cost = mergeCost(step.Cost, &StepCost{
			InputTokens:  int64FromAny(ev.payload["input_tokens"]),
			OutputTokens: int64FromAny(ev.payload["output_tokens"]),
			TotalTokens:  int64FromAny(ev.payload["input_tokens"]) + int64FromAny(ev.payload["output_tokens"]),
		})
	case "pre_tool_use", "post_tool_use", "decision":
		if step.Input == nil {
			if toolInput, ok := mapField(ev.payload, "tool_input"); ok {
				step.Input = toolInput
			}
		}
		if step.Output == nil && ev.EventType == "post_tool_use" {
			if output, ok := mapField(ev.payload, "tool_output"); ok {
				step.Output = output
			}
		}
	}
	return step
}

func normalizeStepCost(step *Step) {
	if step == nil || step.Cost == nil {
		return
	}
	if step.Cost.TotalTokens == 0 {
		step.Cost.TotalTokens = step.Cost.InputTokens + step.Cost.OutputTokens + step.Cost.ThinkingTokens
	}
	step.TokensIn = step.Cost.InputTokens
	step.TokensOut = step.Cost.OutputTokens
	step.CostUSD = step.Cost.USD
}

func propagateStepModels(steps []Step) {
	lastByDriver := map[string]string{}
	lastGlobal := ""
	for idx := range steps {
		if steps[idx].Model != "" {
			lastGlobal = steps[idx].Model
			if steps[idx].Driver != "" {
				lastByDriver[steps[idx].Driver] = steps[idx].Model
			}
			continue
		}
		if steps[idx].Driver != "" && lastByDriver[steps[idx].Driver] != "" {
			steps[idx].Model = lastByDriver[steps[idx].Driver]
			continue
		}
		if lastGlobal != "" {
			steps[idx].Model = lastGlobal
		}
	}
}

func mergeCost(existing *StepCost, extra *StepCost) *StepCost {
	if extra == nil {
		return existing
	}
	if existing == nil {
		copy := *extra
		return &copy
	}
	if existing.USD == 0 {
		existing.USD = extra.USD
	}
	if existing.InputTokens == 0 {
		existing.InputTokens = extra.InputTokens
	}
	if existing.OutputTokens == 0 {
		existing.OutputTokens = extra.OutputTokens
	}
	if existing.ThinkingTokens == 0 {
		existing.ThinkingTokens = extra.ThinkingTokens
	}
	if existing.TotalTokens == 0 {
		existing.TotalTokens = extra.TotalTokens
	}
	if existing.InputBytes == 0 {
		existing.InputBytes = extra.InputBytes
	}
	if existing.OutputBytes == 0 {
		existing.OutputBytes = extra.OutputBytes
	}
	return existing
}

func accumulateSummary(sum *TimelineSummary, step Step) {
	if isToolCallStep(step) {
		sum.ToolCallCount++
		sum.DispatchCount++
	}
	if step.Decision != nil && step.Decision.RuleID != "" {
		sum.DecisionsPerRule[step.Decision.RuleID]++
		if step.Decision.Allowed {
			sum.AllowedCount++
		} else {
			sum.DeniedCount++
		}
	}
	if step.Cost != nil {
		sum.TotalCostUSD += step.Cost.USD
		sum.TotalInputTokens += step.Cost.InputTokens
		sum.TotalOutputTokens += step.Cost.OutputTokens
		sum.TotalTokens += step.Cost.TotalTokens
		if step.Driver != "" {
			sum.CostByDriver[step.Driver] += step.Cost.USD
		}
		if step.Tool != "" {
			sum.CostByTool[step.Tool] += step.Cost.USD
		}
	}
	if step.DurationMs != nil && step.Tool != "" {
		sum.TimeOnToolMs[step.Tool] += *step.DurationMs
	}
}

func finalizeSummary(sum *TimelineSummary, steps []Step) {
	if sum.DispatchCount > 0 {
		sum.SuccessRate = float64(sum.AllowedCount) / float64(sum.DispatchCount)
	}
	driverRunning := map[string]float64{}
	heat := map[string]*CostHeatmapCell{}
	var cumulative float64
	for idx, step := range steps {
		if step.Cost != nil {
			cumulative += step.Cost.USD
			if step.Driver != "" {
				driverRunning[step.Driver] += step.Cost.USD
			}
			if step.Cost.USD > 0 && (step.Driver != "" || step.Model != "") {
				key := step.Driver + "\x1f" + emptyDash(step.Model)
				cell := heat[key]
				if cell == nil {
					cell = &CostHeatmapCell{Driver: step.Driver, Model: emptyDash(step.Model)}
					heat[key] = cell
				}
				cell.CostUSD += step.Cost.USD
				cell.Steps++
				cell.TokensIn += step.TokensIn
				cell.TokensOut += step.TokensOut
			}
		}
		pointDrivers := make(map[string]float64, len(driverRunning))
		for driver, cost := range driverRunning {
			pointDrivers[driver] = cost
		}
		sum.CostTimeline = append(sum.CostTimeline, CostTimelinePoint{
			Ts:             step.Ts,
			StepIndex:      idx,
			CostUSD:        step.CostUSD,
			CumulativeUSD:  cumulative,
			Driver:         step.Driver,
			DriverCostsUSD: pointDrivers,
		})
	}
	for _, cell := range heat {
		sum.CostHeatmap = append(sum.CostHeatmap, *cell)
	}
	sort.Slice(sum.CostHeatmap, func(i, j int) bool {
		if sum.CostHeatmap[i].Driver != sum.CostHeatmap[j].Driver {
			return sum.CostHeatmap[i].Driver < sum.CostHeatmap[j].Driver
		}
		return sum.CostHeatmap[i].Model < sum.CostHeatmap[j].Model
	})
}

func isToolCallStep(step Step) bool {
	return step.Type == "decision"
}

func stepMatches(step Step, from, to time.Time, driver, tool string) bool {
	ts, err := parseTimestamp(step.Ts)
	if err != nil {
		return false
	}
	if !from.IsZero() && ts.Before(from) {
		return false
	}
	if !to.IsZero() && ts.After(to) {
		return false
	}
	if driver != "" && step.Driver != driver {
		return false
	}
	if tool != "" && step.Tool != tool {
		return false
	}
	return true
}

func readSessionEvents(stateDir, sessionID string) ([]sessionEvent, error) {
	pattern := filepath.Join(stateDir, "events-*.jsonl")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	var out []sessionEvent
	for _, path := range paths {
		if err := scanJSONLLines(path, func(line []byte) error {
			var ev event.Event
			if err := json.Unmarshal(line, &ev); err != nil {
				return nil
			}
			if ev.ChainID != sessionID && ev.SessionID != sessionID {
				return nil
			}
			payload := map[string]any{}
			if len(ev.Payload) > 0 {
				if err := json.Unmarshal(ev.Payload, &payload); err != nil {
					payload = map[string]any{}
				}
			}
			out = append(out, sessionEvent{Event: ev, payload: payload})
			return nil
		}); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ti := mustParseOrZero(out[i].Ts)
		tj := mustParseOrZero(out[j].Ts)
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		if out[i].Seq != out[j].Seq {
			return out[i].Seq < out[j].Seq
		}
		return out[i].ThisHash < out[j].ThisHash
	})
	return out, nil
}

func readDecisionJoins(stateDir string, events []sessionEvent) (map[string]*decisionJoin, error) {
	wanted := map[string]struct{}{}
	for _, ev := range events {
		if ev.EventType != "decision" {
			continue
		}
		wanted[decisionKeyForEvent(ev)] = struct{}{}
	}
	joins := make(map[string]*decisionJoin, len(wanted))
	if len(wanted) == 0 {
		return joins, nil
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return joins, nil
		}
		return nil, err
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
		if err := scanJSONLLines(path, func(line []byte) error {
			d, err := parseDecisionLine(line)
			if err != nil {
				return nil
			}
			key := decisionKey(d.Ts, string(d.Action.Type), d.Action.Target, d.AgentInstanceID, d.Driver, d.Agent)
			if _, ok := wanted[key]; !ok {
				return nil
			}
			join := joins[key]
			if join == nil {
				join = &decisionJoin{}
				joins[key] = join
			}
			mergeDecisionJoin(join, d)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return joins, nil
}

func mergeDecisionJoin(dst *decisionJoin, d gov.Decision) {
	if !dst.HasAllowed {
		dst.Allowed = d.Allowed
		dst.HasAllowed = true
	}
	if dst.Mode == "" {
		dst.Mode = d.Mode
	}
	if dst.RuleID == "" {
		dst.RuleID = d.RuleID
	}
	if dst.Reason == "" {
		dst.Reason = d.Reason
	}
	if dst.Suggestion == "" {
		dst.Suggestion = d.Suggestion
	}
	if dst.CorrectedCommand == "" {
		dst.CorrectedCommand = d.CorrectedCommand
	}
	if dst.Escalation == "" {
		dst.Escalation = d.Escalation
	}
	if dst.CostUSD == 0 {
		dst.CostUSD = d.CostUSD
	}
	if dst.InputBytes == 0 {
		dst.InputBytes = d.InputBytes
	}
	if dst.OutputBytes == 0 {
		dst.OutputBytes = d.OutputBytes
	}
	if dst.PredictedBlast == 0 {
		dst.PredictedBlast = d.PredictedBlast
	}
	if dst.FlounderingScore == 0 {
		dst.FlounderingScore = d.FlounderingScore
	}
	if dst.DriftScore == 0 {
		dst.DriftScore = d.DriftScore
	}
	if dst.RoutingDecision == "" {
		dst.RoutingDecision = d.RoutingDecision
	}
	if dst.Driver == "" {
		dst.Driver = d.Driver
	}
	if dst.Agent == "" {
		dst.Agent = firstNonEmpty(d.AgentInstanceID, d.Agent)
	}
	if dst.Model == "" {
		dst.Model = d.Model
	}
	if dst.EnvelopeID == "" {
		dst.EnvelopeID = d.EnvelopeID
	}
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

func decisionKeyForEvent(ev sessionEvent) string {
	return decisionKey(
		ev.Ts,
		stringField(ev.payload, "action_type"),
		stringField(ev.payload, "action_target"),
		firstNonEmpty(ev.AgentInstanceID, ev.Labels["agent_instance_id"]),
		ev.Labels["driver"],
		firstNonEmpty(ev.Labels["agent"], ev.AgentInstanceID),
	)
}

func decisionKey(ts, actionType, actionTarget, agentInstanceID, driver, agent string) string {
	return strings.Join([]string{ts, actionType, actionTarget, agentInstanceID, driver, agent}, "\x1f")
}

func openSidecarStore(path string) (*sidecarStore, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	store, err := sidecar.Open(path)
	if err != nil {
		return nil, err
	}
	return &sidecarStore{store: store}, nil
}

func (s *sidecarStore) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func (s *sidecarStore) Get(eventID string) (sidecarBundle, error) {
	if s == nil || s.store == nil || eventID == "" {
		return sidecarBundle{}, nil
	}
	resolved, err := s.store.ResolveEventID(eventID)
	if err != nil {
		return sidecarBundle{}, err
	}
	blobs, err := s.store.GetAll(resolved)
	if err != nil {
		return sidecarBundle{}, err
	}
	var out sidecarBundle
	for blobType, blob := range blobs {
		val := decodeSidecarBlob(blob)
		switch blobType {
		case "prompt":
			out.Prompt = val
		case "thinking":
			out.Thinking = val
		case "tool_input":
			out.ToolInput = val
		case "tool_output":
			out.ToolOutput = val
		case "model_response":
			out.ModelResponse = val
		}
	}
	return out, nil
}

func decodeSidecarBlob(blob []byte) any {
	return sidecar.DecodeBlob(blob)
}

func chooseEventID(ev sessionEvent) string {
	for _, key := range []string{"event_id", "ulid", "tool_use_id"} {
		if v := stringField(ev.payload, key); v != "" {
			return v
		}
	}
	if ev.ThisHash != "" {
		return ev.ThisHash
	}
	return ""
}

func deriveDriver(ev sessionEvent, joined *decisionJoin) string {
	if joined != nil && joined.Driver != "" {
		return joined.Driver
	}
	if v := ev.Labels["driver"]; v != "" {
		return v
	}
	if v := stringField(ev.payload, "driver"); v != "" {
		return v
	}
	return ""
}

func deriveAgent(ev event.Event, joined *decisionJoin) string {
	if joined != nil && joined.Agent != "" {
		return joined.Agent
	}
	return firstNonEmpty(ev.AgentInstanceID, ev.Labels["agent_instance_id"], ev.Labels["agent"])
}

func deriveTool(payload map[string]any, joined *decisionJoin) string {
	if v := stringField(payload, "tool_name"); v != "" {
		return v
	}
	if joined != nil && joined.RuleID != "" {
		if v := stringField(payload, "action_type"); v != "" {
			return v
		}
	}
	if v := stringField(payload, "action_type"); v != "" {
		return v
	}
	return ""
}

func deriveModel(payload map[string]any, joined *decisionJoin) string {
	if modelUsed, ok := mapField(payload, "model_used"); ok {
		if v := stringField(modelUsed, "name"); v != "" {
			return v
		}
	}
	if v := stringField(payload, "model_name"); v != "" {
		return v
	}
	if usage, ok := mapField(payload, "usage"); ok {
		if v := stringField(usage, "model"); v != "" {
			return v
		}
	}
	if v := stringField(payload, "model"); v != "" {
		return v
	}
	if joined != nil {
		return joined.Model
	}
	return ""
}

func hasJoinedPrediction(joined *decisionJoin) bool {
	return joined != nil && (joined.PredictedBlast != 0 || joined.FlounderingScore != 0 || joined.DriftScore != 0 || joined.RoutingDecision != "")
}

func hasPrediction(pred StepPrediction) bool {
	return pred.PredictedBlast != 0 || pred.FlounderingScore != 0 || pred.DriftScore != 0 || pred.RoutingDecision != ""
}

func parseOptionalTS(ts string) (time.Time, error) {
	if ts == "" {
		return time.Time{}, nil
	}
	return parseTimestamp(ts)
}

func parseTimestamp(ts string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, ts)
}

func mustParseOrZero(ts string) time.Time {
	parsed, _ := parseTimestamp(ts)
	return parsed
}

func scanJSONLLines(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytesTrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func chitinStateDir() (string, error) {
	if v := os.Getenv("CHITIN_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".chitin"), nil
}

func reconcileEnvelopeSpend(stateDir string, steps []Step) {
	if len(steps) == 0 {
		return
	}
	dbPath := filepath.Join(stateDir, "gov.db")
	if _, err := os.Stat(dbPath); err != nil {
		return
	}
	store, err := gov.OpenBudgetStore(dbPath)
	if err != nil {
		return
	}
	defer store.Close()

	type group struct {
		known   float64
		missing []int
	}
	groups := map[string]*group{}
	for idx := range steps {
		envID := steps[idx].EnvelopeID
		if envID == "" {
			continue
		}
		g := groups[envID]
		if g == nil {
			g = &group{}
			groups[envID] = g
		}
		if steps[idx].CostUSD > 0 {
			g.known += steps[idx].CostUSD
		} else {
			g.missing = append(g.missing, idx)
		}
	}
	for envID, g := range groups {
		if len(g.missing) == 0 {
			continue
		}
		env, err := store.Load(envID)
		if err != nil {
			continue
		}
		state, err := env.Inspect()
		if err != nil {
			continue
		}
		remainder := state.SpentUSD - g.known
		if remainder <= 0 {
			continue
		}
		var totalWeight float64
		weights := make([]float64, len(g.missing))
		for i, idx := range g.missing {
			weight := float64(maxInt64(steps[idx].Cost.InputBytes, steps[idx].TokensIn))
			if weight <= 0 {
				weight = 1
			}
			weights[i] = weight
			totalWeight += weight
		}
		if totalWeight <= 0 {
			continue
		}
		remaining := remainder
		for i, idx := range g.missing {
			share := remainder * (weights[i] / totalWeight)
			if i == len(g.missing)-1 {
				share = remaining
			}
			if steps[idx].Cost == nil {
				steps[idx].Cost = &StepCost{}
			}
			steps[idx].Cost.USD = share
			steps[idx].CostUSD = share
			remaining -= share
		}
	}
}

func FindSessionForTicket(ticketID string) (string, error) {
	if ticketID == "" {
		return "", fmt.Errorf("ticket_id is required")
	}
	stateDir, err := chitinStateDir()
	if err != nil {
		return "", err
	}
	pattern := filepath.Join(stateDir, "events-*.jsonl")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	type match struct {
		sessionID string
		ts        time.Time
	}
	var best match
	for _, path := range paths {
		if err := scanJSONLLines(path, func(line []byte) error {
			var ev event.Event
			if err := json.Unmarshal(line, &ev); err != nil {
				return nil
			}
			if ev.EventType != "session_start" {
				return nil
			}
			var payload map[string]any
			if len(ev.Payload) > 0 {
				if err := json.Unmarshal(ev.Payload, &payload); err != nil {
					return nil
				}
			}
			cwd := stringField(payload, "cwd")
			if cwd == "" || !strings.Contains(cwd, ticketID) {
				return nil
			}
			ts := mustParseOrZero(ev.Ts)
			if best.sessionID == "" || ts.After(best.ts) {
				best = match{sessionID: firstNonEmpty(ev.ChainID, ev.SessionID), ts: ts}
			}
			return nil
		}); err != nil {
			return "", err
		}
	}
	if best.sessionID == "" {
		return "", fmt.Errorf("no session found for ticket %s", ticketID)
	}
	return best.sessionID, nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func mapField(m map[string]any, key string) (map[string]any, bool) {
	raw, ok := m[key]
	if !ok {
		return nil, false
	}
	val, ok := raw.(map[string]any)
	return val, ok
}

func stringField(m map[string]any, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := raw.(string)
	return s
}

func int64Field(m map[string]any, key string) (int64, bool) {
	raw, ok := m[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	return 0, false
}

func int64FromAny(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func valueOrEmpty(v string) string {
	return v
}

func emptyDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}
