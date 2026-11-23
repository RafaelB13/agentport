package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rafaelb13/agentport/internal/portscan"
	"github.com/rafaelb13/agentport/internal/portsproto"
)

// PortTools are the port tools of the MCP server. Bridge reaches the Companion;
// ExcludePorts are server ports that are never listed (the tunnel and sshd).
type PortTools struct {
	Bridge       *BridgeClient
	ExcludePorts []int
	// Scan lists the listening ports. Nil uses portscan.Scan; tests inject a fake.
	Scan func(context.Context, portscan.Options) ([]portscan.Listener, error)
}

// sshdPort is never offered to the agent.
const sshdPort = 22

// maxCmdline bounds the command line shown per port.
const maxCmdline = 160

type listInput struct {
	IncludeSystem bool `json:"include_system,omitempty" jsonschema:"also list system services such as resolvers and print daemons; default false"`
	OnlyUser      bool `json:"only_user,omitempty" jsonschema:"list only ports owned by a non-root user; default false"`
}

type portInput struct {
	RemotePort int    `json:"remote_port" jsonschema:"the TCP port the server listens on"`
	Label      string `json:"label,omitempty" jsonschema:"short name for the port, for example web or api"`
	Path       string `json:"path,omitempty" jsonschema:"path to open in the browser; must start with /"`
}

type forwardInput struct {
	Reason string      `json:"reason" jsonschema:"why these ports need to be reachable, in plain words; the user reads it before approving"`
	Group  string      `json:"group,omitempty" jsonschema:"optional name that groups related ports in the panel"`
	Ports  []portInput `json:"ports" jsonschema:"the ports to forward, at most 64 per call"`
}

type unforwardInput struct {
	Ports []int `json:"ports,omitempty" jsonschema:"ports this session should stop using"`
	All   bool  `json:"all,omitempty" jsonschema:"stop using every port this session uses"`
}

type noInput struct{}

// registerPortTools adds the four port tools to s.
func registerPortTools(s *mcp.Server, t *PortTools) {
	if t.Scan == nil {
		t.Scan = portscan.Scan
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "ports_list",
		Description: "Lists the TCP ports this server is listening on, with the owning process, a short command line, the user, " +
			"the container if any, and whether the port is already forwarded to the user's machine. Use it to find the port of a " +
			"server you just started, or to see what is running. System services are hidden unless include_system is true.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, any, error) {
		return t.list(ctx, in)
	})
	mcp.AddTool(s, &mcp.Tool{
		Name: "ports_forward",
		Description: "Makes ports of this server reachable from the user's machine. Call it right after you start a server and confirm " +
			"it is listening (ports_list shows it). Give a reason in plain words: the user reads it in the AgentPort panel and " +
			"approves or refuses the whole batch there. The call waits up to 2 minutes for that decision. Each result has a state: " +
			"active (with local_port and url, which already include the path; tell the user to open that url), pending_approval " +
			"(the user has not decided yet; check forwards_list later), denied or skipped. You never choose local ports, and you " +
			"cannot stop processes on the user's machine. When the server stops, call ports_unforward for its ports.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in forwardInput) (*mcp.CallToolResult, any, error) {
		return t.forward(ctx, in)
	})
	mcp.AddTool(s, &mcp.Tool{
		Name: "ports_unforward",
		Description: "Stops this session's use of forwarded ports, for example when a server you started has stopped. Pass ports, or " +
			"all=true for every port this session uses. A port that another agent session or the user still uses stays open, and " +
			"the result says which ports stay open and who holds them. It never closes another session's port.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in unforwardInput) (*mcp.CallToolResult, any, error) {
		return t.unforward(ctx, in)
	})
	mcp.AddTool(s, &mcp.Tool{
		Name: "forwards_list",
		Description: "Lists the ports currently forwarded to the user's machine, with the local URL, label, origin (agent or manual), " +
			"the owners (this session, other sessions by agent name, or the user), whether this session owns them, and uptime. " +
			"Use it to check whether a pending_approval request was decided.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
		return t.listForwards(ctx)
	})
}

// ListEntry is one port in ports_list.
type ListEntry struct {
	Port      int           `json:"port"`
	Bind      string        `json:"bind"`
	PID       int           `json:"pid,omitempty"`
	Process   string        `json:"process,omitempty"`
	Cmdline   string        `json:"cmdline,omitempty"`
	User      string        `json:"user,omitempty"`
	Container string        `json:"container,omitempty"`
	Forwarded *ForwardedRef `json:"forwarded,omitempty"`
}

// ForwardedRef says a port is already forwarded, and where.
type ForwardedRef struct {
	LocalPort int    `json:"local_port"`
	URL       string `json:"url"`
	Mine      bool   `json:"mine"`
}

func (t *PortTools) list(ctx context.Context, in listInput) (*mcp.CallToolResult, any, error) {
	exclude := append([]int{sshdPort}, t.ExcludePorts...)
	listeners, err := t.Scan(ctx, portscan.Options{IncludeSystem: in.IncludeSystem, ExcludePorts: exclude})
	if err != nil {
		return toolError(fmt.Errorf("list listening ports: %w", err))
	}
	forwards, fwdErr := t.Bridge.Forwards(ctx)
	byRemote := map[int]portsproto.ForwardInfo{}
	for _, f := range forwards {
		byRemote[f.RemotePort] = f
	}
	entries := []ListEntry{}
	for _, l := range listeners {
		if in.OnlyUser && l.User == "root" {
			continue
		}
		entry := ListEntry{Port: l.Port, Bind: l.Bind, PID: l.PID, Process: l.Process,
			Cmdline: shortCmdline(l.Cmdline), User: l.User, Container: l.Container}
		if f, ok := byRemote[l.Port]; ok {
			entry.Forwarded = &ForwardedRef{LocalPort: f.LocalPort, URL: f.URL, Mine: f.Mine}
		}
		entries = append(entries, entry)
	}
	out := map[string]any{"ports": entries}
	if fwdErr != nil {
		out["forwarded_status"] = offlineText(fwdErr)
	}
	return jsonResult(out)
}

func (t *PortTools) forward(ctx context.Context, in forwardInput) (*mcp.CallToolResult, any, error) {
	req := portsproto.ForwardRequest{V: portsproto.Version, Reason: in.Reason, Group: in.Group}
	for _, p := range in.Ports {
		req.Ports = append(req.Ports, portsproto.PortRequest{RemotePort: p.RemotePort, Label: p.Label, Path: p.Path})
	}
	if err := req.Validate(); err != nil {
		return toolError(fmt.Errorf("invalid request: %w", err))
	}
	resp, err := t.Bridge.Forward(ctx, req)
	if err != nil {
		return toolFailure(err)
	}
	out := map[string]any{"results": resp.Results}
	for _, r := range resp.Results {
		if r.State == portsproto.StatePendingApproval {
			out["next"] = "The user has not decided yet. Call forwards_list later to see whether the request was approved."
			break
		}
	}
	return jsonResult(out)
}

func (t *PortTools) unforward(ctx context.Context, in unforwardInput) (*mcp.CallToolResult, any, error) {
	req := portsproto.UnforwardRequest{V: portsproto.Version, Ports: in.Ports, All: in.All}
	if err := req.Validate(); err != nil {
		return toolError(fmt.Errorf("invalid request: %w", err))
	}
	resp, err := t.Bridge.Unforward(ctx, req)
	if err != nil {
		return toolFailure(err)
	}
	return jsonResult(resp)
}

func (t *PortTools) listForwards(ctx context.Context) (*mcp.CallToolResult, any, error) {
	forwards, err := t.Bridge.Forwards(ctx)
	if err != nil {
		return toolFailure(err)
	}
	if forwards == nil {
		forwards = []portsproto.ForwardInfo{}
	}
	return jsonResult(map[string]any{"forwards": forwards})
}

// toolFailure turns a bridge error into a tool error. An offline Companion gets
// the code agents are told to pass on, with the instruction for the user.
func toolFailure(err error) (*mcp.CallToolResult, any, error) {
	if CompanionOffline(err) {
		return toolError(errors.New(offlineText(err)))
	}
	return toolError(err)
}

// offlineText is the companion_offline message. Its instruction names the profile
// when the bridge knows it.
func offlineText(err error) string {
	var api *APIError
	if errors.As(err, &api) && api.Code == portsproto.CodeCompanionOff && api.Message != "" {
		return "companion_offline: " + api.Message
	}
	return "companion_offline: the Companion for this server is not reachable (the reverse tunnel is down). " +
		"Ask the user to run `agentport companion start <profile>` on their machine."
}

func shortCmdline(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if len(cmd) <= maxCmdline {
		return cmd
	}
	return cmd[:maxCmdline-3] + "..."
}

func jsonResult(value any) (*mcp.CallToolResult, any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return toolError(err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
}
