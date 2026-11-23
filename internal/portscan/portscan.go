package portscan

import (
	"context"
	"errors"
)

// ErrUnsupported is returned by Scan on platforms without a listener source.
var ErrUnsupported = errors.New("portscan: listing listening ports is not supported on this platform")

// Listener is one TCP port in the LISTEN state.
type Listener struct {
	Port    int    // listening TCP port
	Bind    string // "127.0.0.1", "0.0.0.0", "::", "::1"...
	PID     int    // 0 if unknown (other user's process)
	Process string // short name (comm), "" if unknown
	Cmdline string // full command line if readable
	User    string // username (or numeric uid when the name is unknown), "" if unknown
	Cwd     string // working directory if readable
	// Container is the docker container name when the process is docker-proxy
	// and it can be resolved cheaply. It is currently always empty.
	Container string
}

// Options narrows the result of Scan.
type Options struct {
	IncludePrivileged bool  // keep ports below 1024
	IncludeSystem     bool  // keep known system daemons (see filter.go)
	ExcludePorts      []int // ports to drop, e.g. the bridge port
}

// Scan returns the TCP ports in the LISTEN state, deduplicated by port and
// sorted by port. It returns ErrUnsupported on platforms without a listener
// source.
func Scan(ctx context.Context, opt Options) ([]Listener, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := listen(ctx)
	if err != nil {
		return nil, err
	}
	return normalize(raw, opt), nil
}
