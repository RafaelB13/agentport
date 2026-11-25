//go:build !windows

package companion

import (
	"io"
	"os/exec"
	"sync"

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

// processTerminal is a process attached to a pseudo-terminal. stop may be called
// from several paths; only the first call acts.
type processTerminal struct {
	cmd    *exec.Cmd
	once   sync.Once
	pty    io.ReadWriteCloser
	resize func(cols, rows int) error
}

func (p *processTerminal) Read(b []byte) (int, error)  { return p.pty.Read(b) }
func (p *processTerminal) Write(b []byte) (int, error) { return p.pty.Write(b) }

func (p *processTerminal) Resize(cols, rows int) error { return p.resize(cols, rows) }

// stop kills the ssh process, closes the terminal and reaps the child, so no
// process is left behind.
func (p *processTerminal) stop() {
	p.once.Do(func() {
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		_ = p.pty.Close()
		_ = p.cmd.Wait()
	})
}
