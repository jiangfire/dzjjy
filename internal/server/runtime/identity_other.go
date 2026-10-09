//go:build !linux && !windows

package runtime

import "fmt"

// ProcessStartIDFor is unavailable on platforms without a cheap process
// identity source; restore treats such processes as unverified.
func ProcessStartIDFor(pid int) (string, error) {
	return "", fmt.Errorf("process identity is unavailable on this platform (pid %d)", pid)
}

// NewProcessIdentity has no identity source on this platform.
func NewProcessIdentity(pid int) string { return "" }
