package pyrun_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/SbxTheDead/armada/internal/agent/pyrun"
)

// findPython returns a working interpreter for the test, or "" if none.
func findPython() string {
	for _, c := range []string{"python3", "python", "py"} {
		out, err := exec.Command(c, "--version").CombinedOutput()
		if err == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "Python") {
			return c
		}
	}
	return ""
}

func TestPyRun_ExecutesScript(t *testing.T) {
	interp := findPython()
	if interp == "" {
		t.Skip("no Python interpreter available")
	}
	r := pyrun.New(interp)
	script := []byte("import sys\nprint('hello', sys.argv[1])\nsys.exit(0)\n")
	res, err := r.Run(context.Background(), script, []string{"world"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("want exit 0, got %d (%s)", res.ExitCode, res.Output)
	}
	if !strings.Contains(res.Output, "hello world") {
		t.Fatalf("script output/args not captured: %q", res.Output)
	}
}

func TestPyRun_NonZeroExit(t *testing.T) {
	interp := findPython()
	if interp == "" {
		t.Skip("no Python interpreter available")
	}
	r := pyrun.New(interp)
	res, err := r.Run(context.Background(), []byte("import sys\nsys.exit(3)\n"), nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("want exit 3, got %d", res.ExitCode)
	}
}

func TestPyRun_MissingInterpreter(t *testing.T) {
	r := pyrun.New("definitely-not-python-xyz")
	if _, err := r.Run(context.Background(), []byte("print('x')"), nil); err == nil {
		t.Fatal("expected error for a missing interpreter")
	}
}
