package forward

import (
	"net"
	"strconv"
	"time"
)

// suggestOffset is the port offset offered as an extra suggestion (want+10000).
const suggestOffset = 10000

// loopbackProbeTimeout bounds the connect that asks whether a loopback port is served.
const loopbackProbeTimeout = 200 * time.Millisecond

// CanListen reports whether port is free on every local address a forward
// could collide with: 127.0.0.1, 0.0.0.0 and, when the host has IPv6, ::1 and ::.
//
// Binding alone is not enough. On macOS and the BSDs a bind to 0.0.0.0 or
// 127.0.0.1 can succeed while another process owns *:port, so the forward would
// quietly share the port. A connect on loopback catches those listeners.
func CanListen(port int) bool {
	if acceptsOnLoopback(port) {
		return false
	}
	for _, host := range localHosts() {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return false
		}
		_ = ln.Close()
	}
	return true
}

// acceptsOnLoopback reports whether something accepts TCP connections on
// 127.0.0.1 or ::1 at port. A refused connect means nothing is listening there.
func acceptsOnLoopback(port int) bool {
	for _, host := range []string{"127.0.0.1", "::1"} {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), loopbackProbeTimeout)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

// localHosts lists the addresses CanListen must bind. The IPv6 pair is included
// only when the host can bind ::1 at all.
func localHosts() []string {
	hosts := []string{"127.0.0.1", "0.0.0.0"}
	if ln, err := net.Listen("tcp", "[::1]:0"); err == nil {
		_ = ln.Close()
		hosts = append(hosts, "::1", "::")
	}
	return hosts
}

func defaultBusy(port int) bool { return !CanListen(port) }

// AllocLocal returns the first free port in want, want+1, ... up to 65535.
// A nil busy uses CanListen to decide.
func AllocLocal(want int, busy func(int) bool) (int, error) {
	if err := validPort(want); err != nil {
		return 0, err
	}
	if busy == nil {
		busy = defaultBusy
	}
	for p := want; p <= 65535; p++ {
		if !busy(p) {
			return p, nil
		}
	}
	return 0, ErrNoFreePort
}

// Suggest returns up to n free ports starting at want, plus want+10000 when it
// is a valid, free port not already in the list. A nil busy uses CanListen.
func Suggest(want, n int, busy func(int) bool) []int {
	if validPort(want) != nil {
		return nil
	}
	if busy == nil {
		busy = defaultBusy
	}
	out := []int{}
	for p := want; p <= 65535 && len(out) < n; p++ {
		if !busy(p) {
			out = append(out, p)
		}
	}
	extra := want + suggestOffset
	if validPort(extra) == nil && !contains(out, extra) && !busy(extra) {
		out = append(out, extra)
	}
	return out
}

func contains(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
