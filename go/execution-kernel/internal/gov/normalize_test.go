package gov

import "testing"

func TestNormalize_TerminalRmRf(t *testing.T) {
	a, err := Normalize("terminal", map[string]any{"command": "rm -rf go/"})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if a.Type != ActShellExec {
		t.Errorf("Type: got %q want shell.exec", a.Type)
	}
	if a.Target != "rm -rf go/" {
		t.Errorf("Target: got %q", a.Target)
	}
}

func TestNormalize_ExecuteCodeSubprocessRm(t *testing.T) {
	// Critical bypass closure: execute_code that shells out to rm -rf
	// must produce the same Action as direct terminal rm -rf.
	code := `import subprocess
subprocess.run(["rm", "-rf", "go/"])`
	a, err := Normalize("execute_code", map[string]any{"code": code})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if a.Type != ActShellExec {
		t.Errorf("Type: got %q want shell.exec (bypass closure failed)", a.Type)
	}
	if a.Target == "" {
		t.Errorf("Target should be non-empty for shell.exec")
	}
}

func TestNormalize_ExecuteCodeShutilRmtree(t *testing.T) {
	code := `import shutil
shutil.rmtree("go/")`
	a, err := Normalize("execute_code", map[string]any{"code": code})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if a.Type != ActShellExec && a.Type != ActFileDelete {
		t.Errorf("shutil.rmtree should map to shell.exec or file.delete, got %q", a.Type)
	}
}

func TestNormalize_TerminalGitPush(t *testing.T) {
	a, _ := Normalize("terminal", map[string]any{"command": "git push origin main"})
	if a.Type != ActGitPush {
		t.Errorf("Type: got %q want git.push", a.Type)
	}
	if a.Target != "main" {
		t.Errorf("Target (branch): got %q want main", a.Target)
	}
}

func TestNormalize_TerminalGitForcePush(t *testing.T) {
	a, _ := Normalize("terminal", map[string]any{"command": "git push --force origin main"})
	if a.Type != ActGitForcePush {
		t.Errorf("Type: got %q want git.force-push", a.Type)
	}
}

func TestNormalize_TerminalGhPRCreate(t *testing.T) {
	a, _ := Normalize("terminal", map[string]any{"command": `gh pr create --title "x"`})
	if a.Type != ActGithubPRCreate {
		t.Errorf("Type: got %q want github.pr.create", a.Type)
	}
}

func TestNormalize_WriteFileEnv(t *testing.T) {
	a, _ := Normalize("write_file", map[string]any{"path": ".env", "content": "X=1"})
	if a.Type != ActFileWrite {
		t.Errorf("Type: got %q want file.write", a.Type)
	}
	if a.Target != ".env" {
		t.Errorf("Target: got %q want .env", a.Target)
	}
}

func TestNormalize_WriteFileGovernanceSelfMod(t *testing.T) {
	a, _ := Normalize("write_file", map[string]any{"path": "chitin.yaml", "content": "..."})
	if a.Type != ActFileWrite {
		t.Errorf("Type: got %q want file.write", a.Type)
	}
	if a.Target != "chitin.yaml" {
		t.Errorf("Target: got %q want chitin.yaml", a.Target)
	}
}

func TestNormalize_DelegateTask(t *testing.T) {
	a, _ := Normalize("delegate_task", map[string]any{"goal": "review"})
	if a.Type != ActDelegateTask {
		t.Errorf("Type: got %q want delegate.task", a.Type)
	}
}

func TestNormalize_UnknownTool(t *testing.T) {
	a, _ := Normalize("no_such_tool", map[string]any{"foo": "bar"})
	if a.Type != ActUnknown {
		t.Errorf("Type: got %q want unknown (fail-closed)", a.Type)
	}
}

func TestNormalize_TerminalReadOnly(t *testing.T) {
	cases := []struct{ cmd string; want ActionType }{
		{"ls -la", ActShellExec},
		{"cat /etc/passwd", ActShellExec},
		{"git status", ActGitStatus},
		{"git log --oneline", ActGitLog},
		{"git diff main", ActGitDiff},
		{"gh issue view 40", ActGithubIssueView},
		{"gh issue list", ActGithubIssueList},
	}
	for _, c := range cases {
		a, _ := Normalize("terminal", map[string]any{"command": c.cmd})
		if a.Type != c.want {
			t.Errorf("%q: got %q want %q", c.cmd, a.Type, c.want)
		}
	}
}
