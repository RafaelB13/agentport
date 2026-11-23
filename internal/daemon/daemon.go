// Package daemon runs the persistent loopback bridge and its local control API.
package daemon

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rafaelb13/agentport/internal/bridge"
	"github.com/rafaelb13/agentport/internal/paths"
	"github.com/rafaelb13/agentport/internal/release"
)

const bridgeTransferTimeout = 30 * time.Second

type persistentSessionRequest struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type State struct {
	Address      string    `json:"address"`
	ControlToken string    `json:"control_token"`
	PID          int       `json:"pid"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Daemon struct {
	Bridge   *bridge.Bridge
	State    State
	server   *http.Server
	listener net.Listener
}

// Start launches a loopback daemon. The control token is required for the
// control endpoints and is never accepted from a remote bind.
func Start(controlToken string) (*Daemon, error) {
	if controlToken == "" {
		return nil, errors.New("control token is required")
	}
	b := bridge.New()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	d := &Daemon{Bridge: b, listener: l}
	d.State = State{Address: l.Addr().String(), ControlToken: controlToken, PID: os.Getpid(), UpdatedAt: time.Now().UTC()}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/control/persistent-session", d.persistentSession)
	mux.HandleFunc("/v1/control/upstream", d.upstream)
	mux.HandleFunc("/v1/control/release", d.release)
	mux.HandleFunc("/v1/control/shutdown", d.shutdown)
	mux.Handle("/", b.Handler())
	d.server = &http.Server{
		Handler:           noStore(authControl(controlToken, mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       bridgeTransferTimeout,
		WriteTimeout:      bridgeTransferTimeout,
	}
	if err := saveState(d.State); err != nil {
		l.Close()
		return nil, err
	}
	go func() { _ = d.server.Serve(l) }()
	return d, nil
}

func (d *Daemon) release(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var status release.Status
	if !decodeJSON(w, r, 8*1024, &status) {
		return
	}
	if status.CurrentVersion == "" {
		http.Error(w, "current_version is required", http.StatusBadRequest)
		return
	}
	d.Bridge.SetReleaseStatus(status)
	writeJSON(w, map[string]bool{"updated": true})
}

func (d *Daemon) Close() error {
	if d.server == nil {
		return nil
	}
	err := d.server.Shutdown(context.Background())
	if current, loadErr := LoadState(); loadErr == nil && current.PID == d.State.PID && current.ControlToken == d.State.ControlToken {
		if path, pathErr := statePath(); pathErr == nil {
			_ = os.Remove(path)
		}
	}
	return err
}

func LoadState() (State, error) {
	p, err := statePath()
	if err != nil {
		return State{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		return State{}, err
	}
	defer f.Close()
	var s State
	return s, json.NewDecoder(f).Decode(&s)
}

func (d *Daemon) persistentSession(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var request persistentSessionRequest
	if !decodeJSON(w, r, 4*1024, &request) {
		return
	}
	if err := d.Bridge.RegisterPersistentSession(request.ID, request.Token); err != nil {
		http.Error(w, bridge.Reason(err), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"registered": true})
}

// upstreamRequest tells the bridge where a Companion serves its port protocol.
type upstreamRequest struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Token   string `json:"token"`
}

func (d *Daemon) upstream(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var request upstreamRequest
	if !decodeJSON(w, r, 4*1024, &request) {
		return
	}
	if err := d.Bridge.SetUpstream(request.ID, request.Address, request.Token); err != nil {
		http.Error(w, bridge.Reason(err), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"registered": true})
}

func (d *Daemon) shutdown(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	writeJSON(w, map[string]bool{"stopping": true})
	go func() { _ = d.Close() }()
}

func authControl(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/control/") {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func statePath() (string, error) {
	return paths.Dir("bridge.json")
}

func saveState(s State) error {
	p, err := statePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "bridge.json.tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		enc := json.NewEncoder(tmp)
		enc.SetIndent("", "  ")
		err = enc.Encode(s)
	}
	if e := tmp.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	if err = os.Chmod(name, 0600); err != nil {
		return err
	}
	return os.Rename(name, p)
}

// requireMethod lets a request through only when it uses the endpoint's single
// accepted method. Otherwise it answers 405 and reports false.
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

// decodeJSON reads a request body of at most limit bytes into value. A body
// that is malformed or larger than the limit is answered with 400 and reported
// as false, so the handler never acts on a partial request.
func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	if err := decoder.Decode(value); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return false
	}
	// One Decode stops after the first value; reading on makes the limit apply
	// to the whole body and rejects trailing data.
	if _, err := decoder.Token(); err != io.EOF {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return false
	}
	return true
}
