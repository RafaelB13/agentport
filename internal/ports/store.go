// Package ports is the Companion-side state of remote port forwards: agent
// sessions, the approval queue, the active forwards with their owners, and the
// closed history. Only a Store allocates local ports, one decision at a time.
package ports

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/rafaelb13/agentport/internal/forward"
	"github.com/rafaelb13/agentport/internal/hostport"
	"github.com/rafaelb13/agentport/internal/portsproto"
)

// UserOwner is the owner ID of a forward the user opened from the panel or CLI.
const UserOwner = "user"

// Durations accepted for an approval.
const (
	DurationSession = "session"
	DurationHour    = "1h"
	DurationPin     = "pin"
)

// Forward origins.
const (
	OriginAgent  = "agent"
	OriginManual = "manual"
)

// Closed-history reasons.
const (
	ReasonUser         = "user"
	ReasonAgent        = "agent"
	ReasonSessionEnded = "session_ended"
	ReasonExpired      = "expired"
)

// Sentinel errors. Callers match them with errors.Is.
var (
	ErrUnknownSession  = errors.New("unknown or ended session")
	ErrNotPending      = errors.New("request is not pending")
	ErrNotFound        = errors.New("not found")
	ErrLimitReached    = errors.New("forward limit reached for this profile")
	ErrBlockedPort     = errors.New("port is blocked for forwarding")
	ErrInvalidDuration = errors.New("duration must be session, 1h or pin")
)

// Options configures a Store. Nil fields get defaults; tests inject the clock,
// the forwarder and the busy/owner probes.
type Options struct {
	Profile   string
	Forwarder forward.Forwarder
	// Busy reports whether a local port is taken on the host. Default: not CanListen.
	Busy func(port int) bool
	// Owner describes who holds a busy local port, for messages. Default: hostport.Lookup.
	Owner func(ctx context.Context, port int) (*hostport.Owner, error)
	Now   func() time.Time
	// Blocked lists remote ports never forwarded. The tunnel port is added by the caller.
	Blocked        []int
	MaxForwards    int           // default 20
	SessionSilence time.Duration // default 60s without a heartbeat ends a session
	SessionGrace   time.Duration // default 20m before session-ended forwards close
	Heartbeat      time.Duration // default 20s, reported to clients
	// Save persists the forwards after every change. Nil disables persistence.
	Save func([]Record) error
	// OnError receives background failures (persistence, closing forwards).
	OnError func(error)
	// StopOwner stops the process or service that holds a busy local port. Nil
	// means hostport.Stop; tests inject a fake.
	StopOwner func(ctx context.Context, o *hostport.Owner) error
}

// Record is the persisted shape of one forward.
type Record struct {
	RemotePort int       `json:"remote_port"`
	LocalPort  int       `json:"local_port"`
	Label      string    `json:"label,omitempty"`
	Path       string    `json:"path,omitempty"`
	Group      string    `json:"group,omitempty"`
	Origin     string    `json:"origin"`
	Pinned     bool      `json:"pinned"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Approval is the user's decision on a pending request.
type Approval struct {
	Duration string
	Ports    []PortDecision
}

// PortDecision decides one port of a request. Ports not listed are skipped.
type PortDecision struct {
	RemotePort int
	LocalPort  int // 0 means the same port number, remapped if busy
	Skip       bool
}

// ClosedForward is one entry of the closed history.
type ClosedForward struct {
	RemotePort int       `json:"remote_port"`
	LocalPort  int       `json:"local_port"`
	Label      string    `json:"label,omitempty"`
	Path       string    `json:"path,omitempty"`
	Reason     string    `json:"reason"`
	ClosedAt   time.Time `json:"closed_at"`
}

// PendingRequest is a request waiting for the user, as the panel shows it.
type PendingRequest struct {
	ID        string        `json:"id"`
	Session   string        `json:"session"`
	Agent     string        `json:"agent"`
	Cwd       string        `json:"cwd,omitempty"`
	Reason    string        `json:"reason"`
	Group     string        `json:"group,omitempty"`
	Ports     []PendingPort `json:"ports"`
	CreatedAt time.Time     `json:"created_at"`
}

// PendingPort is one port waiting in a pending request.
type PendingPort struct {
	RemotePort int    `json:"remote_port"`
	Label      string `json:"label,omitempty"`
	Path       string `json:"path,omitempty"`
}

type session struct {
	id, agent, cwd string
	pid            int
	startedAt      time.Time
	lastSeen       time.Time
	ended          bool
	endedAt        time.Time
	// owned are the remote ports this session owned when it ended, so the same
	// agent in the same folder can take them back within the grace period.
	owned []int
}

type entry struct {
	remote, local int
	label, path   string
	group, origin string
	owners        map[string]bool
	pinned        bool
	expiresAt     time.Time
	createdAt     time.Time
	// endedAt is set when the last owner left: the forward is session_ended.
	endedAt time.Time
	// reserved marks a forward restored from disk that is not yet in the forwarder.
	reserved bool
}

type pendingItem struct {
	remote      int
	label, path string
}

type request struct {
	id, session string
	reason      string
	group       string
	items       []pendingItem
	createdAt   time.Time
	done        chan struct{}
	decided     bool
	outcomes    map[int]portsproto.PortResult
}

// Store holds the Companion's port state. All methods are safe for concurrent use.
type Store struct {
	mu       sync.Mutex
	opts     Options
	blocked  map[int]bool
	sessions map[string]*session
	forwards map[int]*entry // by remote port
	requests map[string]*request
	queue    []string // pending request IDs, oldest first
	closed   []ClosedForward
	// version counts every change a panel must refresh for; activity is the
	// user-facing log shown in the panel's Activity tab (newest last).
	version   uint64
	activity  []Activity
	onPending func(PendingRequest)
}

// NewStore builds an empty Store with defaults applied.
func NewStore(opts Options) (*Store, error) {
	if opts.Forwarder == nil {
		return nil, errors.New("ports: a forwarder is required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Busy == nil {
		opts.Busy = func(port int) bool { return !forward.CanListen(port) }
	}
	if opts.Owner == nil {
		opts.Owner = hostport.Lookup
	}
	if opts.StopOwner == nil {
		opts.StopOwner = hostport.Stop
	}
	if opts.MaxForwards <= 0 {
		opts.MaxForwards = 20
	}
	if opts.SessionSilence <= 0 {
		opts.SessionSilence = 60 * time.Second
	}
	if opts.SessionGrace <= 0 {
		opts.SessionGrace = 20 * time.Minute
	}
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = 20 * time.Second
	}
	blocked := map[int]bool{22: true, 2375: true, 2376: true}
	for _, p := range opts.Blocked {
		blocked[p] = true
	}
	return &Store{
		opts:     opts,
		blocked:  blocked,
		sessions: map[string]*session{},
		forwards: map[int]*entry{},
		requests: map[string]*request{},
	}, nil
}

// RegisterSession creates an agent session. A session of the same agent and
// folder that ended within the grace period takes its forwards back.
func (s *Store) RegisterSession(req portsproto.RegisterSessionRequest) (portsproto.RegisterSessionResponse, error) {
	if err := req.Validate(); err != nil {
		return portsproto.RegisterSessionResponse{}, &InvalidError{err}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.opts.Now()
	s.sweepLocked(now)
	id, err := newID()
	if err != nil {
		return portsproto.RegisterSessionResponse{}, err
	}
	resumed := false
	for oldID, old := range s.sessions {
		if !old.ended || old.agent != req.Agent || old.cwd != req.Cwd {
			continue
		}
		for _, remote := range old.owned {
			if f := s.forwards[remote]; f != nil && !f.endedAt.IsZero() {
				f.owners[id] = true
				f.endedAt = time.Time{}
				resumed = true
			}
		}
		delete(s.sessions, oldID)
	}
	s.sessions[id] = &session{id: id, agent: req.Agent, cwd: req.Cwd, pid: req.PID, startedAt: now, lastSeen: now}
	s.saveLocked()
	return portsproto.RegisterSessionResponse{
		ID: id, HeartbeatSeconds: int(s.opts.Heartbeat / time.Second), Resumed: resumed,
	}, nil
}

// Heartbeat refreshes a session. An unknown or ended session is an error, so
// the client knows to register again.
func (s *Store) Heartbeat(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.opts.Now()
	s.sweepLocked(now)
	sess := s.sessions[id]
	if sess == nil || sess.ended {
		return ErrUnknownSession
	}
	sess.lastSeen = now
	return nil
}

// UnregisterSession ends a session at once, as when the agent's stdio closes.
func (s *Store) UnregisterSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[id]; sess != nil && !sess.ended {
		s.endSessionLocked(sess, s.opts.Now())
		s.saveLocked()
	}
}

// Request asks for a batch of remote ports on behalf of a session. Ports that
// are already forwarded are shared at once; the others wait for the user's
// decision until ctx ends. Ports still undecided then are pending_approval and
// stay in the queue.
func (s *Store) Request(ctx context.Context, sessionID string, in portsproto.ForwardRequest) (portsproto.ForwardResponse, error) {
	if err := in.Validate(); err != nil {
		return portsproto.ForwardResponse{}, &InvalidError{err}
	}
	s.mu.Lock()
	now := s.opts.Now()
	s.sweepLocked(now)
	sess := s.sessions[sessionID]
	if sess == nil || sess.ended {
		s.mu.Unlock()
		return portsproto.ForwardResponse{}, ErrUnknownSession
	}
	sess.lastSeen = now
	results := map[int]portsproto.PortResult{}
	var items []pendingItem
	for _, p := range in.Ports {
		if s.blocked[p.RemotePort] {
			results[p.RemotePort] = portsproto.PortResult{RemotePort: p.RemotePort, State: portsproto.StateDenied, Message: ErrBlockedPort.Error()}
			continue
		}
		if f := s.forwards[p.RemotePort]; f != nil {
			f.owners[sessionID] = true
			f.endedAt = time.Time{}
			results[p.RemotePort] = activeResult(f)
			continue
		}
		items = append(items, pendingItem{remote: p.RemotePort, label: p.Label, path: p.Path})
	}
	if len(items) == 0 {
		s.saveLocked()
		s.mu.Unlock()
		return portsproto.ForwardResponse{Results: ordered(in.Ports, results)}, nil
	}
	id, err := newID()
	if err != nil {
		s.mu.Unlock()
		return portsproto.ForwardResponse{}, err
	}
	req := &request{id: id, session: sessionID, reason: in.Reason, group: in.Group, items: items,
		createdAt: now, done: make(chan struct{}), outcomes: map[int]portsproto.PortResult{}}
	s.requests[id] = req
	s.queue = append(s.queue, id)
	s.logLocked(ActivityAsk, fmt.Sprintf("%s asks for port %s", agentName(s.sessions[sessionID]), portList(items)))
	hook, view := s.onPending, s.pendingViewLocked(req)
	s.mu.Unlock()
	if hook != nil {
		go hook(view)
	}

	select {
	case <-req.done:
	case <-ctx.Done():
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
		if res, ok := req.outcomes[it.remote]; ok {
			results[it.remote] = res
		} else {
			results[it.remote] = portsproto.PortResult{RemotePort: it.remote, State: portsproto.StatePendingApproval, RequestID: id}
		}
	}
	return portsproto.ForwardResponse{Results: ordered(in.Ports, results)}, nil
}

// Pending lists the requests waiting for a decision, oldest first.
func (s *Store) Pending() []PendingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []PendingRequest{}
	for _, id := range s.queue {
		req := s.requests[id]
		if req == nil || req.decided {
			continue
		}
		pr := PendingRequest{ID: req.id, Session: req.session, Reason: req.reason, Group: req.group, CreatedAt: req.createdAt}
		if sess := s.sessions[req.session]; sess != nil {
			pr.Agent, pr.Cwd = sess.agent, sess.cwd
		}
		for _, it := range req.items {
			pr.Ports = append(pr.Ports, PendingPort{RemotePort: it.remote, Label: it.label, Path: it.path})
		}
		out = append(out, pr)
	}
	return out
}

// Approve decides a pending request. Each approved port gets a local port: the
// same number when free, otherwise the next free one. A port that another
// session already forwarded is shared instead of opened again.
func (s *Store) Approve(ctx context.Context, requestID string, a Approval) ([]portsproto.PortResult, error) {
	switch a.Duration {
	case DurationSession, DurationHour, DurationPin, "":
	default:
		return nil, ErrInvalidDuration
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	req, err := s.pendingLocked(requestID)
	if err != nil {
		return nil, err
	}
	decisions := map[int]PortDecision{}
	for _, d := range a.Ports {
		decisions[d.RemotePort] = d
	}
	now := s.opts.Now()
	for _, it := range req.items {
		d, chosen := decisions[it.remote]
		if !chosen || d.Skip {
			req.outcomes[it.remote] = portsproto.PortResult{RemotePort: it.remote, State: portsproto.StateSkipped, Message: "skipped by the user"}
			continue
		}
		if f := s.forwards[it.remote]; f != nil {
			f.owners[req.session] = true
			f.endedAt = time.Time{}
			req.outcomes[it.remote] = activeResult(f)
			continue
		}
		if s.activeCountLocked() >= s.opts.MaxForwards {
			req.outcomes[it.remote] = portsproto.PortResult{RemotePort: it.remote, State: portsproto.StateError, Message: ErrLimitReached.Error()}
			continue
		}
		f, msg, err := s.openLocked(ctx, it.remote, d.LocalPort)
		if err != nil {
			req.outcomes[it.remote] = portsproto.PortResult{RemotePort: it.remote, State: portsproto.StateError, Message: err.Error()}
			s.logLocked(ActivityDenied, fmt.Sprintf("Could not open :%d for %s: %v", it.remote, agentName(s.sessions[req.session]), err))
			continue
		}
		f.label, f.path, f.group = it.label, it.path, req.group
		f.origin = OriginAgent
		f.owners[req.session] = true
		f.createdAt = now
		f.pinned = a.Duration == DurationPin
		if a.Duration == DurationHour {
			f.expiresAt = now.Add(time.Hour)
		}
		res := activeResult(f)
		res.Message = msg
		req.outcomes[it.remote] = res
		s.logLocked(ActivityOK, fmt.Sprintf("Opened :%d for %s → localhost:%d", it.remote, agentName(s.sessions[req.session]), f.local))
	}
	s.finishLocked(req)
	return ordered(itemPorts(req.items), req.outcomes), nil
}

// Deny rejects every port of a pending request.
func (s *Store) Deny(requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, err := s.pendingLocked(requestID)
	if err != nil {
		return err
	}
	for _, it := range req.items {
		req.outcomes[it.remote] = portsproto.PortResult{RemotePort: it.remote, State: portsproto.StateDenied, Message: "denied by the user"}
		s.logLocked(ActivityDenied, fmt.Sprintf("Denied :%d for %s", it.remote, agentName(s.sessions[req.session])))
	}
	s.finishLocked(req)
	return nil
}

// OpenByUser opens a forward from the panel or CLI, owned by the user. A busy
// local port is remapped as for an approval.
func (s *Store) OpenByUser(ctx context.Context, remote, local int) (portsproto.ForwardInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.opts.Now()
	s.sweepLocked(now)
	if s.blocked[remote] {
		return portsproto.ForwardInfo{}, ErrBlockedPort
	}
	if f := s.forwards[remote]; f != nil {
		f.owners[UserOwner] = true
		f.endedAt = time.Time{}
		s.saveLocked()
		return s.infoLocked(f, ""), nil
	}
	if s.activeCountLocked() >= s.opts.MaxForwards {
		return portsproto.ForwardInfo{}, ErrLimitReached
	}
	f, _, err := s.openLocked(ctx, remote, local)
	if err != nil {
		return portsproto.ForwardInfo{}, err
	}
	f.origin = OriginManual
	f.owners[UserOwner] = true
	f.createdAt = now
	s.logLocked(ActivityOK, fmt.Sprintf("Opened :%d → localhost:%d by you", remote, f.local))
	s.saveLocked()
	return s.infoLocked(f, ""), nil
}

// Unforward removes session sessionID from the owners of the given ports, or of
// every port it owns. A port with no owner left closes; one other owners keep
// stays open.
func (s *Store) Unforward(sessionID string, ports []int, all bool) (portsproto.UnforwardResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.opts.Now()
	s.sweepLocked(now)
	if sess := s.sessions[sessionID]; sess == nil || sess.ended {
		return portsproto.UnforwardResponse{}, ErrUnknownSession
	}
	targets := ports
	if all {
		targets = nil
		for remote, f := range s.forwards {
			if f.owners[sessionID] {
				targets = append(targets, remote)
			}
		}
		sort.Ints(targets)
	}
	out := portsproto.UnforwardResponse{Closed: []int{}, Kept: []portsproto.KeptPort{}, NotForwarded: []int{}}
	for _, remote := range targets {
		f := s.forwards[remote]
		if f == nil || !f.owners[sessionID] {
			out.NotForwarded = append(out.NotForwarded, remote)
			continue
		}
		delete(f.owners, sessionID)
		if len(f.owners) > 0 {
			out.Kept = append(out.Kept, portsproto.KeptPort{RemotePort: remote, Owners: s.ownerNamesLocked(f)})
			continue
		}
		if err := s.closeLocked(f, ReasonAgent, now); err != nil {
			f.owners[sessionID] = true
			return out, err
		}
		out.Closed = append(out.Closed, remote)
	}
	s.saveLocked()
	return out, nil
}

// CloseByUser closes a forward whatever its owners, as the user does from the panel.
func (s *Store) CloseByUser(remote int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.forwards[remote]
	if f == nil {
		return ErrNotFound
	}
	err := s.closeLocked(f, ReasonUser, s.opts.Now())
	if err == nil {
		s.logLocked(ActivityInfo, fmt.Sprintf("Closed :%d", remote))
	}
	s.saveLocked()
	return err
}

// Forwards lists the active forwards from the point of view of session
// sessionID (empty for the user), with owners and their origin.
func (s *Store) Forwards(sessionID string) []portsproto.ForwardInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sweepLocked(s.opts.Now()) {
		s.saveLocked()
	}
	remotes := make([]int, 0, len(s.forwards))
	for remote := range s.forwards {
		remotes = append(remotes, remote)
	}
	sort.Ints(remotes)
	out := []portsproto.ForwardInfo{}
	for _, remote := range remotes {
		out = append(out, s.infoLocked(s.forwards[remote], sessionID))
	}
	return out
}

// Closed returns the closed history, newest last.
func (s *Store) Closed() []ClosedForward {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ClosedForward(nil), s.closed...)
}

// Sweep applies time-based rules: silent sessions end, session-ended forwards
// past the grace period close, and expired forwards close. Call it periodically.
func (s *Store) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sweepLocked(s.opts.Now()) {
		s.saveLocked()
	}
}

// Snapshot returns the forwards as they should be persisted.
func (s *Store) Snapshot() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// Restore loads forwards persisted by an earlier Companion. They start as
// reserved: nothing is opened until Resume runs over a live tunnel.
func (s *Store) Restore(records []Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.opts.Now()
	for _, r := range records {
		if !portsproto.ValidPort(r.RemotePort) || !portsproto.ValidPort(r.LocalPort) || s.forwards[r.RemotePort] != nil {
			continue
		}
		s.forwards[r.RemotePort] = &entry{
			remote: r.RemotePort, local: r.LocalPort, label: r.Label, path: r.Path, group: r.Group,
			origin: r.Origin, owners: map[string]bool{}, pinned: r.Pinned, expiresAt: r.ExpiresAt,
			createdAt: r.CreatedAt, endedAt: now, reserved: true,
		}
	}
}

// Resume opens every reserved forward in the forwarder, once the tunnel is up.
// A forward that cannot be reopened stays reserved and is tried again later.
func (s *Store) Resume(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.forwards {
		if !f.reserved {
			continue
		}
		if err := s.opts.Forwarder.Add(ctx, f.remote, f.local); err != nil {
			s.onError(fmt.Errorf("resume forward %d: %w", f.remote, err))
			continue
		}
		f.reserved = false
	}
	s.saveLocked()
}

// Reapply re-opens the forwarder's forwards after the tunnel reconnects.
func (s *Store) Reapply(ctx context.Context) error {
	return s.opts.Forwarder.Reapply(ctx)
}

// --- internals; callers hold s.mu -------------------------------------------

// openLocked allocates a local port for remote and asks the forwarder for it.
// want 0 means "the same number as remote".
func (s *Store) openLocked(ctx context.Context, remote, want int) (*entry, string, error) {
	if want == 0 {
		want = remote
	}
	if !portsproto.ValidPort(want) {
		return nil, "", fmt.Errorf("local port %d is not valid", want)
	}
	msg := ""
	busy := func(p int) bool { return s.localUsedLocked(p) || s.opts.Busy(p) }
	if busy(want) {
		desc := "another program"
		if owner, err := s.opts.Owner(ctx, want); err == nil && owner != nil {
			desc = fmt.Sprintf("%s (pid %d)", owner.Process, owner.PID)
		}
		msg = fmt.Sprintf("local port %d is busy: %s", want, desc)
	}
	local, err := forward.AllocLocal(want, busy)
	if err != nil {
		return nil, "", err
	}
	if local != want && msg == "" {
		msg = fmt.Sprintf("local port %d is busy", want)
	}
	if local != want {
		msg = fmt.Sprintf("%s; using local port %d", msg, local)
	}
	addCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.opts.Forwarder.Add(addCtx, remote, local); err != nil {
		return nil, "", err
	}
	f := &entry{remote: remote, local: local, owners: map[string]bool{}, createdAt: s.opts.Now(), origin: OriginAgent}
	s.forwards[remote] = f
	return f, msg, nil
}

// closeLocked removes a forward from the forwarder and records it in the history.
func (s *Store) closeLocked(f *entry, reason string, now time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.opts.Forwarder.Remove(ctx, f.remote, f.local); err != nil && !errors.Is(err, forward.ErrNotFound) && !f.reserved {
		return err
	}
	delete(s.forwards, f.remote)
	s.closed = append(s.closed, ClosedForward{RemotePort: f.remote, LocalPort: f.local, Label: f.label, Path: f.path, Reason: reason, ClosedAt: now})
	return nil
}

func (s *Store) endSessionLocked(sess *session, now time.Time) {
	sess.ended, sess.endedAt, sess.owned = true, now, nil
	for remote, f := range s.forwards {
		if !f.owners[sess.id] {
			continue
		}
		delete(f.owners, sess.id)
		sess.owned = append(sess.owned, remote)
		if len(f.owners) == 0 {
			f.endedAt = now
		}
	}
	sort.Ints(sess.owned)
}

// sweepLocked applies the time rules and reports whether the set of forwards
// changed, so that only real changes are written to disk.
func (s *Store) sweepLocked(now time.Time) bool {
	changed := false
	for id, sess := range s.sessions {
		if !sess.ended && now.Sub(sess.lastSeen) > s.opts.SessionSilence {
			s.endSessionLocked(sess, now)
			changed = true
		}
		if sess.ended && now.Sub(sess.endedAt) > s.opts.SessionGrace {
			delete(s.sessions, id)
		}
	}
	for _, f := range s.forwards {
		if f.reserved {
			continue
		}
		expired := !f.expiresAt.IsZero() && !now.Before(f.expiresAt)
		orphaned := !f.endedAt.IsZero() && !f.pinned && now.Sub(f.endedAt) >= s.opts.SessionGrace
		if expired || orphaned {
			reason := ReasonExpired
			if orphaned {
				reason = ReasonSessionEnded
			}
			if err := s.closeLocked(f, reason, now); err != nil {
				s.onError(fmt.Errorf("close forward %d: %w", f.remote, err))
			} else {
				changed = true
			}
		}
	}
	return changed
}

func (s *Store) pendingLocked(id string) (*request, error) {
	req := s.requests[id]
	if req == nil {
		return nil, ErrNotFound
	}
	if req.decided {
		return nil, ErrNotPending
	}
	return req, nil
}

// finishLocked marks a request decided, wakes its waiter and saves.
func (s *Store) finishLocked(req *request) {
	req.decided = true
	for i, id := range s.queue {
		if id == req.id {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			break
		}
	}
	close(req.done)
	s.saveLocked()
}

func (s *Store) activeCountLocked() int { return len(s.forwards) }

func (s *Store) localUsedLocked(port int) bool {
	for _, f := range s.forwards {
		if f.local == port {
			return true
		}
	}
	return false
}

func (s *Store) ownerNamesLocked(f *entry) []string {
	names := []string{}
	for id := range f.owners {
		names = append(names, s.ownerNameLocked(id))
	}
	sort.Strings(names)
	return names
}

func (s *Store) ownerNameLocked(id string) string {
	if id == UserOwner {
		return "user"
	}
	if sess := s.sessions[id]; sess != nil {
		return sess.agent
	}
	return "session"
}

func (s *Store) infoLocked(f *entry, viewer string) portsproto.ForwardInfo {
	state := "active"
	if f.reserved {
		state = "reserved"
	} else if !f.endedAt.IsZero() {
		state = "session_ended"
	}
	now := s.opts.Now()
	return portsproto.ForwardInfo{
		RemotePort: f.remote, LocalPort: f.local, URL: portsproto.URL(f.local, f.path),
		Label: f.label, Path: f.path, Group: f.group, Origin: f.origin, State: state,
		Owners: s.ownerNamesLocked(f), Mine: viewer != "" && f.owners[viewer], Pinned: f.pinned,
		CreatedAt: f.createdAt, UptimeSeconds: int64(now.Sub(f.createdAt) / time.Second),
	}
}

func (s *Store) snapshotLocked() []Record {
	out := []Record{}
	for _, f := range s.forwards {
		out = append(out, Record{RemotePort: f.remote, LocalPort: f.local, Label: f.label, Path: f.path,
			Group: f.group, Origin: f.origin, Pinned: f.pinned, ExpiresAt: f.expiresAt, CreatedAt: f.createdAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RemotePort < out[j].RemotePort })
	return out
}

func (s *Store) saveLocked() {
	s.version++
	if s.opts.Save == nil {
		return
	}
	if err := s.opts.Save(s.snapshotLocked()); err != nil {
		s.onError(fmt.Errorf("save port state: %w", err))
	}
}

func (s *Store) onError(err error) {
	if s.opts.OnError != nil {
		s.opts.OnError(err)
	}
}

func activeResult(f *entry) portsproto.PortResult {
	return portsproto.PortResult{RemotePort: f.remote, State: portsproto.StateActive, LocalPort: f.local, URL: portsproto.URL(f.local, f.path)}
}

func itemPorts(items []pendingItem) []portsproto.PortRequest {
	out := make([]portsproto.PortRequest, 0, len(items))
	for _, it := range items {
		out = append(out, portsproto.PortRequest{RemotePort: it.remote, Label: it.label, Path: it.path})
	}
	return out
}

// ordered returns one result per requested port, in request order.
func ordered(ports []portsproto.PortRequest, results map[int]portsproto.PortResult) []portsproto.PortResult {
	out := make([]portsproto.PortResult, 0, len(ports))
	for _, p := range ports {
		if r, ok := results[p.RemotePort]; ok {
			out = append(out, r)
		}
	}
	return out
}

func newID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// InvalidError wraps a validation failure so handlers can answer 400.
type InvalidError struct{ Err error }

func (e *InvalidError) Error() string { return e.Err.Error() }
func (e *InvalidError) Unwrap() error { return e.Err }
