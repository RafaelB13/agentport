package companion

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// A tunnel process that runs but whose server never accepts the reverse forward
// is "connecting", not "connected", and the port layer is not told it is up.
func TestTunnelIsConnectingUntilTheReverseForwardIsConfirmed(t *testing.T) {
	fastTunnel(t, 5*time.Millisecond, 20*time.Millisecond, time.Hour)
	fakeSSH(t, 0, 0, 0)
	t.Setenv("AGENTPORT_TEST_FAKE_SSH_NO_CONFIRM", "1")
	log := &statusLog{}
	var mu sync.Mutex
	connected := 0
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunTunnelWithHooks(ctx, profileForTunnel(), 45678, log.observe, nil, func() {
			mu.Lock()
			connected++
			mu.Unlock()
		})
	}()
	waitFor(t, "a connecting report", func() bool { return len(log.snapshot()) >= 1 })
	time.Sleep(150 * time.Millisecond)
	for _, status := range log.snapshot() {
		if status.Connected || status.LastError != "" {
			t.Fatalf("status %+v before the forward was confirmed, want connecting", status)
		}
	}
	mu.Lock()
	hooked := connected
	mu.Unlock()
	if hooked != 0 {
		t.Fatalf("onConnected ran %d time(s) before the forward was confirmed", hooked)
	}
	cancel()
	<-done
}

// Once ssh reports the forward, the tunnel is connected and the hook runs.
func TestTunnelIsConnectedWhenTheServerAcceptsTheForward(t *testing.T) {
	fastTunnel(t, 5*time.Millisecond, 20*time.Millisecond, time.Hour)
	fakeSSH(t, 0, 0, 0)
	log := &statusLog{}
	hooks := make(chan struct{}, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunTunnelWithHooks(ctx, profileForTunnel(), 45678, log.observe, nil, func() { hooks <- struct{}{} })
	}()
	waitFor(t, "a connected report", func() bool {
		statuses := log.snapshot()
		return len(statuses) >= 2 && statuses[len(statuses)-1].Connected
	})
	select {
	case <-hooks:
	case <-time.After(5 * time.Second):
		t.Fatal("onConnected did not run after the forward was confirmed")
	}
	if statuses := log.snapshot(); statuses[0].Connected {
		t.Fatalf("the first report was %+v, want connecting", statuses[0])
	}
	cancel()
	<-done
}

func TestForwardWatchFindsTheConfirmationAcrossWrites(t *testing.T) {
	ready := make(chan struct{})
	watch := &forwardWatch{ready: ready}
	for _, chunk := range []string{"debug1: Ente", "ring x\ndebug1: remote for", "ward success for: listen 39123\n"} {
		if _, err := watch.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-ready:
	default:
		t.Fatal("the confirmation split across writes was not seen")
	}
}

func TestForwardWatchIgnoresOtherLines(t *testing.T) {
	ready := make(chan struct{})
	watch := &forwardWatch{ready: ready}
	_, _ = watch.Write([]byte(strings.Repeat("x", 5000) + "\ndebug1: Connection established\nError: remote port forwarding failed\n"))
	select {
	case <-ready:
		t.Fatal("a line that is not a confirmation closed the ready channel")
	default:
	}
}
