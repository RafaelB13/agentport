package hostport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"syscall"
)

// This file holds the Windows lookup and the taskkill arguments. It is not
// build-tagged, so the parsers and the classification run in tests on any OS.

// winEndpoint is one listening TCP endpoint from `netstat -ano`.
type winEndpoint struct {
	Port int
	PID  int
}

// parseNetstatListeners reads `netstat -ano -p TCP` and `-p TCPv6` output. A row
// is a listener when its foreign endpoint has port 0, which does not depend on the
// display language (the state column is localised).
func parseNetstatListeners(out []byte) []winEndpoint {
	var eps []winEndpoint
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 5 || f[0] != "TCP" {
			continue
		}
		port, ok := endpointPort(f[1])
		if !ok || port == 0 {
			continue
		}
		if foreign, ok := endpointPort(f[2]); !ok || foreign != 0 {
			continue
		}
		pid, err := strconv.Atoi(f[4])
		if err != nil {
			continue
		}
		eps = append(eps, winEndpoint{Port: port, PID: pid})
	}
	return eps
}

// endpointPort returns the port of "host:port" or "[v6]:port".
func endpointPort(s string) (int, bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return 0, false
	}
	port, err := strconv.Atoi(s[i+1:])
	if err != nil || port < 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

// winTask is one process from `tasklist /V /FO CSV /NH`.
type winTask struct {
	Image string
	User  string
}

// parseTasklistVerbose maps PID to image and account. In /V output the columns
// are Image Name, PID, Session Name, Session#, Mem Usage, Status, User Name, ...
// Rows that do not parse (such as an INFO line) are skipped.
func parseTasklistVerbose(out []byte) map[int]winTask {
	tasks := map[int]winTask{}
	for _, rec := range csvRecords(out) {
		if len(rec) < 7 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(rec[1]))
		if err != nil {
			continue
		}
		tasks[pid] = winTask{Image: rec[0], User: rec[6]}
	}
	return tasks
}

// parseTasklistServices reads `tasklist /svc /FO CSV /NH` and maps each PID that
// hosts Windows services to their names. The Services column is a comma-separated
// list, or "N/A" for a process with none.
func parseTasklistServices(out []byte) map[int][]string {
	services := map[int][]string{}
	for _, rec := range csvRecords(out) {
		if len(rec) < 3 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(rec[1]))
		if err != nil {
			continue
		}
		for _, name := range strings.Split(rec[2], ",") {
			if name = strings.TrimSpace(name); name != "" && name != "N/A" {
				services[pid] = append(services[pid], name)
			}
		}
	}
	return services
}

// csvRecords parses CSV output leniently. Lines that are not CSV records are
// returned as they are; callers reject them by field count or PID.
func csvRecords(out []byte) [][]string {
	var recs [][]string
	r := csv.NewReader(bytes.NewReader(out))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return recs
		}
		if err != nil {
			continue
		}
		recs = append(recs, rec)
	}
}

// trimExe drops a case-insensitive ".exe" suffix so names match the Unix ones ("node").
func trimExe(image string) string {
	if n := len(image); n > 4 && strings.EqualFold(image[n-4:], ".exe") {
		return image[:n-4]
	}
	return image
}

// sameAccount reports whether a tasklist account is the Companion's own. An
// unknown account on either side is not the same, so the owner stays unstoppable.
func sameAccount(owner, self string) bool {
	return owner != "" && self != "" && strings.EqualFold(owner, self)
}

// lookupWindows classifies the listener on port on Windows. Kind rules, in order:
//  1. pid 0 (Idle) or 4 (System), or an account other than ours → system
//  2. docker-proxy / vpnkit / com.docker.* → container, stopped with docker stop
//  3. a process hosting a Windows service → service, never stopped by agentport
//  4. otherwise → proc, stopped with taskkill
func lookupWindows(ctx context.Context, e env, port int) (*Owner, error) {
	v4, err := e.run(ctx, "netstat", "-ano", "-p", "TCP")
	if err != nil {
		return nil, fmt.Errorf("netstat: %w", err)
	}
	// IPv6 is optional: netstat can fail for TCPv6 on a host without IPv6.
	v6, _ := e.run(ctx, "netstat", "-ano", "-p", "TCPv6")
	pid, found := 0, false
	for _, ep := range parseNetstatListeners(bytes.Join([][]byte{v4, v6}, []byte("\n"))) {
		if ep.Port == port {
			pid, found = ep.PID, true
			break
		}
	}
	if !found {
		return nil, nil
	}

	tasks, err := e.run(ctx, "tasklist", "/V", "/FO", "CSV", "/NH")
	if err != nil {
		return nil, fmt.Errorf("tasklist: %w", err)
	}
	task := parseTasklistVerbose(tasks)[pid]
	o := &Owner{Port: port, PID: pid, Process: trimExe(task.Image), User: task.User, Kind: KindProc}
	if pid == 0 || pid == 4 || !sameAccount(task.User, e.user) {
		o.Kind = KindSystem
		return o, nil
	}

	if isContainerProc(o.Process) {
		o.Kind = KindContainer
		if name := e.containerName(ctx, port); name != "" {
			o.Container = name
			o.StopCmd = []string{"docker", "stop", name}
		}
		return o, nil
	}

	// Fail closed: without the service list we cannot rule out a service.
	svcOut, err := e.run(ctx, "tasklist", "/svc", "/FO", "CSV", "/NH")
	if err != nil {
		return nil, fmt.Errorf("tasklist /svc: %w", err)
	}
	if names := parseTasklistServices(svcOut)[pid]; len(names) > 0 {
		// No StopCmd: Stop returns ErrNotStoppable for a service on Windows.
		o.Kind = KindService
		o.Manager = "windows service"
		o.Service = strings.Join(names, ", ")
	}
	return o, nil
}

// taskkillArgs returns the taskkill arguments for a stop request. SIGKILL forces
// the stop (/F); any other signal asks the process to close (no /F).
func taskkillArgs(pid int, sig syscall.Signal) []string {
	if sig == syscall.SIGKILL {
		return []string{"/F", "/PID", strconv.Itoa(pid)}
	}
	return []string{"/PID", strconv.Itoa(pid)}
}
