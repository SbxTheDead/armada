package domain

import (
	"fmt"
	"strings"
	"time"
)

// Health is the coarse operational state of a managed system, derived from
// heartbeat recency and reported agent status.
type Health string

const (
	HealthUnknown  Health = "unknown"  // never checked in, or stale beyond the grace window
	HealthHealthy  Health = "healthy"  // checked in recently, agent reports OK
	HealthDegraded Health = "degraded" // checked in recently, agent reports a problem
	HealthOffline  Health = "offline"  // missed the expected heartbeat window
)

// Lifecycle tracks where a system sits in the enrollment flow.
type Lifecycle string

const (
	LifecyclePending    Lifecycle = "pending"    // enrollment token issued, agent not yet enrolled
	LifecycleEnrolled   Lifecycle = "enrolled"   // agent has a live identity
	LifecycleQuarantine Lifecycle = "quarantine" // administratively isolated; jobs are refused
	LifecycleRetired    Lifecycle = "retired"    // decommissioned, kept for audit history
)

// System is the central aggregate: a single machine (physical, VM, container
// host, SBC, or network appliance) that the platform is authorized to manage.
//
// A System is always scoped to a TenantID for multi-tenant isolation. Grouping
// dimensions (Project, Region, Environment, Provider, Tags, Labels) are plain
// attributes so that the same machine can be sliced along many axes without a
// rigid hierarchy.
type System struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`

	Name string `json:"name"`
	FQDN string `json:"fqdn"`

	// MachineID is a stable, opaque per-host identifier used to deduplicate a
	// device across re-installs, reboots, and IP changes. Empty for systems
	// registered manually by an operator before any agent has joined.
	MachineID string `json:"machine_id,omitempty"`

	// Grouping dimensions.
	Project     string            `json:"project,omitempty"`
	Region      string            `json:"region,omitempty"`
	Environment string            `json:"environment,omitempty"`
	Provider    string            `json:"provider,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`

	// Reported facts (populated from inventory once the agent enrolls).
	Arch         string `json:"arch,omitempty"`
	OS           string `json:"os,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`

	Lifecycle   Lifecycle  `json:"lifecycle"`
	Health      Health     `json:"health"`
	LastCheckIn *time.Time `json:"last_check_in,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewSystemInput is the validated data required to register a system. The
// agent identity and generated fields (ID, timestamps) are assigned by the
// service, never supplied by the caller.
type NewSystemInput struct {
	TenantID    string
	Name        string
	FQDN        string
	Project     string
	Region      string
	Environment string
	Provider    string
	Tags        []string
	Labels      map[string]string
}

// Validate enforces the invariants that must hold for any system, regardless of
// storage backend. It returns an error wrapping ErrValidation on the first
// violation so transport layers can map it to a 4xx.
func (in NewSystemInput) Validate() error {
	if strings.TrimSpace(in.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if strings.TrimSpace(in.FQDN) == "" {
		return fmt.Errorf("%w: fqdn is required", ErrValidation)
	}
	if len(in.Name) > 253 {
		return fmt.Errorf("%w: name exceeds 253 characters", ErrValidation)
	}
	if len(in.FQDN) > 253 {
		return fmt.Errorf("%w: fqdn exceeds 253 characters", ErrValidation)
	}
	for _, t := range in.Tags {
		if strings.TrimSpace(t) == "" {
			return fmt.Errorf("%w: tags must not be empty", ErrValidation)
		}
	}
	return nil
}

// EvaluateHealth derives Health from heartbeat recency. interval is the agent's
// configured heartbeat period; a system is considered offline once it has
// missed a small multiple of that interval. reportedProblem reflects the most
// recent heartbeat's self-assessment.
func EvaluateHealth(lastCheckIn *time.Time, now time.Time, interval time.Duration, reportedProblem bool) Health {
	if lastCheckIn == nil {
		return HealthUnknown
	}
	// Grace: allow up to 3 missed intervals before declaring offline, to absorb
	// transient network blips without flapping.
	deadline := lastCheckIn.Add(3 * interval)
	if now.After(deadline) {
		return HealthOffline
	}
	if reportedProblem {
		return HealthDegraded
	}
	return HealthHealthy
}
