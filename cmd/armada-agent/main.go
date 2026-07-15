// Command armada-agent is the cross-platform management agent. It enrolls with
// the control plane, then reports inventory and heartbeats and runs
// administrator-approved maintenance tasks.
//
// By design the agent contains no persistence beyond its own identity file, no
// stealth, no credential harvesting, no privilege escalation, and no defense
// evasion. It is a transparent, observable management client.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/SbxTheDead/armada/internal/agent"
	"github.com/SbxTheDead/armada/internal/config"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.LoadAgent()
	if err := agent.Run(ctx, cfg, log, version); err != nil {
		log.Error("agent exited with error", "err", err)
		os.Exit(1)
	}
}
