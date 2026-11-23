package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rafaelb13/agentport/internal/portsproto"
)

// ErrCompanionUnreachable means the reverse tunnel to the Companion is down or
// the bridge does not answer. Tools report it as companion_offline.
var ErrCompanionUnreachable = errors.New("companion unreachable")

// APIError is an error answer from the bridge or from the Companion behind it.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("bridge %s (HTTP %d)", e.Code, e.Status)
	}
	return fmt.Sprintf("bridge %s: %s", e.Code, e.Message)
}

// CompanionOffline reports whether err means the Companion cannot be reached.
func CompanionOffline(err error) bool {
	if errors.Is(err, ErrCompanionUnreachable) {
		return true
	}
	var api *APIError
	return errors.As(err, &api) && api.Code == portsproto.CodeCompanionOff
}

// requestTimeout bounds one call. A forward waits up to 120 s for the user, and
// the bridge allows a little more than that.
const requestTimeout = 130 * time.Second

// BridgeClient speaks the port protocol through the bridge on 127.0.0.1. It
// registers one agent session lazily, keeps it alive with heartbeats, and
// unregisters it on Close.
type BridgeClient struct {
	baseURL string
	token   string
	agent   string
	client  *http.Client

	mu        sync.Mutex
	session   string
	heartbeat time.Duration
	stop      chan struct{}
	stopOnce  sync.Once
}

// NewBridgeClient builds a client for the bridge port and session token from the
// environment. The port is the only endpoint input, as for HTTPProvider.
func NewBridgeClient(port int, token, agent string) (*BridgeClient, error) {
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("bridge port must be between 1 and 65535")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("bridge session token is required")
	}
	if strings.TrimSpace(agent) == "" {
		agent = "agent"
	}
	return &BridgeClient{
		baseURL:   "http://127.0.0.1:" + strconv.Itoa(port),
		token:     token,
		agent:     agent,
		client:    &http.Client{Timeout: requestTimeout},
		heartbeat: 20 * time.Second,
		stop:      make(chan struct{}),
	}, nil
}

// AgentFromEnv names the calling agent. The harness sets AGENTPORT_AGENT; the
// default is generic so the panel still shows something useful.
func AgentFromEnv() string {
	if agent := strings.TrimSpace(os.Getenv("AGENTPORT_AGENT")); agent != "" {
		return agent
	}
	return "agent"
}

// Session returns the registered session ID, or "" before the first call.
func (c *BridgeClient) Session() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

// ensureSession registers the session if it is not registered yet.
func (c *BridgeClient) ensureSession(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != "" {
		return c.session, nil
	}
	cwd, _ := os.Getwd()
	var resp portsproto.RegisterSessionResponse
	err := c.call(ctx, http.MethodPost, "/v1/sessions", "", portsproto.RegisterSessionRequest{
		V: portsproto.Version, Agent: c.agent, PID: os.Getpid(), Cwd: cwd,
	}, &resp)
	if err != nil {
		return "", err
	}
	c.session = resp.ID
	if resp.HeartbeatSeconds > 0 {
		c.heartbeat = time.Duration(resp.HeartbeatSeconds) * time.Second
	}
	go c.beat(resp.ID, c.heartbeat)
	return c.session, nil
}

// beat keeps the session alive. If the Companion forgets the session (restart,
// or silence longer than its limit), the next call registers it again.
func (c *BridgeClient) beat(id string, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			err := c.call(context.Background(), http.MethodPost, "/v1/sessions/"+id+"/heartbeat", "", nil, nil)
			var api *APIError
			if errors.As(err, &api) && api.Code == portsproto.CodeUnknownSession {
				c.mu.Lock()
				if c.session == id {
					c.session = ""
				}
				c.mu.Unlock()
				return
			}
		}
	}
}

// Forward asks for a batch of ports. It waits for the user's decision up to ctx.
func (c *BridgeClient) Forward(ctx context.Context, req portsproto.ForwardRequest) (portsproto.ForwardResponse, error) {
	var out portsproto.ForwardResponse
	session, err := c.ensureSession(ctx)
	if err != nil {
		return out, err
	}
	err = c.call(ctx, http.MethodPost, "/v1/forwards", session, req, &out)
	return out, err
}

// Unforward removes this session from the owners of ports.
func (c *BridgeClient) Unforward(ctx context.Context, req portsproto.UnforwardRequest) (portsproto.UnforwardResponse, error) {
	var out portsproto.UnforwardResponse
	session, err := c.ensureSession(ctx)
	if err != nil {
		return out, err
	}
	err = c.call(ctx, http.MethodPost, "/v1/forwards/unforward", session, req, &out)
	return out, err
}

// Forwards lists the forwards of the profile, marking this session's own.
func (c *BridgeClient) Forwards(ctx context.Context) ([]portsproto.ForwardInfo, error) {
	var out portsproto.ForwardsResponse
	session, err := c.ensureSession(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.call(ctx, http.MethodGet, "/v1/forwards", session, nil, &out); err != nil {
		return nil, err
	}
	return out.Forwards, nil
}

// Close stops the heartbeat and unregisters the session, as when the agent's
// stdio closes. It is safe to call more than once.
func (c *BridgeClient) Close() {
	c.stopOnce.Do(func() { close(c.stop) })
	c.mu.Lock()
	session := c.session
	c.session = ""
	c.mu.Unlock()
	if session != "" {
		_ = c.call(context.Background(), http.MethodDelete, "/v1/sessions/"+session, "", nil, nil)
	}
}

// call performs one request. session, when set, goes in the session header.
func (c *BridgeClient) call(ctx context.Context, method, path, session string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if session != "" {
		req.Header.Set(portsproto.SessionHeader, session)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCompanionUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var eb portsproto.ErrorBody
		_ = json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&eb)
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 16*1024))
		if eb.Code == "" {
			eb.Code = "HTTP_" + strconv.Itoa(resp.StatusCode)
		}
		return &APIError{Status: resp.StatusCode, Code: eb.Code, Message: eb.Message}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 16*1024))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(out); err != nil {
		return fmt.Errorf("decode bridge answer: %w", err)
	}
	return nil
}
