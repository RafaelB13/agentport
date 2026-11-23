//go:build windows

package portscan

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

// listen lists the listening TCP endpoints with netstat and names their owners with
// tasklist. Naming is best effort: if tasklist fails the ports are still listed.
func listen(ctx context.Context) ([]Listener, error) {
	v4, err := runHidden(ctx, "netstat", "-ano", "-p", "TCP")
	if err != nil {
		return nil, fmt.Errorf("portscan: running netstat: %w", err)
	}
	// IPv6 is optional: netstat can fail for TCPv6 on a host without IPv6.
	v6, _ := runHidden(ctx, "netstat", "-ano", "-p", "TCPv6")
	names, _ := runHidden(ctx, "tasklist", "/FO", "CSV", "/NH")
	eps := parseNetstat(bytes.Join([][]byte{v4, v6}, []byte("\n")))
	return listenersFromNetstat(eps, parseTasklistNames(names)), nil
}

// runHidden runs a system tool without opening a console window and returns its stdout.
func runHidden(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Output()
}
