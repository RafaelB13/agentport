package portscan

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// lsofProc is the process-level state of an lsof -F record.
type lsofProc struct {
	pid     int
	command string
	login   string
	uid     string
}

// listener builds a Listener for one socket of the process.
func (p lsofProc) listener(host string, port int, family string) Listener {
	v6 := strings.HasPrefix(family, "IPv6") || (family == "" && strings.Contains(host, ":"))
	bind := host
	if host == "" || host == "*" {
		// lsof prints "*" for the wildcard address (-n disables DNS names).
		bind = "0.0.0.0"
		if v6 {
			bind = "::"
		}
	}
	user := p.login
	if user == "" {
		user = p.uid
	}
	return Listener{Port: port, Bind: bind, PID: p.pid, Process: p.command, User: user}
}

// parseLsofF parses the output of `lsof -nP -iTCP -sTCP:LISTEN -F pcLutnf`.
//
// Each line is one field: a one-letter tag followed by the value. "p" starts a
// process record, "c" is its command, "L" its login name and "u" its uid. "f"
// starts a file descriptor, "t" is its family (IPv4/IPv6) and "n" its name
// ("host:port", possibly with a " (LISTEN)" suffix).
func parseLsofF(r io.Reader) ([]Listener, error) {
	var (
		out     []Listener
		proc    lsofProc
		family  string
		host    string
		port    int
		haveFD  bool // an "n" field is waiting to be emitted for the current fd
		sc      = bufio.NewScanner(r)
		flushFD = func() {
			if haveFD {
				out = append(out, proc.listener(host, port, family))
				haveFD = false
			}
		}
	)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		tag, val := line[0], line[1:]
		switch tag {
		case 'p':
			flushFD()
			pid, _ := strconv.Atoi(val)
			proc = lsofProc{pid: pid}
			family = ""
		case 'c':
			proc.command = val
		case 'L':
			proc.login = val
		case 'u':
			proc.uid = val
		case 'f':
			flushFD()
			family = ""
		case 't':
			family = val
		case 'n':
			h, p, ok := splitLsofName(val)
			if ok {
				host, port, haveFD = h, p, true
			}
		}
	}
	flushFD()
	return out, sc.Err()
}

// splitLsofName splits an lsof name field such as "127.0.0.1:3000",
// "*:5173", "[::1]:9000" or "*:8080 (LISTEN)".
func splitLsofName(name string) (host string, port int, ok bool) {
	name, _, _ = strings.Cut(name, " ")
	i := strings.LastIndexByte(name, ':')
	if i < 0 {
		return "", 0, false
	}
	port, err := strconv.Atoi(name[i+1:])
	if err != nil {
		return "", 0, false
	}
	host = strings.TrimSuffix(strings.TrimPrefix(name[:i], "["), "]")
	return host, port, true
}

// parseLsofCwd parses the output of `lsof -nP -a -d cwd -F pfn -p PIDS` and
// returns the working directory of each process that lsof could read.
func parseLsofCwd(r io.Reader) (map[int]string, error) {
	cwd := make(map[int]string)
	pid := 0
	fd := ""
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		tag, val := line[0], line[1:]
		switch tag {
		case 'p':
			pid, _ = strconv.Atoi(val)
		case 'f':
			fd = val
		case 'n':
			if fd == "cwd" && pid != 0 {
				cwd[pid] = val
			}
		}
	}
	return cwd, sc.Err()
}
