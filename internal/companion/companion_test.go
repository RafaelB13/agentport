package companion

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSaveAndLoadProfileUsesPrivateConfigFile(t *testing.T) {
	t.Setenv("AGENTPORT_CONFIG_DIR", t.TempDir())
	want := Profile{Name: "dev", Destination: "dev.example", RemotePort: 39123, Token: "pair-token", CreatedAt: time.Now().UTC().Round(0)}
	if err := SaveProfile(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadProfile("dev")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("profile = %#v, want %#v", got, want)
	}
	path, err := profilePath("dev")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("profile permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestProfileRejectsUnsafeName(t *testing.T) {
	profile := Profile{Name: "../other", Destination: "dev", RemotePort: 39123, Token: "token"}
	if err := profile.Validate(); err == nil {
		t.Fatal("expected invalid profile name")
	}
}

func TestProfileRejectsRelativeManagedIdentity(t *testing.T) {
	profile := Profile{Name: "dev", Destination: "dev", RemotePort: 39123, Token: "token", SSHIdentityFile: "keys/dev"}
	if err := profile.Validate(); err == nil {
		t.Fatal("expected relative identity file to be rejected")
	}
}

func TestListProfilesReturnsPersistedPairings(t *testing.T) {
	t.Setenv("AGENTPORT_CONFIG_DIR", t.TempDir())
	for _, name := range []string{"dev", "prod"} {
		if err := SaveProfile(Profile{Name: name, Destination: name + ".example", RemotePort: 39123, Token: "token"}); err != nil {
			t.Fatal(err)
		}
	}
	profiles, err := ListProfiles()
	if err != nil || len(profiles) != 2 || profiles[0].Name != "dev" || profiles[1].Name != "prod" {
		t.Fatalf("profiles = %#v, %v", profiles, err)
	}
}

func TestTunnelCommandUsesLoopbackReverseForward(t *testing.T) {
	command, err := TunnelCommand(Profile{Name: "dev", Destination: "dev", RemotePort: 39123, Token: "token"}, 45678)
	if err != nil {
		t.Fatal(err)
	}
	arguments := strings.Join(command.Args, " ")
	for _, expected := range []string{"-N", "-R 127.0.0.1:39123:127.0.0.1:45678", "ExitOnForwardFailure=yes", "ServerAliveInterval=30"} {
		if !strings.Contains(arguments, expected) {
			t.Errorf("command %q does not contain %q", arguments, expected)
		}
	}
}

func TestTunnelCommandUsesManagedIdentityWithoutPrompts(t *testing.T) {
	identity := filepath.Join(t.TempDir(), "agentport-key")
	command, err := TunnelCommand(Profile{Name: "dev", Destination: "dev", RemotePort: 39123, Token: "token", SSHIdentityFile: identity}, 45678)
	if err != nil {
		t.Fatal(err)
	}
	arguments := strings.Join(command.Args, " ")
	for _, expected := range []string{"-i " + identity, "IdentitiesOnly=yes", "BatchMode=yes"} {
		if !strings.Contains(arguments, expected) {
			t.Errorf("command %q does not contain %q", arguments, expected)
		}
	}
}

func TestLoggerRotatesAndKeepsOneLinePerEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	logger := &Logger{file: file, path: path, maxBytes: 512}
	defer logger.Close()

	for i := 0; i < 50; i++ {
		logger.Info("event %d value=%s", i, "line\nforged [ERROR] entry")
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("rotated log missing: %v", err)
	}
	if len(current) > 512 || len(previous) > 512 {
		t.Fatalf("log sizes %d/%d exceed the 512 byte bound", len(current), len(previous))
	}
	for _, chunk := range [][]byte{current, previous} {
		for _, line := range strings.Split(strings.TrimSpace(string(chunk)), "\n") {
			if !strings.Contains(line, " [INFO] event ") {
				t.Fatalf("forged or malformed line: %q", line)
			}
		}
	}
}
