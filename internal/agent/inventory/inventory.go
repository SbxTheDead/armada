// Package inventory collects a read-only snapshot of the host it runs on. Every
// function here only observes: it reads runtime facts, environment, and (on
// Linux) well-known procfs/sysfs paths. It never modifies the host, never reads
// credentials, and never escalates privileges. Fields that require elevated
// access simply stay empty rather than attempting to obtain it.
package inventory

import (
	"bufio"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
)

// Collect gathers a best-effort inventory snapshot. It degrades gracefully:
// anything it cannot read on the current OS is left at its zero value.
func Collect() domain.Inventory {
	hostname, _ := os.Hostname()
	inv := domain.Inventory{
		CollectedAt: time.Now().UTC(),
		Hostname:    hostname,
		OS: domain.OSInfo{
			Platform: runtime.GOOS,
			Arch:     runtime.GOARCH,
		},
		CPU: domain.CPUInfo{
			Arch:    runtime.GOARCH,
			Threads: runtime.NumCPU(),
		},
		Interfaces: collectInterfaces(),
	}
	enrichOS(&inv.OS)
	return inv
}

// enrichOS fills distro/version/kernel where cheaply available. On Linux this
// reads /etc/os-release (a public, non-sensitive file); on other platforms it
// leaves the fields for the platform-specific implementation to fill.
func enrichOS(os *domain.OSInfo) {
	if runtime.GOOS != "linux" {
		return
	}
	f, err := openFile("/etc/os-release")
	if err != nil {
		return
	}
	defer f.Close()

	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		key := line[:i]
		val := strings.Trim(line[i+1:], `"`)
		kv[key] = val
	}
	if name := kv["NAME"]; name != "" {
		os.Distro = name
	}
	if ver := kv["VERSION_ID"]; ver != "" {
		os.Version = ver
	}
}

// collectInterfaces enumerates network interfaces via the standard library
// (no raw sockets, no packet capture). MAC and IPs are inventory facts.
func collectInterfaces() []domain.NetInterface {
	ifaces, err := netInterfaces()
	if err != nil {
		return nil
	}
	var out []domain.NetInterface
	for _, ifc := range ifaces {
		ni := domain.NetInterface{Name: ifc.Name, MAC: ifc.HardwareAddr}
		ni.Addrs = ifc.Addrs
		out = append(out, ni)
	}
	return out
}

// openFile is a thin indirection so tests can stub filesystem reads.
var openFile = func(path string) (*os.File, error) { return os.Open(path) }
