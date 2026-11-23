//go:build windows

package companion

import "os/exec"

// terminalSupported is false: see startTerminal.
func terminalSupported() bool { return false }

// startTerminal is not available on Windows: the server terminal needs a
// pseudo-terminal, and this build does not attach one (no ConPTY).
func startTerminal(*exec.Cmd, int, int) (terminal, error) {
	return nil, errTerminalUnsupported
}
