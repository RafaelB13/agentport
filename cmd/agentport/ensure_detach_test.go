package main

import (
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/companion"
	"github.com/rafaelb13/agentport/internal/daemon"
)

// killLeftovers ends the Companion and bridge a test started. Tests run the
// real binary, so a failed test must not leave a process behind.
func killLeftovers(t *testing.T, profile string) {
	t.Helper()
	t.Cleanup(func() {
		if state, err := companion.LoadRuntime(profile); err == nil && state.PID > 0 {
			killPID(state.PID)
		}
		if state, err := daemon.LoadState(); err == nil && state.PID > 0 {
			killPID(state.PID)
		}
	})
}

func killPID(pid int) {
	if process, err := os.FindProcess(pid); err == nil {
		_ = process.Kill()
	}
}

// TestEnsureReturnsWhenItsChildrenInheritNoPipe runs the built binary the way
// ssh's LocalCommand does: its stdout and stderr are a pipe that nobody reads or
// closes. The Companion and the bridge it starts must not keep that pipe, so
// ensure returns at once instead of waiting for them.
func TestEnsureReturnsWhenItsChildrenInheritNoPipe(t *testing.T) {
	isolateEnsure(t)
	saveEnsureProfile(t, "pipeline")
	killLeftovers(t, "pipeline")

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	command := exec.Command(binaryPath, "companion", "ensure", "pipeline")
	command.Env = append(os.Environ(), "AGENTPORT_LOG_LEVEL=debug")
	command.Stdout, command.Stderr = writer, writer
	started := time.Now()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	// Only the child keeps the write end now, exactly as a caller's pipe would.
	writer.Close()

	// ssh waits for end-of-file on the LocalCommand's pipes, not just for the
	// process to exit, so both must happen within the bound.
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	drained := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, reader)
		drained <- err
	}()
	deadline := time.After(2 * time.Second)
	var exitErr error
	exitDone, pipeDone := false, false
	for !exitDone || !pipeDone {
		select {
		case err := <-exited:
			exitErr, exitDone = err, true
		case <-drained:
			pipeDone = true
		case <-deadline:
			t.Fatalf("ensure within 2s: exited=%v, pipe closed=%v; a child still holds its stdout pipe", exitDone, pipeDone)
		}
	}
	if exitErr != nil {
		t.Fatalf("ensure failed: %v", exitErr)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("ensure took %v; it must return within 2s", elapsed)
	}

	// The Companion it started is healthy and was not tied to the caller's pipe.
	state, err := companion.LoadRuntime("pipeline")
	if err != nil || !companion.RuntimeHealthy(state) {
		t.Fatalf("the Companion ensure started is not healthy: %v", err)
	}
}
