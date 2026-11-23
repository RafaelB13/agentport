package bridge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rafaelb13/agentport/internal/release"
)

type recordingLogger struct {
	mu       sync.Mutex
	messages []string
}

func (l *recordingLogger) Info(message string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, fmt.Sprintf(message, args...))
}

func (l *recordingLogger) Error(message string, args ...any) { l.Info(message, args...) }

func get(t *testing.T, handler http.Handler, path, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestHealthzIsOpen(t *testing.T) {
	response := get(t, New().Handler(), "/healthz", "")
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "ok" {
		t.Fatalf("healthz = %d %q", response.Code, response.Body.String())
	}
}

func TestStatusRequiresRegisteredToken(t *testing.T) {
	b := New()
	if err := b.RegisterPersistentSession("companion:work", "token-1"); err != nil {
		t.Fatal(err)
	}
	handler := b.Handler()
	for _, authorization := range []string{"", "Bearer wrong", "Basic token-1"} {
		if response := get(t, handler, "/v1/status", authorization); response.Code != http.StatusUnauthorized {
			t.Fatalf("authorization %q: status %d, want 401", authorization, response.Code)
		}
	}
}

func TestStatusReportsBridgeAndRelease(t *testing.T) {
	b := New()
	if err := b.RegisterPersistentSession("companion:work", "token-1"); err != nil {
		t.Fatal(err)
	}
	b.SetReleaseStatus(release.Status{CurrentVersion: "v1.0.0", LatestVersion: "v1.1.0", UpdateAvailable: true})
	response := get(t, b.Handler(), "/v1/status", "Bearer token-1")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	var status StatusResponse
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Bridge != "ok" || status.Release.LatestVersion != "v1.1.0" || !status.Release.UpdateAvailable {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestStatusRejectsOtherMethods(t *testing.T) {
	b := New()
	if err := b.RegisterPersistentSession("companion:work", "token-1"); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/status", nil)
	request.Header.Set("Authorization", "Bearer token-1")
	response := httptest.NewRecorder()
	b.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", response.Code)
	}
}

func TestRegisterPersistentSessionRequiresIDAndToken(t *testing.T) {
	b := New()
	for _, pair := range [][2]string{{"", "token"}, {"id", " "}} {
		err := b.RegisterPersistentSession(pair[0], pair[1])
		if err == nil {
			t.Fatalf("session %q/%q was accepted", pair[0], pair[1])
		}
		if got := Reason(err); got != "persistent session ID and token are required" {
			t.Fatalf("reason = %q", got)
		}
	}
}

func TestReasonHidesUnknownErrors(t *testing.T) {
	if got := Reason(fmt.Errorf("open /Users/someone/secret: permission denied")); got != "request rejected" {
		t.Fatalf("reason leaked: %q", got)
	}
}

func TestRejectionLogsAreRateLimited(t *testing.T) {
	logger := &recordingLogger{}
	b := New()
	b.SetLogger(logger)
	handler := b.Handler()
	for range 20 {
		get(t, handler, "/v1/status", "Bearer nope")
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if len(logger.messages) != rejectionLogBurst {
		t.Fatalf("logged %d rejections, want %d", len(logger.messages), rejectionLogBurst)
	}
}
