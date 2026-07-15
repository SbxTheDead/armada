package wasmrun_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SbxTheDead/armada/internal/agent/wasmrun"
)

// buildTestModule compiles testdata/echomod to a WASM binary. It skips the test
// if the wasip1 toolchain isn't available in this environment.
func buildTestModule(t *testing.T) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "echo.wasm")
	cmd := exec.Command("go", "build", "-o", out, "./testdata/echomod")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build wasip1 test module: %v\n%s", err, b)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read module: %v", err)
	}
	return b
}

func TestRunner_ExecutesModuleAndCapturesOutput(t *testing.T) {
	wasm := buildTestModule(t)

	// Stub the host exec so the test is deterministic and cross-platform: record
	// the command and echo back a marker instead of really shelling out.
	var gotCmd string
	r := wasmrun.New()
	r.Exec = func(ctx context.Context, command string) (int, string) {
		gotCmd = command
		return 0, "exec-output\n"
	}

	res, err := r.Run(context.Background(), wasm, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("want exit 0, got %d (output: %s)", res.ExitCode, res.Output)
	}
	if !strings.Contains(res.Output, "hello-from-module") {
		t.Fatalf("log output not captured: %q", res.Output)
	}
	if !strings.Contains(res.Output, "exec-output") {
		t.Fatalf("exec output not captured: %q", res.Output)
	}
	if gotCmd != "echo exec-output" {
		t.Fatalf("host exec got wrong command: %q", gotCmd)
	}
}

func TestRunner_RealShellExec(t *testing.T) {
	wasm := buildTestModule(t)

	// Default runner uses the real platform shell; "echo exec-output" works on
	// both cmd.exe and sh.
	res, err := wasmrun.New().Run(context.Background(), wasm, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(res.Output, "exec-output") {
		t.Fatalf("real shell exec output missing: %q", res.Output)
	}
}
