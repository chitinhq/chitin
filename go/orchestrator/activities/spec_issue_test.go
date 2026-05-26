package activities

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type specIssueEvent struct {
	eventType string
	repo      string
	specRef   string
	payload   map[string]any
}

func withSpecIssueHooks(t *testing.T, gh func(context.Context, ...string) (string, error), prior func(string, string, string) (string, bool)) *[]specIssueEvent {
	t.Helper()
	oldGH, oldEmit, oldPrior := specIssueGHFn, specIssueEmitFn, specIssuePriorCommentFn
	events := []specIssueEvent{}
	specIssueGHFn = gh
	specIssueEmitFn = func(_ context.Context, eventType, repo, specRef string, payload map[string]any) {
		events = append(events, specIssueEvent{eventType: eventType, repo: repo, specRef: specRef, payload: payload})
	}
	if prior != nil {
		specIssuePriorCommentFn = prior
	} else {
		specIssuePriorCommentFn = func(string, string, string) (string, bool) { return "", false }
	}
	t.Cleanup(func() {
		specIssueGHFn, specIssueEmitFn, specIssuePriorCommentFn = oldGH, oldEmit, oldPrior
	})
	return &events
}

func TestEnsureSpecIssue_CreatesWhenAbsent(t *testing.T) {
	var calls [][]string
	events := withSpecIssueHooks(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, args)
		switch strings.Join(args[:2], " ") {
		case "issue list":
			return `[]`, nil
		case "issue create":
			got := strings.Join(args, "\x00")
			if !strings.Contains(got, "[126-spec-issue-for-visibility] Visibility") {
				t.Fatalf("create title args=%q", got)
			}
			if !strings.Contains(got, "Spec PR: https://github.com/o/r/pull/1") {
				t.Fatalf("create body args missing spec PR: %q", got)
			}
			return "https://github.com/o/r/issues/77", nil
		default:
			t.Fatalf("unexpected gh args: %v", args)
		}
		return "", nil
	}, nil)

	res, err := NewEnsureSpecIssue().Execute(context.Background(), EnsureSpecIssueInput{
		Repo: "o/r", SpecRef: "126-spec-issue-for-visibility", SpecTitle: "Visibility",
		SpecPRURL: "https://github.com/o/r/pull/1", SpecMDURL: "spec", TasksMDURL: "tasks",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IssueNumber != 77 || !res.WasNew {
		t.Fatalf("res=%+v want issue 77 was_new", res)
	}
	if len(calls) != 2 {
		t.Fatalf("calls=%v want list+create", calls)
	}
	if len(*events) != 1 || (*events)[0].eventType != SpecIssueOpenedEvent || (*events)[0].payload["was_new"] != true {
		t.Fatalf("events=%+v", *events)
	}
}

func TestEnsureSpecIssue_ReusesWhenPresent(t *testing.T) {
	var created bool
	events := withSpecIssueHooks(t, func(_ context.Context, args ...string) (string, error) {
		if strings.Join(args[:2], " ") == "issue list" {
			return `[{"number":22,"title":"[126-spec-issue-for-visibility] Visibility","state":"open"}]`, nil
		}
		if strings.Join(args[:2], " ") == "issue create" {
			created = true
		}
		return "", nil
	}, nil)
	res, _ := NewEnsureSpecIssue().Execute(context.Background(), EnsureSpecIssueInput{Repo: "o/r", SpecRef: "126-spec-issue-for-visibility"})
	if res.IssueNumber != 22 || res.WasNew {
		t.Fatalf("res=%+v want existing issue 22 was_new=false", res)
	}
	if created {
		t.Fatal("issue create was called for existing issue")
	}
	if (*events)[0].payload["was_new"] != false {
		t.Fatalf("event payload=%+v", (*events)[0].payload)
	}
}

func TestCommentSpecIssue_IdempotentOnRetry(t *testing.T) {
	var comments int
	prior := false
	events := withSpecIssueHooks(t, func(_ context.Context, args ...string) (string, error) {
		switch strings.Join(args[:2], " ") {
		case "issue list":
			return `[{"number":22,"title":"[126-spec-issue-for-visibility] Visibility","state":"open"}]`, nil
		case "issue comment":
			comments++
			prior = true
			return "", nil
		default:
			t.Fatalf("unexpected gh args: %v", args)
		}
		return "", nil
	}, func(_, _, _ string) (string, bool) {
		if prior {
			return "2026-05-26T00:00:00Z", true
		}
		return "", false
	})
	in := CommentSpecIssueInput{Repo: "o/r", SpecRef: "126-spec-issue-for-visibility", TemplateID: SpecIssueTemplateDispatchTriggered, Params: map[string]string{"run_id": "r1", "driver": "codex", "capability": "code.implement", "at": "now"}}
	first, _ := NewCommentSpecIssue().Execute(context.Background(), in)
	second, _ := NewCommentSpecIssue().Execute(context.Background(), in)
	if !first.Commented || !second.Skipped || comments != 1 {
		t.Fatalf("first=%+v second=%+v comments=%d", first, second, comments)
	}
	if got := []string{(*events)[0].eventType, (*events)[1].eventType}; !reflect.DeepEqual(got, []string{SpecIssueCommentedEvent, SpecIssueCommentSkippedEvent}) {
		t.Fatalf("events=%v", got)
	}
}

func TestUpdateSpecIssueBody_PatchesNamedAnchors(t *testing.T) {
	var edited string
	withSpecIssueHooks(t, func(_ context.Context, args ...string) (string, error) {
		switch strings.Join(args[:2], " ") {
		case "issue list":
			return `[{"number":22,"title":"[126-spec-issue-for-visibility] Visibility","state":"open"}]`, nil
		case "issue view":
			return `{"body":"before\n<!-- chitin:impl_pr -->placeholder<!-- /chitin:impl_pr -->\nafter"}`, nil
		case "issue edit":
			edited = args[len(args)-1]
			return "", nil
		default:
			t.Fatalf("unexpected gh args: %v", args)
		}
		return "", nil
	}, nil)
	res, _ := NewUpdateSpecIssueBody().Execute(context.Background(), UpdateSpecIssueBodyInput{
		Repo: "o/r", SpecRef: "126-spec-issue-for-visibility", Patches: map[string]string{"impl_pr": "#1135", "nonexistent": "x"},
	})
	if !res.Updated || !strings.Contains(edited, "<!-- chitin:impl_pr -->#1135<!-- /chitin:impl_pr -->") || strings.Contains(edited, "nonexistent") {
		t.Fatalf("res=%+v edited=%q", res, edited)
	}
}

func TestCloseSpecIssue_PostsFinalCommentThenCloses(t *testing.T) {
	var order []string
	events := withSpecIssueHooks(t, func(_ context.Context, args ...string) (string, error) {
		switch strings.Join(args[:2], " ") {
		case "issue list":
			return `[{"number":22,"title":"[126-spec-issue-for-visibility] Visibility","state":"open"}]`, nil
		case "issue comment":
			order = append(order, "comment")
			return "", nil
		case "issue close":
			order = append(order, "close")
			return "", nil
		default:
			t.Fatalf("unexpected gh args: %v", args)
		}
		return "", nil
	}, nil)
	res, _ := NewCloseSpecIssue().Execute(context.Background(), CloseSpecIssueInput{
		Repo: "o/r", SpecRef: "126-spec-issue-for-visibility",
		FinalCommentParams: map[string]string{"pr_url": "https://github.com/o/r/pull/2", "merge_sha": "abc", "elapsed": "1h"},
	})
	if !res.Closed || !reflect.DeepEqual(order, []string{"comment", "close"}) {
		t.Fatalf("res=%+v order=%v", res, order)
	}
	if (*events)[len(*events)-1].eventType != SpecIssueClosedEvent {
		t.Fatalf("events=%+v", *events)
	}
}

func TestCommentSpecIssue_DispatchFailed_KeepsIssueOpen(t *testing.T) {
	var calls []string
	withSpecIssueHooks(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args[:2], " "))
		if strings.Join(args[:2], " ") == "issue list" {
			return `[{"number":22,"title":"[126-spec-issue-for-visibility] Visibility","state":"open"}]`, nil
		}
		return "", nil
	}, nil)
	res, _ := NewCommentSpecIssue().Execute(context.Background(), CommentSpecIssueInput{
		Repo: "o/r", SpecRef: "126-spec-issue-for-visibility", TemplateID: SpecIssueTemplateDispatchFailed,
		Params: map[string]string{"reason": "capability_mismatch", "at": "now", "run_id": "r1"},
	})
	if !res.Commented {
		t.Fatalf("res=%+v", res)
	}
	for _, c := range calls {
		if c == "issue close" {
			t.Fatal("dispatch_failed closed the issue")
		}
	}
}

func TestSpecIssue_GracefulOnGHFailure(t *testing.T) {
	events := withSpecIssueHooks(t, func(context.Context, ...string) (string, error) {
		return "", errors.New("rate limit: retry later")
	}, nil)
	res, err := NewEnsureSpecIssue().Execute(context.Background(), EnsureSpecIssueInput{Repo: "o/r", SpecRef: "126-spec-issue-for-visibility"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IssueNumber != 0 {
		t.Fatalf("res=%+v", res)
	}
	if len(*events) != 1 || (*events)[0].eventType != SpecIssueUpdateFailedEvent || (*events)[0].payload["op"] != "issue_list" {
		t.Fatalf("events=%+v", *events)
	}
	if !strings.Contains((*events)[0].payload["stderr_tail"].(string), "rate limit") {
		t.Fatalf("payload=%+v", (*events)[0].payload)
	}
}

func TestSpecIssue_BreakGlassEnvDisablesAll(t *testing.T) {
	t.Setenv("CHITIN_SPEC_ISSUE_DISABLED", "1")
	var ghCalls int
	events := withSpecIssueHooks(t, func(context.Context, ...string) (string, error) {
		ghCalls++
		return "", nil
	}, nil)
	_, _ = NewEnsureSpecIssue().Execute(context.Background(), EnsureSpecIssueInput{SpecRef: "126-spec-issue-for-visibility"})
	_, _ = NewCommentSpecIssue().Execute(context.Background(), CommentSpecIssueInput{SpecRef: "126-spec-issue-for-visibility"})
	_, _ = NewUpdateSpecIssueBody().Execute(context.Background(), UpdateSpecIssueBodyInput{SpecRef: "126-spec-issue-for-visibility"})
	_, _ = NewCloseSpecIssue().Execute(context.Background(), CloseSpecIssueInput{SpecRef: "126-spec-issue-for-visibility"})
	if ghCalls != 0 {
		t.Fatalf("ghCalls=%d want 0", ghCalls)
	}
	if len(*events) != 4 {
		t.Fatalf("events=%+v", *events)
	}
	for _, ev := range *events {
		if ev.eventType != SpecIssueUpdateFailedEvent || ev.payload["op"] != "disabled_by_env" {
			t.Fatalf("event=%+v", ev)
		}
	}
}
