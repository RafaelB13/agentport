package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"sync"
	"time"

	"github.com/rafaelb13/agentport/internal/ports"
)

// detectTTL is how long a server scan stays fresh. The panel asks for a new scan
// when it opens the Detected tab or the port list, so the cache only absorbs the
// repeated requests of one screen.
const detectTTL = 3 * time.Second

// detectTimeout bounds one scan over SSH.
const detectTimeout = 15 * time.Second

// remoteScanCommand is what the server runs to list its listening ports.
const remoteScanCommand = `sh -lc 'PATH="$HOME/.local/bin:$PATH" agentport ports --json'`

// Runner runs an external command and returns its standard output.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// RemoteDetector lists the server's listening ports. It runs
// `agentport ports --json` on the server over the profile's ControlMaster, so it
// needs no connection of its own, and keeps the last answer for detectTTL.
type RemoteDetector struct {
	Destination  string
	ControlPath  string // the ControlMaster socket; empty opens a connection of its own
	ExcludePorts []int  // ports that are never listed, such as the reverse tunnel's
	Run          Runner // nil runs ssh
	TTL          time.Duration
	Now          func() time.Time

	mu       sync.Mutex
	cached   []ports.DetectedPort
	cachedAt time.Time
	hasCache bool
}

// Detect returns the listening ports of the server. refresh skips the cache.
func (d *RemoteDetector) Detect(ctx context.Context, refresh bool) ([]ports.DetectedPort, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	ttl := detectTTL
	if d.TTL > 0 {
		ttl = d.TTL
	}
	if !refresh && d.hasCache && now().Sub(d.cachedAt) < ttl {
		return slices.Clone(d.cached), nil
	}
	listed, err := d.scan(ctx)
	if err != nil {
		return nil, err
	}
	d.cached, d.cachedAt, d.hasCache = listed, now(), true
	return slices.Clone(listed), nil
}

func (d *RemoteDetector) scan(ctx context.Context) ([]ports.DetectedPort, error) {
	ctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()
	run := d.Run
	if run == nil {
		run = runCommand
	}
	arguments := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}
	if d.ControlPath != "" {
		arguments = append(arguments, "-S", d.ControlPath, "-o", "ControlMaster=no")
	}
	arguments = append(arguments, d.Destination, remoteScanCommand)
	output, err := run(ctx, "ssh", arguments...)
	if err != nil {
		return nil, fmt.Errorf("scan the server over SSH: %w", err)
	}
	return parseRemotePorts(output, d.ExcludePorts)
}

// remoteListener mirrors the entries of `agentport ports --json` on the server.
type remoteListener struct {
	Port    int    `json:"port"`
	Bind    string `json:"bind"`
	PID     int    `json:"pid"`
	Process string `json:"process"`
	Cmdline string `json:"cmdline"`
	User    string `json:"user"`
	Cwd     string `json:"cwd"`
}

// parseRemotePorts reads the output of `agentport ports --json` and drops the
// excluded ports. The result is sorted by port, as the server sends it.
func parseRemotePorts(output []byte, exclude []int) ([]ports.DetectedPort, error) {
	var body struct {
		Ports []remoteListener `json:"ports"`
	}
	if err := json.Unmarshal(output, &body); err != nil {
		return nil, fmt.Errorf("read the server's port list: %w", err)
	}
	if body.Ports == nil {
		return nil, errors.New("read the server's port list: no ports field")
	}
	listed := make([]ports.DetectedPort, 0, len(body.Ports))
	for _, l := range body.Ports {
		if l.Port < 1 || l.Port > 65535 || slices.Contains(exclude, l.Port) {
			continue
		}
		listed = append(listed, ports.DetectedPort{Port: l.Port, Bind: l.Bind, PID: l.PID, Process: l.Process,
			Cmdline: l.Cmdline, User: l.User, Cwd: l.Cwd})
	}
	slices.SortFunc(listed, func(a, b ports.DetectedPort) int { return a.Port - b.Port })
	return listed, nil
}
