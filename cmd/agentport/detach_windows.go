//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachedProcess is the Win32 DETACHED_PROCESS creation flag, which the syscall
// package does not export.
const detachedProcess = 0x00000008

// detachProcess starts the child without a console and in its own process
// group, so a Ctrl+C in the caller's console does not reach it.
func detachProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess}
}
