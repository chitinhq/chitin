package workflows

import (
	"sort"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
)

// Wave is a set of tasks the orchestrator may run concurrently.
type Wave struct {
	Phase    int      `json:"phase"`
	Parallel bool     `json:"parallel"`
	TaskIDs  []string `json:"task_ids"`
}

// Plan is the deterministic execution sequence derived from a task graph.
type Plan struct {
	TasksPath string `json:"tasks_path"`
	TaskCount int    `json:"task_count"`
	Waves     []Wave `json:"waves"`
}

// SequenceWorkflow deterministically derives an execution plan from a spec's
// tasks.md. This is the orchestrator's core job under the 2026-05-20 refocus:
// the work sequence is *computed* from the task graph — mathematically,
// reproducibly — not pulled from a kanban board.
//
// Determinism: phase order is the dependency order; within a phase the [P]
// tasks form one parallel wave and the rest run sequentially; the only
// tie-breaker is the numeric task id. No clocks, no randomness, no map
// iteration order — the same tasks.md always yields the same Plan, so the
// workflow replays identically.
func SequenceWorkflow(ctx workflow.Context, tasksPath string) (Plan, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	})

	var tasks []activities.Task
	if err := workflow.ExecuteActivity(ctx, activities.ParseTasks, tasksPath).Get(ctx, &tasks); err != nil {
		return Plan{}, err
	}

	plan := Plan{TasksPath: tasksPath, TaskCount: len(tasks)}

	// Group by phase. The map is built by iterating the (ordered) tasks slice
	// and is used only for lookup — never iterated for order.
	byPhase := map[int][]activities.Task{}
	var phaseOrder []int
	for _, t := range tasks {
		if _, seen := byPhase[t.Phase]; !seen {
			phaseOrder = append(phaseOrder, t.Phase)
		}
		byPhase[t.Phase] = append(byPhase[t.Phase], t)
	}
	sort.Ints(phaseOrder)

	for _, p := range phaseOrder {
		ph := byPhase[p]
		sort.Slice(ph, func(i, j int) bool { return ph[i].Num < ph[j].Num })

		var parallel, sequential []string
		for _, t := range ph {
			if t.Parallel {
				parallel = append(parallel, t.ID)
			} else {
				sequential = append(sequential, t.ID)
			}
		}
		if len(parallel) > 0 {
			plan.Waves = append(plan.Waves, Wave{Phase: p, Parallel: true, TaskIDs: parallel})
		}
		for _, id := range sequential {
			plan.Waves = append(plan.Waves, Wave{Phase: p, Parallel: false, TaskIDs: []string{id}})
		}
	}
	return plan, nil
}
