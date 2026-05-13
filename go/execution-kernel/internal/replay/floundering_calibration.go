package replay

import (
	"encoding/json"
	"math"
	"os"
	"strings"
)

const flounderingFixedLoopCount = 2

type flounderingDecision struct {
	ToolName     string
	ActionType   string
	ActionTarget string
	Decision     string
}

type flounderingConfusion struct {
	tp int
	fp int
	fn int
	tn int
}

func computeFlounderingCalibration(paths []string) *FlounderingCalibration {
	fixed := flounderingConfusion{}
	adaptive := flounderingConfusion{}
	sessions := 0
	for _, p := range paths {
		decisions := readFlounderingDecisions(p)
		if len(decisions) == 0 {
			continue
		}
		sessions++
		actual := terminalLoopProxy(decisions)
		fixedPred := fixedLoopPrediction(decisions, flounderingFixedLoopCount)
		adaptivePred := adaptiveLoopPrediction(decisions, flounderingFixedLoopCount)
		fixed.add(fixedPred, actual)
		adaptive.add(adaptivePred, actual)
	}
	out := &FlounderingCalibration{
		Sessions:                  sessions,
		FixedPrecision:            precision(fixed),
		FixedRecall:               recall(fixed),
		FixedFalsePositiveRate:    falsePositiveRate(fixed),
		FixedFalseNegativeRate:    falseNegativeRate(fixed),
		AdaptivePrecision:         precision(adaptive),
		AdaptiveRecall:            recall(adaptive),
		AdaptiveFalsePositiveRate: falsePositiveRate(adaptive),
		AdaptiveFalseNegativeRate: falseNegativeRate(adaptive),
		LoopMisfireIncrease:       falseNegativeRate(adaptive) - falseNegativeRate(fixed),
	}
	if out.FixedFalsePositiveRate > 0 {
		out.FalsePositiveReduction = (out.FixedFalsePositiveRate - out.AdaptiveFalsePositiveRate) / out.FixedFalsePositiveRate
	}
	return out
}

func readFlounderingDecisions(path string) []flounderingDecision {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []flounderingDecision
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if etype, _ := ev["event_type"].(string); etype != "decision" {
			continue
		}
		payload, _ := ev["payload"].(map[string]interface{})
		if payload == nil {
			continue
		}
		toolName, _ := payload["tool_name"].(string)
		actionTarget, _ := payload["action_target"].(string)
		if toolName == "" || actionTarget == "" {
			continue
		}
		actionType, _ := payload["action_type"].(string)
		decision, _ := payload["decision"].(string)
		out = append(out, flounderingDecision{
			ToolName:     toolName,
			ActionType:   actionType,
			ActionTarget: actionTarget,
			Decision:     decision,
		})
	}
	return out
}

func terminalLoopProxy(decisions []flounderingDecision) bool {
	for i := 1; i < len(decisions); i++ {
		if !sameFlounderingSignature(decisions[i-1], decisions[i]) {
			continue
		}
		if isLowRiskFileLoopForStats(decisions[i].ActionType) || decisions[i].ActionType == "delegate.task" {
			continue
		}
		if !hasFutureWriteProgress(decisions, i) {
			return true
		}
	}
	return false
}

func fixedLoopPrediction(decisions []flounderingDecision, threshold int) bool {
	if threshold <= 0 {
		threshold = 3
	}
	runLen := 0
	last := flounderingDecision{}
	for i, d := range decisions {
		if i > 0 && sameFlounderingSignature(last, d) {
			runLen++
		} else {
			runLen = 1
		}
		if runLen >= threshold {
			return true
		}
		last = d
	}
	return false
}

func adaptiveLoopPrediction(decisions []flounderingDecision, configured int) bool {
	if configured <= 0 {
		configured = 3
	}
	runLen := 0
	last := flounderingDecision{}
	var completedRuns []int
	for i, d := range decisions {
		if i > 0 && sameFlounderingSignature(last, d) {
			runLen++
		} else {
			if runLen > 1 {
				completedRuns = appendRecentRun(completedRuns, runLen, 8)
			}
			runLen = 1
		}
		if runLen >= adaptiveLoopThresholdForStats(configured, d.ActionType, completedRuns) {
			return true
		}
		last = d
	}
	return false
}

func adaptiveLoopThresholdForStats(configured int, actionType string, completedRuns []int) int {
	floor := configured
	if isLowRiskFileLoopForStats(actionType) {
		floor = maxReplayInt(floor, configured+2)
	}
	if len(completedRuns) < 3 {
		return floor
	}
	sum := 0
	for _, n := range completedRuns {
		sum += n
	}
	adaptive := int(math.Ceil(float64(sum)/float64(len(completedRuns)))) + 1
	if adaptive > configured+2 {
		adaptive = configured + 2
	}
	return maxReplayInt(floor, adaptive)
}

func isLowRiskFileLoopForStats(actionType string) bool {
	return actionType == "file.read" || actionType == "file.write"
}

func appendRecentRun(runs []int, n int, window int) []int {
	runs = append(runs, n)
	if len(runs) > window {
		return runs[1:]
	}
	return runs
}

func sameFlounderingSignature(a, b flounderingDecision) bool {
	return a.ToolName == b.ToolName && a.ActionTarget == b.ActionTarget
}

func hasFutureWriteProgress(decisions []flounderingDecision, idx int) bool {
	for _, d := range decisions[idx+1:] {
		if d.Decision != "allow" {
			continue
		}
		if d.ActionType == "file.write" || d.ActionType == "git.commit" || d.ActionType == "git.push" {
			return true
		}
	}
	return false
}

func (c *flounderingConfusion) add(predicted, actual bool) {
	switch {
	case predicted && actual:
		c.tp++
	case predicted && !actual:
		c.fp++
	case !predicted && actual:
		c.fn++
	default:
		c.tn++
	}
}

func precision(c flounderingConfusion) float64 {
	if c.tp+c.fp == 0 {
		return 0
	}
	return float64(c.tp) / float64(c.tp+c.fp)
}

func recall(c flounderingConfusion) float64 {
	if c.tp+c.fn == 0 {
		return 0
	}
	return float64(c.tp) / float64(c.tp+c.fn)
}

func falsePositiveRate(c flounderingConfusion) float64 {
	if c.fp+c.tn == 0 {
		return 0
	}
	return float64(c.fp) / float64(c.fp+c.tn)
}

func falseNegativeRate(c flounderingConfusion) float64 {
	if c.fn+c.tp == 0 {
		return 0
	}
	return float64(c.fn) / float64(c.fn+c.tp)
}

func maxReplayInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
