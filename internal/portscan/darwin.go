//go:build darwin

package portscan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func listen(ctx context.Context) ([]Listener, error) {
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pcLutnf").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return nil, fmt.Errorf("portscan: running lsof: %w", err)
		}
		// lsof exits 1 when nothing matches; that is an empty result.
		if len(out) == 0 {
			return nil, nil
		}
	}
	listeners, err := parseLsofF(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("portscan: parsing lsof: %w", err)
	}
	if len(listeners) == 0 {
		return nil, nil
	}
	fillCwd(ctx, listeners)
	fillCmdlines(ctx, listeners)
	return listeners, nil
}

// fillCwd sets Cwd with one lsof call for all owning pids. It is best effort.
func fillCwd(ctx context.Context, ls []Listener) {
	pids := distinctPIDs(ls)
	if len(pids) == 0 {
		return
	}
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-a", "-d", "cwd", "-F", "pfn", "-p", strings.Join(pids, ",")).Output()
	if err != nil && len(out) == 0 {
		return
	}
	cwd, err := parseLsofCwd(bytes.NewReader(out))
	if err != nil {
		return
	}
	for i := range ls {
		ls[i].Cwd = cwd[ls[i].PID]
	}
}

// fillCmdlines sets Cmdline with `ps` once per pid. It is best effort.
func fillCmdlines(ctx context.Context, ls []Listener) {
	cache := make(map[int]string)
	for i := range ls {
		pid := ls[i].PID
		if pid == 0 {
			continue
		}
		cmd, ok := cache[pid]
		if !ok {
			cmd = psCommand(ctx, pid)
			cache[pid] = cmd
		}
		ls[i].Cmdline = cmd
	}
}

func psCommand(ctx context.Context, pid int) string {
	out, err := exec.CommandContext(ctx, "ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func distinctPIDs(ls []Listener) []string {
	seen := make(map[int]bool)
	var pids []string
	for _, l := range ls {
		if l.PID != 0 && !seen[l.PID] {
			seen[l.PID] = true
			pids = append(pids, strconv.Itoa(l.PID))
		}
	}
	return pids
}
