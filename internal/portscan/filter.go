package portscan

import (
	"net"
	"slices"
	"sort"
	"strings"
)

// systemDaemons are dropped unless Options.IncludeSystem is set. Names are the
// short process names as Linux (comm) and macOS (lsof) report them.
var systemDaemons = []string{
	"systemd-resolved",
	"cupsd",
	"rpcbind",
	"avahi-daemon",
	"chronyd",
	"systemd",
}

// commLen is the longest name the Linux kernel keeps in comm (TASK_COMM_LEN-1).
// Longer names are truncated, so comparisons must truncate too.
const commLen = 15

// normalize deduplicates raw listeners by port, applies opt and sorts the
// result by port, then by bind address. The input slice is not modified.
func normalize(raw []Listener, opt Options) []Listener {
	best := make(map[int]Listener, len(raw))
	for _, l := range raw {
		cur, seen := best[l.Port]
		if !seen || preferred(l, cur) {
			best[l.Port] = l
		}
	}
	out := make([]Listener, 0, len(best))
	for _, l := range best {
		if keep(l, opt) {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Bind < out[j].Bind
	})
	return out
}

// preferred reports whether a should replace b for the same port. A specific
// bind address beats a wildcard one, then an entry with a known PID wins, then
// IPv4 wins over IPv6 so the choice is deterministic.
func preferred(a, b Listener) bool {
	aw, bw := isWildcard(a.Bind), isWildcard(b.Bind)
	if aw != bw {
		return !aw
	}
	if (a.PID != 0) != (b.PID != 0) {
		return a.PID != 0
	}
	return isIPv4(a.Bind) && !isIPv4(b.Bind)
}

func isWildcard(bind string) bool {
	if bind == "" {
		return true
	}
	ip := net.ParseIP(bind)
	return ip != nil && ip.IsUnspecified()
}

func isIPv4(bind string) bool {
	ip := net.ParseIP(bind)
	return ip != nil && ip.To4() != nil
}

// keep applies the Options filters to one listener.
func keep(l Listener, opt Options) bool {
	if strings.HasPrefix(l.Process, "sshd") {
		return false
	}
	if slices.Contains(opt.ExcludePorts, l.Port) {
		return false
	}
	if l.Port < 1024 && !opt.IncludePrivileged {
		return false
	}
	if !opt.IncludeSystem && isSystemDaemon(l.Process) {
		return false
	}
	return true
}

func isSystemDaemon(process string) bool {
	if process == "" {
		return false
	}
	for _, d := range systemDaemons {
		if process == d || process == truncate(d, commLen) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
