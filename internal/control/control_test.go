package control

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rafaelb13/agentport/internal/daemon"
	"github.com/rafaelb13/agentport/internal/release"
)

func stateFor(server *httptest.Server, token string) daemon.State {
	return daemon.State{Address: server.Listener.Addr().String(), ControlToken: token}
}

func TestPostSendsControlTokenAndDecodesReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer control-1" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/control/release" || !strings.Contains(string(body), "v1.1.0") {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"updated": true})
	}))
	defer server.Close()
	state := stateFor(server, "control-1")
	if err := PublishReleaseStatus(state, release.Status{CurrentVersion: "v1.0.0", LatestVersion: "v1.1.0"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := Post(stateFor(server, "wrong"), "/v1/control/release", []byte(`{}`), nil); err == nil {
		t.Fatal("wrong token was accepted")
	}
}

func TestHealthyReadsHealthz(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if !Healthy(stateFor(server, "x")) {
		t.Fatal("healthy bridge reported unhealthy")
	}
	if Healthy(daemon.State{Address: "192.0.2.1:9"}) {
		t.Fatal("non-loopback address reported healthy")
	}
}

func TestValidLoopbackAddressAndPort(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:4000":  true,
		"localhost:4000":  false,
		"0.0.0.0:4000":    false,
		"127.0.0.1:0":     false,
		"127.0.0.1:70000": false,
	}
	for address, want := range cases {
		if got := ValidLoopbackAddress(address); got != want {
			t.Fatalf("ValidLoopbackAddress(%q) = %v, want %v", address, got, want)
		}
	}
	if got := Port(daemon.State{Address: "127.0.0.1:4123"}); got != 4123 {
		t.Fatalf("Port = %d", got)
	}
}

func TestEndpointRejectsAuthorityTricks(t *testing.T) {
	state := daemon.State{Address: "127.0.0.1:4000", ControlToken: "x"}
	for _, path := range []string{"//evil.example/x", "@evil.example/x", "relative"} {
		if _, err := endpoint(state, path); err == nil {
			t.Fatalf("path %q was accepted", path)
		}
	}
	target, err := endpoint(state, "/v1/control/shutdown")
	if err != nil || target != "http://127.0.0.1:4000/v1/control/shutdown" {
		t.Fatalf("endpoint = %q, %v", target, err)
	}
}
