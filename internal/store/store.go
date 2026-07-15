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

// TokenStore persists enrollment tokens.
type TokenStore interface {
	Create(ctx context.Context, t *domain.EnrollmentToken) error
	GetByHash(ctx context.Context, hash string) (*domain.EnrollmentToken, error)
	Update(ctx context.Context, t *domain.EnrollmentToken) error
}

// IdentityStore persists per-system agent credentials.
type IdentityStore interface {
	Create(ctx context.Context, id *domain.AgentIdentity) error
	GetBySystem(ctx context.Context, tenantID, systemID string) (*domain.AgentIdentity, error)
	// GetByAPIKeyHash resolves an authenticating agent from its presented key.
	GetByAPIKeyHash(ctx context.Context, hash string) (*domain.AgentIdentity, error)
}

// TelemetryStore persists the append-only heartbeat and inventory streams.
// Implementations may down-sample or retain a rolling window.
type TelemetryStore interface {
	AppendHeartbeat(ctx context.Context, hb *domain.Heartbeat) error
	LatestHeartbeat(ctx context.Context, systemID string) (*domain.Heartbeat, error)
	PutInventory(ctx context.Context, inv *domain.Inventory) error
	GetInventory(ctx context.Context, systemID string) (*domain.Inventory, error)
}
