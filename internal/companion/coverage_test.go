package companion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelb13/agentport/internal/testenv"
)

func TestLoggerWritesLevelsEscapesNewlinesAndExportsPrivately(t *testing.T) {
	testenv.IsolateUserDirs(t)
	logger, err := NewLogger("logs")
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	logger.Info("started %s", "ok")
	logger.Error("bad\nINJECTED [INFO] fake line")
	logger.Debug("hidden unless debugging")
	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "[INFO] started ok") || !strings.Contains(content, "[ERROR] bad") {
		t.Errorf("log = %q", content)
	}
	if strings.Count(content, "\n") != 2 || strings.Contains(content, "hidden") {
		t.Errorf("a newline in a message must not start a line of its own, and debug stays off: %q", content)
	}

	destination := filepath.Join(t.TempDir(), "export.log")
	if err := logger.Export(destination); err != nil {
		t.Fatal(err)
	}
	if exported, _ := os.ReadFile(destination); string(exported) != content {
		t.Errorf("export differs from the log")
	}
	if info, _ := os.Stat(destination); info.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
		t.Errorf("export mode = %v, want 0600", info.Mode().Perm())
	}
	if err := logger.Export("  "); err == nil {
		t.Error("an empty export destination was accepted")
	}
	if err := logger.Export(filepath.Join(t.TempDir(), "no", "such", "dir", "x.log")); err == nil {
		t.Error("an export into a missing directory was accepted")
	}
}

func TestDebugLoggingIsOptInThroughTheEnvironment(t *testing.T) {
	testenv.IsolateUserDirs(t)
	t.Setenv("AGENTPORT_LOG_LEVEL", "debug")
	logger, err := NewLogger("verbose")
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	logger.Debug("now visible")
	if data, _ := os.ReadFile(logger.Path()); !strings.Contains(string(data), "[DEBUG] now visible") {
		t.Errorf("log = %q", data)
	}
	// A nil logger is safe to use: callers pass one when logging is off.
	var none *Logger
	none.Debug("ignored")
}

func TestProfileHelpers(t *testing.T) {
	testenv.IsolateUserDirs(t)
	path, err := SSHIdentityPath("m2")
	if err != nil || filepath.Base(path) != "m2" || filepath.Base(filepath.Dir(path)) != "keys" {
		t.Fatalf("SSHIdentityPath = %q, %v", path, err)
	}
	for _, name := range []string{"", "../x", "a/b", "a b"} {
		if _, err := SSHIdentityPath(name); err == nil {
			t.Errorf("SSHIdentityPath(%q) was accepted", name)
		}
	}
}
