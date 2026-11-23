package portscan

import (
	"context"
	"errors"
	"net"
	"testing"
)

// TestScanFindsLiveListener is a smoke test against the real OS: it opens a
// listener and checks that Scan reports its port.
func TestScanFindsLiveListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	got, err := Scan(context.Background(), Options{IncludePrivileged: true, IncludeSystem: true})
	if errors.Is(err, ErrUnsupported) {
		t.Skipf("unsupported platform: %v", err)
	}
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, l := range got {
		if l.Port == port {
			return
		}
	}
	t.Fatalf("port %d not found in %+v", port, got)
}
