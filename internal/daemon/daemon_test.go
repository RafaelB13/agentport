package daemon

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func send(t *testing.T, method, url, authorization, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func TestControlEndpointsRequireToken(t *testing.T) {
	d, err := Start("secret")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	url := "http://" + d.State.Address + "/v1/control/persistent-session"
	if response := send(t, http.MethodPost, url, "", `{"id":"companion:work","token":"t1"}`); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated control call: status %d, want 401", response.StatusCode)
	}
}

func TestPersistentSessionGrantsBridgeAccess(t *testing.T) {
	d, err := Start("secret")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	base := "http://" + d.State.Address
	register := base + "/v1/control/persistent-session"
	if response := send(t, http.MethodPost, register, "Bearer secret", `{"id":"","token":""}`); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty session: status %d, want 400", response.StatusCode)
	}
	if response := send(t, http.MethodPost, register, "Bearer secret", `{"id":"companion:work","token":"t1"}`); response.StatusCode != http.StatusOK {
		t.Fatalf("register: status %d, want 200", response.StatusCode)
	}
	if response := send(t, http.MethodGet, base+"/v1/status", "Bearer t1", ""); response.StatusCode != http.StatusOK {
		t.Fatalf("bridge status with registered token: %d", response.StatusCode)
	}
}

func TestReleaseControlPublishesStatusToBridge(t *testing.T) {
	d, err := Start("secret")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	base := "http://" + d.State.Address
	if response := send(t, http.MethodPost, base+"/v1/control/persistent-session", "Bearer secret", `{"id":"companion:work","token":"t1"}`); response.StatusCode != http.StatusOK {
		t.Fatalf("register: %d", response.StatusCode)
	}
	release := base + "/v1/control/release"
	if response := send(t, http.MethodPost, release, "Bearer secret", `{"current_version":"v1.0.0","latest_version":"v1.1.0","update_available":true}`); response.StatusCode != http.StatusOK {
		t.Fatalf("release publish: %d", response.StatusCode)
	}
	if response := send(t, http.MethodPost, release, "Bearer secret", `{"latest_version":"v1.1.0"}`); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("release without current_version: %d, want 400", response.StatusCode)
	}
	response := send(t, http.MethodGet, base+"/v1/status", "Bearer t1", "")
	var body struct {
		Release struct {
			LatestVersion string `json:"latest_version"`
		} `json:"release"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Release.LatestVersion != "v1.1.0" {
		t.Fatalf("published release not visible: %+v", body)
	}
}

func TestStateIsRemovedOnClose(t *testing.T) {
	d, err := Start("secret")
	if err != nil {
		t.Fatal(err)
	}
	state, err := LoadState()
	if err != nil || state.Address != d.State.Address || state.ControlToken != "secret" {
		t.Fatalf("state = %+v, err %v", state, err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(); err == nil {
		t.Fatal("state file survived Close")
	}
}
