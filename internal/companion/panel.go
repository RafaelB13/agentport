package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rafaelb13/agentport/internal/i18n"
	"github.com/rafaelb13/agentport/internal/ports"
)

// PanelSettings are the per-profile panel choices the Companion acts on.
// AutoOpen opens the panel when a request arrives and no panel tab is open;
// Notify sends a desktop notification for each such request.
type PanelSettings struct {
	AutoOpen bool `json:"auto_open"`
	Notify   bool `json:"notify"`
}

// DefaultPanelSettings is what a profile gets before anyone changes it.
func DefaultPanelSettings() PanelSettings { return PanelSettings{AutoOpen: true, Notify: true} }

func panelSettingsPath(profile string) (string, error) {
	if !validProfileName(profile) {
		return "", errors.New("companion profile name must be a simple filename")
	}
	dir, err := agentPortConfigDir()
	if err != nil {
		return "", fmt.Errorf("find agentport config directory: %w", err)
	}
	return filepath.Join(dir, "agentport", "panel", profile+".json"), nil
}

// LoadPanelSettings reads the settings of profile. A missing file means the defaults.
func LoadPanelSettings(profile string) (PanelSettings, error) {
	path, err := panelSettingsPath(profile)
	if err != nil {
		return PanelSettings{}, err
	}
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultPanelSettings(), nil
	}
	if err != nil {
		return PanelSettings{}, err
	}
	settings := DefaultPanelSettings()
	if err := json.Unmarshal(payload, &settings); err != nil {
		return PanelSettings{}, fmt.Errorf("read panel settings: %w", err)
	}
	return settings, nil
}

// SavePanelSettings stores the settings of profile privately.
func SavePanelSettings(profile string, settings PanelSettings) error {
	path, err := panelSettingsPath(profile)
	if err != nil {
		return err
	}
	return writePrivateJSON(path, profile+".panel-", settings)
}

// withPanelDefaults fills the hooks a caller left out with the real ones.
func withPanelDefaults(o ControlOptions) ControlOptions {
	profile := o.Profile
	if o.Settings == nil {
		o.Settings = func() (PanelSettings, error) { return LoadPanelSettings(profile) }
	}
	if o.SetSettings == nil {
		o.SetSettings = func(s PanelSettings) error { return SavePanelSettings(profile, s) }
	}
	if o.CloseProfile == nil {
		o.CloseProfile = CloseProfileForwards
	}
	if o.Terminal == nil {
		o.Terminal = func() (*exec.Cmd, error) { return ServerTerminalCommand(profile) }
	}
	if o.Profiles == nil {
		o.Profiles = func() ([]ProfileEntry, error) { return ListPanelProfiles(profile) }
	}
	return o
}

// ProfileEntry is one row of the panel's switch menu.
type ProfileEntry struct {
	Name        string `json:"name"`
	Destination string `json:"destination"`
	Current     bool   `json:"current"`
	Running     bool   `json:"running"`
	// URL opens the panel of a running Companion. It is set for other profiles only.
	URL string `json:"url,omitempty"`
	// Forwards counts the open forwards, when the running Companion answers.
	Forwards *int `json:"forwards,omitempty"`
}

// ListPanelProfiles lists the saved profiles, marking which Companions run and
// how many forwards each has open. current is the profile of the asking panel.
func ListPanelProfiles(current string) ([]ProfileEntry, error) {
	profiles, err := ListProfiles()
	if err != nil {
		return nil, err
	}
	entries := make([]ProfileEntry, 0, len(profiles))
	for _, p := range profiles {
		entry := ProfileEntry{Name: p.Name, Destination: p.Destination, Current: p.Name == current}
		if state, err := LoadRuntime(p.Name); err == nil && RuntimeHealthy(state) {
			entry.Running = true
			if n, err := countForwards(state); err == nil {
				entry.Forwards = &n
			}
			if !entry.Current {
				entry.URL = ViewURL(state)
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// countForwards asks a running Companion for its open forwards.
func countForwards(state RuntimeState) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+state.Address+"/v1/ports/state", nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", "Bearer "+state.ControlToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("ports state returned HTTP %d", response.StatusCode)
	}
	var body struct {
		Forwards []json.RawMessage `json:"forwards"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body); err != nil {
		return 0, err
	}
	return len(body.Forwards), nil
}

// Opener opens a URL in the user's browser. Notifier shows a desktop notification.
type (
	Opener   func(url string) error
	Notifier func(title, body string) error
)

// AutoOpen reacts to new approval requests: when no panel tab is connected it
// opens the panel and notifies the user, as the profile's settings say.
type AutoOpen struct {
	URL      func() string
	Settings func() PanelSettings
	Clients  func() int
	Open     Opener
	Notify   Notifier
	// Now and Cooldown are injected by tests; the cooldown keeps a burst of
	// requests from opening one browser tab per request.
	Now      func() time.Time
	Cooldown time.Duration
	OnError  func(error)

	mu       sync.Mutex
	lastOpen time.Time
}

// defaultAutoOpenCooldown bounds how often the panel is opened automatically.
const defaultAutoOpenCooldown = 10 * time.Second

// OnPending handles one new request. It is safe to call from any goroutine.
func (a *AutoOpen) OnPending(req ports.PendingRequest) {
	settings := a.Settings()
	if a.Clients() > 0 || (!settings.AutoOpen && !settings.Notify) {
		return
	}
	if settings.AutoOpen && a.claimOpen() {
		if err := a.Open(a.URL()); err != nil {
			a.report(fmt.Errorf("open panel: %w", err))
		}
	}
	if settings.Notify && a.Notify != nil {
		if err := a.Notify("agentport", requestSummary(req)); err != nil {
			a.report(fmt.Errorf("notify: %w", err))
		}
	}
}

// claimOpen reserves an automatic open unless one happened within the cooldown.
func (a *AutoOpen) claimOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	cooldown := a.Cooldown
	if cooldown <= 0 {
		cooldown = defaultAutoOpenCooldown
	}
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	if !a.lastOpen.IsZero() && now.Sub(a.lastOpen) < cooldown {
		return false
	}
	a.lastOpen = now
	return true
}

func (a *AutoOpen) report(err error) {
	if a.OnError != nil {
		a.OnError(err)
	}
}

// requestSummary is the notification text: who asks for which ports.
func requestSummary(req ports.PendingRequest) string {
	agent := req.Agent
	if agent == "" {
		agent = "an agent"
	}
	list := make([]string, 0, len(req.Ports))
	for _, p := range req.Ports {
		list = append(list, fmt.Sprintf(":%d", p.RemotePort))
	}
	return i18n.T("%s asks for port %s", agent, strings.Join(list, ", "))
}

// OpenURL opens url with the platform's opener: open, xdg-open or rundll32.
func OpenURL(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Start(); err != nil {
		return err
	}
	_ = command.Process.Release()
	return nil
}

// NotifyDesktop shows a system notification. It is best effort: on Windows
// nothing is shown, and on Linux a missing notify-send is not an error.
func NotifyDesktop(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %s with title %s", appleScriptString(body), appleScriptString(title))
		return runDetached("osascript", "-e", script)
	case "windows":
		return nil
	default:
		if _, err := exec.LookPath("notify-send"); err != nil {
			return nil
		}
		return runDetached("notify-send", title, body)
	}
}

func runDetached(name string, args ...string) error {
	command := exec.Command(name, args...)
	if err := command.Start(); err != nil {
		return err
	}
	_ = command.Process.Release()
	return nil
}

// appleScriptString quotes s for AppleScript, so a remote agent name cannot end the script.
func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return `"` + s + `"`
}

// panelLanguages are the languages the panel ships with, besides English.
var panelLanguages = []string{"pt-BR", "es", "zh-Hant"}

// panelCatalogs returns the panel catalogs by language, English excluded (its
// strings are the keys of the script).
func panelCatalogs() map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, language := range panelLanguages {
		payload, err := dashboardFiles.ReadFile("ui/assets/i18n/" + language + ".json")
		if err != nil {
			continue
		}
		catalog := map[string]string{}
		if err := json.Unmarshal(payload, &catalog); err != nil {
			continue
		}
		out[language] = catalog
	}
	return out
}

// CloseProfileForwards closes every forward of a running profile through its own
// Companion's control API. It returns how many ports were closed.
func CloseProfileForwards(profile string) (int, error) {
	state, err := LoadRuntime(profile)
	if err != nil || !RuntimeHealthy(state) {
		return 0, fmt.Errorf("profile %q is not running", profile)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+state.Address+"/v1/ports/forwards/close-all", strings.NewReader("{}"))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", "Bearer "+state.ControlToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("close-all returned HTTP %d", response.StatusCode)
	}
	var body struct {
		Closed []int `json:"closed"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body); err != nil {
		return 0, err
	}
	return len(body.Closed), nil
}
