package canon

import (
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func TestBinOpToChainOp(t *testing.T) {
	tests := []struct {
		name string
		op   syntax.BinCmdOperator
		want ChainOp
	}{
		{"AndStmt", syntax.AndStmt, OpAnd},
		{"OrStmt", syntax.OrStmt, OpOr},
		{"Pipe", syntax.Pipe, OpPipe},
		{"PipeAll", syntax.PipeAll, OpPipe},
	}
	for _, tc := range tests {
		got := binOpToChainOp(tc.op)
		if got != tc.want {
			t.Errorf("binOpToChainOp(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
	// Unknown op should return OpSeq
	got := binOpToChainOp(syntax.BinCmdOperator(99))
	if got != OpSeq {
		t.Errorf("binOpToChainOp(unknown) = %v, want OpSeq", got)
	}
}

func TestIsOutputRedirOp(t *testing.T) {
	if !isOutputRedirOp(syntax.RdrOut) {
		t.Error("RdrOut should be output redirect")
	}
	if !isOutputRedirOp(syntax.AppOut) {
		t.Error("AppOut should be output redirect")
	}
	if isOutputRedirOp(syntax.RdrIn) {
		t.Error("RdrIn should not be output redirect")
	}
	if !isOutputRedirOp(syntax.RdrAll) {
		t.Error("RdrAll should be output redirect")
	}
}

func TestUnquote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`'hello'`, "hello"},
		{`"world"`, "world"},
		{"plain", "plain"},
		{"", ""},
		{"x", "x"},
		{`'a`, `'a`},
	}
	for _, tc := range tests {
		got := unquote(tc.input)
		if got != tc.want {
			t.Errorf("unquote(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestWordLiteral_Nil(t *testing.T) {
	if got := wordLiteral(nil); got != "" {
		t.Errorf("wordLiteral(nil) = %q, want empty", got)
	}
}

func TestWordLiteral_Lit(t *testing.T) {
	prog, err := syntax.NewParser().Parse(strings.NewReader("hello world"), "")
	if err != nil {
		t.Skipf("parse failed: %v", err)
	}
	call := prog.Stmts[0].Cmd.(*syntax.CallExpr)
	got := wordLiteral(call.Args[0])
	if got != "hello" {
		t.Errorf("wordLiteral(first arg) = %q, want 'hello'", got)
	}
}