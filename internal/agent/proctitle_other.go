//go:build !linux

package agent

// setProcTitle is a no-op on non-Linux platforms. Process-title rewriting is
// platform-specific and only wired up for Linux (where htop/ps/top read it from
// /proc). On Windows and macOS the process shows its normal image name.
func setProcTitle(string) {}
