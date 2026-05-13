package worktree

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	outputs map[string][]byte
}

func (f fakeRunner) Run(_ context.Context, dir string, name string, args ...string) ([]byte, error) {
	key := dir + "|" + name + " " + strings.Join(args, " ")
	out, ok := f.outputs[key]
	if !ok {
		return nil, fmt.Errorf("missing fake command %s", key)
	}
	return out, nil
}

func TestStatusBuildsRowsSortedAndTagged(t *testing.T) {
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	ts := func(ageDays int) []byte {
		return []byte(fmt.Sprintf("%d\n", now.Add(-time.Duration(ageDays)*24*time.Hour).Unix()))
	}
	cacheDir := t.TempDir()
	repo := "/repo"
	runner := fakeRunner{outputs: map[string][]byte{
		repo + "|git worktree list --porcelain": []byte(strings.Join([]string{
			"worktree /repo",
			"HEAD a",
			"branch refs/heads/main",
			"",
			"worktree /cache/swarm-codex-t_c083fd6d",
			"HEAD b",
			"branch refs/heads/swarm/codex-c083fd6d",
			"",
			"worktree /cache/swarm-old-t_11111111",
			"HEAD c",
			"branch refs/heads/codex/old-branch",
			"",
			"worktree /cache/no-ticket",
			"HEAD d",
			"branch refs/heads/topic/no-ticket",
		}, "\n")),
		repo + "|gh pr list --state all --limit 200 --json number,state,headRefName,mergedAt": []byte(`[
			{"number": 10, "state": "OPEN", "headRefName": "swarm/codex-c083fd6d", "mergedAt": null},
			{"number": 11, "state": "MERGED", "headRefName": "codex/old-branch", "mergedAt": "2026-05-01T12:00:00Z"}
		]`),
		"/repo|git log -1 --format=%ct":                         ts(0),
		"/cache/swarm-codex-t_c083fd6d|git log -1 --format=%ct": ts(1),
		"/cache/swarm-old-t_11111111|git log -1 --format=%ct":   ts(9),
		"/cache/no-ticket|git log -1 --format=%ct":              ts(15),
	}}

	rows, err := Status(context.Background(), Options{
		RepoDir:  repo,
		Now:      now,
		CacheDir: cacheDir,
		Runner:   runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(rows), 4; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got := []int{rows[0].AgeDays, rows[1].AgeDays, rows[2].AgeDays, rows[3].AgeDays}; !reflect.DeepEqual(got, []int{0, 1, 9, 15}) {
		t.Fatalf("rows sorted by age asc = %v", got)
	}

	canonical := rows[1]
	if canonical.KanbanTicket != "t_c083fd6d" || canonical.PRNumber != 10 || canonical.PRState != PRStateOpen || canonical.OwnerLane != "codex" {
		t.Fatalf("canonical row mismatch: %+v", canonical)
	}
	if !reflect.DeepEqual(canonical.Tags, []string{"in-progress"}) {
		t.Fatalf("canonical tags = %v", canonical.Tags)
	}

	legacy := rows[2]
	if legacy.KanbanTicket != "t_11111111" || !reflect.DeepEqual(legacy.Tags, []string{"stale", "legacy-naming"}) {
		t.Fatalf("legacy row mismatch: %+v", legacy)
	}

	orphan := rows[3]
	if orphan.KanbanTicket != "" || !reflect.DeepEqual(orphan.Tags, []string{"stale", "orphan"}) {
		t.Fatalf("orphan row mismatch: %+v", orphan)
	}

	cacheRaw, err := os.ReadFile(filepath.Join(cacheDir, "worktree-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cached []Row
	if err := json.Unmarshal(cacheRaw, &cached); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cached, rows) {
		t.Fatalf("cache rows differ from result")
	}
}

func TestStatusStaleFilterAndPruneOutput(t *testing.T) {
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	rows, err := Status(context.Background(), Options{
		RepoDir:  "/repo",
		Now:      now,
		Stale:    true,
		CacheDir: t.TempDir(),
		Runner: fakeRunner{outputs: map[string][]byte{
			"/repo|git worktree list --porcelain": []byte(strings.Join([]string{
				"worktree /cache/open",
				"branch refs/heads/swarm/codex-aaaaaaaa",
				"",
				"worktree /cache/merged",
				"branch refs/heads/swarm/codex-bbbbbbbb",
				"",
				"worktree /cache/none-old",
				"branch refs/heads/swarm/codex-cccccccc",
			}, "\n")),
			"/repo|gh pr list --state all --limit 200 --json number,state,headRefName,mergedAt": []byte(`[
				{"number": 1, "state": "OPEN", "headRefName": "swarm/codex-aaaaaaaa", "mergedAt": null},
				{"number": 2, "state": "MERGED", "headRefName": "swarm/codex-bbbbbbbb", "mergedAt": "2026-05-05T11:59:59Z"}
			]`),
			"/cache/open|git log -1 --format=%ct":     []byte("1777723200\n"),
			"/cache/merged|git log -1 --format=%ct":   []byte("1777723200\n"),
			"/cache/none-old|git log -1 --format=%ct": []byte("1777204800\n"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d: %+v", got, want, rows)
	}
	if got, want := FormatPruneEligible(rows), "/cache/merged\n/cache/none-old\n"; got != want {
		t.Fatalf("prune output = %q, want %q", got, want)
	}
}

func TestFormatJSONLinesIsDeterministic(t *testing.T) {
	rows := []Row{{
		Path:         "/cache/swarm-codex-t_c083fd6d",
		Branch:       "swarm/codex-c083fd6d",
		KanbanTicket: "t_c083fd6d",
		PRState:      PRStateNone,
		OwnerLane:    "codex",
		LastCommitTS: "2026-05-13T12:00:00Z",
		AgeDays:      0,
	}}
	first, err := FormatJSONLines(rows)
	if err != nil {
		t.Fatal(err)
	}
	second, err := FormatJSONLines(rows)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("json lines not deterministic:\n%s\n%s", first, second)
	}
	if !strings.HasSuffix(first, "\n") || !strings.Contains(first, `"kanban_ticket":"t_c083fd6d"`) {
		t.Fatalf("unexpected json lines: %q", first)
	}
}

func TestFormatTextIncludesAcceptanceColumnsAndTags(t *testing.T) {
	out := FormatText([]Row{{
		Path:         "/cache/old",
		Branch:       "codex/old",
		KanbanTicket: "t_11111111",
		PRNumber:     7,
		PRState:      PRStateMerged,
		OwnerLane:    "codex",
		LastCommitTS: "2026-05-01T00:00:00Z",
		AgeDays:      12,
		Tags:         []string{"stale", "legacy-naming"},
	}})
	for _, want := range []string{"PATH", "BRANCH", "KANBAN_TICKET", "PR_NUMBER", "PR_STATE", "OWNER_LANE", "LAST_COMMIT_TS", "AGE_DAYS", "[stale] [legacy-naming]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatted text missing %q:\n%s", want, out)
		}
	}
}
