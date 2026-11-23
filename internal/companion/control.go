package companion

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rafaelb13/agentport/internal/i18n"
)

//go:embed ui/index.html ui/assets/*
var dashboardFiles embed.FS

// ControlServer exposes a private loopback panel and lifecycle API for a
// running Companion. The view URL contains an unguessable local capability.
type ControlServer struct {
	State    RuntimeState
	server   *http.Server
	listener net.Listener
	// panelClients counts the panel tabs with an open event stream.
	panelClients atomic.Int64
}

// ControlOptions configures the control server. Zero hooks get the defaults
// of the real Companion (the profile's settings file, the profile list).
type ControlOptions struct {
	Profile string
	Status  func() any
	Stop    func()
	// Ports is the port API, served under /v1/ports/ (control token) and, for the
	// panel, under the view prefix (view token).
	Ports http.Handler
	// Settings reads and writes the panel settings of this profile.
	Settings    func() (PanelSettings, error)
	SetSettings func(PanelSettings) error
	// Profiles lists the profiles on this machine for the switch menu.
	Profiles func() ([]ProfileEntry, error)
	// CloseProfile closes every forward of another running profile.
	CloseProfile func(profile string) (int, error)
}

// StartControl starts the loopback control server. An optional ports handler
// is served under /v1/ports/ behind the same control token.
func StartControl(profile string, status func() any, stop func(), ports ...http.Handler) (*ControlServer, error) {
	options := ControlOptions{Profile: profile, Status: status, Stop: stop}
	if len(ports) > 0 {
		options.Ports = ports[0]
	}
	return StartControlWith(options)
}

// StartControlWith starts the loopback control server with explicit options.
func StartControlWith(options ControlOptions) (*ControlServer, error) {
	profile := options.Profile
	if !validProfileName(profile) {
		return nil, fmt.Errorf("invalid companion profile %q", profile)
	}
	if options.Status == nil || options.Stop == nil {
		return nil, fmt.Errorf("Companion control callbacks are required")
	}
	options = withPanelDefaults(options)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	controlToken, err := randomToken(32)
	if err != nil {
		listener.Close()
		return nil, err
	}
	viewToken, err := randomToken(24)
	if err != nil {
		listener.Close()
		return nil, err
	}
	control := &ControlServer{
		State: RuntimeState{
			Profile: profile, Address: listener.Addr().String(), ControlToken: controlToken,
			ViewToken: viewToken, PID: os.Getpid(), StartedAt: time.Now().UTC(),
		},
		listener: listener,
	}
	prefix := "/view/" + viewToken
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", control.requireControl(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	mux.HandleFunc("/v1/status", control.requireControl(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, options.Status())
	}))
	mux.HandleFunc("/v1/control/stop", control.requireControl(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]bool{"stopping": true})
		go options.Stop()
	}))
	viewHandler := control.viewHandler(prefix, options)
	mux.HandleFunc(prefix, viewHandler)
	mux.HandleFunc(prefix+"/", viewHandler)
	if options.Ports != nil {
		portsHandler := options.Ports
		mux.HandleFunc("/v1/ports/", control.requireControl(func(w http.ResponseWriter, r *http.Request) {
			portsHandler.ServeHTTP(w, r)
		}))
	}
	control.server = &http.Server{
		Handler:           noStore(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
	}
	if err := SaveRuntime(control.State); err != nil {
		listener.Close()
		return nil, err
	}
	go func() { _ = control.server.Serve(listener) }()
	return control, nil
}

// PanelClients is the number of panel tabs currently connected to the event stream.
func (c *ControlServer) PanelClients() int { return int(c.panelClients.Load()) }

// viewHandler serves the panel and its API. Everything under the view prefix is
// reached through the view token, which is the only capability the page needs.
func (c *ControlServer) viewHandler(prefix string, options ControlOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == prefix || r.URL.Path == prefix+"/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Vary", "Accept-Language")
			w.Header().Set("Content-Security-Policy", panelCSP)
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			page, err := dashboardFiles.ReadFile("ui/index.html")
			if err != nil {
				http.Error(w, "dashboard unavailable", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(renderDashboard(string(page), prefix, dashboardLanguage(r))))
			return
		}
		if strings.HasPrefix(r.URL.Path, prefix+"/assets/") && r.Method == http.MethodGet {
			name := strings.TrimPrefix(r.URL.Path, prefix+"/")
			asset, err := dashboardFiles.ReadFile("ui/" + name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", assetContentType(name))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_, _ = w.Write(asset)
			return
		}
		if strings.HasPrefix(r.URL.Path, prefix+"/api/ports/") {
			if !panelRequestAllowed(r) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if options.Ports == nil {
				http.NotFound(w, r)
				return
			}
			rest := strings.TrimPrefix(r.URL.Path, prefix+"/api/ports")
			if rest == "/events" && r.Method == http.MethodGet {
				c.panelClients.Add(1)
				defer c.panelClients.Add(-1)
			}
			inner := r.Clone(r.Context())
			inner.URL.Path = "/v1/ports" + rest
			inner.URL.RawPath = ""
			options.Ports.ServeHTTP(w, inner)
			return
		}
		if r.URL.Path == prefix+"/api/settings" {
			c.settingsAPI(w, r, options)
			return
		}
		if r.URL.Path == prefix+"/api/profiles" && r.Method == http.MethodGet {
			entries, err := options.Profiles()
			if err != nil {
				http.Error(w, "profiles unavailable", http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"profiles": entries})
			return
		}
		if r.URL.Path == prefix+"/api/profiles/close-all" && r.Method == http.MethodPost {
			if !panelRequestAllowed(r) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			var body struct {
				Profile string `json:"profile"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || !validProfileName(body.Profile) || body.Profile == options.Profile {
				http.Error(w, "invalid profile", http.StatusBadRequest)
				return
			}
			closed, err := options.CloseProfile(body.Profile)
			if err != nil {
				http.Error(w, "profile ports could not be closed", http.StatusConflict)
				return
			}
			writeJSON(w, map[string]int{"closed": closed})
			return
		}
		if r.URL.Path == prefix+"/api/status" && r.Method == http.MethodGet {
			writeJSON(w, options.Status())
			return
		}
		if r.URL.Path == prefix+"/api/stop" && r.Method == http.MethodPost {
			if !panelRequestAllowed(r) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			writeJSON(w, map[string]bool{"stopping": true})
			go options.Stop()
			return
		}
		http.NotFound(w, r)
	}
}

// settingsAPI reads (GET) or replaces (PUT) the panel settings.
func (c *ControlServer) settingsAPI(w http.ResponseWriter, r *http.Request, options ControlOptions) {
	switch r.Method {
	case http.MethodGet:
		settings, err := options.Settings()
		if err != nil {
			http.Error(w, "settings unavailable", http.StatusInternalServerError)
			return
		}
		writeJSON(w, settings)
	case http.MethodPut:
		if !panelRequestAllowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var settings PanelSettings
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&settings); err != nil {
			http.Error(w, "invalid settings", http.StatusBadRequest)
			return
		}
		if err := options.SetSettings(settings); err != nil {
			http.Error(w, "settings could not be saved", http.StatusInternalServerError)
			return
		}
		writeJSON(w, settings)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// panelRequestAllowed rejects changes that a page on another site or a plain
// HTML form could make: they cannot send JSON, and a cross-site fetch is refused.
func panelRequestAllowed(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
}

// panelCSP lets the page run only its own script and style: no inline code and
// no network other than this Companion.
const panelCSP = "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; object-src 'none'; frame-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

func assetContentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".json"):
		return "application/json; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	}
	return "application/octet-stream"
}

func (c *ControlServer) Close() error {
	if c.server == nil {
		return nil
	}
	err := c.server.Shutdown(context.Background())
	RemoveRuntime(c.State)
	return err
}

func (c *ControlServer) requireControl(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || subtle.ConstantTimeCompare([]byte(parts[1]), []byte(c.State.ControlToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// dashboardLanguage is the language of the first render: ?lang= wins, then what
// the browser asks for, then the language of this process, then English. The
// page itself settles the language again (saved choice, then navigator.languages).
func dashboardLanguage(r *http.Request) string {
	if language, ok := i18n.Supported(r.URL.Query().Get("lang")); ok {
		return language
	}
	if language := i18n.FromAcceptLanguage(r.Header.Get("Accept-Language")); language != "" {
		return language
	}
	return i18n.Language()
}

// renderDashboard fills the page in: the base path, the language and the panel
// catalogs as JSON. Every substituted value is escaped for HTML, and the catalogs
// travel in a JSON block, so nothing in them can become markup or script.
func renderDashboard(page, prefix, language string) string {
	catalogs, err := json.Marshal(panelCatalogs())
	if err != nil {
		catalogs = []byte("{}")
	}
	return strings.NewReplacer(
		"{{BASE}}", html.EscapeString(prefix),
		"{{LANG}}", html.EscapeString(language),
		"{{I18N}}", strings.NewReplacer("<", `<`, ">", `>`, "&", `&`).Replace(string(catalogs)),
	).Replace(page)
}
