package domain

import "time"

// A Job is an operator-issued request to run a module across a set of devices.
// It fans out into one Task per matched system. The Job itself is a lightweight
// grouping; live progress is derived from its Tasks.
type Job struct {
	ID       string   `json:"id"`
	TenantID string   `json:"tenant_id"`
	Module   string   `json:"module"`         // module name to execute on each device
	Args     []string `json:"args,omitempty"` // arguments passed to the module
	Selector string   `json:"selector"`       // human description of the target filter
	Total    int      `json:"total"`          // number of tasks fanned out

	CreatedAt time.Time `json:"created_at"`
}

// TaskStatus is the lifecycle of a single device's execution of a job.
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"    // created, not yet picked up by the agent
	TaskDispatched TaskStatus = "dispatched" // handed to the agent, awaiting a result
	TaskSucceeded  TaskStatus = "succeeded"  // ran, module exit code 0
	TaskFailed     TaskStatus = "failed"     // module exit non-zero, or agent-side error
)

// Task is the unit an agent executes: run Module (with Args) on one System and
// report the outcome. Output holds captured module stdout/stderr (truncated);
// Error holds an agent-side failure (module download/instantiation) distinct
// from a non-zero module exit.
type Task struct {
	ID       string     `json:"id"`
	JobID    string     `json:"job_id"`
	TenantID string     `json:"tenant_id"`
	SystemID string     `json:"system_id"`
	Module   string     `json:"module"`
	Args     []string   `json:"args,omitempty"`
	Status   TaskStatus `json:"status"`
	ExitCode int        `json:"exit_code"`
	Output   string     `json:"output,omitempty"`
	Error    string     `json:"error,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Terminal reports whether the task has reached a final state.
func (t Task) Terminal() bool {
	return t.Status == TaskSucceeded || t.Status == TaskFailed
}
