//go:build linux

package portscan

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const procfsRoot = "/proc"

func listen(ctx context.Context) ([]Listener, error) {
	return scanProc(ctx, procfsRoot, lookupUsername)
}

// scanProc lists the LISTEN sockets described by the procfs tree at root. It
// is a parameter so tests can use a fixture tree. Owners are resolved through
// root/<pid>/fd. Processes that cannot be read keep PID 0 and an empty Process.
func scanProc(ctx context.Context, root string, username func(uid uint32) string) ([]Listener, error) {
	socks, err := readSockets(root)
	if err != nil {
		return nil, err
	}
	if len(socks) == 0 {
		return nil, nil
	}
	owners, err := socketOwners(ctx, root, socks)
	if err != nil {
		return nil, err
	}

	procs := make(map[int]procInfo)
	names := make(map[uint32]string)
	out := make([]Listener, 0, len(socks))
	for _, s := range socks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name, ok := names[s.UID]
		if !ok {
			name = username(s.UID)
			names[s.UID] = name
		}
		l := Listener{Port: s.Port, Bind: s.Bind, User: name}
		if pid, ok := owners[s.Inode]; ok {
			info, seen := procs[pid]
			if !seen {
				info = readProcInfo(root, pid)
				procs[pid] = info
			}
			l.PID = pid
			l.Process = info.comm
			l.Cmdline = info.cmdline
			l.Cwd = info.cwd
		}
		out = append(out, l)
	}
	return out, nil
}

// readSockets returns the LISTEN rows of root/net/tcp and root/net/tcp6. A
// missing tcp6 table (IPv6 disabled) is not an error.
func readSockets(root string) ([]netEntry, error) {
	v4, err := readNetTable(filepath.Join(root, "net", "tcp"), false)
	if err != nil {
		return nil, err
	}
	v6, err := readNetTable(filepath.Join(root, "net", "tcp6"), true)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return append(v4, v6...), nil
}

func readNetTable(path string, v6 bool) ([]netEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseProcNetTCP(f, v6)
}

// socketOwners maps the inode of each listening socket to the pid holding it.
// It walks root/<pid>/fd and looks for "socket:[inode]" links. Processes whose
// fd directory cannot be read (other users, or exited) are skipped.
func socketOwners(ctx context.Context, root string, socks []netEntry) (map[uint64]int, error) {
	wanted := make(map[uint64]bool, len(socks))
	for _, s := range socks {
		wanted[s.Inode] = true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	owners := make(map[uint64]int, len(socks))
	for _, e := range entries {
		pid, ok := parsePID(e.Name())
		if !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fdDir := filepath.Join(root, e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			inode, ok := parseSocketLink(target)
			if !ok || !wanted[inode] {
				continue
			}
			if _, taken := owners[inode]; !taken {
				owners[inode] = pid
			}
		}
	}
	return owners, nil
}

// parseSocketLink extracts the inode from a "socket:[1234]" fd link target.
func parseSocketLink(target string) (uint64, bool) {
	const prefix, suffix = "socket:[", "]"
	if !strings.HasPrefix(target, prefix) || !strings.HasSuffix(target, suffix) {
		return 0, false
	}
	inode, err := strconv.ParseUint(target[len(prefix):len(target)-len(suffix)], 10, 64)
	if err != nil {
		return 0, false
	}
	return inode, true
}

func parsePID(name string) (int, bool) {
	pid, err := strconv.Atoi(name)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// procInfo holds what the scan reads about one process.
type procInfo struct {
	comm    string
	cmdline string
	cwd     string
}

// readProcInfo reads comm, cmdline and cwd. Unreadable files give "".
func readProcInfo(root string, pid int) procInfo {
	dir := filepath.Join(root, strconv.Itoa(pid))
	return procInfo{
		comm:    readFileTrimmed(filepath.Join(dir, "comm")),
		cmdline: readCmdline(filepath.Join(dir, "cmdline")),
		cwd:     readLinkOrEmpty(filepath.Join(dir, "cwd")),
	}
}

func readFileTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// readCmdline joins the NUL-separated arguments of /proc/<pid>/cmdline.
func readCmdline(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(strings.TrimRight(string(b), "\x00"), "\x00", " ")
}

func readLinkOrEmpty(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	return target
}

// lookupUsername returns the account name for uid, or the decimal uid when the
// account is unknown.
func lookupUsername(uid uint32) string {
	id := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(id); err == nil {
		return u.Username
	}
	return id
}
