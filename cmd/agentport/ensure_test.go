package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rafaelb13/agentport/internal/companion"
)

// isolateEnsure gives the test its own config and cache directories, so no real
// profile or running Companion is read or written.
func isolateEnsure(t *testing.T) {
	t.Helper()
	t.Setenv("AGENTPORT_CONFIG_DIR", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
}

func saveEnsureProfile(t *testing.T, name string) {
	t.Helper()
	profile := companion.Profile{Name: name, Destination: "host.example", RemotePort: 39123, Token: "token"}
	if err := companion.SaveProfile(profile); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRejectsAnUnknownProfileWithoutStarting(t *testing.T) {
	isolateEnsure(t)
	started := false
	err := ensureCompanion("nope", &bytes.Buffer{}, func(string, bool) error { started = true; return nil })
	if err == nil || started {
		t.Fatalf("unknown profile: err=%v started=%v", err, started)
	}
}

func TestEnsureStartsAStoppedCompanionOnce(t *testing.T) {
	isolateEnsure(t)
	saveEnsureProfile(t, "ensure")
	var calls []string
	var out bytes.Buffer
	err := ensureCompanion("ensure", &out, func(name string, verbose bool) error {
		calls = append(calls, name)
		if verbose {
			t.Fatal("ensure must not start with verbose logging")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "ensure" || !strings.Contains(out.String(), "started") {
		t.Fatalf("calls=%v out=%q", calls, out.String())
	}
}

func TestEnsureIsANoOpWhenTheCompanionRuns(t *testing.T) {
	isolateEnsure(t)
	saveEnsureProfile(t, "running")
	control, err := companion.StartControl("running", func() any { return map[string]any{} }, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	started := false
	var out bytes.Buffer
	if err := ensureCompanion("running", &out, func(string, bool) error { started = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if started || !strings.Contains(out.String(), "already running") {
		t.Fatalf("started=%v out=%q", started, out.String())
	}
}

func TestEnsureReportsAStartFailure(t *testing.T) {
	isolateEnsure(t)
	saveEnsureProfile(t, "broken")
	err := ensureCompanion("broken", &bytes.Buffer{}, func(string, bool) error { return errors.New("boom") })
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}
