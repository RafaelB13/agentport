package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/forward"
	"github.com/rafaelb13/agentport/internal/ports"
	"github.com/rafaelb13/agentport/internal/portsproto"
)

// recordingForwarder stands in for the SSH tunnel: it only remembers what is open.
type recordingForwarder struct {
	mu   sync.Mutex
	open map[int]int
}

func (f *recordingForwarder) Add(_ context.Context, remote, local int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.open == nil {
		f.open = map[int]int{}
	}
	f.open[remote] = local
	return nil
}

func (f *recordingForwarder) Remove(_ context.Context, remote, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.open[remote]; !ok {
		return forward.ErrNotFound
	}
	delete(f.open, remote)
	return nil
}

func (f *recordingForwarder) List() []forward.Forward { return nil }

func (f *recordingForwarder) Reapply(context.Context) error { return nil }

// newCompanionStore is a Companion's port store with approvals that never clash
// with the host's own ports, and a heartbeat the test can wait for.
func newCompanionStore(t *testing.T, snap []ports.Record) *ports.Store {
	t.Helper()
	store, err := ports.NewStore(ports.Options{
		Forwarder: &recordingForwarder{},
		Busy:      func(int) bool { return false },
		Heartbeat: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.Restore(snap)
	store.Resume(context.Background())
	return store
}

// relayCompanion plays the bridge and the Companion together: it checks the token,
// maps /v1/... to the Companion's port protocol the way the bridge does, and can
// be restarted with a fresh session table.
type relayCompanion struct {
	server  *httptest.Server
	mu      sync.Mutex
	store   *ports.Store
	handler http.Handler
	offline bool
}

func newRelayCompanion(t *testing.T, store *ports.Store) *relayCompanion {
	t.Helper()
	r := &relayCompanion{store: store, handler: ports.NewHandler(store)}
	r.server = httptest.NewServer(r)
	t.Cleanup(r.server.Close)
	return r
}

func (r *relayCompanion) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	handler, offline := r.handler, r.offline
	r.mu.Unlock()
	if req.Header.Get("Authorization") != "Bearer "+fakeToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if offline {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(portsproto.ErrorBody{Code: portsproto.CodeCompanionOff, Message: "the Companion is not running"})
		return
	}
	req.URL.Path = "/v1/ports" + strings.TrimPrefix(req.URL.Path, "/v1")
	handler.ServeHTTP(w, req)
}

// restart replaces the Companion's process: a new store with no sessions, the
// forwards restored from the snapshot as reserved and then resumed.
func (r *relayCompanion) restart(t *testing.T, snap []ports.Record) *ports.Store {
	t.Helper()
	store := newCompanionStore(t, snap)
	r.mu.Lock()
	r.store = store
	r.handler = ports.NewHandler(store)
	r.mu.Unlock()
	return store
}

func (r *relayCompanion) current() *ports.Store {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store
}

func (r *relayCompanion) setOffline(v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.offline = v
}

func (r *relayCompanion) port(t *testing.T) int {
	t.Helper()
	_, p, err := net.SplitHostPort(strings.TrimPrefix(r.server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(p)
	return n
}

// approveWhenQueued approves the next pending request of store for remote, as the
// user does from the panel.
func approveWhenQueued(t *testing.T, store *ports.Store, remote int) {
	t.Helper()
	go func() {
		for i := 0; i < 5000; i++ {
			if pending := store.Pending(); len(pending) > 0 {
				_, _ = store.Approve(context.Background(), pending[0].ID, ports.Approval{
					Duration: ports.DurationSession, Ports: []ports.PortDecision{{RemotePort: remote}},
				})
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
}

// waitUntil polls check for up to timeout and fails the test with what when it never holds.
func waitUntil(t *testing.T, timeout time.Duration, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

// ownedBy reports whether the store has exactly one forward on remote, active, and
// owned by exactly the agent named agent.
func ownedBy(store *ports.Store, remote int, agent string) bool {
	for _, f := range store.Forwards("") {
		if f.RemotePort != remote {
			continue
		}
		return f.State == "active" && len(f.Owners) == 1 && f.Owners[0] == agent
	}
	return false
}

func newTestClient(t *testing.T, relay *relayCompanion) *BridgeClient {
	t.Helper()
	client, err := NewBridgeClient(relay.port(t), fakeToken, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func TestCompanionRestartReclaimsForwardsWithoutAnyFurtherCall(t *testing.T) {
	relay := newRelayCompanion(t, newCompanionStore(t, nil))
	client := newTestClient(t, relay)

	approveWhenQueued(t, relay.current(), 3000)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := client.Forward(ctx, portsproto.ForwardRequest{V: portsproto.Version, Reason: "api", Ports: []portsproto.PortRequest{{RemotePort: 3000}}})
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if len(out.Results) != 1 || out.Results[0].State != portsproto.StateActive {
		t.Fatalf("forward results = %+v, want active", out.Results)
	}

	// The Companion restarts. The agent makes no further calls: the session must
	// come back by itself through the heartbeat.
	snap := relay.current().Snapshot()
	fresh := relay.restart(t, snap)
	waitUntil(t, 10*time.Second, "port 3000 active again, owned by the agent", func() bool {
		return ownedBy(fresh, 3000, "claude-code")
	})
	if n := len(fresh.Forwards("")); n != 1 {
		t.Fatalf("after recovery %d forwards, want 1 (no duplicates)", n)
	}
}

func TestCallAfterCompanionRestartSeesTheForwardAsMine(t *testing.T) {
	relay := newRelayCompanion(t, newCompanionStore(t, nil))
	client := newTestClient(t, relay)

	approveWhenQueued(t, relay.current(), 5173)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Forward(ctx, portsproto.ForwardRequest{V: portsproto.Version, Reason: "web", Ports: []portsproto.PortRequest{{RemotePort: 5173}}}); err != nil {
		t.Fatalf("forward: %v", err)
	}

	fresh := relay.restart(t, relay.current().Snapshot())
	// A tool call right after the restart may meet an unknown session first; it
	// must register again and still answer with the forward as its own.
	forwards, err := client.Forwards(ctx)
	if err != nil {
		t.Fatalf("forwards after restart: %v", err)
	}
	mine := false
	for _, f := range forwards {
		if f.RemotePort == 5173 && f.Mine && f.State == "active" {
			mine = true
		}
	}
	if !mine {
		t.Fatalf("forwards after restart = %+v, want 5173 active and mine", forwards)
	}
	waitUntil(t, 10*time.Second, "5173 owned by the agent on the Companion", func() bool {
		return ownedBy(fresh, 5173, "claude-code")
	})
}

func TestRepeatedRestartsKeepOneOwnerPerForward(t *testing.T) {
	relay := newRelayCompanion(t, newCompanionStore(t, nil))
	client := newTestClient(t, relay)

	approveWhenQueued(t, relay.current(), 8080)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Forward(ctx, portsproto.ForwardRequest{V: portsproto.Version, Reason: "api", Ports: []portsproto.PortRequest{{RemotePort: 8080}}}); err != nil {
		t.Fatalf("forward: %v", err)
	}
	store := relay.current()
	for round := 1; round <= 2; round++ {
		store = relay.restart(t, store.Snapshot())
		waitUntil(t, 10*time.Second, "8080 owned by the agent after restart", func() bool {
			return ownedBy(store, 8080, "claude-code")
		})
	}
	if n := len(store.Forwards("")); n != 1 {
		t.Fatalf("%d forwards after two restarts, want 1", n)
	}
}

func TestForwardLostWhileWaitingIsReportedForRetry(t *testing.T) {
	bridge := newFakeBridge(t)
	session, _ := portsSession(t, bridge, nil)
	// The session is registered; the Companion dies while the request waits.
	if _, isErr := callText(t, session, "forwards_list", map[string]any{}); isErr {
		t.Fatal("forwards_list failed before the outage")
	}
	bridge.setOffline(true)
	text, isErr := callText(t, session, "ports_forward", map[string]any{
		"reason": "api", "ports": []map[string]any{{"remote_port": 8080}},
	})
	if !isErr || !strings.HasPrefix(text, "companion_restarted") || !strings.Contains(text, "Call ports_forward again") {
		t.Fatalf("lost request = %q (error %v), want companion_restarted with the retry instruction", text, isErr)
	}
}

func TestLostRequestErrorIsDetectableAndStillOffline(t *testing.T) {
	bridge := newFakeBridge(t)
	client, err := NewBridgeClient(bridge.port(t), fakeToken, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Forwards(context.Background()); err != nil {
		t.Fatal(err)
	}
	bridge.setOffline(true)
	_, err = client.Forward(context.Background(), portsproto.ForwardRequest{V: portsproto.Version, Reason: "x", Ports: []portsproto.PortRequest{{RemotePort: 8080}}})
	if !errors.Is(err, ErrRequestLost) || !CompanionOffline(err) {
		t.Fatalf("err = %v, want ErrRequestLost that is also companion offline", err)
	}
}
