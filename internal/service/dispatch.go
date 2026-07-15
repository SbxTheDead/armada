package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/store"
)

// RunModule creates a job that runs a module on every system matching the
// filter, fanning out one task per device. It returns the job and the number of
// devices targeted. The module itself is fetched and executed by each agent.
func (f *Fleet) RunModule(ctx context.Context, tenantID, module string, args []string, filter store.SystemFilter) (*domain.Job, error) {
	if strings.TrimSpace(module) == "" {
		return nil, fmt.Errorf("%w: module is required", domain.ErrValidation)
	}
	systems, err := f.systems.List(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	// Only dispatch to enrolled (active) devices.
	targets := systems[:0]
	for _, s := range systems {
		if s.Lifecycle == domain.LifecycleEnrolled {
			targets = append(targets, s)
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("%w: no enrolled devices match the target", domain.ErrValidation)
	}

	now := f.now().UTC()
	job := &domain.Job{
		ID:        f.id(),
		TenantID:  tenantID,
		Module:    module,
		Args:      args,
		Selector:  describeFilter(filter, len(targets)),
		Total:     len(targets),
		CreatedAt: now,
	}
	if err := f.work.CreateJob(ctx, job); err != nil {
		return nil, err
	}
	for _, s := range targets {
		task := &domain.Task{
			ID:        f.id(),
			JobID:     job.ID,
			TenantID:  tenantID,
			SystemID:  s.ID,
			Module:    module,
			Args:      args,
			Status:    domain.TaskPending,
			CreatedAt: now,
		}
		if err := f.work.CreateTask(ctx, task); err != nil {
			return nil, err
		}
	}
	return job, nil
}

// JobStatus returns a job and its per-device tasks.
func (f *Fleet) JobStatus(ctx context.Context, tenantID, jobID string) (*domain.Job, []*domain.Task, error) {
	job, err := f.work.GetJob(ctx, tenantID, jobID)
	if err != nil {
		return nil, nil, err
	}
	tasks, err := f.work.ListTasksByJob(ctx, tenantID, jobID)
	if err != nil {
		return nil, nil, err
	}
	return job, tasks, nil
}

// ListJobs returns a tenant's jobs, newest first.
func (f *Fleet) ListJobs(ctx context.Context, tenantID string) ([]*domain.Job, error) {
	return f.work.ListJobs(ctx, tenantID)
}

// ClaimTasks is the agent poll: it returns (and marks dispatched) the pending
// tasks for the authenticated system.
func (f *Fleet) ClaimTasks(ctx context.Context, tenantID, systemID string) ([]*domain.Task, error) {
	return f.work.ClaimPendingForSystem(ctx, tenantID, systemID)
}

// GetTaskForAgent fetches a task and verifies it belongs to the calling agent's
// system, so an agent cannot post results for another device's task.
func (f *Fleet) GetTaskForAgent(ctx context.Context, tenantID, systemID, taskID string) (*domain.Task, error) {
	task, err := f.work.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return nil, err
	}
	if task.SystemID != systemID {
		return nil, domain.ErrNotFound
	}
	return task, nil
}

// CompleteTask records an agent's result for one task. success reflects a module
// exit code of 0; errMsg carries an agent-side failure (download/instantiation).
func (f *Fleet) CompleteTask(ctx context.Context, tenantID, taskID string, exitCode int, output, errMsg string) error {
	task, err := f.work.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return err
	}
	now := f.now().UTC()
	task.ExitCode = exitCode
	task.Output = truncate(output, 64*1024)
	task.Error = errMsg
	task.FinishedAt = &now
	if errMsg == "" && exitCode == 0 {
		task.Status = domain.TaskSucceeded
	} else {
		task.Status = domain.TaskFailed
	}
	return f.work.UpdateTask(ctx, task)
}

func describeFilter(f store.SystemFilter, n int) string {
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	add("project", f.Project)
	add("region", f.Region)
	add("environment", f.Environment)
	add("provider", f.Provider)
	add("tag", f.Tag)
	scope := "all devices"
	if len(parts) > 0 {
		scope = strings.Join(parts, ",")
	}
	return fmt.Sprintf("%s (%d)", scope, n)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...[truncated]"
}
