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
	"regexp"
	"strings"
	"time"

	"github.com/rafaelb13/agentport/internal/i18n"
)

//go:embed ui/index.html ui/assets/*
var dashboardFiles embed.FS

// ControlServer exposes a private loopback dashboard and lifecycle API for a
// running Companion. The view URL contains an unguessable local capability.
type ControlServer struct {
	State    RuntimeState
	server   *http.Server
	listener net.Listener
}

// StartControl starts the loopback control server. An optional ports handler
// is served under /v1/ports/ behind the same control token.
func StartControl(profile string, status func() any, stop func(), ports ...http.Handler) (*ControlServer, error) {
	if !validProfileName(profile) {
		return nil, fmt.Errorf("invalid companion profile %q", profile)
	}
	if status == nil || stop == nil {
		return nil, fmt.Errorf("Companion control callbacks are required")
	}
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
		writeJSON(w, status())
	}))
	mux.HandleFunc("/v1/control/stop", control.requireControl(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]bool{"stopping": true})
		go stop()
	}))
	viewHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == prefix || r.URL.Path == prefix+"/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Vary", "Accept-Language")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
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
			contentType := "application/octet-stream"
			switch {
			case strings.HasSuffix(name, ".css"):
				contentType = "text/css; charset=utf-8"
			case strings.HasSuffix(name, ".js"):
				contentType = "text/javascript; charset=utf-8"
			case strings.HasSuffix(name, ".svg"):
				contentType = "image/svg+xml"
			}
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_, _ = w.Write(asset)
			return
		}
		if r.URL.Path == prefix+"/api/status" && r.Method == http.MethodGet {
			writeJSON(w, status())
			return
		}
		if r.URL.Path == prefix+"/api/stop" && r.Method == http.MethodPost {
			writeJSON(w, map[string]bool{"stopping": true})
			go stop()
			return
		}
		http.NotFound(w, r)
	}
	mux.HandleFunc(prefix, viewHandler)
	mux.HandleFunc(prefix+"/", viewHandler)
	if len(ports) > 0 && ports[0] != nil {
		portsHandler := ports[0]
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

// dashboardLanguage is the language of a page: ?lang= wins, then what the browser
// asks for, then the language of this process (AGENTPORT_LANG), then English.
func dashboardLanguage(r *http.Request) string {
	if language, ok := i18n.Supported(r.URL.Query().Get("lang")); ok {
		return language
	}
	if language := i18n.FromAcceptLanguage(r.Header.Get("Accept-Language")); language != "" {
		return language
	}
	return i18n.Language()
}

var pageMessage = regexp.MustCompile(`\{\{t:(.*?)\}\}`)

// renderDashboard fills the page in: the base path, the language, the
// translations the script needs and the messages written in the markup. Every
// substituted value is escaped for HTML, and the translations travel as JSON in
// an attribute, so nothing in a catalog can become markup or script.
func renderDashboard(page, prefix, language string) string {
	catalog, err := json.Marshal(scriptCatalog(language))
	if err != nil {
		catalog = []byte("{}")
	}
	page = pageMessage.ReplaceAllStringFunc(page, func(match string) string {
		return html.EscapeString(i18n.Tr(language, pageMessage.FindStringSubmatch(match)[1])) // i18n:dynamic
	})
	return strings.NewReplacer(
		"{{BASE}}", prefix,
		"{{LANG}}", html.EscapeString(language),
		"{{I18N}}", html.EscapeString(string(catalog)),
	).Replace(page)
}

var scriptMessage = regexp.MustCompile(`\bt\(\s*'((?:[^'\\]|\\.)*)'`)

// scriptCatalog is the part of a language's catalog the dashboard script asks
// for. The catalog also holds the help text of the command line, which the
// browser has no use for.
func scriptCatalog(language string) map[string]string {
	script, err := dashboardFiles.ReadFile("ui/assets/app.js")
	if err != nil {
		return map[string]string{}
	}
	wanted := map[string]bool{}
	for _, match := range scriptMessage.FindAllStringSubmatch(string(script), -1) {
		wanted[match[1]] = true
	}
	subset := map[string]string{}
	for message, translated := range i18n.Catalog(language) {
		if wanted[message] {
			subset[message] = translated
		}
	}
	return subset
}
