// Package store defines the persistence ports for the platform. Concrete
// adapters (in-memory for tests/dev, PostgreSQL for production) implement these
// interfaces. Keeping them here — depended on by the service layer, satisfied
// by infrastructure — is the dependency-inversion seam of the architecture.
package store

import (
	"context"

	"github.com/SbxTheDead/armada/internal/domain"
)

// SystemStore persists System aggregates.
type SystemStore interface {
	Create(ctx context.Context, s *domain.System) error
	Get(ctx context.Context, tenantID, id string) (*domain.System, error)
	GetByFQDN(ctx context.Context, tenantID, fqdn string) (*domain.System, error)
	// GetByMachineID resolves a system by its stable machine identifier, used to
	// deduplicate zero-touch joins. Returns ErrNotFound if none matches.
	GetByMachineID(ctx context.Context, tenantID, machineID string) (*domain.System, error)
	List(ctx context.Context, tenantID string, f SystemFilter) ([]*domain.System, error)
	Update(ctx context.Context, s *domain.System) error
}

// SystemFilter narrows a List query. Zero-value fields are ignored, so the
// empty filter returns every system in the tenant.
type SystemFilter struct {
	Project     string
	Region      string
	Environment string
	Provider    string
	Tag         string
	Lifecycle   domain.Lifecycle
	Health      domain.Health
	Limit       int
	Offset      int
}

// JoinTokenStore persists reusable join tokens.
type JoinTokenStore interface {
	Create(ctx context.Context, t *domain.JoinToken) error
	GetByHash(ctx context.Context, hash string) (*domain.JoinToken, error)
	GetByID(ctx context.Context, tenantID, id string) (*domain.JoinToken, error)
	List(ctx context.Context, tenantID string) ([]*domain.JoinToken, error)
	Update(ctx context.Context, t *domain.JoinToken) error
}

// IdentityStore persists per-system agent credentials.
type IdentityStore interface {
	Create(ctx context.Context, id *domain.AgentIdentity) error
	GetBySystem(ctx context.Context, tenantID, systemID string) (*domain.AgentIdentity, error)
	// GetByAPIKeyHash resolves an authenticating agent from its presented key.
	GetByAPIKeyHash(ctx context.Context, hash string) (*domain.AgentIdentity, error)
}

// WorkStore persists jobs and their fanned-out tasks. ClaimPendingForSystem is
// the agent's poll: it atomically flips a system's pending tasks to dispatched
// and returns them, so a task is handed out exactly once.
type WorkStore interface {
	CreateJob(ctx context.Context, j *domain.Job) error
	GetJob(ctx context.Context, tenantID, id string) (*domain.Job, error)
	ListJobs(ctx context.Context, tenantID string) ([]*domain.Job, error)

	CreateTask(ctx context.Context, t *domain.Task) error
	GetTask(ctx context.Context, tenantID, id string) (*domain.Task, error)
	ListTasksByJob(ctx context.Context, tenantID, jobID string) ([]*domain.Task, error)
	ClaimPendingForSystem(ctx context.Context, tenantID, systemID string) ([]*domain.Task, error)
	UpdateTask(ctx context.Context, t *domain.Task) error
}

// TelemetryStore persists the append-only heartbeat and inventory streams.
// Implementations may down-sample or retain a rolling window.
type TelemetryStore interface {
	AppendHeartbeat(ctx context.Context, hb *domain.Heartbeat) error
	LatestHeartbeat(ctx context.Context, systemID string) (*domain.Heartbeat, error)
	PutInventory(ctx context.Context, inv *domain.Inventory) error
	GetInventory(ctx context.Context, systemID string) (*domain.Inventory, error)
}
