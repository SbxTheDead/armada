//go:build !linux && !windows

package agent

// osMachineID has no portable source on macOS/BSD here, so the common
// machineID() falls back to a hostname-derived identifier. (A darwin build can
// later read IOPlatformUUID via ioreg.)
func osMachineID() string { return "" }
