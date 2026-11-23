package forward

import (
	"net"
	"testing"
)

// listenOrSkip opens a listener on addr and returns it with its port.
func listenOrSkip(t *testing.T, addr string) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("cannot listen on %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln, ln.Addr().(*net.TCPAddr).Port
}

// assertBusy checks that a listener on another address holding port makes
// CanListen and AllocLocal treat it as taken.
func assertBusy(t *testing.T, port int) {
	t.Helper()
	if CanListen(port) {
		t.Fatalf("CanListen(%d) = true while a wildcard/other listener holds it", port)
	}
	got, err := AllocLocal(port, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == port {
		t.Fatalf("AllocLocal(%d) = %d; must skip the busy port", port, got)
	}
}

func TestCanListenBusyOnIPv4Wildcard(t *testing.T) {
	_, port := listenOrSkip(t, "0.0.0.0:0")
	assertBusy(t, port)
}

func TestCanListenBusyOnIPv6Wildcard(t *testing.T) {
	_, port := listenOrSkip(t, "[::]:0")
	assertBusy(t, port)
}

func TestCanListenBusyOnIPv6Loopback(t *testing.T) {
	_, port := listenOrSkip(t, "[::1]:0")
	assertBusy(t, port)
}

func TestSuggestSkipsWildcardListener(t *testing.T) {
	_, port := listenOrSkip(t, "0.0.0.0:0")
	for _, p := range Suggest(port, 1, nil) {
		if p == port {
			t.Fatalf("Suggest returned the busy wildcard port %d", port)
		}
	}
}
