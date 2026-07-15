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
	"errors"
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
	joinTokens store.JoinTokenStore
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
func NewFleet(systems store.SystemStore, joinTokens store.JoinTokenStore, identities store.IdentityStore, telemetry store.TelemetryStore, opts Options) *Fleet {
	f := &Fleet{
		systems:           systems,
		joinTokens:        joinTokens,
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

// --- Reusable join tokens (zero-touch onboarding) ---

// JoinResult carries the credentials handed back to an agent when it joins.
type JoinResult struct {
	System *domain.System
	APIKey string // plaintext bearer key, shown once
}

// NewJoinTokenInput describes a reusable join key. Zero MaxUses means unlimited;
// zero TTL means the key never expires — the "generate once, works forever"
// default.
type NewJoinTokenInput struct {
	TenantID    string
	Name        string
	Project     string
	Region      string
	Environment string
	Provider    string
	Tags        []string
	Approval    domain.ApprovalPolicy
	MaxUses     int
	TTL         time.Duration
}

// CreateJoinToken mints a reusable join key, returning the plaintext once.
func (f *Fleet) CreateJoinToken(ctx context.Context, in NewJoinTokenInput) (plaintext string, tok *domain.JoinToken, err error) {
	if in.TenantID == "" {
		return "", nil, fmt.Errorf("%w: tenant_id is required", domain.ErrValidation)
	}
	approval := in.Approval
	if approval == "" {
		approval = domain.ApprovalAuto
	}
	if approval != domain.ApprovalAuto && approval != domain.ApprovalManual {
		return "", nil, fmt.Errorf("%w: approval must be 'auto' or 'manual'", domain.ErrValidation)
	}
	secret, err := randomSecret(32)
	if err != nil {
		return "", nil, err
	}
	now := f.now().UTC()
	tok = &domain.JoinToken{
		ID:          f.id(),
		TenantID:    in.TenantID,
		Name:        in.Name,
		Hash:        hashSecret(secret),
		Project:     in.Project,
		Region:      in.Region,
		Environment: in.Environment,
		Provider:    in.Provider,
		Tags:        in.Tags,
		Approval:    approval,
		MaxUses:     in.MaxUses,
		CreatedAt:   now,
	}
	if in.TTL > 0 {
		exp := now.Add(in.TTL)
		tok.ExpiresAt = &exp
	}
	if err := f.joinTokens.Create(ctx, tok); err != nil {
		return "", nil, err
	}
	return secret, tok, nil
}

// ListJoinTokens returns a tenant's join keys (without plaintext).
func (f *Fleet) ListJoinTokens(ctx context.Context, tenantID string) ([]*domain.JoinToken, error) {
	return f.joinTokens.List(ctx, tenantID)
}

// RevokeJoinToken permanently disables a join key.
func (f *Fleet) RevokeJoinToken(ctx context.Context, tenantID, id string) error {
	tok, err := f.joinTokens.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if tok.RevokedAt != nil {
		return nil // already revoked; idempotent
	}
	now := f.now().UTC()
	tok.RevokedAt = &now
	return f.joinTokens.Update(ctx, tok)
}

// JoinWithToken is the zero-touch onboarding path: an agent presents a reusable
// join key plus its self-reported facts, and the system is found-or-created,
// grouped per the key's presets, and issued a bearer API key. It is idempotent
// on MachineID, so re-running the installer on the same host re-attaches to the
// existing System rather than duplicating it.
func (f *Fleet) JoinWithToken(ctx context.Context, joinKey string, facts domain.DeviceFacts) (*JoinResult, error) {
	if joinKey == "" {
		return nil, domain.ErrJoinToken
	}
	tok, err := f.joinTokens.GetByHash(ctx, hashSecret(joinKey))
	if err != nil {
		return nil, domain.ErrJoinToken
	}
	now := f.now().UTC()
	if !tok.Active(now) {
		return nil, domain.ErrJoinToken
	}

	// Find an existing system for this machine (dedupe), else create one.
	sys, err := f.findDeviceSystem(ctx, tok.TenantID, facts)
	switch {
	case err == nil:
		if sys.Lifecycle == domain.LifecycleRetired || sys.Lifecycle == domain.LifecycleQuarantine {
			return nil, fmt.Errorf("%w: system is %s", domain.ErrValidation, sys.Lifecycle)
		}
		f.applyFacts(sys, facts, now)
		if err := f.systems.Update(ctx, sys); err != nil {
			return nil, err
		}
	case errors.Is(err, domain.ErrNotFound):
		sys = f.newDeviceSystem(tok, facts, now)
		if err := f.systems.Create(ctx, sys); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}

	// Issue (or rotate) the agent credential.
	apiKey, err := randomSecret(32)
	if err != nil {
		return nil, err
	}
	if err := f.identities.Create(ctx, &domain.AgentIdentity{
		SystemID:   sys.ID,
		TenantID:   sys.TenantID,
		APIKeyHash: hashSecret(apiKey),
		CreatedAt:  now,
	}); err != nil {
		return nil, err
	}

	// Count the use.
	tok.Uses++
	if err := f.joinTokens.Update(ctx, tok); err != nil {
		return nil, err
	}
	return &JoinResult{System: sys, APIKey: apiKey}, nil
}

// findDeviceSystem resolves a joining device to an existing system, preferring
// the stable machine id and falling back to FQDN.
func (f *Fleet) findDeviceSystem(ctx context.Context, tenantID string, facts domain.DeviceFacts) (*domain.System, error) {
	if facts.MachineID != "" {
		if sys, err := f.systems.GetByMachineID(ctx, tenantID, facts.MachineID); err == nil {
			return sys, nil
		}
	}
	if facts.FQDN != "" {
		return f.systems.GetByFQDN(ctx, tenantID, facts.FQDN)
	}
	return nil, domain.ErrNotFound
}

// newDeviceSystem builds a System from a join token's presets and the device's
// facts. Auto-approval activates it immediately; manual leaves it pending.
func (f *Fleet) newDeviceSystem(tok *domain.JoinToken, facts domain.DeviceFacts, now time.Time) *domain.System {
	lifecycle := domain.LifecycleEnrolled
	if tok.Approval == domain.ApprovalManual {
		lifecycle = domain.LifecyclePending
	}
	name := facts.Hostname
	if name == "" {
		name = facts.FQDN
	}
	sys := &domain.System{
		ID:          f.id(),
		TenantID:    tok.TenantID,
		Name:        name,
		FQDN:        facts.FQDN,
		MachineID:   facts.MachineID,
		Project:     tok.Project,
		Region:      tok.Region,
		Environment: tok.Environment,
		Provider:    tok.Provider,
		Tags:        tok.Tags,
		Lifecycle:   lifecycle,
		Health:      domain.HealthUnknown,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	f.applyFacts(sys, facts, now)
	return sys
}

// applyFacts refreshes the reported fields on a system from a device report.
func (f *Fleet) applyFacts(sys *domain.System, facts domain.DeviceFacts, now time.Time) {
	if facts.MachineID != "" {
		sys.MachineID = facts.MachineID
	}
	if facts.FQDN != "" {
		sys.FQDN = facts.FQDN
	}
	if facts.Arch != "" {
		sys.Arch = facts.Arch
	}
	if facts.OS != "" {
		sys.OS = facts.OS
	}
	if facts.AgentVersion != "" {
		sys.AgentVersion = facts.AgentVersion
	}
	sys.UpdatedAt = now
}

// ApproveSystem activates a device that joined under a manual-approval key.
func (f *Fleet) ApproveSystem(ctx context.Context, tenantID, id string) (*domain.System, error) {
	sys, err := f.systems.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if sys.Lifecycle == domain.LifecyclePending {
		sys.Lifecycle = domain.LifecycleEnrolled
		sys.UpdatedAt = f.now().UTC()
		if err := f.systems.Update(ctx, sys); err != nil {
			return nil, err
		}
	}
	return sys, nil
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
