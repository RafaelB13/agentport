package companion

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/ports"
)

// startPanel starts a control server with fake settings and a port handler, under an isolated home.
func startPanel(t *testing.T, ports http.Handler, settings *PanelSettings) (*ControlServer, string) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("AGENTPORT_CONFIG_DIR", t.TempDir())
	if settings == nil {
		settings = &PanelSettings{}
	}
	var mu sync.Mutex
	control, err := StartControlWith(ControlOptions{
		Profile: "panel",
		Status:  func() any { return map[string]any{"profile": "panel"} },
		Stop:    func() {},
		Ports:   ports,
		Settings: func() (PanelSettings, error) {
			mu.Lock()
			defer mu.Unlock()
			return *settings, nil
		},
		SetSettings: func(next PanelSettings) error {
			mu.Lock()
			defer mu.Unlock()
			*settings = next
			return nil
		},
		Profiles: func() ([]ProfileEntry, error) {
			return []ProfileEntry{{Name: "panel", Current: true, Running: true}}, nil
		},
		CloseProfile: func(string) (int, error) { return 2, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { control.Close() })
	return control, ViewURL(control.State)
}

type recordingPorts struct {
	mu    sync.Mutex
	paths []string
}

func (r *recordingPorts) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.paths = append(r.paths, req.Method+" "+req.URL.Path)
	r.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func fetchText(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(data)
}

// The panel must not load anything from another origin: its markup, script and
// style may reference only this Companion, apart from the project's GitHub link.
var externalURL = regexp.MustCompile(`https?://[^\s"'<>)]+`)

// panelCodeFiles lists the panel's embedded markup, script and style: the page
// and everything the web/ build wrote under ui/assets.
func panelCodeFiles(t *testing.T) []string {
	t.Helper()
	var names []string
	err := fs.WalkDir(dashboardFiles, "ui", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch path.Ext(name) {
		case ".html", ".js", ".css":
			if !entry.IsDir() {
				names = append(names, strings.TrimPrefix(name, "ui/"))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"index.html", "assets/app.js", "assets/app.css"} {
		if !slices.Contains(names, want) {
			t.Fatalf("the built panel lacks %s (run npm run build in web/): %v", want, names)
		}
	}
	return names
}

func TestPanelPageLoadsWithoutExternalURLsAndSetsCSP(t *testing.T) {
	_, base := startPanel(t, &recordingPorts{}, nil)
	response, page := fetchText(t, base)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("page status = %d", response.StatusCode)
	}
	csp := response.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "script-src 'self'", "style-src 'self'", "object-src 'none'", "base-uri 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP %q lacks %q", csp, want)
		}
	}
	// Scripts must never run inline or through eval; only style-src may allow inline styles.
	directives := map[string]string{}
	for _, part := range strings.Split(csp, ";") {
		fields := strings.Fields(part)
		if len(fields) > 0 {
			directives[fields[0]] = strings.Join(fields[1:], " ")
		}
	}
	if script := directives["script-src"]; strings.Contains(script, "unsafe-inline") || strings.Contains(script, "unsafe-eval") {
		t.Fatalf("script-src allows inline or eval code: %q", script)
	}
	for name, sources := range directives {
		if name != "style-src" && strings.Contains(sources, "unsafe-") {
			t.Fatalf("%s allows unsafe sources: %q", name, sources)
		}
	}
	if style := directives["style-src"]; style != "'self' 'unsafe-inline'" {
		t.Fatalf("style-src = %q, want 'self' 'unsafe-inline' (xterm.js needs inline styles)", style)
	}
	// Every file of the bundle is checked as the Companion serves it.
	for _, name := range panelCodeFiles(t) {
		var body string
		if name == "index.html" {
			body = page
		} else {
			served, text := fetchText(t, base+name)
			if served.StatusCode != http.StatusOK {
				t.Fatalf("%s status = %d", name, served.StatusCode)
			}
			body = text
		}
		for _, link := range externalURL.FindAllString(body, -1) {
			if !strings.HasPrefix(link, "https://github.com/rafaelb13/agentport") {
				t.Errorf("%s refers to %s, which is outside this Companion", name, link)
			}
		}
		if strings.Contains(body, "@import") || strings.Contains(body, "fonts.googleapis") {
			t.Errorf("%s loads a remote resource", name)
		}
	}
	if !strings.Contains(page, "/assets/app.js") {
		t.Fatal("the page does not load its script from this Companion")
	}
}

// The page names its assets with the base the Companion writes in ({{BASE}}),
// so they load under the view prefix whatever the page's own URL looks like;
// the script reads the same base from the page to reach the API.
func TestPanelPageLoadsItsAssetsUnderTheViewPrefix(t *testing.T) {
	control, base := startPanel(t, &recordingPorts{}, nil)
	_, page := fetchText(t, base)
	prefix := "/view/" + control.State.ViewToken
	for _, placeholder := range []string{"{{BASE}}", "{{LANG}}", "{{I18N}}"} {
		if strings.Contains(page, placeholder) {
			t.Fatalf("the served page still holds %s", placeholder)
		}
	}
	if !strings.Contains(page, `<meta name="agentport-base" content="`+prefix+`"`) {
		t.Fatal("the page does not carry its base path")
	}
	refs := regexp.MustCompile(`<(?:script|link)\b[^>]*\b(?:src|href)="([^"]+)"`).FindAllStringSubmatch(page, -1)
	if len(refs) < 3 {
		t.Fatalf("expected the icon, the script and the stylesheet, got %v", refs)
	}
	for _, ref := range refs {
		if !strings.HasPrefix(ref[1], prefix+"/assets/") {
			t.Errorf("%s does not load from %s/assets/", ref[1], prefix)
			continue
		}
		response, body := fetchText(t, "http://"+control.State.Address+ref[1])
		if response.StatusCode != http.StatusOK || body == "" {
			t.Errorf("%s = %d (%d bytes)", ref[1], response.StatusCode, len(body))
		}
	}
	script, err := dashboardFiles.ReadFile("ui/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "agentport-base") {
		t.Fatal("the script does not read the base path from the page")
	}
}

func TestPanelPageSendsNoStoreAndStaysOnLoopbackAddress(t *testing.T) {
	control, base := startPanel(t, &recordingPorts{}, nil)
	if !strings.HasPrefix(control.State.Address, "127.0.0.1:") {
		t.Fatalf("panel bound to %s", control.State.Address)
	}
	response, _ := fetchText(t, base)
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %v", response.Header)
	}
}

// The page's CSP forbids inline scripts, so the markup may hold only scripts
// loaded from the Companion and the JSON block of catalogs (data, never run).
func TestPanelMarkupHasNoInlineStylesOrScripts(t *testing.T) {
	page, err := dashboardFiles.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(page), " style=") {
		t.Fatal("index.html has an inline style attribute")
	}
	for _, tag := range regexp.MustCompile(`<script\b[^>]*>`).FindAllString(string(page), -1) {
		if !strings.Contains(tag, ` src="`) && !strings.Contains(tag, `type="application/json"`) {
			t.Errorf("index.html has an inline script %s, which the panel CSP blocks", tag)
		}
	}
}

// The panel runs under script-src 'self' without 'unsafe-eval': no part of the
// bundle may turn a string into code.
func TestPanelBundleNeverEvaluatesStrings(t *testing.T) {
	for _, name := range panelCodeFiles(t) {
		if path.Ext(name) != ".js" {
			continue
		}
		script, err := dashboardFiles.ReadFile("ui/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"eval(", "new Function("} {
			if strings.Contains(string(script), forbidden) {
				t.Errorf("%s uses %s, which the panel CSP blocks", name, forbidden)
			}
		}
	}
}

// panelSource walks the panel's source in web/src, which the build bundles.
func panelSource(t *testing.T, visit func(name, text string)) {
	t.Helper()
	root := filepath.Join("..", "..", "web", "src")
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch filepath.Ext(name) {
		case ".ts", ".tsx", ".js":
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			visit(filepath.ToSlash(name), string(data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The panel builds its DOM with React elements, which set every value as text;
// nothing in its source writes markup from a string or runs a string as code.
func TestPanelSourceNeverBuildsMarkupFromStrings(t *testing.T) {
	files := 0
	panelSource(t, func(name, text string) {
		files++
		for _, forbidden := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "dangerouslySetInnerHTML", "eval(", "new Function("} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s uses %s; build the DOM with React elements", name, forbidden)
			}
		}
	})
	if files < 10 {
		t.Fatalf("only %d source files found in web/src", files)
	}
}

func TestPanelScriptParsesWithNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	for _, name := range panelCodeFiles(t) {
		if path.Ext(name) != ".js" {
			continue
		}
		script, err := dashboardFiles.ReadFile("ui/" + name)
		if err != nil {
			t.Fatal(err)
		}
		// The bundle is made of ES modules.
		file := filepath.Join(t.TempDir(), strings.TrimSuffix(path.Base(name), ".js")+".mjs")
		if err := os.WriteFile(file, script, 0600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(node, "--check", file).CombinedOutput(); err != nil {
			t.Fatalf("node --check %s: %v\n%s", name, err, out)
		}
	}
}

// pickLanguage must follow the rule: saved choice, then navigator.languages
// (zh-TW/HK/MO/Hant to Traditional, zh-CN to English), then English. The
// panel's own module (plain JavaScript) runs in Node with the inputs given.
func TestPanelLanguagePickingRuleRunsInNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	module, err := filepath.Abs(filepath.Join("..", "..", "web", "src", "lib", "language.js"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(module); err != nil {
		t.Fatal(err)
	}
	moduleURL := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(module), "/")}).String()
	program := "import { pickLanguage } from " + strconv.Quote(moduleURL) + ";\n" +
		`const cases = [
  [null, ['pt-BR'], 'pt-BR'],
  ['es', ['pt-BR'], 'es'],
  [null, ['zh-TW', 'en'], 'zh-Hant'],
  [null, ['zh-HK'], 'zh-Hant'],
  [null, ['zh-Hant-TW'], 'zh-Hant'],
  [null, ['zh-CN'], 'en'],
  [null, ['fr-FR'], 'en'],
  ['pt-BR', ['zh-TW'], 'pt-BR'],
  [null, ['en-US'], 'en'],
  ['bogus', ['es-MX'], 'es'],
  [null, [], 'en'],
];
let bad = [];
for (const [saved, langs, want] of cases) {
  const got = pickLanguage(saved, langs);
  if (got !== want) bad.push(JSON.stringify([saved, langs, want, got]));
}
if (bad.length) { console.error(bad.join('\n')); process.exit(1); }
`
	file := filepath.Join(t.TempDir(), "lang.mjs")
	if err := os.WriteFile(file, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, file).CombinedOutput(); err != nil {
		t.Fatalf("language rule: %v\n%s", err, out)
	}
}

var (
	// Keys the source looks up: t('...') with a literal English key.
	scriptKey = regexp.MustCompile(`\bt\(\s*'((?:[^'\\]|\\.)*)'`)
	// A t( call whose key is not a single-quoted literal would hide its key
	// from this test.
	dynamicKey  = regexp.MustCompile(`\bt\(\s*[^'\s]`)
	placeholder = regexp.MustCompile(`\{[a-z]+\}`)
)

func TestPanelCatalogsMatchTheEnglishKeys(t *testing.T) {
	english := map[string]bool{}
	panelSource(t, func(name, text string) {
		for _, m := range scriptKey.FindAllStringSubmatch(text, -1) {
			english[strings.ReplaceAll(m[1], `\'`, `'`)] = true
		}
		for _, m := range dynamicKey.FindAllString(text, -1) {
			t.Errorf("%s calls %s…: pass t a literal key in single quotes", name, m)
		}
	})
	if len(english) < 100 {
		t.Fatalf("only %d English keys found; the extraction is broken", len(english))
	}
	for _, language := range panelLanguages {
		payload, err := dashboardFiles.ReadFile("ui/assets/i18n/" + language + ".json")
		if err != nil {
			t.Fatalf("catalog %s: %v", language, err)
		}
		var catalog map[string]string
		if err := json.Unmarshal(payload, &catalog); err != nil {
			t.Fatalf("catalog %s is not valid JSON: %v", language, err)
		}
		for key := range english {
			translated, ok := catalog[key]
			if !ok {
				t.Errorf("%s misses the key %q", language, key)
				continue
			}
			if !sameSet(placeholder.FindAllString(key, -1), placeholder.FindAllString(translated, -1)) {
				t.Errorf("%s: placeholders of %q differ in %q", language, key, translated)
			}
			if strings.TrimSpace(translated) == "" {
				t.Errorf("%s has an empty translation for %q", language, key)
			}
		}
		for key := range catalog {
			if !english[key] {
				t.Errorf("%s has the orphan key %q", language, key)
			}
		}
	}
}

func sameSet(a, b []string) bool {
	count := map[string]int{}
	for _, v := range a {
		count[v]++
	}
	for _, v := range b {
		count[v]--
	}
	for _, n := range count {
		if n != 0 {
			return false
		}
	}
	return true
}

func TestRenderDashboardEmbedsCatalogsSafely(t *testing.T) {
	page := renderDashboard(`<meta name="agentport-base" content="{{BASE}}"><html lang="{{LANG}}"><script id="agentport-i18n" type="application/json">{{I18N}}</script>`, "/view/x", `pt-BR"><script>alert(1)</script>`)
	if strings.Contains(page, "<script>alert") {
		t.Fatal("language was not escaped")
	}
	start := strings.Index(page, `type="application/json">`) + len(`type="application/json">`)
	end := strings.Index(page[start:], "</script>")
	var catalogs map[string]map[string]string
	if err := json.Unmarshal([]byte(page[start:start+end]), &catalogs); err != nil {
		t.Fatalf("embedded catalogs are not JSON: %v", err)
	}
	if catalogs["pt-BR"]["Approve"] != "Aprovar" || catalogs["zh-Hant"]["Approve"] != "核准" {
		t.Fatalf("catalogs = %v", catalogs)
	}
}

func TestPanelSettingsRoundTripAndDefaults(t *testing.T) {
	t.Setenv("AGENTPORT_CONFIG_DIR", t.TempDir())
	settings, err := LoadPanelSettings("panel")
	if err != nil || settings != DefaultPanelSettings() {
		t.Fatalf("defaults = %+v, %v", settings, err)
	}
	if err := SavePanelSettings("panel", PanelSettings{AutoOpen: false, Notify: true}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPanelSettings("panel")
	if err != nil || got.AutoOpen || !got.Notify {
		t.Fatalf("saved = %+v, %v", got, err)
	}
	if _, err := LoadPanelSettings("../escape"); err == nil {
		t.Fatal("unsafe profile name accepted")
	}
}

func TestPanelSettingsEndpointReadsAndWrites(t *testing.T) {
	settings := &PanelSettings{AutoOpen: true, Notify: true}
	_, base := startPanel(t, &recordingPorts{}, settings)
	response, body := fetchText(t, base+"api/settings")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `"auto_open":true`) {
		t.Fatalf("GET settings = %d %s", response.StatusCode, body)
	}
	put := func(contentType, site, payload string) int {
		request, _ := http.NewRequest(http.MethodPut, base+"api/settings", strings.NewReader(payload))
		request.Header.Set("Content-Type", contentType)
		if site != "" {
			request.Header.Set("Sec-Fetch-Site", site)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if code := put("text/plain", "same-origin", `{"auto_open":false}`); code != http.StatusForbidden {
		t.Fatalf("non-JSON PUT = %d", code)
	}
	if code := put("application/json", "cross-site", `{"auto_open":false}`); code != http.StatusForbidden {
		t.Fatalf("cross-site PUT = %d", code)
	}
	if code := put("application/json", "same-origin", `{"auto_open":false,"notify":false}`); code != http.StatusOK {
		t.Fatalf("PUT = %d", code)
	}
	if settings.AutoOpen || settings.Notify {
		t.Fatalf("settings not stored: %+v", settings)
	}
}

func TestPanelPortAPIIsRoutedUnderTheViewTokenWithJSONOnly(t *testing.T) {
	recorder := &recordingPorts{}
	_, base := startPanel(t, recorder, nil)
	request, _ := http.NewRequest(http.MethodPost, base+"api/ports/forwards/5432/pin", strings.NewReader(`{"pinned":true}`))
	request.Header.Set("Content-Type", "text/plain")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("form-style POST = %d, want 403", response.StatusCode)
	}
	request, _ = http.NewRequest(http.MethodPost, base+"api/ports/forwards/5432/pin", strings.NewReader(`{"pinned":true}`))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("JSON POST = %d", response.StatusCode)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.paths) != 1 || recorder.paths[0] != "POST /v1/ports/forwards/5432/pin" {
		t.Fatalf("ports handler saw %v", recorder.paths)
	}
}

func TestPanelRequiresTheViewTokenForTheAPI(t *testing.T) {
	_, base := startPanel(t, &recordingPorts{}, nil)
	wrong := strings.Replace(base, "/view/", "/view/nope", 1)
	response, _ := fetchText(t, wrong+"api/ports/state")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("API without the token = %d", response.StatusCode)
	}
}

func TestPanelEventStreamCountsConnectedTabs(t *testing.T) {
	block := make(chan struct{})
	streaming := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: state\ndata: {\"version\":1}\n\n"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-block:
		}
	})
	control, base := startPanel(t, streaming, nil)
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"api/ports/events", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
	}()
	waitPanel(t, func() bool { return control.PanelClients() == 1 })
	cancel()
	<-done
	waitPanel(t, func() bool { return control.PanelClients() == 0 })
	close(block)
}

func TestPanelProfileCloseGoesThroughTheCompanionOfThatProfile(t *testing.T) {
	control, base := startPanel(t, &recordingPorts{}, nil)
	_ = control
	request, _ := http.NewRequest(http.MethodPost, base+"api/profiles/close-all", strings.NewReader(`{"profile":"other"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"closed":2`) {
		t.Fatalf("close-all = %d %s", response.StatusCode, body)
	}
	request, _ = http.NewRequest(http.MethodPost, base+"api/profiles/close-all", strings.NewReader(`{"profile":"panel"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("closing the current profile through the switch menu = %d", response.StatusCode)
	}
}

func waitPanel(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

// --- auto open ----------------------------------------------------------------

type autoRecorder struct {
	mu       sync.Mutex
	opened   []string
	notified []string
}

func (a *autoRecorder) open(url string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.opened = append(a.opened, url)
	return nil
}

func (a *autoRecorder) notify(title, body string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.notified = append(a.notified, title+": "+body)
	return nil
}

func newAuto(clients int, settings PanelSettings, rec *autoRecorder, now *time.Time) *AutoOpen {
	return &AutoOpen{
		URL:      func() string { return "http://127.0.0.1:1/view/x/" },
		Settings: func() PanelSettings { return settings },
		Clients:  func() int { return clients },
		Open:     rec.open,
		Notify:   rec.notify,
		Now:      func() time.Time { return *now },
		Cooldown: 10 * time.Second,
	}
}

func pendingFrom(agent string, remote ...int) ports.PendingRequest {
	req := ports.PendingRequest{Agent: agent}
	for _, port := range remote {
		req.Ports = append(req.Ports, ports.PendingPort{RemotePort: port})
	}
	return req
}

func TestAutoOpenOpensThePanelWhenNoTabIsConnected(t *testing.T) {
	rec := &autoRecorder{}
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	newAuto(0, DefaultPanelSettings(), rec, &now).OnPending(pendingFrom("claude", 5432))
	if len(rec.opened) != 1 || rec.opened[0] != "http://127.0.0.1:1/view/x/" {
		t.Fatalf("opened = %v", rec.opened)
	}
	if len(rec.notified) != 1 || !strings.Contains(rec.notified[0], "claude asks for port :5432") {
		t.Fatalf("notified = %v", rec.notified)
	}
}

func TestAutoOpenStaysQuietWhilePanelIsOpen(t *testing.T) {
	rec := &autoRecorder{}
	now := time.Now()
	newAuto(1, DefaultPanelSettings(), rec, &now).OnPending(pendingFrom("claude", 5432))
	if len(rec.opened) != 0 || len(rec.notified) != 0 {
		t.Fatalf("acted with a panel open: opened=%v notified=%v", rec.opened, rec.notified)
	}
}

func TestAutoOpenHonoursEachSettingSeparately(t *testing.T) {
	rec := &autoRecorder{}
	now := time.Now()
	newAuto(0, PanelSettings{AutoOpen: false, Notify: true}, rec, &now).OnPending(pendingFrom("", 8080))
	if len(rec.opened) != 0 || len(rec.notified) != 1 || !strings.Contains(rec.notified[0], "an agent asks for port :8080") {
		t.Fatalf("notify only: opened=%v notified=%v", rec.opened, rec.notified)
	}
	rec = &autoRecorder{}
	newAuto(0, PanelSettings{AutoOpen: true, Notify: false}, rec, &now).OnPending(pendingFrom("x", 8080))
	if len(rec.opened) != 1 || len(rec.notified) != 0 {
		t.Fatalf("open only: opened=%v notified=%v", rec.opened, rec.notified)
	}
	rec = &autoRecorder{}
	newAuto(0, PanelSettings{}, rec, &now).OnPending(pendingFrom("x", 8080))
	if len(rec.opened) != 0 || len(rec.notified) != 0 {
		t.Fatal("both off still acted")
	}
}

func TestAutoOpenCooldownAvoidsOneTabPerRequest(t *testing.T) {
	rec := &autoRecorder{}
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	auto := newAuto(0, DefaultPanelSettings(), rec, &now)
	auto.OnPending(pendingFrom("a", 1))
	now = now.Add(3 * time.Second)
	auto.OnPending(pendingFrom("a", 2))
	if len(rec.opened) != 1 {
		t.Fatalf("second request within the cooldown opened again: %v", rec.opened)
	}
	if len(rec.notified) != 2 {
		t.Fatalf("each request should still notify: %v", rec.notified)
	}
	now = now.Add(11 * time.Second)
	auto.OnPending(pendingFrom("a", 3))
	if len(rec.opened) != 2 {
		t.Fatalf("after the cooldown the panel should open again: %v", rec.opened)
	}
}

func TestAutoOpenReportsFailuresWithoutPanicking(t *testing.T) {
	now := time.Now()
	var reported []error
	auto := &AutoOpen{
		URL:      func() string { return "u" },
		Settings: func() PanelSettings { return DefaultPanelSettings() },
		Clients:  func() int { return 0 },
		Open:     func(string) error { return errors.New("no browser") },
		Notify:   func(string, string) error { return errors.New("no notifier") },
		Now:      func() time.Time { return now },
		OnError:  func(err error) { reported = append(reported, err) },
	}
	auto.OnPending(pendingFrom("a", 1))
	if len(reported) != 2 {
		t.Fatalf("reported = %v", reported)
	}
}

func TestAppleScriptStringCannotEndTheScript(t *testing.T) {
	got := appleScriptString("evil\" & do shell script \"rm\\\n")
	if strings.Contains(got, "\n") || !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
		t.Fatalf("quoted = %s", got)
	}
	if strings.Count(got, `"`)-strings.Count(got, `\"`) != 2 {
		t.Fatalf("unescaped quote in %s", got)
	}
}

func TestListPanelProfilesWithoutAnyProfileIsEmpty(t *testing.T) {
	t.Setenv("AGENTPORT_CONFIG_DIR", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	entries, err := ListPanelProfiles("none")
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries = %v, %v", entries, err)
	}
}

func TestCloseProfileForwardsRefusesAStoppedProfile(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if _, err := CloseProfileForwards("missing"); err == nil {
		t.Fatal("a stopped profile answered")
	}
}
