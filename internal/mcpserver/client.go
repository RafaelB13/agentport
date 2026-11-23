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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rafaelb13/agentport/internal/portsproto"
)

// ErrCompanionUnreachable means the reverse tunnel to the Companion is down or
// the bridge does not answer. Tools report it as companion_offline.
var ErrCompanionUnreachable = errors.New("companion unreachable")

// ErrRequestLost means the Companion stopped or restarted while a forward request
// was waiting for the user's decision. The request is not kept: the agent must
// call ports_forward again. Ports this session already uses come back on their own.
var ErrRequestLost = errors.New("the Companion stopped or restarted while the request was waiting for approval")

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

// unknownSession reports whether the Companion no longer knows the session, as
// after a restart or a long silence.
func unknownSession(err error) bool {
	var api *APIError
	return errors.As(err, &api) && api.Code == portsproto.CodeUnknownSession
}

// requestTimeout bounds one call. A forward waits up to 120 s for the user, and
// the bridge allows a little more than that.
const requestTimeout = 130 * time.Second

// registerTimeout bounds one registration attempt made in the background.
const registerTimeout = 10 * time.Second

// Reconnect backoff after the Companion forgot the session or is unreachable.
const (
	reconnectFirstDelay = time.Second
	reconnectMaxDelay   = 10 * time.Second
)

// BridgeClient speaks the port protocol through the bridge on 127.0.0.1. It
// registers one agent session lazily and keeps it alive with heartbeats.
//
// When the Companion restarts, or the tunnel drops, the session is lost. The
// client then registers again in the background, with exponential backoff, and
// reclaims the remote ports this session used, so they come back as active with
// this session as owner even when the agent makes no further calls. Unregisters
// the session on Close.
type BridgeClient struct {
	baseURL string
	token   string
	agent   string
	client  *http.Client

	// regMu serializes registrations, so a background reconnect and a tool call
	// never register two sessions at once.
	regMu sync.Mutex

	mu      sync.Mutex
	session string
	// dropped is the last session ID this client gave up. The next registration
	// names it, so the Companion ends it instead of keeping two of this client.
	dropped   string
	owned     map[int]bool // remote ports this session uses; sent as reclaim
	heartbeat time.Duration
	started   bool
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
		owned:     map[int]bool{},
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

// Session returns the registered session ID, or "" before the first call or
// while the session is being registered again.
func (c *BridgeClient) Session() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

// ensureSession registers the session if it is not registered yet.
func (c *BridgeClient) ensureSession(ctx context.Context) (string, error) {
	if id := c.Session(); id != "" {
		return id, nil
	}
	return c.register(ctx)
}

// register creates a session and reclaims the ports this client uses. The new
// session ID replaces the old one; the Companion answers with the ports it kept.
func (c *BridgeClient) register(ctx context.Context) (string, error) {
	c.regMu.Lock()
	defer c.regMu.Unlock()
	if id := c.Session(); id != "" {
		return id, nil
	}
	if c.closed() {
		return "", errors.New("bridge client is closed")
	}
	cwd, _ := os.Getwd()
	c.mu.Lock()
	reclaim := sortedPorts(c.owned)
	previous := c.dropped
	c.mu.Unlock()
	var resp portsproto.RegisterSessionResponse
	err := c.call(ctx, http.MethodPost, "/v1/sessions", "", portsproto.RegisterSessionRequest{
		V: portsproto.Version, Agent: c.agent, PID: os.Getpid(), Cwd: cwd, Reclaim: reclaim, Previous: previous,
	}, &resp)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.session = resp.ID
	c.dropped = ""
	c.owned = map[int]bool{}
	for _, p := range resp.Reclaimed {
		c.owned[p] = true
	}
	if resp.HeartbeatSeconds > 0 {
		c.heartbeat = time.Duration(resp.HeartbeatSeconds) * time.Second
	}
	start := !c.started
	c.started = true
	every := c.heartbeat
	c.mu.Unlock()
	if start {
		go c.keepAlive(every)
	}
	return resp.ID, nil
}

// keepAlive sends heartbeats until the client closes. A heartbeat that fails
// drops the session and registers again in the background, reclaiming its ports.
func (c *BridgeClient) keepAlive(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
		}
		id := c.Session()
		if id == "" {
			c.reconnect()
			continue
		}
		if err := c.call(context.Background(), http.MethodPost, "/v1/sessions/"+id+"/heartbeat", "", nil, nil); err != nil {
			c.dropSession(id)
			c.reconnect()
		}
	}
}

// reconnect registers again, retrying with exponential backoff, until it succeeds
// or the client closes.
func (c *BridgeClient) reconnect() {
	delay := reconnectFirstDelay
	for !c.closed() {
		ctx, cancel := context.WithTimeout(context.Background(), registerTimeout)
		_, err := c.register(ctx)
		cancel()
		if err == nil {
			return
		}
		select {
		case <-c.stop:
			return
		case <-time.After(delay):
		}
		if delay *= 2; delay > reconnectMaxDelay {
			delay = reconnectMaxDelay
		}
	}
}

// dropSession forgets session id, so the next call or heartbeat registers again.
func (c *BridgeClient) dropSession(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == id {
		c.session = ""
		c.dropped = id
	}
}

// withSession runs fn with a registered session. When the Companion does not
// know the session any more, the session registers again (reclaiming its ports)
// and fn runs once more. A Companion that is offline drops the session, so the
// next call reports it as offline instead of using a stale ID.
func (c *BridgeClient) withSession(ctx context.Context, fn func(session string) error) error {
	session, err := c.ensureSession(ctx)
	if err != nil {
		return err
	}
	err = fn(session)
	if CompanionOffline(err) {
		c.dropSession(session)
	}
	if !unknownSession(err) {
		return err
	}
	c.dropSession(session)
	session, err = c.ensureSession(ctx)
	if err != nil {
		return err
	}
	return fn(session)
}

// Forward asks for a batch of ports. It waits for the user's decision up to ctx.
func (c *BridgeClient) Forward(ctx context.Context, req portsproto.ForwardRequest) (portsproto.ForwardResponse, error) {
	var out portsproto.ForwardResponse
	err := c.withSession(ctx, func(session string) error {
		out = portsproto.ForwardResponse{}
		err := c.call(ctx, http.MethodPost, "/v1/forwards", session, req, &out)
		if CompanionOffline(err) {
			return fmt.Errorf("%w: %w", ErrRequestLost, err)
		}
		return err
	})
	if err == nil {
		for _, r := range out.Results {
			if r.State == portsproto.StateActive {
				c.setOwned(r.RemotePort, true)
			}
		}
	}
	return out, err
}

// Unforward removes this session from the owners of ports.
func (c *BridgeClient) Unforward(ctx context.Context, req portsproto.UnforwardRequest) (portsproto.UnforwardResponse, error) {
	var out portsproto.UnforwardResponse
	err := c.withSession(ctx, func(session string) error {
		out = portsproto.UnforwardResponse{}
		return c.call(ctx, http.MethodPost, "/v1/forwards/unforward", session, req, &out)
	})
	if err == nil {
		for _, p := range out.Closed {
			c.setOwned(p, false)
		}
		for _, k := range out.Kept {
			c.setOwned(k.RemotePort, false)
		}
		for _, p := range out.NotForwarded {
			c.setOwned(p, false)
		}
	}
	return out, err
}

// Forwards lists the forwards of the profile, marking this session's own.
func (c *BridgeClient) Forwards(ctx context.Context) ([]portsproto.ForwardInfo, error) {
	var out portsproto.ForwardsResponse
	err := c.withSession(ctx, func(session string) error {
		out = portsproto.ForwardsResponse{}
		return c.call(ctx, http.MethodGet, "/v1/forwards", session, nil, &out)
	})
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.owned = map[int]bool{}
	for _, f := range out.Forwards {
		if f.Mine {
			c.owned[f.RemotePort] = true
		}
	}
	c.mu.Unlock()
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

func (c *BridgeClient) closed() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

func (c *BridgeClient) setOwned(port int, owned bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if owned {
		c.owned[port] = true
	} else {
		delete(c.owned, port)
	}
}

func sortedPorts(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
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
