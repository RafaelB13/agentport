package selfupgrade

import (
	"os"
	"path/filepath"
	"testing"
)

// kegBinary creates <root>/<relative> as a regular file and returns its path.
func kegBinary(t *testing.T, root, relative string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func noEnv(string) string { return "" }

func TestHomebrewInstallIsDetectedThroughTheSymlink(t *testing.T) {
	root := t.TempDir()
	keg := kegBinary(t, root, "prefix/Cellar/agentport/0.1.0/bin/agentport")
	link := filepath.Join(root, "prefix", "bin", "agentport")
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(keg, link); err != nil {
		t.Fatal(err)
	}

	got, err := InstalledWithHomebrew(link, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatalf("symlink %s into the Cellar was not detected as a Homebrew install", link)
	}
}

func TestBinaryOutsideTheCellarIsNotHomebrew(t *testing.T) {
	root := t.TempDir()
	executable := kegBinary(t, root, "home/user/.local/bin/agentport")

	got, err := InstalledWithHomebrew(executable, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatalf("%s was detected as a Homebrew install", executable)
	}
}

func TestAnotherFormulaInTheCellarIsNotAgentport(t *testing.T) {
	root := t.TempDir()
	executable := kegBinary(t, root, "prefix/Cellar/otherTool/1.0.0/bin/agentport")

	got, err := InstalledWithHomebrew(executable, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("a binary of another formula was detected as agentport's Homebrew install")
	}
}

func TestHomebrewPrefixDoesNotMatchBinariesOutsideItsCellar(t *testing.T) {
	root := t.TempDir()
	executable := kegBinary(t, root, "home/user/bin/agentport")
	prefix := filepath.Join(root, "brew")
	got, err := InstalledWithHomebrew(executable, func(name string) string {
		if name == "HOMEBREW_PREFIX" {
			return prefix
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatalf("%s is outside %s/Cellar/agentport but was detected as Homebrew", executable, prefix)
	}
}

func TestMissingExecutableIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "agentport")
	if _, err := InstalledWithHomebrew(missing, noEnv); err == nil {
		t.Fatal("a missing executable should be reported, so the caller can fall back to the normal upgrade")
	}
}
