//go:build !windows

package companion

import (
	"os/exec"

	"github.com/creack/pty"
)

// terminalSupported reports whether this platform can attach a server terminal.
func terminalSupported() bool { return true }

// startTerminal runs command attached to a new pseudo-terminal of the given size.
func startTerminal(command *exec.Cmd, cols, rows int) (terminal, error) {
	file, err := pty.StartWithSize(command, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &processTerminal{
		cmd: command,
		pty: file,
		resize: func(cols, rows int) error {
			return pty.Setsize(file, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
		},
	}, nil
}
