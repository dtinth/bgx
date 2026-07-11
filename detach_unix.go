//go:build !windows

package main

import "syscall"

// daemonSysProcAttr returns the attributes used to detach the background daemon
// from the launching process. Setsid starts a new session so the daemon
// survives the parent shell (or CI step) exiting.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
