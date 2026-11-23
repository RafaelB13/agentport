package forward

import (
	"testing"
)

// withHostGOOS runs fn with hostGOOS replaced, so the Windows choice is testable anywhere.
func withHostGOOS(t *testing.T, goos string, fn func()) {
	t.Helper()
	saved := hostGOOS
	hostGOOS = goos
	defer func() { hostGOOS = saved }()
	fn()
}

func TestNewForwarderUsesControlMasterWhereSupported(t *testing.T) {
	withHostGOOS(t, "linux", func() {
		if !ControlMasterSupported() {
			t.Fatal("linux must support ControlMaster")
		}
		f := NewForwarder("ssh", "/tmp/cm.sock", "host", nil, nil)
		if _, ok := f.(*controlMaster); !ok {
			t.Fatalf("got %T, want *controlMaster", f)
		}
	})
}

func TestNewForwarderAlwaysPerProcessOnWindows(t *testing.T) {
	withHostGOOS(t, "windows", func() {
		if ControlMasterSupported() {
			t.Fatal("windows must not report ControlMaster support")
		}
		// A control path is given on purpose: Windows must still ignore it.
		f := NewForwarder("ssh.exe", `C:\cm\profile.sock`, "host", nil, &fakeStarter{})
		if _, ok := f.(*perProcess); !ok {
			t.Fatalf("got %T, want *perProcess", f)
		}
	})
}

func TestNewForwarderWithoutControlPathIsPerProcess(t *testing.T) {
	withHostGOOS(t, "darwin", func() {
		f := NewForwarder("ssh", "", "host", nil, nil)
		if _, ok := f.(*perProcess); !ok {
			t.Fatalf("got %T, want *perProcess", f)
		}
	})
}
