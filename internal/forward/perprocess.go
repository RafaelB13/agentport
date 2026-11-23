package forward

import (
	"context"
	"fmt"
	"os/exec"
)

// Process is a running `ssh -N -L ...` process owned by a perProcess forward.
type Process interface {
	Kill() error
}

// Starter starts a process. It is injected so tests can fake ssh.
type Starter interface {
	Start(name string, args ...string) (Process, error)
}

// perProcess runs one `ssh -N -L` process per forward. It is the fallback for
// platforms without ControlMaster sockets (Windows).
type perProcess struct {
	*manager
	sshPath string
	host    string
	starter Starter
	procs   map[int]Process // keyed by Local; only touched while manager.ops is held
}

// NewPerProcess returns a Forwarder that starts one ssh process per forward.
func NewPerProcess(sshPath, host string, starter Starter) Forwarder {
	if starter == nil {
		starter = execStarter{}
	}
	p := &perProcess{sshPath: sshPath, host: host, starter: starter, procs: map[int]Process{}}
	p.manager = newManager(p.start, p.stop, p.restart)
	return p
}

func (p *perProcess) start(_ context.Context, f Forward) error {
	proc, err := p.starter.Start(p.sshPath, "-N", "-L", spec(f.Local, f.Remote), p.host)
	if err != nil {
		return fmt.Errorf("start ssh forward %d->%d: %w", f.Remote, f.Local, err)
	}
	p.procs[f.Local] = proc
	return nil
}

// stop kills the process for f. The handle is dropped even if Kill fails, so a
// retry of Remove does not kill the same process twice.
func (p *perProcess) stop(_ context.Context, f Forward) error {
	proc, ok := p.procs[f.Local]
	if !ok {
		return nil
	}
	delete(p.procs, f.Local)
	if err := proc.Kill(); err != nil {
		return fmt.Errorf("stop ssh forward %d->%d: %w", f.Remote, f.Local, err)
	}
	return nil
}

// restart replaces the process for f. A dead predecessor is not an error.
func (p *perProcess) restart(ctx context.Context, f Forward) error {
	_ = p.stop(ctx, f)
	return p.start(ctx, f)
}

// execStarter starts real ssh processes with os/exec.
type execStarter struct{}

type execProcess struct{ cmd *exec.Cmd }

func (execStarter) Start(name string, args ...string) (Process, error) {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execProcess{cmd: cmd}, nil
}

func (e *execProcess) Kill() error {
	if err := e.cmd.Process.Kill(); err != nil {
		return err
	}
	_ = e.cmd.Wait()
	return nil
}
