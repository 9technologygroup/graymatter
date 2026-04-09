//go:build !windows

package main

import "syscall"

// detachSysProcAttr returns a SysProcAttr that starts the child process in
// a new session (Setsid), detaching it from the parent's controlling terminal.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
