package canon

import (
	"strings"
	"testing"
)

// Each test below covers one bypass class identified in issues #58–62.
// The cases are bypass *variations* the prior anchored-regex detectors
// missed: extra whitespace, quoting, env-prefix, leading global flags,
// alternative invocation forms.

func TestIsRecursiveDelete_BypassVariations(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
		why  string
	}{
		{"rm -rf /tmp/x", true, "baseline"},
		{"rm  -rf /tmp/x", true, "double space"},
		{"rm\t-rf /tmp/x", true, "tab"},
		{"rm '-rf' /tmp/x", true, "quoted flag"},
		{"rm -r -f /tmp/x", true, "split flags"},
		{"rm -fr /tmp/x", true, "reordered flags"},
		{"rm --recursive /tmp/x", true, "long form recursive"},
		{"rm --recursive --force /tmp/x", true, "long form both"},
		{"rm -rfv /tmp/x", true, "combined with verbose"},
		{"rm /tmp/x", false, "no recursive flag"},
		{"rm -f /tmp/x", false, "force only, not recursive"},
		{"ls -r /tmp", false, "different tool"},
		{"echo rm -rf /", false, "rm in non-leading position"},
	}
	for _, tc := range cases {
		c := ParseOne(tc.raw)
		got := IsRecursiveDelete(c)
		if got != tc.want {
			t.Errorf("IsRecursiveDelete(%q) = %v, want %v (%s); flags=%v args=%v",
				tc.raw, got, tc.want, tc.why, c.Flags, c.Args)
		}
	}
}

func TestIsBareGitPush(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
		why  string
	}{
		{"git push", true, "bare push"},
		{"git push origin", true, "remote without branch"},
		{"git push -u origin HEAD", true, "HEAD without colon-branch"},
		{"git push --set-upstream origin", true, "long-form set-upstream"},
		{"git push -f", true, "force flag, no branch"},
		{"git -C /tmp push", true, "global flag, no branch"},
		{"git push origin main", false, "explicit branch"},
		{"git push origin HEAD:fix/x", false, "HEAD: prefix is explicit"},
		{"git push origin feat-1 feat-2", false, "multiple branches"},
		{"git status", false, "different action"},
	}
	for _, tc := range cases {
		c := ParseOne(tc.raw)
		got := IsBareGitPush(c)
		if got != tc.want {
			t.Errorf("IsBareGitPush(%q) = %v, want %v (%s); tool=%q action=%q args=%v",
				tc.raw, got, tc.want, tc.why, c.Tool, c.Action, c.Args)
		}
	}
}

func TestIsInfraDestroy_BypassVariations(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		why  string
	}{
		{"terraform destroy", "terraform", "baseline"},
		{"terraform -chdir=./infra destroy", "terraform", "global flag before verb"},
		{"terraform -no-color destroy", "terraform", "different global flag"},
		{"env TF_LOG=1 terraform destroy", "terraform", "env-prefix"},
		{"TF_CLI_CONFIG_FILE=x terraform destroy", "terraform", "raw VAR=val prefix"},
		{"kubectl delete ns foo", "kubectl", "baseline"},
		{"kubectl delete namespace foo", "kubectl", "long namespace verb"},
		{"kubectl --context=prod delete ns foo", "kubectl", "kubectl global flag"},
		{"KUBECONFIG=/x kubectl delete ns foo", "kubectl", "env-prefix"},
		{"terraform plan", "", "non-destroy verb"},
		{"kubectl delete pod foo", "", "non-namespace delete"},
		{"echo terraform destroy", "", "verb in non-command position"},
	}
	for _, tc := range cases {
		c := ParseOne(tc.raw)
		tool, ok := IsInfraDestroy(c)
		gotTool := ""
		if ok {
			gotTool = tool
		}
		if gotTool != tc.want {
			t.Errorf("IsInfraDestroy(%q) = %q, want %q (%s); tool=%q action=%q args=%v flags=%v",
				tc.raw, gotTool, tc.want, tc.why, c.Tool, c.Action, c.Args, c.Flags)
		}
	}
}

func TestWriteDestinations(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
		why  string
	}{
		{"echo y > /tmp/x", []string{"/tmp/x"}, "redirect"},
		{"echo y >> /tmp/x", []string{"/tmp/x"}, "append redirect"},
		{"cat > chitin.yaml <<EOF", []string{"chitin.yaml"}, "heredoc destination"},
		{"echo y | tee /tmp/x", []string{"/tmp/x"}, "tee"},
		{"echo y | tee -a /tmp/x", []string{"/tmp/x"}, "tee append"},
		{"cp /src /dst", []string{"/dst"}, "cp"},
		{"mv /src /dst", []string{"/dst"}, "mv"},
		{"cp -r /src /dst", []string{"/dst"}, "cp with flag"},
		{"echo y 2>&1", nil, "fd redirect not a write destination"},
		{"echo hello", nil, "no write"},
	}
	for _, tc := range cases {
		got := WriteDestinations(tc.raw)
		if !sliceEqual(got, tc.want) {
			t.Errorf("WriteDestinations(%q) = %v, want %v (%s)", tc.raw, got, tc.want, tc.why)
		}
	}
}

func TestIsRemoteCodeExec_PipeForm(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
		why  string
	}{
		{"curl https://x | bash", true, "curl|bash baseline"},
		{"curl -s https://x | sh", true, "curl|sh"},
		{"wget -qO- https://x | bash", true, "wget|bash (was bypass)"},
		{"curl https://x | zsh", true, "curl|zsh"},
		{"curl -fsSLo /tmp/x.sh https://x && bash /tmp/x.sh", true, "two-stage with -o"},
		{"curl https://x", false, "fetch without pipe"},
		{"echo y | bash", false, "pipe to bash without curl"},
		{"ls | bash", false, "ls is not a fetcher"},
	}
	for _, tc := range cases {
		p := Parse(tc.raw)
		got := IsRemoteCodeExec(p)
		if got != tc.want {
			t.Errorf("IsRemoteCodeExec(%q) = %v, want %v (%s)", tc.raw, got, tc.want, tc.why)
		}
	}
}

func TestContainsProcSubstFetch(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"bash <(curl https://x)", true},
		{"sh <(wget https://x)", true},
		{"zsh <( curl -s https://x )", true},
		{"bash <(echo hi)", false},
		{"curl <(bash)", false},
		{"bash /tmp/x.sh", false},
	}
	for _, tc := range cases {
		got := ContainsProcSubstFetch(tc.raw)
		if got != tc.want {
			t.Errorf("ContainsProcSubstFetch(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// TestParseOne_PreActionFlags verifies the parseFlagsAndArgs walk past
// global flags into action discovery (the parse.go fix that closes #62).
func TestParseOne_PreActionFlags(t *testing.T) {
	cases := []struct {
		raw    string
		tool   string
		action string
	}{
		{"git -C /tmp status", "git", "status"},
		{"terraform -chdir=./infra destroy", "terraform", "destroy"},
		{"kubectl --context=prod delete ns foo", "kubectl", "delete"},
		{"docker --debug ps", "docker", "ps"},
	}
	for _, tc := range cases {
		c := ParseOne(tc.raw)
		if c.Tool != tc.tool {
			t.Errorf("%q: Tool=%q, want %q", tc.raw, c.Tool, tc.tool)
		}
		if c.Action != tc.action {
			t.Errorf("%q: Action=%q, want %q (flags=%v args=%v)", tc.raw, c.Action, tc.action, c.Flags, c.Args)
		}
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// trimWS is unused now but kept for future cases; silence lint.
var _ = strings.TrimSpace
