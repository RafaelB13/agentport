package companion

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/testenv"
)

func TestDiagnoseTunnelFailure(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		exit     error
		wantKind failureKind
		wantPort int
		wantMsg  string
	}{
		{"forward refused names its port", []string{"Error: remote port forwarding failed for listen port 40000"}, errors.New("exit status 255"), failureForwardRefused, 40000, "remote port 40000 is already in use on the server"},
		{"warning variant", []string{"Warning: remote port forwarding failed for listen port 39123"}, nil, failureForwardRefused, 39123, "remote port 39123 is already in use on the server"},
		{"refused without a port uses the profile's", []string{"remote port forwarding failed"}, nil, failureForwardRefused, 39123, "remote port 39123 is already in use on the server"},
		{"authentication", []string{"user@host: Permission denied (publickey)."}, errors.New("exit status 255"), failureAuth, 39123, "SSH authentication failed: user@host: Permission denied (publickey)."},
		{"network down keeps the ssh line", []string{"ssh: Could not resolve hostname devbox: nodename nor servname provided"}, errors.New("exit status 255"), failureOther, 39123, "Could not resolve hostname devbox: nodename nor servname provided"},
		{"no lines falls back to the exit error", nil, errors.New("exit status 255"), failureOther, 39123, "exit status 255"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := diagnoseTunnelFailure(test.lines, test.exit, 39123)
			if got.kind != test.wantKind || got.port != test.wantPort || got.message != test.wantMsg {
				t.Fatalf("diagnosis = %+v, want kind %v port %d message %q", got, test.wantKind, test.wantPort, test.wantMsg)
			}
		})
	}
}

func TestErrorWatchKeepsOnlyRelevantLines(t *testing.T) {
	var logged []string
	watch := &errorWatch{onLine: func(line string) { logged = append(logged, line) }}
	input := "debug1: Connection established\ndebug1: Authenticated to host\nWarning: Permanently added 'x' to known hosts.\n" +
		"Error: remote port forwarding fai"
	_, _ = watch.Write([]byte(input))
	_, _ = watch.Write([]byte("led for listen port 39123\ndebug1: channel 0: free\nlast failed line"))
	lines := watch.Lines()
	want := []string{"Error: remote port forwarding failed for listen port 39123", "last failed line"}
	if strings.Join(lines, "|") != strings.Join(want, "|") || strings.Join(logged, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, logged = %q, want %q", lines, logged, want)
	}
}

type healRecorder struct {
	mu       sync.Mutex
	profiles []Profile
	next     int
	err      error
}

func (h *healRecorder) heal(_ context.Context, profile Profile) (Profile, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.profiles = append(h.profiles, profile)
	if h.next == 0 {
		return profile, h.err
	}
	profile.RemotePort = h.next
	return profile, h.err
}

func (h *healRecorder) calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.profiles)
}

func readArgs(t *testing.T, ssh fakeTunnel) []string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(ssh.dir, "args"))
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func runHealing(t *testing.T, heal PortHealer, log *statusLog) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunTunnelHealing(ctx, profileForTunnel(), 45678, log.observe, nil, nil, heal) }()
	return func() {
		cancel()
		<-done
	}
}

// A refused forward picks a new port once, and the reconnect uses it.
func TestTunnelSelfHealsWhenTheForwardIsRefused(t *testing.T) {
	fastTunnel(t, 5*time.Millisecond, 20*time.Millisecond, time.Hour)
	ssh := fakeSSH(t, 1, 0, 0)
	healer := &healRecorder{next: 39124}
	log := &statusLog{}
	defer runHealing(t, healer.heal, log)()

	waitFor(t, "a connected report on the new port", func() bool {
		statuses := log.snapshot()
		return len(statuses) > 0 && statuses[len(statuses)-1].Connected
	})
	if healer.calls() != 1 || healer.profiles[0].RemotePort != 39123 {
		t.Fatalf("heal calls = %+v, want one call for port 39123", healer.profiles)
	}
	args := readArgs(t, ssh)
	if len(args) != 2 || !strings.Contains(args[0], "127.0.0.1:39123:") || !strings.Contains(args[1], "127.0.0.1:39124:") {
		t.Fatalf("ssh args = %q, want the first start on 39123 and the second on 39124", args)
	}
	var switched bool
	for _, status := range log.snapshot() {
		if strings.Contains(status.LastError, "switched to 39124") {
			switched = true
		}
	}
	if !switched {
		t.Errorf("no status told about the switch: %+v", log.snapshot())
	}
}

// Failures that are not a refused forward never move the port.
func TestTunnelDoesNotChangePortForOtherErrors(t *testing.T) {
	fastTunnel(t, 5*time.Millisecond, 20*time.Millisecond, time.Hour)
	ssh := fakeSSH(t, 3, 0, 0)
	t.Setenv("AGENTPORT_TEST_FAKE_SSH_FAIL_MSG", "user@host: Permission denied (publickey).")
	healer := &healRecorder{next: 39124}
	log := &statusLog{}
	defer runHealing(t, healer.heal, log)()

	waitFor(t, "three failed starts", func() bool { return ssh.starts() >= 3 })
	if healer.calls() != 0 {
		t.Fatalf("heal was called %d times for an authentication error", healer.calls())
	}
	for _, line := range readArgs(t, ssh) {
		if strings.Contains(line, ":39124:") {
			t.Fatalf("the port changed after an authentication error: %q", line)
		}
	}
	var told bool
	for _, status := range log.snapshot() {
		if strings.HasPrefix(status.LastError, "SSH authentication failed") {
			told = true
		}
	}
	if !told {
		t.Errorf("status never carried the authentication message: %+v", log.snapshot())
	}
}

// A heal that cannot help is retried a bounded number of times, then left alone.
func TestTunnelStopsHealingAfterBoundedAttempts(t *testing.T) {
	fastTunnel(t, 5*time.Millisecond, 20*time.Millisecond, time.Hour)
	ssh := fakeSSH(t, 100, 0, 0)
	healer := &healRecorder{err: errors.New("no free port")}
	log := &statusLog{}
	defer runHealing(t, healer.heal, log)()

	waitFor(t, "more starts than heal attempts", func() bool { return ssh.starts() >= maxHealAttempts+3 })
	if healer.calls() != maxHealAttempts {
		t.Fatalf("heal was called %d times, want %d", healer.calls(), maxHealAttempts)
	}
}

// A tunnel process that never confirms and never exits is stopped and retried.
func TestTunnelRetriesAProcessThatNeverConnects(t *testing.T) {
	testenv.IsolateUserDirs(t)
	fastTunnel(t, 5*time.Millisecond, 20*time.Millisecond, time.Hour)
	tunnelTiming.connect = 200 * time.Millisecond
	ssh := fakeSSH(t, 0, 0, 0)
	t.Setenv("AGENTPORT_TEST_FAKE_SSH_NO_CONFIRM", "1")
	log := &statusLog{}
	defer runHealing(t, nil, log)()

	waitFor(t, "a second start after the first hung", func() bool { return ssh.starts() >= 2 })
	var told bool
	for _, status := range log.snapshot() {
		if strings.Contains(status.LastError, "not established within") {
			told = true
		}
	}
	if !told {
		t.Errorf("status never reported the timeout: %+v", log.snapshot())
	}
}
