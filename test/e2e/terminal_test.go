//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rafaelb13/agentport/internal/companion"
)

// TestServerTerminalAgainstSSHD opens the panel's terminal endpoint against the
// fixture's sshd, runs a command in the shell, and checks that closing the
// terminal leaves no ssh process behind.
func TestServerTerminalAgainstSSHD(t *testing.T) {
	f := fixtureFor(t)
	t.Setenv("AGENTPORT_CONFIG_DIR", t.TempDir())
	control, err := companion.StartControlWith(companion.ControlOptions{
		Profile: "e2e",
		Status:  func() any { return map[string]any{} },
		Stop:    func() {},
		Terminal: func() (*exec.Cmd, error) {
			return exec.Command(f.sshPath, "-F", f.sshConfig, "-tt", "-S", f.ctl, "-o", "ControlMaster=no", hostAlias), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url := "ws://" + control.State.Address + "/view/" + control.State.ViewToken + "/api/terminal?cols=120&rows=40"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://" + control.State.Address}}})
	if err != nil {
		t.Fatalf("dial terminal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("echo agentport-$((40+2))\n")); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	for !strings.Contains(seen.String(), "agentport-42") {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for agentport-42, got %q: %v", seen.String(), err)
		}
		seen.Write(data)
	}
	if err := conn.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Logf("close: %v", err)
	}

	// The terminal's ssh is the one started with -tt on this fixture's socket.
	deadline := time.Now().Add(10 * time.Second)
	for {
		out, _ := run("ps", "-axww", "-o", "command=")
		left := 0
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, f.ctl) && strings.Contains(line, " -tt ") {
				left++
			}
		}
		if left == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d terminal ssh process(es) left after close", left)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
