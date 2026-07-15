package agent

import (
	"context"
	"log/slog"
	"runtime"

	"github.com/SbxTheDead/armada/internal/agent/nativerun"
	"github.com/SbxTheDead/armada/internal/agent/pyrun"
	"github.com/SbxTheDead/armada/internal/agent/wasmrun"
	"github.com/SbxTheDead/armada/internal/domain"
)

// runners bundles the per-runtime executors so the task loop can dispatch by
// the module's declared runtime.
type runners struct {
	wasm   *wasmrun.Runner
	py     *pyrun.Runner
	native *nativerun.Runner
}

// pollAndRunTasks claims any pending tasks for this device and executes each
// one: download the module, run it with the matching runtime, report the result.
func pollAndRunTasks(ctx context.Context, client *Client, rs runners, log *slog.Logger) {
	tasks, err := client.ClaimTasks(ctx)
	if err != nil {
		log.Warn("task poll failed", "err", err)
		return
	}
	for _, task := range tasks {
		runOneTask(ctx, client, rs, log, task)
	}
}

func runOneTask(ctx context.Context, client *Client, rs runners, log *slog.Logger, task domain.Task) {
	log.Info("running task", "task", task.ID, "module", task.Module, "runtime", task.Runtime)

	// Fetch the module — native fetches the build for this device's OS/arch.
	var (
		body []byte
		err  error
	)
	if task.Runtime == domain.RuntimeNative {
		body, err = client.FetchNativeBinary(ctx, task.Module, runtime.GOOS, runtime.GOARCH)
	} else {
		body, err = client.FetchModule(ctx, task.Module)
	}
	if err != nil {
		// Agent-side failure (module missing/unreachable): report as an error,
		// distinct from a non-zero module exit.
		report(ctx, client, log, task.ID, 0, "", "fetch module: "+err.Error())
		return
	}

	var (
		exit   int
		output string
		runErr error
	)
	switch task.Runtime {
	case domain.RuntimePython:
		res, e := rs.py.Run(ctx, body, task.Args)
		exit, output, runErr = res.ExitCode, res.Output, e
	case domain.RuntimeNative:
		res, e := rs.native.Run(ctx, body, task.Args)
		exit, output, runErr = res.ExitCode, res.Output, e
	default: // wasm is the default runtime
		res, e := rs.wasm.Run(ctx, body, task.Args)
		exit, output, runErr = res.ExitCode, res.Output, e
	}
	if runErr != nil {
		report(ctx, client, log, task.ID, 0, output, "run module: "+runErr.Error())
		return
	}
	report(ctx, client, log, task.ID, exit, output, "")
}

func report(ctx context.Context, client *Client, log *slog.Logger, taskID string, exit int, output, errMsg string) {
	if err := client.CompleteTask(ctx, taskID, exit, output, errMsg); err != nil {
		log.Warn("failed to report task result", "task", taskID, "err", err)
	}
}
