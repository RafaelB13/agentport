package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCacheBaseHonorsAgentportCacheDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTPORT_CACHE_DIR", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "xdg"))

	base, err := CacheBase()
	if err != nil {
		t.Fatal(err)
	}
	if base != dir {
		t.Fatalf("CacheBase() = %q, want %q", base, dir)
	}
	got, err := Dir("logs", "work.log")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "agentport", "logs", "work.log"); got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
}

func TestCacheBaseRejectsRelativeAgentportCacheDir(t *testing.T) {
	t.Setenv("AGENTPORT_CACHE_DIR", "relative/cache")
	if _, err := CacheBase(); err == nil {
		t.Fatal("a relative AGENTPORT_CACHE_DIR must be rejected")
	}
}

func TestCacheBaseHonorsXDGCacheHomeOnEveryOS(t *testing.T) {
	t.Setenv("AGENTPORT_CACHE_DIR", "")
	xdg := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)

	base, err := CacheBase()
	if err != nil {
		t.Fatal(err)
	}
	if base != xdg {
		t.Fatalf("CacheBase() = %q, want XDG_CACHE_HOME %q", base, xdg)
	}
}

func TestCacheBaseIgnoresRelativeXDGCacheHome(t *testing.T) {
	t.Setenv("AGENTPORT_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "relative")
	base, err := CacheBase()
	if err != nil {
		t.Fatal(err)
	}
	if base == "relative" {
		t.Fatal("a relative XDG_CACHE_HOME must be ignored")
	}
}

// shortTempDir returns a directory whose path is short enough for a unix socket:
// t.TempDir is under /var/folders on macOS, which is already too long.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "agp")
	if err != nil {
		t.Skipf("no short temporary directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestControlSocketStaysInCacheWhenPathIsShort(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket path")
	}
	cache := shortTempDir(t)
	t.Setenv("AGENTPORT_CACHE_DIR", cache)
	socket, err := ControlSocket("work")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(socket, filepath.Join(cache, "agentport", "cm")+string(filepath.Separator)) {
		t.Fatalf("socket %q is not under the cache directory", socket)
	}
	if len(socket) > MaxSocketPath {
		t.Fatalf("socket path %d bytes, limit %d", len(socket), MaxSocketPath)
	}
	info, err := os.Stat(filepath.Dir(socket))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
		t.Fatalf("socket directory mode %v, want 0700", info.Mode().Perm())
	}
}

func TestControlSocketFallsBackWhenCacheIsTooLong(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket fallback")
	}
	root := shortTempDir(t)
	previous := fallbackSocketRoot
	fallbackSocketRoot = root
	t.Cleanup(func() { fallbackSocketRoot = previous })
	long := filepath.Join(root, strings.Repeat("deep-", 30))
	t.Setenv("AGENTPORT_CACHE_DIR", long)
	socket, err := ControlSocket("work")
	if err != nil {
		t.Fatal(err)
	}
	if len(socket) > MaxSocketPath {
		t.Fatalf("fallback socket path %d bytes, limit %d", len(socket), MaxSocketPath)
	}
	if !strings.HasPrefix(socket, filepath.Join(root, "agentport-")) {
		t.Fatalf("fallback socket %q is not under the fallback root", socket)
	}
	info, err := os.Stat(filepath.Dir(socket))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("fallback directory mode %v, want 0700", info.Mode().Perm())
	}
}
