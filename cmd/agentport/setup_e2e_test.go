package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelb13/agentport/internal/companion"
	"github.com/rafaelb13/agentport/internal/remote"
	"github.com/rafaelb13/agentport/internal/testenv"
	"github.com/rafaelb13/agentport/internal/upgrader"
)

// The whole lifecycle of a server, through the same functions the commands run:
// setup installs the verified binary and registers the MCP entry, running it
// again does not download anything, connect registers again, uninstall removes
// only agentport's entry. The server is a fake `ssh` that runs the remote
// commands locally with a home of its own, the harness is a script that logs its
// arguments, and the release download is replaced; everything else is real.
func TestServerLifecycleSetupConnectUninstall(t *testing.T) {
	server := testenv.NewFakeServer(t)
	harnessLog := filepath.Join(t.TempDir(), "codex.log")
	server.Install("codex", `echo "codex $*" >> '`+harnessLog+`'`)

	downloads := 0
	previous := remote.FetchRelease
	t.Cleanup(func() { remote.FetchRelease = previous })
	remote.FetchRelease = func(_ context.Context, options upgrader.Options) (upgrader.Fetched, error) {
		downloads++
		path := filepath.Join(t.TempDir(), "agentport")
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho "+options.Version+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return upgrader.Fetched{Path: path}, nil
	}
	logged := func() []string {
		data, _ := os.ReadFile(harnessLog)
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}

	if err := runSetup([]string{"devbox-e2e", "--profile", "e2e", "--version", "v0.7.3", "--agent", "codex", "--no-start"}); err != nil {
		t.Fatal(err)
	}
	profile, err := companion.LoadProfile("e2e")
	if err != nil || profile.Destination != "devbox-e2e" || profile.Token == "" {
		t.Fatalf("saved profile = %+v, %v", profile, err)
	}
	if downloads != 1 {
		t.Fatalf("downloads = %d, want the release fetched once", downloads)
	}
	if out, err := os.ReadFile(server.Path("agentport")); err != nil || !strings.Contains(string(out), "v0.7.3") {
		t.Fatalf("the server has %q, %v", out, err)
	}
	calls := logged()
	if len(calls) != 2 || calls[0] != "codex mcp remove agentport-e2e" || !strings.Contains(calls[1], "codex mcp add agentport-e2e") {
		t.Fatalf("harness calls after setup = %q, want a removal then an addition", calls)
	}
	for _, secret := range []string{"AGENTPORT_SESSION_TOKEN=" + profile.Token} {
		if !strings.Contains(calls[1], secret) {
			t.Errorf("the server was registered without %s", strings.SplitN(secret, "=", 2)[0])
		}
	}

	// Running setup again pairs afresh but has nothing to install.
	if err := runSetup([]string{"devbox-e2e", "--profile", "e2e", "--version", "v0.7.3", "--agent", "codex", "--no-start"}); err != nil {
		t.Fatal(err)
	}
	if downloads != 1 {
		t.Fatalf("a second setup downloaded again (%d downloads)", downloads)
	}
	repaired, err := companion.LoadProfile("e2e")
	if err != nil || repaired.Token == profile.Token {
		t.Fatalf("a second setup must issue a new pairing token (err %v)", err)
	}
	got := logged()
	if len(got) != 4 || got[2] != "codex mcp remove agentport-e2e" || !strings.Contains(got[3], "AGENTPORT_SESSION_TOKEN="+repaired.Token) || strings.Contains(got[3], profile.Token) {
		t.Fatalf("harness calls after the second setup = %q, want the re-registration to carry the new token and not the old one", got)
	}

	// connect registers the saved profile again, without touching the profile.
	if err := runConnect([]string{"e2e", "--agent", "codex"}); err != nil {
		t.Fatal(err)
	}
	if got := logged(); len(got) != 6 || !strings.Contains(got[5], "AGENTPORT_SESSION_TOKEN="+repaired.Token) {
		t.Fatalf("harness calls after connect = %q", got)
	}

	// uninstall removes only agentport's own entry.
	if err := runUninstall([]string{"e2e", "--agent", "codex"}); err != nil {
		t.Fatal(err)
	}
	if got := logged(); len(got) != 7 || got[6] != "codex mcp remove agentport-e2e" {
		t.Fatalf("harness calls after uninstall = %q, want exactly one more: the removal of agentport-e2e", got)
	}
}

// setup refuses to touch a server whose release cannot be verified, and leaves
// no profile behind.
func TestSetupWithAnUnverifiableReleaseInstallsNothingAndSavesNoProfile(t *testing.T) {
	server := testenv.NewFakeServer(t)
	server.Install("codex", "true")
	previous := remote.FetchRelease
	t.Cleanup(func() { remote.FetchRelease = previous })
	remote.FetchRelease = func(context.Context, upgrader.Options) (upgrader.Fetched, error) {
		return upgrader.Fetched{}, os.ErrPermission
	}
	err := runSetup([]string{"devbox-e2e", "--profile", "refused", "--version", "v0.7.3", "--agent", "codex", "--no-start"})
	if err == nil || !strings.Contains(err.Error(), "install agentport on devbox-e2e") {
		t.Fatalf("err = %v, want the install failure", err)
	}
	if _, statErr := os.Stat(server.Path("agentport")); statErr == nil {
		t.Error("a binary was installed although verification failed")
	}
	if _, loadErr := companion.LoadProfile("refused"); loadErr == nil {
		t.Error("a profile was saved for a server that was never set up")
	}
}
