// Package nativerun executes a natively-compiled module binary on the device.
// The control plane serves the build matching the device's OS/CPU (the agent
// requests its own GOOS/GOARCH); this package writes it to a temp file, marks it
// executable, runs it, and captures its output.
//
// Native modules run with the agent's full privileges and no sandbox — they are
// the fastest, most capable runtime and the primary path for C modules. (The
// Python runtime trades native access for needing an interpreter on the device.)
package nativerun

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Result mirrors the other runners so the task loop can treat runtimes alike.
type Result struct {
	ExitCode int
	Output   string
}

// Runner executes native module binaries.
type Runner struct {
	Timeout time.Duration
}

// New returns a Runner with a default timeout.
func New() *Runner { return &Runner{Timeout: 5 * time.Minute} }

// Run writes bin to a temp file, makes it executable, and runs it with args,
// capturing combined output.
func (rn *Runner) Run(ctx context.Context, bin []byte, args []string) (Result, error) {
	timeout := rn.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "armada-native-*")
	if err != nil {
		return Result{}, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	name := "module"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	// 0755: the file must be executable by the agent's user.
	if err := os.WriteFile(path, bin, 0o755); err != nil {
		return Result{}, fmt.Errorf("write binary: %w", err)
	}

	cmd := exec.CommandContext(ctx, path, args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		return Result{Output: string(out)}, fmt.Errorf("run native module: %w", err)
	}
	return Result{ExitCode: code, Output: string(out)}, nil
}
