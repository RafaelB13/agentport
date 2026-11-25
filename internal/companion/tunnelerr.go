package companion

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// failureKind says why a tunnel process ended, as far as its stderr tells.
type failureKind int

const (
	failureOther failureKind = iota
	// failureForwardRefused: the server refused the reverse forward, which is
	// what ssh reports when the remote port is already bound by someone else.
	failureForwardRefused
	// failureAuth: the server rejected the credentials.
	failureAuth
)

// tunnelDiagnosis is the reading of one failed tunnel run.
type tunnelDiagnosis struct {
	kind    failureKind
	port    int    // the remote port ssh named, or the profile's when it named none
	message string // short and human, for status and the log
}

var forwardRefusedPattern = regexp.MustCompile(`remote port forwarding failed(?: for listen port (\d+))?`)

// relevantMarkers are the words that make an ssh stderr line worth keeping. The
// tunnel runs at LogLevel=DEBUG1, so most lines are noise; an allowlist keeps the
// log and the status to what explains a failure.
var relevantMarkers = []string{
	"error", "warning", "failed", "denied", "refused", "timed out", "timeout", "could not",
	"unable", "no route", "unreachable", "closed by", "reset by", "broken pipe",
	"host key", "authentication", "not allowed", "disabled", "unknown host",
}

// relevantSSHLine reports whether a stderr line of the tunnel explains a failure.
func relevantSSHLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "debug") {
		return false
	}
	lower := strings.ToLower(line)
	if strings.Contains(lower, "permanently added") {
		return false
	}
	for _, marker := range relevantMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// diagnoseTunnelFailure turns the relevant stderr lines of a failed run (and
// its exit error) into a kind and a short message.
func diagnoseTunnelFailure(lines []string, exitErr error, profilePort int) tunnelDiagnosis {
	for _, line := range lines {
		if match := forwardRefusedPattern.FindStringSubmatch(line); match != nil {
			port := profilePort
			if match[1] != "" {
				if value, err := strconv.Atoi(match[1]); err == nil {
					port = value
				}
			}
			return tunnelDiagnosis{kind: failureForwardRefused, port: port,
				message: fmt.Sprintf("remote port %d is already in use on the server", port)}
		}
	}
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "permission denied") || strings.Contains(lower, "authentication failed") ||
			strings.Contains(lower, "too many authentication failures") || strings.Contains(lower, "no supported authentication methods") {
			return tunnelDiagnosis{kind: failureAuth, port: profilePort, message: "SSH authentication failed: " + cleanSSHLine(line)}
		}
	}
	if len(lines) > 0 {
		return tunnelDiagnosis{kind: failureOther, port: profilePort, message: cleanSSHLine(lines[len(lines)-1])}
	}
	if exitErr != nil {
		return tunnelDiagnosis{kind: failureOther, port: profilePort, message: exitErr.Error()}
	}
	return tunnelDiagnosis{kind: failureOther, port: profilePort}
}

func cleanSSHLine(line string) string {
	line = strings.TrimSpace(line)
	for _, prefix := range []string{"ssh: ", "Error: ", "Warning: "} {
		line = strings.TrimPrefix(line, prefix)
	}
	if len(line) > 200 {
		line = line[:200] + "…"
	}
	return line
}

// errorWatch collects the relevant stderr lines of one tunnel run, and hands
// each to onLine as it arrives. It keeps only the last few, so a chatty ssh
// cannot grow it.
type errorWatch struct {
	mu      sync.Mutex
	partial string
	lines   []string
	onLine  func(string)
}

const maxKeptSSHLines = 20

func (w *errorWatch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.partial += string(p)
	for {
		index := strings.IndexByte(w.partial, '\n')
		if index < 0 {
			break
		}
		w.add(w.partial[:index])
		w.partial = w.partial[index+1:]
	}
	if len(w.partial) > 4096 {
		w.partial = ""
	}
	return len(p), nil
}

func (w *errorWatch) add(line string) {
	line = strings.TrimSpace(line)
	if !relevantSSHLine(line) {
		return
	}
	if len(line) > 300 {
		line = line[:300] + "…"
	}
	if len(w.lines) == maxKeptSSHLines {
		w.lines = w.lines[1:]
	}
	w.lines = append(w.lines, line)
	if w.onLine != nil {
		w.onLine(line)
	}
}

// Lines returns what was kept, flushing a final line that had no newline.
func (w *errorWatch) Lines() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.TrimSpace(w.partial) != "" {
		w.add(w.partial)
		w.partial = ""
	}
	return append([]string(nil), w.lines...)
}
