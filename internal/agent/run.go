package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/SbxTheDead/armada/internal/agent/inventory"
	"github.com/SbxTheDead/armada/internal/config"
	"github.com/SbxTheDead/armada/internal/domain"
)

// ProcTitle is the name the agent presents to process viewers (htop, ps, top)
// on Linux. Overridable at build time with:
//
//	go build -ldflags "-X github.com/SbxTheDead/armada/internal/agent.ProcTitle=..."
var ProcTitle = "MANAGEMENT AGENT"

// state is the small piece of identity the agent persists between runs so it
// need not re-enroll every restart.
type state struct {
	SystemID string `json:"system_id"`
	APIKey   string `json:"api_key"`
}

// Run boots the agent: ensure enrollment, then loop sending heartbeats and
// periodic inventory until the context is cancelled.
func Run(ctx context.Context, cfg config.Agent, log *slog.Logger, version string) error {
	// Present a stable, recognizable name to process viewers (htop/ps/top).
	setProcTitle(ProcTitle)

	client := NewClient(cfg.ServerURL, cfg.APIKey, version)

	st, err := ensureEnrolled(ctx, cfg, client, log)
	if err != nil {
		return err
	}
	client.SetAPIKey(st.APIKey)
	log.Info("agent ready", "system_id", st.SystemID, "server", cfg.ServerURL)

	// Send an inventory snapshot immediately, then on its own slower cadence.
	sendInventory(ctx, client, st.SystemID, log)

	hbTicker := time.NewTicker(cfg.HeartbeatInterval)
	defer hbTicker.Stop()
	invTicker := time.NewTicker(cfg.InventoryInterval)
	defer invTicker.Stop()

	// Fire one heartbeat right away so the system flips healthy without waiting
	// a full interval.
	sendHeartbeat(ctx, client, st.SystemID, log)

	for {
		select {
		case <-ctx.Done():
			log.Info("agent shutting down")
			return nil
		case <-hbTicker.C:
			sendHeartbeat(ctx, client, st.SystemID, log)
		case <-invTicker.C:
			sendInventory(ctx, client, st.SystemID, log)
		}
	}
}

// ensureEnrolled loads persisted identity, or enrolls using the configured
// token and saves the result.
func ensureEnrolled(ctx context.Context, cfg config.Agent, client *Client, log *slog.Logger) (state, error) {
	// 1. Already have a key via env or persisted state?
	if cfg.APIKey != "" {
		return state{APIKey: cfg.APIKey}, nil
	}
	if st, ok := loadState(cfg.StatePath); ok {
		return st, nil
	}

	fqdn := cfg.FQDN
	if fqdn == "" {
		fqdn, _ = os.Hostname()
	}

	var (
		systemID, apiKey string
		err              error
	)
	switch {
	// 2. Zero-touch: reusable join key self-registers this device.
	case cfg.JoinToken != "":
		hostname, _ := os.Hostname()
		mid := cfg.MachineID
		if mid == "" {
			mid = machineID()
		}
		facts := domain.DeviceFacts{
			MachineID: mid,
			Hostname:  hostname,
			FQDN:      fqdn,
			OS:        runtime.GOOS,
			Arch:      runtime.GOARCH,
		}
		log.Info("joining fleet with join key", "fqdn", fqdn, "machine_id", shortID(facts.MachineID))
		systemID, apiKey, err = client.Join(ctx, cfg.JoinToken, facts)
		if err != nil {
			return state{}, fmt.Errorf("join failed: %w", err)
		}
	// 3. Single-use enrollment token against a pre-registered system.
	case cfg.EnrollToken != "":
		log.Info("enrolling with control plane", "fqdn", fqdn)
		systemID, apiKey, err = client.Enroll(ctx, cfg.EnrollToken, fqdn)
		if err != nil {
			return state{}, fmt.Errorf("enrollment failed: %w", err)
		}
	default:
		return state{}, fmt.Errorf("agent has no API key, join key, or enrollment token; set ARMADA_JOIN_TOKEN")
	}

	st := state{SystemID: systemID, APIKey: apiKey}
	if err := saveState(cfg.StatePath, st); err != nil {
		log.Warn("could not persist agent state; will re-enroll on restart", "err", err)
	}
	return st, nil
}

func sendHeartbeat(ctx context.Context, client *Client, systemID string, log *slog.Logger) {
	hb := domain.Heartbeat{
		SystemID: systemID,
		Uptime:   int64(time.Since(processStart).Seconds()),
		Metrics:  collectMetrics(),
	}
	if err := client.SendHeartbeat(ctx, hb); err != nil {
		log.Warn("heartbeat failed", "err", err)
	}
}

func sendInventory(ctx context.Context, client *Client, systemID string, log *slog.Logger) {
	inv := inventory.Collect()
	inv.SystemID = systemID
	if err := client.SendInventory(ctx, inv); err != nil {
		log.Warn("inventory upload failed", "err", err)
	}
}

// collectMetrics gathers lightweight runtime metrics. This scaffold reports the
// Go runtime's memory view; the platform-specific collectors (procfs, GetTick-
// Count, sysctl) plug in behind this function.
func collectMetrics() domain.Metrics {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return domain.Metrics{
		MemUsedBytes: ms.Alloc,
	}
}

var processStart = time.Now()

// shortID truncates an identifier for log readability without panicking on
// short values (e.g. an operator-supplied ARMADA_MACHINE_ID).
func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

// --- state persistence ---

func loadState(path string) (state, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return state{}, false
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		return state{}, false
	}
	if st.APIKey == "" {
		return state{}, false
	}
	return st, true
}

func saveState(path string, st state) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	// 0600: the API key is a secret; restrict to the owning user.
	return os.WriteFile(path, b, 0o600)
}
