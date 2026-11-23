package bridge

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// upstream is a Companion's loopback control API: where the port protocol of
// one profile is served, and the control token that guards it.
type upstream struct {
	address string
	token   string
}

// proxyTimeout covers a forward request that waits for the user's decision
// (120 s) plus transfer time. The bridge's default write deadline is shorter.
const proxyTimeout = 150 * time.Second

// maxProxyBody bounds a request or response body that the bridge relays.
const maxProxyBody = 256 * 1024

// companionOffline is the code agents receive when the Companion behind their
// token is not running (the tunnel is down or the control API is unknown).
const companionOffline = "COMPANION_OFFLINE"

// SetUpstream records where the port protocol of session id is served. Only a
// loopback address is accepted: the Companion's control API never listens elsewhere.
func (b *Bridge) SetUpstream(id, address, token string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(token) == "" {
		return public("upstream session ID and token are required")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return public("upstream address must be a 127.0.0.1 address")
	}
	if value, err := strconv.Atoi(port); err != nil || value < 1 || value > 65535 {
		return public("upstream port is invalid")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.upstreams[id] = upstream{address: address, token: token}
	return nil
}

// RemoveUpstream forgets the upstream of session id, e.g. when its Companion stops.
func (b *Bridge) RemoveUpstream(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.upstreams, id)
}

// proxyPorts relays one port-protocol request to the Companion that owns the
// caller's token. The caller's path is kept under /v1/ports on the upstream.
func (b *Bridge) proxyPorts(w http.ResponseWriter, r *http.Request) {
	sessionID, code := b.resolve(r)
	if code != "" {
		b.err(w, http.StatusUnauthorized, code, "unauthorized")
		return
	}
	b.mu.Lock()
	up, ok := b.upstreams[sessionID]
	b.mu.Unlock()
	if !ok {
		b.err(w, http.StatusServiceUnavailable, companionOffline, offlineMessage(sessionID))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), proxyTimeout)
	defer cancel()
	body := http.MaxBytesReader(w, r.Body, maxProxyBody)
	target := "http://" + up.address + "/v1/ports" + strings.TrimPrefix(r.URL.Path, "/v1")
	upstreamReq, err := http.NewRequestWithContext(ctx, r.Method, target, body)
	if err != nil {
		b.err(w, http.StatusInternalServerError, "INTERNAL", "request could not be relayed")
		return
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+up.token)
	upstreamReq.Header.Set("Content-Type", "application/json")
	if session := r.Header.Get("X-Agentport-Session"); session != "" {
		upstreamReq.Header.Set("X-Agentport-Session", session)
	}
	// The response may come after the server's default write deadline (long-poll).
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(proxyTimeout + 10*time.Second))

	client := &http.Client{Transport: upstreamTransport, CheckRedirect: noRedirect}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		b.err(w, http.StatusServiceUnavailable, companionOffline, offlineMessage(sessionID))
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, maxProxyBody))
}

// upstreamTransport is the transport for Companion calls. It is separate from
// the default one so its idle connections never mix with other traffic.
var upstreamTransport = &http.Transport{
	MaxIdleConnsPerHost: 4,
	IdleConnTimeout:     30 * time.Second,
}

// noRedirect stops the bridge from following a redirect a Companion returns, so
// the bridge only ever talks to the loopback address it was given.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// offlineMessage names the profile when the session ID carries it.
func offlineMessage(sessionID string) string {
	profile := strings.TrimPrefix(sessionID, "companion:")
	if profile == "" || profile == sessionID {
		profile = "<profile>"
	}
	return "the Companion for this server is not running; ask the user to run `agentport companion start " + profile + "` on their machine"
}
