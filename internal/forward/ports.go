package forward

import (
	"net"
	"strconv"
)

// suggestOffset is the port offset offered as an extra suggestion (want+10000).
const suggestOffset = 10000

// CanListen reports whether 127.0.0.1:port can be bound right now.
func CanListen(port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
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
