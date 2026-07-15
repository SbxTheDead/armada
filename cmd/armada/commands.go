package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/SbxTheDead/armada/internal/opclient"
)

// runSystems dispatches the `systems` sub-subcommands.
func runSystems(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: armada systems <register|list|get|inventory>")
	}
	switch args[0] {
	case "register":
		return runSystemsRegister(ctx, args[1:])
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

func runSystemsRegister(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("systems register", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	name := fs.String("name", "", "human-readable name (required)")
	fqdn := fs.String("fqdn", "", "fully-qualified domain name (required)")
	project := fs.String("project", "", "project grouping")
	region := fs.String("region", "", "region grouping")
	environment := fs.String("environment", "", "environment grouping (prod/staging/...)")
	provider := fs.String("provider", "", "cloud/host provider")
	var tags stringSlice
	fs.Var(&tags, "tag", "tag (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	if *name == "" || *fqdn == "" {
		return fmt.Errorf("--name and --fqdn are required")
	}

	client := opclient.New(cfg)
	sys, err := client.RegisterSystem(ctx, opclient.RegisterInput{
		Name:        *name,
		FQDN:        *fqdn,
		Project:     *project,
		Region:      *region,
		Environment: *environment,
		Provider:    *provider,
		Tags:        tags,
	})
	if err != nil {
		return err
	}
	fmt.Printf("registered system %s (%s)\n", sys.Name, sys.ID)
	fmt.Printf("next: armada enroll %s\n", sys.ID)
	return nil
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

func runEnroll(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("enroll", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	ttl := fs.Duration("ttl", 15*time.Minute, "token lifetime")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		return fmt.Errorf("usage: armada enroll <system-id> [--ttl 15m]")
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	res, err := opclient.New(cfg).IssueEnrollToken(ctx, id, *ttl)
	if err != nil {
		return err
	}
	fmt.Printf("enrollment token (valid until %s):\n\n  %s\n\n",
		res.ExpiresAt.Local().Format(time.RFC3339), res.Token)
	fmt.Printf("on the device, run the agent with:\n")
	fmt.Printf("  ARMADA_SERVER_URL=%s ARMADA_ENROLL_TOKEN=%s armada-agent\n", cfg.BaseURL, res.Token)
	return nil
}

// runInstallCommand issues an enrollment token and prints ready-to-paste
// one-liners that download, install, and enroll the correct agent on a device —
// the server auto-detects OS/arch. This is the fast path for binding a device
// or VM to the fleet.
func runInstallCommand(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("install-command", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	ttl := fs.Duration("ttl", 30*time.Minute, "token lifetime")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		return fmt.Errorf("usage: armada install-command <system-id> [--ttl 30m]")
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	res, err := opclient.New(cfg).IssueEnrollToken(ctx, id, *ttl)
	if err != nil {
		return err
	}

	server := cfg.BaseURL
	fmt.Printf("Bind this device to the fleet (token valid until %s):\n\n",
		res.ExpiresAt.Local().Format(time.RFC3339))
	fmt.Printf("  Linux / macOS / BSD:\n")
	fmt.Printf("    curl -fsSL '%s/manage?token=%s' | sh\n\n", server, res.Token)
	fmt.Printf("  Windows (PowerShell, as Administrator):\n")
	fmt.Printf("    iwr -useb '%s/manage/install.ps1?token=%s' | iex\n\n", server, res.Token)
	fmt.Printf("The server auto-detects the device's OS and CPU architecture and\n")
	fmt.Printf("installs the matching agent as a service. It appears in htop as\n")
	fmt.Printf("\"MANAGEMENT AGENT\".\n")
	return nil
}
