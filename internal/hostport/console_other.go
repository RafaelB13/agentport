//go:build !windows

package hostport

import "os/exec"

// hideConsole has nothing to hide on Unix.
func hideConsole(*exec.Cmd) {}
