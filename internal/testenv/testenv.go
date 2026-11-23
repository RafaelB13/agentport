// Package testenv keeps tests away from the developer's real files.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// IsolateUserDirs points every per-user directory at a temporary one for the
// duration of the test. os.UserCacheDir and os.UserConfigDir read different
// variables on each platform (XDG_* on Linux, HOME on macOS, LocalAppData and
// AppData on Windows), so setting only XDG_CACHE_HOME leaves macOS and Windows
// writing into the real cache, where agentport keeps its bridge state,
// profiles, logs and received files.
func IsolateUserDirs(t testing.TB) string {
	t.Helper()
	home := t.TempDir()
	for _, name := range userDirVariables {
		t.Setenv(name, home)
	}
	return home
}

var userDirVariables = []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "LocalAppData", "AppData", "AGENTPORT_CACHE_DIR", "AGENTPORT_CONFIG_DIR"}

// Main runs a package's tests with every per-user directory redirected to a
// temporary one, so no test can reach the real files even if it forgets to call
// IsolateUserDirs. Use it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }
func Main(m *testing.M) int {
	home, err := os.MkdirTemp("", "agentport-test-home-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(home)
	for _, name := range userDirVariables {
		os.Setenv(name, home)
	}
	return m.Run()
}

// IsolateCacheDirs points the agentport cache and config directories at
// temporary ones for the duration of the test. It leaves HOME alone, so tools
// such as docker and ssh keep their own configuration. Use it in tests that
// start a companion or create ControlMaster sockets.
func IsolateCacheDirs(t testing.TB) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AGENTPORT_CACHE_DIR", filepath.Join(root, "cache"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg-cache"))
	t.Setenv("AGENTPORT_CONFIG_DIR", filepath.Join(root, "config"))
}

// CacheGuard proves that a test run did not create the developer's real
// agentport cache directory (os.UserCacheDir()/agentport). Create it before any
// environment change, then call Isolate and, after the run, Verify.
type CacheGuard struct {
	realDir  string
	existed  bool
	isolated bool
}

// NewCacheGuard records the real agentport cache directory and whether it
// exists now. It must run before Isolate changes the environment.
func NewCacheGuard() *CacheGuard {
	guard := &CacheGuard{}
	base, err := os.UserCacheDir()
	if err != nil {
		return guard
	}
	guard.realDir = filepath.Join(base, "agentport")
	_, err = os.Stat(guard.realDir)
	guard.existed = err == nil
	return guard
}

// Isolate points AGENTPORT_CACHE_DIR, XDG_CACHE_HOME and AGENTPORT_CONFIG_DIR at
// a fresh temporary directory, but only when AGENTPORT_CACHE_DIR is unset: a
// developer who chose a cache directory keeps it. The returned function removes
// the temporary directory.
func (g *CacheGuard) Isolate() (func(), error) {
	if strings.TrimSpace(os.Getenv("AGENTPORT_CACHE_DIR")) != "" {
		return func() {}, nil
	}
	root, err := os.MkdirTemp("", "agentport-test-")
	if err != nil {
		return nil, fmt.Errorf("create isolated agentport directories: %w", err)
	}
	if err := os.Setenv("AGENTPORT_CACHE_DIR", filepath.Join(root, "cache")); err != nil {
		os.RemoveAll(root)
		return nil, err
	}
	if err := os.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg-cache")); err != nil {
		os.RemoveAll(root)
		return nil, err
	}
	if err := os.Setenv("AGENTPORT_CONFIG_DIR", filepath.Join(root, "config")); err != nil {
		os.RemoveAll(root)
		return nil, err
	}
	g.isolated = true
	return func() { os.RemoveAll(root) }, nil
}

// Verify fails when the run created the real agentport cache directory, which
// did not exist before the run. A directory that already existed is never
// touched, and Verify does nothing when Isolate did not run.
func (g *CacheGuard) Verify() error {
	if !g.isolated || g.realDir == "" || g.existed {
		return nil
	}
	if _, err := os.Stat(g.realDir); err == nil {
		return fmt.Errorf("the test run created %s: a test reached the real agentport cache; isolate it with testenv", g.realDir)
	}
	return nil
}
