package hostport

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// pidLine matches the `pid = N` line of `launchctl print`.
var pidLine = regexp.MustCompile(`(?m)^\s*pid = (\d+)\s*$`)

// lookupWith classifies the listener on port. Kind rules, in order:
//  1. a uid other than ours (or an unparseable one) → system, not stoppable
//  2. docker-proxy / vpnkit / com.docker.* → container, stopped with docker stop
//  3. a launchd job whose main pid is this process (macOS) → service
//  4. a systemd --user unit whose cgroup holds this process (Linux) → service
//  5. otherwise → proc, stopped with signals
//
// On Windows the rules are those of lookupWindows (winlookup.go).
func lookupWith(ctx context.Context, e env, port int) (*Owner, error) {
	if err := validPort(port); err != nil {
		return nil, err
	}
	if e.goos == "windows" {
		return lookupWindows(ctx, e, port)
	}
	out, err := e.run(ctx, "lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-F", "pcuL")
	recs, perr := parseLsof(out)
	if perr != nil {
		return nil, perr
	}
	if len(recs) == 0 {
		// lsof exits 1 when nothing matches; any other failure is real.
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			return nil, fmt.Errorf("lsof: %w", err)
		}
		return nil, nil
	}

	r := recs[0]
	o := &Owner{Port: port, PID: r.PID, Process: r.Command, User: r.Login, Kind: KindProc}
	if o.User == "" {
		o.User = r.UID
	}

	if uid, uerr := strconv.Atoi(r.UID); uerr != nil || uid != e.uid {
		o.Kind = KindSystem
		return o, nil
	}

	if isContainerProc(r.Command) {
		o.Kind = KindContainer
		if name := e.containerName(ctx, port); name != "" {
			o.Container = name
			o.StopCmd = []string{"docker", "stop", name}
		}
		return o, nil
	}

	if mgr, svc, stop, ok := e.managedService(ctx, r.PID); ok {
		o.Kind = KindService
		o.Manager, o.Service, o.StopCmd = mgr, svc, stop
	}
	return o, nil
}

func isContainerProc(cmd string) bool {
	return cmd == "docker-proxy" || cmd == "vpnkit" || strings.HasPrefix(cmd, "com.docker.")
}

// containerName returns the first running container publishing port, if any.
func (e env) containerName(ctx context.Context, port int) string {
	out, err := e.run(ctx, "docker", "ps", "--filter", "publish="+strconv.Itoa(port), "--format", "{{.Names}}")
	if err != nil {
		return ""
	}
	return firstLine(out)
}

// managedService reports whether pid is the main process of a service that
// restarts by itself, and the command that stops it.
func (e env) managedService(ctx context.Context, pid int) (manager, service string, stop []string, ok bool) {
	switch e.goos {
	case "darwin":
		return e.launchdService(ctx, pid)
	case "linux":
		return e.systemdUnit(ctx, pid)
	}
	return "", "", nil, false
}

// launchdService asks `launchctl blame` for the job that started pid, then
// confirms with `launchctl print` that the job's own pid is pid. Blame alone
// is not trusted: a shell started from a terminal app would otherwise be
// reported as a service of that app.
func (e env) launchdService(ctx context.Context, pid int) (string, string, []string, bool) {
	out, err := e.run(ctx, "launchctl", "blame", strconv.Itoa(pid))
	if err != nil {
		return "", "", nil, false
	}
	label := firstLine(out)
	if label == "" || strings.ContainsAny(label, " \t/") {
		return "", "", nil, false
	}
	domain := "gui/" + strconv.Itoa(e.uid) + "/" + label
	printed, err := e.run(ctx, "launchctl", "print", domain)
	if err != nil {
		return "", "", nil, false
	}
	m := pidLine.FindSubmatch(printed)
	if m == nil || string(m[1]) != strconv.Itoa(pid) {
		return "", "", nil, false
	}
	if svc, found := strings.CutPrefix(label, "homebrew.mxcl."); found {
		return "brew services", svc, []string{"brew", "services", "stop", svc}, true
	}
	return "launchd", label, []string{"launchctl", "bootout", domain}, true
}

// systemdUnit returns the --user unit whose status lists pid.
func (e env) systemdUnit(ctx context.Context, pid int) (string, string, []string, bool) {
	out, _ := e.run(ctx, "systemctl", "--user", "status", strconv.Itoa(pid)) // exits non-zero when inactive or not a unit
	unit := parseSystemctlUnit(out)
	if unit == "" {
		return "", "", nil, false
	}
	return "systemd --user", unit, []string{"systemctl", "--user", "stop", unit}, true
}

// parseSystemctlUnit finds the first `<name>.service` token in `systemctl status`
// output. The header line starts with a bullet, e.g. "● app.service - Desc".
func parseSystemctlUnit(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(strings.TrimLeft(strings.TrimSpace(line), "●○*"))
		if len(fields) > 0 && strings.HasSuffix(fields[0], ".service") {
			return fields[0]
		}
	}
	return ""
}

// firstLine returns the first non-empty trimmed line of out.
func firstLine(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}
