package companion

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type trackedReadCloser struct {
	io.Reader
	closed bool
}

func (r *trackedReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestControlServerServesPrivateStatusViewAndStop(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("AGENTPORT_CONFIG_DIR", t.TempDir())
	stopped := make(chan struct{}, 1)
	control, err := StartControl("dev", func() any {
		return map[string]any{"profile": "dev", "tunnel": map[string]bool{"connected": true}}
	}, func() { stopped <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	state, err := LoadRuntime("dev")
	if err != nil {
		t.Fatal(err)
	}
	if !RuntimeHealthy(state) {
		t.Fatal("runtime health check failed")
	}
	payload, err := FetchRuntimeStatus(state)
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	if err := json.Unmarshal(payload, &status); err != nil {
		t.Fatal(err)
	}
	if status["profile"] != "dev" {
		t.Fatalf("status = %#v", status)
	}
	response, err := http.Get(ViewURL(state))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("view status = %d", response.StatusCode)
	}
	markup, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markup), "<title>agentport</title>") || !strings.Contains(string(markup), `src="`) {
		t.Fatal("panel markup missing")
	}
	if err := StopRuntime(state); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop callback was not called")
	}
}

func TestControlServerServesDashboardBrandAssets(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	control, err := StartControl("dev", func() any { return map[string]any{} }, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()

	for _, asset := range []struct {
		path        string
		contentType string
	}{
		{"assets/agentport-mark.svg", "image/svg+xml"},
		{"assets/favicon.svg", "image/svg+xml"},
		{"assets/styles.css", "text/css; charset=utf-8"},
		{"assets/app.js", "text/javascript; charset=utf-8"},
	} {
		response, err := http.Get(ViewURL(control.State) + asset.path)
		if err != nil {
			t.Fatalf("get %s: %v", asset.path, err)
		}
		data, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", asset.path, readErr)
		}
		if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != asset.contentType || len(data) == 0 {
			t.Fatalf("asset %s response = %d %q (%d bytes)", asset.path, response.StatusCode, response.Header.Get("Content-Type"), len(data))
		}
	}
}
