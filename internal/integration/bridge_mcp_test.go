package integration

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rafaelb13/agentport/internal/daemon"
	"github.com/rafaelb13/agentport/internal/mcpserver"
	"github.com/rafaelb13/agentport/internal/release"
)

// TestStatusToolReachesTheBridgeThroughItsSession runs the real daemon, the
// real HTTP provider and the MCP server together, as a Companion and the
// remote harness would: the agent's call must come back with the bridge's
// answer, and an unregistered token must be refused.
func TestStatusToolReachesTheBridgeThroughItsSession(t *testing.T) {
	ctx := context.Background()
	d, err := daemon.Start("control-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Bridge.SetReleaseStatus(release.Status{CurrentVersion: "v1.0.0"})
	if err := d.Bridge.RegisterPersistentSession("companion:work", "session-token"); err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(d.State.Address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	call := func(token string) (string, bool) {
		t.Helper()
		provider, err := mcpserver.NewHTTPProvider(port, token)
		if err != nil {
			t.Fatal(err)
		}
		server := mcpserver.New(provider)
		client := mcp.NewClient(&mcp.Implementation{Name: "harness", Version: "0.1.0"}, nil)
		serverTransport, clientTransport := mcp.NewInMemoryTransports()
		if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
			t.Fatal(err)
		}
		session, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "agentport_status", Arguments: map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		return result.Content[0].(*mcp.TextContent).Text, result.IsError
	}

	text, isError := call("session-token")
	if isError || !strings.Contains(text, `"tunnel":"connected"`) || !strings.Contains(text, `"bridge":"ok"`) {
		t.Fatalf("status through the session = %q (error %v)", text, isError)
	}
	if text, isError := call("forged-token"); !isError || strings.Contains(text, "connected") {
		t.Fatalf("forged token was accepted: %q", text)
	}
}
