package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rafaelb13/agentport/internal/release"
)

type fakeProvider struct {
	status Status
	err    error
}

func (f fakeProvider) Status(context.Context) (Status, error) { return f.status, f.err }

func connect(t *testing.T, provider StatusProvider) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := New(provider)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestToolErrorsAreMCPVisible(t *testing.T) {
	result, _, err := toolError(errors.New("bridge unreachable"))
	if err != nil || !result.IsError || len(result.Content) != 1 {
		t.Fatalf("unexpected error result: %#v, %v", result, err)
	}
}

func TestOnlyStatusToolIsRegistered(t *testing.T) {
	session := connect(t, fakeProvider{})
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "agentport_status" {
		t.Fatalf("tools = %+v, want only agentport_status", listed.Tools)
	}
}

func TestStatusToolReturnsTunnelAndRelease(t *testing.T) {
	session := connect(t, fakeProvider{status: Status{Bridge: "ok", Tunnel: "connected", Release: release.Status{CurrentVersion: "v1.0.0", LatestVersion: "v1.1.0", UpdateAvailable: true}}})
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "agentport_status", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("call failed: %v %+v", err, result)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{`"tunnel":"connected"`, `"latest_version":"v1.1.0"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("status %s lacks %s", text, want)
		}
	}
}

func TestStatusToolReportsProviderErrors(t *testing.T) {
	session := connect(t, fakeProvider{err: errors.New("bridge unreachable")})
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "agentport_status", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("provider error was not reported as a tool error: %+v", result)
	}
}

func TestHTTPProviderReadsBridgeStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer session-1" {
			http.Error(w, `{"code":"UNAUTHORIZED","message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"bridge": "ok", "release": map[string]any{"current_version": "v1.0.0"}})
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	provider, err := NewHTTPProvider(port, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	status, err := provider.Status(context.Background())
	if err != nil || status.Tunnel != "connected" || status.Bridge != "ok" || status.Release.CurrentVersion != "v1.0.0" {
		t.Fatalf("status = %+v, err %v", status, err)
	}
	bad, _ := NewHTTPProvider(port, "wrong")
	if _, err := bad.Status(context.Background()); err == nil || !strings.Contains(err.Error(), "UNAUTHORIZED") {
		t.Fatalf("wrong token: %v", err)
	}
}

func TestNewHTTPProviderValidatesInput(t *testing.T) {
	if _, err := NewHTTPProvider(0, "token"); err == nil {
		t.Fatal("port 0 accepted")
	}
	if _, err := NewHTTPProvider(4000, " "); err == nil {
		t.Fatal("empty token accepted")
	}
}
