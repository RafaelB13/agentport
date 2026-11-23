package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/rafaelb13/agentport/internal/forward"
	"github.com/rafaelb13/agentport/internal/paths"
	"github.com/rafaelb13/agentport/internal/ports"
)

// controlMasterSupported is false where OpenSSH has no unix-socket ControlMaster
// (Windows): forwards then run one ssh process each.
var controlMasterSupported = runtime.GOOS != "windows"

// sweepInterval is how often time-based port rules run (silent sessions, grace
// periods, expiry).
const sweepInterval = 5 * time.Second

// socketWait bounds how long a reconnected tunnel may take to create its
// ControlMaster socket before forwards are reapplied.
const socketWait = 10 * time.Second

// ControlPath returns the ControlMaster socket of a profile. The socket lives in
// a private directory and its name is a short hash of the profile, because unix
// socket paths are limited to about 100 bytes.
func ControlPath(profile string) (string, error) {
	if !validProfileName(profile) {
		return "", fmt.Errorf("invalid companion profile %q", profile)
	}
	return paths.ControlSocket(profile)
}

// PortManager is the Companion's port layer for one profile: the state store,
// the forwarder that talks to the tunnel's ControlMaster, and the persisted file.
type PortManager struct {
	Store       *ports.Store
	Handler     http.Handler
	ControlPath string
	stateFile   string
}

// NewPortManager builds the port layer for profile. Forwards persisted by an
// earlier run are restored as reserved; they open once a tunnel is up.
func NewPortManager(profile Profile, onError func(error)) (*PortManager, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	stateFile, err := portStatePath(profile.Name)
	if err != nil {
		return nil, err
	}
	var fwd forward.Forwarder
	controlPath := ""
	if controlMasterSupported {
		controlPath, err = ControlPath(profile.Name)
		if err != nil {
			return nil, err
		}
		fwd = forward.NewControlMaster("ssh", controlPath, profile.Destination, nil)
	} else {
		fwd = forward.NewPerProcess("ssh", profile.Destination, nil)
	}
	store, err := ports.NewStore(ports.Options{
		Profile:   profile.Name,
		Forwarder: fwd,
		Blocked:   []int{profile.RemotePort},
		Save: func(records []ports.Record) error {
			return writePrivateJSON(stateFile, profile.Name+".ports-", records)
		},
		OnError: onError,
	})
	if err != nil {
		return nil, err
	}
	records, err := loadPortRecords(stateFile)
	if err != nil {
		if onError != nil {
			onError(fmt.Errorf("ignore unreadable port state: %w", err))
		}
	}
	store.Restore(records)
	return &PortManager{Store: store, Handler: ports.NewHandler(store), ControlPath: controlPath, stateFile: stateFile}, nil
}

// Start runs the time-based rules until ctx ends.
func (m *PortManager) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.Store.Sweep()
			}
		}
	}()
}

// OnConnected is called after each tunnel start. It waits for the ControlMaster
// socket, then reapplies the forwards the forwarder already knows and opens the
// restored ones. It runs in the background so the tunnel loop never blocks.
func (m *PortManager) OnConnected(ctx context.Context, onError func(error)) {
	go func() {
		if m.ControlPath != "" {
			if err := waitForSocket(ctx, m.ControlPath, socketWait); err != nil {
				if onError != nil {
					onError(err)
				}
				return
			}
		}
		if err := m.Store.Reapply(ctx); err != nil && onError != nil {
			onError(fmt.Errorf("reapply forwards: %w", err))
		}
		m.Store.Resume(ctx)
	}()
}

// waitForSocket polls until path exists, or fails after timeout or cancellation.
func waitForSocket(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("the SSH control socket did not appear; forwards will be reapplied on the next reconnect")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func portStatePath(profile string) (string, error) {
	if !validProfileName(profile) {
		return "", errors.New("companion profile name must be a simple filename")
	}
	dir, err := paths.Dir("ports")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, profile+".json"), nil
}

func loadPortRecords(path string) ([]ports.Record, error) {
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []ports.Record
	if err := json.Unmarshal(payload, &records); err != nil {
		return nil, err
	}
	return records, nil
}
