//go:build !windows

package companion

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeTerminal stands in for ssh: a shell on the pseudo-terminal, with the same
// interface as the real command.
func fakeTerminal(script string) func() (*exec.Cmd, error) {
	return func() (*exec.Cmd, error) {
		return exec.Command("/bin/sh", "-c", script), nil
	}
}

func startTerminalControl(t *testing.T, terminal func() (*exec.Cmd, error)) *ControlServer {
	t.Helper()
	control, err := StartControlWith(ControlOptions{
		Profile:  "dev",
		Status:   func() any { return map[string]any{} },
		Stop:     func() {},
		Terminal: terminal,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.Close() })
	return control
}

func terminalURL(control *ControlServer, token string) string {
	return "ws://" + control.State.Address + "/view/" + token + "/api/terminal?cols=80&rows=24"
}

func panelOrigin(control *ControlServer) string { return "http://" + control.State.Address }

func dialTerminal(t *testing.T, control *ControlServer) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, terminalURL(control, control.State.ViewToken), &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {panelOrigin(control)}},
	})
	if err != nil {
		t.Fatalf("dial terminal: %v", err)
	}
	return conn
}

// readUntil reads output until it contains want.
func readUntil(t *testing.T, conn *websocket.Conn, want string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var seen strings.Builder
	for !strings.Contains(seen.String(), want) {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q, got %q: %v", want, seen.String(), err)
		}
		seen.Write(data)
	}
	return seen.String()
}

func TestTerminalRejectsForeignOriginAndWrongToken(t *testing.T) {
	control := startTerminalControl(t, fakeTerminal("exec cat"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A wrong token is not a route at all.
	_, response, err := websocket.Dial(ctx, terminalURL(control, "wrong-token"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {panelOrigin(control)}},
	})
	if err == nil {
		t.Fatal("a wrong token opened a terminal")
	}
	if response == nil || response.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong token: status %v, want 404", response)
	}

	// A page on another site is refused, even with the right token.
	for _, origin := range []string{"http://evil.example", "https://127.0.0.1:" + portOf(control), ""} {
		headers := http.Header{}
		if origin != "" {
			headers.Set("Origin", origin)
		}
		_, response, err := websocket.Dial(ctx, terminalURL(control, control.State.ViewToken), &websocket.DialOptions{HTTPHeader: headers})
		if err == nil {
			t.Fatalf("origin %q opened a terminal", origin)
		}
		if response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: status %v, want 403", origin, response)
		}
	}
}

func portOf(control *ControlServer) string {
	parts := strings.Split(control.State.Address, ":")
	return parts[len(parts)-1]
}

func TestTerminalRoundTripAndResize(t *testing.T) {
	control := startTerminalControl(t, fakeTerminal("read x; stty size; exec cat"))
	conn := dialTerminal(t, control)
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":100,"rows":30}`)); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("go\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, conn, "30 100")

	if err := conn.Write(ctx, websocket.MessageBinary, []byte("hello-terminal\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, conn, "hello-terminal")
}

// processAlive reports whether pid still exists. A reaped process reports false.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// shellPID reads the pid the fake shell prints first.
func shellPID(t *testing.T, conn *websocket.Conn) int {
	t.Helper()
	output := readUntil(t, conn, "\n")
	line := strings.TrimSpace(strings.SplitN(output, "\n", 2)[0])
	pid, err := strconv.Atoi(line)
	if err != nil {
		t.Fatalf("first output line %q is not a pid", line)
	}
	return pid
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("process %d is still running after the terminal closed", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestTerminalProcessIsKilledWhenTheSocketCloses(t *testing.T) {
	control := startTerminalControl(t, fakeTerminal("echo $$; exec cat"))
	conn := dialTerminal(t, control)
	pid := shellPID(t, conn)
	if !processAlive(pid) {
		t.Fatalf("process %d is not running", pid)
	}
	if err := conn.Close(websocket.StatusNormalClosure, "bye"); err != nil {
		t.Logf("close: %v", err)
	}
	waitGone(t, pid)
}

func TestTerminalProcessIsKilledWhenTheCompanionStops(t *testing.T) {
	control := startTerminalControl(t, fakeTerminal("echo $$; exec cat"))
	conn := dialTerminal(t, control)
	defer conn.CloseNow()
	pid := shellPID(t, conn)
	if err := control.Close(); err != nil {
		t.Fatal(err)
	}
	waitGone(t, pid)
}

func TestTerminalLimitsConcurrentTerminals(t *testing.T) {
	control := startTerminalControl(t, fakeTerminal("exec cat"))
	var open []*websocket.Conn
	defer func() {
		for _, conn := range open {
			conn.CloseNow()
		}
	}()
	for i := 0; i < maxTerminals; i++ {
		open = append(open, dialTerminal(t, control))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, response, err := websocket.Dial(ctx, terminalURL(control, control.State.ViewToken), &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {panelOrigin(control)}},
	})
	if err == nil {
		t.Fatal("a terminal beyond the limit opened")
	}
	if response == nil || response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status %v, want 429", response)
	}
}

func TestTerminalIsRefusedWithoutACommand(t *testing.T) {
	control := startTerminalControl(t, func() (*exec.Cmd, error) { return nil, errTerminalUnsupported })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, response, err := websocket.Dial(ctx, terminalURL(control, control.State.ViewToken), &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {panelOrigin(control)}},
	})
	if err == nil || response == nil || response.StatusCode != http.StatusNotImplemented {
		t.Fatalf("unsupported terminal: err %v, status %v, want 501", err, response)
	}
}

func TestTerminalStatusNeedsNoOriginAndReportsAvailability(t *testing.T) {
	control := startTerminalControl(t, fakeTerminal("exec cat"))
	response, err := http.Get("http://" + control.State.Address + "/view/" + control.State.ViewToken + "/api/terminal")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body struct {
		Available bool `json:"available"`
		Max       int  `json:"max"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !body.Available || body.Max != maxTerminals {
		t.Fatalf("status %d body %+v, want an available terminal with max %d", response.StatusCode, body, maxTerminals)
	}
}
