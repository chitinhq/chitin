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
		// Unknown/default falls to OpSeq
		{syntax.BinCmdOperator(999), OpSeq},
	}
	for _, tc := range tests {
		got := binOpToChainOp(tc.op)
		if got != tc.want {
			t.Errorf("binOpToChainOp(%v) = %v, want %v", tc.op, got, tc.want)
		}
	}
}

func TestIsOutputRedirOp(t *testing.T) {
	tests := []struct {
		op   syntax.RedirOperator
		want bool
	}{
		{syntax.RdrOut, true},
		{syntax.AppOut, true},
		{syntax.RdrClob, true},
		{syntax.AppClob, true},
		{syntax.Hdoc, true},
		{syntax.DashHdoc, true},
		{syntax.WordHdoc, true},
		{syntax.RdrAll, true},
		{syntax.AppAll, true},
		// Not output redirects
		{syntax.RdrIn, false},
	}
	for _, tc := range tests {
		got := isOutputRedirOp(tc.op)
		if got != tc.want {
			t.Errorf("isOutputRedirOp(%v) = %v, want %v", tc.op, got, tc.want)
		}
	}
}

func TestUnquote(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`"hello"`, "hello"},
		{`'hello'`, "hello"},
		{`hello`, "hello"},
		{`"a`, `"a`},
		{`a"`, `a"`},
		{"", ""},
		{`""`, ""},
		{`''`, ""},
	}
	for _, tc := range tests {
		got := unquote(tc.in)
		if got != tc.want {
			t.Errorf("unquote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWordLiteral(t *testing.T) {
	t.Run("nil word", func(t *testing.T) {
		if got := wordLiteral(nil); got != "" {
			t.Errorf("wordLiteral(nil) = %q, want empty", got)
		}
	})
	t.Run("lit word", func(t *testing.T) {
		w := &syntax.Word{Parts: []syntax.WordPart{&syntax.Lit{Value: "hello"}}}
		if got := wordLiteral(w); got != "hello" {
			t.Errorf("wordLiteral(lit) = %q, want hello", got)
		}
	})
	t.Run("single-quoted word", func(t *testing.T) {
		w := &syntax.Word{Parts: []syntax.WordPart{&syntax.SglQuoted{Value: "world"}}}
		if got := wordLiteral(w); got != "world" {
			t.Errorf("wordLiteral(sgl) = %q, want world", got)
		}
	})
	t.Run("param expansion", func(t *testing.T) {
		w := &syntax.Word{Parts: []syntax.WordPart{&syntax.ParamExp{Param: &syntax.Lit{Value: "HOME"}}}}
		if got := wordLiteral(w); got != "$HOME" {
			t.Errorf("wordLiteral(param) = %q, want $HOME", got)
		}
	})
}

func TestWalkStmt_NilCmd(t *testing.T) {
	// nil stmt and nil cmd should not panic
	var pipeline Pipeline
	walkStmt(nil, OpSeq, &pipeline)
	if len(pipeline.Segments) != 0 {
		t.Errorf("walkStmt(nil) should produce no segments, got %d", len(pipeline.Segments))
	}
	stmt := &syntax.Stmt{} // Cmd is nil
	walkStmt(stmt, OpSeq, &pipeline)
	if len(pipeline.Segments) != 0 {
		t.Errorf("walkStmt(nil Cmd) should produce no segments, got %d", len(pipeline.Segments))
	}
}