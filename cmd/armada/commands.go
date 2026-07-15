package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/SbxTheDead/armada/internal/opclient"
)

// runSystems dispatches the `systems` sub-subcommands.
func runSystems(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: armada systems <list|get|inventory|approve>")
	}
	switch args[0] {
	case "list":
		return runSystemsList(ctx, args[1:])
	case "get":
		return runSystemsGet(ctx, args[1:])
	case "inventory":
		return runSystemsInventory(ctx, args[1:])
	case "approve":
		return runSystemsApprove(ctx, args[1:])
	default:
		return fmt.Errorf("unknown systems subcommand %q", args[0])
	}
}

func runSystemsList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("systems list", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	filter := opclient.ListFilter{}
	fs.StringVar(&filter.Project, "project", "", "filter by project")
	fs.StringVar(&filter.Region, "region", "", "filter by region")
	fs.StringVar(&filter.Environment, "environment", "", "filter by environment")
	fs.StringVar(&filter.Provider, "provider", "", "filter by provider")
	fs.StringVar(&filter.Tag, "tag", "", "filter by tag")
	fs.StringVar(&filter.Lifecycle, "lifecycle", "", "filter by lifecycle")
	fs.StringVar(&filter.Health, "health", "", "filter by health (healthy/degraded/offline/unknown)")
	fs.IntVar(&filter.Limit, "limit", 0, "max rows")
	jsonOut := fs.Bool("json", false, "emit JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}

	systems, err := opclient.New(cfg).ListSystems(ctx, filter)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(systems)
	}
	printSystemsTable(systems)
	return nil
}

func runSystemsGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("systems get", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		return fmt.Errorf("usage: armada systems get <id>")
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	sys, err := opclient.New(cfg).GetSystem(ctx, id)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(sys)
	}
	printSystemDetail(sys)
	return nil
}

func runSystemsInventory(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("systems inventory", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		return fmt.Errorf("usage: armada systems inventory <id>")
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	inv, err := opclient.New(cfg).GetInventory(ctx, id)
	if err != nil {
		return err
	}
	return printJSON(inv)
}
