// Package portsproto defines the JSON protocol spoken between the remote
// `agentport mcp` process and the Companion's port manager, through the bridge.
// It is dependency-free on purpose: the remote binary imports it and must stay
// small.
package portsproto

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Version is the protocol version every request must carry in its "v" field.
const Version = 1

// Limits enforced on every request.
const (
	MaxPortsPerBatch = 64
	MaxReasonLength  = 500
	MaxLabelLength   = 64
	MaxPathLength    = 256
	MaxGroupLength   = 64
)

// Port states reported per requested port.
const (
	StateActive          = "active"
	StatePendingApproval = "pending_approval"
	StateDenied          = "denied"
	StateSkipped         = "skipped"
	StateError           = "error"
)

// Error codes returned in the bridge's error body.
const (
	CodeInvalidRequest  = "INVALID_REQUEST"
	CodeCompanionOff    = "COMPANION_OFFLINE"
	CodeUnknownSession  = "SESSION_UNKNOWN"
	CodeLimitReached    = "FORWARD_LIMIT"
	CodeInternal        = "INTERNAL"
	CodeMethodNotAllow  = "METHOD_NOT_ALLOWED"
	CodeNotFound        = "NOT_FOUND"
	CodeConflict        = "CONFLICT"
	CodeBlockedPort     = "PORT_BLOCKED"
	CodeNoFreeLocalPort = "NO_FREE_LOCAL_PORT"
)

// ErrorBody is the JSON body of every rejected request.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RegisterSessionRequest registers one agent session (one `agentport mcp`).
type RegisterSessionRequest struct {
	V     int    `json:"v"`
	Agent string `json:"agent"`
	PID   int    `json:"pid"`
	Cwd   string `json:"cwd"`
}

// RegisterSessionResponse carries the session ID and the heartbeat cadence.
type RegisterSessionResponse struct {
	ID               string `json:"id"`
	HeartbeatSeconds int    `json:"heartbeat_seconds"`
	// Resumed is true when a session of the same agent and folder came back
	// within the grace period and took over its still-open forwards.
	Resumed bool `json:"resumed"`
}

// PortRequest asks for one remote port.
type PortRequest struct {
	RemotePort int    `json:"remote_port"`
	Label      string `json:"label,omitempty"`
	Path       string `json:"path,omitempty"`
}

// ForwardRequest is one batch: a single approval request in the panel.
type ForwardRequest struct {
	V      int           `json:"v"`
	Reason string        `json:"reason"`
	Group  string        `json:"group,omitempty"`
	Ports  []PortRequest `json:"ports"`
}

// PortResult is the outcome for one requested port.
type PortResult struct {
	RemotePort int    `json:"remote_port"`
	State      string `json:"state"`
	LocalPort  int    `json:"local_port,omitempty"`
	URL        string `json:"url,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	Message    string `json:"message,omitempty"`
}

// ForwardResponse is returned by a forward request, after a decision or a timeout.
type ForwardResponse struct {
	Results []PortResult `json:"results"`
}

// UnforwardRequest removes this session from the owners of ports, or all of them.
type UnforwardRequest struct {
	V     int   `json:"v"`
	Ports []int `json:"ports,omitempty"`
	All   bool  `json:"all,omitempty"`
}

// KeptPort is a port this session stopped owning but that stays open.
type KeptPort struct {
	RemotePort int      `json:"remote_port"`
	Owners     []string `json:"owners"`
}

// UnforwardResponse says which ports closed and which stay open.
type UnforwardResponse struct {
	Closed       []int      `json:"closed"`
	Kept         []KeptPort `json:"kept"`
	NotForwarded []int      `json:"not_forwarded"`
}

// ForwardInfo describes one active (or session-ended) forward.
type ForwardInfo struct {
	RemotePort    int       `json:"remote_port"`
	LocalPort     int       `json:"local_port"`
	URL           string    `json:"url"`
	Label         string    `json:"label,omitempty"`
	Path          string    `json:"path,omitempty"`
	Group         string    `json:"group,omitempty"`
	Origin        string    `json:"origin"`
	State         string    `json:"state"`
	Owners        []string  `json:"owners"`
	Mine          bool      `json:"mine"`
	Pinned        bool      `json:"pinned"`
	CreatedAt     time.Time `json:"created_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
}

// ForwardsResponse lists the forwards of the Companion's profile.
type ForwardsResponse struct {
	Forwards []ForwardInfo `json:"forwards"`
}

// ValidPort reports whether p is a TCP port number usable for forwarding.
func ValidPort(p int) bool { return p >= 1 && p <= 65535 }

// Validate checks a register request.
func (r RegisterSessionRequest) Validate() error {
	if r.V != Version {
		return fmt.Errorf("unsupported protocol version %d", r.V)
	}
	if strings.TrimSpace(r.Agent) == "" || len(r.Agent) > MaxLabelLength {
		return errors.New("agent is required and must be at most 64 characters")
	}
	if r.PID < 0 {
		return errors.New("pid must not be negative")
	}
	if len(r.Cwd) > 1024 {
		return errors.New("cwd is too long")
	}
	return nil
}

// Validate checks a forward batch. It rejects the whole batch on the first bad field.
func (r ForwardRequest) Validate() error {
	if r.V != Version {
		return fmt.Errorf("unsupported protocol version %d", r.V)
	}
	if strings.TrimSpace(r.Reason) == "" {
		return errors.New("reason is required")
	}
	if len(r.Reason) > MaxReasonLength {
		return fmt.Errorf("reason must be at most %d characters", MaxReasonLength)
	}
	if len(r.Group) > MaxGroupLength {
		return fmt.Errorf("group must be at most %d characters", MaxGroupLength)
	}
	if len(r.Ports) == 0 {
		return errors.New("at least one port is required")
	}
	if len(r.Ports) > MaxPortsPerBatch {
		return fmt.Errorf("at most %d ports per batch", MaxPortsPerBatch)
	}
	seen := map[int]bool{}
	for _, p := range r.Ports {
		if !ValidPort(p.RemotePort) {
			return fmt.Errorf("remote_port %d must be between 1 and 65535", p.RemotePort)
		}
		if seen[p.RemotePort] {
			return fmt.Errorf("remote_port %d is listed twice", p.RemotePort)
		}
		seen[p.RemotePort] = true
		if len(p.Label) > MaxLabelLength {
			return fmt.Errorf("label for port %d must be at most %d characters", p.RemotePort, MaxLabelLength)
		}
		if p.Path != "" && (!strings.HasPrefix(p.Path, "/") || len(p.Path) > MaxPathLength) {
			return fmt.Errorf("path for port %d must start with / and be at most %d characters", p.RemotePort, MaxPathLength)
		}
	}
	return nil
}

// Validate checks an unforward request: either all, or a non-empty list of ports.
func (r UnforwardRequest) Validate() error {
	if r.V != Version {
		return fmt.Errorf("unsupported protocol version %d", r.V)
	}
	if r.All && len(r.Ports) > 0 {
		return errors.New("use either all or ports, not both")
	}
	if !r.All && len(r.Ports) == 0 {
		return errors.New("ports or all is required")
	}
	if len(r.Ports) > MaxPortsPerBatch {
		return fmt.Errorf("at most %d ports per request", MaxPortsPerBatch)
	}
	for _, p := range r.Ports {
		if !ValidPort(p) {
			return fmt.Errorf("port %d must be between 1 and 65535", p)
		}
	}
	return nil
}

// URL builds the local URL for a forward, with the path the agent asked for.
func URL(localPort int, path string) string {
	return fmt.Sprintf("http://localhost:%d%s", localPort, path)
}
