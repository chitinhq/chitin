package activities

import (
	"bufio"
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Task is one entry parsed from a spec's tasks.md.
type Task struct {
	ID       string // e.g. "T001"
	Num      int    // numeric form, for deterministic ordering
	Phase    int    // the "## Phase N" header it sits under (0 if none)
	Parallel bool   // carried the [P] marker
	Desc     string
}

var (
	phaseRe = regexp.MustCompile(`^##\s+Phase\s+(\d+)`)
	taskRe  = regexp.MustCompile(`^- \[[ xX]\]\s+T(\d+)\s+(.*)$`)
)

// ParseTasks reads a spec's tasks.md and returns its tasks in file order.
// This is file I/O — hence an activity, never workflow code. The workflow
// derives the *sequence* deterministically from what this returns.
func ParseTasks(_ context.Context, tasksPath string) ([]Task, error) {
	f, err := os.Open(tasksPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Task
	phase := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if m := phaseRe.FindStringSubmatch(line); m != nil {
			phase, _ = strconv.Atoi(m[1])
			continue
		}
		if m := taskRe.FindStringSubmatch(line); m != nil {
			num, _ := strconv.Atoi(m[1])
			rest := m[2]
			out = append(out, Task{
				ID:       "T" + m[1],
				Num:      num,
				Phase:    phase,
				Parallel: strings.HasPrefix(rest, "[P]"),
				Desc:     rest,
			})
		}
	}
	return out, sc.Err()
}
