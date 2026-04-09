//go:build windows

package main

import "syscall"

const (
	detachedProcess    = 0x00000008
	createNewProcGroup = 0x00000200
)

// detachSysProcAttr returns a SysProcAttr that starts the child process
// detached from the parent console (DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP).
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcGroup,
		HideWindow:    true,
	}
}
