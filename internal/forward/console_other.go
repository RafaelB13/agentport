//go:build !windows

package forward

import "os/exec"

// hideConsole has nothing to hide on Unix.
func hideConsole(*exec.Cmd) {}

// resolveCommand returns name unchanged; exec resolves it on PATH when it starts.
func resolveCommand(name string) (string, error) { return name, nil }
