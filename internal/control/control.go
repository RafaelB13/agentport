// Package control is the client of the bridge's local control plane: the
// authenticated loopback API the CLI and the Companion use to register
// sessions, publish release status and stop the bridge.
package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rafaelb13/agentport/internal/daemon"
	"github.com/rafaelb13/agentport/internal/release"
)

func PublishReleaseStatus(state daemon.State, status release.Status) error {
	payload, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return Post(state, "/v1/control/release", payload, nil)
}

func Post(state daemon.State, path string, payload []byte, output any) error {
	target, err := endpoint(state, path)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+state.ControlToken)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 8*1024))
		return fmt.Errorf("bridge control returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(output)
}

func Healthy(state daemon.State) bool {
	if !ValidLoopbackAddress(state.Address) {
		return false
	}
	response, err := (&http.Client{Timeout: time.Second}).Get("http://" + state.Address + "/healthz")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func Port(state daemon.State) int {
	_, port, err := net.SplitHostPort(state.Address)
	if err != nil {
		return 0
	}
	value, _ := strconv.Atoi(port)
	return value
}

func ValidLoopbackAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return false
	}
	value, err := strconv.Atoi(port)
	return err == nil && value > 0 && value <= 65535
}

// Shutdown asks the bridge to stop.
func Shutdown(state daemon.State) error {
	return Post(state, "/v1/control/shutdown", nil, nil)
}

// endpoint builds the URL of a control path on the bridge's loopback address.
// The request carries the control token, so the URL is assembled from parts
// instead of by concatenation: a path such as "@evil.example/x" would otherwise
// turn the loopback address into credentials and send the token to another host.
func endpoint(state daemon.State, path string) (string, error) {
	if !ValidLoopbackAddress(state.Address) {
		return "", errors.New("bridge state has an invalid loopback address")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", errors.New("control path must start with a single slash")
	}
	target := url.URL{Scheme: "http", Host: state.Address, Path: path}
	return target.String(), nil
}
