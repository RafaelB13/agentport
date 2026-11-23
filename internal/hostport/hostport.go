// Package hostport reports which process owns a busy local TCP port on the
// host, and how to stop it. It is self-contained: standard library only, and
// no other package of this repo. It never escalates privileges.
package hostport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// Kind classifies who owns a port and what stopping it means.
type Kind string

const (
	// KindProc is a plain process of the current user; stopped with SIGTERM, then SIGKILL
	// (taskkill without /F, then with /F, on Windows).
	KindProc Kind = "proc"
	// KindService restarts by itself (brew services, launchd LaunchAgent, systemd --user).
	KindService Kind = "service"
	// KindContainer is a docker container publishing the port.
	KindContainer Kind = "container"
	// KindSystem belongs to another user or root and cannot be stopped here.
	KindSystem Kind = "system"
)

var (
	// ErrInvalidPort is returned for a port outside 1..65535.
	ErrInvalidPort = errors.New("invalid port")
	// ErrNotStoppable is returned by Stop for system owners and owners without a stop command.
	ErrNotStoppable = errors.New("owner cannot be stopped by agentport")
	// ErrStillBusy is returned by Stop when the port still accepts listeners afterwards.
	ErrStillBusy = errors.New("port still in use after stopping its owner")
	// ErrUnsupported is returned on platforms without an implementation.
	ErrUnsupported = errors.New("hostport: not supported on this platform")
)

// Owner describes the process (or service) listening on a local port.
// StopCmd is set for service and container owners. For KindProc, Stop uses
// signals instead and StopCmd stays nil.
type Owner struct {
	Port      int
	PID       int
	Process   string
	User      string
	Kind      Kind
	Manager   string
	Service   string
	Container string
	StopCmd   []string
}

// Lookup returns the listener on port, or nil, nil when the port is free.
func Lookup(ctx context.Context, port int) (*Owner, error) {
	if !supported(runtime.GOOS) {
		return nil, ErrUnsupported
	}
	return lookupWith(ctx, defaultEnv(), port)
}

// Stop stops the owner of a port and confirms the port is free afterwards.
func Stop(ctx context.Context, o *Owner) error {
	if !supported(runtime.GOOS) {
		return ErrUnsupported
	}
	return stopWith(ctx, defaultEnv(), o)
}

func supported(goos string) bool {
	return goos == "darwin" || goos == "linux" || goos == "windows"
}

// runner runs an external command and returns its stdout.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// signaler sends signals to pids. It is an interface so tests can fake it.
// On Windows the "signals" are taskkill requests; see signal_windows.go.
type signaler interface {
	Signal(pid int, sig syscall.Signal) error
	Alive(pid int) bool
}

// env holds everything Lookup and Stop touch outside the process, so tests can replace it.
type env struct {
	goos string
	uid  int    // numeric uid on Unix; unused on Windows
	user string // the current account on Windows ("DOMAIN\\name"), to tell our processes from others'
	run  runner
	sig  signaler
	// listening reports whether something still accepts connections on the port.
	listening func(port int) bool
	grace     time.Duration // how long a process gets to exit after SIGTERM, and after SIGKILL
	poll      time.Duration
}

func defaultEnv() env {
	e := env{
		goos:      runtime.GOOS,
		uid:       os.Getuid(),
		run:       execRun,
		sig:       newSignaler(execRun),
		listening: isListening,
		grace:     5 * time.Second,
		poll:      100 * time.Millisecond,
	}
	if runtime.GOOS == "windows" {
		e.user = currentUser()
	}
	return e
}

// currentUser returns the account name the Companion runs as, or "" if unknown.
func currentUser() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return u.Username
}

func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	hideConsole(cmd)
	return cmd.Output()
}

// isListening reports whether something accepts on 127.0.0.1:port.
func isListening(port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

func validPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: %d", ErrInvalidPort, port)
	}
	return nil
}
