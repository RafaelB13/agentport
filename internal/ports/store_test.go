package ports

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/forward"
	"github.com/rafaelb13/agentport/internal/hostport"
	"github.com/rafaelb13/agentport/internal/portsproto"
)

// fakeForwarder records every call and can fail on demand.
type fakeForwarder struct {
	mu      sync.Mutex
	active  map[int]int // remote -> local
	addErr  error
	reapply int
}

func newFakeForwarder() *fakeForwarder { return &fakeForwarder{active: map[int]int{}} }

func (f *fakeForwarder) Add(_ context.Context, remote, local int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return f.addErr
	}
	f.active[remote] = local
	return nil
}

func (f *fakeForwarder) Remove(_ context.Context, remote, local int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.active[remote]; !ok {
		return forward.ErrNotFound
	}
	delete(f.active, remote)
	return nil
}

func (f *fakeForwarder) List() []forward.Forward {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []forward.Forward{}
	for r, l := range f.active {
		out = append(out, forward.Forward{Remote: r, Local: l})
	}
	return out
}

func (f *fakeForwarder) Reapply(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reapply++
	return nil
}

func (f *fakeForwarder) isOpen(remote int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.active[remote]
	return ok
}

// harness is a Store with a manual clock, a fake forwarder and a set of busy local ports.
type harness struct {
	t     *testing.T
	store *Store
	fwd   *fakeForwarder
	now   time.Time
	busy  map[int]bool
}

func newHarness(t *testing.T, mutate func(*Options)) *harness {
	t.Helper()
	h := &harness{t: t, fwd: newFakeForwarder(), now: time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC), busy: map[int]bool{}}
	opts := Options{
		Profile:   "m2",
		Forwarder: h.fwd,
		Busy:      func(p int) bool { return h.busy[p] },
		Owner: func(context.Context, int) (*hostport.Owner, error) {
			return &hostport.Owner{Port: 0, PID: 42, Process: "node"}, nil
		},
		Now: func() time.Time { return h.now },
	}
	if mutate != nil {
		mutate(&opts)
	}
	store, err := NewStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	h.store = store
	return h
}

func (h *harness) advance(d time.Duration) { h.now = h.now.Add(d) }

func (h *harness) register(agent, cwd string) string {
	h.t.Helper()
	resp, err := h.store.RegisterSession(portsproto.RegisterSessionRequest{V: 1, Agent: agent, PID: 1, Cwd: cwd})
	if err != nil {
		h.t.Fatal(err)
	}
	return resp.ID
}

func batch(reason string, ports ...int) portsproto.ForwardRequest {
	req := portsproto.ForwardRequest{V: 1, Reason: reason}
	for _, p := range ports {
		req.Ports = append(req.Ports, portsproto.PortRequest{RemotePort: p})
	}
	return req
}

// cancelled returns a context that is already done: Request then returns the
// current state at once, which is how a timeout looks to the caller.
func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// requestAsync starts a Request and waits until it is queued, without sleeping
// on the clock: the queue itself is the signal.
func (h *harness) requestAsync(session string, req portsproto.ForwardRequest) <-chan portsproto.ForwardResponse {
	h.t.Helper()
	before := len(h.store.Pending())
	out := make(chan portsproto.ForwardResponse, 1)
	go func() {
		resp, err := h.store.Request(context.Background(), session, req)
		if err != nil {
			h.t.Error(err)
		}
		out <- resp
	}()
	for i := 0; i < 10000 && len(h.store.Pending()) == before; i++ {
		time.Sleep(time.Millisecond)
	}
	if len(h.store.Pending()) == before {
		h.t.Fatal("request never reached the approval queue")
	}
	return out
}

func (h *harness) firstPending() PendingRequest {
	h.t.Helper()
	p := h.store.Pending()
	if len(p) == 0 {
		h.t.Fatal("no pending request")
	}
	return p[len(p)-1]
}

func TestBatchApproveWithPartialSkip(t *testing.T) {
	h := newHarness(t, nil)
	sess := h.register("claude-code", "/work/web")
	out := h.requestAsync(sess, batch("web and api", 3000, 5173, 8080))
	req := h.firstPending()
	if len(req.Ports) != 3 {
		t.Fatalf("batch became %d pending ports, want 3 in one request", len(req.Ports))
	}
	res, err := h.store.Approve(context.Background(), req.ID, Approval{Duration: DurationSession, Ports: []PortDecision{
		{RemotePort: 3000}, {RemotePort: 5173, Skip: true}, {RemotePort: 8080, LocalPort: 9090},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := <-out
	if len(got.Results) != 3 || len(res) != 3 {
		t.Fatalf("results = %+v", got.Results)
	}
	byPort := map[int]portsproto.PortResult{}
	for _, r := range got.Results {
		byPort[r.RemotePort] = r
	}
	if byPort[3000].State != portsproto.StateActive || byPort[3000].LocalPort != 3000 || byPort[3000].URL != "http://localhost:3000" {
		t.Errorf("3000 = %+v", byPort[3000])
	}
	if byPort[5173].State != portsproto.StateSkipped {
		t.Errorf("5173 = %+v, want skipped", byPort[5173])
	}
	if byPort[8080].State != portsproto.StateActive || byPort[8080].LocalPort != 9090 {
		t.Errorf("8080 = %+v", byPort[8080])
	}
	if !h.fwd.isOpen(3000) || h.fwd.isOpen(5173) || !h.fwd.isOpen(8080) {
		t.Errorf("forwarder state wrong: %v", h.fwd.active)
	}
	if len(h.store.Pending()) != 0 {
		t.Error("decided request still pending")
	}
}

func TestDenyResolvesEveryPort(t *testing.T) {
	h := newHarness(t, nil)
	sess := h.register("claude-code", "/w")
	out := h.requestAsync(sess, batch("try it", 3000, 4000))
	req := h.firstPending()
	if err := h.store.Deny(req.ID); err != nil {
		t.Fatal(err)
	}
	for _, r := range (<-out).Results {
		if r.State != portsproto.StateDenied {
			t.Errorf("port %d state %s, want denied", r.RemotePort, r.State)
		}
	}
	if len(h.fwd.active) != 0 {
		t.Error("denied ports were forwarded")
	}
	if err := h.store.Deny(req.ID); !errors.Is(err, ErrNotPending) {
		t.Errorf("second decision err = %v, want ErrNotPending", err)
	}
}

func TestTimeoutReturnsPendingApprovalAndKeepsTheRequestQueued(t *testing.T) {
	h := newHarness(t, nil)
	sess := h.register("claude-code", "/w")
	resp, err := h.store.Request(cancelled(), sess, batch("later", 3000))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Results[0].State != portsproto.StatePendingApproval || resp.Results[0].RequestID == "" {
		t.Fatalf("result = %+v", resp.Results[0])
	}
	if len(h.store.Pending()) != 1 {
		t.Fatal("timed-out request left the queue; the user must still be able to answer it")
	}
}

func TestSharedPortIsReusedWithoutAnotherApproval(t *testing.T) {
	h := newHarness(t, nil)
	first := h.register("claude-code", "/a")
	second := h.register("codex", "/b")
	out := h.requestAsync(first, batch("api", 3000))
	if _, err := h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession,
		Ports: []PortDecision{{RemotePort: 3000}}}); err != nil {
		t.Fatal(err)
	}
	<-out

	resp, err := h.store.Request(cancelled(), second, batch("same api", 3000))
	if err != nil {
		t.Fatal(err)
	}
	if len(h.store.Pending()) != 0 {
		t.Fatal("second session produced an approval request for a port already open")
	}
	r := resp.Results[0]
	if r.State != portsproto.StateActive || r.URL != "http://localhost:3000" {
		t.Fatalf("second session result = %+v", r)
	}
	for _, f := range h.store.Forwards(second) {
		if f.RemotePort == 3000 && (!f.Mine || len(f.Owners) != 2) {
			t.Fatalf("forward for second session = %+v, want mine with two owners", f)
		}
	}
}

func TestUnforwardByOneOwnerKeepsItOpenAndByLastOwnerCloses(t *testing.T) {
	h := newHarness(t, nil)
	a := h.register("claude-code", "/a")
	b := h.register("codex", "/b")
	out := h.requestAsync(a, batch("api", 3000))
	_, _ = h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession, Ports: []PortDecision{{RemotePort: 3000}}})
	<-out
	_, _ = h.store.Request(cancelled(), b, batch("api", 3000))

	resp, err := h.store.Unforward(a, []int{3000}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Closed) != 0 || len(resp.Kept) != 1 || resp.Kept[0].RemotePort != 3000 {
		t.Fatalf("first unforward = %+v, want kept", resp)
	}
	if !h.fwd.isOpen(3000) {
		t.Fatal("port closed while another session still owns it")
	}
	resp, err = h.store.Unforward(b, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Closed) != 1 || resp.Closed[0] != 3000 {
		t.Fatalf("last unforward = %+v, want closed 3000", resp)
	}
	if h.fwd.isOpen(3000) {
		t.Fatal("last owner left but the forwarder still has the port")
	}
}

func TestUnforwardReportsPortsNotOwnedAndUserPortsStayOpen(t *testing.T) {
	h := newHarness(t, nil)
	a := h.register("claude-code", "/a")
	if _, err := h.store.OpenByUser(context.Background(), 5432, 0); err != nil {
		t.Fatal(err)
	}
	resp, err := h.store.Unforward(a, []int{5432, 7777}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.NotForwarded) != 2 || len(resp.Closed) != 0 {
		t.Fatalf("unforward = %+v, want both not forwarded for this session", resp)
	}
	if !h.fwd.isOpen(5432) {
		t.Fatal("agent closed a port the user opened")
	}
}

func TestSessionSilenceEndsSessionAndClosesAfterGrace(t *testing.T) {
	h := newHarness(t, nil)
	sess := h.register("claude-code", "/a")
	out := h.requestAsync(sess, batch("api", 3000))
	_, _ = h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession, Ports: []PortDecision{{RemotePort: 3000}}})
	<-out

	h.advance(61 * time.Second)
	h.store.Sweep()
	fw := h.store.Forwards("")
	if len(fw) != 1 || fw[0].State != "session_ended" {
		t.Fatalf("after silence forwards = %+v, want session_ended", fw)
	}
	if err := h.store.Heartbeat(sess); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("heartbeat after silence = %v, want ErrUnknownSession", err)
	}
	h.advance(19 * time.Minute)
	h.store.Sweep()
	if !h.fwd.isOpen(3000) {
		t.Fatal("closed before the 20 minute grace period ended")
	}
	h.advance(2 * time.Minute)
	h.store.Sweep()
	if h.fwd.isOpen(3000) {
		t.Fatal("still open after the grace period")
	}
	closed := h.store.Closed()
	if len(closed) != 1 || closed[0].Reason != ReasonSessionEnded {
		t.Fatalf("closed history = %+v", closed)
	}
}

func TestHeartbeatKeepsSessionAlive(t *testing.T) {
	h := newHarness(t, nil)
	sess := h.register("claude-code", "/a")
	for i := 0; i < 5; i++ {
		h.advance(30 * time.Second)
		if err := h.store.Heartbeat(sess); err != nil {
			t.Fatalf("heartbeat %d: %v", i, err)
		}
	}
	h.store.Sweep()
	if _, err := h.store.Unforward(sess, nil, true); err != nil {
		t.Fatalf("session expired despite heartbeats: %v", err)
	}
}

func TestReRegisterTakesBackForwardsWithinGrace(t *testing.T) {
	h := newHarness(t, nil)
	first := h.register("claude-code", "/apps/web")
	out := h.requestAsync(first, batch("api", 3000))
	_, _ = h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession, Ports: []PortDecision{{RemotePort: 3000}}})
	<-out
	h.store.UnregisterSession(first)

	again, err := h.store.RegisterSession(portsproto.RegisterSessionRequest{V: 1, Agent: "claude-code", PID: 2, Cwd: "/apps/web"})
	if err != nil {
		t.Fatal(err)
	}
	if !again.Resumed {
		t.Fatal("same agent and folder within grace did not resume")
	}
	fw := h.store.Forwards(again.ID)
	if len(fw) != 1 || fw[0].State != "active" || !fw[0].Mine {
		t.Fatalf("after resume = %+v", fw)
	}
}

func TestReRegisterOtherFolderDoesNotTakeForwards(t *testing.T) {
	h := newHarness(t, nil)
	first := h.register("claude-code", "/apps/web")
	out := h.requestAsync(first, batch("api", 3000))
	_, _ = h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession, Ports: []PortDecision{{RemotePort: 3000}}})
	<-out
	h.store.UnregisterSession(first)
	other := h.register("claude-code", "/apps/other")
	fw := h.store.Forwards(other)
	if len(fw) != 1 || fw[0].Mine {
		t.Fatalf("other folder sees %+v", fw)
	}
}

func TestPinnedSurvivesSessionEnd(t *testing.T) {
	h := newHarness(t, nil)
	sess := h.register("claude-code", "/a")
	out := h.requestAsync(sess, batch("db ui", 3000))
	_, _ = h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationPin, Ports: []PortDecision{{RemotePort: 3000}}})
	<-out
	h.store.UnregisterSession(sess)
	h.advance(time.Hour)
	h.store.Sweep()
	if !h.fwd.isOpen(3000) {
		t.Fatal("pinned forward closed when its session ended")
	}
}

func TestOneHourApprovalExpires(t *testing.T) {
	h := newHarness(t, nil)
	sess := h.register("claude-code", "/a")
	out := h.requestAsync(sess, batch("demo", 3000))
	_, _ = h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationHour, Ports: []PortDecision{{RemotePort: 3000}}})
	<-out
	h.advance(59 * time.Minute)
	h.store.Sweep()
	if !h.fwd.isOpen(3000) {
		t.Fatal("expired before one hour")
	}
	h.advance(2 * time.Minute)
	h.store.Sweep()
	if h.fwd.isOpen(3000) {
		t.Fatal("still open after one hour")
	}
}

func TestBusyLocalPortIsRemappedWithOwnerDescription(t *testing.T) {
	h := newHarness(t, nil)
	h.busy[3000] = true
	sess := h.register("claude-code", "/a")
	out := h.requestAsync(sess, batch("web", 3000))
	res, err := h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession, Ports: []PortDecision{{RemotePort: 3000}}})
	if err != nil {
		t.Fatal(err)
	}
	<-out
	if res[0].State != portsproto.StateActive || res[0].LocalPort != 3001 {
		t.Fatalf("result = %+v, want remapped to 3001", res[0])
	}
	if res[0].Message == "" {
		t.Error("remap carries no message describing the owner")
	}
}

func TestTwoForwardsNeverShareALocalPort(t *testing.T) {
	h := newHarness(t, nil)
	h.busy[3000] = true
	sess := h.register("claude-code", "/a")
	out := h.requestAsync(sess, batch("web", 3000, 3001))
	res, _ := h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession,
		Ports: []PortDecision{{RemotePort: 3000}, {RemotePort: 3001}}})
	<-out
	if res[0].LocalPort == res[1].LocalPort {
		t.Fatalf("both forwards got local %d", res[0].LocalPort)
	}
}

func TestForwarderFailureIsAPortErrorNotAPanic(t *testing.T) {
	h := newHarness(t, func(o *Options) {})
	h.fwd.addErr = errors.New("ssh -O forward failed")
	sess := h.register("claude-code", "/a")
	out := h.requestAsync(sess, batch("web", 3000))
	res, err := h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession, Ports: []PortDecision{{RemotePort: 3000}}})
	if err != nil {
		t.Fatal(err)
	}
	<-out
	if res[0].State != portsproto.StateError {
		t.Fatalf("result = %+v", res[0])
	}
}

func TestBlockedPortsAreDeniedWithoutAskingUser(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Blocked = []int{39123} })
	sess := h.register("claude-code", "/a")
	resp, err := h.store.Request(cancelled(), sess, batch("ssh", 22, 39123))
	if err != nil {
		t.Fatal(err)
	}
	if len(h.store.Pending()) != 0 {
		t.Fatal("blocked ports reached the approval queue")
	}
	for _, r := range resp.Results {
		if r.State != portsproto.StateDenied {
			t.Errorf("port %d state %s", r.RemotePort, r.State)
		}
	}
}

func TestForwardLimitPerProfile(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.MaxForwards = 1 })
	sess := h.register("claude-code", "/a")
	out := h.requestAsync(sess, batch("two", 3000, 4000))
	res, _ := h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession,
		Ports: []PortDecision{{RemotePort: 3000}, {RemotePort: 4000}}})
	<-out
	if res[0].State != portsproto.StateActive || res[1].State != portsproto.StateError {
		t.Fatalf("results = %+v, want one active and one limited", res)
	}
}

func TestInvalidApprovalDurationIsRejected(t *testing.T) {
	h := newHarness(t, nil)
	sess := h.register("claude-code", "/a")
	_ = h.requestAsync(sess, batch("x", 3000))
	if _, err := h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: "forever"}); !errors.Is(err, ErrInvalidDuration) {
		t.Fatalf("err = %v, want ErrInvalidDuration", err)
	}
}

func TestRequestFromUnknownSessionIsRejected(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.store.Request(cancelled(), "nope", batch("x", 3000)); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("err = %v", err)
	}
}

func TestInvalidRequestIsRejectedBeforeAnyState(t *testing.T) {
	h := newHarness(t, nil)
	sess := h.register("claude-code", "/a")
	var inv *InvalidError
	_, err := h.store.Request(cancelled(), sess, batch("", 3000))
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want InvalidError", err)
	}
	if len(h.store.Pending()) != 0 {
		t.Fatal("invalid request queued")
	}
}

func TestUserCloseWinsOverOwners(t *testing.T) {
	h := newHarness(t, nil)
	sess := h.register("claude-code", "/a")
	out := h.requestAsync(sess, batch("api", 3000))
	_, _ = h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession, Ports: []PortDecision{{RemotePort: 3000}}})
	<-out
	if err := h.store.CloseByUser(3000); err != nil {
		t.Fatal(err)
	}
	if h.fwd.isOpen(3000) {
		t.Fatal("user close left the port open")
	}
	if err := h.store.CloseByUser(3000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second close = %v", err)
	}
}

func TestSnapshotRestoreIsReservedUntilResume(t *testing.T) {
	h := newHarness(t, nil)
	sess := h.register("claude-code", "/a")
	out := h.requestAsync(sess, batch("api", 3000))
	_, _ = h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationPin, Ports: []PortDecision{{RemotePort: 3000}}})
	<-out
	snap := h.store.Snapshot()
	if len(snap) != 1 || snap[0].LocalPort != 3000 || !snap[0].Pinned {
		t.Fatalf("snapshot = %+v", snap)
	}

	fresh := newHarness(t, nil)
	fresh.store.Restore(snap)
	fw := fresh.store.Forwards("")
	if len(fw) != 1 || fw[0].State != "reserved" {
		t.Fatalf("restored = %+v, want reserved", fw)
	}
	if fresh.fwd.isOpen(3000) {
		t.Fatal("restore opened a port before the tunnel was up")
	}
	fresh.store.Resume(context.Background())
	if !fresh.fwd.isOpen(3000) {
		t.Fatal("resume did not reopen the pinned forward")
	}
	// Nobody owns a restored forward until an agent claims it, so it reads as
	// session_ended; pinned, it stays open. It must no longer be reserved.
	if fw := fresh.store.Forwards(""); fw[0].State == "reserved" {
		t.Fatalf("after resume state = %s", fw[0].State)
	}
}

func TestSaveCallbackRunsOnChange(t *testing.T) {
	var saves int
	h := newHarness(t, func(o *Options) {
		o.Save = func([]Record) error { saves++; return nil }
	})
	sess := h.register("claude-code", "/a")
	out := h.requestAsync(sess, batch("api", 3000))
	_, _ = h.store.Approve(context.Background(), h.firstPending().ID, Approval{Duration: DurationSession, Ports: []PortDecision{{RemotePort: 3000}}})
	<-out
	if saves == 0 {
		t.Fatal("state was not persisted after an approval")
	}
}
