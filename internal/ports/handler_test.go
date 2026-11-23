package ports

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/portsproto"
)

func do(t *testing.T, h http.Handler, method, path, session string, body any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Error(err)
			return nil
		}
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if session != "" {
		req.Header.Set(SessionHeader, session)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func registerHTTP(t *testing.T, h http.Handler, agent, cwd string) string {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/v1/ports/sessions", "", portsproto.RegisterSessionRequest{V: 1, Agent: agent, PID: 7, Cwd: cwd})
	if rec.Code != http.StatusOK {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var resp portsproto.RegisterSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.HeartbeatSeconds != 20 {
		t.Fatalf("heartbeat cadence = %d, want 20", resp.HeartbeatSeconds)
	}
	return resp.ID
}

func TestRemoteFlowRegisterForwardApproveUnforward(t *testing.T) {
	store := newStoreForHandler(t)
	h := NewHandler(store)
	session := registerHTTP(t, h, "claude-code", "/apps/web")

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- do(t, h, http.MethodPost, "/v1/ports/forwards", session,
			portsproto.ForwardRequest{V: 1, Reason: "dev server", Ports: []portsproto.PortRequest{{RemotePort: 3000, Label: "web"}}})
	}()
	pending := waitPending(t, store)

	rec := do(t, h, http.MethodPost, "/v1/ports/requests/"+pending.ID+"/approve", "", map[string]any{
		"duration": "session", "ports": []map[string]any{{"remote_port": 3000}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body.String())
	}
	got := <-done
	if got.Code != http.StatusOK {
		t.Fatalf("forward: %d %s", got.Code, got.Body.String())
	}
	var resp portsproto.ForwardResponse
	if err := json.Unmarshal(got.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 || resp.Results[0].State != portsproto.StateActive || resp.Results[0].URL != "http://localhost:3000" {
		t.Fatalf("results = %+v", resp.Results)
	}

	list := do(t, h, http.MethodGet, "/v1/ports/forwards", session, nil)
	if !strings.Contains(list.Body.String(), `"mine":true`) {
		t.Fatalf("forwards list does not mark the session as owner: %s", list.Body.String())
	}
	un := do(t, h, http.MethodPost, "/v1/ports/forwards/unforward", session, portsproto.UnforwardRequest{V: 1, All: true})
	if un.Code != http.StatusOK || !strings.Contains(un.Body.String(), `"closed":[3000]`) {
		t.Fatalf("unforward: %d %s", un.Code, un.Body.String())
	}
}

func TestRemoteRoutesRejectBadInput(t *testing.T) {
	store := newStoreForHandler(t)
	h := NewHandler(store)
	session := registerHTTP(t, h, "claude-code", "/a")

	cases := []struct {
		name    string
		method  string
		path    string
		session string
		body    any
		status  int
	}{
		{"forward without session header", http.MethodPost, "/v1/ports/forwards", "", portsproto.ForwardRequest{V: 1, Reason: "r", Ports: []portsproto.PortRequest{{RemotePort: 1}}}, http.StatusBadRequest},
		{"forward with bad port", http.MethodPost, "/v1/ports/forwards", session, portsproto.ForwardRequest{V: 1, Reason: "r", Ports: []portsproto.PortRequest{{RemotePort: 70000}}}, http.StatusBadRequest},
		{"forward with unknown session", http.MethodPost, "/v1/ports/forwards", "nope", portsproto.ForwardRequest{V: 1, Reason: "r", Ports: []portsproto.PortRequest{{RemotePort: 1}}}, http.StatusNotFound},
		{"register with wrong version", http.MethodPost, "/v1/ports/sessions", "", portsproto.RegisterSessionRequest{V: 9, Agent: "a"}, http.StatusBadRequest},
		{"heartbeat unknown session", http.MethodPost, "/v1/ports/sessions/zzz/heartbeat", "", nil, http.StatusNotFound},
		{"unforward nothing", http.MethodPost, "/v1/ports/forwards/unforward", session, portsproto.UnforwardRequest{V: 1}, http.StatusBadRequest},
		{"approve unknown request", http.MethodPost, "/v1/ports/requests/nope/approve", "", map[string]any{"duration": "session"}, http.StatusNotFound},
		{"approve bad duration", http.MethodPost, "/v1/ports/requests/nope/approve", "", map[string]any{"duration": "forever"}, http.StatusBadRequest},
		{"deny unknown request", http.MethodPost, "/v1/ports/requests/nope/deny", "", nil, http.StatusNotFound},
		{"open bad port", http.MethodPost, "/v1/ports/forwards/open", "", map[string]any{"remote_port": 0}, http.StatusBadRequest},
		{"body that is not an object", http.MethodPost, "/v1/ports/forwards/open", "", "not-an-object", http.StatusBadRequest},
		{"method not allowed", http.MethodGet, "/v1/ports/sessions", "", nil, http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(t, h, c.method, c.path, c.session, c.body)
			if rec.Code != c.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, c.status, rec.Body.String())
			}
		})
	}
}

func TestStateEndpointListsPendingAndForwards(t *testing.T) {
	store := newStoreForHandler(t)
	h := NewHandler(store)
	session := registerHTTP(t, h, "claude-code", "/a")
	go do(t, h, http.MethodPost, "/v1/ports/forwards", session,
		portsproto.ForwardRequest{V: 1, Reason: "api", Ports: []portsproto.PortRequest{{RemotePort: 8080}}})
	waitPending(t, store)
	rec := do(t, h, http.MethodGet, "/v1/ports/state", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("state: %d", rec.Code)
	}
	var state struct {
		Pending []PendingRequest `json:"pending"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Pending) != 1 || state.Pending[0].Agent != "claude-code" || state.Pending[0].Ports[0].RemotePort != 8080 {
		t.Fatalf("state pending = %+v", state.Pending)
	}
	// Deny so the goroutine above finishes and the test leaves nothing behind.
	if err := store.Deny(state.Pending[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestUserOpenAndCloseThroughLocalAPI(t *testing.T) {
	store := newStoreForHandler(t)
	h := NewHandler(store)
	rec := do(t, h, http.MethodPost, "/v1/ports/forwards/open", "", map[string]any{"remote_port": 5432})
	if rec.Code != http.StatusOK {
		t.Fatalf("open: %d %s", rec.Code, rec.Body.String())
	}
	closed := do(t, h, http.MethodPost, "/v1/ports/forwards/close", "", map[string]any{"remote_ports": []int{5432, 9999}})
	if !strings.Contains(closed.Body.String(), `"closed":[5432]`) || !strings.Contains(closed.Body.String(), `"not_open":[9999]`) {
		t.Fatalf("close = %s", closed.Body.String())
	}
}

// newStoreForHandler is a Store wired to fakes, for HTTP tests.
func newStoreForHandler(t *testing.T) *Store {
	t.Helper()
	h := newHarness(t, nil)
	return h.store
}

// waitPending polls the queue until one request appears and returns it.
func waitPending(t *testing.T, store *Store) PendingRequest {
	t.Helper()
	for i := 0; i < 10000; i++ {
		if p := store.Pending(); len(p) > 0 {
			return p[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no request reached the queue")
	return PendingRequest{}
}
