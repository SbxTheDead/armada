package domain

import "time"

// EnrollmentToken is a short-lived, single-use secret an operator hands to a
// host so its agent can bind itself to a pre-registered System. Tokens are
// scoped to a tenant and, optionally, to a specific system.
//
// Security notes:
//   - Only a hash of the token is stored; the plaintext is shown to the
//     operator exactly once at creation time.
//   - Tokens expire and are consumed on first successful enrollment.
type EnrollmentToken struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	SystemID   string     `json:"system_id,omitempty"` // optional pre-binding
	Hash       string     `json:"-"`                   // never serialized
	ExpiresAt  time.Time  `json:"expires_at"`
	ConsumedAt *time.Time `json:"consumed_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Active reports whether the token may still be redeemed at time now.
func (t EnrollmentToken) Active(now time.Time) bool {
	if t.RevokedAt != nil || t.ConsumedAt != nil {
		return false
	}
	return now.Before(t.ExpiresAt)
}

// AgentIdentity is the credential material an enrolled agent uses to
// authenticate subsequent requests (heartbeat, inventory, job results). The
// APIKeyHash is a hash of a bearer secret returned to the agent once at
// enrollment. In production this is superseded by mutual TLS client certs; the
// bearer key is the bootstrap path.
type AgentIdentity struct {
	SystemID   string    `json:"system_id"`
	TenantID   string    `json:"tenant_id"`
	APIKeyHash string    `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
}

// Heartbeat is the periodic liveness + telemetry beacon an agent sends. It is
// deliberately small: rich inventory is uploaded separately and less often.
type Heartbeat struct {
	SystemID     string    `json:"system_id"`
	AgentVersion string    `json:"agent_version"`
	Uptime       int64     `json:"uptime_seconds"`
	Metrics      Metrics   `json:"metrics"`
	Problem      bool      `json:"problem"` // agent-side self-assessment
	Message      string    `json:"message,omitempty"`
	SentAt       time.Time `json:"sent_at"`
}

// Metrics is the point-in-time resource snapshot carried on each heartbeat.
type Metrics struct {
	CPUPercent     float64 `json:"cpu_percent"`
	MemUsedBytes   uint64  `json:"mem_used_bytes"`
	MemTotalBytes  uint64  `json:"mem_total_bytes"`
	DiskUsedBytes  uint64  `json:"disk_used_bytes"`
	DiskTotalBytes uint64  `json:"disk_total_bytes"`
	Load1          float64 `json:"load1"`
	NetRxBytes     uint64  `json:"net_rx_bytes"`
	NetTxBytes     uint64  `json:"net_tx_bytes"`
	TempCelsius    float64 `json:"temp_celsius,omitempty"`
}

// Inventory is the periodic, detailed hardware/software snapshot. Every field is
// read-only observation; the agent never mutates the host to produce it.
type Inventory struct {
	SystemID    string            `json:"system_id"`
	CollectedAt time.Time         `json:"collected_at"`
	Hostname    string            `json:"hostname"`
	OS          OSInfo            `json:"os"`
	CPU         CPUInfo           `json:"cpu"`
	MemoryBytes uint64            `json:"memory_bytes"`
	Disks       []DiskInfo        `json:"disks,omitempty"`
	Interfaces  []NetInterface    `json:"interfaces,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
}

type OSInfo struct {
	Platform string `json:"platform"` // e.g. "linux", "windows", "darwin"
	Distro   string `json:"distro,omitempty"`
	Version  string `json:"version,omitempty"`
	Kernel   string `json:"kernel,omitempty"`
	Arch     string `json:"arch"`
}

type CPUInfo struct {
	Model   string `json:"model,omitempty"`
	Arch    string `json:"arch"`
	Cores   int    `json:"cores"`
	Threads int    `json:"threads"`
}

type DiskInfo struct {
	Device     string `json:"device"`
	Mountpoint string `json:"mountpoint,omitempty"`
	FSType     string `json:"fstype,omitempty"`
	TotalBytes uint64 `json:"total_bytes"`
}

type NetInterface struct {
	Name  string   `json:"name"`
	MAC   string   `json:"mac,omitempty"`
	Addrs []string `json:"addrs,omitempty"`
}
