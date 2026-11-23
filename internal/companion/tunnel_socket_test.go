package companion

import (
	"os"
	"path/filepath"
	"testing"
)

// A socket left behind by a killed tunnel must not count as a live master: the
// next tunnel start removes it so waitForSocket waits for the new one.
func TestClearStaleControlSocketRemovesLeftoverSocket(t *testing.T) {
	if !controlMasterSupported {
		t.Skip("no unix-socket ControlMaster on this platform")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", "")
	profile := Profile{Name: "stale-socket"}
	path, err := ControlPath(profile.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	clearStaleControlSocket(profile)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("leftover control socket %s still exists (stat err: %v)", filepath.Base(path), err)
	}
}

func TestClearStaleControlSocketIsSafeWithoutASocket(t *testing.T) {
	if !controlMasterSupported {
		t.Skip("no unix-socket ControlMaster on this platform")
	}
	t.Setenv("HOME", t.TempDir())
	clearStaleControlSocket(Profile{Name: "no-socket"})
}
