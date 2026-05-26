package activities

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

type MergePRInput struct {
	Repo     string `json:"repo"`
	PRNumber int    `json:"pr_number"`
}

type MergeResult struct {
	Succeeded         bool   `json:"succeeded"`
	MergeSHA          string `json:"merge_sha,omitempty"`
	HeadBranchDeleted bool   `json:"head_branch_deleted"`
	StderrTail        string `json:"stderr_tail,omitempty"`
}

type UnlabelPRInput struct {
	Repo      string `json:"repo"`
	PRNumber  int    `json:"pr_number"`
	LabelName string `json:"label_name"`
}

type UnlabelPRResult struct {
	Succeeded  bool `json:"succeeded"`
	WasPresent bool `json:"was_present"`
}

type CommentTemplateID string

const (
	CommentTemplateCIFailed      CommentTemplateID = "ci_failed"
	CommentTemplateMergeConflict CommentTemplateID = "merge_conflict"
	CommentTemplateCITimeout     CommentTemplateID = "ci_timeout"
)

type CommentPRInput struct {
	Repo              string            `json:"repo"`
	PRNumber          int               `json:"pr_number"`
	WorkflowID        string            `json:"workflow_id"`
	TemplateID        CommentTemplateID `json:"template_id"`
	FailedChecks      []string          `json:"failed_checks,omitempty"`
	ConflictFileCount int               `json:"conflict_file_count,omitempty"`
	ElapsedSeconds    int               `json:"elapsed_seconds,omitempty"`
}

type CommentPRResult struct {
	Posted  bool   `json:"posted"`
	Skipped bool   `json:"skipped"`
	Body    string `json:"body,omitempty"`
}

type MergePR struct{}
type UnlabelPR struct{}
type CommentPR struct{}

func NewMergePR() *MergePR                { return &MergePR{} }
func NewUnlabelPR() *UnlabelPR            { return &UnlabelPR{} }
func NewCommentPR() *CommentPR            { return &CommentPR{} }
func (a *MergePR) ActivityName() string   { return "MergePR" }
func (a *UnlabelPR) ActivityName() string { return "UnlabelPR" }
func (a *CommentPR) ActivityName() string { return "CommentPR" }

func (a *MergePR) Execute(ctx context.Context, in MergePRInput) (MergeResult, error) {
	if in.PRNumber <= 0 || in.Repo == "" {
		return MergeResult{StderrTail: "missing Repo or PRNumber"}, nil
	}
	cmd := exec.CommandContext(ctx, "gh", "pr", "merge", fmt.Sprintf("%d", in.PRNumber),
		"--repo", in.Repo, "--squash", "--delete-branch")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String() + "\n" + stderr.String()
	res := MergeResult{
		Succeeded:         err == nil || strings.Contains(strings.ToLower(out), "already merged"),
		MergeSHA:          parseMergeSHA(out),
		HeadBranchDeleted: err == nil && !strings.Contains(strings.ToLower(out), "delete branch failed"),
		StderrTail:        tailKiB(stderr.String()),
	}
	return res, nil
}

func (a *UnlabelPR) Execute(ctx context.Context, in UnlabelPRInput) (UnlabelPRResult, error) {
	label := in.LabelName
	if label == "" {
		label = ReadyToMergeLabel
	}
	cmd := exec.CommandContext(ctx, "gh", "pr", "edit", fmt.Sprintf("%d", in.PRNumber),
		"--repo", in.Repo, "--remove-label", label)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.ToLower(stderr.String())
		if strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist") ||
			strings.Contains(msg, "could not remove") || strings.Contains(msg, "no label") {
			return UnlabelPRResult{Succeeded: true, WasPresent: false}, nil
		}
		return UnlabelPRResult{Succeeded: false, WasPresent: true}, nil
	}
	return UnlabelPRResult{Succeeded: true, WasPresent: true}, nil
}

func (a *CommentPR) Execute(ctx context.Context, in CommentPRInput) (CommentPRResult, error) {
	body := renderAutoMergeComment(in)
	if body == "" {
		return CommentPRResult{Skipped: true}, nil
	}
	cmd := exec.CommandContext(ctx, "gh", "pr", "comment", fmt.Sprintf("%d", in.PRNumber),
		"--repo", in.Repo, "--body", body)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return CommentPRResult{Body: body}, nil
	}
	return CommentPRResult{Posted: true, Body: body}, nil
}

func renderAutoMergeComment(in CommentPRInput) string {
	footer := "\n\nAuto-merge stepped back; re-apply the `chitin/ready-to-merge` label to retry."
	switch in.TemplateID {
	case CommentTemplateCIFailed:
		checks := strings.Join(in.FailedChecks, ", ")
		if checks == "" {
			checks = "unknown required check"
		}
		return fmt.Sprintf("Auto-merge did not merge this PR because CI reported failing required check(s): %s.%s", checks, footer)
	case CommentTemplateMergeConflict:
		return fmt.Sprintf("Auto-merge did not merge this PR because GitHub reports a merge conflict. Conflict file count: %d.%s", in.ConflictFileCount, footer)
	case CommentTemplateCITimeout:
		return fmt.Sprintf("Auto-merge waited %d seconds for CI to finish, but the checks did not reach a terminal green state.%s", in.ElapsedSeconds, footer)
	default:
		return ""
	}
}

var shaRe = regexp.MustCompile(`\b[0-9a-f]{40}\b`)

func parseMergeSHA(out string) string {
	if m := shaRe.FindString(out); m != "" {
		return m
	}
	return ""
}

func tailKiB(s string) string {
	if len(s) <= 1024 {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[len(s)-1024:])
}
