// Package forward manages local-port forwards from the host to 127.0.0.1 on a
// remote server, through an SSH connection the host already keeps open. It is
// self-contained: standard library only, and no other package of this repo.
package forward

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"sync"
)

var (
	// ErrInvalidPort is returned for a port outside 1..65535.
	ErrInvalidPort = errors.New("invalid port")
	// ErrLocalInUse is returned when the local port is already forwarded.
	ErrLocalInUse = errors.New("local port already forwarded")
	// ErrNotFound is returned when Remove targets a forward that does not exist.
	ErrNotFound = errors.New("forward not found")
	// ErrNoFreePort is returned by AllocLocal when no free port exists above want.
	ErrNoFreePort = errors.New("no free local port")
)

// Forward maps a port on the server's 127.0.0.1 to 127.0.0.1:Local on the host.
type Forward struct {
	Remote int
	Local  int
}

// Forwarder adds, removes and lists forwards for one server.
type Forwarder interface {
	Add(ctx context.Context, remote, local int) error
	Remove(ctx context.Context, remote, local int) error
	List() []Forward
	// Reapply re-establishes every active forward, e.g. after the tunnel reconnects.
	Reapply(ctx context.Context) error
}

// Runner runs an external command and returns its combined output.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRunner is the Runner used when a constructor receives nil.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func validPort(p int) error {
	if p < 1 || p > 65535 {
		return fmt.Errorf("%w: %d", ErrInvalidPort, p)
	}
	return nil
}

// spec builds the -L argument. Both ends are pinned to loopback.
func spec(local, remote int) string {
	return "127.0.0.1:" + strconv.Itoa(local) + ":127.0.0.1:" + strconv.Itoa(remote)
}

// manager holds the in-memory forward table and serialises mutations. The
// implementations supply the ssh side through the three hook functions.
type manager struct {
	ops  sync.Mutex      // serialises Add, Remove and Reapply
	mu   sync.Mutex      // guards fwds for List
	fwds map[int]Forward // keyed by Local port

	openFn   func(ctx context.Context, f Forward) error
	shutFn   func(ctx context.Context, f Forward) error
	reopenFn func(ctx context.Context, f Forward) error
}

func newManager(open, shut, reopen func(context.Context, Forward) error) *manager {
	if reopen == nil {
		reopen = open
	}
	return &manager{fwds: map[int]Forward{}, openFn: open, shutFn: shut, reopenFn: reopen}
}

// Add opens a forward. A local port that is already forwarded is rejected.
func (m *manager) Add(ctx context.Context, remote, local int) error {
	if err := validPort(remote); err != nil {
		return err
	}
	if err := validPort(local); err != nil {
		return err
	}
	m.ops.Lock()
	defer m.ops.Unlock()
	if _, dup := m.get(local); dup {
		return fmt.Errorf("%w: %d", ErrLocalInUse, local)
	}
	f := Forward{Remote: remote, Local: local}
	if err := m.openFn(ctx, f); err != nil {
		return err
	}
	m.mu.Lock()
	m.fwds[local] = f
	m.mu.Unlock()
	return nil
}

// Remove closes a forward. On failure the forward stays listed so the call can be retried.
func (m *manager) Remove(ctx context.Context, remote, local int) error {
	m.ops.Lock()
	defer m.ops.Unlock()
	f, ok := m.get(local)
	if !ok || f.Remote != remote {
		return fmt.Errorf("%w: %d->%d", ErrNotFound, remote, local)
	}
	if err := m.shutFn(ctx, f); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.fwds, local)
	m.mu.Unlock()
	return nil
}

// List returns the active forwards sorted by Remote, then Local.
func (m *manager) List() []Forward {
	m.mu.Lock()
	out := make([]Forward, 0, len(m.fwds))
	for _, f := range m.fwds {
		out = append(out, f)
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Remote != out[j].Remote {
			return out[i].Remote < out[j].Remote
		}
		return out[i].Local < out[j].Local
	})
	return out
}

// Reapply re-opens every listed forward and keeps them listed even when one fails.
func (m *manager) Reapply(ctx context.Context) error {
	m.ops.Lock()
	defer m.ops.Unlock()
	var errs []error
	for _, f := range m.List() {
		if err := m.reopenFn(ctx, f); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *manager) get(local int) (Forward, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.fwds[local]
	return f, ok
}
