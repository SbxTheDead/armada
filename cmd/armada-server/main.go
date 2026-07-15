// Command armada-server is the control-plane API for the fleet-management
// platform. It exposes the operator REST API and the agent ingestion API.
//
// This scaffold uses the in-memory store by default so it runs with zero
// external dependencies; set ARMADA_DATABASE_URL to point at PostgreSQL once
// the pgx adapter is wired in (see internal/store).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SbxTheDead/armada/internal/config"
	"github.com/SbxTheDead/armada/internal/httpapi"
	"github.com/SbxTheDead/armada/internal/service"
	"github.com/SbxTheDead/armada/internal/store/memory"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg := config.LoadServer()

	// Persistence. The in-memory store is the default dev backend; production
	// swaps in the PostgreSQL adapter behind the same interfaces.
	db := memory.New()
	if cfg.DatabaseURL != "" {
		log.Warn("ARMADA_DATABASE_URL set but the PostgreSQL adapter is not yet wired; using in-memory store")
	}

	fleet := service.NewFleet(db.Systems, db.JoinTokens, db.Identities, db.Telemetry, db.Work, service.Options{
		HeartbeatInterval: cfg.HeartbeatInterval,
	})

	srv := httpapi.New(httpapi.Config{
		Fleet:         fleet,
		Logger:        log,
		OperatorToken: os.Getenv("ARMADA_OPERATOR_TOKEN"),
		AgentDistDir:  cfg.AgentDistDir,
		ModuleDir:     cfg.ModuleDir,
	})
	warnIfNoAgents(log, cfg.AgentDistDir)

	httpServer := &http.Server{
		Addr:         cfg.Addr,
		Handler:      srv.Handler(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("armada-server listening", "addr", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received, draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

// warnIfNoAgents logs a hint at startup if the agent distribution directory has
// no binaries, since /manage installers will fail until it is populated.
func warnIfNoAgents(log *slog.Logger, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		log.Warn("no agent binaries to serve; run 'make agents' so /manage can install devices",
			"dist_dir", dir)
	}
}
