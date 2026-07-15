// Package wasmrun executes WebAssembly modules inside the agent using wazero, a
// pure-Go WASM runtime (no CGO, so it cross-compiles to the whole agent
// architecture matrix). This is what lets modules be written in C: a module is
// compiled once to a single `.wasm` (e.g. with the wasi-sdk clang) and that one
// artifact runs on every device regardless of CPU/OS.
//
// Host ABI — a module imports these from the "armada" module:
//
//	(import "armada" "exec" (func (param i32 i32) (result i32)))  ; ptr,len -> exit code
//	(import "armada" "log"  (func (param i32 i32)))               ; ptr,len
//
// exec runs a shell command on the host and returns its exit code; its combined
// output is appended to the task output. log appends a message to the output.
// Modules compiled against WASI (the usual case for C) also get their stdout and
// stderr captured. The module entrypoint is the standard WASI `_start`.
package wasmrun

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// Result is the outcome of running a module.
type Result struct {
	ExitCode int
	Output   string
}

// Runner executes WASM modules. It is safe to reuse across tasks.
type Runner struct {
	// Timeout bounds a single module run.
	Timeout time.Duration
	// Exec runs a host shell command and returns (exitCode, combinedOutput).
	// Overridable in tests; defaults to the platform shell.
	Exec func(ctx context.Context, command string) (int, string)
}

// New returns a Runner with sane defaults.
func New() *Runner {
	return &Runner{Timeout: 5 * time.Minute, Exec: shellExec}
}

// Run executes wasmBytes with the given args (passed as WASI argv[1:]). It
// captures module stdout/stderr plus anything written via the host log/exec
// functions, and returns the module's exit code.
func (rn *Runner) Run(ctx context.Context, wasmBytes []byte, args []string) (Result, error) {
	timeout := rn.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var out bytes.Buffer

	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	// WASI gives C modules libc, malloc, and stdio.
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	// Host ABI the module calls back into.
	execFn := rn.Exec
	if execFn == nil {
		execFn = shellExec
	}
	_, err := r.NewHostModuleBuilder("armada").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, ptr, ln uint32) uint32 {
			cmd := readString(m, ptr, ln)
			code, o := execFn(ctx, cmd)
			out.WriteString(o)
			return uint32(int32(code))
		}).
		Export("exec").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, ptr, ln uint32) {
			out.WriteString(readString(m, ptr, ln))
		}).
		Export("log").
		Instantiate(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("install host module: %w", err)
	}

	cfg := wazero.NewModuleConfig().
		WithStdout(&out).
		WithStderr(&out).
		WithArgs(append([]string{"module"}, args...)...)

	_, err = r.InstantiateWithConfig(ctx, wasmBytes, cfg)
	// A WASI module that calls proc_exit (Go/clang do at the end of main)
	// surfaces as sys.ExitError even on success; treat its code as the result.
	if err != nil {
		var exitErr *sys.ExitError
		if asExit(err, &exitErr) {
			return Result{ExitCode: int(exitErr.ExitCode()), Output: out.String()}, nil
		}
		return Result{Output: out.String()}, fmt.Errorf("run module: %w", err)
	}
	return Result{ExitCode: 0, Output: out.String()}, nil
}

// readString copies a (ptr,len) region out of the module's linear memory.
func readString(m api.Module, ptr, ln uint32) string {
	if ln == 0 {
		return ""
	}
	buf, ok := m.Memory().Read(ptr, ln)
	if !ok {
		return ""
	}
	return string(buf)
}

// shellExec runs a command through the platform shell, capturing combined
// output. There is intentionally no sandboxing here yet — that is deferred to
// production hardening.
func shellExec(ctx context.Context, command string) (int, string) {
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/c", command)
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", command)
	}
	b, err := c.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		code = -1
		b = append(b, []byte("\n"+err.Error())...)
	}
	return code, string(b)
}

// asExit unwraps err into a *sys.ExitError if present.
func asExit(err error, target **sys.ExitError) bool {
	for e := err; e != nil; {
		if ex, ok := e.(*sys.ExitError); ok {
			*target = ex
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
