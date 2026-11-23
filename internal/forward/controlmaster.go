package forward

import (
	"context"
	"fmt"
	"strings"
)

// controlMaster drives forwards through an existing OpenSSH ControlMaster
// connection with `ssh -S <controlPath> -O forward|cancel`.
type controlMaster struct {
	*manager
	sshPath     string
	controlPath string
	host        string
	run         Runner
}

// NewControlMaster returns a Forwarder that uses the ControlMaster socket at
// controlPath. A nil run uses os/exec. Windows OpenSSH has no such socket: use
// NewForwarder there, which never returns this strategy on Windows.
func NewControlMaster(sshPath, controlPath, host string, run Runner) Forwarder {
	if run == nil {
		run = execRunner
	}
	c := &controlMaster{sshPath: sshPath, controlPath: controlPath, host: host, run: run}
	c.manager = newManager(
		func(ctx context.Context, f Forward) error { return c.control(ctx, "forward", f) },
		func(ctx context.Context, f Forward) error { return c.control(ctx, "cancel", f) },
		nil,
	)
	return c
}

// control runs one `ssh -O <op>` call with the forward spec for f.
func (c *controlMaster) control(ctx context.Context, op string, f Forward) error {
	args := []string{"-S", c.controlPath, "-O", op, "-L", spec(f.Local, f.Remote), c.host}
	out, err := c.run(ctx, c.sshPath, args...)
	if err != nil {
		return fmt.Errorf("ssh -O %s %d->%d: %w: %s", op, f.Remote, f.Local, err, strings.TrimSpace(string(out)))
	}
	return nil
}
