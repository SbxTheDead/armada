package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// machineID returns a stable, opaque per-host identifier used by the control
// plane to deduplicate a device across re-installs, reboots, and IP changes.
//
// It derives from the OS machine id where available (platform-specific), and
// falls back to the hostname when none is exposed. The raw value is hashed so
// the wire identifier is opaque and the underlying OS GUID is never sent as-is.
func machineID() string {
	raw := osMachineID()
	if raw == "" {
		host, _ := os.Hostname()
		raw = "host:" + host
	}
	sum := sha256.Sum256([]byte("armada-machine:" + raw))
	return hex.EncodeToString(sum[:])
}
