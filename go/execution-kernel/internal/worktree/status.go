package worktree

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

const (
	PRStateNone   = "NONE"
	PRStateOpen   = "OPEN"
	PRStateMerged = "MERGED"

	staleMergedAfter = 7 * 24 * time.Hour
	staleNoneAfter   = 14 * 24 * time.Hour
)

var (
	ticketRe          = regexp.MustCompile(`t_[0-9a-f]{8}`)
	canonicalBranchRe = regexp.MustCompile(`^swarm/([a-z-]+)-([0-9a-f]{8})$`)
	legacyBranchRe    = regexp.MustCompile(`^([a-z-]+)/[^/]+$`)
)

type Runner interface {
	Run(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.Output()
}

type Options struct {
	RepoDir  string
	Now      time.Time
	Stale    bool
	CacheDir string
	Runner   Runner
}

type Row struct {
	Path         string   `json:"path"`
	Branch       string   `json:"branch"`
	KanbanTicket string   `json:"kanban_ticket,omitempty"`
	PRNumber     int      `json:"pr_number,omitempty"`
	PRState      string   `json:"pr_state"`
	OwnerLane    string   `json:"owner_lane"`
	LastCommitTS string   `json:"last_commit_ts"`
	AgeDays      int      `json:"age_days"`
	MergedAt     string   `json:"merged_at,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

type gitWorktree struct {
	Path   string
	Branch string
}

type prInfo struct {
	Number      int     `json:"number"`
	State       string  `json:"state"`
	HeadRefName string  `json:"headRefName"`
	MergedAt    *string `json:"mergedAt"`
}

func Status(ctx context.Context, opts Options) ([]Row, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.RepoDir == "" {
		opts.RepoDir = "."
	}
	if opts.Runner == nil {
		opts.Runner = ExecRunner{}
	}

	rawWorktrees, err := opts.Runner.Run(ctx, opts.RepoDir, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	worktrees := parseGitWorktrees(rawWorktrees)
	prs, err := fetchPRs(ctx, opts)
	if err != nil {
		return nil, err
	}

	rows := make([]Row, 0, len(worktrees))
	for _, wt := range worktrees {
		row, err := buildRow(ctx, opts, wt, prs)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	sortRows(rows)
	if err := writeCache(opts, rows); err != nil {
		return nil, err
	}
	if opts.Stale {
		rows = filterStale(rows)
	}
	return rows, nil
}

func parseGitWorktrees(raw []byte) []gitWorktree {
	blocks := bytes.Split(bytes.TrimSpace(raw), []byte("\n\n"))
	out := make([]gitWorktree, 0, len(blocks))
	for _, block := range blocks {
		if len(bytes.TrimSpace(block)) == 0 {
			continue
		}
		var wt gitWorktree
		for _, line := range bytes.Split(block, []byte("\n")) {
			key, value, ok := strings.Cut(string(line), " ")
			if !ok {
				continue
			}
			switch key {
			case "worktree":
				wt.Path = value
			case "branch":
				wt.Branch = strings.TrimPrefix(value, "refs/heads/")
			}
		}
		if wt.Branch == "" {
			wt.Branch = "HEAD"
		}
		if wt.Path != "" {
			out = append(out, wt)
		}
	}
	return out
}

func fetchPRs(ctx context.Context, opts Options) (map[string]prInfo, error) {
	ghCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	raw, err := opts.Runner.Run(ghCtx, opts.RepoDir, "gh", "pr", "list", "--state", "all", "--limit", "200", "--json", "number,state,headRefName,mergedAt")
	if err != nil {
		// gh is useful enrichment, not a reason to suppress local worktree state.
		return map[string]prInfo{}, nil
	}
	var prs []prInfo
	if err := json.Unmarshal(raw, &prs); err != nil {
		return nil, fmt.Errorf("parse gh pr list: %w", err)
	}
	byBranch := make(map[string]prInfo, len(prs))
	for _, pr := range prs {
		if pr.HeadRefName != "" {
			byBranch[pr.HeadRefName] = pr
		}
	}
	return byBranch, nil
}

func buildRow(ctx context.Context, opts Options, wt gitWorktree, prs map[string]prInfo) (Row, error) {
	rawTS, err := opts.Runner.Run(ctx, wt.Path, "git", "log", "-1", "--format=%ct")
	if err != nil {
		return Row{}, fmt.Errorf("git log %s: %w", wt.Path, err)
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(string(rawTS)), 10, 64)
	if err != nil {
		return Row{}, fmt.Errorf("parse last commit timestamp for %s: %w", wt.Path, err)
	}
	last := time.Unix(seconds, 0).UTC()

	pr := prs[wt.Branch]
	prState := normalizePRState(pr.State)
	ageDays := int(opts.Now.Sub(last) / (24 * time.Hour))
	if ageDays < 0 {
		ageDays = 0
	}

	row := Row{
		Path:         wt.Path,
		Branch:       wt.Branch,
		KanbanTicket: ticketFor(wt.Branch, wt.Path),
		PRNumber:     pr.Number,
		PRState:      prState,
		OwnerLane:    ownerLane(wt.Branch, wt.Path),
		LastCommitTS: last.Format(time.RFC3339),
		AgeDays:      ageDays,
		MergedAt:     normalizedOptionalTimeString(pr.MergedAt),
	}
	row.Tags = tagsFor(row, opts.Now)
	return row, nil
}

func normalizePRState(state string) string {
	state = strings.ToUpper(strings.TrimSpace(state))
	if state == "" {
		return PRStateNone
	}
	return state
}

func ticketFor(branch, path string) string {
	if match := ticketRe.FindString(branch); match != "" {
		return match
	}
	if m := canonicalBranchRe.FindStringSubmatch(branch); len(m) == 3 {
		return "t_" + m[2]
	}
	return ticketRe.FindString(path)
}

func ownerLane(branch, path string) string {
	if m := canonicalBranchRe.FindStringSubmatch(branch); len(m) == 3 {
		return m[1]
	}
	if m := legacyBranchRe.FindStringSubmatch(branch); len(m) == 2 && isLane(m[1]) {
		return m[1]
	}
	base := filepath.Base(path)
	if strings.HasPrefix(base, "swarm-") {
		rest := strings.TrimPrefix(base, "swarm-")
		if lane, _, ok := strings.Cut(rest, "-t_"); ok && isLane(lane) {
			return lane
		}
	}
	return "unknown"
}

func isLane(value string) bool {
	switch value {
	case "clawta", "codex", "copilot", "claude-code", "gemini", "human":
		return true
	default:
		return false
	}
}

func tagsFor(row Row, now time.Time) []string {
	tags := []string{}
	if isStale(row, now) {
		tags = append(tags, "stale")
	}
	if row.KanbanTicket == "" {
		tags = append(tags, "orphan")
	}
	if row.PRState == PRStateOpen {
		tags = append(tags, "in-progress")
	}
	if isLegacyBranch(row.Branch) {
		tags = append(tags, "legacy-naming")
	}
	return tags
}

func isStale(row Row, now time.Time) bool {
	if row.PRState == PRStateMerged && row.MergedAt != "" {
		mergedAt, err := time.Parse(time.RFC3339, row.MergedAt)
		return err == nil && now.Sub(mergedAt) > staleMergedAfter
	}
	return row.PRState == PRStateNone && row.AgeDays > 14
}

func isLegacyBranch(branch string) bool {
	if strings.HasPrefix(branch, "swarm/") {
		return false
	}
	m := legacyBranchRe.FindStringSubmatch(branch)
	return len(m) == 2 && isLane(m[1])
}

func normalizedOptionalTimeString(value *string) string {
	if value == nil || *value == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return *value
	}
	return t.UTC().Format(time.RFC3339)
}

func sortRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].AgeDays != rows[j].AgeDays {
			return rows[i].AgeDays < rows[j].AgeDays
		}
		if rows[i].Path != rows[j].Path {
			return rows[i].Path < rows[j].Path
		}
		return rows[i].Branch < rows[j].Branch
	})
}

func filterStale(rows []Row) []Row {
	filtered := make([]Row, 0, len(rows))
	for _, row := range rows {
		if hasTag(row.Tags, "stale") {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func writeCache(opts Options, rows []Row) error {
	cacheDir := opts.CacheDir
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		cacheDir = filepath.Join(home, ".cache", "chitin")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("create worktree status cache dir: %w", err)
	}
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal worktree status cache: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "worktree-status.json"), append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("write worktree status cache: %w", err)
	}
	return nil
}

func FormatText(rows []Row) string {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PATH\tBRANCH\tKANBAN_TICKET\tPR_NUMBER\tPR_STATE\tOWNER_LANE\tLAST_COMMIT_TS\tAGE_DAYS\tTAGS")
	for _, row := range rows {
		ticket := dash(row.KanbanTicket)
		prNumber := "-"
		if row.PRNumber != 0 {
			prNumber = fmt.Sprintf("#%d", row.PRNumber)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			row.Path,
			row.Branch,
			ticket,
			prNumber,
			row.PRState,
			row.OwnerLane,
			row.LastCommitTS,
			row.AgeDays,
			formatTags(row.Tags),
		)
	}
	_ = tw.Flush()
	return buf.String()
}

func FormatJSONLines(rows []Row) (string, error) {
	var buf strings.Builder
	for _, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil {
			return "", err
		}
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	return buf.String(), nil
}

func FormatPruneEligible(rows []Row) string {
	var buf strings.Builder
	for _, row := range rows {
		if hasTag(row.Tags, "stale") {
			buf.WriteString(row.Path)
			buf.WriteByte('\n')
		}
	}
	return buf.String()
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatTags(tags []string) string {
	if len(tags) == 0 {
		return "-"
	}
	parts := make([]string, len(tags))
	for i, tag := range tags {
		parts[i] = "[" + tag + "]"
	}
	return strings.Join(parts, " ")
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}
