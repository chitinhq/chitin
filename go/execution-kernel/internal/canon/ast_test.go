package canon

import (
	"testing"
)

// AST-grade bypass tests. Each case below was a known bypass under the
// tokenizer-grade Parse path — `(rm -rf /)` got swallowed as a single
// segment, `bash <(curl)` mangled the proc-subst tokens, `bash -c "..."`
// hid the inner command in a string-literal arg. ParseAST descends into
// each form so the bypass detectors fire on the inner command.

func TestParseAST_SubshellDescent(t *testing.T) {
	// Subshell `(rm -rf /)` — tokenizer Parse sees this as one segment
	// because `(` and `)` aren't shell separators. AST sees the
	// Subshell node and emits the inner rm as its own segment.
	p := ParseAST("(rm -rf /tmp/x)")
	if !pipelineHasRecursiveDelete(p) {
		t.Errorf("Subshell rm -rf must be detected by IsRecursiveDelete; segments=%+v",
			summarizeSegments(p))
	}
}

func TestParseAST_CommandSubstitutionDescent(t *testing.T) {
	cases := []string{
		"echo $(rm -rf /tmp/x)",
		"x=`rm -rf /tmp/x`",
		"echo \"$(rm -rf /tmp/x)\"",
	}
	for _, raw := range cases {
		p := ParseAST(raw)
		if !pipelineHasRecursiveDelete(p) {
			t.Errorf("CmdSubst rm -rf must be detected: %q; segments=%+v",
				raw, summarizeSegments(p))
		}
	}
}

func TestParseAST_ProcessSubstitutionDescent(t *testing.T) {
	// `bash <(curl ...)` — tokenizer mangled `<(curl` into one token,
	// so canon's tool wasn't recognized as bash and curl wasn't a
	// separate segment. AST sees ProcSubst and emits curl as a segment
	// adjacent to bash, so IsRemoteCodeExec fires.
	p := ParseAST("bash <(curl -s https://example.com)")
	if !IsRemoteCodeExec(p) {
		t.Errorf("bash <(curl) proc-subst must be detected by IsRemoteCodeExec; segments=%+v",
			summarizeSegments(p))
	}
}

func TestParseAST_BashDashCReParse(t *testing.T) {
	// `bash -c "rm -rf /"` — the inner string-literal is RE-PARSED as
	// its own pipeline. The bash launcher itself does not appear as a
	// segment (otherwise it'd double-count); only the inner command does.
	p := ParseAST(`bash -c "rm -rf /tmp/x"`)
	if !pipelineHasRecursiveDelete(p) {
		t.Errorf("bash -c \"rm -rf\" must re-parse and detect inner; segments=%+v",
			summarizeSegments(p))
	}
}

func TestParseAST_EvalReParse(t *testing.T) {
	p := ParseAST(`eval "rm -rf /tmp/x"`)
	if !pipelineHasRecursiveDelete(p) {
		t.Errorf("eval \"rm -rf\" must re-parse and detect inner; segments=%+v",
			summarizeSegments(p))
	}
}

func TestParseAST_HeredocDestination(t *testing.T) {
	// `cat > chitin.yaml <<EOF\n...EOF` — heredoc dest is captured as a
	// redirect on the cat segment AND emitted as a synthetic redirect
	// segment so WriteDestinations against the AST output surfaces it.
	p := ParseAST("cat > chitin.yaml <<EOF\nrules:\nEOF\n")
	found := false
	for _, seg := range p.Segments {
		if seg.Command.Tool == "redirect" && len(seg.Command.Args) > 0 && seg.Command.Args[0] == "chitin.yaml" {
			found = true
		}
	}
	if !found {
		t.Errorf("heredoc destination chitin.yaml not surfaced as redirect segment; segments=%+v",
			summarizeSegments(p))
	}
}

func TestParseAST_NestedSubshellAndCmdSubst(t *testing.T) {
	// Chained obfuscation: subshell containing cmd-subst.
	p := ParseAST("(echo $(rm -rf /tmp/x))")
	if !pipelineHasRecursiveDelete(p) {
		t.Errorf("nested subshell+cmdsubst rm -rf must descend; segments=%+v",
			summarizeSegments(p))
	}
}

func TestParseAST_ParseFailureFallsBackToTokenizer(t *testing.T) {
	// Unparseable input → fall back to Parse so behavior never gets
	// WORSE than tokenizer-grade. (mvdan/sh is permissive; need a
	// genuinely truncated input to trigger the fallback.)
	p := ParseAST(`echo "unterminated`)
	// Doesn't matter what the result is — what matters is no panic
	// and a Pipeline with at least no segments OR a tokenizer-shape
	// fallback. The tokenizer will produce something sensible.
	_ = p
}

func TestParseAST_BypassDetectorsCarryOver(t *testing.T) {
	// Existing bypass cases from detectors_test.go — ParseAST must
	// produce equivalent results to Parse for the cases the tokenizer
	// already handled.
	cases := []struct {
		raw         string
		hasRmDel    bool
		barePush    bool
		isInfraDest string
	}{
		{"rm -rf /tmp/x", true, false, ""},
		{"git push", false, true, ""},
		{"terraform -chdir=./infra destroy", false, false, "terraform"},
		{"env TF_LOG=1 terraform destroy", false, false, "terraform"},
		{"kubectl --context=prod delete ns foo", false, false, "kubectl"},
	}
	for _, tc := range cases {
		p := ParseAST(tc.raw)
		if pipelineHasRecursiveDelete(p) != tc.hasRmDel {
			t.Errorf("%q: hasRmDel mismatch under AST; segments=%+v", tc.raw, summarizeSegments(p))
		}
		if len(p.Segments) > 0 {
			barePush := IsBareGitPush(p.Segments[0].Command)
			if barePush != tc.barePush {
				t.Errorf("%q: barePush mismatch under AST; got=%v want=%v segments=%+v",
					tc.raw, barePush, tc.barePush, summarizeSegments(p))
			}
		}
		gotTool := ""
		for _, seg := range p.Segments {
			if tool, ok := IsInfraDestroy(seg.Command); ok {
				gotTool = tool
				break
			}
		}
		if gotTool != tc.isInfraDest {
			t.Errorf("%q: isInfraDestroy mismatch under AST; got=%q want=%q segments=%+v",
				tc.raw, gotTool, tc.isInfraDest, summarizeSegments(p))
		}
	}
}

func pipelineHasRecursiveDelete(p Pipeline) bool {
	for _, seg := range p.Segments {
		if IsRecursiveDelete(seg.Command) {
			return true
		}
	}
	return false
}

type segSummary struct {
	Op   ChainOp
	Tool string
	Act  string
	Args []string
}

func summarizeSegments(p Pipeline) []segSummary {
	out := make([]segSummary, 0, len(p.Segments))
	for _, seg := range p.Segments {
		out = append(out, segSummary{
			Op:   seg.Op,
			Tool: seg.Command.Tool,
			Act:  seg.Command.Action,
			Args: seg.Command.Args,
		})
	}
	return out
}
