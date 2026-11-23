// Package mcpserver exposes the agentport tools to the coding agent over MCP.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rafaelb13/agentport/internal/buildinfo"
	"github.com/rafaelb13/agentport/internal/release"
)

// StatusProvider is the bridge-facing interface used by the MCP server.
type StatusProvider interface {
	Status(context.Context) (Status, error)
}

// Status is what agentport_status returns: whether the reverse tunnel to the
// local Companion is up, the bridge's answer, and the release state.
type Status struct {
	Bridge  string         `json:"bridge,omitempty"`
	Tunnel  string         `json:"tunnel"`
	Release release.Status `json:"release"`
}

var ErrNoProvider = errors.New("status provider is nil")

// New constructs the MCP server. Its only tool today is agentport_status.
//
// TODO(ports): the port tools (ports_list, ports_forward, ports_unforward and
// forwards_list) are registered here in a later phase, on top of the same
// bridge provider.
func New(provider StatusProvider) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "agentport", Version: buildinfo.Version}, nil)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "agentport_status",
		Description: "Reports whether the agentport tunnel to the local Companion is connected, and whether a newer agentport release is available.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		if provider == nil {
			return toolError(ErrNoProvider)
		}
		status, err := provider.Status(ctx)
		if err != nil {
			return toolError(err)
		}
		data, _ := json.Marshal(status)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
	})
	return s
}

func toolError(err error) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{
		&mcp.TextContent{Text: err.Error()},
	}}, nil, nil
}
