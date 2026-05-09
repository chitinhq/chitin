package canon

import (
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func TestBinOpToChainOp(t *testing.T) {
	tests := []struct {
		op   syntax.BinCmdOperator
		want ChainOp
	}{
		{syntax.AndStmt, OpAnd},
		{syntax.OrStmt, OpOr},
		{syntax.Pipe, OpPipe},
		{syntax.PipeAll, OpPipe},
	}
	for _, tt := range tests {
		got := binOpToChainOp(tt.op)
		if got != tt.want {
			t.Errorf("binOpToChainOp(%v) = %v, want %v", tt.op, got, tt.want)
		}
	}
	// Unknown/zero operator returns OpSeq
	got := binOpToChainOp(syntax.BinCmdOperator(0))
	if got != OpSeq {
		t.Errorf("binOpToChainOp(0) = %v, want OpSeq", got)
	}
}

func TestIsOutputRedirOp(t *testing.T) {
	outputOps := []syntax.RedirOperator{
		syntax.RdrOut,
		syntax.AppOut,
		syntax.RdrClob,
		syntax.AppClob,
		syntax.Hdoc,
		syntax.DashHdoc,
		syntax.WordHdoc,
		syntax.RdrAll,
		syntax.AppAll,
	}
	for _, op := range outputOps {
		if !isOutputRedirOp(op) {
			t.Errorf("isOutputRedirOp(%v) = false, want true", op)
		}
	}

	// Input redirections should be false
	inputOps := []syntax.RedirOperator{
		syntax.RdrIn,
		syntax.RdrInOut,
	}
	for _, op := range inputOps {
		if isOutputRedirOp(op) {
			t.Errorf("isOutputRedirOp(%v) = true, want false", op)
		}
	}
}

func TestWalkStmtNilCmd(t *testing.T) {
	// nil stmt should not panic, just return
	p := Pipeline{}
	walkStmt(nil, OpNone, &p)
	if len(p.Segments) != 0 {
		t.Errorf("walkStmt(nil) should produce 0 segments, got %d", len(p.Segments))
	}
}

func TestWalkStmtNilCmdField(t *testing.T) {
	// syntax.Stmt with nil Cmd — should not produce segments
	p := Pipeline{}
	stmt := &syntax.Stmt{Cmd: nil}
	walkStmt(stmt, OpNone, &p)
	if len(p.Segments) != 0 {
		t.Errorf("walkStmt(stmt with nil Cmd) should produce 0 segments, got %d", len(p.Segments))
	}
}

func TestWordLiteral(t *testing.T) {
	tests := []struct {
		name string
		word *syntax.Word
		want string
	}{
		{"lit", &syntax.Word{Parts: []syntax.WordPart{&syntax.Lit{Value: "hello"}}}, "hello"},
		{"empty", &syntax.Word{Parts: nil}, ""},
		{"concat", &syntax.Word{Parts: []syntax.WordPart{
			&syntax.Lit{Value: "a"},
			&syntax.Lit{Value: "b"},
		}}, "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wordLiteral(tt.word)
			if got != tt.want {
				t.Errorf("wordLiteral() = %q, want %q", got, tt.want)
			}
		})
	}
}