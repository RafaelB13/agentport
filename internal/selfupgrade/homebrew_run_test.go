package selfupgrade

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rafaelb13/agentport/internal/companion"
	"github.com/rafaelb13/agentport/internal/upgrader"
)

// brewRunner returns a Runner whose local steps fail the test if they run: a
// Homebrew install must never download, replace or restart on this machine.
func brewRunner(t *testing.T, version string, profiles []companion.Profile) (Runner, *bytes.Buffer, *[]string) {
	t.Helper()
	var out, errOut bytes.Buffer
	var installed []string
	forbidden := func(step string) func() {
		return func() { t.Errorf("Homebrew upgrade ran the local step %s", step) }
	}
	runner := Runner{
		Stdout:         &out,
		Stderr:         &errOut,
		CurrentVersion: version,
		LatestTag: func(context.Context) (string, error) {
			forbidden("LatestTag")()
			return "", errors.New("unexpected")
		},
		Executable: func() (string, error) { forbidden("Executable")(); return "", nil },
		Prepare: func(context.Context, string, string) (upgrader.StagedBinary, error) {
			forbidden("Prepare")()
			return upgrader.StagedBinary{}, errors.New("unexpected")
		},
		Profiles: func() ([]companion.Profile, error) { return profiles, nil },
		InstallRemote: func(profile companion.Profile, tag string) (bool, error) {
			installed = append(installed, profile.Name+"@"+tag)
			return true, nil
		},
		StopCompanions:    func([]string) error { forbidden("StopCompanions")(); return nil },
		RestartCompanions: func([]string) error { forbidden("RestartCompanions")(); return nil },
		Replace:           func(string, string) error { forbidden("Replace")(); return nil },
	}
	return runner, &out, &installed
}

func TestHomebrewUpgradePrintsTheHintAndUpdatesServersToThisVersion(t *testing.T) {
	runner, out, installed := brewRunner(t, "v0.2.0", []companion.Profile{{Name: "m2", Destination: "bastion-m2"}})
	if err := runner.RunHomebrew(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "brew upgrade agentport") {
		t.Errorf("output lacks the brew hint:\n%s", out.String())
	}
	if len(*installed) != 1 || (*installed)[0] != "m2@v0.2.0" {
		t.Errorf("servers were updated as %v, want [m2@v0.2.0]", *installed)
	}
}

func TestHomebrewUpgradeReportsAFailedServerButStillUpdatesTheOthers(t *testing.T) {
	profiles := []companion.Profile{{Name: "a", Destination: "a"}, {Name: "b", Destination: "b"}}
	runner, _, installed := brewRunner(t, "v0.2.0", profiles)
	runner.InstallRemote = func(profile companion.Profile, tag string) (bool, error) {
		*installed = append(*installed, profile.Name+"@"+tag)
		if profile.Name == "a" {
			return false, errors.New("unreachable")
		}
		return true, nil
	}
	if err := runner.RunHomebrew(context.Background()); err == nil {
		t.Fatal("a failed server must make the upgrade fail")
	}
	if len(*installed) != 2 {
		t.Errorf("servers attempted = %v, want both", *installed)
	}
}

func TestHomebrewUpgradeLeavesServersAloneForANonReleaseBuild(t *testing.T) {
	for _, version := range []string{"dev", "", "0.2.0"} {
		runner, out, installed := brewRunner(t, version, []companion.Profile{{Name: "m2", Destination: "bastion-m2"}})
		if err := runner.RunHomebrew(context.Background()); err != nil {
			t.Fatalf("version %q: %v", version, err)
		}
		if len(*installed) != 0 {
			t.Errorf("version %q: servers were updated to a non-release build: %v", version, *installed)
		}
		if !strings.Contains(out.String(), "brew upgrade agentport") {
			t.Errorf("version %q: the brew hint is missing", version)
		}
	}
}
