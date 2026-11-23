//go:build windows

package hostport

import (
	"os/exec"
	"syscall"
)

// hideConsole stops a child process from opening a console window while the Companion runs.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
