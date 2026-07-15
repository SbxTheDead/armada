// Package memory is an in-process implementation of the store ports. It is the
// default backend for local development and the substrate for unit tests: no
// external services, deterministic, and safe for concurrent use. Production
// deployments swap in the PostgreSQL adapter behind the same interfaces.
//
// One type cannot implement Create for both SystemStore and TokenStore, so the
// backend is a shared data core (`core`) fronted by four small adapter types,
// one per port. All adapters share the same mutex and maps.
package memory

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/store"
)

// core holds the actual data behind one lock. The fleet is small (hosts, not
// events) so coarse locking is appropriate.
type core struct {
	mu         sync.RWMutex
	systems    map[string]*domain.System          // key: tenantID/id
	tokens     map[string]*domain.EnrollmentToken // key: hash
	identities map[string]*domain.AgentIdentity   // key: tenantID/systemID
	byAPIKey   map[string]string                  // apiKeyHash -> tenantID/systemID
	heartbeats map[string]*domain.Heartbeat       // key: systemID (latest only)
	inventory  map[string]*domain.Inventory       // key: systemID
}

// DB is the set of port adapters returned by New. Wire each field into the
// service that needs it.
type DB struct {
	Systems    *SystemStore
	Tokens     *TokenStore
	Identities *IdentityStore
	Telemetry  *TelemetryStore
}

// New constructs an initialised in-memory DB.
func New() *DB {
	c := &core{
		systems:    make(map[string]*domain.System),
		tokens:     make(map[string]*domain.EnrollmentToken),
		identities: make(map[string]*domain.AgentIdentity),
		byAPIKey:   make(map[string]string),
		heartbeats: make(map[string]*domain.Heartbeat),
		inventory:  make(map[string]*domain.Inventory),
	}
	return &DB{
		Systems:    &SystemStore{c},
		Tokens:     &TokenStore{c},
		Identities: &IdentityStore{c},
		Telemetry:  &TelemetryStore{c},
	}
}

func sysKey(tenantID, id string) string { return tenantID + "/" + id }

// --- SystemStore ---

type SystemStore struct{ c *core }

var _ store.SystemStore = (*SystemStore)(nil)

func (s *SystemStore) Create(ctx context.Context, sys *domain.System) error {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	for _, existing := range s.c.systems {
		if existing.TenantID == sys.TenantID && strings.EqualFold(existing.FQDN, sys.FQDN) {
			return domain.ErrAlreadyExists
		}
	}
	cp := *sys
	s.c.systems[sysKey(sys.TenantID, sys.ID)] = &cp
	return nil
}

func (s *SystemStore) Get(ctx context.Context, tenantID, id string) (*domain.System, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	sys, ok := s.c.systems[sysKey(tenantID, id)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *sys
	return &cp, nil
}

func (s *SystemStore) GetByFQDN(ctx context.Context, tenantID, fqdn string) (*domain.System, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	for _, sys := range s.c.systems {
		if sys.TenantID == tenantID && strings.EqualFold(sys.FQDN, fqdn) {
			cp := *sys
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *SystemStore) List(ctx context.Context, tenantID string, f store.SystemFilter) ([]*domain.System, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()

	var out []*domain.System
	for _, sys := range s.c.systems {
		if sys.TenantID != tenantID || !matches(sys, f) {
			continue
		}
		cp := *sys
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return paginate(out, f.Offset, f.Limit), nil
}

func (s *SystemStore) Update(ctx context.Context, sys *domain.System) error {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	key := sysKey(sys.TenantID, sys.ID)
	if _, ok := s.c.systems[key]; !ok {
		return domain.ErrNotFound
	}
	cp := *sys
	s.c.systems[key] = &cp
	return nil
}

func matches(sys *domain.System, f store.SystemFilter) bool {
	if f.Project != "" && sys.Project != f.Project {
		return false
	}
	if f.Region != "" && sys.Region != f.Region {
		return false
	}
	if f.Environment != "" && sys.Environment != f.Environment {
		return false
	}
	if f.Provider != "" && sys.Provider != f.Provider {
		return false
	}
	if f.Lifecycle != "" && sys.Lifecycle != f.Lifecycle {
		return false
	}
	if f.Health != "" && sys.Health != f.Health {
		return false
	}
	if f.Tag != "" && !containsTag(sys.Tags, f.Tag) {
		return false
	}
	return true
}

func containsTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

func paginate[T any](items []T, offset, limit int) []T {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []T{}
	}
	items = items[offset:]
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return items
}

// --- TokenStore ---

type TokenStore struct{ c *core }

var _ store.TokenStore = (*TokenStore)(nil)

func (s *TokenStore) Create(ctx context.Context, t *domain.EnrollmentToken) error {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	cp := *t
	s.c.tokens[t.Hash] = &cp
	return nil
}

func (s *TokenStore) GetByHash(ctx context.Context, hash string) (*domain.EnrollmentToken, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	t, ok := s.c.tokens[hash]
	if !ok {
		return nil, domain.ErrEnrollmentToken
	}
	cp := *t
	return &cp, nil
}

func (s *TokenStore) Update(ctx context.Context, t *domain.EnrollmentToken) error {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if _, ok := s.c.tokens[t.Hash]; !ok {
		return domain.ErrNotFound
	}
	cp := *t
	s.c.tokens[t.Hash] = &cp
	return nil
}

// --- IdentityStore ---

type IdentityStore struct{ c *core }

var _ store.IdentityStore = (*IdentityStore)(nil)

func (s *IdentityStore) Create(ctx context.Context, id *domain.AgentIdentity) error {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	cp := *id
	s.c.identities[sysKey(id.TenantID, id.SystemID)] = &cp
	s.c.byAPIKey[id.APIKeyHash] = sysKey(id.TenantID, id.SystemID)
	return nil
}

func (s *IdentityStore) GetBySystem(ctx context.Context, tenantID, systemID string) (*domain.AgentIdentity, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	id, ok := s.c.identities[sysKey(tenantID, systemID)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *id
	return &cp, nil
}

func (s *IdentityStore) GetByAPIKeyHash(ctx context.Context, hash string) (*domain.AgentIdentity, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	key, ok := s.c.byAPIKey[hash]
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	id, ok := s.c.identities[key]
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	cp := *id
	return &cp, nil
}

// --- TelemetryStore ---

type TelemetryStore struct{ c *core }

var _ store.TelemetryStore = (*TelemetryStore)(nil)

func (s *TelemetryStore) AppendHeartbeat(ctx context.Context, hb *domain.Heartbeat) error {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	cp := *hb
	s.c.heartbeats[hb.SystemID] = &cp
	return nil
}

func (s *TelemetryStore) LatestHeartbeat(ctx context.Context, systemID string) (*domain.Heartbeat, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	hb, ok := s.c.heartbeats[systemID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *hb
	return &cp, nil
}

func (s *TelemetryStore) PutInventory(ctx context.Context, inv *domain.Inventory) error {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	cp := *inv
	s.c.inventory[inv.SystemID] = &cp
	return nil
}

func (s *TelemetryStore) GetInventory(ctx context.Context, systemID string) (*domain.Inventory, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	inv, ok := s.c.inventory[systemID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *inv
	return &cp, nil
}
