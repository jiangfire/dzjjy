//go:build !windows

package runtime

import (
	"os"
	"syscall"
)

// newSysProcAttr makes each managed process its own process group so stop can
// recycle the whole tree instead of only the main process.
func newSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// ownsProcessGroups reports whether spawned children become process-group
// leaders that can be signalled as a group.
func ownsProcessGroups() bool { return true }

// killProcessTree terminates the process group we created (and therefore all
// of its descendants). For adopted processes we never owned a group, so only
// the main process is signalled.
func killProcessTree(pid int, proc *os.Process, ownsGroup bool) error {
	if ownsGroup {
		if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil {
			return nil
		}
		// The group may already be gone; fall through to a direct kill.
	}
	return proc.Kill()
}
