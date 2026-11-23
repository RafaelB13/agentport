// Package bridge provides the authenticated, loopback HTTP bridge that the
// remote MCP process reaches through the SSH reverse tunnel. Every endpoint
// except /healthz requires a persistent session token registered by a
// Companion profile.
package bridge

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rafaelb13/agentport/internal/release"
)

// Session is a token grant held by one Companion profile.
type Session struct {
	ID        string
	TokenHash [32]byte
	CreatedAt time.Time
}

// Logger receives security-relevant events. Implementations must record
// metadata only: never tokens or destination paths.
type Logger interface {
	Info(message string, args ...any)
	Error(message string, args ...any)
}

// StatusResponse is what the remote MCP process reads to report the bridge.
type StatusResponse struct {
	Bridge  string         `json:"bridge"`
	Release release.Status `json:"release"`
}

// ErrorResponse is the JSON body of every rejected request.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Bridge struct {
	mu     sync.Mutex
	logger Logger
	// rejections rate-limits rejection logs per error code so unauthenticated
	// traffic cannot grow the log without bound.
	rejections map[string]*rejectionWindow
	sessions   map[string]*Session
	release    release.Status
	now        func() time.Time
}

func New() *Bridge {
	return &Bridge{sessions: make(map[string]*Session), rejections: make(map[string]*rejectionWindow), now: time.Now}
}

// RegisterPersistentSession grants the token access to the bridge until it is
// revoked or the bridge stops.
func (b *Bridge) RegisterPersistentSession(id, token string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(token) == "" {
		return public("persistent session ID and token are required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sessions[id] = &Session{ID: id, TokenHash: sha256.Sum256([]byte(token)), CreatedAt: b.now()}
	return nil
}

// SetReleaseStatus stores the public release state supplied by a Companion.
func (b *Bridge) SetReleaseStatus(status release.Status) {
	b.mu.Lock()
	b.release = status
	b.mu.Unlock()
}

// SetLogger enables event logging; a nil logger disables it.
func (b *Bridge) SetLogger(logger Logger) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logger = logger
}

func (b *Bridge) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", b.health)
	mux.HandleFunc("/v1/status", b.status)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func (b *Bridge) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (b *Bridge) status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		b.err(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		return
	}
	if code := b.authenticate(r); code != "" {
		b.err(w, http.StatusUnauthorized, code, "unauthorized")
		return
	}
	b.mu.Lock()
	out := StatusResponse{Bridge: "ok", Release: b.release}
	b.mu.Unlock()
	b.json(w, out)
}

// authenticate returns an empty string when the request carries a live token.
func (b *Bridge) authenticate(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "UNAUTHORIZED"
	}
	hash := sha256.Sum256([]byte(parts[1]))
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, session := range b.sessions {
		if subtle.ConstantTimeCompare(hash[:], session.TokenHash[:]) == 1 {
			return ""
		}
	}
	return "UNAUTHORIZED"
}

// logLocked must be called with b.mu held.
func (b *Bridge) logLocked(message string, args ...any) {
	if b.logger == nil {
		return
	}
	b.logger.Info("%s", sanitizeLogLine(fmt.Sprintf(message, args...)))
}

// sanitizeLogLine keeps request-derived text from forging extra log lines.
func sanitizeLogLine(line string) string {
	line = strings.ReplaceAll(line, "\n", `\n`)
	return strings.ReplaceAll(line, "\r", `\r`)
}

const (
	rejectionLogWindow = time.Minute
	// rejectionLogBurst is how many rejections per code are logged in each
	// window; the rest are counted and summarized when the window rolls over.
	rejectionLogBurst = 5
	maxRejectionCodes = 64
)

type rejectionWindow struct {
	start      time.Time
	logged     int
	suppressed int
}

func (b *Bridge) logRejection(status int, code string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.logger == nil {
		return
	}
	if _, known := b.rejections[code]; !known && len(b.rejections) >= maxRejectionCodes {
		code = "OTHER"
	}
	window := b.rejections[code]
	if window == nil {
		window = &rejectionWindow{}
		b.rejections[code] = window
	}
	now := b.now()
	if now.Sub(window.start) >= rejectionLogWindow {
		if window.suppressed > 0 {
			b.logLocked("request rejections suppressed: code=%s count=%d", code, window.suppressed)
		}
		*window = rejectionWindow{start: now}
	}
	if window.logged >= rejectionLogBurst {
		window.suppressed++
		return
	}
	window.logged++
	b.logLocked("request rejected: status=%d code=%s", status, code)
}

func (b *Bridge) err(w http.ResponseWriter, status int, code, msg string) {
	b.logRejection(status, code)
	b.jsonStatus(w, status, ErrorResponse{Code: code, Message: msg})
}

func (b *Bridge) json(w http.ResponseWriter, value any) { b.jsonStatus(w, http.StatusOK, value) }

func (b *Bridge) jsonStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// publicError is an error whose text is safe to show to any caller.
type publicError struct{ message string }

func (e *publicError) Error() string { return e.message }

func public(message string) error { return &publicError{message} }

// Reason is the text to send to a caller for err. Only errors made with public
// carry their own words; anything else is reduced to a generic sentence.
func Reason(err error) string {
	if known, ok := err.(*publicError); ok {
		return known.message
	}
	return "request rejected"
}
