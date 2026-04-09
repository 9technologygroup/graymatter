//go:build !windows

package harness

import (
	"fmt"
	"os"
	"syscall"
)

// killPID sends SIGTERM to the process with the given PID.
// Returns an error if the process is not found or the signal fails.
func killPID(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID %d", pid)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal process %d: %w", pid, err)
	}
	return nil
}
