//go:build windows

package runtime

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// newSysProcAttr keeps default spawning behavior on Windows; tree cleanup is
// handled by taskkill /T instead of process groups.
func newSysProcAttr() *syscall.SysProcAttr { return nil }

// ownsProcessGroups reports whether spawned children become process-group
// leaders; Windows uses taskkill /T for tree cleanup instead.
func ownsProcessGroups() bool { return false }

// killProcessTree terminates the process and all of its descendants via
// taskkill; if that fails, fall back to a direct kill whose error tolerance
// is handled by the caller.
func killProcessTree(pid int, proc *os.Process, ownsGroup bool) error {
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F") // #nosec G204 -- pid is a validated integer
	if err := cmd.Run(); err != nil {
		return proc.Kill()
	}
	return nil
}
