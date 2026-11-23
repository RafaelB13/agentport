package ports

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/rafaelb13/agentport/internal/portsproto"
)

// DetectedPort is a port the server listens on, with the process that owns it.
// Cwd and the command line are empty when the server does not show them.
type DetectedPort struct {
	Port      int    `json:"port"`
	Bind      string `json:"bind"`
	PID       int    `json:"pid,omitempty"`
	Process   string `json:"process,omitempty"`
	Cmdline   string `json:"cmdline,omitempty"`
	User      string `json:"user,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Forwarded bool   `json:"forwarded"`
	LocalPort int    `json:"local_port,omitempty"`
}

// Detector lists the server's listening ports. refresh asks for a scan that
// ignores any cache the Detector keeps.
type Detector interface {
	Detect(ctx context.Context, refresh bool) ([]DetectedPort, error)
}

type detectedResponse struct {
	Ports     []DetectedPort `json:"ports"`
	UpdatedAt time.Time      `json:"updated_at"`
	Error     string         `json:"error,omitempty"`
}

// detected answers GET /v1/ports/detected. By default the forwarded ports are
// left out, since the Open tab already shows them; all=1 keeps them, marked, so
// the panel can fill the details of an open forward. refresh=1 bypasses the
// cache. A failed scan is reported in error, with an empty list and HTTP 200, so
// the panel can say the server could not be scanned instead of failing.
func (h *handler) detected(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	refresh := query.Get("refresh") == "1"
	all := query.Get("all") == "1"
	response := detectedResponse{Ports: []DetectedPort{}, UpdatedAt: time.Now().UTC()}
	var listed []DetectedPort
	if h.detector == nil {
		response.Error = "the server cannot be scanned from this Companion"
	} else if found, err := h.detector.Detect(r.Context(), refresh); err != nil {
		response.Error = err.Error()
	} else {
		listed = found
	}
	forwarded := map[int]int{}
	for _, f := range h.store.Forwards("") {
		forwarded[f.RemotePort] = f.LocalPort
	}
	for _, port := range listed {
		if local, ok := forwarded[port.Port]; ok {
			if !all {
				continue
			}
			port.Forwarded, port.LocalPort = true, local
		}
		response.Ports = append(response.Ports, port)
	}
	writeJSON(w, http.StatusOK, response)
}

// LocalHealth is what the panel shows about one forward's local port.
type LocalHealth struct {
	Port          int       `json:"port"`
	TCP           bool      `json:"tcp"`
	TCPError      string    `json:"tcp_error,omitempty"`
	HTTPStatus    int       `json:"http_status,omitempty"`
	HTTPLatencyMS int64     `json:"http_latency_ms,omitempty"`
	CheckedAt     time.Time `json:"checked_at"`
}

// healthTimeout bounds the TCP connect and the HTTP HEAD of a health check.
const healthTimeout = time.Second

// CheckHealth connects to 127.0.0.1:port within one second. When the port
// accepts the connection, it also sends an HTTP HEAD and records the status and
// the latency; a port that does not speak HTTP leaves those fields empty.
func CheckHealth(ctx context.Context, port int) LocalHealth {
	health := LocalHealth{Port: port, CheckedAt: time.Now().UTC()}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	dialer := net.Dialer{Timeout: healthTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		health.TCPError = err.Error()
		return health
	}
	_ = conn.Close()
	health.TCP = true
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, "http://"+address+"/", nil)
	if err != nil {
		return health
	}
	client := &http.Client{Timeout: healthTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return health
	}
	defer response.Body.Close()
	health.HTTPStatus = response.StatusCode
	health.HTTPLatencyMS = time.Since(started).Milliseconds()
	return health
}

// localHealth answers GET /v1/ports/local/health?port=N.
func (h *handler) localHealth(w http.ResponseWriter, r *http.Request) {
	port, err := strconv.Atoi(r.URL.Query().Get("port"))
	if err != nil || !portsproto.ValidPort(port) {
		fail(w, &InvalidError{errors.New("port must be between 1 and 65535")})
		return
	}
	writeJSON(w, http.StatusOK, CheckHealth(r.Context(), port))
}
