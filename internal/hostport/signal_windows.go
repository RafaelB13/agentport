//go:build windows

package hostport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
)

const (
	// processQueryLimitedInformation is the least access that lets GetExitCodeProcess work.
	processQueryLimitedInformation = 0x1000
	// stillActive is STILL_ACTIVE: the exit code of a process that has not exited yet.
	stillActive = 259
)

// newSignaler returns the Windows signaler, which stops processes with taskkill.
func newSignaler(run runner) signaler { return winSignaler{run: run} }

// winSignaler has no Unix signals on Windows. Signal maps SIGTERM to a close
// request (`taskkill /PID n`, no /F) and SIGKILL to `taskkill /F /PID n`. A close
// request that taskkill refuses is not an error here: terminate waits the grace
// period and then escalates to SIGKILL, the same as on Unix.
type winSignaler struct{ run runner }

func (s winSignaler) Signal(pid int, sig syscall.Signal) error {
	args := taskkillArgs(pid, sig)
	out, err := s.run(context.Background(), "taskkill", args...)
	if err != nil && sig == syscall.SIGKILL {
		return fmt.Errorf("taskkill %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Alive reports whether pid is still running. Process.Signal cannot probe a process
// on Windows, so it asks the kernel for the exit code instead.
func (winSignaler) Alive(pid int) bool {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		// The process exists but may not be queried: treat it as alive, so a Stop
		// reports that it is still running rather than claiming success.
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}
