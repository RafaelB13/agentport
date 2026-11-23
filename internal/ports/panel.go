package ports

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rafaelb13/agentport/internal/forward"
	"github.com/rafaelb13/agentport/internal/hostport"
	"github.com/rafaelb13/agentport/internal/portsproto"
)

// Activity kinds, mapped to the colored dots of the panel's Activity tab.
const (
	ActivityOK     = "ok"
	ActivityAsk    = "ask"
	ActivityDenied = "no"
	ActivityInfo   = "info"
)

// maxActivity bounds the in-memory activity log.
const maxActivity = 200

// ErrAlreadyOpen is returned when a reopen targets a port that is open already.
var ErrAlreadyOpen = errors.New("port is already open")

// Activity is one line of the panel's Activity tab.
type Activity struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
}

// ForwardEdit changes an open forward. Nil fields and zero values are kept.
type ForwardEdit struct {
	Label     *string
	Path      *string
	LocalPort int
	Duration  string
}

// ReopenRequest reopens a closed forward, with optional changes.
type ReopenRequest struct {
	RemotePort int
	LocalPort  int
	Label      string
	Path       string
	Duration   string
	ClosedAt   time.Time
}

// LocalCheck describes what holds a local port on this host.
// Status is one of: free, open_port (a forward of this profile), other_profile
// (an agentport process), or process (any other program).
type LocalCheck struct {
	Port      int           `json:"port"`
	Status    string        `json:"status"`
	Remote    int           `json:"remote_port,omitempty"`
	Process   string        `json:"process,omitempty"`
	PID       int           `json:"pid,omitempty"`
	Kind      hostport.Kind `json:"kind,omitempty"`
	Stoppable bool          `json:"stoppable"`
}

// Local port check statuses.
const (
	LocalFree         = "free"
	LocalOpenPort     = "open_port"
	LocalOtherProfile = "other_profile"
	LocalProcess      = "process"
)

// Version changes whenever the state a panel shows changes.
func (s *Store) Version() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.version
}

// SetOnPending registers a hook called (in its own goroutine) for every new
// approval request. The Companion uses it to open the panel.
func (s *Store) SetOnPending(fn func(PendingRequest)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onPending = fn
}

// Activity returns the activity log, oldest first.
func (s *Store) Activity() []Activity {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Activity(nil), s.activity...)
}

// logLocked appends an activity line and marks the state changed.
func (s *Store) logLocked(kind, text string) {
	s.version++
	s.activity = append(s.activity, Activity{At: s.opts.Now(), Kind: kind, Text: text})
	if over := len(s.activity) - maxActivity; over > 0 {
		s.activity = append([]Activity(nil), s.activity[over:]...)
	}
}

func (s *Store) pendingViewLocked(req *request) PendingRequest {
	pr := PendingRequest{ID: req.id, Session: req.session, Reason: req.reason, Group: req.group, CreatedAt: req.createdAt}
	if sess := s.sessions[req.session]; sess != nil {
		pr.Agent, pr.Cwd = sess.agent, sess.cwd
	}
	for _, it := range req.items {
		pr.Ports = append(pr.Ports, PendingPort{RemotePort: it.remote, Label: it.label, Path: it.path})
	}
	return pr
}

func agentName(sess *session) string {
	if sess == nil || sess.agent == "" {
		return "an agent"
	}
	return sess.agent
}

func portList(items []pendingItem) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf(":%d", it.remote))
	}
	return strings.Join(parts, ", ")
}

// applyDurationLocked sets expiry and pinning from a duration. An empty duration changes nothing.
func (s *Store) applyDurationLocked(f *entry, duration string, now time.Time) error {
	switch duration {
	case "":
		return nil
	case DurationSession:
		f.pinned, f.expiresAt = false, time.Time{}
	case DurationHour:
		f.pinned, f.expiresAt = false, now.Add(time.Hour)
	case DurationPin:
		f.pinned, f.expiresAt = true, time.Time{}
	default:
		return ErrInvalidDuration
	}
	return nil
}

// EditForward changes the label, path, local port or duration of an open forward.
// A new local port must be free; the forwarder is switched to it.
func (s *Store) EditForward(ctx context.Context, remote int, e ForwardEdit) (portsproto.ForwardInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.forwards[remote]
	if f == nil {
		return portsproto.ForwardInfo{}, ErrNotFound
	}
	if e.Label != nil {
		if len(*e.Label) > portsproto.MaxLabelLength {
			return portsproto.ForwardInfo{}, &InvalidError{fmt.Errorf("label must be at most %d characters", portsproto.MaxLabelLength)}
		}
	}
	if e.Path != nil {
		if len(*e.Path) > portsproto.MaxPathLength {
			return portsproto.ForwardInfo{}, &InvalidError{fmt.Errorf("path must be at most %d characters", portsproto.MaxPathLength)}
		}
	}
	now := s.opts.Now()
	if e.LocalPort != 0 && e.LocalPort != f.local {
		if !portsproto.ValidPort(e.LocalPort) {
			return portsproto.ForwardInfo{}, &InvalidError{errors.New("ports must be between 1 and 65535")}
		}
		if s.localUsedLocked(e.LocalPort) || s.opts.Busy(e.LocalPort) {
			return portsproto.ForwardInfo{}, &InvalidError{fmt.Errorf("local port %d is busy", e.LocalPort)}
		}
		addCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := s.opts.Forwarder.Remove(addCtx, f.remote, f.local); err != nil && !f.reserved && !errors.Is(err, forward.ErrNotFound) {
			return portsproto.ForwardInfo{}, err
		}
		if err := s.opts.Forwarder.Add(addCtx, f.remote, e.LocalPort); err != nil {
			_ = s.opts.Forwarder.Add(addCtx, f.remote, f.local) // best effort: keep the old mapping
			return portsproto.ForwardInfo{}, err
		}
		f.local = e.LocalPort
		f.reserved = false
	}
	if e.Label != nil {
		f.label = *e.Label
	}
	if e.Path != nil {
		f.path = *e.Path
	}
	if err := s.applyDurationLocked(f, e.Duration, now); err != nil {
		return portsproto.ForwardInfo{}, &InvalidError{err}
	}
	s.logLocked(ActivityInfo, fmt.Sprintf("Edited :%d → localhost:%d", remote, f.local))
	s.saveLocked()
	return s.infoLocked(f, UserOwner), nil
}

// SetPinned pins or unpins an open forward. A pinned forward survives its owners.
func (s *Store) SetPinned(remote int, pinned bool) (portsproto.ForwardInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.forwards[remote]
	if f == nil {
		return portsproto.ForwardInfo{}, ErrNotFound
	}
	f.pinned = pinned
	if pinned {
		f.expiresAt = time.Time{}
	}
	verb := "Unpinned"
	if pinned {
		verb = "Pinned"
	}
	s.logLocked(ActivityInfo, fmt.Sprintf("%s :%d", verb, remote))
	s.saveLocked()
	return s.infoLocked(f, UserOwner), nil
}

// ReopenClosed opens a port from the closed history, with the given changes,
// and removes its entry from the history.
func (s *Store) ReopenClosed(ctx context.Context, r ReopenRequest) (portsproto.ForwardInfo, error) {
	if !portsproto.ValidPort(r.RemotePort) {
		return portsproto.ForwardInfo{}, &InvalidError{errors.New("ports must be between 1 and 65535")}
	}
	if r.LocalPort != 0 && !portsproto.ValidPort(r.LocalPort) {
		return portsproto.ForwardInfo{}, &InvalidError{errors.New("ports must be between 1 and 65535")}
	}
	if err := checkDuration(r.Duration); err != nil {
		return portsproto.ForwardInfo{}, &InvalidError{err}
	}
	s.mu.Lock()
	if s.forwards[r.RemotePort] != nil {
		s.mu.Unlock()
		return portsproto.ForwardInfo{}, ErrAlreadyOpen
	}
	s.mu.Unlock()
	info, err := s.OpenByUser(ctx, r.RemotePort, r.LocalPort)
	if err != nil {
		return portsproto.ForwardInfo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.forwards[r.RemotePort]; f != nil {
		f.label, f.path = r.Label, r.Path
		_ = s.applyDurationLocked(f, r.Duration, s.opts.Now())
		info = s.infoLocked(f, UserOwner)
	}
	s.removeClosedLocked(r.RemotePort, r.ClosedAt)
	s.logLocked(ActivityOK, fmt.Sprintf("Reopened :%d → localhost:%d", r.RemotePort, info.LocalPort))
	s.saveLocked()
	return info, nil
}

func checkDuration(d string) error {
	switch d {
	case "", DurationSession, DurationHour, DurationPin:
		return nil
	}
	return ErrInvalidDuration
}

// ForgetClosed removes one entry from the closed history. A zero closedAt matches the first entry for remote.
func (s *Store) ForgetClosed(remote int, closedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.removeClosedLocked(remote, closedAt) {
		return ErrNotFound
	}
	s.version++
	return nil
}

func (s *Store) removeClosedLocked(remote int, closedAt time.Time) bool {
	for i, c := range s.closed {
		if c.RemotePort == remote && (closedAt.IsZero() || c.ClosedAt.Equal(closedAt)) {
			s.closed = append(s.closed[:i], s.closed[i+1:]...)
			return true
		}
	}
	return false
}

// CloseAll closes every open forward, as the user does from the panel. It returns the closed remote ports.
func (s *Store) CloseAll() ([]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	remotes := make([]int, 0, len(s.forwards))
	for remote := range s.forwards {
		remotes = append(remotes, remote)
	}
	sort.Ints(remotes)
	now := s.opts.Now()
	closed := []int{}
	var firstErr error
	for _, remote := range remotes {
		f := s.forwards[remote]
		if err := s.closeLocked(f, ReasonUser, now); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		closed = append(closed, remote)
	}
	if len(closed) > 0 {
		s.logLocked(ActivityInfo, fmt.Sprintf("Closed %d ports", len(closed)))
	}
	s.saveLocked()
	return closed, firstErr
}

// CloseGroup closes the given forwards, as the user does from the panel.
// Ports that are not open are reported in notOpen.
func (s *Store) CloseGroup(remotes []int) (closed, notOpen []int, err error) {
	closed, notOpen = []int{}, []int{}
	for _, remote := range remotes {
		if err := s.CloseByUser(remote); err != nil {
			if errors.Is(err, ErrNotFound) {
				notOpen = append(notOpen, remote)
				continue
			}
			return closed, notOpen, err
		}
		closed = append(closed, remote)
	}
	return closed, notOpen, nil
}

// CheckLocal reports what holds local port on this host. It never changes state.
func (s *Store) CheckLocal(ctx context.Context, port int) (LocalCheck, error) {
	if !portsproto.ValidPort(port) {
		return LocalCheck{}, &InvalidError{errors.New("ports must be between 1 and 65535")}
	}
	s.mu.Lock()
	for remote, f := range s.forwards {
		if f.local == port {
			s.mu.Unlock()
			return LocalCheck{Port: port, Status: LocalOpenPort, Remote: remote}, nil
		}
	}
	s.mu.Unlock()
	if !s.opts.Busy(port) {
		return LocalCheck{Port: port, Status: LocalFree}, nil
	}
	owner, err := s.opts.Owner(ctx, port)
	if err != nil {
		return LocalCheck{}, err
	}
	if owner == nil {
		return LocalCheck{Port: port, Status: LocalProcess}, nil
	}
	check := LocalCheck{Port: port, Status: LocalProcess, Process: owner.Process, PID: owner.PID, Kind: owner.Kind,
		Stoppable: owner.Kind == hostport.KindProc || owner.Kind == hostport.KindService || owner.Kind == hostport.KindContainer}
	if strings.EqualFold(owner.Process, "agentport") {
		check.Status = LocalOtherProfile
	}
	return check, nil
}

// StopOwnerAndOpen stops the program holding local port and then opens remote on it.
// Privileges are never escalated: a non-stoppable owner is refused. If the port
// is still busy, the forward takes the next free port and Activity records it.
func (s *Store) StopOwnerAndOpen(ctx context.Context, remote, local int) (portsproto.ForwardInfo, error) {
	if !portsproto.ValidPort(local) {
		return portsproto.ForwardInfo{}, &InvalidError{errors.New("ports must be between 1 and 65535")}
	}
	owner, err := s.opts.Owner(ctx, local)
	if err != nil {
		return portsproto.ForwardInfo{}, err
	}
	if owner != nil {
		if owner.Kind != hostport.KindProc && owner.Kind != hostport.KindService && owner.Kind != hostport.KindContainer {
			return portsproto.ForwardInfo{}, hostport.ErrNotStoppable
		}
		stopCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := s.opts.StopOwner(stopCtx, owner)
		cancel()
		if err != nil {
			s.mu.Lock()
			s.logLocked(ActivityDenied, fmt.Sprintf("Could not stop %s on :%d: %v", owner.Process, local, err))
			s.mu.Unlock()
			return portsproto.ForwardInfo{}, err
		}
		s.mu.Lock()
		s.logLocked(ActivityInfo, fmt.Sprintf("Stopped %s (pid %d) that held :%d", owner.Process, owner.PID, local))
		s.mu.Unlock()
	}
	info, err := s.OpenByUser(ctx, remote, local)
	if err != nil {
		return portsproto.ForwardInfo{}, err
	}
	if info.LocalPort != local {
		s.mu.Lock()
		s.logLocked(ActivityAsk, fmt.Sprintf("Port %d was still busy: :%d uses localhost:%d instead", local, remote, info.LocalPort))
		s.mu.Unlock()
	}
	return info, nil
}
