//go:build linux

package agent

import (
	"os"
	"strings"
)

// osMachineID reads the systemd/D-Bus machine id, a stable 128-bit host
// identifier present on virtually all modern Linux systems.
func osMachineID() string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				return s
			}
		}
	}
	return ""
}
