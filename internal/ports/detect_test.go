package ports

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

// fakeDetector returns a fixed list and records what it was asked.
type fakeDetector struct {
	ports   []DetectedPort
	err     error
	mu      sync.Mutex
	refresh []bool
}

func (f *fakeDetector) Detect(_ context.Context, refresh bool) ([]DetectedPort, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refresh = append(f.refresh, refresh)
	return f.ports, f.err
}

type detectedBody struct {
	Ports []DetectedPort `json:"ports"`
	Error string         `json:"error"`
}

func decodeDetected(t *testing.T, body []byte) detectedBody {
	t.Helper()
	var out detectedBody
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return out
}

func TestDetectedLeavesOutForwardedPortsUnlessAsked(t *testing.T) {
	store := newStoreForHandler(t)
	if _, err := store.OpenByUser(context.Background(), 3000, 13000); err != nil {
		t.Fatal(err)
	}
	detector := &fakeDetector{ports: []DetectedPort{
		{Port: 3000, Bind: "127.0.0.1", PID: 41, Process: "node", User: "dev", Cwd: "/apps/web"},
		{Port: 8080, Bind: "0.0.0.0", PID: 42, Process: "python3", Cmdline: "python3 -m http.server"},
	}}
	h := NewHandlerWithDetector(store, detector)

	rec := do(t, h, http.MethodGet, "/v1/ports/detected", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeDetected(t, rec.Body.Bytes())
	if len(body.Ports) != 1 || body.Ports[0].Port != 8080 || body.Error != "" {
		t.Fatalf("default list = %+v, want only 8080", body)
	}

	rec = do(t, h, http.MethodGet, "/v1/ports/detected?all=1&refresh=1", "", nil)
	body = decodeDetected(t, rec.Body.Bytes())
	if len(body.Ports) != 2 {
		t.Fatalf("all=1 list = %+v, want both ports", body.Ports)
	}
	forwarded := body.Ports[0]
	if forwarded.Port != 3000 || !forwarded.Forwarded || forwarded.LocalPort != 13000 || forwarded.PID != 41 || forwarded.Cwd != "/apps/web" {
		t.Fatalf("forwarded port = %+v", forwarded)
	}
	if detector.refresh[len(detector.refresh)-1] != true || detector.refresh[0] != false {
		t.Fatalf("refresh flags = %v, want refresh only when asked", detector.refresh)
	}
}

func TestDetectedReportsAFailedScanWithoutFailingTheRequest(t *testing.T) {
	store := newStoreForHandler(t)
	h := NewHandlerWithDetector(store, &fakeDetector{err: errors.New("ssh: connection refused")})
	rec := do(t, h, http.MethodGet, "/v1/ports/detected", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a failed scan must answer 200 so the panel can explain it, got %d", rec.Code)
	}
	body := decodeDetected(t, rec.Body.Bytes())
	if body.Error == "" || len(body.Ports) != 0 {
		t.Fatalf("body = %+v, want the error and no ports", body)
	}
}

func TestDetectedWithoutADetectorSaysSo(t *testing.T) {
	h := NewHandler(newStoreForHandler(t))
	body := decodeDetected(t, do(t, h, http.MethodGet, "/v1/ports/detected", "", nil).Body.Bytes())
	if body.Error == "" || body.Ports == nil || len(body.Ports) != 0 {
		t.Fatalf("body = %+v", body)
	}
}

func TestCheckHealthReportsTCPAndHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("health check sent %s, want HEAD", r.Method)
		}
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()
	port := portOf(t, server.Listener.Addr())

	health := CheckHealth(context.Background(), port)
	if !health.TCP || health.HTTPStatus != http.StatusTeapot || health.HTTPLatencyMS < 0 || health.TCPError != "" {
		t.Fatalf("health = %+v", health)
	}
}

func TestCheckHealthOfAClosedPortIsNotTCPOk(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := portOf(t, listener.Addr())
	listener.Close()

	health := CheckHealth(context.Background(), port)
	if health.TCP || health.TCPError == "" || health.HTTPStatus != 0 {
		t.Fatalf("health = %+v, want a refused TCP connect", health)
	}
}

func TestLocalHealthRoute(t *testing.T) {
	h := NewHandler(newStoreForHandler(t))
	if rec := do(t, h, http.MethodGet, "/v1/ports/local/health?port=abc", "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-numeric port = %d, want 400", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/v1/ports/local/health?port=70000", "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("out-of-range port = %d, want 400", rec.Code)
	}
}

func portOf(t *testing.T, address net.Addr) int {
	t.Helper()
	_, portText, err := net.SplitHostPort(address.String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return port
}
