//go:build !windows

package hostport

import (
	"os"
	"syscall"
)

// newSignaler returns the Unix signaler; run is only used on Windows.
func newSignaler(runner) signaler { return osSignaler{} }

// osSignaler sends real Unix signals with os.Process.
type osSignaler struct{}

func (osSignaler) Signal(pid int, sig syscall.Signal) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(sig)
}

func (osSignaler) Alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
