package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/opclient"
)

// runMonitor renders a live, auto-refreshing view of the fleet: health, last
// check-in, and current CPU/memory/disk pulled from each system's latest
// heartbeat. Ctrl-C exits.
func runMonitor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
	resolve := registerGlobals(fs)
	filter := opclient.ListFilter{}
	fs.StringVar(&filter.Region, "region", "", "filter by region")
	fs.StringVar(&filter.Environment, "environment", "", "filter by environment")
	fs.StringVar(&filter.Project, "project", "", "filter by project")
	fs.StringVar(&filter.Health, "health", "", "filter by health")
	interval := fs.Duration("interval", 5*time.Second, "refresh interval")
	once := fs.Bool("once", false, "render a single snapshot and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := resolve()
	if err != nil {
		return err
	}
	client := opclient.New(cfg)

	render := func() error {
		systems, err := client.ListSystems(ctx, filter)
		if err != nil {
			return err
		}
		clearScreen()
		fmt.Printf("armada monitor — %s — %d systems — refresh %s (Ctrl-C to exit)\n\n",
			time.Now().Format("15:04:05"), len(systems), *interval)
		renderMonitorTable(ctx, client, systems)
		return nil
	}

	if *once {
		return render()
	}

	// Initial paint, then tick.
	if err := render(); err != nil {
		return err
	}
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Println()
			return nil
		case <-ticker.C:
			if err := render(); err != nil {
				// Transient control-plane errors shouldn't kill the monitor.
				fmt.Fprintf(os.Stderr, "refresh error: %v\n", err)
			}
		}
	}
}

func renderMonitorTable(ctx context.Context, client *opclient.Client, systems []domain.System) {
	if len(systems) == 0 {
		fmt.Println("no systems match the filter.")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "HEALTH\tNAME\tCPU%\tMEM\tDISK\tUP\tLAST\tAGENT")
	for _, s := range systems {
		cpu, mem, disk, up := "-", "-", "-", "-"
		// Metrics come from the latest heartbeat; skip quietly if none yet.
		if hb, err := client.GetMetrics(ctx, s.ID); err == nil {
			cpu = fmt.Sprintf("%.0f", hb.Metrics.CPUPercent)
			mem = pctBytes(hb.Metrics.MemUsedBytes, hb.Metrics.MemTotalBytes)
			disk = pctBytes(hb.Metrics.DiskUsedBytes, hb.Metrics.DiskTotalBytes)
			up = shortDur(time.Duration(hb.Uptime) * time.Second)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			healthDot(s.Health), dash(s.Name), cpu, mem, disk, up,
			relTime(s.LastCheckIn), dash(s.AgentVersion),
		)
	}
	_ = tw.Flush()
}

// pctBytes renders "used/total (NN%)" compactly, or "-" when total is unknown.
func pctBytes(used, total uint64) string {
	if total == 0 {
		return "-"
	}
	pct := float64(used) / float64(total) * 100
	return fmt.Sprintf("%.0f%%", pct)
}

func shortDur(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func clearScreen() {
	// ANSI clear + home cursor. Supported by modern Windows Terminal, conhost
	// (Win10+), and every POSIX terminal.
	fmt.Print("\033[H\033[2J")
}
