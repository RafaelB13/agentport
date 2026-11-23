//go:build windows

package forward

import (
	"os/exec"
	"syscall"
)

// hideConsole stops the ssh child from opening a console window while the Companion runs.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// resolveCommand finds ssh.exe on PATH, so a missing OpenSSH fails with a clear error.
func resolveCommand(name string) (string, error) {
	return exec.LookPath(name)
}
