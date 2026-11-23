//go:build !linux && !darwin

package portscan

import "context"

// listen has no listener source on this platform (Windows, BSD, ...).
// TODO: add a Windows implementation based on GetExtendedTcpTable.
func listen(context.Context) ([]Listener, error) {
	return nil, ErrUnsupported
}
