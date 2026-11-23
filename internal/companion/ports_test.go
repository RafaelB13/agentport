package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/ports"
	"github.com/rafaelb13/agentport/internal/portsproto"
)

func portProfile() Profile {
	return Profile{Name: "dev", Destination: "dev-host", RemotePort: 39123, Token: "token"}
}

func TestControlPathIsPrivateAndShort(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	path, err := ControlPath(strings.Repeat("p", 64))
	if err != nil {
		t.Fatal(err)
	}
	if len(path) > 100 {
		t.Fatalf("control socket path is %d bytes, over the unix limit", len(path))
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("socket directory mode = %o, want 700", info.Mode().Perm())
	}
	again, _ := ControlPath(strings.Repeat("p", 64))
	other, _ := ControlPath("other")
	if path != again || path == other {
		t.Fatal("control path must be stable per profile and distinct across profiles")
	}
	if _, err := ControlPath("../escape"); err == nil {
		t.Fatal("profile name with path separators accepted")
	}
}

func TestTunnelCommandIsAControlMasterOnTheProfileSocket(t *testing.T) {
	if !controlMasterSupported {
		t.Skip("OpenSSH on Windows has no unix-socket ControlMaster")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	command, err := TunnelCommand(portProfile(), 45678)
	if err != nil {
		t.Fatal(err)
	}
	controlPath, _ := ControlPath("dev")
	arguments := strings.Join(command.Args, " ")
	for _, expected := range []string{"ControlMaster=auto", "ControlPath=" + controlPath, "-R 127.0.0.1:39123:127.0.0.1:45678"} {
		if !strings.Contains(arguments, expected) {
			t.Errorf("tunnel %q does not contain %q", arguments, expected)
		}
	}
}

func TestWaitForSocketReturnsOnceTheSocketExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cm.sock")
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = os.WriteFile(path, nil, 0o600)
	}()
	if err := waitForSocket(context.Background(), path, 5*time.Second); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

func TestWaitForSocketTimesOutWithAClearMessage(t *testing.T) {
	err := waitForSocket(context.Background(), filepath.Join(t.TempDir(), "never"), 60*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "reconnect") {
		t.Fatalf("err = %v", err)
	}
}

func TestPortStateSurvivesARestart(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	// Persist one forward as the store would on a change.
	records := []ports.Record{{RemotePort: 5432, LocalPort: 5432, Origin: "manual", Pinned: true, CreatedAt: time.Now()}}
	stateFile, _ := portStatePath("dev")
	if err := writePrivateJSON(stateFile, "dev.ports-", records); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewPortManager(portProfile(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fw := restarted.Store.Forwards("")
	if len(fw) != 1 || fw[0].RemotePort != 5432 || fw[0].State != "reserved" {
		t.Fatalf("restored forwards = %+v, want one reserved 5432", fw)
	}
}

func TestPortProfileBlocksTheTunnelPort(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	manager, err := NewPortManager(portProfile(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := manager.Store.RegisterSession(testRegister())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := manager.Store.Request(cancelledContext(), sess.ID, testForward(39123))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Results[0].State != "denied" {
		t.Fatalf("the tunnel's own port was requested and %q, want denied", resp.Results[0].State)
	}
}

func TestPortAPIIsBehindTheControlToken(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	manager, err := NewPortManager(portProfile(), nil)
	if err != nil {
		t.Fatal(err)
	}
	control, err := StartControl("dev", func() any { return map[string]bool{} }, func() {}, manager.Handler)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()

	unauthenticated, err := http.Get("http://" + control.State.Address + "/v1/ports/state")
	if err != nil {
		t.Fatal(err)
	}
	unauthenticated.Body.Close()
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: status %d, want 401", unauthenticated.StatusCode)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	authReq, _ := http.NewRequest(http.MethodGet, "http://"+control.State.Address+"/v1/ports/state", nil)
	authReq.Header.Set("Authorization", "Bearer "+control.State.ControlToken)
	resp, err := client.Do(authReq)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || body["forwards"] == nil {
		t.Fatalf("with token: status %d body %v", resp.StatusCode, body)
	}
}

func testRegister() portsproto.RegisterSessionRequest {
	return portsproto.RegisterSessionRequest{V: 1, Agent: "claude-code", PID: 1, Cwd: "/w"}
}

func testForward(port int) portsproto.ForwardRequest {
	return portsproto.ForwardRequest{V: 1, Reason: "check", Ports: []portsproto.PortRequest{{RemotePort: port}}}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
