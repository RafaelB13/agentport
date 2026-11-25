package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/companion"
	"github.com/rafaelb13/agentport/internal/control"
	"github.com/rafaelb13/agentport/internal/daemon"
)

// fakeProcess is a Companion that stops after stopAfter requests or kills.
type fakeProcess struct {
	stopOnRequest bool
	stopOnKill    bool
	killErr       error
	alive         bool
	requests      int
	kills         int
}

func (f *fakeProcess) stopper() companionStopper {
	f.alive = true
	return companionStopper{
		request: func() error {
			f.requests++
			if f.stopOnRequest {
				f.alive = false
			}
			return nil
		},
		running: func() bool { return f.alive },
		kill: func() error {
			f.kills++
			if f.killErr != nil {
				return f.killErr
			}
			if f.stopOnKill {
				f.alive = false
			}
			return nil
		},
		grace:     50 * time.Millisecond,
		forceWait: 50 * time.Millisecond,
		poll:      time.Millisecond,
	}
}

func TestStopperStopsGracefullyWithoutKilling(t *testing.T) {
	f := &fakeProcess{stopOnRequest: true}
	forced, err := f.stopper().stop()
	if err != nil || forced || f.kills != 0 {
		t.Fatalf("forced=%v err=%v kills=%d, want a graceful stop", forced, err, f.kills)
	}
}

func TestStopperKillsAfterTheGraceTimeout(t *testing.T) {
	f := &fakeProcess{stopOnKill: true}
	forced, err := f.stopper().stop()
	if err != nil || !forced || f.kills != 1 || f.requests != 1 {
		t.Fatalf("forced=%v err=%v kills=%d requests=%d, want a forced stop", forced, err, f.kills, f.requests)
	}
}

func TestStopperReportsAFailedKill(t *testing.T) {
	f := &fakeProcess{killErr: errors.New("permission denied")}
	_, err := f.stopper().stop()
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v, want the kill error", err)
	}
}

func TestStopperReportsAProcessThatSurvivesTheKill(t *testing.T) {
	f := &fakeProcess{}
	forced, err := f.stopper().stop()
	if err == nil || !forced {
		t.Fatalf("forced=%v err=%v, want a failure after a forced stop", forced, err)
	}
}

func TestStopperDoesNotWaitOutTheGraceWhenTheRequestFails(t *testing.T) {
	f := &fakeProcess{stopOnKill: true}
	s := f.stopper()
	s.request = func() error { return errors.New("connection refused") }
	s.grace = time.Hour
	started := time.Now()
	forced, err := s.stop()
	if err != nil || !forced || time.Since(started) > 5*time.Second {
		t.Fatalf("forced=%v err=%v after %v", forced, err, time.Since(started))
	}
}

// stopStandaloneBridgeAtCleanup kills a bridge a test started itself, if it is still up.
func stopStandaloneBridgeAtCleanup(t *testing.T, process *exec.Cmd) {
	t.Helper()
	t.Cleanup(func() {
		if process.Process != nil {
			_ = process.Process.Kill()
		}
		_, _ = process.Process.Wait()
	})
}

// TestCompanionStopEndsTheCompanionAndItsBridge runs the built binary: ensure
// starts a Companion and its bridge, stop must end both.
func TestCompanionStopEndsTheCompanionAndItsBridge(t *testing.T) {
	isolateEnsure(t)
	saveEnsureProfile(t, "halting")
	killLeftovers(t, "halting")

	if result := runCLI(t, "companion", "ensure", "halting"); result.exitCode != 0 {
		t.Fatalf("ensure = %+v", result)
	}
	if !bridgeHealthy(t) {
		t.Fatal("ensure did not start the bridge")
	}
	// A bridge still in use refuses to stop.
	if result := runCLI(t, "bridge", "stop"); result.exitCode == 0 || !strings.Contains(result.stderr, "still use the bridge") {
		t.Fatalf("bridge stop while a Companion runs = %+v", result)
	}

	started := time.Now()
	result := runCLI(t, "companion", "stop", "halting")
	if result.exitCode != 0 || !strings.Contains(result.stdout, "stopped") {
		t.Fatalf("companion stop = %+v", result)
	}
	if elapsed := time.Since(started); elapsed > companionStopTimeout+companionKillTimeout {
		t.Fatalf("companion stop took %v", elapsed)
	}
	if state, err := companion.LoadRuntime("halting"); err == nil && companion.RuntimeHealthy(state) {
		t.Fatal("the Companion still answers after stop")
	}
	// The bridge is not stopped by the stop request itself: the Companion process
	// shuts it down as it exits, so it can still answer for a moment after the
	// Companion has gone. Wait for it within the bridge's own stop budget.
	deadline := time.Now().Add(companionStopTimeout + companionKillTimeout)
	for bridgeHealthy(t) {
		if time.Now().After(deadline) {
			t.Fatal("the bridge is still running after the last Companion stopped")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestBridgeStopEndsAStandaloneBridge(t *testing.T) {
	isolateEnsure(t)
	if result := runCLI(t, "bridge", "stop"); result.exitCode == 0 || !strings.Contains(result.stderr, "not running") {
		t.Fatalf("bridge stop with no bridge = %+v", result)
	}

	bridge := exec.Command(binaryPath, "bridge")
	bridge.Stdin = strings.NewReader(`{"control_token":"test-token-1234567890"}`)
	bridge.Stdout, bridge.Stderr = os.Stderr, os.Stderr
	if err := bridge.Start(); err != nil {
		t.Fatal(err)
	}
	stopStandaloneBridgeAtCleanup(t, bridge)
	deadline := time.Now().Add(5 * time.Second)
	for !bridgeHealthy(t) {
		if time.Now().After(deadline) {
			t.Fatal("the standalone bridge did not become healthy")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if result := runCLI(t, "bridge", "stop"); result.exitCode != 0 {
		t.Fatalf("bridge stop = %+v", result)
	}
	exited := make(chan error, 1)
	go func() { exited <- bridge.Wait() }()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the bridge process did not exit after bridge stop")
	}
	if bridgeHealthy(t) {
		t.Fatal("the bridge still answers after bridge stop")
	}
}

func bridgeHealthy(t *testing.T) bool {
	t.Helper()
	state, err := daemon.LoadState()
	return err == nil && control.Healthy(state)
}
