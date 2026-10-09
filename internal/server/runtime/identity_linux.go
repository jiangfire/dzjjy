//go:build linux

package runtime

import (
	"fmt"
	"os"
	"strings"
)

// ProcessStartIDFor returns the kernel start time of the process (field 22 of
// /proc/<pid>/stat) as a stable identity. A PID that the OS reused for another
// process yields a different value, which is how restore detects misadoption.
func ProcessStartIDFor(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	// The comm field can contain spaces and parentheses; fields resume after the last ')'.
	content := string(data)
	idx := strings.LastIndexByte(content, ')')
	if idx < 0 || idx+2 > len(content) {
		return "", fmt.Errorf("malformed stat entry for pid %d", pid)
	}
	fields := strings.Fields(content[idx+2:])
	// fields[0] is field 3 (state); field 22 (starttime) is fields[19].
	if len(fields) < 20 {
		return "", fmt.Errorf("unexpected stat layout for pid %d", pid)
	}
	return fields[19], nil
}

// NewProcessIdentity returns a low-cost identity for a process we just
// spawned; on Linux the /proc start time is cheap to read.
func NewProcessIdentity(pid int) string {
	id, err := ProcessStartIDFor(pid)
	if err != nil {
		return ""
	}
	return id
}
