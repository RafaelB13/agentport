package portscan

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strconv"
	"strings"
)

// netstatEndpoint is one listening TCP endpoint from `netstat -ano`.
type netstatEndpoint struct {
	Port int
	Bind string
	PID  int
}

// parseNetstat parses `netstat -ano -p TCP` and `netstat -ano -p TCPv6` output
// (concatenated is fine). Rows look like
//
//	TCP    127.0.0.1:3000         0.0.0.0:0              LISTENING       12345
//	TCP    [::]:445               [::]:0                 LISTENING       4
//
// A row is a listener when its foreign endpoint has port 0. That test does not
// depend on the Windows display language, which localises the state column.
func parseNetstat(out []byte) []netstatEndpoint {
	var eps []netstatEndpoint
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 5 || fields[0] != "TCP" {
			continue // headers, blank lines and rows of other protocols
		}
		bind, port, ok := splitEndpoint(fields[1])
		if !ok || port == 0 {
			continue
		}
		_, foreign, ok := splitEndpoint(fields[2])
		if !ok || foreign != 0 {
			continue
		}
		pid, err := strconv.Atoi(fields[4])
		if err != nil {
			continue
		}
		eps = append(eps, netstatEndpoint{Port: port, Bind: bind, PID: pid})
	}
	return eps
}

// splitEndpoint splits "host:port" or "[v6host]:port" as netstat prints them.
// The bind address has its brackets removed: "[::1]:9000" gives "::1".
func splitEndpoint(s string) (host string, port int, ok bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return "", 0, false
	}
	port, err := strconv.Atoi(s[i+1:])
	if err != nil || port < 0 || port > 65535 {
		return "", 0, false
	}
	host = strings.TrimSuffix(strings.TrimPrefix(s[:i], "["), "]")
	return host, port, true
}

// parseTasklistNames reads `tasklist /FO CSV /NH` output and maps each PID to its
// image name without ".exe". Lines that are not CSV records (such as an INFO
// line) are skipped.
func parseTasklistNames(out []byte) map[int]string {
	names := map[int]string{}
	r := csv.NewReader(bytes.NewReader(out))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || len(rec) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(rec[1]))
		if err != nil {
			continue
		}
		names[pid] = trimExe(rec[0])
	}
	return names
}

// trimExe drops a case-insensitive ".exe" suffix, so "node.exe" reads "node" as
// it does on Linux and macOS.
func trimExe(image string) string {
	if n := len(image); n > 4 && strings.EqualFold(image[n-4:], ".exe") {
		return image[:n-4]
	}
	return image
}

// listenersFromNetstat builds one Listener per endpoint, named from names.
func listenersFromNetstat(eps []netstatEndpoint, names map[int]string) []Listener {
	out := make([]Listener, 0, len(eps))
	for _, ep := range eps {
		out = append(out, Listener{Port: ep.Port, Bind: ep.Bind, PID: ep.PID, Process: names[ep.PID]})
	}
	return out
}
