package agent

import (
	"context"
	"log/slog"

	"github.com/SbxTheDead/armada/internal/agent/wasmrun"
	"github.com/SbxTheDead/armada/internal/domain"
)

// pollAndRunTasks claims any pending tasks for this device and executes each
// one: download the module WASM, run it in the sandbox, report the result.
func pollAndRunTasks(ctx context.Context, client *Client, runner *wasmrun.Runner, log *slog.Logger) {
	tasks, err := client.ClaimTasks(ctx)
	if err != nil {
		log.Warn("task poll failed", "err", err)
		return
	}
	for _, task := range tasks {
		runOneTask(ctx, client, runner, log, task)
	}
}

func runOneTask(ctx context.Context, client *Client, runner *wasmrun.Runner, log *slog.Logger, task domain.Task) {
	log.Info("running task", "task", task.ID, "module", task.Module)

	wasmBytes, err := client.FetchModule(ctx, task.Module)
	if err != nil {
		// Agent-side failure (module missing/unreachable): report as an error,
		// distinct from a non-zero module exit.
		report(ctx, client, log, task.ID, 0, "", "fetch module: "+err.Error())
		return
	}

	res, err := runner.Run(ctx, wasmBytes, task.Args)
	if err != nil {
		report(ctx, client, log, task.ID, 0, res.Output, "run module: "+err.Error())
		return
	}
	report(ctx, client, log, task.ID, res.ExitCode, res.Output, "")
}

func report(ctx context.Context, client *Client, log *slog.Logger, taskID string, exit int, output, errMsg string) {
	if err := client.CompleteTask(ctx, taskID, exit, output, errMsg); err != nil {
		log.Warn("failed to report task result", "task", taskID, "err", err)
	}
}
