package speclint

import (
	"strings"
	"testing"
)

func TestCheckL05_GhApi_RepoPathOK(t *testing.T) {
	spec := "Use `gh api repos/owner/repo/pulls/123/files` to fetch.\n"
	vs := CheckL05("spec.md", spec, nil)
	if len(vs) != 0 {
		t.Fatalf("expected 0 violations, got %d: %+v", len(vs), vs)
	}
}

func TestCheckL05_GhApi_RepoPathWithPlaceholdersOK(t *testing.T) {
	spec := "Call `gh api repos/<owner>/<repo>/pulls/<N>/files` from the dispatcher.\n"
	vs := CheckL05("spec.md", spec, nil)
	if len(vs) != 0 {
		t.Fatalf("expected 0 violations (placeholders allowed), got %d: %+v", len(vs), vs)
	}
}

func TestCheckL05_GhApi_NonReposPathFlagged(t *testing.T) {
	spec := "Use `gh api /pulls/N/comments/M/replies` to reply.\n"
	vs := CheckL05("spec.md", spec, nil)
	if len(vs) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(vs), vs)
	}
	if vs[0].Rule != "L05" {
		t.Errorf("expected rule L05, got %q", vs[0].Rule)
	}
	if vs[0].Line != 1 {
		t.Errorf("expected line 1, got %d", vs[0].Line)
	}
	if vs[0].Severity != SeverityError {
		t.Errorf("expected error severity, got %q", vs[0].Severity)
	}
	if vs[0].File != "spec.md" {
		t.Errorf("expected file spec.md, got %q", vs[0].File)
	}
	if !strings.Contains(vs[0].Message, "gh api") || !strings.Contains(vs[0].Message, "repos/") {
		t.Errorf("expected message about gh api repos/, got %q", vs[0].Message)
	}
}

func TestCheckL05_OrchestratorSubcommand_InAllowlist(t *testing.T) {
	spec := "Run `chitin-orchestrator schedule` daily.\n"
	vs := CheckL05("spec.md", spec, []string{"chitin-orchestrator schedule"})
	if len(vs) != 0 {
		t.Fatalf("expected 0 violations, got %d: %+v", len(vs), vs)
	}
}

func TestCheckL05_OrchestratorSubcommand_NotInAllowlist(t *testing.T) {
	spec := "Run `chitin-orchestrator unknown-cmd` to do something.\n"
	vs := CheckL05("spec.md", spec, []string{"chitin-orchestrator schedule"})
	if len(vs) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(vs), vs)
	}
	if !strings.Contains(vs[0].Message, "unknown-cmd") {
		t.Errorf("expected message naming the subcommand, got %q", vs[0].Message)
	}
}

func TestCheckL05_KernelSubcommand_NotInAllowlist(t *testing.T) {
	spec := "Use `chitin-kernel events` to list.\n"
	vs := CheckL05("spec.md", spec, []string{"chitin-kernel emit"})
	if len(vs) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(vs), vs)
	}
	if !strings.Contains(vs[0].Message, "chitin-kernel") || !strings.Contains(vs[0].Message, "events") {
		t.Errorf("expected message naming binary + subcommand, got %q", vs[0].Message)
	}
}

func TestCheckL05_KernelSubcommand_InAllowlist(t *testing.T) {
	spec := "Use `chitin-kernel emit` to emit a chain event.\n"
	vs := CheckL05("spec.md", spec, []string{"chitin-kernel emit"})
	if len(vs) != 0 {
		t.Fatalf("expected 0 violations, got %d: %+v", len(vs), vs)
	}
}

func TestCheckL05_IntroducedByFR_IsAllowed(t *testing.T) {
	spec := "## Functional requirements\n" +
		"\n" +
		"- **FR-003** `chitin-orchestrator spec-lint <spec-dir>` subcommand performs deterministic checks.\n" +
		"- **FR-004** The linter runs deterministically.\n"
	vs := CheckL05("spec.md", spec, nil)
	if len(vs) != 0 {
		t.Fatalf("expected 0 violations (introduced by FR-003), got %d: %+v", len(vs), vs)
	}
}

func TestCheckL05_IntroducedByFR_AppliesToLaterReferences(t *testing.T) {
	// spec-lint introduced in FR-003 and later mentioned in edge-cases section —
	// the later mention should also pass because the heuristic seeded the set.
	spec := "## Functional requirements\n" +
		"\n" +
		"- **FR-003** `chitin-orchestrator spec-lint <spec-dir>` subcommand performs deterministic checks.\n" +
		"\n" +
		"## Edge cases\n" +
		"\n" +
		"- Operator can run `chitin-orchestrator spec-lint --refresh-allowlist` to refresh.\n"
	vs := CheckL05("spec.md", spec, nil)
	if len(vs) != 0 {
		t.Fatalf("expected 0 violations, got %d: %+v", len(vs), vs)
	}
}

func TestCheckL05_FRWithoutSubcommandWord_StillFlagged(t *testing.T) {
	// FR mentions chitin-orchestrator but no "subcommand" word — heuristic
	// does not trigger; the unknown sub must be flagged.
	spec := "- **FR-099** Operator runs `chitin-orchestrator bogus-cmd` to do a thing.\n"
	vs := CheckL05("spec.md", spec, nil)
	if len(vs) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(vs), vs)
	}
}

func TestCheckL05_MentionOutsideFR_StillFlagged(t *testing.T) {
	// Mention outside any FR block — heuristic does not trigger.
	spec := "## Why\n" +
		"\n" +
		"The `chitin-orchestrator newcmd` is new but mentioned outside any FR.\n"
	vs := CheckL05("spec.md", spec, nil)
	if len(vs) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(vs), vs)
	}
}

func TestCheckL05_AtPrefixIgnored(t *testing.T) {
	// `@chitin-orchestrator iterate` is a comment-trigger notation, not a CLI.
	spec := "Reply with `@chitin-orchestrator iterate` to force-iterate.\n"
	vs := CheckL05("spec.md", spec, nil)
	if len(vs) != 0 {
		t.Fatalf("expected 0 violations (@-prefixed), got %d: %+v", len(vs), vs)
	}
}

func TestCheckL05_LineNumbersReported(t *testing.T) {
	spec := "line 1\n" +
		"line 2\n" +
		"use `gh api /bad/path` here\n" +
		"line 4\n"
	vs := CheckL05("spec.md", spec, nil)
	if len(vs) != 1 || vs[0].Line != 3 {
		t.Fatalf("expected one violation on line 3, got %+v", vs)
	}
}

func TestCheckL05_MultipleViolationsInSameSpec(t *testing.T) {
	spec := "Bad refs:\n" +
		"`gh api /foo` here.\n" +
		"`gh api /bar` here.\n" +
		"`chitin-kernel events` here.\n"
	vs := CheckL05("spec.md", spec, nil)
	if len(vs) != 3 {
		t.Fatalf("expected 3 violations, got %d: %+v", len(vs), vs)
	}
}

func TestCheckL05_EmptyAllowlistAllowsFRIntroduced(t *testing.T) {
	spec := "- **FR-001** `chitin-kernel emit` subcommand emits an event.\n"
	vs := CheckL05("spec.md", spec, nil)
	if len(vs) != 0 {
		t.Fatalf("expected 0 violations (introduced + empty allowlist), got %d: %+v", len(vs), vs)
	}
}

func TestCheckL05_IntroducedButNotInPopulatedAllowlist_WarnsToPatch(t *testing.T) {
	// Operator maintains an allowlist; spec introduces a new subcommand
	// via FR. Spec author should patch the allowlist (spec 115 FR-006),
	// so we surface a warning rather than silently allowing the divergence.
	spec := "- **FR-001** `chitin-kernel emit` subcommand emits an event.\n"
	vs := CheckL05("spec.md", spec, []string{"chitin-kernel events"})
	if len(vs) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(vs), vs)
	}
	if vs[0].Severity != SeverityWarning {
		t.Errorf("expected warning severity, got %q", vs[0].Severity)
	}
	if !strings.Contains(vs[0].Message, "allowlist") {
		t.Errorf("expected message to reference allowlist, got %q", vs[0].Message)
	}
}

func TestParseAllowlist_CommentsAndBlanksIgnored(t *testing.T) {
	raw := "# comment line\n" +
		"\n" +
		"chitin-orchestrator schedule\n" +
		"  chitin-kernel emit  \n" +
		"# another comment\n"
	entries := ParseAllowlist(raw)
	want := map[string]bool{
		"chitin-orchestrator schedule": true,
		"chitin-kernel emit":           true,
	}
	if len(entries) != len(want) {
		t.Fatalf("expected %d entries, got %d: %+v", len(want), len(entries), entries)
	}
	for _, e := range entries {
		if !want[e] {
			t.Errorf("unexpected entry %q", e)
		}
	}
}

func TestParseAllowlist_Empty(t *testing.T) {
	if got := ParseAllowlist(""); len(got) != 0 {
		t.Errorf("expected empty list, got %+v", got)
	}
}
