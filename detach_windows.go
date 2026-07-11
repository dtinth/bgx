//go:build windows

package main

import "syscall"

// Process creation flags (from the Windows API). DETACHED_PROCESS runs the
// daemon without a console, and CREATE_NEW_PROCESS_GROUP puts it in its own
// group so it is not signalled when the launching process (or CI step) exits.
const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// daemonSysProcAttr returns the attributes used to detach the background daemon
// from the launching process so it survives the parent shell (or CI step)
// exiting.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
}
