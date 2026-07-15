//go:build windows

package agent

import (
	"os/exec"
	"strings"
)

// osMachineID reads the Windows MachineGuid from the registry via reg.exe (no
// external dependency). MachineGuid is set at install time and is stable for
// the life of the OS install.
func osMachineID() string {
	out, err := exec.Command("reg", "query",
		`HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid").Output()
	if err != nil {
		return ""
	}
	// The value line looks like: "    MachineGuid    REG_SZ    <guid>".
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "MachineGuid") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				return fields[len(fields)-1]
			}
		}
	}
	return ""
}
