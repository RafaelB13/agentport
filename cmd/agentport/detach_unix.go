//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess moves the child into a new session, so it has no controlling
// terminal and is not signalled when the caller's terminal or ssh session ends.
func detachProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
