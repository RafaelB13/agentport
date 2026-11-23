package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// tunnelLoop stands in for RunTunnelWithHooks: it returns when ctx is cancelled,
// with result as its error, and then reports it on the channel once.
func tunnelLoop(ctx context.Context, result error) <-chan error {
	out := make(chan error, 1)
	go func() {
		<-ctx.Done()
		out <- result
	}()
	return out
}

func waitWithin(t *testing.T, run func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- run() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("waitTunnelStop blocked: the tunnel result was read more than once or never")
		return nil
	}
}

func TestWaitTunnelStopReturnsAfterSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signals <- os.Interrupt
	tunnel := tunnelLoop(ctx, nil)
	if err := waitWithin(t, func() error { return waitTunnelStop(signals, make(chan struct{}), tunnel, cancel) }); err != nil {
		t.Fatalf("signal stop returned %v; want nil", err)
	}
	if ctx.Err() == nil {
		t.Fatal("the tunnel context was not cancelled")
	}
}

func TestWaitTunnelStopReturnsAfterControlStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := make(chan struct{}, 1)
	stop <- struct{}{}
	tunnel := tunnelLoop(ctx, errors.New("tunnel shutdown"))
	err := waitWithin(t, func() error { return waitTunnelStop(make(chan os.Signal), stop, tunnel, cancel) })
	if err == nil || err.Error() != "tunnel shutdown" {
		t.Fatalf("control stop returned %v; want the tunnel error", err)
	}
}

func TestWaitTunnelStopReturnsWhenTunnelEndsAlone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tunnel := make(chan error, 1)
	want := errors.New("ssh exited")
	tunnel <- want
	err := waitWithin(t, func() error { return waitTunnelStop(make(chan os.Signal), make(chan struct{}), tunnel, cancel) })
	if !errors.Is(err, want) {
		t.Fatalf("tunnel end returned %v; want %v", err, want)
	}
	if ctx.Err() == nil {
		t.Fatal("cancel was not called")
	}
}
