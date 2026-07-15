// Package pyrun executes Python module scripts on the device using its installed
// Python interpreter. Python modules run with the agent's full privileges (no
// sandbox) and require Python to be present — if none is found the task fails
// with a clear message. For devices without Python, use a native module.
package pyrun

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Result is the outcome of running a Python module.
type Result struct {
	ExitCode int
	Output   string
}

// Runner executes Python scripts.
type Runner struct {
	// Timeout bounds a single script run.
	Timeout time.Duration
	// Interpreter overrides auto-discovery when set (e.g. from ARMADA_PYTHON).
	Interpreter string
	// candidates is the discovery order; overridable in tests.
	candidates []string
}

// New returns a Runner with default discovery order. If interpreter is
// non-empty it is used verbatim.
func New(interpreter string) *Runner {
	return &Runner{
		Timeout:     5 * time.Minute,
		Interpreter: interpreter,
		// python3/python may be Windows Store stubs; py (the launcher) and a
		// concrete python3 are tried too. discover() verifies each actually runs.
		candidates: []string{"python3", "python", "py"},
	}
}

// Run writes the script to a temp file and executes it with the discovered
// interpreter, passing args as script arguments and capturing combined output.
func (rn *Runner) Run(ctx context.Context, script []byte, args []string) (Result, error) {
	interp, err := rn.discover(ctx)
	if err != nil {
		return Result{}, err
	}

	timeout := rn.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "armada-py-*")
	if err != nil {
		return Result{}, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	scriptPath := filepath.Join(dir, "module.py")
	if err := os.WriteFile(scriptPath, script, 0o600); err != nil {
		return Result{}, fmt.Errorf("write script: %w", err)
	}

	cmdArgs := append([]string{scriptPath}, args...)
	cmd := exec.CommandContext(ctx, interp, cmdArgs...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		return Result{Output: string(out)}, fmt.Errorf("run python: %w", err)
	}
	return Result{ExitCode: code, Output: string(out)}, nil
}

// discover returns the first interpreter that reports a Python version. This
// skips the Windows "python3.exe" App Execution Alias stub, which exits with an
// error instead of a version.
func (rn *Runner) discover(ctx context.Context) (string, error) {
	if rn.Interpreter != "" {
		return rn.Interpreter, nil
	}
	for _, cand := range rn.candidates {
		vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		out, err := exec.CommandContext(vctx, cand, "--version").CombinedOutput()
		cancel()
		if err == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "Python") {
			return cand, nil
		}
	}
	return "", fmt.Errorf("no Python interpreter found on this device (set ARMADA_PYTHON, or install python3)")
}
