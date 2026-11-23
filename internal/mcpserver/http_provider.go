package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rafaelb13/agentport/internal/release"
)

// HTTPProvider reads bridge state through the loopback port exposed by the SSH
// reverse forward. Its endpoint must be an HTTP URL on 127.0.0.1.
type HTTPProvider struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewHTTPProvider constructs a bridge client. The port is intentionally the
// only endpoint input accepted by the remote command, preventing requests to
// arbitrary hosts.
func NewHTTPProvider(port int, token string) (*HTTPProvider, error) {
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("bridge port must be between 1 and 65535")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("bridge session token is required")
	}
	return &HTTPProvider{
		baseURL: "http://127.0.0.1:" + strconv.Itoa(port),
		token:   token,
		client:  &http.Client{Timeout: 5 * time.Second},
	}, nil
}

// Status asks the bridge for its state. Getting an answer at all means the
// reverse tunnel from the Companion is up, so the tunnel is reported as
// connected only when the bridge responds.
func (p *HTTPProvider) Status(ctx context.Context) (Status, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/v1/status", nil)
	if err != nil {
		return Status{}, err
	}
	request.Header.Set("Authorization", "Bearer "+p.token)
	response, err := p.client.Do(request)
	if err != nil {
		return Status{Tunnel: "unreachable"}, fmt.Errorf("bridge unreachable: %w", err)
	}
	defer response.Body.Close()
	if err := responseError(response); err != nil {
		return Status{Tunnel: "unreachable"}, err
	}
	var bridge struct {
		Bridge  string         `json:"bridge"`
		Release release.Status `json:"release"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&bridge); err != nil {
		return Status{}, fmt.Errorf("decode bridge status: %w", err)
	}
	return Status{Bridge: bridge.Bridge, Tunnel: "connected", Release: bridge.Release}, nil
}

func responseError(response *http.Response) error {
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	defer io.Copy(io.Discard, io.LimitReader(response.Body, 8*1024))
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err == nil && body.Code != "" {
		if body.Message != "" {
			return fmt.Errorf("bridge %s: %s", body.Code, body.Message)
		}
		return fmt.Errorf("bridge %s", body.Code)
	}
	return fmt.Errorf("bridge returned HTTP %d", response.StatusCode)
}
