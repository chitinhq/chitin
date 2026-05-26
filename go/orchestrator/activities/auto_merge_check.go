package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type CIStatus string

const (
	CIStatusGreen   CIStatus = "green"
	CIStatusFailed  CIStatus = "failed"
	CIStatusPending CIStatus = "pending"
)

type CheckPRMergeabilityInput struct {
	Repo      string `json:"repo"`
	PRNumber  int    `json:"pr_number"`
	LabelName string `json:"label_name"`
}

type MergeabilityReport struct {
	CIStatus          CIStatus `json:"ci_status"`
	IsMergeable       bool     `json:"is_mergeable"`
	MergeStateStatus  string   `json:"merge_state_status"`
	IsDraft           bool     `json:"is_draft"`
	IsOpen            bool     `json:"is_open"`
	HasLabel          bool     `json:"has_label"`
	FailedChecks      []string `json:"failed_checks,omitempty"`
	BaseRef           string   `json:"base_ref,omitempty"`
	ConflictFileCount int      `json:"conflict_file_count"`
}

type rollupCheck struct {
	Name       string `json:"name"`
	Context    string `json:"context"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type CheckPRMergeability struct{}

func NewCheckPRMergeability() *CheckPRMergeability { return &CheckPRMergeability{} }

func (a *CheckPRMergeability) ActivityName() string { return "CheckPRMergeability" }

func (a *CheckPRMergeability) Execute(ctx context.Context, in CheckPRMergeabilityInput) (MergeabilityReport, error) {
	if in.PRNumber <= 0 || in.Repo == "" {
		return MergeabilityReport{CIStatus: CIStatusPending}, nil
	}
	label := in.LabelName
	if label == "" {
		label = ReadyToMergeLabel
	}
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", fmt.Sprintf("%d", in.PRNumber),
		"--repo", in.Repo,
		"--json", "statusCheckRollup,mergeStateStatus,mergeable,isDraft,state,labels,baseRefName")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return MergeabilityReport{CIStatus: CIStatusPending}, fmt.Errorf("gh pr view failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var raw struct {
		StatusCheckRollup []rollupCheck `json:"statusCheckRollup"`
		MergeStateStatus  string        `json:"mergeStateStatus"`
		Mergeable         string        `json:"mergeable"`
		IsDraft           bool          `json:"isDraft"`
		State             string        `json:"state"`
		BaseRefName       string        `json:"baseRefName"`
		Labels            []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return MergeabilityReport{CIStatus: CIStatusPending}, fmt.Errorf("parse gh pr view json: %w", err)
	}

	report := MergeabilityReport{
		CIStatus:         classifyRollup(raw.StatusCheckRollup),
		IsMergeable:      strings.EqualFold(raw.Mergeable, "MERGEABLE") || strings.EqualFold(raw.MergeStateStatus, "CLEAN"),
		MergeStateStatus: raw.MergeStateStatus,
		IsDraft:          raw.IsDraft,
		IsOpen:           strings.EqualFold(raw.State, "OPEN"),
		BaseRef:          raw.BaseRefName,
	}
	for _, l := range raw.Labels {
		if strings.EqualFold(l.Name, label) {
			report.HasLabel = true
			break
		}
	}
	for _, c := range raw.StatusCheckRollup {
		if isFailedConclusion(c.Conclusion) {
			name := c.Name
			if name == "" {
				name = c.Context
			}
			if name != "" {
				report.FailedChecks = append(report.FailedChecks, name)
			}
		}
	}
	if strings.EqualFold(raw.MergeStateStatus, "DIRTY") || strings.EqualFold(raw.Mergeable, "CONFLICTING") {
		report.ConflictFileCount = 0
	}
	return report, nil
}

func classifyRollup(checks []rollupCheck) CIStatus {
	if len(checks) == 0 {
		return CIStatusPending
	}
	for _, c := range checks {
		if isFailedConclusion(c.Conclusion) {
			return CIStatusFailed
		}
		if strings.EqualFold(c.Status, "PENDING") || c.Conclusion == "" {
			return CIStatusPending
		}
	}
	for _, c := range checks {
		if !strings.EqualFold(c.Conclusion, "SUCCESS") {
			return CIStatusPending
		}
	}
	return CIStatusGreen
}

func isFailedConclusion(c string) bool {
	switch strings.ToUpper(c) {
	case "FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED":
		return true
	default:
		return false
	}
}
