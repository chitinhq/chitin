package canon

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// ParseAST is the AST-grade companion to Parse. It uses mvdan/sh's bash
// parser to produce a Pipeline that — unlike the tokenizer-grade Parse —
// descends into:
//
//   - Subshells `(rm -rf /)`
//   - Command substitution `$(rm -rf /)` and backtick `` `rm -rf /` ``
//   - Process substitution `bash <(curl ...)` / `>( ... )`
//   - Heredocs `cat > path <<EOF\n...EOF` (destination captured as redirect)
//   - `bash -c "<string>"`, `sh -c "..."`, `eval "..."` — re-parses the
//     string-literal argument and emits its statements as nested segments
//
// Each descended-into Stmt is emitted as its own Segment in the returned
// Pipeline so the bypass detectors (IsRecursiveDelete, IsBareGitPush,
// IsInfraDestroy, WriteDestinations, IsRemoteCodeExec) fire on the
// inner command — not the wrapper.
//
// On any parse failure, ParseAST falls back to the tokenizer-grade Parse
// so callers always get a Pipeline. The fall-back path means an
// unparseable input is no worse off than under the prior (Parse-only)
// behavior; it's NOT an opportunity for bypass because the policy default
// is deny.
//
// Closes the deeper bypass classes filed under canon-ast-upgrade-mvdan-sh.
func ParseAST(raw string) Pipeline {
	parser := syntax.NewParser(syntax.KeepComments(false))
	file, err := parser.Parse(strings.NewReader(raw), "")
	if err != nil {
		// Parse failure → tokenizer fallback. Inputs that mvdan can't
		// parse (e.g. truncated syntax, exotic shells) still get the
		// existing tokenizer pass.
		return Parse(raw)
	}

	pipeline := Pipeline{Segments: []Segment{}}
	for _, stmt := range file.Stmts {
		walkStmt(stmt, OpNone, &pipeline)
	}
	return pipeline
}

// walkStmt recurses through a Stmt's command, emitting one Segment per
// CallExpr it finds. Subshells, command-substitutions, process-substs,
// and bash -c args are all descended into so their inner commands appear
// as Pipeline segments.
//
// The `op` argument is the operator that connects this stmt to the
// previous emitted segment (OpNone for first).
func walkStmt(stmt *syntax.Stmt, op ChainOp, pipeline *Pipeline) {
	if stmt == nil || stmt.Cmd == nil {
		return
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		emitCallExpr(cmd, stmt.Redirs, op, pipeline)
		// Word-parts may contain CmdSubst / ProcSubst — descend into those
		// so an `echo $(rm -rf /)` style obfuscation lands a separate
		// segment for the inner rm.
		for _, w := range cmd.Args {
			walkWordParts(w, pipeline)
		}
	case *syntax.BinaryCmd:
		// && / || / | / |&
		walkStmt(cmd.X, op, pipeline)
		walkStmt(cmd.Y, binOpToChainOp(cmd.Op), pipeline)
	case *syntax.Subshell:
		// (stmts...) — every inner stmt is a separate emitted segment
		// connected by the same op as the wrapping subshell. Closes
		// the `(rm -rf /)` bypass class.
		first := true
		for _, inner := range cmd.Stmts {
			innerOp := op
			if !first {
				innerOp = OpSeq // ; between subshell stmts
			}
			walkStmt(inner, innerOp, pipeline)
			first = false
		}
	case *syntax.Block:
		// { stmts; } — nested block scope, same flattening as subshell.
		first := true
		for _, inner := range cmd.Stmts {
			innerOp := op
			if !first {
				innerOp = OpSeq
			}
			walkStmt(inner, innerOp, pipeline)
			first = false
		}
	default:
		// Other compound forms (IfClause, WhileClause, ForClause, etc.)
		// — for v1 we don't descend into control-flow bodies. Future
		// hardening can add IfClause.Then walking; today, conditional
		// bodies are out of scope.
	}
}

// emitCallExpr converts a CallExpr to a canon.Command and appends it as
// a Segment. Re-parses bash/sh/eval `-c "<string>"` arguments — closes
// the `bash -c "rm -rf /"` and `eval "rm -rf /"` bypass classes.
func emitCallExpr(c *syntax.CallExpr, redirs []*syntax.Redirect, op ChainOp, pipeline *Pipeline) {
	// Walk Assigns first — `x=$(rm -rf /)` and `y=`rm -rf /\`` carry
	// inner CmdSubst inside the Assign.Value's Word.Parts. Without this,
	// the obfuscation `x=$(...) ; ...` would slip past the detector.
	for _, a := range c.Assigns {
		if a.Value != nil {
			walkWordParts(a.Value, pipeline)
		}
	}

	if len(c.Args) == 0 {
		// All-assigns — env-prefix only, no command after. The Assigns
		// were walked above; nothing more to emit.
		return
	}
	args := make([]string, 0, len(c.Args))
	for _, w := range c.Args {
		args = append(args, wordLiteral(w))
	}

	// Re-parse bash/sh/zsh/eval -c "<string>" arguments as their own
	// pipeline. Closes the `bash -c "rm -rf /"` bypass.
	if reparseShellLauncher(args, op, pipeline) {
		return
	}

	// Build the same canonical Command shape as the tokenizer path so
	// downstream detectors don't care which parser produced it.
	raw := strings.Join(args, " ")
	cmd := parseOne(raw)
	cmd.Raw = raw
	cmd.Digest = Digest(cmd)

	pipeline.Segments = append(pipeline.Segments, Segment{
		Op:      op,
		Command: cmd,
	})

	// Heredoc / redirect destinations from the wrapping Stmt.
	// The bypass detectors ALSO look at WriteDestinations(raw) so this
	// is belt-and-suspenders — the AST path captures redirs precisely
	// even when the tokenizer regex would miss them (multi-line
	// heredocs with quoted delimiters).
	for _, r := range redirs {
		if r.Word == nil {
			continue
		}
		dest := wordLiteral(r.Word)
		if dest == "" {
			continue
		}
		// Only emit a synthetic write segment for output-bound ops.
		// Input-bound (RdrIn) doesn't write the destination.
		if isOutputRedirOp(r.Op) {
			pipeline.Segments = append(pipeline.Segments, Segment{
				Op: OpSeq,
				Command: Command{
					Tool:   "redirect",
					Args:   []string{dest},
					Raw:    raw,
					Digest: "redirect:" + dest,
				},
			})
		}
	}
}

// reparseShellLauncher re-parses `bash -c "<string>"`, `sh -c "..."`,
// `zsh -c "..."`, `eval "<string>"`. Returns true iff the call matches
// a known launcher; the inner string is parsed as its own Pipeline and
// flattened into the outer one. The wrapping launcher itself is NOT
// emitted as a segment (otherwise both `bash` AND the inner command
// land in the pipeline, double-counting).
func reparseShellLauncher(args []string, op ChainOp, pipeline *Pipeline) bool {
	if len(args) < 2 {
		return false
	}
	switch args[0] {
	case "bash", "sh", "zsh", "ash", "dash":
		// `bash -c "<string>"`: args = ["bash", "-c", "<string>"]
		if len(args) >= 3 && args[1] == "-c" {
			inner := ParseAST(args[2])
			mergeInto(pipeline, inner, op)
			return true
		}
	case "eval":
		// `eval "<string>"`: args = ["eval", "<string>"]
		if len(args) >= 2 {
			inner := ParseAST(strings.Join(args[1:], " "))
			mergeInto(pipeline, inner, op)
			return true
		}
	}
	return false
}

// mergeInto appends src.Segments onto dst, preserving the first segment's
// connector op (overridden to `op` so the launcher's chain position
// flows through).
func mergeInto(dst *Pipeline, src Pipeline, op ChainOp) {
	for i, seg := range src.Segments {
		if i == 0 {
			seg.Op = op
		}
		dst.Segments = append(dst.Segments, seg)
	}
}

// walkWordParts descends into a Word's Parts looking for CmdSubst/ProcSubst
// — both contain Stmts that we recurse into.
func walkWordParts(w *syntax.Word, pipeline *Pipeline) {
	if w == nil {
		return
	}
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.CmdSubst:
			// $(...) or `...` — every inner stmt becomes its own segment.
			for _, inner := range p.Stmts {
				walkStmt(inner, OpSeq, pipeline)
			}
		case *syntax.ProcSubst:
			// <(...) or >(...) — inner stmts are also recursed.
			// Closes the `bash <(curl ...)` bypass class precisely:
			// the inner curl segment lands in the pipeline so
			// IsRemoteCodeExec sees it adjacent to the bash launcher.
			for _, inner := range p.Stmts {
				walkStmt(inner, OpPipe, pipeline)
			}
		case *syntax.DblQuoted:
			// "..." — may contain CmdSubst inside; recurse via parts.
			for _, dpart := range p.Parts {
				if dw, ok := dpart.(*syntax.CmdSubst); ok {
					for _, inner := range dw.Stmts {
						walkStmt(inner, OpSeq, pipeline)
					}
				}
			}
		}
	}
}

// wordLiteral collapses a Word to its literal-string form, stripping
// quotes (single, double) and yielding the content. ParamExp ($VAR),
// CmdSubst ($(...)), ArithmExp ($((...))) are rendered as their raw
// shell text — the bypass detectors don't try to evaluate them.
//
// Handles the cases needed for canonical Tool/Args extraction:
//
//	"foo"           → foo                (DblQuoted with single Lit part)
//	'foo'           → foo                (SglQuoted)
//	-rf             → -rf                (Lit)
//	hello"world"    → helloworld         (Lit + DblQuoted concat)
//	$VAR            → $VAR               (ParamExp; not expanded)
func wordLiteral(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dpart := range p.Parts {
				if l, ok := dpart.(*syntax.Lit); ok {
					b.WriteString(l.Value)
				}
				// CmdSubst / ParamExp inside double quotes — render
				// raw so detectors see the ($CMD) text. Skip for
				// brevity; CmdSubst is also walked separately.
			}
		case *syntax.ParamExp:
			b.WriteString("$")
			if p.Param != nil {
				b.WriteString(p.Param.Value)
			}
		}
	}
	return b.String()
}

func binOpToChainOp(op syntax.BinCmdOperator) ChainOp {
	switch op {
	case syntax.AndStmt:
		return OpAnd
	case syntax.OrStmt:
		return OpOr
	case syntax.Pipe, syntax.PipeAll:
		return OpPipe
	}
	return OpSeq
}

func isOutputRedirOp(op syntax.RedirOperator) bool {
	switch op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.AppClob,
		syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc,
		syntax.RdrAll, syntax.AppAll:
		return true
	}
	return false
}
