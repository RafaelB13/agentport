package hostport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"
)

// stopWith stops o's owner and then checks that the port is free.
//   - proc: SIGTERM, wait up to grace, then SIGKILL, wait up to grace again.
//   - service / container: run StopCmd.
//   - system or no StopCmd: ErrNotStoppable. Privileges are never escalated.
func stopWith(ctx context.Context, e env, o *Owner) error {
	if o == nil {
		return errors.New("hostport: nil owner")
	}
	switch o.Kind {
	case KindProc:
		if err := e.terminate(ctx, o.PID); err != nil {
			return err
		}
	case KindService, KindContainer:
		if len(o.StopCmd) == 0 {
			return ErrNotStoppable
		}
		if out, err := e.run(ctx, o.StopCmd[0], o.StopCmd[1:]...); err != nil {
			return fmt.Errorf("%s: %w: %s", strings.Join(o.StopCmd, " "), err, strings.TrimSpace(string(out)))
		}
	default:
		return ErrNotStoppable
	}
	if e.listening(o.Port) {
		return fmt.Errorf("port %d: %w", o.Port, ErrStillBusy)
	}
	return nil
}

// terminate sends SIGTERM, escalates to SIGKILL after e.grace, and fails if the pid survives.
func (e env) terminate(ctx context.Context, pid int) error {
	if err := e.sig.Signal(pid, syscall.SIGTERM); err != nil && e.sig.Alive(pid) {
		return fmt.Errorf("sigterm %d: %w", pid, err)
	}
	if e.waitGone(ctx, pid) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.sig.Signal(pid, syscall.SIGKILL); err != nil && e.sig.Alive(pid) {
		return fmt.Errorf("sigkill %d: %w", pid, err)
	}
	if e.waitGone(ctx, pid) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("process %d did not exit after SIGKILL", pid)
}

// waitGone polls until pid is gone or e.grace elapses. It returns true if pid is gone.
func (e env) waitGone(ctx context.Context, pid int) bool {
	deadline := time.Now().Add(e.grace)
	for e.sig.Alive(pid) {
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(e.poll):
		}
	}
	return true
}
