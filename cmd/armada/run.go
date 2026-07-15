package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/opclient"
)

// runRun dispatches a module to matching devices: `armada run <module> [filters]`.
func runRun(ctx context.Context, args []string) error {
	// The module name is the first token; flags follow it. (Go's flag package
	// stops at the first positional, so we split the module off before parsing.)
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: armada run <module> [--all|--region|--tag ...] [--arg ...]")
	}
	module := args[0]

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	filter := opclient.ListFilter{}
	fs.StringVar(&filter.Region, "region", "", "target devices in this region")
	fs.StringVar(&filter.Environment, "environment", "", "target this environment")
	fs.StringVar(&filter.Project, "project", "", "target this project")
	fs.StringVar(&filter.Provider, "provider", "", "target this provider")
	fs.StringVar(&filter.Tag, "tag", "", "target devices with this tag")
	fs.Bool("all", false, "target all devices (default when no filter is given)")
	var modArgs stringSlice
	fs.Var(&modArgs, "arg", "argument passed to the module (repeatable)")
	wait := fs.Bool("wait", true, "wait for results and print them")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	client := opclient.New(cfg)

	job, err := client.RunModule(ctx, module, modArgs, filter)
	if err != nil {
		return err
	}
	fmt.Printf("dispatched %q to %d device(s) — job %s\n", job.Module, job.Total, job.ID)
	if !*wait {
		fmt.Printf("track it with: armada jobs get %s\n", job.ID)
		return nil
	}
	return watchJob(ctx, client, job.ID)
}

// runJobs dispatches the `jobs` sub-subcommands.
func runJobs(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: armada jobs <list|get|watch>")
	}
	switch args[0] {
	case "list":
		return runJobsList(ctx, args[1:])
	case "get":
		return runJobsGet(ctx, args[1:], false)
	case "watch":
		return runJobsGet(ctx, args[1:], true)
	default:
		return fmt.Errorf("unknown jobs subcommand %q", args[0])
	}
}

func runJobsList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("jobs list", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	jobs, err := opclient.New(cfg).ListJobs(ctx)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		fmt.Println("no jobs yet.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tMODULE\tTARGETS\tSELECTOR")
	for _, j := range jobs {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", j.ID, j.Module, j.Total, j.Selector)
	}
	_ = tw.Flush()
	return nil
}

func runJobsGet(ctx context.Context, args []string, wait bool) error {
	fs := flag.NewFlagSet("jobs get", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		return fmt.Errorf("usage: armada jobs get <id>")
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	client := opclient.New(cfg)
	if wait {
		return watchJob(ctx, client, id)
	}
	d, err := client.GetJob(ctx, id)
	if err != nil {
		return err
	}
	printJobDetail(d)
	return nil
}

// watchJob polls a job until every task is terminal, then prints the results.
func watchJob(ctx context.Context, client *opclient.Client, id string) error {
	for {
		d, err := client.GetJob(ctx, id)
		if err != nil {
			return err
		}
		if allTerminal(d.Tasks) {
			printJobDetail(d)
			return nil
		}
		select {
		case <-ctx.Done():
			printJobDetail(d)
			return nil
		case <-time.After(2 * time.Second):
		}
	}
}

func allTerminal(tasks []domain.Task) bool {
	for _, t := range tasks {
		if t.Status != domain.TaskSucceeded && t.Status != domain.TaskFailed {
			return false
		}
	}
	return len(tasks) > 0
}

func printJobDetail(d *opclient.JobDetail) {
	ok, fail, pending := 0, 0, 0
	for _, t := range d.Tasks {
		switch t.Status {
		case domain.TaskSucceeded:
			ok++
		case domain.TaskFailed:
			fail++
		default:
			pending++
		}
	}
	fmt.Printf("job %s — module %q — %d ok, %d failed, %d pending\n\n",
		d.Job.ID, d.Job.Module, ok, fail, pending)

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tSYSTEM\tEXIT")
	for _, t := range d.Tasks {
		fmt.Fprintf(tw, "%s\t%s\t%d\n", statusMark(t.Status), t.SystemID, t.ExitCode)
	}
	_ = tw.Flush()

	// Print output/errors below the table.
	for _, t := range d.Tasks {
		if t.Output == "" && t.Error == "" {
			continue
		}
		fmt.Printf("\n── %s (%s) ──\n", t.SystemID, t.Status)
		if t.Error != "" {
			fmt.Printf("error: %s\n", t.Error)
		}
		if t.Output != "" {
			fmt.Println(t.Output)
		}
	}
}

func statusMark(s domain.TaskStatus) string {
	switch s {
	case domain.TaskSucceeded:
		return "\033[32mok\033[0m"
	case domain.TaskFailed:
		return "\033[31mfail\033[0m"
	default:
		return "\033[90m" + string(s) + "\033[0m"
	}
}

// runModules lists the modules the control plane can dispatch.
func runModules(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("modules", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	mods, err := opclient.New(cfg).ListModules(ctx)
	if err != nil {
		return err
	}
	if len(mods) == 0 {
		fmt.Println("no modules published. Drop a compiled <name>.wasm into the server's module dir.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "MODULE\tRUNTIME\tDETAIL")
	for _, m := range mods {
		detail := fmt.Sprintf("%d bytes  %s", m.Size, short(m.SHA256))
		if m.Runtime == "native" {
			detail = fmt.Sprintf("%d builds: %s", len(m.Targets), strings.Join(m.Targets, " "))
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", m.Name, m.Runtime, detail)
	}
	_ = tw.Flush()
	return nil
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
