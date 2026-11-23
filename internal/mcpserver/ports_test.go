package mcpserver

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rafaelb13/agentport/internal/portscan"
	"github.com/rafaelb13/agentport/internal/portsproto"
)

// fakeBridge answers the port protocol the way the bridge and Companion do, and
// records what the MCP side sent.
type fakeBridge struct {
	mu        sync.Mutex
	offline   bool
	pending   bool
	sessions  int
	deleted   []string
	lastSess  string
	lastAgent string
	lastCwd   string
	forwards  []portsproto.ForwardRequest
	unforward []portsproto.UnforwardRequest
	server    *httptest.Server
}

const fakeToken = "agent-token"

func newFakeBridge(t *testing.T) *fakeBridge {
	t.Helper()
	f := &fakeBridge{}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeBridge) port(t *testing.T) int {
	t.Helper()
	_, p, err := net.SplitHostPort(strings.TrimPrefix(f.server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(p)
	return n
}

func (f *fakeBridge) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+fakeToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if f.offline {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(portsproto.ErrorBody{Code: portsproto.CodeCompanionOff,
			Message: "the Companion for this server is not running; ask the user to run `agentport companion start m2` on their machine"})
		return
	}
	f.lastSess = r.Header.Get(portsproto.SessionHeader)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
		var req portsproto.RegisterSessionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.sessions++
		f.lastAgent, f.lastCwd = req.Agent, req.Cwd
		_ = json.NewEncoder(w).Encode(portsproto.RegisterSessionResponse{ID: "s-1", HeartbeatSeconds: 20})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/heartbeat"):
		_, _ = w.Write([]byte(`{"ok":true}`))
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/sessions/"):
		f.deleted = append(f.deleted, strings.TrimPrefix(r.URL.Path, "/v1/sessions/"))
		_, _ = w.Write([]byte(`{"ok":true}`))
	case r.Method == http.MethodPost && r.URL.Path == "/v1/forwards":
		var req portsproto.ForwardRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.forwards = append(f.forwards, req)
		results := []portsproto.PortResult{}
		for _, p := range req.Ports {
			state := portsproto.StateActive
			if f.pending {
				state = portsproto.StatePendingApproval
			}
			results = append(results, portsproto.PortResult{RemotePort: p.RemotePort, State: state,
				LocalPort: p.RemotePort, URL: portsproto.URL(p.RemotePort, p.Path), RequestID: "r-1"})
		}
		_ = json.NewEncoder(w).Encode(portsproto.ForwardResponse{Results: results})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/forwards":
		_ = json.NewEncoder(w).Encode(portsproto.ForwardsResponse{Forwards: []portsproto.ForwardInfo{
			{RemotePort: 3000, LocalPort: 3000, URL: "http://localhost:3000", Origin: "agent", State: "active", Mine: true, Owners: []string{"claude-code"}},
		}})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/forwards/unforward":
		var req portsproto.UnforwardRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.unforward = append(f.unforward, req)
		_ = json.NewEncoder(w).Encode(portsproto.UnforwardResponse{Closed: []int{3000}, Kept: []portsproto.KeptPort{}, NotForwarded: []int{}})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeBridge) setOffline(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offline = v
}

func portsSession(t *testing.T, bridge *fakeBridge, scan func(context.Context, portscan.Options) ([]portscan.Listener, error)) (*mcp.ClientSession, *BridgeClient) {
	t.Helper()
	client, err := NewBridgeClient(bridge.port(t), fakeToken, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if scan == nil {
		scan = func(context.Context, portscan.Options) ([]portscan.Listener, error) {
			return []portscan.Listener{
				{Port: 3000, Bind: "127.0.0.1", PID: 42, Process: "node", Cmdline: "node server.js", User: "rafael"},
				{Port: 5432, Bind: "127.0.0.1", PID: 7, Process: "postgres", User: "postgres"},
			}, nil
		}
	}
	tools := &PortTools{Bridge: client, ExcludePorts: []int{39123}, Scan: scan}
	server := New(fakeProvider{}, tools)
	mc := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mc.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close(); client.Close() })
	return session, client
}

func callText(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

func TestPortToolsAreRegisteredWithAgentFacingDescriptions(t *testing.T) {
	bridge := newFakeBridge(t)
	session, _ := portsSession(t, bridge, nil)
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	descriptions := map[string]string{}
	for _, tool := range list.Tools {
		descriptions[tool.Name] = tool.Description
	}
	for _, name := range []string{"agentport_status", "ports_list", "ports_forward", "ports_unforward", "forwards_list"} {
		if descriptions[name] == "" {
			t.Errorf("tool %s is missing", name)
		}
	}
	forward := descriptions["ports_forward"]
	// The agent learns when to call the tool, why, and what it may not do from these words.
	for _, must := range []string{"right after you start", "listening", "reason", "ports_unforward", "never choose local ports", "cannot stop processes"} {
		if !strings.Contains(forward, must) {
			t.Errorf("ports_forward description does not contain %q: %q", must, forward)
		}
	}
}

func TestPortsListMergesForwardedAndHidesSshd(t *testing.T) {
	bridge := newFakeBridge(t)
	var seen portscan.Options
	session, _ := portsSession(t, bridge, func(_ context.Context, opt portscan.Options) ([]portscan.Listener, error) {
		seen = opt
		return []portscan.Listener{{Port: 3000, Bind: "127.0.0.1", PID: 42, Process: "node"}}, nil
	})
	text, isErr := callText(t, session, "ports_list", map[string]any{})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	if !containsInt(seen.ExcludePorts, 22) || !containsInt(seen.ExcludePorts, 39123) {
		t.Fatalf("exclude list = %v, want sshd and the bridge port", seen.ExcludePorts)
	}
	for _, want := range []string{`"port":3000`, `"forwarded":{"local_port":3000,"url":"http://localhost:3000","mine":true}`} {
		if !strings.Contains(text, want) {
			t.Errorf("ports_list %s does not contain %s", text, want)
		}
	}
}

func TestPortsListStillWorksWhenCompanionIsOffline(t *testing.T) {
	bridge := newFakeBridge(t)
	bridge.setOffline(true)
	session, _ := portsSession(t, bridge, nil)
	text, isErr := callText(t, session, "ports_list", map[string]any{})
	if isErr {
		t.Fatalf("ports_list failed without the Companion: %s", text)
	}
	if !strings.Contains(text, "forwarded_status") || !strings.Contains(text, "companion_offline") {
		t.Fatalf("offline status not reported: %s", text)
	}
}

func TestPortsForwardSendsOneBatchAndReturnsStates(t *testing.T) {
	bridge := newFakeBridge(t)
	session, _ := portsSession(t, bridge, nil)
	text, isErr := callText(t, session, "ports_forward", map[string]any{
		"reason": "web dev server for review",
		"group":  "frontend",
		"ports":  []map[string]any{{"remote_port": 3000, "label": "web", "path": "/app"}, {"remote_port": 5173}},
	})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	if len(bridge.forwards) != 1 || len(bridge.forwards[0].Ports) != 2 {
		t.Fatalf("bridge got %+v, want one batch of 2 ports", bridge.forwards)
	}
	if bridge.forwards[0].Ports[0].Path != "/app" || bridge.forwards[0].Group != "frontend" {
		t.Fatalf("batch lost fields: %+v", bridge.forwards[0])
	}
	if !strings.Contains(text, `"state":"active"`) || !strings.Contains(text, `"url":"http://localhost:3000/app"`) {
		t.Fatalf("result = %s", text)
	}
	if bridge.lastAgent != "claude-code" {
		t.Errorf("session registered as %q", bridge.lastAgent)
	}
}

func TestPortsForwardRejectsBadInputBeforeAnyNetworkCall(t *testing.T) {
	bridge := newFakeBridge(t)
	session, _ := portsSession(t, bridge, nil)
	cases := []map[string]any{
		{"reason": "", "ports": []map[string]any{{"remote_port": 3000}}},
		{"reason": "x", "ports": []map[string]any{{"remote_port": 70000}}},
		{"reason": "x", "ports": []map[string]any{{"remote_port": 3000, "path": "app"}}},
	}
	for _, args := range cases {
		text, isErr := callText(t, session, "ports_forward", args)
		if !isErr {
			t.Errorf("args %v accepted: %s", args, text)
		}
	}
	if len(bridge.forwards) != 0 {
		t.Fatal("an invalid batch reached the Companion")
	}
}

func TestPortsForwardPendingSaysToCheckForwardsLater(t *testing.T) {
	bridge := newFakeBridge(t)
	bridge.pending = true
	session, _ := portsSession(t, bridge, nil)
	text, isErr := callText(t, session, "ports_forward", map[string]any{
		"reason": "api", "ports": []map[string]any{{"remote_port": 8080}},
	})
	if isErr || !strings.Contains(text, "pending_approval") || !strings.Contains(text, "forwards_list") {
		t.Fatalf("pending result = %s (error %v)", text, isErr)
	}
}

func TestOfflineCompanionIsCompanionOfflineWithTheInstruction(t *testing.T) {
	bridge := newFakeBridge(t)
	bridge.setOffline(true)
	session, _ := portsSession(t, bridge, nil)
	text, isErr := callText(t, session, "ports_forward", map[string]any{
		"reason": "api", "ports": []map[string]any{{"remote_port": 8080}},
	})
	if !isErr || !strings.HasPrefix(text, "companion_offline") || !strings.Contains(text, "agentport companion start m2") {
		t.Fatalf("offline result = %q (error %v)", text, isErr)
	}
}

func TestUnreachableBridgeIsCompanionOffline(t *testing.T) {
	bridge := newFakeBridge(t)
	port := bridge.port(t)
	bridge.server.Close()
	client, err := NewBridgeClient(port, fakeToken, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Forwards(context.Background())
	if !CompanionOffline(err) {
		t.Fatalf("err = %v, want companion offline", err)
	}
}

func TestPortsUnforwardSendsAllOrPorts(t *testing.T) {
	bridge := newFakeBridge(t)
	session, _ := portsSession(t, bridge, nil)
	if text, isErr := callText(t, session, "ports_unforward", map[string]any{"ports": []int{3000}}); isErr {
		t.Fatalf("error: %s", text)
	}
	if text, isErr := callText(t, session, "ports_unforward", map[string]any{"all": true}); isErr {
		t.Fatalf("error: %s", text)
	}
	if len(bridge.unforward) != 2 || bridge.unforward[0].Ports[0] != 3000 || !bridge.unforward[1].All {
		t.Fatalf("unforward requests = %+v", bridge.unforward)
	}
	if text, isErr := callText(t, session, "ports_unforward", map[string]any{}); !isErr {
		t.Fatalf("empty unforward accepted: %s", text)
	}
}

func TestForwardsListShowsOwners(t *testing.T) {
	bridge := newFakeBridge(t)
	session, _ := portsSession(t, bridge, nil)
	text, isErr := callText(t, session, "forwards_list", map[string]any{})
	if isErr || !strings.Contains(text, `"mine":true`) || !strings.Contains(text, `"owners":["claude-code"]`) {
		t.Fatalf("forwards_list = %s (error %v)", text, isErr)
	}
}

func TestCloseUnregistersTheSession(t *testing.T) {
	bridge := newFakeBridge(t)
	_, client := portsSession(t, bridge, nil)
	session, err := client.ensureSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	client.Close() // a second close must not send a second unregister
	if len(bridge.deleted) != 1 || bridge.deleted[0] != session {
		t.Fatalf("deleted sessions = %v, want [%s]", bridge.deleted, session)
	}
}

func TestSessionHeaderCarriesTheRegisteredID(t *testing.T) {
	bridge := newFakeBridge(t)
	session, _ := portsSession(t, bridge, nil)
	if _, isErr := callText(t, session, "forwards_list", map[string]any{}); isErr {
		t.Fatal("forwards_list failed")
	}
	if bridge.lastSess != "s-1" {
		t.Fatalf("session header = %q, want s-1", bridge.lastSess)
	}
}

func TestAgentNameFromEnvironmentFallsBack(t *testing.T) {
	t.Setenv("AGENTPORT_AGENT", "  codex ")
	if got := AgentFromEnv(); got != "codex" {
		t.Fatalf("AgentFromEnv = %q", got)
	}
	t.Setenv("AGENTPORT_AGENT", "")
	if got := AgentFromEnv(); got != "agent" {
		t.Fatalf("empty env AgentFromEnv = %q", got)
	}
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
