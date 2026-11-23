//go:build !linux && !darwin && !windows

package portscan

import "context"

// listen has no listener source on this platform (BSD, ...). Windows has one in netstat_windows.go.
func listen(context.Context) ([]Listener, error) {
	return nil, ErrUnsupported
}
