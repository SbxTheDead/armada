package domain

import "time"

// ApprovalPolicy decides what happens to a device the moment it joins with a
// reusable join token.
type ApprovalPolicy string

const (
	// ApprovalAuto activates the device immediately (lifecycle enrolled).
	ApprovalAuto ApprovalPolicy = "auto"
	// ApprovalManual parks the device as pending until an operator approves it.
	ApprovalManual ApprovalPolicy = "manual"
)

// JoinToken is a reusable, tenant-scoped bootstrap key. Unlike EnrollmentToken
// (single-use, one device), a JoinToken can bind many devices over its whole
// life, which is what enables zero-touch fleet onboarding: bake one key into a
// cloud-init snippet or IoT image and every device that runs the installer
// self-registers.
//
// Security:
//   - Only a hash of the key is stored; the plaintext is shown once at creation.
//   - Revocable at any time. Optional expiry and max-uses caps limit blast
//     radius if a key leaks. By default (both zero/nil) a key never expires and
//     has unlimited uses.
//
// Devices that join with a key inherit its grouping presets, so a key can mean
// "everything installed with this belongs to project X, region eu, tagged iot".
type JoinToken struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Name     string `json:"name,omitempty"`
	Hash     string `json:"-"` // never serialized

	// Grouping presets applied to every device that joins with this key.
	Project     string   `json:"project,omitempty"`
	Region      string   `json:"region,omitempty"`
	Environment string   `json:"environment,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	Tags        []string `json:"tags,omitempty"`

	Approval ApprovalPolicy `json:"approval"`

	MaxUses   int        `json:"max_uses"` // 0 = unlimited
	Uses      int        `json:"uses"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // nil = never
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Active reports whether the key may still bind a device at time now.
func (t JoinToken) Active(now time.Time) bool {
	if t.RevokedAt != nil {
		return false
	}
	if t.ExpiresAt != nil && !now.Before(*t.ExpiresAt) {
		return false
	}
	if t.MaxUses > 0 && t.Uses >= t.MaxUses {
		return false
	}
	return true
}

// DeviceFacts is the self-reported identity a joining agent presents. MachineID
// is a stable, opaque per-host identifier (derived from the OS machine id) used
// to deduplicate: re-running the installer on the same box re-attaches to its
// existing System instead of creating a second one.
type DeviceFacts struct {
	MachineID    string `json:"machine_id"`
	Hostname     string `json:"hostname"`
	FQDN         string `json:"fqdn"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	AgentVersion string `json:"agent_version"`
}
