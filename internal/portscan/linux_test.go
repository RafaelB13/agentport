//go:build linux

package portscan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fakeUsers stands in for /etc/passwd lookups.
func fakeUsers(uid uint32) string {
	return map[uint32]string{0: "root", 1000: "alice"}[uid]
}

// copyFixtureProc copies testdata/proc into a fresh temp dir and returns it.
// Symlinks cannot be stored in testdata, so the fd and cwd links are added by
// the caller.
func copyFixtureProc(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir("testdata/proc", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("testdata/proc", path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return dst
}

func link(t *testing.T, root, rel, target string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

func TestScanProc(t *testing.T) {
	root := copyFixtureProc(t)
	// pid 4321 owns sockets 1001 and 2001; pid 5555 owns 2002 and 2003.
	// Socket 1002 (0.0.0.0:5173) has no owner visible here, as if it belonged
	// to another user, so its PID stays 0.
	link(t, root, "4321/fd/0", "/dev/null")
	link(t, root, "4321/fd/3", "socket:[1001]")
	link(t, root, "4321/fd/4", "socket:[2001]")
	link(t, root, "4321/fd/5", "pipe:[99]")
	link(t, root, "5555/fd/3", "socket:[2002]")
	link(t, root, "5555/fd/4", "socket:[2003]")
	link(t, root, "4321/cwd", "/srv/app")
	link(t, root, "5555/cwd", "/home/bob/site")

	got, err := scanProc(context.Background(), root, fakeUsers)
	if err != nil {
		t.Fatalf("scanProc: %v", err)
	}
	want := []Listener{
		{Port: 3000, Bind: "127.0.0.1", PID: 4321, Process: "node",
			Cmdline: "node server.js --port three", Cwd: "/srv/app", User: "alice"},
		{Port: 5173, Bind: "0.0.0.0", User: "alice"},
		{Port: 8080, Bind: "::", PID: 4321, Process: "node",
			Cmdline: "node server.js --port three", Cwd: "/srv/app", User: "alice"},
		{Port: 9000, Bind: "::1", PID: 5555, Process: "python3",
			Cmdline: "python3 -m http.server 9000", Cwd: "/home/bob/site", User: "root"},
		{Port: 5173, Bind: "::", PID: 5555, Process: "python3",
			Cmdline: "python3 -m http.server 9000", Cwd: "/home/bob/site", User: "alice"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%+v\nwant:\n%+v", got, want)
	}
}

func TestScanProcWithoutProcfsFiles(t *testing.T) {
	root := t.TempDir()
	got, err := scanProc(context.Background(), root, fakeUsers)
	if err == nil {
		t.Fatalf("expected an error for a missing net/tcp, got %+v", got)
	}
}

func TestScanProcCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scanProc(ctx, "testdata/proc", fakeUsers); err == nil {
		t.Fatal("expected the canceled context to be reported")
	}
}

func TestParseSocketLink(t *testing.T) {
	tests := []struct {
		in     string
		want   uint64
		wantOK bool
	}{
		{in: "socket:[1001]", want: 1001, wantOK: true},
		{in: "pipe:[1001]"},
		{in: "socket:[abc]"},
		{in: "/dev/null"},
	}
	for _, tt := range tests {
		got, ok := parseSocketLink(tt.in)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("parseSocketLink(%q) = (%d, %v), want (%d, %v)", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}
