package replay

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

var ticketPattern = regexp.MustCompile(`t_[0-9a-f]{8}`)

type DashboardSession struct {
	SessionID string   `json:"session_id"`
	Ts        string   `json:"ts"`
	Driver    string   `json:"driver,omitempty"`
	Agent     string   `json:"agent,omitempty"`
	TicketID  string   `json:"ticket_id,omitempty"`
	CostUSD   float64  `json:"cost_usd"`
	Success   *bool    `json:"success,omitempty"`
	ELODelta  *float64 `json:"elo_delta,omitempty"`
}

type DashboardEloEntry struct {
	Driver          string  `json:"driver"`
	Model           string  `json:"model"`
	Role            string  `json:"role,omitempty"`
	TaskClass       string  `json:"task_class,omitempty"`
	EloScore        float64 `json:"elo_score"`
	DispatchesCount int     `json:"dispatches_count"`
	LastDispatchID  string  `json:"last_dispatch_id,omitempty"`
	LastUpdated     int64   `json:"last_updated,omitempty"`
}

func ListDashboardSessions(limit int) ([]DashboardSession, error) {
	recent, err := ListRecentSessions(limit)
	if err != nil {
		return nil, err
	}
	stateDir, err := chitinStateDir()
	if err != nil {
		return nil, err
	}
	out := make([]DashboardSession, 0, len(recent))
	for _, item := range recent {
		events, err := readSessionEvents(stateDir, item.SessionID)
		if err != nil {
			return nil, err
		}
		joins, err := readDecisionJoins(stateDir, events)
		if err != nil {
			return nil, err
		}
		summary := DashboardSession{
			SessionID: item.SessionID,
			Ts:        item.LastTs,
			Driver:    item.Driver,
			Agent:     item.Agent,
		}
		var denyCount int
		for _, ev := range events {
			if summary.Driver == "" {
				summary.Driver = valueOrEmpty(ev.Labels["driver"])
			}
			if summary.Agent == "" {
				summary.Agent = deriveAgent(ev.Event, nil)
			}
			if summary.TicketID == "" {
				summary.TicketID = extractTicketID(ev)
			}
			switch ev.EventType {
			case "decision":
				if joined := joins[decisionKeyForEvent(ev)]; joined != nil {
					summary.CostUSD += joined.CostUSD
					if !joined.Allowed {
						denyCount++
					}
				} else if valueOrEmpty(stringField(ev.payload, "decision")) == "deny" {
					denyCount++
				}
			case "session_end":
				if success := successFromSessionEnd(ev.payload); success != nil {
					summary.Success = success
				}
			}
		}
		// Success is only known from a terminal session_end event. A session
		// without one is in-progress or aborted — leave Success nil ("unknown")
		// rather than declaring it successful just because no deny was seen.
		if delta, ok := lookupEloDelta(summary.TicketID); ok {
			summary.ELODelta = &delta
		}
		out = append(out, summary)
	}
	return out, nil
}

func ReadDashboardPolicy(cwd string) (string, string, error) {
	_, paths, err := loadPolicyPaths(cwd)
	if err != nil {
		return "", "", err
	}
	if len(paths) == 0 {
		return "", "", fmt.Errorf("no chitin.yaml from %s upward", cwd)
	}
	path := paths[len(paths)-1]
	body, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	return path, string(body), nil
}

func ReadDashboardElo(limit int) ([]DashboardEloEntry, bool, error) {
	if limit <= 0 {
		limit = 10
	}
	dbPath := filepath.Join(os.Getenv("HOME"), ".openclaw", "data", "clawta.db")
	// mode=ro: this is a read-only dashboard lookup — without it sql/sqlite
	// would create an empty clawta.db when the data dir exists but the DB
	// does not, masking the "no leaderboard yet" state with a fake-empty one.
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return placeholderEloBoard(limit), true, nil
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT driver, model, role, task_class, elo_score, dispatches_count, last_dispatch_id, last_updated
		  FROM swarm_elo
		 ORDER BY elo_score DESC, dispatches_count DESC, driver ASC, model ASC
		 LIMIT ?
	`, limit)
	if err != nil {
		lower := strings.ToLower(err.Error())
		// "no such column" covers legacy swarm_elo tables that predate the
		// role/task_class columns — fall back to the placeholder board
		// rather than erroring the whole dashboard.
		if strings.Contains(lower, "no such table") ||
			strings.Contains(lower, "no such column") ||
			strings.Contains(lower, "unable to open database file") {
			return placeholderEloBoard(limit), true, nil
		}
		return nil, false, err
	}
	defer rows.Close()
	out := make([]DashboardEloEntry, 0, limit)
	for rows.Next() {
		var item DashboardEloEntry
		// last_dispatch_id is nullable in the swarm_elo schema; scan through
		// sql.NullString so a NULL row doesn't fail the whole query.
		var lastDispatchID sql.NullString
		if err := rows.Scan(
			&item.Driver,
			&item.Model,
			&item.Role,
			&item.TaskClass,
			&item.EloScore,
			&item.DispatchesCount,
			&lastDispatchID,
			&item.LastUpdated,
		); err != nil {
			return nil, false, err
		}
		item.LastDispatchID = lastDispatchID.String
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(out) == 0 {
		return placeholderEloBoard(limit), true, nil
	}
	return out, false, nil
}

func placeholderEloBoard(limit int) []DashboardEloEntry {
	base := []DashboardEloEntry{
		{Driver: "codex", Model: "gpt-5.5", Role: "programmer", TaskClass: "feature", EloScore: 1500, DispatchesCount: 0},
		{Driver: "claude-code", Model: "sonnet", Role: "programmer", TaskClass: "bugfix", EloScore: 1500, DispatchesCount: 0},
		{Driver: "gemini", Model: "2.5-pro", Role: "researcher", TaskClass: "analysis", EloScore: 1500, DispatchesCount: 0},
	}
	if len(base) > limit {
		return base[:limit]
	}
	return base
}

func lookupEloDelta(ticketID string) (float64, bool) {
	if ticketID == "" {
		return 0, false
	}
	dbPath := filepath.Join(os.Getenv("HOME"), ".openclaw", "data", "clawta.db")
	// mode=ro: read-only lookup; without it a missing clawta.db would be
	// created empty as a side effect of rendering the dashboard.
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return 0, false
	}
	defer db.Close()
	var total sql.NullFloat64
	err = db.QueryRow(`
		SELECT CAST(total_score AS REAL)
		  FROM swarm_dispatch_scores
		 WHERE ticket_id = ?
		 ORDER BY scored_at DESC, id DESC
		 LIMIT 1
	`, ticketID).Scan(&total)
	if err != nil {
		lower := strings.ToLower(err.Error())
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(lower, "no such table") || strings.Contains(lower, "unable to open database file") {
			return 0, false
		}
		return 0, false
	}
	if !total.Valid {
		return 0, false
	}
	return total.Float64, true
}

func extractTicketID(ev sessionEvent) string {
	candidates := []string{
		ev.ChainID,
		valueOrEmpty(ev.Labels["workflow_id"]),
		valueOrEmpty(ev.Labels["ticket_id"]),
		valueOrEmpty(ev.Labels["branch"]),
		stringField(ev.payload, "workflow_id"),
		stringField(ev.payload, "ticket_id"),
		stringField(ev.payload, "kanban_ticket"),
		stringField(ev.payload, "branch"),
		stringField(ev.payload, "worktree_path"),
		stringField(ev.payload, "cwd"),
		stringField(ev.payload, "action_target"),
	}
	for _, candidate := range candidates {
		if match := ticketPattern.FindString(candidate); match != "" {
			return match
		}
	}
	return ""
}

func successFromSessionEnd(payload map[string]any) *bool {
	reason := strings.ToLower(strings.TrimSpace(stringField(payload, "reason")))
	switch reason {
	case "clean", "success":
		v := true
		return &v
	case "exit_nonzero", "failed", "error":
		v := false
		return &v
	}
	if result, ok := mapField(payload, "result"); ok {
		if ok, found := boolFromAny(result["success"]); found {
			return &ok
		}
	}
	return nil
}

func boolFromAny(v any) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "true", "success", "ok", "clean":
			return true, true
		case "false", "failed", "error":
			return false, true
		}
	}
	return false, false
}

func loadPolicyPaths(cwd string) (map[string]any, []string, error) {
	policy, paths, err := LoadPolicyWithPaths(cwd)
	if err != nil {
		return nil, nil, err
	}
	return policy, paths, nil
}

func LoadPolicyWithPaths(cwd string) (map[string]any, []string, error) {
	policy, paths, err := loadMergedPolicy(cwd)
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, err
	}
	return out, paths, nil
}
