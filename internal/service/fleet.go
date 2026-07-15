// Package service holds application logic: it orchestrates domain objects and
// persistence ports without knowing anything about transport (HTTP/gRPC) or
// concrete storage. This is the use-case layer of the clean architecture.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/store"
)

// Clock and IDGen are injected so the service is deterministic under test. The
// production wiring uses wall-clock time and crypto/rand-backed IDs.
type Clock func() time.Time

type IDGen func() string

// Fleet is the primary use-case service: register systems, enroll agents,
// ingest telemetry, and answer queries about fleet state.
type Fleet struct {
	systems    store.SystemStore
	tokens     store.TokenStore
	identities store.IdentityStore
	telemetry  store.TelemetryStore

	now Clock
	id  IDGen

	// heartbeatInterval is the agents' expected beacon period, used to derive
	// health. Configurable so different fleets can tune liveness sensitivity.
	heartbeatInterval time.Duration
}

// Options configures a Fleet. Zero values fall back to safe defaults.
type Options struct {
	Now               Clock
	IDGen             IDGen
	HeartbeatInterval time.Duration
}

// NewFleet wires the service to its persistence ports.
func NewFleet(systems store.SystemStore, tokens store.TokenStore, identities store.IdentityStore, telemetry store.TelemetryStore, opts Options) *Fleet {
	f := &Fleet{
		systems:           systems,
		tokens:            tokens,
		identities:        identities,
		telemetry:         telemetry,
		now:               opts.Now,
		id:                opts.IDGen,
		heartbeatInterval: opts.HeartbeatInterval,
	}
	if f.now == nil {
		f.now = time.Now
	}
	if f.id == nil {
		f.id = randomID
	}
	if f.heartbeatInterval <= 0 {
		f.heartbeatInterval = 60 * time.Second
	}
	return f
}

// RegisterSystem creates a new managed system in the pending lifecycle. It is an
// operator action (authenticated as a user/API key), distinct from agent
// enrollment.
func (f *Fleet) RegisterSystem(ctx context.Context, in domain.NewSystemInput) (*domain.System, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	now := f.now().UTC()
	sys := &domain.System{
		ID:          f.id(),
		TenantID:    in.TenantID,
		Name:        in.Name,
		FQDN:        in.FQDN,
		Project:     in.Project,
		Region:      in.Region,
		Environment: in.Environment,
		Provider:    in.Provider,
		Tags:        in.Tags,
		Labels:      in.Labels,
		Lifecycle:   domain.LifecyclePending,
		Health:      domain.HealthUnknown,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := f.systems.Create(ctx, sys); err != nil {
		return nil, err
	}
	return sys, nil
}

// GetSystem returns a single system with its live health recomputed.
func (f *Fleet) GetSystem(ctx context.Context, tenantID, id string) (*domain.System, error) {
	sys, err := f.systems.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	f.refreshHealth(ctx, sys)
	return sys, nil
}

// ListSystems returns systems matching the filter with health recomputed.
func (f *Fleet) ListSystems(ctx context.Context, tenantID string, filter store.SystemFilter) ([]*domain.System, error) {
	sys, err := f.systems.List(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	for _, s := range sys {
		f.refreshHealth(ctx, s)
	}
	return sys, nil
}

// refreshHealth recomputes Health from the latest heartbeat without persisting;
// it reflects "now" at read time so a system that stopped beaconing reads as
// offline even if no writer has touched it.
func (f *Fleet) refreshHealth(ctx context.Context, sys *domain.System) {
	problem := false
	if hb, err := f.telemetry.LatestHeartbeat(ctx, sys.ID); err == nil {
		problem = hb.Problem
	}
	sys.Health = domain.EvaluateHealth(sys.LastCheckIn, f.now().UTC(), f.heartbeatInterval, problem)
}

// --- Enrollment ---

// IssueEnrollmentToken mints a single-use token bound to a tenant (and
// optionally a pre-registered system). It returns the plaintext exactly once;
// only the hash is persisted.
func (f *Fleet) IssueEnrollmentToken(ctx context.Context, tenantID, systemID string, ttl time.Duration) (plaintext string, tok *domain.EnrollmentToken, err error) {
	if tenantID == "" {
		return "", nil, fmt.Errorf("%w: tenant_id is required", domain.ErrValidation)
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	secret, err := randomSecret(32)
	if err != nil {
		return "", nil, err
	}
	now := f.now().UTC()
	tok = &domain.EnrollmentToken{
		ID:        f.id(),
		TenantID:  tenantID,
		SystemID:  systemID,
		Hash:      hashSecret(secret),
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	if err := f.tokens.Create(ctx, tok); err != nil {
		return "", nil, err
	}
	return secret, tok, nil
}

// EnrollResult carries the credentials handed back to an agent once.
type EnrollResult struct {
	System *domain.System
	APIKey string // plaintext bearer key, shown once
}

// Enroll redeems an enrollment token and binds the calling agent to a system,
// returning a bearer API key the agent stores locally. If the token was
// pre-bound to a system, that system is used; otherwise the agent supplies its
// desired FQDN and a matching pending system is claimed.
func (f *Fleet) Enroll(ctx context.Context, tokenPlaintext, fqdn string) (*EnrollResult, error) {
	tok, err := f.tokens.GetByHash(ctx, hashSecret(tokenPlaintext))
	if err != nil {
		return nil, domain.ErrEnrollmentToken
	}
	now := f.now().UTC()
	if !tok.Active(now) {
		return nil, domain.ErrEnrollmentToken
	}

	// Resolve the target system.
	var sys *domain.System
	if tok.SystemID != "" {
		sys, err = f.systems.Get(ctx, tok.TenantID, tok.SystemID)
	} else {
		sys, err = f.systems.GetByFQDN(ctx, tok.TenantID, fqdn)
	}
	if err != nil {
		return nil, err
	}
	if sys.Lifecycle == domain.LifecycleRetired || sys.Lifecycle == domain.LifecycleQuarantine {
		return nil, fmt.Errorf("%w: system is %s", domain.ErrValidation, sys.Lifecycle)
	}

	// Mint the agent credential.
	apiKey, err := randomSecret(32)
	if err != nil {
		return nil, err
	}
	identity := &domain.AgentIdentity{
		SystemID:   sys.ID,
		TenantID:   sys.TenantID,
		APIKeyHash: hashSecret(apiKey),
		CreatedAt:  now,
	}
	if err := f.identities.Create(ctx, identity); err != nil {
		return nil, err
	}

	// Consume the token and advance the system lifecycle.
	tok.ConsumedAt = &now
	if err := f.tokens.Update(ctx, tok); err != nil {
		return nil, err
	}
	sys.Lifecycle = domain.LifecycleEnrolled
	sys.UpdatedAt = now
	if err := f.systems.Update(ctx, sys); err != nil {
		return nil, err
	}
	return &EnrollResult{System: sys, APIKey: apiKey}, nil
}

// AuthenticateAgent resolves an agent identity from a presented bearer key,
// using a constant-time comparison at the hash level via the store lookup.
func (f *Fleet) AuthenticateAgent(ctx context.Context, apiKey string) (*domain.AgentIdentity, error) {
	if apiKey == "" {
		return nil, domain.ErrUnauthorized
	}
	return f.identities.GetByAPIKeyHash(ctx, hashSecret(apiKey))
}

// --- Telemetry ingestion ---

// RecordHeartbeat stores a heartbeat and advances the system's last-check-in and
// health. The caller must have already authenticated the agent and confirmed it
// owns systemID.
func (f *Fleet) RecordHeartbeat(ctx context.Context, tenantID string, hb *domain.Heartbeat) error {
	sys, err := f.systems.Get(ctx, tenantID, hb.SystemID)
	if err != nil {
		return err
	}
	if sys.Lifecycle == domain.LifecycleQuarantine {
		return fmt.Errorf("%w: system is quarantined", domain.ErrValidation)
	}
	now := f.now().UTC()
	hb.SentAt = now
	if err := f.telemetry.AppendHeartbeat(ctx, hb); err != nil {
		return err
	}
	sys.LastCheckIn = &now
	sys.AgentVersion = hb.AgentVersion
	sys.Health = domain.EvaluateHealth(sys.LastCheckIn, now, f.heartbeatInterval, hb.Problem)
	sys.UpdatedAt = now
	return f.systems.Update(ctx, sys)
}

// RecordInventory stores an inventory snapshot and back-fills the derived facts
// (arch, OS) onto the system record for cheap querying.
func (f *Fleet) RecordInventory(ctx context.Context, tenantID string, inv *domain.Inventory) error {
	sys, err := f.systems.Get(ctx, tenantID, inv.SystemID)
	if err != nil {
		return err
	}
	inv.CollectedAt = f.now().UTC()
	if err := f.telemetry.PutInventory(ctx, inv); err != nil {
		return err
	}
	sys.Arch = inv.OS.Arch
	sys.OS = osLabel(inv.OS)
	sys.UpdatedAt = f.now().UTC()
	return f.systems.Update(ctx, sys)
}

// GetInventory returns the latest inventory snapshot for a system.
func (f *Fleet) GetInventory(ctx context.Context, tenantID, systemID string) (*domain.Inventory, error) {
	if _, err := f.systems.Get(ctx, tenantID, systemID); err != nil {
		return nil, err
	}
	return f.telemetry.GetInventory(ctx, systemID)
}

// LatestMetrics returns the most recent heartbeat (metrics + self-assessment)
// for a system, scoped to the tenant. Used by the CLI `monitor` command.
func (f *Fleet) LatestMetrics(ctx context.Context, tenantID, systemID string) (*domain.Heartbeat, error) {
	if _, err := f.systems.Get(ctx, tenantID, systemID); err != nil {
		return nil, err
	}
	return f.telemetry.LatestHeartbeat(ctx, systemID)
}

func osLabel(os domain.OSInfo) string {
	if os.Distro != "" {
		if os.Version != "" {
			return os.Distro + " " + os.Version
		}
		return os.Distro
	}
	return os.Platform
}

// --- crypto helpers ---

func randomSecret(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func randomID() string {
	// 128 bits of randomness rendered as hex; collision-safe for our scale.
	s, err := randomSecret(16)
	if err != nil {
		// rand.Read failing is catastrophic and effectively never happens on a
		// healthy host; panicking surfaces it loudly rather than minting weak IDs.
		panic(fmt.Sprintf("armada: entropy source unavailable: %v", err))
	}
	return s
}

func hashSecret(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// ConstantTimeEqual compares two secrets without leaking timing information. It
// is exported for use by transport-layer auth that needs to compare raw values.
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
