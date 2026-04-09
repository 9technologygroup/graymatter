//go:build windows

package harness

import (
	"fmt"
	"os"
)

// killPID terminates the process with the given PID via os.Process.Kill().
// On Windows this calls TerminateProcess under the hood.
// Returns an error if the process is not found or termination fails.
func killPID(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID %d", pid)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := p.Kill(); err != nil {
		return fmt.Errorf("kill process %d: %w", pid, err)
	}
	return nil
}
