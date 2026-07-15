package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/SbxTheDead/armada/internal/opclient"
)

// runJoinToken dispatches the `join-token` sub-subcommands.
func runJoinToken(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: armada join-token <create|list|revoke>")
	}
	switch args[0] {
	case "create":
		return runJoinTokenCreate(ctx, args[1:])
	case "list":
		return runJoinTokenList(ctx, args[1:])
	case "revoke":
		return runJoinTokenRevoke(ctx, args[1:])
	default:
		return fmt.Errorf("unknown join-token subcommand %q", args[0])
	}
}

func runJoinTokenCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("join-token create", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	in := opclient.JoinTokenInput{}
	fs.StringVar(&in.Name, "name", "", "human label for the key")
	fs.StringVar(&in.Project, "project", "", "preset: project for joining devices")
	fs.StringVar(&in.Region, "region", "", "preset: region for joining devices")
	fs.StringVar(&in.Environment, "environment", "", "preset: environment")
	fs.StringVar(&in.Provider, "provider", "", "preset: provider")
	var tags stringSlice
	fs.Var(&tags, "tag", "preset: tag for joining devices (repeatable)")
	fs.StringVar(&in.Approval, "approval", "auto", "auto (activate now) or manual (require approval)")
	fs.IntVar(&in.MaxUses, "max-uses", 0, "max devices that may use this key (0 = unlimited)")
	ttl := fs.Duration("ttl", 0, "key lifetime (0 = never expires)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	in.Tags = tags
	in.TTLSeconds = int(ttl.Seconds())

	res, err := opclient.New(cfg).CreateJoinToken(ctx, in)
	if err != nil {
		return err
	}

	fmt.Printf("join key created (id %s)", res.ID)
	if res.Details != nil && res.Details.ExpiresAt != nil {
		fmt.Printf(", expires %s", res.Details.ExpiresAt.Local().Format(time.RFC3339))
	} else {
		fmt.Printf(", never expires")
	}
	fmt.Printf("\n\nBind ANY device or VM with one command (reusable across your whole fleet):\n\n")
	fmt.Printf("  Linux / macOS / BSD:\n")
	fmt.Printf("    wget -qO- '%s/manage?join=%s' | sh\n", cfg.BaseURL, res.Token)
	fmt.Printf("    curl -fsSL '%s/manage?join=%s' | sh\n\n", cfg.BaseURL, res.Token)
	fmt.Printf("  Windows (PowerShell, as Administrator):\n")
	fmt.Printf("    iwr -useb '%s/manage/install.ps1?join=%s' | iex\n\n", cfg.BaseURL, res.Token)
	fmt.Printf("Save this key now — it is not shown again. Devices auto-detect their\n")
	fmt.Printf("OS/arch, self-register, and appear in 'armada monitor'.\n")
	return nil
}

func runJoinTokenList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("join-token list", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	toks, err := opclient.New(cfg).ListJoinTokens(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(toks)
	}
	if len(toks) == 0 {
		fmt.Println("no join keys. Create one with: armada join-token create")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tAPPROVAL\tUSES\tPRESETS")
	for _, t := range toks {
		status := "active"
		if t.RevokedAt != nil {
			status = "revoked"
		} else if t.ExpiresAt != nil && t.ExpiresAt.Before(time.Now()) {
			status = "expired"
		}
		uses := fmt.Sprintf("%d", t.Uses)
		if t.MaxUses > 0 {
			uses = fmt.Sprintf("%d/%d", t.Uses, t.MaxUses)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			t.ID, dash(t.Name), status, t.Approval, uses, presetSummary(t.Project, t.Region, t.Environment, t.Tags))
	}
	_ = tw.Flush()
	return nil
}

func runJoinTokenRevoke(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("join-token revoke", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		return fmt.Errorf("usage: armada join-token revoke <id>")
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	if err := opclient.New(cfg).RevokeJoinToken(ctx, id); err != nil {
		return err
	}
	fmt.Printf("revoked join key %s\n", id)
	return nil
}

func runSystemsApprove(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("systems approve", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		return fmt.Errorf("usage: armada systems approve <id>")
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	sys, err := opclient.New(cfg).ApproveSystem(ctx, id)
	if err != nil {
		return err
	}
	fmt.Printf("approved %s (%s) — lifecycle now %s\n", sys.Name, sys.ID, sys.Lifecycle)
	return nil
}

func presetSummary(project, region, environment string, tags []string) string {
	parts := []string{}
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	add("project", project)
	add("region", region)
	add("env", environment)
	if len(tags) > 0 {
		parts = append(parts, "tags="+fmt.Sprint(tags))
	}
	if len(parts) == 0 {
		return "-"
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += " " + p
	}
	return out
}
