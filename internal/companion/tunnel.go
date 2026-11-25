package companion

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// tunnelTiming shapes the reconnection: the wait starts at min, doubles after each
// exit up to max, and starts over once a tunnel has stayed up for stable. It is a
// variable only so tests do not have to wait.
//
// connect bounds how long a tunnel process may run without the server accepting
// the forward: ssh can hang before that (a stalled handshake, a half-open
// connection after the host wakes), and without a bound the loop would wait on
// it forever and never retry.
var tunnelTiming = struct{ min, max, stable, connect time.Duration }{time.Second, 30 * time.Second, time.Minute, 90 * time.Second}

// maxHealAttempts bounds how many times in a row the loop asks for another remote
// port without ever getting a connected tunnel.
const maxHealAttempts = 3

// PortHealer is called when the server refused the reverse forward on
// profile.RemotePort, which means another process holds that port there. It
// returns the profile to use next: a different RemotePort once it has been saved
// and the server's agents point at it. When the returned RemotePort differs from
// the old one it is used even if err is not nil (the port moved but a later step
// failed); err is logged and shown in the status.
type PortHealer func(ctx context.Context, profile Profile) (Profile, error)

type TunnelStatus struct {
	Connected bool      `json:"connected"`
	LastError string    `json:"last_error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TunnelCommand builds the long-running reverse SSH forward for a paired
// profile. It never binds a public remote interface.
func TunnelCommand(profile Profile, localPort int) (*exec.Cmd, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	if localPort < 1 || localPort > 65535 {
		return nil, fmt.Errorf("companion local port must be between 1 and 65535")
	}
	forward := fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", profile.RemotePort, localPort)
	arguments := []string{}
	if profile.SSHIdentityFile != "" {
		arguments = append(arguments, "-i", profile.SSHIdentityFile, "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes")
	}
	arguments = append(arguments,
		"-N",
		"-R", forward,
		"-o", "ExitOnForwardFailure=yes",
		// At this level ssh reports "remote forward success" once the server has
		// accepted the forward; the tunnel reports "connected" only then.
		"-o", "LogLevel=DEBUG1",
		"-o", "ConnectTimeout=20",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
	)
	// The tunnel process is also the ControlMaster: `ssh -O forward|cancel` adds
	// and removes port forwards over this socket without a second connection.
	// Windows OpenSSH has no unix-socket ControlMaster, so there the forwards
	// use one ssh process each instead (see NewPortManager).
	if controlMasterSupported {
		controlPath, err := ControlPath(profile.Name)
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, "-o", "ControlMaster=auto", "-o", "ControlPath="+controlPath)
	}
	arguments = append(arguments, profile.Destination)
	return exec.Command("ssh", arguments...), nil
}

// clearStaleControlSocket removes the ControlMaster socket left by a tunnel that
// was killed (SIGKILL does not let ssh remove it). Nothing else can own the socket
// when a new tunnel starts, so a leftover file is always stale. Keeping it would
// make waitForSocket return at once, before the new master exists, and the port
// forwards would fail with "Control socket connect". Errors are ignored: a missing
// socket is the normal case.
func clearStaleControlSocket(profile Profile) {
	if !controlMasterSupported {
		return
	}
	if path, err := ControlPath(profile.Name); err == nil {
		_ = os.Remove(path)
	}
}

// RunTunnel reconnects after unexpected SSH exits. Cancellation stops the
// current child process and terminates the retry loop.
func RunTunnel(ctx context.Context, profile Profile, localPort int) error {
	return RunTunnelWithStatus(ctx, profile, localPort, nil)
}

// RunTunnelWithStatus reconnects after unexpected SSH exits and reports the
// effective state to callers that render a dashboard.
func RunTunnelWithStatus(ctx context.Context, profile Profile, localPort int, observe func(TunnelStatus)) error {
	return runTunnelWithLogger(ctx, profile, localPort, observe, nil, nil)
}

func RunTunnelWithLogger(ctx context.Context, profile Profile, localPort int, observe func(TunnelStatus), logger *Logger) error {
	return runTunnelWithLogger(ctx, profile, localPort, observe, logger, nil)
}

// RunTunnelWithHooks is RunTunnelWithLogger plus onConnected, which runs each time
// a tunnel process has started. The ControlMaster socket may not exist yet at
// that moment, so the caller must wait for it before using it.
func RunTunnelWithHooks(ctx context.Context, profile Profile, localPort int, observe func(TunnelStatus), logger *Logger, onConnected func()) error {
	return runTunnelWithLogger(ctx, profile, localPort, observe, logger, onConnected)
}

// RunTunnelHealing is RunTunnelWithHooks plus heal, which picks another remote
// port when the server reports the current one as taken (see PortHealer). Other
// failures, such as authentication or a network that is down, never change the
// port. A nil heal behaves like RunTunnelWithHooks.
func RunTunnelHealing(ctx context.Context, profile Profile, localPort int, observe func(TunnelStatus), logger *Logger, onConnected func(), heal PortHealer) error {
	return runTunnel(ctx, profile, localPort, observe, logger, onConnected, heal)
}

func runTunnelWithLogger(ctx context.Context, profile Profile, localPort int, observe func(TunnelStatus), logger *Logger, onConnected func()) error {
	return runTunnel(ctx, profile, localPort, observe, logger, onConnected, nil)
}

func runTunnel(ctx context.Context, profile Profile, localPort int, observe func(TunnelStatus), logger *Logger, onConnected func(), heal PortHealer) error {
	notify := func(connected bool, message string) {
		if observe == nil {
			return
		}
		observe(TunnelStatus{Connected: connected, LastError: message, UpdatedAt: time.Now().UTC()})
	}
	delay := tunnelTiming.min
	healAttempts := 0
	for {
		command, err := TunnelCommand(profile, localPort)
		if err != nil {
			notify(false, err.Error())
			return err
		}
		forwardConfirmed := make(chan struct{})
		errLines := &errorWatch{onLine: func(line string) { logger.Info("SSH: %s", line) }}
		command.Stdout = loggerWriter{logger: logger}
		command.Stderr = io.MultiWriter(loggerWriter{logger: logger}, &forwardWatch{ready: forwardConfirmed}, errLines)
		clearStaleControlSocket(profile)
		if err := command.Start(); err != nil {
			err = fmt.Errorf("start companion SSH tunnel: %w", err)
			notify(false, err.Error())
			return err
		}
		logger.Debug("SSH tunnel started")
		startedAt := time.Now()
		// The process runs, but the server has not accepted the reverse forward yet:
		// that is "connecting", not "connected".
		notify(false, "")
		result := make(chan error, 1)
		go func() { result <- command.Wait() }()
		connectTimer := time.NewTimer(tunnelTiming.connect)
		timedOut := false
	waiting:
		for {
			select {
			case <-ctx.Done():
				connectTimer.Stop()
				_ = command.Process.Kill()
				<-result
				notify(false, "")
				return nil
			case <-forwardConfirmed:
				forwardConfirmed = nil
				connectTimer.Stop()
				healAttempts = 0
				notify(true, "")
				if onConnected != nil {
					onConnected()
				}
			case <-connectTimer.C:
				// Never confirmed and never exited: stop it so the loop can retry.
				timedOut = true
				_ = command.Process.Kill()
				err = <-result
				break waiting
			case err = <-result:
				connectTimer.Stop()
				break waiting
			}
		}
		diagnosis := diagnoseTunnelFailure(errLines.Lines(), err, profile.RemotePort)
		if timedOut {
			diagnosis = tunnelDiagnosis{kind: failureOther, port: profile.RemotePort,
				message: fmt.Sprintf("the SSH tunnel was not established within %s", tunnelTiming.connect)}
			err = errors.New(diagnosis.message)
		}
		message := diagnosis.message
		if message == "" && err != nil {
			message = err.Error()
		}
		if err != nil {
			logger.Error("SSH tunnel exited: %v (%s)", err, message)
		}
		oldPort := profile.RemotePort
		if diagnosis.kind == failureForwardRefused && heal != nil {
			if healAttempts >= maxHealAttempts {
				message += fmt.Sprintf(" (gave up choosing another port after %d attempts)", healAttempts)
				logger.Error("remote port %d is busy and %d automatic attempts did not help", oldPort, healAttempts)
			} else {
				healAttempts++
				logger.Info("remote port %d is already in use on the server; choosing another (attempt %d of %d)", oldPort, healAttempts, maxHealAttempts)
				next, healErr := heal(ctx, profile)
				if healErr != nil {
					logger.Error("choose another remote port: %v", healErr)
					message += ": " + healErr.Error()
				}
				if next.RemotePort != 0 && next.RemotePort != oldPort {
					profile = next
					logger.Info("remote port changed from %d to %d", oldPort, next.RemotePort)
					message = fmt.Sprintf("remote port %d was in use on the server; switched to %d", oldPort, next.RemotePort)
					if healErr != nil {
						message += ": " + healErr.Error()
					}
					delay = tunnelTiming.min
				}
			}
		}
		notify(false, message)
		// A tunnel that stayed up earns a fresh start: only repeated quick exits
		// should push the wait toward its maximum.
		if time.Since(startedAt) >= tunnelTiming.stable {
			delay = tunnelTiming.min
		}
		logger.Info("SSH tunnel reconnecting in %s", delay)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		delay = min(delay*2, tunnelTiming.max)
	}
}

type loggerWriter struct{ logger *Logger }

func (w loggerWriter) Write(p []byte) (int, error) {
	if w.logger != nil {
		for _, line := range strings.Split(strings.TrimRight(string(p), "\r\n"), "\n") {
			if line != "" {
				w.logger.Debug("SSH: %s", line)
			}
		}
	}
	return len(p), nil
}

// forwardSuccess is the line ssh writes at debug level once the server has
// accepted a -R forward.
const forwardSuccess = "remote forward success"

// forwardWatch reads ssh's stderr line by line and closes ready on the first line
// that reports the reverse forward. It keeps only an incomplete line in memory.
type forwardWatch struct {
	mu      sync.Mutex
	partial string
	ready   chan struct{}
	once    sync.Once
}

func (w *forwardWatch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.partial += string(p)
	for {
		index := strings.IndexByte(w.partial, '\n')
		if index < 0 {
			break
		}
		if strings.Contains(w.partial[:index], forwardSuccess) {
			w.once.Do(func() { close(w.ready) })
		}
		w.partial = w.partial[index+1:]
	}
	if len(w.partial) > 4096 {
		w.partial = ""
	}
	return len(p), nil
}
