package remote

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultRemotePort is the loopback port setup tries first on the server.
const DefaultRemotePort = 39123

// PortScanSpan bounds the search for a free port: the preferred one and the
// PortScanSpan-1 that follow it.
const PortScanSpan = 100

// ErrNoFreePort means every candidate port in the scan range is taken.
var ErrNoFreePort = errors.New("no free remote port found")

// ListeningPortsUnavailable marks the server output when none of ss, netstat or
// lsof exists there.
const listeningUnavailable = "agentport-listening=unavailable"

// listeningPortsScript prints the server's listening TCP sockets with whichever
// of ss (Linux), netstat (Linux, macOS, BSD) or lsof is installed. It always
// exits 0: an unreadable answer is "unknown", not an error.
const listeningPortsScript = `if command -v ss >/dev/null 2>&1; then ss -ltn 2>/dev/null; ` +
	`elif command -v netstat >/dev/null 2>&1; then netstat -an 2>/dev/null; ` +
	`elif command -v lsof >/dev/null 2>&1; then lsof -nP -iTCP -sTCP:LISTEN 2>/dev/null; ` +
	`else echo ` + listeningUnavailable + `; fi; true`

// localAddress matches the address:port of a socket in ss, netstat and lsof
// output: "127.0.0.1:39123", "*:22", "[::1]:39123", "::1.39123" (macOS netstat).
var localAddress = regexp.MustCompile(`^(?:\*|\[?[0-9A-Fa-f:.]*\]?)[:.](\d{1,5})$`)

// ParseListeningPorts reads the output of listeningPortsScript. known is false
// when the answer cannot be trusted (no tool on the server, or nothing parsed),
// so callers keep their port instead of acting on an empty list.
func ParseListeningPorts(output string) (listening map[int]bool, known bool) {
	listening = map[int]bool{}
	if strings.Contains(output, listeningUnavailable) {
		return listening, false
	}
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "LISTEN") {
			continue
		}
		for _, field := range strings.Fields(line) {
			match := localAddress.FindStringSubmatch(field)
			if match == nil {
				continue
			}
			port, err := strconv.Atoi(match[1])
			if err == nil && port > 0 && port <= 65535 {
				listening[port] = true
			}
			// Only the first address of the line is the local one.
			break
		}
	}
	return listening, len(listening) > 0
}

// ListeningPorts asks the server which TCP ports are listening. known is false
// when the server cannot say; an ssh failure is also reported as unknown, since
// the steps that follow fail on their own with a clearer message.
func ListeningPorts(ctx context.Context, destination, identityFile string) (listening map[int]bool, known bool) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	command := remoteSSHCommandContext(ctx, destination, identityFile, "sh -c "+shellQuote(listeningPortsScript))
	output, err := command.Output()
	if err != nil {
		return map[int]bool{}, false
	}
	return ParseListeningPorts(string(output))
}

// PickPort chooses the remote port from the server's listening set.
//
// preferred is kept when it is free. When it is busy: an explicit choice fails
// with a clear message, and otherwise the next free port above it is used,
// scanning at most PortScanSpan ports. avoid lists ports that must not be picked
// even if the server shows them free. With known false the server could not say,
// so preferred is kept (unless it is in avoid).
func PickPort(listening map[int]bool, known bool, preferred int, explicit bool, avoid ...int) (int, error) {
	if preferred < 1 || preferred > 65535 {
		return 0, fmt.Errorf("remote port must be between 1 and 65535, got %d", preferred)
	}
	avoided := func(port int) bool {
		for _, a := range avoid {
			if a == port {
				return true
			}
		}
		return false
	}
	if !avoided(preferred) && (!known || !listening[preferred]) {
		return preferred, nil
	}
	if explicit {
		return 0, fmt.Errorf("remote port %d is already in use on the server; choose another with --remote-port, or omit the flag to let agentport pick a free one", preferred)
	}
	for port := preferred + 1; port < preferred+PortScanSpan && port <= 65535; port++ {
		if !avoided(port) && !listening[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("%w: ports %d-%d are all in use on the server", ErrNoFreePort, preferred, min(preferred+PortScanSpan-1, 65535))
}

// ChoosePort is the port decision of setup and pair: it asks query for the
// server's listening ports and applies PickPort.
//
// A port that this profile used before may still be held by our own tunnel for a
// moment after the Companion stopped (the server notices the dead client only
// later), so a caller passes recheck > 0 for such a port: when it is busy the
// server is asked up to recheck more times, waiting between asks, before another
// port is chosen. wait may be nil.
func ChoosePort(query func() (map[int]bool, bool), preferred int, explicit bool, recheck int, wait func()) (int, error) {
	listening, known := query()
	for attempt := 0; attempt < recheck && known && listening[preferred]; attempt++ {
		if wait != nil {
			wait()
		}
		listening, known = query()
	}
	return PickPort(listening, known, preferred, explicit)
}
