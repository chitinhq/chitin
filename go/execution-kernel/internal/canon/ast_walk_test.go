package canon

import "testing"

// TestWalkStmt_Block verifies that { stmt1; stmt2 } flattens into
// sequential segments (OpSeq between inner statements).
func TestWalkStmt_Block(t *testing.T) {
	p := ParseAST("{ echo a; echo b; }")
	if len(p.Segments) != 2 {
		t.Fatalf("expected 2 segments from block, got %d", len(p.Segments))
	}
	if p.Segments[0].Op != OpNone {
		t.Errorf("segments[0].Op = %v, want OpNone", p.Segments[0].Op)
	}
	if p.Segments[1].Op != OpSeq {
		t.Errorf("segments[1].Op = %v, want OpSeq", p.Segments[1].Op)
	}
}

// TestWalkStmt_Subshell verifies that (cmd1; cmd2) produces separate
// segments with OpSeq between them.
func TestWalkStmt_Subshell(t *testing.T) {
	p := ParseAST("(echo a; echo b)")
	if len(p.Segments) != 2 {
		t.Fatalf("expected 2 segments from subshell, got %d", len(p.Segments))
	}
}

// TestWalkStmt_BinaryOr verifies that cmd1 || cmd2 produces OpOr.
func TestWalkStmt_BinaryOr(t *testing.T) {
	p := ParseAST("echo a || echo b")
	if len(p.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(p.Segments))
	}
	if p.Segments[1].Op != OpOr {
		t.Errorf("segments[1].Op = %v, want OpOr", p.Segments[1].Op)
	}
}

// TestWalkStmt_BinaryPipe verifies that cmd1 | cmd2 produces OpPipe.
func TestWalkStmt_BinaryPipe(t *testing.T) {
	p := ParseAST("echo a | grep b")
	if len(p.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(p.Segments))
	}
	if p.Segments[1].Op != OpPipe {
		t.Errorf("segments[1].Op = %v, want OpPipe", p.Segments[1].Op)
	}
}

// TestWalkStmt_BinaryPipeAll verifies that cmd1 |& cmd2 produces OpPipe.
func TestWalkStmt_BinaryPipeAll(t *testing.T) {
	p := ParseAST("echo a |& grep b")
	if len(p.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(p.Segments))
	}
	if p.Segments[1].Op != OpPipe {
		t.Errorf("segments[1].Op = %v, want OpPipe (PipeAll maps to OpPipe)", p.Segments[1].Op)
	}
}

// TestEmitCallExpr_EnvPrefixAssign verifies that VAR=$(cmd) produces a descent
// into the inner command.
func TestEmitCallExpr_EnvPrefixAssign(t *testing.T) {
	p := ParseAST("x=$(rm -rf /tmp)")
	if !pipelineHasRecursiveDelete(p) {
		t.Errorf("env-prefix command substitution must detect rm -rf; segments=%+v", summarizeSegments(p))
	}
}