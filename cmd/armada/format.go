package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
)

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// printSystemsTable renders systems as an aligned table.
func printSystemsTable(systems []domain.System) {
	if len(systems) == 0 {
		fmt.Println("no systems match.")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "HEALTH\tNAME\tFQDN\tREGION\tOS\tLIFECYCLE\tLAST CHECK-IN\tID")
	for _, s := range systems {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			healthDot(s.Health),
			dash(s.Name),
			dash(s.FQDN),
			dash(s.Region),
			dash(s.OS),
			s.Lifecycle,
			relTime(s.LastCheckIn),
			s.ID,
		)
	}
	_ = tw.Flush()
}

func printSystemDetail(s *domain.System) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	row := func(k, v string) { fmt.Fprintf(tw, "%s\t%s\n", k, v) }
	row("ID", s.ID)
	row("Name", s.Name)
	row("FQDN", s.FQDN)
	row("Health", string(s.Health))
	row("Lifecycle", string(s.Lifecycle))
	row("Project", dash(s.Project))
	row("Region", dash(s.Region))
	row("Environment", dash(s.Environment))
	row("Provider", dash(s.Provider))
	row("Arch", dash(s.Arch))
	row("OS", dash(s.OS))
	row("Agent", dash(s.AgentVersion))
	if len(s.Tags) > 0 {
		row("Tags", fmt.Sprint(s.Tags))
	}
	row("Last check-in", relTime(s.LastCheckIn))
	_ = tw.Flush()
}

func healthDot(h domain.Health) string {
	// ANSI-coloured status marker; degrades to plain text if the terminal
	// ignores escapes.
	switch h {
	case domain.HealthHealthy:
		return "\033[32m●\033[0m healthy"
	case domain.HealthDegraded:
		return "\033[33m●\033[0m degraded"
	case domain.HealthOffline:
		return "\033[31m●\033[0m offline"
	default:
		return "\033[90m●\033[0m unknown"
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func relTime(t *time.Time) string {
	if t == nil {
		return "never"
	}
	d := time.Since(*t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
