package selfupgrade

import (
	"fmt"
	"path/filepath"
	"strings"
)

// InstalledWithHomebrew reports whether the executable at path is the keg of a
// Homebrew install of agentport. Homebrew links bin/agentport into its Cellar, so
// the path is resolved through symlinks before it is compared: the resolved path
// must sit under a Cellar/agentport/ directory, either the default one or the one
// of HOMEBREW_PREFIX. getenv is injected so the check can be tested without
// touching the process environment.
//
// An error means the executable could not be resolved; callers should then
// continue with the normal upgrade rather than refuse it.
func InstalledWithHomebrew(executable string, getenv func(string) string) (bool, error) {
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return false, fmt.Errorf("resolve agentport executable: %w", err)
	}
	resolved = filepath.ToSlash(resolved)
	if strings.Contains(resolved, "/Cellar/agentport/") {
		return true, nil
	}
	prefix := strings.TrimSpace(getenv("HOMEBREW_PREFIX"))
	if prefix == "" {
		return false, nil
	}
	if realPrefix, err := filepath.EvalSymlinks(prefix); err == nil {
		prefix = realPrefix
	}
	cellar := filepath.ToSlash(filepath.Join(prefix, "Cellar")) + "/agentport/"
	return strings.HasPrefix(resolved, cellar), nil
}
