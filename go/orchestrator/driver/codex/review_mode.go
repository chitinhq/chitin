package codex

import "github.com/chitinhq/chitin/go/orchestrator/driver"

// reviewArgvFor builds the argv passed to the codex CLI for a review-mode
// invocation (spec 110 FR-001). The mandatory --skip-git-repo-check flag lets
// codex exec run inside the work unit's worktree without tripping the CLI's
// trusted-directory safety check, which otherwise fails the subprocess in
// ~130ms before any model call (spec 110 §Why).
//
// Non-review-mode invocations build argv inline in Driver.Invoke and MUST NOT
// pass this flag (FR-002): the trust check is the expected safety behaviour on
// local-driver implementation work.
func reviewArgvFor(wu driver.WorkUnit, model string) []string {
	return []string{"exec", "--skip-git-repo-check", "--model", model, promptFor(wu)}
}
