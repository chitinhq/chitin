package activities

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	SpecIssueLabel = "chitin/spec"

	SpecIssueOpenedEvent         = "spec_issue_opened"
	SpecIssueCommentedEvent      = "spec_issue_commented"
	SpecIssueCommentSkippedEvent = "spec_issue_comment_skipped"
	SpecIssueClosedEvent         = "spec_issue_closed"
	SpecIssueUpdateFailedEvent   = "spec_issue_update_failed"

	SpecIssueTemplateDispatchTriggered = "dispatch_triggered"
	SpecIssueTemplateImplPROpened      = "impl_pr_opened"
	SpecIssueTemplateImplPRMerged      = "impl_pr_merged"
	SpecIssueTemplateDispatchFailed    = "dispatch_failed"
)

type EnsureSpecIssueInput struct {
	Repo       string `json:"repo"`
	SpecRef    string `json:"spec_ref"`
	SpecTitle  string `json:"spec_title"`
	SpecPRURL  string `json:"spec_pr_url"`
	SpecMDURL  string `json:"spec_md_url"`
	TasksMDURL string `json:"tasks_md_url"`
}

type EnsureSpecIssueResult struct {
	IssueNumber int    `json:"issue_number"`
	WasNew      bool   `json:"was_new"`
	Explanation string `json:"explanation,omitempty"`
}

type CommentSpecIssueInput struct {
	Repo       string            `json:"repo"`
	SpecRef    string            `json:"spec_ref"`
	TemplateID string            `json:"template_id"`
	Params     map[string]string `json:"params"`
}

type CommentSpecIssueResult struct {
	IssueNumber int    `json:"issue_number"`
	Commented   bool   `json:"commented"`
	Skipped     bool   `json:"skipped"`
	Explanation string `json:"explanation,omitempty"`
}

type UpdateSpecIssueBodyInput struct {
	Repo    string            `json:"repo"`
	SpecRef string            `json:"spec_ref"`
	Patches map[string]string `json:"patches"`
}

type UpdateSpecIssueBodyResult struct {
	IssueNumber int    `json:"issue_number"`
	Updated     bool   `json:"updated"`
	Explanation string `json:"explanation,omitempty"`
}

type CloseSpecIssueInput struct {
	Repo               string            `json:"repo"`
	SpecRef            string            `json:"spec_ref"`
	FinalCommentParams map[string]string `json:"final_comment_params"`
}

type CloseSpecIssueResult struct {
	IssueNumber int    `json:"issue_number"`
	Closed      bool   `json:"closed"`
	Explanation string `json:"explanation,omitempty"`
}

type EnsureSpecIssue struct{}
type CommentSpecIssue struct{}
type UpdateSpecIssueBody struct{}
type CloseSpecIssue struct{}

func NewEnsureSpecIssue() *EnsureSpecIssue          { return &EnsureSpecIssue{} }
func NewCommentSpecIssue() *CommentSpecIssue        { return &CommentSpecIssue{} }
func NewUpdateSpecIssueBody() *UpdateSpecIssueBody  { return &UpdateSpecIssueBody{} }
func NewCloseSpecIssue() *CloseSpecIssue            { return &CloseSpecIssue{} }
func (a *EnsureSpecIssue) ActivityName() string     { return "EnsureSpecIssue" }
func (a *CommentSpecIssue) ActivityName() string    { return "CommentSpecIssue" }
func (a *UpdateSpecIssueBody) ActivityName() string { return "UpdateSpecIssueBody" }
func (a *CloseSpecIssue) ActivityName() string      { return "CloseSpecIssue" }

func (a *EnsureSpecIssue) Execute(ctx context.Context, in EnsureSpecIssueInput) (EnsureSpecIssueResult, error) {
	var res EnsureSpecIssueResult
	if specIssueDisabled(ctx, in.Repo, in.SpecRef, 0) {
		res.Explanation = "disabled by CHITIN_SPEC_ISSUE_DISABLED"
		return res, nil
	}
	issue, found, ok := resolveSpecIssue(ctx, in.Repo, in.SpecRef)
	if !ok {
		return res, nil
	}
	if found {
		res.IssueNumber = issue.Number
		res.WasNew = false
		specIssueEmitFn(ctx, SpecIssueOpenedEvent, in.Repo, in.SpecRef, map[string]any{
			"spec_ref": in.SpecRef, "issue_number": issue.Number, "repo": in.Repo, "was_new": false,
		})
		return res, nil
	}

	title := fmt.Sprintf("[%s] %s", in.SpecRef, strings.TrimSpace(in.SpecTitle))
	if strings.TrimSpace(in.SpecTitle) == "" {
		title = fmt.Sprintf("[%s] Spec lifecycle", in.SpecRef)
	}
	body := renderSpecIssueBody(in, "")
	out, err := specIssueGH(ctx, "issue", "create", "--repo", in.Repo, "--label", SpecIssueLabel, "--title", title, "--body", body)
	if err != nil {
		specIssueEmitFailure(ctx, in.Repo, in.SpecRef, 0, "issue_create", err)
		return res, nil
	}
	n := parseIssueNumber(strings.TrimSpace(out))
	res.IssueNumber = n
	res.WasNew = true
	specIssueEmitFn(ctx, SpecIssueOpenedEvent, in.Repo, in.SpecRef, map[string]any{
		"spec_ref": in.SpecRef, "issue_number": n, "repo": in.Repo, "was_new": true,
	})
	return res, nil
}

func (a *CommentSpecIssue) Execute(ctx context.Context, in CommentSpecIssueInput) (CommentSpecIssueResult, error) {
	var res CommentSpecIssueResult
	if specIssueDisabled(ctx, in.Repo, in.SpecRef, 0) {
		res.Explanation = "disabled by CHITIN_SPEC_ISSUE_DISABLED"
		return res, nil
	}
	issue, found, ok := resolveSpecIssue(ctx, in.Repo, in.SpecRef)
	if !ok {
		return res, nil
	}
	if !found {
		specIssueEmitFailure(ctx, in.Repo, in.SpecRef, 0, "issue_resolve", fmt.Errorf("spec issue not found"))
		return res, nil
	}
	res.IssueNumber = issue.Number
	if priorAt, found := specIssuePriorCommentFn(in.Repo, in.SpecRef, in.TemplateID); found {
		res.Skipped = true
		specIssueEmitFn(ctx, SpecIssueCommentSkippedEvent, in.Repo, in.SpecRef, map[string]any{
			"spec_ref": in.SpecRef, "issue_number": issue.Number, "repo": in.Repo, "template_id": in.TemplateID, "prior_at": priorAt,
		})
		return res, nil
	}
	body, err := renderSpecIssueComment(in.TemplateID, in.Params)
	if err != nil {
		// Template render is a non-GitHub failure mode (unknown template_id,
		// missing params). Emit a failure event so the silent skip is
		// recoverable from the chain — otherwise operators only see an
		// unaccounted-for gap in the spec-issue comment trail.
		res.Explanation = err.Error()
		specIssueEmitFn(ctx, SpecIssueUpdateFailedEvent, in.Repo, in.SpecRef, map[string]any{
			"spec_ref": in.SpecRef, "issue_number": issue.Number, "repo": in.Repo,
			"op": "template_render", "template_id": in.TemplateID, "stderr_tail": err.Error(),
		})
		return res, nil
	}
	if _, err := specIssueGH(ctx, "issue", "comment", strconv.Itoa(issue.Number), "--repo", in.Repo, "--body", body); err != nil {
		specIssueEmitFailure(ctx, in.Repo, in.SpecRef, issue.Number, "issue_comment", err)
		return res, nil
	}
	res.Commented = true
	specIssueEmitFn(ctx, SpecIssueCommentedEvent, in.Repo, in.SpecRef, map[string]any{
		"spec_ref": in.SpecRef, "issue_number": issue.Number, "repo": in.Repo, "template_id": in.TemplateID, "params": in.Params,
	})
	return res, nil
}

func (a *UpdateSpecIssueBody) Execute(ctx context.Context, in UpdateSpecIssueBodyInput) (UpdateSpecIssueBodyResult, error) {
	var res UpdateSpecIssueBodyResult
	if specIssueDisabled(ctx, in.Repo, in.SpecRef, 0) {
		res.Explanation = "disabled by CHITIN_SPEC_ISSUE_DISABLED"
		return res, nil
	}
	issue, found, ok := resolveSpecIssue(ctx, in.Repo, in.SpecRef)
	if !ok {
		return res, nil
	}
	if !found {
		specIssueEmitFailure(ctx, in.Repo, in.SpecRef, 0, "issue_resolve", fmt.Errorf("spec issue not found"))
		return res, nil
	}
	res.IssueNumber = issue.Number
	out, err := specIssueGH(ctx, "issue", "view", strconv.Itoa(issue.Number), "--repo", in.Repo, "--json", "body")
	if err != nil {
		specIssueEmitFailure(ctx, in.Repo, in.SpecRef, issue.Number, "issue_view", err)
		return res, nil
	}
	var v struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		specIssueEmitFailure(ctx, in.Repo, in.SpecRef, issue.Number, "issue_view_decode", err)
		return res, nil
	}
	patched := patchSpecIssueBody(v.Body, in.Patches)
	if patched == v.Body {
		return res, nil
	}
	if _, err := specIssueGH(ctx, "issue", "edit", strconv.Itoa(issue.Number), "--repo", in.Repo, "--body", patched); err != nil {
		specIssueEmitFailure(ctx, in.Repo, in.SpecRef, issue.Number, "issue_edit", err)
		return res, nil
	}
	res.Updated = true
	return res, nil
}

func (a *CloseSpecIssue) Execute(ctx context.Context, in CloseSpecIssueInput) (CloseSpecIssueResult, error) {
	var res CloseSpecIssueResult
	if specIssueDisabled(ctx, in.Repo, in.SpecRef, 0) {
		res.Explanation = "disabled by CHITIN_SPEC_ISSUE_DISABLED"
		return res, nil
	}
	commentRes, _ := NewCommentSpecIssue().Execute(ctx, CommentSpecIssueInput{
		Repo: in.Repo, SpecRef: in.SpecRef, TemplateID: SpecIssueTemplateImplPRMerged, Params: in.FinalCommentParams,
	})
	res.IssueNumber = commentRes.IssueNumber
	if res.IssueNumber == 0 {
		issue, found, ok := resolveSpecIssue(ctx, in.Repo, in.SpecRef)
		if !ok || !found {
			return res, nil
		}
		res.IssueNumber = issue.Number
	}
	if _, err := specIssueGH(ctx, "issue", "close", strconv.Itoa(res.IssueNumber), "--repo", in.Repo); err != nil {
		specIssueEmitFailure(ctx, in.Repo, in.SpecRef, res.IssueNumber, "issue_close", err)
		return res, nil
	}
	res.Closed = true
	specIssueEmitFn(ctx, SpecIssueClosedEvent, in.Repo, in.SpecRef, map[string]any{
		"spec_ref": in.SpecRef, "issue_number": res.IssueNumber, "repo": in.Repo,
	})
	return res, nil
}

type specIssueSummary struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

func resolveSpecIssue(ctx context.Context, repo, specRef string) (specIssueSummary, bool, bool) {
	out, err := specIssueGH(ctx, "issue", "list", "--repo", repo, "--label", SpecIssueLabel, "--search", specRef, "--state", "all", "--json", "number,title,state")
	if err != nil {
		specIssueEmitFailure(ctx, repo, specRef, 0, "issue_list", err)
		return specIssueSummary{}, false, false
	}
	var issues []specIssueSummary
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		specIssueEmitFailure(ctx, repo, specRef, 0, "issue_list_decode", err)
		return specIssueSummary{}, false, false
	}
	for _, issue := range issues {
		if strings.Contains(issue.Title, "["+specRef+"]") {
			return issue, true, true
		}
	}
	return specIssueSummary{}, false, true
}

func renderSpecIssueBody(in EnsureSpecIssueInput, implPR string) string {
	if implPR == "" {
		implPR = "pending"
	}
	return fmt.Sprintf(`# Spec %s Lifecycle

Spec PR: %s
spec.md: %s
tasks.md: %s

Dispatch status: pending

<!-- chitin:impl_pr -->%s<!-- /chitin:impl_pr -->
`, in.SpecRef, in.SpecPRURL, in.SpecMDURL, in.TasksMDURL, implPR)
}

func renderSpecIssueComment(templateID string, p map[string]string) (string, error) {
	switch templateID {
	case SpecIssueTemplateDispatchTriggered:
		return fmt.Sprintf("### Dispatch triggered\nrun_id: %s\ndriver: %s\ncapability: %s\nat: %s", p["run_id"], p["driver"], p["capability"], p["at"]), nil
	case SpecIssueTemplateImplPROpened:
		return fmt.Sprintf("### Impl PR opened\nPR: %s\nbranch: %s\nopened_at: %s", p["pr_url"], p["branch"], p["opened_at"]), nil
	case SpecIssueTemplateImplPRMerged:
		return fmt.Sprintf("### Impl PR merged ✓\nPR: %s\nmerge_sha: %s\nelapsed: %s", p["pr_url"], p["merge_sha"], p["elapsed"]), nil
	case SpecIssueTemplateDispatchFailed:
		return fmt.Sprintf("### Dispatch failed\nreason: %s\nat: %s\nrun_id: %s", p["reason"], p["at"], p["run_id"]), nil
	default:
		return "", fmt.Errorf("unknown spec issue template_id %q", templateID)
	}
}

func patchSpecIssueBody(body string, patches map[string]string) string {
	out := body
	for name, value := range patches {
		re := regexp.MustCompile(`(?s)<!-- chitin:` + regexp.QuoteMeta(name) + ` -->.*?<!-- /chitin:` + regexp.QuoteMeta(name) + ` -->`)
		next := re.ReplaceAllString(out, "<!-- chitin:"+name+" -->"+value+"<!-- /chitin:"+name+" -->")
		out = next
	}
	return out
}

var specIssueGHFn = defaultSpecIssueGH
var specIssueEmitFn = emitSpecIssueChainEvent
var specIssuePriorCommentFn = priorSpecIssueCommentFromChain

// specIssueScope returns a per-(repo, specRef) key used in chain run_id /
// session_id so events from different repos sharing the same $CHITIN_DIR
// don't collide. Empty repo (older callers / break-glass paths) falls back
// to bare specRef so existing chain history remains discoverable.
func specIssueScope(repo, specRef string) string {
	if repo == "" {
		return specRef
	}
	return repo + "/" + specRef
}

func specIssueGH(ctx context.Context, args ...string) (string, error) {
	return specIssueGHFn(ctx, args...)
}

func defaultSpecIssueGH(ctx context.Context, args ...string) (string, error) {
	bin := os.Getenv("CHITIN_GH_BIN")
	if bin == "" {
		// Tests can intercept the gh-shaped calls with the same single-binary
		// pattern used elsewhere. In production CHITIN_KERNEL_BIN usually points
		// at the real kernel, so do not accidentally run `chitin-kernel issue`.
		if kernelBin := os.Getenv("CHITIN_KERNEL_BIN"); kernelBin != "" && !strings.Contains(filepath.Base(kernelBin), "chitin-kernel") {
			bin = kernelBin
		}
	}
	if bin == "" {
		bin = "gh"
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if tail == "" {
			tail = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("%w: %s", err, tail)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func parseIssueNumber(out string) int {
	if n, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
		return n
	}
	re := regexp.MustCompile(`/issues/(\d+)`)
	if m := re.FindStringSubmatch(out); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

func specIssueDisabled(ctx context.Context, repo, specRef string, issueNumber int) bool {
	if os.Getenv("CHITIN_SPEC_ISSUE_DISABLED") != "1" {
		return false
	}
	specIssueEmitFn(ctx, SpecIssueUpdateFailedEvent, repo, specRef, map[string]any{
		"spec_ref": specRef, "issue_number": issueNumber, "repo": repo, "op": "disabled_by_env", "stderr_tail": "",
	})
	return true
}

func specIssueEmitFailure(ctx context.Context, repo, specRef string, issueNumber int, op string, err error) {
	tail := ""
	if err != nil {
		tail = err.Error()
	}
	if len(tail) > 500 {
		tail = tail[len(tail)-500:]
	}
	specIssueEmitFn(ctx, SpecIssueUpdateFailedEvent, repo, specRef, map[string]any{
		"spec_ref": specRef, "issue_number": issueNumber, "repo": repo, "op": op, "stderr_tail": tail,
	})
}

func emitSpecIssueChainEvent(ctx context.Context, eventType, repo, specRef string, payload map[string]any) {
	if os.Getenv("CHITIN_DISABLE_CHAIN_EMIT") == "1" || os.Getenv("CHITIN_SPEC_ISSUE_DISABLED") == "1" && eventType != SpecIssueUpdateFailedEvent {
		return
	}
	binPath := os.Getenv("CHITIN_KERNEL_BIN")
	if binPath == "" {
		binPath = "chitin-kernel"
	}
	// Ensure repo is always queryable on the event itself — the chain
	// stores raw payloads, so a missing repo here would not be
	// recoverable downstream.
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["repo"]; !ok && repo != "" {
		payload["repo"] = repo
	}
	scope := specIssueScope(repo, specRef)
	envelope := map[string]any{
		"schema_version":    "2",
		"event_type":        eventType,
		"run_id":            "spec-issue-" + scope,
		"session_id":        "chitin-orchestrator-spec-issue-" + scope,
		"surface":           "chitin-orchestrator",
		"agent_instance_id": "chitin-orchestrator",
		"chain_type":        "spec-issue",
		"ts":                time.Now().UTC().Format(time.RFC3339Nano),
		"payload":           payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp("", "chitin-spec-issue-emit-*.json")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	chitinDir := os.Getenv("CHITIN_DIR")
	if chitinDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			chitinDir = filepath.Join(home, ".chitin")
		} else {
			chitinDir = ".chitin"
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = exec.CommandContext(cctx, binPath, "emit", "-dir", chitinDir, "-event-file", tmpPath).Run()
}

func priorSpecIssueCommentFromChain(repo, specRef, templateID string) (string, bool) {
	chitinDir := os.Getenv("CHITIN_DIR")
	if chitinDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			chitinDir = filepath.Join(home, ".chitin")
		} else {
			return "", false
		}
	}
	paths, _ := filepath.Glob(filepath.Join(chitinDir, "events-*.jsonl"))
	// Stream each file line-by-line and return on first match — chain
	// files grow without bound, so reading whole files for every dedup
	// check would scale linearly with chain age on every webhook event.
	for _, path := range paths {
		if ts, ok := scanPriorSpecIssueComment(path, repo, specRef, templateID); ok {
			return ts, true
		}
	}
	return "", false
}

func scanPriorSpecIssueComment(path, repo, specRef, templateID string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	// Allow long event lines (default 64 KiB ceiling can truncate large
	// payloads — chain envelopes routinely exceed that).
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var ev struct {
			EventType string         `json:"event_type"`
			TS        string         `json:"ts"`
			Payload   map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.EventType != SpecIssueCommentedEvent {
			continue
		}
		if ev.Payload["spec_ref"] != specRef || ev.Payload["template_id"] != templateID {
			continue
		}
		// Match repo when supplied so multi-repo orchestrators sharing
		// $CHITIN_DIR do not cross-skip comments. Older events without
		// a repo field are matched only when the caller leaves repo
		// empty (preserves the original single-repo behavior).
		if repo != "" {
			if got, _ := ev.Payload["repo"].(string); got != repo {
				continue
			}
		}
		return ev.TS, true
	}
	return "", false
}
