package replay

import "github.com/chitinhq/chitin/go/execution-kernel/internal/gov"

func loadMergedPolicy(cwd string) (gov.Policy, []string, error) {
	return gov.LoadWithInheritance(cwd)
}
