// Package config loads runtime configuration from the environment with sane
// defaults. Twelve-factor style: all config comes from env vars so the same
// binary runs unchanged across dev, staging, and prod.
package config

import (
	"fmt"
	"os"
	"time"
)

// Server holds the control-plane configuration.
type Server struct {
	Addr              string        // listen address, e.g. ":8080"
	HeartbeatInterval time.Duration // expected agent beacon period
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	// DatabaseURL selects the PostgreSQL DSN. When empty, the in-memory store
	// is used (development only).
	DatabaseURL string

	// AgentDistDir is the directory of cross-compiled agent binaries the
	// control plane serves from /manage/bin/... . Populate it with `make agents`.
	AgentDistDir string

	// ModuleDir is the directory of modules the control plane
	// serves to agents for task execution.
	ModuleDir string
}

// LoadServer reads server config from the environment.
func LoadServer() Server {
	return Server{
		Addr:              env("ARMADA_ADDR", ":8080"),
		HeartbeatInterval: envDuration("ARMADA_HEARTBEAT_INTERVAL", 60*time.Second),
		ReadTimeout:       envDuration("ARMADA_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:      envDuration("ARMADA_WRITE_TIMEOUT", 15*time.Second),
		DatabaseURL:       os.Getenv("ARMADA_DATABASE_URL"),
		AgentDistDir:      env("ARMADA_AGENT_DIST_DIR", "bin/agents"),
		ModuleDir:         env("ARMADA_MODULE_DIR", "modules/dist"),
	}
}

// Agent holds the management-agent configuration.
type Agent struct {
	ServerURL         string
	JoinToken         string // reusable join key (zero-touch onboarding)
	MachineID         string // override the auto-derived machine id (cloned images/containers)
	FQDN              string
	APIKey            string // populated after enrollment
	StatePath         string // where the agent persists its identity
	ModuleCacheDir    string // where the agent caches downloaded modules
	PythonInterpreter string // override Python interpreter for .py modules
	HeartbeatInterval time.Duration
	InventoryInterval time.Duration
	TaskPollInterval  time.Duration
}

// LoadAgent reads agent config from the environment.
func LoadAgent() Agent {
	return Agent{
		ServerURL:         env("ARMADA_SERVER_URL", "http://localhost:8080"),
		JoinToken:         os.Getenv("ARMADA_JOIN_TOKEN"),
		MachineID:         os.Getenv("ARMADA_MACHINE_ID"),
		FQDN:              os.Getenv("ARMADA_FQDN"),
		APIKey:            os.Getenv("ARMADA_API_KEY"),
		StatePath:         env("ARMADA_AGENT_STATE", "armada-agent.json"),
		ModuleCacheDir:    env("ARMADA_MODULE_CACHE", "armada-modules"),
		PythonInterpreter: os.Getenv("ARMADA_PYTHON"),
		HeartbeatInterval: envDuration("ARMADA_HEARTBEAT_INTERVAL", 60*time.Second),
		InventoryInterval: envDuration("ARMADA_INVENTORY_INTERVAL", 30*time.Minute),
		TaskPollInterval:  envDuration("ARMADA_TASK_POLL_INTERVAL", 10*time.Second),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "armada: invalid duration for %s=%q, using default %s\n", key, v, def)
		return def
	}
	return d
}
