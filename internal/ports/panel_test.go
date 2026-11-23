package ports

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/hostport"
)

func openUser(t *testing.T, h *harness, remote int) {
	t.Helper()
	if _, err := h.store.OpenByUser(context.Background(), remote, 0); err != nil {
		t.Fatal(err)
	}
}

func TestEditForwardChangesLabelDurationAndLocalPort(t *testing.T) {
	h := newHarness(t, nil)
	openUser(t, h, 5432)
	label := "database"
	info, err := h.store.EditForward(context.Background(), 5432, ForwardEdit{Label: &label, Duration: DurationHour, LocalPort: 15432})
	if err != nil {
		t.Fatal(err)
	}
	if info.Label != "database" || info.LocalPort != 15432 {
		t.Fatalf("edit = %+v", info)
	}
	if h.fwd.active[5432] != 15432 {
		t.Fatalf("forwarder not switched to the new local port: %v", h.fwd.active)
	}
	h.store.mu.Lock()
	expires := h.store.forwards[5432].expiresAt
	h.store.mu.Unlock()
	if !expires.Equal(h.now.Add(time.Hour)) {
		t.Fatalf("1h duration: expires %v", expires)
	}
}

func TestEditForwardRejectsBusyLocalPortAndBadInput(t *testing.T) {
	h := newHarness(t, nil)
	openUser(t, h, 5432)
	h.busy[15432] = true
	if _, err := h.store.EditForward(context.Background(), 5432, ForwardEdit{LocalPort: 15432}); err == nil {
		t.Fatal("busy local port accepted")
	}
	if h.fwd.active[5432] != 5432 {
		t.Fatalf("a refused edit moved the forward: %v", h.fwd.active)
	}
	if _, err := h.store.EditForward(context.Background(), 5432, ForwardEdit{Duration: "forever"}); !errors.Is(err, ErrInvalidDuration) {
		t.Fatalf("bad duration: %v", err)
	}
	long := strings.Repeat("x", 200)
	if _, err := h.store.EditForward(context.Background(), 5432, ForwardEdit{Label: &long}); err == nil {
		t.Fatal("overlong label accepted")
	}
	if _, err := h.store.EditForward(context.Background(), 9999, ForwardEdit{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown forward: %v", err)
	}
}

func TestCloseAllClosesEveryForwardInHistory(t *testing.T) {
	h := newHarness(t, nil)
	openUser(t, h, 5432)
	openUser(t, h, 8080)
	closed, err := h.store.CloseAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(closed) != 2 || closed[0] != 5432 || closed[1] != 8080 {
		t.Fatalf("closed = %v", closed)
	}
	if len(h.store.Forwards("")) != 0 || len(h.store.Closed()) != 2 {
		t.Fatalf("state after close-all: forwards=%v closed=%v", h.store.Forwards(""), h.store.Closed())
	}
	if h.store.Closed()[0].Reason != ReasonUser {
		t.Fatalf("reason = %q", h.store.Closed()[0].Reason)
	}
}

func TestReopenClosedRestoresWithChangesAndDropsHistory(t *testing.T) {
	h := newHarness(t, nil)
	openUser(t, h, 5432)
	if _, err := h.store.CloseAll(); err != nil {
		t.Fatal(err)
	}
	closedAt := h.store.Closed()[0].ClosedAt
	info, err := h.store.ReopenClosed(context.Background(), ReopenRequest{RemotePort: 5432, LocalPort: 15432, Label: "db", Duration: DurationPin, ClosedAt: closedAt})
	if err != nil {
		t.Fatal(err)
	}
	if info.LocalPort != 15432 || info.Label != "db" || !info.Pinned {
		t.Fatalf("reopened = %+v", info)
	}
	if len(h.store.Closed()) != 0 {
		t.Fatalf("history not cleaned: %v", h.store.Closed())
	}
	if _, err := h.store.ReopenClosed(context.Background(), ReopenRequest{RemotePort: 5432}); !errors.Is(err, ErrAlreadyOpen) {
		t.Fatalf("reopen of an open port: %v", err)
	}
}

func TestForgetClosedRemovesOneEntry(t *testing.T) {
	h := newHarness(t, nil)
	openUser(t, h, 5432)
	if _, err := h.store.CloseAll(); err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForgetClosed(5432, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForgetClosed(5432, time.Time{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second forget: %v", err)
	}
}

func TestCheckLocalReportsWhatHoldsThePort(t *testing.T) {
	h := newHarness(t, nil)
	openUser(t, h, 5432)
	check := func(port int) LocalCheck {
		t.Helper()
		c, err := h.store.CheckLocal(context.Background(), port)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if c := check(7000); c.Status != LocalFree {
		t.Fatalf("free port: %+v", c)
	}
	if c := check(5432); c.Status != LocalOpenPort || c.Remote != 5432 {
		t.Fatalf("own forward port should be checked on its local port: %+v", c)
	}
	h.busy[7001] = true
	h.store.opts.Owner = func(context.Context, int) (*hostport.Owner, error) {
		return &hostport.Owner{Port: 7001, PID: 9, Process: "node", Kind: hostport.KindProc}, nil
	}
	if c := check(7001); c.Status != LocalProcess || !c.Stoppable || c.PID != 9 {
		t.Fatalf("process: %+v", c)
	}
	h.busy[7002] = true
	h.store.opts.Owner = func(context.Context, int) (*hostport.Owner, error) {
		return &hostport.Owner{Port: 7002, PID: 10, Process: "agentport", Kind: hostport.KindProc}, nil
	}
	if c := check(7002); c.Status != LocalOtherProfile {
		t.Fatalf("other profile: %+v", c)
	}
	if _, err := h.store.CheckLocal(context.Background(), 0); err == nil {
		t.Fatal("port 0 accepted")
	}
}

func TestStopOwnerAndOpenStopsOnlyStoppableOwners(t *testing.T) {
	var stopped []string
	h := newHarness(t, func(o *Options) {
		o.StopOwner = func(_ context.Context, owner *hostport.Owner) error {
			stopped = append(stopped, owner.Process)
			return nil
		}
	})
	h.busy[5432] = true
	h.store.opts.Owner = func(context.Context, int) (*hostport.Owner, error) {
		return &hostport.Owner{Port: 5432, PID: 77, Process: "postgres", Kind: hostport.KindService, StopCmd: []string{"brew", "services", "stop", "postgresql"}}, nil
	}
	// The stop frees the port in the fake, so the original port is used.
	h.store.opts.StopOwner = func(_ context.Context, owner *hostport.Owner) error {
		stopped = append(stopped, owner.Process)
		h.busy[5432] = false
		return nil
	}
	info, err := h.store.StopOwnerAndOpen(context.Background(), 5432, 5432)
	if err != nil {
		t.Fatal(err)
	}
	if info.LocalPort != 5432 || len(stopped) != 1 || stopped[0] != "postgres" {
		t.Fatalf("info=%+v stopped=%v", info, stopped)
	}
}

func TestStopOwnerFallsBackAndRecordsActivity(t *testing.T) {
	h := newHarness(t, nil)
	h.busy[5432] = true
	h.store.opts.Owner = func(context.Context, int) (*hostport.Owner, error) {
		return &hostport.Owner{Port: 5432, PID: 77, Process: "node", Kind: hostport.KindProc}, nil
	}
	h.store.opts.StopOwner = func(context.Context, *hostport.Owner) error { return nil } // port stays busy
	info, err := h.store.StopOwnerAndOpen(context.Background(), 5432, 5432)
	if err != nil {
		t.Fatal(err)
	}
	if info.LocalPort == 5432 {
		t.Fatalf("expected a fallback local port, got %d", info.LocalPort)
	}
	found := false
	for _, a := range h.store.Activity() {
		if strings.Contains(a.Text, "still busy") && a.Kind == ActivityAsk {
			found = true
		}
	}
	if !found {
		t.Fatalf("fallback not recorded in activity: %+v", h.store.Activity())
	}
}

func TestStopOwnerNeverStopsSystemOwners(t *testing.T) {
	called := false
	h := newHarness(t, func(o *Options) {
		o.StopOwner = func(context.Context, *hostport.Owner) error { called = true; return nil }
	})
	h.busy[22] = true
	h.store.opts.Owner = func(context.Context, int) (*hostport.Owner, error) {
		return &hostport.Owner{Port: 22, PID: 1, Process: "launchd", Kind: hostport.KindSystem}, nil
	}
	if _, err := h.store.StopOwnerAndOpen(context.Background(), 22, 22); !errors.Is(err, hostport.ErrNotStoppable) {
		t.Fatalf("system owner: %v", err)
	}
	if called {
		t.Fatal("a system owner was stopped")
	}
}

func TestVersionAndActivityAdvanceWithChanges(t *testing.T) {
	h := newHarness(t, nil)
	before := h.store.Version()
	openUser(t, h, 5432)
	if h.store.Version() == before {
		t.Fatal("opening a forward did not change the version")
	}
	acts := h.store.Activity()
	if len(acts) == 0 || !strings.Contains(acts[len(acts)-1].Text, "5432") {
		t.Fatalf("activity = %+v", acts)
	}
}

func TestOnPendingHookRunsForNewRequests(t *testing.T) {
	h := newHarness(t, nil)
	seen := make(chan PendingRequest, 1)
	h.store.SetOnPending(func(p PendingRequest) { seen <- p })
	session := h.register("claude", "/work")
	_ = h.requestAsync(session, batch("db", 5432))
	select {
	case p := <-seen:
		if p.Agent != "claude" || len(p.Ports) != 1 {
			t.Fatalf("hook got %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hook not called")
	}
}

func TestPanelRoutesEditRenamePinAndCloseAll(t *testing.T) {
	store := newStoreForHandler(t)
	h := NewHandler(store)
	if rec := do(t, h, http.MethodPost, "/v1/ports/forwards/open", "", map[string]any{"remote_port": 5432}); rec.Code != http.StatusOK {
		t.Fatalf("open: %d %s", rec.Code, rec.Body.String())
	}
	rec := do(t, h, http.MethodPost, "/v1/ports/forwards/5432/rename", "", map[string]any{"label": "db"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"label":"db"`) {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/ports/forwards/5432/pin", "", map[string]any{"pinned": true})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pinned":true`) {
		t.Fatalf("pin: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/ports/forwards/5432/edit", "", map[string]any{"duration": "bogus"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad duration status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/ports/forwards/close-all", "", map[string]any{})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"closed":[5432]`) {
		t.Fatalf("close-all: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPanelRoutesRejectBadPortsAndUnknownForwards(t *testing.T) {
	h := NewHandler(newStoreForHandler(t))
	cases := []struct {
		method, path string
		body         any
		want         int
	}{
		{http.MethodPost, "/v1/ports/forwards/0/rename", map[string]any{"label": "x"}, http.StatusBadRequest},
		{http.MethodPost, "/v1/ports/forwards/abc/pin", map[string]any{"pinned": true}, http.StatusBadRequest},
		{http.MethodPost, "/v1/ports/forwards/9999/edit", map[string]any{"label": "x"}, http.StatusNotFound},
		{http.MethodPost, "/v1/ports/closed/reopen", map[string]any{"remote_port": 0}, http.StatusBadRequest},
		{http.MethodPost, "/v1/ports/closed/reopen", map[string]any{"remote_port": 5432, "duration": "nope"}, http.StatusBadRequest},
		{http.MethodPost, "/v1/ports/closed/forget", map[string]any{"remote_port": 5432}, http.StatusNotFound},
		{http.MethodPost, "/v1/ports/forwards/stop-owner", map[string]any{"remote_port": 5432, "local_port": 0}, http.StatusBadRequest},
		{http.MethodGet, "/v1/ports/local/check?port=0", nil, http.StatusBadRequest},
		{http.MethodGet, "/v1/ports/local/check?port=x", nil, http.StatusBadRequest},
	}
	for _, c := range cases {
		rec := do(t, h, c.method, c.path, "", c.body)
		if rec.Code != c.want {
			t.Errorf("%s %s = %d, want %d (%s)", c.method, c.path, rec.Code, c.want, rec.Body.String())
		}
	}
}

func TestLocalCheckRouteAnswersJSON(t *testing.T) {
	h := NewHandler(newStoreForHandler(t))
	rec := do(t, h, http.MethodGet, "/v1/ports/local/check?port=7000", "", nil)
	var got LocalCheck
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got.Status != LocalFree {
		t.Fatalf("check: %d %s", rec.Code, rec.Body.String())
	}
}

func TestStateIncludesVersionAndActivity(t *testing.T) {
	h := NewHandler(newStoreForHandler(t))
	do(t, h, http.MethodPost, "/v1/ports/forwards/open", "", map[string]any{"remote_port": 5432})
	rec := do(t, h, http.MethodGet, "/v1/ports/state", "", nil)
	if !strings.Contains(rec.Body.String(), `"version":`) || !strings.Contains(rec.Body.String(), `"activity":[`) {
		t.Fatalf("state = %s", rec.Body.String())
	}
}

func TestEventsStreamSendsTheVersion(t *testing.T) {
	store := newStoreForHandler(t)
	h := NewHandler(store)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/v1/ports/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "event: state\ndata: {\"version\":") {
		t.Fatalf("no state event: %q", rec.Body.String())
	}
}

func TestForwarderFailureOnApprovalIsRecordedInActivity(t *testing.T) {
	h := newHarness(t, nil)
	h.fwd.addErr = errors.New("tunnel is down")
	session := h.register("claude", "/work")
	ch := h.requestAsync(session, batch("db", 5432))
	p := h.firstPending()
	if _, err := h.store.Approve(context.Background(), p.ID, Approval{Duration: DurationSession, Ports: []PortDecision{{RemotePort: 5432}}}); err != nil {
		t.Fatal(err)
	}
	<-ch
	acts := h.store.Activity()
	if len(acts) == 0 || acts[len(acts)-1].Kind != ActivityDenied || !strings.Contains(acts[len(acts)-1].Text, "tunnel is down") {
		t.Fatalf("failure not recorded: %+v", acts)
	}
}
