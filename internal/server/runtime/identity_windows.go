//go:build windows

package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// NewProcessIdentity returns a zero-cost identity for a process we just
// spawned: its parent PID (us). Windows keeps ParentProcessId stable after the
// parent exits, so a reused PID will report a different parent after a server
// restart and fail verification.
func NewProcessIdentity(pid int) string {
	return strconv.Itoa(os.Getpid())
}

// ProcessStartIDFor returns the parent PID recorded for an existing process,
// used at restore time to compare against the persisted identity.
func ProcessStartIDFor(pid int) (string, error) {
	query := fmt.Sprintf("(Get-CimInstance Win32_Process -Filter 'ProcessId=%d').ParentProcessId", pid)
	cmd := exec.Command("powershell", "-NoProfile", "-Command", query) // #nosec G204 -- pid is a validated integer
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("query process %d: %w", pid, err)
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", fmt.Errorf("process %d not found", pid)
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return "", fmt.Errorf("unexpected identity output for pid %d: %q", pid, value)
	}
	return value, nil
}
