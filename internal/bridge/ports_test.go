package bridge

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// upstreamServer stands in for a Companion's control API. It records the last
// request it received, so tests can check what the bridge relayed.
type upstreamServer struct {
	server   *httptest.Server
	gotPath  string
	gotAuth  string
	gotSess  string
	gotBody  string
	status   int
	response string
}

func newUpstream(t *testing.T, status int, response string) *upstreamServer {
	t.Helper()
	u := &upstreamServer{status: status, response: response}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.gotPath = r.URL.Path
		u.gotAuth = r.Header.Get("Authorization")
		u.gotSess = r.Header.Get("X-Agentport-Session")
		body, _ := io.ReadAll(r.Body)
		u.gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(u.status)
		_, _ = w.Write([]byte(u.response))
	}))
	t.Cleanup(u.server.Close)
	return u
}

// bridgeWithSession returns a bridge that accepts token for session id, with
// the given upstream address (empty means no upstream yet).
func bridgeWithSession(t *testing.T, id, token, upstreamAddr string) *Bridge {
	t.Helper()
	b := New()
	if err := b.RegisterPersistentSession(id, token); err != nil {
		t.Fatal(err)
	}
	if upstreamAddr != "" {
		if err := b.SetUpstream(id, upstreamAddr, "control-token"); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

func post(t *testing.T, handler http.Handler, path, token, session, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	if session != "" {
		request.Header.Set("X-Agentport-Session", session)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestPortsRouteIsRelayedWithUpstreamTokenAndSessionHeader(t *testing.T) {
	up := newUpstream(t, http.StatusOK, `{"results":[]}`)
	addr := strings.TrimPrefix(up.server.URL, "http://")
	b := bridgeWithSession(t, "companion:m2", "agent-token", addr)

	response := post(t, b.Handler(), "/v1/forwards", "agent-token", "sess-1", `{"v":1,"reason":"r"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	if up.gotPath != "/v1/ports/forwards" {
		t.Errorf("upstream path = %q, want /v1/ports/forwards", up.gotPath)
	}
	if up.gotAuth != "Bearer control-token" {
		t.Errorf("upstream auth = %q: the agent's token must not reach the Companion", up.gotAuth)
	}
	if up.gotSess != "sess-1" {
		t.Errorf("session header = %q", up.gotSess)
	}
	if up.gotBody != `{"v":1,"reason":"r"}` {
		t.Errorf("body not relayed: %q", up.gotBody)
	}
}

func TestPortsRouteNeedsAValidToken(t *testing.T) {
	up := newUpstream(t, http.StatusOK, `{}`)
	b := bridgeWithSession(t, "companion:m2", "agent-token", strings.TrimPrefix(up.server.URL, "http://"))
	if got := post(t, b.Handler(), "/v1/sessions", "wrong", "", `{}`); got.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d", got.Code)
	}
	if up.gotPath != "" {
		t.Fatal("a rejected request reached the Companion")
	}
}

func TestPortsRouteWithoutUpstreamSaysCompanionOffline(t *testing.T) {
	b := bridgeWithSession(t, "companion:m2", "agent-token", "")
	response := post(t, b.Handler(), "/v1/sessions", "agent-token", "", `{}`)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, `"code":"COMPANION_OFFLINE"`) || !strings.Contains(body, "agentport companion start m2") {
		t.Fatalf("body = %s, want COMPANION_OFFLINE with the start instruction", body)
	}
}

func TestPortsRouteWhenUpstreamIsDownSaysCompanionOffline(t *testing.T) {
	// A closed port: nothing listens there, so the relay fails.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	b := bridgeWithSession(t, "companion:m2", "agent-token", addr)
	response := post(t, b.Handler(), "/v1/forwards", "agent-token", "s", `{}`)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "COMPANION_OFFLINE") {
		t.Fatalf("status %d body %s", response.Code, response.Body.String())
	}
}

func TestPortsRelayKeepsUpstreamErrorBodies(t *testing.T) {
	up := newUpstream(t, http.StatusConflict, `{"code":"FORWARD_LIMIT","message":"limit"}`)
	b := bridgeWithSession(t, "companion:m2", "agent-token", strings.TrimPrefix(up.server.URL, "http://"))
	response := post(t, b.Handler(), "/v1/forwards", "agent-token", "s", `{}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "FORWARD_LIMIT") {
		t.Fatalf("status %d body %s", response.Code, response.Body.String())
	}
}

func TestSetUpstreamOnlyAcceptsLoopback(t *testing.T) {
	b := New()
	for _, addr := range []string{"10.0.0.5:9000", "0.0.0.0:9000", "localhost:9000", "127.0.0.1:0", "127.0.0.1:99999", "nonsense"} {
		if err := b.SetUpstream("companion:m2", addr, "t"); err == nil {
			t.Errorf("address %q accepted", addr)
		}
	}
	if err := b.SetUpstream("companion:m2", "127.0.0.1:9000", "t"); err != nil {
		t.Fatalf("loopback rejected: %v", err)
	}
	if err := b.SetUpstream("", "127.0.0.1:9000", "t"); err == nil {
		t.Error("empty ID accepted")
	}
}

func TestRemoveUpstreamMakesCompanionOffline(t *testing.T) {
	up := newUpstream(t, http.StatusOK, `{}`)
	b := bridgeWithSession(t, "companion:m2", "agent-token", strings.TrimPrefix(up.server.URL, "http://"))
	b.RemoveUpstream("companion:m2")
	if got := post(t, b.Handler(), "/v1/sessions", "agent-token", "", `{}`); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d after removal", got.Code)
	}
}

func TestUpstreamRedirectIsNotFollowed(t *testing.T) {
	elsewhere := newUpstream(t, http.StatusOK, `{"hijacked":true}`)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.server.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	b := bridgeWithSession(t, "companion:m2", "agent-token", strings.TrimPrefix(redirect.URL, "http://"))
	response := post(t, b.Handler(), "/v1/sessions", "agent-token", "", `{}`)
	if strings.Contains(response.Body.String(), "hijacked") {
		t.Fatal("bridge followed a redirect away from the loopback Companion")
	}
	if elsewhere.gotPath != "" {
		t.Fatal("redirect target was contacted")
	}
}
